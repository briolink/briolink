package gen_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/briolink/briolink/core/project"
	"github.com/briolink/briolink/generate"
	"github.com/briolink/briolink/plugins/domain/gen"
	"github.com/briolink/briolink/plugins/domain/spec"
)

// fixture is the acme project the host test also generates; its gen/ is
// committed, so building and running it here tests what the host wrote.
const fixture = "../../../generate/testdata/acme"

func load(t *testing.T) *spec.Context {
	t.Helper()
	c, err := spec.Load(filepath.Join(fixture, "spec", "domain", "acme.json"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGenerate_IsDeterministicAndHeadered(t *testing.T) {
	in := gen.Input{Module: "example.com/acme", Contexts: []*spec.Context{load(t)}}
	a, err := gen.Generate(in)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := gen.Generate(in)
	for name, src := range a {
		if !bytes.Equal(src, b[name]) {
			t.Errorf("%s differs between runs", name)
		}
		if !bytes.Contains(src[:200], []byte("DO NOT EDIT")) {
			t.Errorf("%s has no DO NOT EDIT header", name)
		}
	}
	for _, want := range []string{
		"acme/types.go", "acme/funcs.go", "acme/errors.go", "acme/invoice_machine.go",
		"acme/acmetest/derived.go", "acme/stubs/stubs.go", "acme/stubs/line_acceptable.go", "acme/stubs/total.go",
	} {
		if a[want] == nil {
			t.Errorf("missing %s", want)
		}
	}
	// one stub file per function, plus stubs.go
	n := 0
	for name := range a {
		if strings.HasPrefix(name, "acme/stubs/") {
			n++
		}
	}
	if want := len(in.Contexts[0].Functions) + 1; n != want {
		t.Errorf("%d stub files, want %d", n, want)
	}
}

func TestGenerate_RejectsGoKeywordParam(t *testing.T) {
	c := load(t)
	c.Functions[len(c.Functions)-1].Params[0].Name = "type"
	_, err := gen.Generate(gen.Input{Module: "example.com/acme", Contexts: []*spec.Context{c}})
	if err == nil || !strings.Contains(err.Error(), `"type" is a Go keyword`) {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerate_RejectsTwoContextsOnOnePackage(t *testing.T) {
	a, b := load(t), load(t)
	a.Context, b.Context = "ab_c", "abc"
	_, err := gen.Generate(gen.Input{Module: "example.com/acme", Contexts: []*spec.Context{a, b}})
	if err == nil || !strings.Contains(err.Error(), `same package "abc"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestHost_NoSpecFiles(t *testing.T) {
	p, err := project.LoadProject(filepath.Join(fixture, "spec", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := generate.LoadManifests("../..", p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gen.Host{}.Generate(context.Background(), generate.Input{
		Root: t.TempDir(), Project: p, Plugin: p.Plugins[0], Manifest: ms["domain"], Kind: ms["domain"].SpecKinds[0],
	})
	if err == nil || !strings.Contains(err.Error(), "no files match spec/domain/*.json") {
		t.Fatalf("err = %v", err)
	}
}

// goTest runs `go test` in dir (a copy of the fixture module) on pkgs.
func goTest(t *testing.T, dir string, pkgs ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), append([]string{"test", "-count=1"}, pkgs...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// copyFixture copies the acme module (without the sqlite output, which
// needs a dependency) into a temp dir.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(fixture, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixture, p)
		if d.IsDir() {
			if rel == "gen/sqlite" || rel == "migrations" || rel == "spec/queries" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), src, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// The issue's "done when": the generated code builds, the derived tests
// pass on a hand-written reference and fail on the stubs.
func TestAcme_BuildsPassesOnReferenceFailsOnStubs(t *testing.T) {
	dir := copyFixture(t)
	if out, err := goTest(t, dir, "./gen/domain/...", "./reference/..."); err != nil {
		t.Fatalf("reference run failed:\n%s", out)
	}
	out, err := goTest(t, dir, "./stubscheck/...")
	if err == nil {
		t.Fatal("derived tests passed on the stubs")
	}
	if !strings.Contains(out, "panic: not implemented: ") {
		t.Fatalf("stubs failed, but not by reaching a stub (a build error?):\n%s", out)
	}
}

// The derived tests have teeth: a reference with a guard off by one fails.
func TestAcme_DerivedTestsCatchAWrongImplementation(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"guard boundary", "amount == total(invoice.Lines)-invoice.Paid", "amount >= total(invoice.Lines)-invoice.Paid"},
		{"guard quantity", "line.Quantity >= 1", "line.Quantity >= 0"},
		{"action breaks an invariant", "invoice.Paid += amount", "invoice.Paid += amount * 2"},
		{"guard ignores the balance", "amount < total(invoice.Lines)-invoice.Paid", "amount <= total(invoice.Lines)-invoice.Paid"},
		{"guard allows zero", "amount > 0 && amount == total", "amount >= 0 && amount == total"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t)
			file := filepath.Join(dir, "reference", "reference.go")
			src, err := os.ReadFile(file)
			if err != nil || !bytes.Contains(src, []byte(tc.from)) {
				t.Fatalf("cannot patch reference: %v", err)
			}
			os.WriteFile(file, bytes.Replace(src, []byte(tc.from), []byte(tc.to), 1), 0o644)
			out, err := goTest(t, dir, "./reference/...")
			if err == nil {
				t.Fatalf("derived tests passed on a wrong implementation:\n%s", out)
			}
			if !strings.Contains(out, "--- FAIL") {
				t.Fatalf("failed, but not in a test:\n%s", out)
			}
		})
	}
}
