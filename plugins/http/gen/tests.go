package gen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/briolink/briolink/plugins/http/spec"
)

// testFile writes the tests derived from the spec, against any Service:
//
//   - every valid request is routed to its method with the values it
//     carried, and answered with the declared status and fields;
//   - every request the spec rules out is rejected before the Service is
//     reached;
//   - each route needs its role;
//   - the Service's errors map to the documented statuses;
//   - the JSON shape of each domain type decodes and encodes back unchanged.
func (g *gen) testFile() (string, error) {
	x := &w{}
	rs := g.in.Routes.Routes

	x.f("type apiCase struct {")
	x.f("name, method, target, ctype, body string")
	x.f("want int")
	x.f("}")
	x.f("")
	x.f("// fakeService records the call it gets and returns zero responses.")
	x.f("type fakeService struct {")
	x.f("calls []string")
	x.f("last  any")
	x.f("err   error")
	x.f("}")
	x.f("")
	for _, rt := range rs {
		if rt.Status == 204 {
			x.f("func (f *fakeService) %s(_ context.Context, req %s) error {", methodName(rt), reqName(rt))
			x.f("f.calls, f.last = append(f.calls, %q), req", methodName(rt))
			x.f("return f.err")
		} else {
			x.f("func (f *fakeService) %s(_ context.Context, req %s) (%s, error) {", methodName(rt), reqName(rt), respName(rt))
			x.f("f.calls, f.last = append(f.calls, %q), req", methodName(rt))
			x.f("return %s{}, f.err", respName(rt))
		}
		x.f("}")
		x.f("")
	}
	x.f("// do sends c to the handler as role; an empty role is not authenticated.")
	x.f("func do(svc Service, role string, c apiCase) *httptest.ResponseRecorder {")
	x.f("req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))")
	x.f("if c.ctype != \"\" {")
	x.f("req.Header.Set(\"Content-Type\", c.ctype)")
	x.f("}")
	x.f("rec := httptest.NewRecorder()")
	x.f("Handler(svc, func(*http.Request) (string, bool) { return role, role != \"\" }).ServeHTTP(rec, req)")
	x.f("return rec")
	x.f("}")
	x.f("")
	x.f("func wantOK(t *testing.T, rec *httptest.ResponseRecorder, status int, keys ...string) {")
	x.f("t.Helper()")
	x.f("if rec.Code != status {")
	x.f("t.Fatalf(\"status = %%d, want %%d: %%s\", rec.Code, status, rec.Body)")
	x.f("}")
	x.f("if status == http.StatusNoContent {")
	x.f("if rec.Body.Len() != 0 {")
	x.f("t.Fatalf(\"a 204 has a body: %%s\", rec.Body)")
	x.f("}")
	x.f("return")
	x.f("}")
	x.f("if ct := rec.Header().Get(\"Content-Type\"); ct != \"application/json\" {")
	x.f("t.Fatalf(\"Content-Type = %%q\", ct)")
	x.f("}")
	x.f("var m map[string]json.RawMessage")
	x.f("if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {")
	x.f("t.Fatalf(\"body is not a JSON object: %%v\", err)")
	x.f("}")
	x.f("var got []string")
	x.f("for k := range m {")
	x.f("got = append(got, k)")
	x.f("}")
	x.f("sort.Strings(got)")
	x.f("sort.Strings(keys)")
	x.f("if strings.Join(got, \",\") != strings.Join(keys, \",\") {")
	x.f("t.Fatalf(\"response fields = %%v, want %%v\", got, keys)")
	x.f("}")
	x.f("}")
	x.f("")
	x.f("func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, status int) {")
	x.f("t.Helper()")
	x.f("if rec.Code != status {")
	x.f("t.Fatalf(\"status = %%d, want %%d: %%s\", rec.Code, status, rec.Body)")
	x.f("}")
	x.f("if ct := rec.Header().Get(\"Content-Type\"); ct != \"application/problem+json\" {")
	x.f("t.Fatalf(\"Content-Type = %%q, want a problem\", ct)")
	x.f("}")
	x.f("var pr Problem")
	x.f("if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil || pr.Status != status {")
	x.f("t.Fatalf(\"not a problem body for %%d: %%s\", status, rec.Body)")
	x.f("}")
	x.f("}")
	x.f("")

	for _, rt := range rs {
		if err := g.routeTests(x, rt); err != nil {
			return "", err
		}
	}
	if len(rs) > 0 {
		if err := g.crossTests(x); err != nil {
			return "", err
		}
	}
	if err := g.wireTests(x); err != nil {
		return "", err
	}
	return x.String(), nil
}

// caseLit is an apiCase composite literal body, braces included.
func caseLit(r request, name string, want int) string {
	return fmt.Sprintf("{%q, %q, %q, %q, %q, %d}", name, r.method, r.target, r.ctype, r.body, want)
}

func (g *gen) routeTests(x *w, rt spec.Route) error {
	m := methodName(rt)
	b, err := g.baseline(rt)
	if err != nil {
		return err
	}
	base := g.render(rt, b)
	x.f("var %sBaseline = apiCase%s", lowerFirst(m), caseLit(base, "all params", rt.Status))
	x.f("")

	keys := make([]string, len(rt.Response))
	for i, f := range rt.Response {
		keys[i] = fmt.Sprintf("%q", f.Name)
	}
	ok := func(var_ string) {
		x.f("svc := &fakeService{}")
		x.f("rec := do(svc, %q, %s)", rt.Role, var_)
		x.f("wantOK(t, rec, %d%s)", rt.Status, prefixComma(strings.Join(keys, ", ")))
		x.f("if len(svc.calls) != 1 || svc.calls[0] != %q {", m)
		x.f("t.Fatalf(\"calls = %%v, want [%s]\", svc.calls)", m)
		x.f("}")
	}

	// valid requests
	x.f("func Test%s_AcceptsValid(t *testing.T) {", m)
	x.f("for _, c := range []apiCase{")
	x.f("%sBaseline,", lowerFirst(m))
	if min, any := g.minimal(rt, b); any {
		x.f("%s,", caseLit(g.render(rt, min), "optional params left out", rt.Status))
	}
	if len(g.bodyParams(rt)) > 0 && g.allOptionalBody(rt) {
		r := g.render(rt, b)
		r.body, r.ctype = "", ""
		x.f("%s,", caseLit(r, "empty body", rt.Status))
	}
	x.f("} {")
	x.f("t.Run(c.name, func(t *testing.T) {")
	ok("c")
	x.f("})")
	x.f("}")
	x.f("}")
	x.f("")

	// the values the service gets
	if asserts := g.valueAsserts(rt, b); len(asserts) > 0 {
		x.f("func Test%s_PassesValues(t *testing.T) {", m)
		x.f("svc := &fakeService{}")
		x.f("wantOK(t, do(svc, %q, %sBaseline), %d%s)", rt.Role, lowerFirst(m), rt.Status, prefixComma(strings.Join(keys, ", ")))
		x.f("req := svc.last.(%s)", reqName(rt))
		for _, a := range asserts {
			x.f("%s", a)
		}
		if min, any := g.minimal(rt, b); any {
			x.f("svc = &fakeService{}")
			x.f("wantOK(t, do(svc, %q, apiCase%s), %d%s)", rt.Role,
				caseLit(g.render(rt, min), "", rt.Status), rt.Status, prefixComma(strings.Join(keys, ", ")))
			x.f("req = svc.last.(%s)", reqName(rt))
			for _, q := range rt.Request {
				if strings.HasPrefix(q.Type, "?") {
					x.f("if req.%s != nil {", pascal(q.Name))
					x.f("t.Errorf(\"%s = %%v, want it absent\", req.%s)", q.Name, pascal(q.Name))
					x.f("}")
				}
			}
		}
		x.f("}")
		x.f("")
	}

	// invalid requests
	x.f("func Test%s_RejectsInvalid(t *testing.T) {", m)
	x.f("for _, c := range []apiCase{")
	for _, br := range g.breakages(rt, b) {
		x.f("%s,", caseLit(g.render(rt, br.c), br.what, br.want))
	}
	x.f("} {")
	x.f("t.Run(c.name, func(t *testing.T) {")
	x.f("svc := &fakeService{}")
	x.f("wantProblem(t, do(svc, %q, c), c.want)", rt.Role)
	x.f("if len(svc.calls) != 0 {")
	x.f("t.Fatalf(\"the service was called: %%v\", svc.calls)")
	x.f("}")
	x.f("})")
	x.f("}")
	x.f("}")
	x.f("")
	return nil
}

func prefixComma(s string) string {
	if s == "" {
		return ""
	}
	return ", " + s
}

// valueAsserts checks the scalar values the baseline carried arrive in the
// request unchanged.
func (g *gen) valueAsserts(rt spec.Route, b rcase) []string {
	var out []string
	for _, q := range rt.Request {
		optional, lists, base := split(q.Type)
		if lists > 0 {
			continue
		}
		var v any
		switch q.In {
		case "path":
			v = b.path[q.Name]
		case "query":
			v = firstQuery(b, q.Name)
		case "body":
			v = b.body[q.Name]
		}
		var want string
		switch {
		case base == "string" || g.kindOf(rt.Context, base) == kEnum:
			s := fmt.Sprint(v)
			want = fmt.Sprintf("%q", s)
			if base != "string" {
				want = fmt.Sprintf("%s(%q)", g.goType(rt.Context, base), s)
			}
		case base == "int":
			want = fmt.Sprint(v)
		case base == "bool":
			want = fmt.Sprint(v)
		default:
			continue
		}
		f := "req." + pascal(q.Name)
		msg := fmt.Sprintf("%s = %%v, want %s", q.Name, want)
		if optional {
			out = append(out, fmt.Sprintf("if %s == nil || *%s != %s {", f, f, want))
		} else {
			out = append(out, fmt.Sprintf("if %s != %s {", f, want))
		}
		out = append(out, fmt.Sprintf("t.Errorf(%q, %s)", msg, f))
		out = append(out, "}")
	}
	return out
}

// crossTests are the tests that span the routes.
func (g *gen) crossTests(x *w) error {
	rs := g.in.Routes.Routes
	x.f("func TestRoles(t *testing.T) {")
	x.f("for _, c := range []struct {")
	x.f("route string")
	x.f("role  string")
	x.f("req   apiCase")
	x.f("}{")
	for _, rt := range rs {
		x.f("{%q, %q, %sBaseline},", rt.Name, rt.Role, lowerFirst(methodName(rt)))
	}
	x.f("} {")
	x.f("t.Run(c.route, func(t *testing.T) {")
	x.f("svc := &fakeService{}")
	x.f("wantProblem(t, do(svc, \"\", c.req), http.StatusUnauthorized)")
	x.f("wantProblem(t, do(svc, \"nobody\", c.req), http.StatusForbidden)")
	x.f("if len(svc.calls) != 0 {")
	x.f("t.Fatalf(\"the service was called without the role: %%v\", svc.calls)")
	x.f("}")
	x.f("if rec := do(svc, c.role, c.req); rec.Code >= 400 {")
	x.f("t.Fatalf(\"the route's own role was refused: %%d %%s\", rec.Code, rec.Body)")
	x.f("}")
	x.f("})")
	x.f("}")
	x.f("}")
	x.f("")

	x.f("// TestRouteTable: the table lists every route once, in the mux's own terms.")
	x.f("func TestRouteTable(t *testing.T) {")
	x.f("if len(Routes) != %d {", len(rs))
	x.f("t.Fatalf(\"%%d routes, want %d\", len(Routes))", len(rs))
	x.f("}")
	for i, rt := range rs {
		x.f("if r := Routes[%d]; r.Name != %q || r.Method != %q || r.Path != %q || r.Role != %q || r.Call != %q || r.Status != %d {",
			i, rt.Name, rt.Method, g.fullPath(rt), rt.Role, rt.Call, rt.Status)
		x.f("t.Errorf(\"Routes[%d] = %%+v\", r)", i)
		x.f("}")
	}
	x.f("}")
	x.f("")

	first := rs[0]
	m := methodName(first)
	x.f("func TestUnknownRoutes(t *testing.T) {")
	x.f("svc := &fakeService{}")
	x.f("if rec := do(svc, %q, apiCase{method: \"GET\", target: %q}); rec.Code != http.StatusNotFound {", first.Role, g.fullPath(spec.Route{Path: "/no-such-route"}))
	x.f("t.Errorf(\"unknown path: status %%d\", rec.Code)")
	x.f("}")
	x.f("if len(svc.calls) != 0 {")
	x.f("t.Errorf(\"the service was called: %%v\", svc.calls)")
	x.f("}")
	x.f("}")
	x.f("")

	x.f("// TestServiceErrors: what a Service returns decides the status, and an")
	x.f("// unexpected error says nothing about its cause.")
	x.f("func TestServiceErrors(t *testing.T) {")
	x.f("for _, c := range []struct {")
	x.f("name string")
	x.f("err  error")
	x.f("want int")
	x.f("}{")
	x.f("{\"not found\", ErrNotFound, http.StatusNotFound},")
	x.f("{\"wrapped not found\", fmt.Errorf(\"loading: %%w\", ErrNotFound), http.StatusNotFound},")
	x.f("{\"conflict\", fmt.Errorf(\"storing: %%w\", ErrConflict), http.StatusConflict},")
	x.f("{\"unprocessable\", fmt.Errorf(\"checking: %%w\", ErrUnprocessable), http.StatusUnprocessableEntity},")
	for _, c := range hasStatefulErrors(g) {
		pkg := pkgOf(c)
		x.f("{\"%s forbidden\", %s.ErrForbidden, http.StatusConflict},", c, pkg)
		x.f("{\"%s no transition\", fmt.Errorf(\"step: %%w\", %s.ErrNoTransition), http.StatusConflict},", c, pkg)
		x.f("{\"%s invariant\", &%s.InvariantError{Entity: \"E\", Invariant: \"i\"}, http.StatusUnprocessableEntity},", c, pkg)
	}
	x.f("{\"anything else\", errors.New(\"database password is hunter2\"), http.StatusInternalServerError},")
	x.f("} {")
	x.f("t.Run(c.name, func(t *testing.T) {")
	x.f("svc := &fakeService{err: c.err}")
	x.f("rec := do(svc, %q, %sBaseline)", first.Role, lowerFirst(m))
	x.f("wantProblem(t, rec, c.want)")
	x.f("if strings.Contains(rec.Body.String(), \"hunter2\") {")
	x.f("t.Fatalf(\"the response leaks the error: %%s\", rec.Body)")
	x.f("}")
	x.f("})")
	x.f("}")
	x.f("}")
	x.f("")
	return nil
}

// wireTests check each domain type's JSON shape: a full sample decodes
// without problems and encodes back to the same JSON.
func (g *gen) wireTests(x *w) error {
	if len(g.wires) == 0 {
		return nil
	}
	x.f("func sameJSON(t *testing.T, got, want string) {")
	x.f("t.Helper()")
	x.f("var a, b any")
	x.f("if err := json.Unmarshal([]byte(got), &a); err != nil {")
	x.f("t.Fatal(err)")
	x.f("}")
	x.f("if err := json.Unmarshal([]byte(want), &b); err != nil {")
	x.f("t.Fatal(err)")
	x.f("}")
	x.f("if !reflect.DeepEqual(a, b) {")
	x.f("t.Fatalf(\"got %%s, want %%s\", got, want)")
	x.f("}")
	x.f("}")
	x.f("")
	for _, wt := range g.wires {
		if wt.enum {
			continue
		}
		tree, err := g.sample(wt.ctx, wt.name, nil, 0)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(tree)
		n := pascal(wt.ctx) + wt.name
		x.f("func TestWire%s_RoundTrip(t *testing.T) {", n)
		x.f("in := %q", string(b))
		x.f("var wv %s", wireName(wt.ctx, wt.name))
		x.f("if err := json.Unmarshal([]byte(in), &wv); err != nil {")
		x.f("t.Fatal(err)")
		x.f("}")
		x.f("var p problems")
		x.f("d := decode%s(wv, \"\", &p)", n)
		x.f("if p.any() {")
		x.f("t.Fatalf(\"a valid sample has problems: %%v\", p.list)")
		x.f("}")
		x.f("out, err := json.Marshal(encode%s(d))", n)
		x.f("if err != nil {")
		x.f("t.Fatal(err)")
		x.f("}")
		x.f("sameJSON(t, string(out), in)")
		x.f("}")
		x.f("")
	}
	return nil
}
