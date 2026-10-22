package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/coder"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repo makes a git repository holding a tiny Go module with one stub and its test.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                      "module example.com/e2e\n\ngo 1.27\n",
		"domain/acme/settles.go":      "package acme\n\n// Settles is true when amount is positive and equals balance.\nfunc Settles(balance, amount int64) bool { panic(\"not implemented\") }\n",
		"domain/acme/settles_test.go": "package acme\n\nimport \"testing\"\n\nfunc TestSettles(t *testing.T) {\n\tif !Settles(10, 10) || Settles(10, 9) || Settles(0, 0) {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
	}
	for p, s := range files {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// hostDocker stands in for the container engine: it runs the command that
// would run in the image directly in the worktree mounted at /workspace, and
// keeps the engine's arguments for the test to read.
func hostDocker(t *testing.T, seen *[][]string) Command {
	return func(ctx context.Context, argv []string) ([]byte, int, error) {
		if argv[0] != "docker" {
			return execCommand(ctx, argv)
		}
		*seen = append(*seen, argv)
		var dir string
		img := -1
		for i, a := range argv {
			if strings.HasSuffix(a, ":/workspace") {
				dir = strings.TrimSuffix(a, ":/workspace")
			}
			if a == "img" {
				img = i
			}
		}
		if dir == "" || img < 0 {
			t.Fatalf("not a workspace run: %q", argv)
		}
		return execCommand(ctx, append([]string{"env", "-C", dir}, argv[img+1:]...))
	}
}

func TestContainerEndToEnd(t *testing.T) {
	src := repo(t)
	var docker [][]string
	c := &Container{Repo: src, Dir: t.TempDir(), Image: "img", Tools: "/host/tools", ModCache: "/host/mod", Cache: "/host/cache", User: "1000:1000", Cmd: hostDocker(t, &docker)}

	cd := &scripted{answers: []func() (coder.Response, error){
		answer("m1", map[string]string{"domain/acme/settles.go": "package acme\n\nfunc Settles(balance, amount int64) bool { return amount == balance }\n"}),
		answer("m2", map[string]string{"domain/acme/settles.go": "package acme\n\n// Settles is true when amount is positive and equals balance.\nfunc Settles(balance, amount int64) bool {\n\treturn amount > 0 && amount == balance\n}\n"}),
	}}
	rec := &fakeRecords{}
	r := &Runner{Coder: cd, Workspaces: c, Records: rec, Generate: []string{"true"},
		Routing: &routing.Table{Format: "v0", Tiers: map[string]string{"coder": "qwen-test"}, Jobs: map[string]string{"pure_function": "coder"}}}
	ct := testContract(2)
	ct.AllowedFiles, ct.ExpectedArtifacts = []string{"domain/acme/settles.go"}, []string{"domain/acme/settles.go"}
	ct.VerificationCommands = [][]string{{"go", "test", "./domain/acme/..."}}

	res, err := r.Run(context.Background(), 60, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || len(res.Attempts) != 2 || res.Attempts[0].Check != "test" {
		t.Fatalf("%+v", res)
	}
	if rec.runs[0].Outcome != control.RunOutcomeFailed || rec.runs[1].Outcome != control.RunOutcomePassed {
		t.Errorf("%+v", rec.runs)
	}

	// Every command ran in the engine with no network, the worktree mounted.
	if len(docker) == 0 {
		t.Fatal("nothing ran in the container")
	}
	for _, a := range docker {
		joined := strings.Join(a, " ")
		for _, want := range []string{"run --rm --network none --user 1000:1000", ":/workspace -w /workspace", "-e GOPROXY=off", "/host/tools:/briolink/bin:ro", "/host/mod:/go/pkg/mod:ro", "/host/cache:/tmp/gocache", "img "} {
			if !strings.Contains(joined, want) {
				t.Errorf("%q lacks %q", joined, want)
			}
		}
	}

	// The worktree is gone and the repository's own file is untouched.
	if entries, _ := os.ReadDir(c.Dir); len(entries) != 0 {
		t.Errorf("worktree left behind: %v", entries)
	}
	b, _ := os.ReadFile(filepath.Join(src, "domain/acme/settles.go"))
	if !strings.Contains(string(b), "not implemented") {
		t.Errorf("repository file changed: %s", b)
	}
}

func TestWorktreeFiles(t *testing.T) {
	src := repo(t)
	c := &Container{Repo: src, Dir: t.TempDir(), Image: "img", Cmd: hostDocker(t, new([][]string))}
	ws, err := c.Create(context.Background(), "files")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	if b, err := ws.Read("domain/acme/settles.go"); err != nil || !strings.Contains(string(b), "Settles") {
		t.Fatalf("%q %v", b, err)
	}
	if err := ws.Write("domain/new/x.go", []byte("package new\n")); err != nil {
		t.Fatal(err)
	}
	names, err := ws.List("domain/acme")
	if err != nil || strings.Join(names, ",") != "settles.go,settles_test.go" {
		t.Errorf("%v %v", names, err)
	}
	if err := ws.Remove("domain/new/x.go"); err != nil {
		t.Fatal(err)
	}
	if err := ws.Remove("domain/new/x.go"); err != nil {
		t.Errorf("removing a missing file: %v", err)
	}
	for _, bad := range []string{"../outside", "/etc/passwd", "domain/../../outside"} {
		if err := ws.Write(bad, []byte("x")); err == nil {
			t.Errorf("Write(%q) left the workspace", bad)
		}
		if _, err := ws.Read(bad); err == nil {
			t.Errorf("Read(%q) left the workspace", bad)
		}
	}
}

func TestCreateFailureCleansUp(t *testing.T) {
	parent := t.TempDir()
	c := &Container{Repo: t.TempDir(), Dir: parent, Image: "img"} // not a git repository
	if _, err := c.Create(context.Background(), "x"); err == nil {
		t.Fatal("want an error")
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("left behind: %v", entries)
	}
}
