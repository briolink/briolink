// Command runner is the composition root for the runner: it wires the coder
// and the run records from the installation's operator config and the build
// worker container, loads one task contract and runs it for an issue
// (docs/2-architecture.md § Runner). Like cmd/control, it is the only place
// that names adapters.
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
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/briolink/briolink/adapter/coder/ollama"
	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/runner"
	"github.com/briolink/briolink/runner/contract"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		os.Exit(1)
	}
}

// The coders the runner can be bound to, by the name operator.json gives
// (cmd/control binds the same names).
var coders = map[string]func(model string) coder.Coder{
	"fake":   func(string) coder.Coder { return &coder.Fake{} },
	"ollama": func(model string) coder.Coder { return &ollama.Coder{Model: model} },
}

// errFailed is a run that ended without a pass: the exit status says so, the
// attempts are already printed.
var errFailed = errors.New("the task did not pass")

// run works one task. host, when not nil, runs every host command (git and
// the container engine) in place of os/exec.
func run(ctx context.Context, args []string, out io.Writer, host runner.Command) error {
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	configPath := fs.String("config", "operator.json", "the installation's operator config")
	repo := fs.String("repo", ".", "the project's git repository")
	base := fs.String("base", "", "the ref worktrees branch from (HEAD when empty)")
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
	newCoder, ok := coders[cfg.Adapters.Coder]
	if !ok {
		known := make([]string, 0, len(coders))
		for k := range coders {
			known = append(known, k)
		}
		sort.Strings(known)
		return fmt.Errorf("adapters.coder: no adapter %q (known: %s)", cfg.Adapters.Coder, strings.Join(known, ", "))
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

	// The records go to the control plane's database; events reach a running
	// control plane's streams only after it restarts (no broker is shared).
	db, err := store.Open(ctx, cfg.Database, events.New(), time.Now)
	if err != nil {
		return err
	}
	defer db.Close()

	r := &runner.Runner{
		Coder: newCoder(model),
		Workspaces: &runner.Container{
			Repo: *repo, Base: *base, Image: *image,
			Tools: abs(*tools), ModCache: abs(*modcache), Cache: abs(*cache),
			Cmd: host,
		},
		Records: db,
		Routing: table,
	}
	fmt.Fprintf(out, "runner: %s for issue #%d, coder %s (%s)\n", ct.TaskID, *issue, cfg.Adapters.Coder, model)
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
	fmt.Fprintln(out, "passed")
	return nil
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
