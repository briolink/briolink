package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/runner"
)

// installation writes an operator.json on the repo's own routing table and a
// project repository with one task contract, and returns the config path, the
// database path and the repository.
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
  "budget": {"coder_retries": 2, "minutes": 5, "reasoner_usd": 0},
  "expected_artifacts": ["domain/acme/settles_balance.go"]
}`)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "acme"}} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return cfg, db, repo
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

func TestRunsATaskInTheOfflineContainerAndRecordsIt(t *testing.T) {
	cfg, dbPath, repo := installation(t, "fake")
	old := coders["fake"]
	defer func() { coders["fake"] = old }()
	coders["fake"] = func(string) coder.Coder {
		return &coder.Fake{Answer: coder.Response{Files: map[string]string{"domain/acme/settles_balance.go": "package acme\n"}, Model: "scripted"}}
	}
	var docker [][]string
	var out bytes.Buffer
	args := []string{"-config", cfg, "-repo", repo, "-task", "acme-0001", "-issue", "7", "-image", "img", "-tools", "bin", "-modcache", "mod"}
	if err := run(context.Background(), args, &out, container(0, &docker)); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "attempt 1:") || !strings.HasSuffix(out.String(), "passed\n") {
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

	db, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task := int64(7)
	runs, err := db.ListRuns(context.Background(), &task, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Outcome != control.RunOutcomePassed || runs[0].Tier != "coder" || runs[0].Model != "scripted" {
		t.Errorf("records %+v", runs)
	}
}

func TestFailedCheckIsAnErrorAndRecorded(t *testing.T) {
	cfg, dbPath, repo := installation(t, "fake")
	old := coders["fake"]
	defer func() { coders["fake"] = old }()
	coders["fake"] = func(string) coder.Coder {
		return &coder.Fake{Answer: coder.Response{Files: map[string]string{"domain/acme/settles_balance.go": "package acme\n"}}}
	}
	var docker [][]string
	var out bytes.Buffer
	err := run(context.Background(), []string{"-config", cfg, "-repo", repo, "-task", "acme-0001", "-issue", "8"}, &out, container(1, &docker))
	if !errors.Is(err, errFailed) {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "failed gofmt") || !strings.Contains(out.String(), "stopped:") {
		t.Errorf("output %q", out.String())
	}
	db, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task := int64(8)
	runs, err := db.ListRuns(context.Background(), &task, 10)
	if err != nil || len(runs) == 0 || runs[0].Outcome == control.RunOutcomePassed {
		t.Errorf("records %+v, %v", runs, err)
	}
}

func TestRefusals(t *testing.T) {
	cfg, _, repo := installation(t, "fake")
	badCoder, _, _ := installation(t, "bogus")
	tests := map[string]struct {
		args []string
		want string
	}{
		"no task":          {[]string{"-config", cfg, "-issue", "1"}, "usage:"},
		"no issue":         {[]string{"-config", cfg, "-task", "acme-0001"}, "usage:"},
		"a path":           {[]string{"-config", cfg, "-repo", repo, "-task", "../x", "-issue", "1"}, "is an id, not a path"},
		"no such contract": {[]string{"-config", cfg, "-repo", repo, "-task", "acme-9999", "-issue", "1"}, "acme-9999.json"},
		"unknown coder":    {[]string{"-config", badCoder, "-repo", repo, "-task", "acme-0001", "-issue", "1"}, `adapters.coder: no adapter "bogus" (known: fake, ollama)`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := run(context.Background(), tc.args, &bytes.Buffer{}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
