package runner

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/adapter/coder/ollama"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/runner/contract"
)

const settlesStub = `package acme

import "example.com/acme/gen/domain/acme"

// SettlesBalance is a guard.
//
// Rules:
//   - True when the amount is positive and equals the balance exactly
//
// The balance is balance(invoice): the total of the lines minus what is paid.
func (Impl) SettlesBalance(invoice acme.Invoice, amount int64) bool {
	panic("not implemented: settles_balance")
}
`

// TestAcmeChecks runs the real checks of one real acme pure function,
// SettlesBalance (gofmt, go vet, regenerate-and-diff, the derived tests, the
// mutation check) with a coder that answers a weak version first, then a
// right one: the stub, a body without the positive-amount rule, then the full
// rule.
func TestAcmeChecks(t *testing.T) {
	const doc = settlesStub
	body := func(expr string) string {
		return strings.Replace(doc, "\tpanic(\"not implemented: settles_balance\")", "\treturn "+expr, 1)
	}
	cd := &scripted{answers: []func() (coder.Response, error){
		answer("scripted", map[string]string{"domain/acme/settles_balance.go": body("amount == balance(invoice)")}),
		answer("scripted", map[string]string{"domain/acme/settles_balance.go": body("amount > 0 && amount == balance(invoice)")}),
	}}
	res, rec := runAcme(t, cd, "scripted")
	if !res.Passed || len(res.Attempts) != 2 || res.Attempts[0].Check != "mutate" && res.Attempts[0].Check != "test" {
		t.Fatalf("%+v", res)
	}
	t.Logf("attempt 1 failed %s:\n%s", res.Attempts[0].Check, res.Attempts[0].Output)
	if len(rec.runs) != 2 || rec.runs[1].Outcome != control.RunOutcomePassed {
		t.Errorf("records %+v", rec.runs)
	}
}

// TestLive passes the same function through the real coder. Skipped unless
// BRIOLINK_LIVE_OLLAMA names the coder model (BRIOLINK_LIVE_OLLAMA_URL
// overrides the server). The commands run on the host, in the worktree,
// unless BRIOLINK_LIVE_DOCKER names the build worker image: then each runs by
// `docker run --network none` (liveWorkspaces).
func TestLive(t *testing.T) {
	model := os.Getenv("BRIOLINK_LIVE_OLLAMA")
	if model == "" {
		t.Skip("BRIOLINK_LIVE_OLLAMA is not set")
	}
	res, rec := runAcme(t, &ollama.Coder{Model: model, BaseURL: os.Getenv("BRIOLINK_LIVE_OLLAMA_URL")}, model)
	for _, a := range res.Attempts {
		t.Logf("attempt %d (%s) files=%v check=%q\n%s", a.N, a.Model, a.Files, a.Check, a.Output)
	}
	if !res.Passed {
		t.Fatalf("not passed, stopped: %s", res.Stop)
	}
	if rec.runs[len(rec.runs)-1].Outcome != control.RunOutcomePassed {
		t.Errorf("records %+v", rec.runs)
	}
}

// liveWorkspaces gives the worktrees of src: on the host, with the freshly
// built tools first on the PATH of every docker command, or, when
// BRIOLINK_LIVE_DOCKER names the build worker image, in the offline container
// (BRIOLINK_LIVE_TOOLS: the linux generate and mutate from build-tools.sh;
// BRIOLINK_LIVE_MODCACHE: the seeded module cache).
func liveWorkspaces(t *testing.T, src, tools string) *Container {
	t.Helper()
	if image := os.Getenv("BRIOLINK_LIVE_DOCKER"); image != "" {
		return &Container{Repo: src, Dir: t.TempDir(), Image: image,
			Tools: os.Getenv("BRIOLINK_LIVE_TOOLS"), ModCache: os.Getenv("BRIOLINK_LIVE_MODCACHE")}
	}
	var docker [][]string
	host := hostDocker(t, &docker)
	onHost := func(ctx context.Context, argv []string) ([]byte, int, error) {
		if argv[0] == "docker" {
			for i, a := range argv {
				if a == "img" {
					argv = append(append([]string{argv[0]}, argv[1:i]...), append([]string{"img", "env", "PATH=" + tools + ":" + os.Getenv("PATH")}, argv[i+1:]...)...)
					break
				}
			}
		}
		return host(ctx, argv)
	}
	return &Container{Repo: src, Dir: t.TempDir(), Image: "img", Cmd: onHost}
}

// runAcme sets up the acme project with SettlesBalance as a stub and runs its
// task with cd, the real tools and a real worktree.
func runAcme(t *testing.T, cd coder.Coder, model string) (*Result, *fakeRecords) {
	t.Helper()
	platform := filepath.Join("..")
	tools := t.TempDir()
	for _, cmd := range []string{"generate", "mutate"} {
		build := exec.Command("go", "build", "-o", filepath.Join(tools, cmd), "./cmd/"+cmd)
		build.Dir = platform
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", cmd, err, out)
		}
	}

	src := t.TempDir()
	copyTree(t, filepath.Join(platform, "generate", "testdata", "acme"), src, nil)
	copyTree(t, filepath.Join(platform, "plugins"), filepath.Join(src, "plugins"), func(p string) bool { return filepath.Base(p) == "plugin.json" })
	ref, err := os.ReadFile(filepath.Join(src, "reference", "reference.go"))
	if err != nil {
		t.Fatal(err)
	}
	impl := strings.Replace(string(ref), "package reference", "package acme", 1)
	i := strings.Index(impl, "func (Impl) SettlesBalance")
	j := i + strings.Index(impl[i:], "\n}\n") + 3
	impl = impl[:i] + "func balance(invoice acme.Invoice) int64 { return total(invoice.Lines) - invoice.Paid }\n" + impl[j:]
	writeFile(t, src, "domain/acme/impl.go", impl)
	writeFile(t, src, "domain/acme/settles_balance.go", settlesStub)
	writeFile(t, src, "domain/acme/impl_test.go", `package acme

import (
	"testing"

	"example.com/acme/gen/domain/acme/acmetest"
	"example.com/acme/samples"
)

func TestInvoice(t *testing.T) { acmetest.Invoice(t, Impl{}, samples.Invoice{}) }
`)
	git(t, src, "init", "-q")
	git(t, src, "add", ".")
	git(t, src, "commit", "-q", "-m", "acme")

	ws := liveWorkspaces(t, src, tools)

	ct, err := contract.Decode([]byte(`{
  "format": "v0", "task_id": "acme-0001", "project_id": "acme",
  "objective": "SettlesBalance returns true exactly when a positive amount equals the balance",
  "kind": "logic",
  "spec_refs": ["spec/domain/acme.json#functions.settles_balance"],
  "allowed_files": ["domain/acme/settles_balance.go"],
  "dependencies": [],
  "acceptance_criteria": ["The generated transition tests pass", "The mutation check catches every mutant of SettlesBalance"],
  "verification_commands": [["go", "test", "./domain/acme/..."], ["mutate", "./domain/acme", "-func", "SettlesBalance"]],
  "risk_level": "low", "required_approvals": [],
  "budget": {"coder_retries": 3, "minutes": 20, "reasoner_usd": 0},
  "expected_artifacts": ["domain/acme/settles_balance.go"]
}`))
	if err != nil {
		t.Fatal(err)
	}
	rec := &fakeRecords{}
	r := &Runner{
		Coder:      cd,
		Workspaces: ws, Records: rec,
		Routing: &routing.Table{Format: "v0", Tiers: map[string]string{"coder": model}, Jobs: map[string]string{Job: "coder"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	res, err := r.Run(ctx, 60, ct)
	if err != nil {
		t.Fatal(err)
	}
	return res, rec
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, from, to string, keep func(string) bool) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || (keep != nil && !keep(p)) {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		writeFile(t, to, filepath.ToSlash(rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
