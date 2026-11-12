// Command runner is the composition root for the runner: it wires the coder,
// the reasoner, the code host and the run records from the installation's
// operator config and the build worker container, loads one task contract and
// runs it for an issue (docs/2-architecture.md § Runner). It binds the same
// adapters as cmd/control, through cmd/internal/wire.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/briolink/briolink/cmd/internal/wire"
	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/control/vault"
	"github.com/briolink/briolink/runner"
	"github.com/briolink/briolink/runner/contract"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		os.Exit(1)
	}
}

// errFailed is a run that ended without a pass: the exit status says so, the
// attempts are already printed.
var errFailed = errors.New("the task did not pass")

// run works one task. host, when not nil, runs every host command (git and
// the container engine) in place of os/exec.
func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer, host runner.Command) error {
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	configPath := fs.String("config", "operator.json", "the installation's operator config")
	repo := fs.String("repo", ".", "the project's git repository")
	base := fs.String("base", "", "the ref worktrees branch from (HEAD when empty)")
	mergeInto := fs.String("merge-into", "", "the branch a pass's pull request asks to merge into (main when empty)")
	remote := fs.String("remote", "", "the git remote a pass is pushed to (origin when empty)")
	gitName := fs.String("git-name", "", "the name on the pass's commit (git's own config when empty)")
	gitEmail := fs.String("git-email", "", "the e-mail on the pass's commit (git's own config when empty)")
	tasks := fs.String("tasks", "", "the directory of task contracts (<repo>/tasks when empty)")
	task := fs.String("task", "", "the task contract to run: its id, the file name in the tasks directory without .json")
	issue := fs.Int64("issue", 0, "the task's issue number, the key of its run records")
	image := fs.String("image", "briolink-build-worker", "the build worker image (runner/container/Containerfile)")
	tools := fs.String("tools", "", "directory holding the linux generate and mutate binaries (runner/container/build-tools.sh)")
	modcache := fs.String("modcache", "", "the module cache seeded by runner/container/seed-modcache.sh")
	cache := fs.String("cache", "", "a directory kept for the Go build cache")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *task == "" || *issue <= 0 || fs.NArg() > 0 {
		return errors.New("usage: runner [flags] -task <id> -issue <n>")
	}

	cfg, err := operator.Load(*configPath)
	if err != nil {
		return err
	}
	table, err := routing.Load(cfg.RoutingFile)
	if err != nil {
		return err
	}
	model, ok := table.Tiers[table.Jobs[runner.Job]]
	if !ok {
		return fmt.Errorf("the routing table has no model for the job %q", runner.Job)
	}
	// Refuse a bad binding before touching the key or the database; nothing
	// is used yet, so no secret is needed.
	noSecret := func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("the secret store is not open")
	}
	if _, err = wire.All(wire.Deps{Cfg: cfg, Table: table, Secret: noSecret}); err != nil {
		return err
	}
	dir := *tasks
	if dir == "" {
		dir = filepath.Join(*repo, "tasks")
	}
	if *task != filepath.Base(*task) {
		return fmt.Errorf("-task %q is an id, not a path", *task)
	}
	ct, err := contract.Load(filepath.Join(dir, *task+".json"))
	if err != nil {
		return err
	}

	key, err := wire.LoadKey(getenv, cfg.MasterKeyCredential)
	if err != nil {
		return err
	}
	// The records go to the control plane's database; events reach a running
	// control plane's streams only after it restarts (no broker is shared).
	db, err := store.Open(ctx, cfg.Database, events.New(), time.Now)
	if err != nil {
		return err
	}
	defer db.Close()
	// The vault gives the code host its App key, audited like in the control
	// plane. The runner makes no tokens, so it accepts no role.
	vlt, err := vault.New(db.DB(), key, db, nil, time.Now)
	if err != nil {
		return err
	}
	if err := vlt.Verify(ctx); err != nil {
		return fmt.Errorf("master key %s: %w", cfg.MasterKeyCredential, err)
	}
	ports, err := wire.All(wire.Deps{Cfg: cfg, Table: table, Secret: vlt.Use})
	if err != nil {
		return err
	}

	r := &runner.Runner{
		Coder: ports.Coder,
		Workspaces: &runner.Container{
			Repo: *repo, Base: *base, Image: *image,
			Tools: abs(*tools), ModCache: abs(*modcache), Cache: abs(*cache),
			Remote: *remote, GitName: *gitName, GitEmail: *gitEmail,
			Cmd: host,
		},
		Records:  db,
		Routing:  table,
		Reasoner: ports.Reasoner,
		Host:     ports.CodeHost,
		Base:     *mergeInto,
		History:  history(cfg),
	}
	fmt.Fprintf(out, "runner: %s for issue #%d, coder %s (%s), reasoner %s, codehost %s\n",
		ct.TaskID, *issue, cfg.Adapters.Coder, model, cfg.Adapters.Reasoner, cfg.Adapters.CodeHost)
	res, err := r.Run(ctx, *issue, ct)
	if err != nil {
		return err
	}
	for _, a := range res.Attempts {
		fmt.Fprintf(out, "attempt %d: %s %s: %s\n", a.N, a.Tier, a.Model, outcome(a))
	}
	if !res.Passed {
		fmt.Fprintf(out, "stopped: %s\n", res.Stop)
		return errFailed
	}
	if res.PullRequest > 0 {
		fmt.Fprintf(out, "pull request #%d opened\n", res.PullRequest)
	}
	fmt.Fprintln(out, "passed")
	return nil
}

// history is the link to a task's full run history for the hand-off: the
// control plane's runs of that issue.
func history(cfg *operator.Config) func(issue int64) string {
	scheme := "http"
	if cfg.TLS != nil {
		scheme = "https"
	}
	return func(issue int64) string {
		return fmt.Sprintf("%s://%s/api/v1/runs?task=%d", scheme, cfg.Listen, issue)
	}
}

func outcome(a runner.Attempt) string {
	if a.Check == "" {
		return "passed"
	}
	return "failed " + a.Check
}

// abs makes a mounted directory absolute, as the container engine needs.
func abs(p string) string {
	if p == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
