package gen_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/briolink/briolink/core/project"
	"github.com/briolink/briolink/generate"
	domaingen "github.com/briolink/briolink/plugins/domain/gen"
	domain "github.com/briolink/briolink/plugins/domain/spec"
	"github.com/briolink/briolink/plugins/http/gen"
	"github.com/briolink/briolink/plugins/http/spec"
	sqlitegen "github.com/briolink/briolink/plugins/sqlite/gen"
)

// acme is the project the host test also generates; its gen/ is committed.
const acme = "../../../generate/testdata/acme"

// shop is a second project that covers what acme does not: query values,
// lists, optional values, a base path, every method and a context without
// a statechart.
const shop = "testdata/shop"

func registry() generate.Registry {
	return generate.Registry{
		generate.Key("domain", "domain"):  domaingen.Host{},
		generate.Key("http", "routes"):    gen.Host{},
		generate.Key("sqlite", "queries"): sqlitegen.Host{},
	}
}

// generated runs the host over the project at root and returns its files.
func generated(t *testing.T, root string) map[string][]byte {
	t.Helper()
	p, err := project.LoadProject(filepath.Join(root, "spec", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := generate.LoadManifests("../..", p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := generate.Run(context.Background(), root, p, ms, registry())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// module writes the project's spec and the files the host generated for it
// into a fresh Go module and returns its directory.
func module(t *testing.T, root string, mutate func(path string, src []byte) []byte) string {
	t.Helper()
	dir := t.TempDir()
	mod := "example.com/" + filepath.Base(root)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+mod+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, src := range generated(t, root) {
		if strings.HasPrefix(rel, "sqlite/") { // needs a dependency this module does not have
			continue
		}
		if mutate != nil {
			src = mutate(rel, src)
		}
		full := filepath.Join(dir, "gen", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func goTest(t *testing.T, dir string, pkgs ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), append([]string{"test", "-count=1"}, pkgs...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGenerate_IsDeterministicAndHeadered(t *testing.T) {
	a, b := generated(t, shop), generated(t, shop)
	for _, name := range []string{"http/runtime.go", "http/wire.go", "http/routes.go", "http/api_test.go"} {
		if a[name] == nil {
			t.Fatalf("missing %s", name)
		}
		if !bytes.Equal(a[name], b[name]) {
			t.Errorf("%s differs between runs", name)
		}
		if !bytes.Contains(a[name][:200], []byte("DO NOT EDIT")) {
			t.Errorf("%s has no DO NOT EDIT header", name)
		}
	}
}

// TestShop_GeneratedCodeBuildsAndItsTestsPass: the generated tests, which
// send every valid and every invalid request the spec describes, pass on
// the generated handlers.
func TestShop_GeneratedCodeBuildsAndItsTestsPass(t *testing.T) {
	dir := module(t, shop, nil)
	if out, err := goTest(t, dir, "./gen/http/..."); err != nil {
		t.Fatalf("%s", out)
	}
}

func TestAcme_GeneratedCodeBuildsAndItsTestsPass(t *testing.T) {
	dir := module(t, acme, nil)
	if out, err := goTest(t, dir, "./gen/http/..."); err != nil {
		t.Fatalf("%s", out)
	}
}

// The base path option prefixes every route, in the table and on the wire.
func TestShop_BasePath(t *testing.T) {
	files := generated(t, shop)
	routes := string(files["http/routes.go"])
	for _, want := range []string{`Path: "/api/items/{item_id}"`, `Path: "/api"`, `"GET /api/items"`, `"GET /api/{$}"`} {
		if want == `"GET /api/{$}"` {
			want = `"GET /api"`
		}
		if !strings.Contains(routes, want) {
			t.Errorf("routes.go lacks %s", want)
		}
	}
}

// The generated tests have teeth: each change below breaks one thing the
// spec requires, and the generated tests must notice.
func TestShop_GeneratedTestsCatchBrokenHandlers(t *testing.T) {
	for _, tc := range []struct {
		name, file, from, to string
	}{
		{"max length ignored", "http/routes.go", "case n > 50:", "case n > 51:"},
		{"min length ignored", "http/routes.go", "case n < 2:", "case n < 1:"},
		{"pattern ignored", "http/routes.go", "!reListItemsQuery.MatchString(v)", "false"},
		{"int minimum ignored", "http/routes.go", "if v < 1 {", "if v < 0 {"},
		{"int maximum ignored", "http/routes.go", "if v > 100 {", "if v > 101 {"},
		{"list too long accepted", "http/routes.go", "case n > 3:", "case n > 4:"},
		{"unknown query parameter accepted", "http/routes.go", `p.add(k, "is not a parameter of this route")`, `_ = k`},
		{"query given twice accepted", "http/routes.go", `case len(vals) > 1:`, `case false:`},
		{"required query accepted when absent", "http/routes.go", `p.add("sizes", "is required")`, `_ = p`},
		{"bad bool accepted", "http/routes.go", `p.add("active", "must be true or false")`, `_ = ok`},
		{"role not checked", "http/routes.go", "if got != role {", "if got != role && false {"},
		{"no authentication needed", "http/routes.go", "if !ok {", "if !ok && false {"},
		{"not found is a 500", "http/routes.go", "case errors.Is(err, ErrNotFound):", "case errors.Is(err, ErrNotFound) && false:"},
		{"errors leak", "http/routes.go", `writeProblem(w, http.StatusInternalServerError, "internal error", nil)`, `writeProblem(w, http.StatusInternalServerError, err.Error(), nil)`},
		{"unknown body field accepted", "http/runtime.go", "dec.DisallowUnknownFields()", ""},
		{"content type unchecked", "http/runtime.go", `err != nil || mt != "application/json"`, `false && (err != nil || mt != "application/json")`},
		{"trailing data accepted", "http/runtime.go", "err2 != io.EOF", "err2 != io.EOF && false"},
		{"enum unchecked", "http/wire.go", "switch string(v) {", "switch string(v) + \"\" {\n\tdefault:\n\t\treturn v"},
		{"nested field not required", "http/wire.go", `p.add(join(at, "weight"), "is required")`, `_ = p`},
		{"encoder drops a field", "http/wire.go", "out.Name = ptr(same[string](v.Name))", ""},
		{"body field not required", "http/routes.go", `p.add(join("", "size"), "is required")`, `_ = p`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hit := false
			dir := module(t, shop, func(path string, src []byte) []byte {
				if path == tc.file && bytes.Contains(src, []byte(tc.from)) {
					hit = true
					return bytes.Replace(src, []byte(tc.from), []byte(tc.to), 1)
				}
				return src
			})
			if !hit {
				t.Fatalf("%s has no %q to break", tc.file, tc.from)
			}
			out, err := goTest(t, dir, "./gen/http/...")
			if err == nil {
				t.Fatalf("the generated tests passed on a broken handler:\n%s", out)
			}
			if !strings.Contains(out, "--- FAIL") && !strings.Contains(out, "panic:") {
				t.Fatalf("failed, but not in a test (a build error?):\n%s", out)
			}
		})
	}
}

func TestParseOptions(t *testing.T) {
	o, err := gen.ParseOptions([]byte(`{}`))
	if err != nil || o.BasePath != "/" || o.ErrorFormat != "problem" {
		t.Fatalf("defaults = %+v, %v", o, err)
	}
	for in, want := range map[string]string{
		`{"base_path": "api"}`:      "must start with /",
		`{"base_path": "/api/"}`:    "not end with one",
		`{"error_format": "plain"}`: `must be "problem"`,
		`{"prefix": "/"}`:           `unknown field "prefix"`,
		`{"base_path": 3}`:          "base_path",
	} {
		if _, err := gen.ParseOptions([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to contain %q", in, err, want)
		}
	}
}

func TestGenerate_Rejections(t *testing.T) {
	load := func(t *testing.T) gen.Input {
		t.Helper()
		in := gen.Input{Module: "example.com/shop", Options: gen.Options{BasePath: "/", ErrorFormat: "problem"}}
		r, err := spec.Load(filepath.Join(shop, "spec", "routes.json"))
		if err != nil {
			t.Fatal(err)
		}
		in.Routes = r
		in.Contexts = contexts(t, shop)
		return in
	}
	for name, tc := range map[string]struct {
		break_ func(in *gen.Input)
		want   string
	}{
		"unknown context": {func(in *gen.Input) { in.Contexts = nil }, `context "shop" has no spec/domain/shop.json`},
		"bad base path":   {func(in *gen.Input) { in.Options.BasePath = "api" }, "base_path"},
		"no sample":       {func(in *gen.Input) { in.Routes.Routes[0].Request[1].Pattern = "^$" }, "no sample string satisfies"},
		"bad route":       {func(in *gen.Input) { in.Routes.Routes[0].Method = "TRACE" }, "method must be"},
	} {
		t.Run(name, func(t *testing.T) {
			in := load(t)
			tc.break_(&in)
			_, err := gen.Generate(in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestHost_NoRoutesFile(t *testing.T) {
	p, err := project.LoadProject(filepath.Join(shop, "spec", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := generate.LoadManifests("../..", p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gen.Host{}.Generate(context.Background(), generate.Input{
		Root: t.TempDir(), Project: p, Plugin: p.Plugins[1], Manifest: ms["http"], Kind: ms["http"].SpecKinds[0],
	})
	if err == nil || !strings.Contains(err.Error(), "routes.json") {
		t.Fatalf("err = %v", err)
	}
}

func contexts(t *testing.T, root string) map[string]*domain.Context {
	t.Helper()
	c, err := domain.Load(filepath.Join(root, "spec", "domain", "shop.json"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]*domain.Context{"shop": c}
}
