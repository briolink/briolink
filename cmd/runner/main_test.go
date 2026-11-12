package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/cmd/internal/wire"
	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/control/vault"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/codehost"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/port/reasoner"
	"github.com/briolink/briolink/runner"
)

// masterKey is the master key every test installation is sealed with.
const masterKey = "0707070707070707070707070707070707070707070707070707070707070707"

// credentials writes the master key as a systemd credential and returns the
// environment lookup that points at it.
func credentials(t *testing.T) func(string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "master-key"), masterKey+"\n")
	if err := os.Chmod(filepath.Join(dir, "master-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	return func(k string) string {
		if k == "CREDENTIALS_DIRECTORY" {
			return dir
		}
		return ""
	}
}

// installation writes an operator.json on the repo's own routing table and a
// project repository with one task contract and a bare remote called origin,
// and returns the config path, the database path and the repository.
func installation(t *testing.T, coderName string) (cfg, db, repo string) {
	t.Helper()
	dir := t.TempDir()
	db = filepath.Join(dir, "control.db")
	routing, err := filepath.Abs("../../spec/routing.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg = filepath.Join(dir, "operator.json")
	write(t, cfg, `{"format":"v0","listen":"127.0.0.1:8443","tls":null,"database":"`+db+`","routing_file":"`+routing+`",
"master_key_credential":"master-key","adapters":{"codehost":"fake","coder":"`+coderName+`","reasoner":"fake"}}`)
	repo = filepath.Join(dir, "acme")
	write(t, filepath.Join(repo, "domain", "acme", "settles_balance.go"), "package acme\n")
	write(t, filepath.Join(repo, "tasks", "acme-0001.json"), `{
  "format": "v0", "task_id": "acme-0001", "project_id": "acme",
  "objective": "SettlesBalance", "kind": "logic",
  "spec_refs": ["spec/domain/acme.json#functions.settles_balance"],
  "allowed_files": ["domain/acme/settles_balance.go"],
  "dependencies": [], "acceptance_criteria": ["it passes"],
  "verification_commands": [["go", "test", "./domain/acme/..."]],
  "risk_level": "low", "required_approvals": [],
  "budget": {"coder_retries": 2, "minutes": 5, "reasoner_usd": 1},
  "expected_artifacts": ["domain/acme/settles_balance.go"]
}`)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "acme"}} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	remote := filepath.Join(dir, "origin.git")
	for _, args := range [][]string{{"init", "-q", "--bare", remote}, {"-C", repo, "remote", "add", "origin", remote}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return cfg, db, repo
}

// bind replaces the "fake" adapters of the three ports for one test: the
// coder answers with files, the code host is host (with an issue to work on)
// and the reasoner answers every tier with answer.
func bind(t *testing.T, files map[string]string, host codehost.Host, answer string) *reasoner.Fake {
	t.Helper()
	oldCoder, oldHost, oldReasoner := wire.Coders["fake"], wire.CodeHosts["fake"], wire.Reasoners["fake"]
	t.Cleanup(func() {
		wire.Coders["fake"], wire.CodeHosts["fake"], wire.Reasoners["fake"] = oldCoder, oldHost, oldReasoner
	})
	fake := &reasoner.Fake{Tiers: map[reasoner.Tier]json.RawMessage{
		reasoner.Routine: json.RawMessage(answer), reasoner.Top: json.RawMessage(answer),
	}}
	wire.Coders["fake"] = func(wire.Deps) (coder.Coder, error) {
		return &coder.Fake{Answer: coder.Response{Files: files, Model: "scripted"}}, nil
	}
	wire.CodeHosts["fake"] = func(wire.Deps) (codehost.Host, error) { return host, nil }
	wire.Reasoners["fake"] = func(wire.Deps) (reasoner.Reasoner, error) { return fake, nil }
	return fake
}

// hostWithIssue is a fake code host holding one issue, number 1, and the
// branch the runner's push will have made (a real host gets it from the push).
func hostWithIssue(t *testing.T) *codehost.Fake {
	t.Helper()
	h := codehost.NewFake()
	if _, err := h.CreateIssue(context.Background(), codehost.NewIssue{Title: "SettlesBalance", Labels: []string{"state:queued"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.CreateBranch(context.Background(), "task/1-acme-0001", "main"); err != nil {
		t.Fatal(err)
	}
	return h
}

func write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// container answers git for real and the container engine with a fixed exit
// status, recording the engine's command lines.
func container(code int, docker *[][]string) runner.Command {
	return func(ctx context.Context, argv []string) ([]byte, int, error) {
		if argv[0] != "docker" {
			c := exec.CommandContext(ctx, argv[0], argv[1:]...)
			var buf bytes.Buffer
			c.Stdout, c.Stderr = &buf, &buf
			err := c.Run()
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return buf.Bytes(), ee.ExitCode(), nil
			}
			return buf.Bytes(), 0, err
		}
		*docker = append(*docker, argv)
		if code != 0 {
			return []byte("boom"), code, nil
		}
		return nil, 0, nil
	}
}

func TestRunsATaskInTheOfflineContainerAndOpensItsPullRequest(t *testing.T) {
	cfg, dbPath, repo := installation(t, "fake")
	host := hostWithIssue(t)
	bind(t, map[string]string{"domain/acme/settles_balance.go": "package acme // settled\n"}, host, `{"diagnosis":"unused","files":{}}`)
	var docker [][]string
	var out bytes.Buffer
	args := []string{"-config", cfg, "-repo", repo, "-task", "acme-0001", "-issue", "1", "-image", "img", "-tools", "bin", "-modcache", "mod",
		"-git-name", "briolink-bot", "-git-email", "bot@example.com"}
	if err := run(context.Background(), args, credentials(t), &out, container(0, &docker)); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "attempt 1:") || !strings.Contains(out.String(), "pull request #2 opened") || !strings.HasSuffix(out.String(), "passed\n") {
		t.Errorf("output %q", out.String())
	}
	if len(docker) == 0 {
		t.Fatal("no check ran in the container")
	}
	for _, argv := range docker {
		line := strings.Join(argv, " ")
		for _, want := range []string{"run --rm --network none", "GOPROXY=off", filepath.Join(mustAbs(t, "bin")) + ":/briolink/bin:ro", mustAbs(t, "mod") + ":/go/pkg/mod:ro", " img "} {
			if !strings.Contains(line, want) {
				t.Errorf("%q lacks %q", line, want)
			}
		}
	}

	// The pass was pushed under the bot's identity and opened as a pull
	// request that closes the issue, and the issue got the summary.
	branch := "task/1-acme-0001"
	remote := filepath.Join(filepath.Dir(dbPath), "origin.git")
	who, err := exec.Command("git", "-C", remote, "show", "-s", "--format=%an <%ae>", branch).CombinedOutput()
	if err != nil || strings.TrimSpace(string(who)) != "briolink-bot <bot@example.com>" {
		t.Errorf("commit %q, %v", who, err)
	}
	pr, err := host.GetPullRequest(context.Background(), 2)
	if err != nil || pr.Head != branch || pr.Base != "main" || !strings.Contains(pr.Body, "Closes #1") {
		t.Errorf("pull request %+v, %v", pr, err)
	}
	comments, _ := host.ListComments(context.Background(), 1)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "Pull request #2 is open") {
		t.Errorf("comments %+v", comments)
	}

	db, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task := int64(1)
	runs, err := db.ListRuns(context.Background(), &task, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Outcome != control.RunOutcomePassed || runs[0].Tier != "coder" || runs[0].Model != "scripted" {
		t.Errorf("records %+v", runs)
	}
}

// A task nobody can pass ends in the reasoner, then the founder: the issue
// gets state:needs-founder, the attempt summary and the link to its history.
func TestFailedTaskEscalatesThenHandsOverToTheFounder(t *testing.T) {
	cfg, dbPath, repo := installation(t, "fake")
	host := hostWithIssue(t)
	reasoning := bind(t, map[string]string{"domain/acme/settles_balance.go": "package acme\n"}, host, `{"diagnosis":"the task is cut wrong","files":{}}`)
	var docker [][]string
	var out bytes.Buffer
	err := run(context.Background(), []string{"-config", cfg, "-repo", repo, "-task", "acme-0001", "-issue", "1"}, credentials(t), &out, container(1, &docker))
	if !errors.Is(err, errFailed) {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "failed gofmt") || !strings.Contains(out.String(), "stopped:") {
		t.Errorf("output %q", out.String())
	}
	if len(reasoning.Calls()) == 0 {
		t.Error("the reasoner was never asked")
	}
	is, err := host.GetIssue(context.Background(), 1)
	if err != nil || !slices.Contains(is.Labels, "state:needs-founder") || slices.Contains(is.Labels, "state:queued") {
		t.Errorf("issue %+v, %v", is, err)
	}
	comments, _ := host.ListComments(context.Background(), 1)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "Attempt summary") || !strings.Contains(comments[0].Body, "Full history: http://127.0.0.1:8443/api/v1/runs?task=1") {
		t.Errorf("comments %+v", comments)
	}
	db, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task := int64(1)
	runs, err := db.ListRuns(context.Background(), &task, 10)
	if err != nil || len(runs) == 0 || runs[0].Outcome == control.RunOutcomePassed {
		t.Errorf("records %+v, %v", runs, err)
	}
}

func TestRefusals(t *testing.T) {
	cfg, _, repo := installation(t, "fake")
	badCoder, _, _ := installation(t, "bogus")
	badReasoner, _, _ := installation(t, "fake")
	write(t, badReasoner, strings.Replace(read(t, badReasoner), `"reasoner":"fake"`, `"reasoner":"bogus"`, 1))
	tests := map[string]struct {
		args []string
		env  func(string) string
		want string
	}{
		"no task":          {[]string{"-config", cfg, "-issue", "1"}, nil, "usage:"},
		"no issue":         {[]string{"-config", cfg, "-task", "acme-0001"}, nil, "usage:"},
		"a path":           {[]string{"-config", cfg, "-repo", repo, "-task", "../x", "-issue", "1"}, nil, "is an id, not a path"},
		"no such contract": {[]string{"-config", cfg, "-repo", repo, "-task", "acme-9999", "-issue", "1"}, nil, "acme-9999.json"},
		"unknown coder":    {[]string{"-config", badCoder, "-repo", repo, "-task", "acme-0001", "-issue", "1"}, nil, `adapters.coder: no adapter "bogus" (known: fake, ollama)`},
		"unknown reasoner": {[]string{"-config", badReasoner, "-repo", repo, "-task", "acme-0001", "-issue", "1"}, nil, `adapters.reasoner: no adapter "bogus" (known: claudecode, fake)`},
		"no master key":    {[]string{"-config", cfg, "-repo", repo, "-task", "acme-0001", "-issue", "1"}, func(string) string { return "" }, "CREDENTIALS_DIRECTORY is not set"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := tc.env
			if env == nil {
				env = credentials(t)
			}
			err := run(context.Background(), tc.args, env, &bytes.Buffer{}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A database sealed under another master key is refused before anything runs.
func TestRefusesAWrongMasterKey(t *testing.T) {
	cfg, dbPath, repo := installation(t, "fake")
	db, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	vlt, err := vault.New(db.DB(), bytes.Repeat([]byte{9}, 32), db, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := vlt.Put(context.Background(), "test", "BOT_APP_KEY", []byte("secret"), vault.Dates{}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	err = run(context.Background(), []string{"-config", cfg, "-repo", repo, "-task", "acme-0001", "-issue", "1"}, credentials(t), &bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "master key master-key") {
		t.Errorf("err = %v", err)
	}
}

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
