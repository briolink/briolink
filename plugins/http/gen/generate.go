// Package gen is the http plugin's generator: it turns spec/routes.json into
// the typed requests and responses, the request validation, the handlers
// and the route table, plus the tests derived from the spec
// (docs/2-architecture.md § Specs and generators, M3).
package gen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/briolink/briolink/core/check"
	domain "github.com/briolink/briolink/plugins/domain/spec"
	"github.com/briolink/briolink/plugins/http/spec"
)

// Options are the plugin's options from spec/project.json.
type Options struct {
	BasePath    string // prefix of every route: "/" or "/api" (no trailing slash)
	ErrorFormat string // "problem": RFC 9457 problem details
}

func (o Options) validate() error {
	var l check.List
	if !strings.HasPrefix(o.BasePath, "/") || (len(o.BasePath) > 1 && strings.HasSuffix(o.BasePath, "/")) {
		l.Addf("base_path", "%q must start with / and not end with one", o.BasePath)
	}
	if o.ErrorFormat != "problem" {
		l.Addf("error_format", "must be \"problem\", got %q", o.ErrorFormat)
	}
	return l.Err()
}

// Input is what Generate needs: the project's Go module path, the options,
// the routes and the domain contexts they call into.
type Input struct {
	Module   string
	Options  Options
	Routes   *spec.Routes
	Contexts map[string]*domain.Context
}

// Package is the name of the Go package the output is generated as.
const Package = "api"

// Generate returns files keyed by name relative to the plugin's directory
// in gen/:
//
//	runtime.go    the fixed support code: problems, JSON reading and writing
//	wire.go       the JSON shapes of the domain types the routes carry, with
//	              their decoding (validating) and encoding
//	routes.go     Service, the request and response types, the route table
//	              and the handlers
//	api_test.go   the tests derived from the spec
//
// It fails, listing every problem, before returning anything.
func Generate(in Input) (map[string][]byte, error) {
	if err := in.Options.validate(); err != nil {
		return nil, fmt.Errorf("options:\n%w", err)
	}
	if err := in.Routes.Validate(); err != nil {
		return nil, err
	}
	if err := in.Routes.CheckAgainst(in.Contexts); err != nil {
		return nil, err
	}
	g := &gen{in: in}
	if err := g.check(); err != nil {
		return nil, err
	}
	g.collect()

	tests, err := g.testFile()
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for name, body := range map[string]string{
		"runtime.go":  runtimeSrc,
		"wire.go":     g.wireFile(),
		"routes.go":   g.routesFile(),
		"api_test.go": tests,
	} {
		src, err := g.assemble(name, body)
		if err != nil {
			return nil, err
		}
		out[name] = src
	}
	return out, nil
}

type gen struct {
	in    Input
	ctxs  []string        // contexts the routes use, sorted
	wires []wireType      // domain types the routes carry, sorted
	seen  map[string]bool // "ctx/Name" already in wires
}

// check rejects what would not compile or would clash.
func (g *gen) check() error {
	var l check.List
	for _, rt := range g.in.Routes.Routes {
		for _, p := range rt.Request {
			if p.In == "path" && strings.Contains(p.Type, "[]") {
				l.Addf("routes["+rt.Name+"]", "path param %s cannot be a list", p.Name)
			}
		}
	}
	return l.Err()
}

// ---- naming ----

// pkgOf is the Go package name of a context, as the domain plugin spells it.
func pkgOf(ctx string) string { return strings.ReplaceAll(ctx, "_", "") }

func (g *gen) importPath(ctx string) string { return g.in.Module + "/gen/domain/" + pkgOf(ctx) }

func pascal(s string) string {
	var b strings.Builder
	for _, part := range strings.Split(s, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// ---- types ----

// split takes a type expression apart: optional, the "[]" depth and the base.
func split(t string) (optional bool, lists int, base string) {
	optional = strings.HasPrefix(t, "?")
	base = strings.TrimPrefix(t, "?")
	for strings.HasPrefix(base, "[]") {
		lists++
		base = base[2:]
	}
	return
}

func isList(t string) bool { _, n, _ := split(t); return n > 0 }

// elem drops one leading "[]" (after any "?").
func elem(t string) string { return strings.TrimPrefix(strings.TrimPrefix(t, "?"), "[]") }

type kind int

const (
	kScalar kind = iota
	kEnum
	kStruct
)

func (g *gen) kindOf(ctx, base string) kind {
	if domain.Primitives[base] {
		return kScalar
	}
	c := g.in.Contexts[ctx]
	for _, e := range c.Enums {
		if e.Name == base {
			return kEnum
		}
	}
	return kStruct
}

// fieldsOf returns the fields of a value or entity.
func (g *gen) fieldsOf(ctx, name string) []domain.Field {
	c := g.in.Contexts[ctx]
	for _, v := range c.Values {
		if v.Name == name {
			return v.Fields
		}
	}
	return c.Entity(name).Fields
}

func (g *gen) enumOf(ctx, name string) domain.Enum {
	for _, e := range g.in.Contexts[ctx].Enums {
		if e.Name == name {
			return e
		}
	}
	panic("not an enum: " + name)
}

// goType is the domain Go type of a type expression ("acme.Line", "[]int64").
func (g *gen) goType(ctx, t string) string {
	optional, lists, base := split(t)
	var b string
	switch base {
	case "string", "bool":
		b = base
	case "int":
		b = "int64"
	case "time":
		b = "time.Time"
	default:
		b = pkgOf(ctx) + "." + base
	}
	out := strings.Repeat("[]", lists) + b
	if optional {
		out = "*" + out
	}
	return out
}

// wireGo is the Go type a non-optional type expression decodes from JSON
// into; wire structs hold pointers so absence can be told from a zero value.
func (g *gen) wireGo(ctx, t string) string {
	_, lists, base := split(t)
	var b string
	switch g.kindOf(ctx, base) {
	case kStruct:
		b = wireName(ctx, base)
	default:
		b = g.goType(ctx, base)
	}
	return strings.Repeat("[]", lists) + b
}

func wireName(ctx, name string) string { return "wire" + pascal(ctx) + name }

// wireType is one domain type that crosses the wire.
type wireType struct {
	ctx, name string
	enum      bool
}

// collect finds every domain type the routes' bodies and responses carry.
func (g *gen) collect() {
	g.seen = map[string]bool{}
	ctxs := map[string]bool{}
	var visit func(ctx, t string)
	visit = func(ctx, t string) {
		_, _, base := split(t)
		if domain.Primitives[base] {
			return
		}
		key := ctx + "/" + base
		if g.seen[key] {
			return
		}
		g.seen[key] = true
		if g.kindOf(ctx, base) == kEnum {
			g.wires = append(g.wires, wireType{ctx, base, true})
			return
		}
		g.wires = append(g.wires, wireType{ctx, base, false})
		for _, f := range g.fieldsOf(ctx, base) {
			visit(ctx, f.Type)
		}
	}
	for _, rt := range g.in.Routes.Routes {
		ctxs[rt.Context] = true
		for _, p := range rt.Request {
			if p.In == "body" {
				visit(rt.Context, p.Type)
			}
		}
		for _, f := range rt.Response {
			visit(rt.Context, f.Type)
		}
	}
	for c := range ctxs {
		g.ctxs = append(g.ctxs, c)
	}
	sort.Strings(g.ctxs)
	sort.Slice(g.wires, func(i, j int) bool {
		a, b := g.wires[i], g.wires[j]
		if a.ctx != b.ctx {
			return a.ctx < b.ctx
		}
		return a.name < b.name
	})
}

// ---- assembling a file ----

// assemble puts the header, the package clause and exactly the imports the
// body uses in front of body, and formats the result.
func (g *gen) assemble(name, body string) ([]byte, error) {
	known := map[string]string{
		"context": "context", "json": "encoding/json", "errors": "errors", "fmt": "fmt", "io": "io",
		"mime": "mime", "http": "net/http", "httptest": "net/http/httptest", "url": "net/url",
		"reflect": "reflect", "regexp": "regexp", "sort": "sort", "strconv": "strconv", "strings": "strings",
		"testing": "testing", "time": "time", "utf8": "unicode/utf8",
	}
	for _, c := range g.in.Contexts {
		known[pkgOf(c.Context)] = g.importPath(c.Context)
	}
	pkg := Package
	f, err := parser.ParseFile(token.NewFileSet(), name, "package "+pkg+"\n"+body, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", name, err, body)
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	var std, mod []string
	for alias, path := range known {
		if !used[alias] {
			continue
		}
		if strings.Contains(strings.Split(path, "/")[0], ".") {
			mod = append(mod, path)
		} else {
			std = append(std, path)
		}
	}
	sort.Strings(std)
	sort.Strings(mod)
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by plugins/http/gen from spec/routes.json. DO NOT EDIT.\n\npackage %s\n\n", pkg)
	if len(std)+len(mod) > 0 {
		b.WriteString("import (\n")
		for _, p := range std {
			fmt.Fprintf(&b, "\t%q\n", p)
		}
		if len(std) > 0 && len(mod) > 0 {
			b.WriteString("\n")
		}
		for _, p := range mod {
			fmt.Fprintf(&b, "\t%q\n", p)
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(body)
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", name, err, b.String())
	}
	return src, nil
}

// w accumulates source text.
type w struct{ b strings.Builder }

func (x *w) f(format string, a ...any) { fmt.Fprintf(&x.b, format+"\n", a...) }
func (x *w) String() string            { return x.b.String() }
