package gen

import (
	"fmt"
	"strings"

	"github.com/briolink/briolink/plugins/http/spec"
)

func methodName(rt spec.Route) string { return pascal(rt.Name) }
func reqName(rt spec.Route) string    { return pascal(rt.Name) + "Request" }
func respName(rt spec.Route) string   { return pascal(rt.Name) + "Response" }

// fullPath is the route's path under the base path.
func (g *gen) fullPath(rt spec.Route) string {
	base := g.in.Options.BasePath
	if base == "/" {
		return rt.Path
	}
	if rt.Path == "/" {
		return base
	}
	return base + rt.Path
}

// pattern is the net/http mux pattern of a route.
func (g *gen) pattern(rt spec.Route) string {
	p := g.fullPath(rt)
	if p == "/" {
		p = "/{$}"
	}
	return rt.Method + " " + p
}

func hasStatefulErrors(g *gen) []string {
	var out []string
	for _, c := range g.ctxs {
		for _, e := range g.in.Contexts[c].Entities {
			if e.Statechart != nil {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// routesFile declares the Service seam, the requests and responses, the
// route table and the handlers.
func (g *gen) routesFile() string {
	x := &w{}
	rs := g.in.Routes.Routes

	x.f("// Service is what the routes call: one method per route, taking the")
	x.f("// validated request and returning the response. The project implements it;")
	x.f("// each method does what its route's call names. Return ErrNotFound when")
	x.f("// something the request names does not exist, ErrConflict (409) when it")
	x.f("// clashes with the current state and ErrUnprocessable (422) when a rule")
	x.f("// refuses it; the domain package's ErrForbidden and ErrNoTransition (409)")
	x.f("// and InvariantError (422) are mapped too. Any other error is a 500 that")
	x.f("// says nothing about its cause.")
	x.f("type Service interface {")
	for _, rt := range rs {
		x.f("// %s runs %s in %s: %s %s, as %s.", methodName(rt), rt.Call, rt.Context, rt.Method, g.fullPath(rt), rt.Role)
		if rt.Status == 204 {
			x.f("%s(ctx context.Context, req %s) error", methodName(rt), reqName(rt))
		} else {
			x.f("%s(ctx context.Context, req %s) (%s, error)", methodName(rt), reqName(rt), respName(rt))
		}
	}
	x.f("}")
	x.f("")

	x.f("// RouteInfo is one row of the route table.")
	x.f("type RouteInfo struct {")
	x.f("Name, Method, Path, Role, Context, Call string")
	x.f("Status int")
	x.f("}")
	x.f("")
	x.f("// Routes is the route table, in spec order; Path includes the base path.")
	x.f("var Routes = []RouteInfo{")
	for _, rt := range rs {
		x.f("{Name: %q, Method: %q, Path: %q, Role: %q, Context: %q, Call: %q, Status: %d},",
			rt.Name, rt.Method, g.fullPath(rt), rt.Role, rt.Context, rt.Call, rt.Status)
	}
	x.f("}")
	x.f("")

	for _, rt := range rs {
		g.types(x, rt)
	}
	g.patterns(x)

	x.f("type handler struct {")
	x.f("svc  Service")
	x.f("auth Auth")
	x.f("}")
	x.f("")
	x.f("// Handler serves every route of the spec. A request without a role is a")
	x.f("// 401; one made as another role than the route's is a 403.")
	x.f("func Handler(svc Service, auth Auth) http.Handler {")
	x.f("h := &handler{svc: svc, auth: auth}")
	x.f("mux := http.NewServeMux()")
	for _, rt := range rs {
		x.f("mux.HandleFunc(%q, h.guard(%q, h.%s))", g.pattern(rt), rt.Role, lowerFirst(methodName(rt)))
	}
	x.f("return mux")
	x.f("}")
	x.f("")
	x.f("func (h *handler) guard(role string, next http.HandlerFunc) http.HandlerFunc {")
	x.f("return func(w http.ResponseWriter, r *http.Request) {")
	x.f("got, ok := h.auth(r)")
	x.f("if !ok {")
	x.f("writeProblem(w, http.StatusUnauthorized, \"authentication is required\", nil)")
	x.f("return")
	x.f("}")
	x.f("if got != role {")
	x.f("writeProblem(w, http.StatusForbidden, \"this role may not use this route\", nil)")
	x.f("return")
	x.f("}")
	x.f("next(w, r)")
	x.f("}")
	x.f("}")
	x.f("")

	cs := hasStatefulErrors(g)
	x.f("// writeError maps what a Service returned to a status.")
	x.f("func writeError(w http.ResponseWriter, err error) {")
	x.f("switch {")
	x.f("case errors.Is(err, ErrNotFound):")
	x.f("writeProblem(w, http.StatusNotFound, \"not found\", nil)")
	conflict, invariant := []string{"errors.Is(err, ErrConflict)"}, []string{"errors.Is(err, ErrUnprocessable)"}
	for _, c := range cs {
		conflict = append(conflict, fmt.Sprintf("errors.Is(err, %s.ErrForbidden)", pkgOf(c)), fmt.Sprintf("errors.Is(err, %s.ErrNoTransition)", pkgOf(c)))
		invariant = append(invariant, "isInvariant"+pascal(c)+"(err)")
	}
	x.f("case %s:", strings.Join(conflict, ", "))
	x.f("writeProblem(w, http.StatusConflict, \"the request conflicts with the current state\", nil)")
	x.f("case %s:", strings.Join(invariant, ", "))
	x.f("writeProblem(w, http.StatusUnprocessableEntity, \"the request would break a rule\", nil)")
	x.f("default:")
	x.f("writeProblem(w, http.StatusInternalServerError, \"internal error\", nil)")
	x.f("}")
	x.f("}")
	x.f("")
	for _, c := range cs {
		x.f("func isInvariant%s(err error) bool {", pascal(c))
		x.f("var e *%s.InvariantError", pkgOf(c))
		x.f("return errors.As(err, &e)")
		x.f("}")
		x.f("")
	}

	for _, rt := range rs {
		g.handler(x, rt)
	}
	return x.String()
}

// types declares a route's request, response and JSON shapes.
func (g *gen) types(x *w, rt spec.Route) {
	x.f("// %s is the validated request of %s.", reqName(rt), rt.Name)
	x.f("type %s struct {", reqName(rt))
	for _, p := range rt.Request {
		if p.Doc != "" {
			x.f("// %s", oneLine(p.Doc))
		}
		x.f("%s %s", pascal(p.Name), g.goType(rt.Context, p.Type))
	}
	x.f("}")
	x.f("")
	if body := g.bodyParams(rt); len(body) > 0 {
		x.f("type body%s struct {", methodName(rt))
		for _, p := range body {
			wt := g.wireGo(rt.Context, p.Type)
			if !isList(p.Type) {
				wt = "*" + wt
			}
			x.f("%s %s `json:%q`", pascal(p.Name), wt, p.Name)
		}
		x.f("}")
		x.f("")
	}
	if rt.Status == 204 {
		return
	}
	x.f("// %s is what %s returns.", respName(rt), rt.Name)
	x.f("type %s struct {", respName(rt))
	for _, f := range rt.Response {
		if f.Doc != "" {
			x.f("// %s", oneLine(f.Doc))
		}
		x.f("%s %s", pascal(f.Name), g.goType(rt.Context, f.Type))
	}
	x.f("}")
	x.f("")
	fields := make([]fld, len(rt.Response))
	for i, f := range rt.Response {
		fields[i] = fld{name: f.Name, typ: f.Type}
	}
	g.wireStruct(x, rt.Context, "resp"+methodName(rt), fields)
	x.f("")
	x.f("func encodeResp%s(v %s) resp%s {", methodName(rt), respName(rt), methodName(rt))
	x.f("var out resp%s", methodName(rt))
	g.encodeStmts(x, rt.Context, fields, "v", "out")
	x.f("return out")
	x.f("}")
	x.f("")
}

func (g *gen) bodyParams(rt spec.Route) []spec.Param {
	var out []spec.Param
	for _, p := range rt.Request {
		if p.In == "body" {
			out = append(out, p)
		}
	}
	return out
}

func (g *gen) paramsIn(rt spec.Route, in string) []spec.Param {
	var out []spec.Param
	for _, p := range rt.Request {
		if p.In == in {
			out = append(out, p)
		}
	}
	return out
}

func reName(rt spec.Route, p spec.Param) string { return "re" + methodName(rt) + pascal(p.Name) }

// patterns declares each route's compiled patterns.
func (g *gen) patterns(x *w) {
	var lines []string
	for _, rt := range g.in.Routes.Routes {
		for _, p := range rt.Request {
			if p.Pattern != "" {
				lines = append(lines, fmt.Sprintf("%s = regexp.MustCompile(%q)", reName(rt, p), p.Pattern))
			}
		}
	}
	if len(lines) == 0 {
		return
	}
	x.f("var (")
	for _, l := range lines {
		x.f("%s", l)
	}
	x.f(")")
	x.f("")
}

// constraints returns the statements that check a decoded value v of param
// p against its min, max and pattern.
func (g *gen) constraints(rt spec.Route, p spec.Param, v, at string) []string {
	var out []string
	_, lists, base := split(p.Type)
	switch {
	case lists > 0 && (p.Min != nil || p.Max != nil):
		out = append(out, g.bounds(fmt.Sprintf("len(%s)", v), at, p, "items", "item")...)
	case base == "string":
		if p.Min != nil || p.Max != nil {
			out = append(out, g.bounds(fmt.Sprintf("utf8.RuneCountInString(%s)", v), at, p, "characters", "character")...)
		}
		if p.Pattern != "" {
			out = append(out, fmt.Sprintf("if !%s.MatchString(%s) {", reName(rt, p), v),
				fmt.Sprintf("p.add(%s, %q)", at, "must match "+p.Pattern), "}")
		}
	case base == "int":
		if p.Min != nil {
			out = append(out, fmt.Sprintf("if %s < %d {", v, *p.Min), fmt.Sprintf("p.add(%s, %q)", at, fmt.Sprintf("must be at least %d", *p.Min)), "}")
		}
		if p.Max != nil {
			out = append(out, fmt.Sprintf("if %s > %d {", v, *p.Max), fmt.Sprintf("p.add(%s, %q)", at, fmt.Sprintf("must be at most %d", *p.Max)), "}")
		}
	}
	return out
}

// bounds checks a length expression against min and max.
func (g *gen) bounds(expr, at string, p spec.Param, plural, singular string) []string {
	unit := func(n int) string {
		if n == 1 {
			return singular
		}
		return plural
	}
	out := []string{fmt.Sprintf("switch n := %s; {", expr)}
	if p.Min != nil {
		out = append(out, fmt.Sprintf("case n < %d:", *p.Min),
			fmt.Sprintf("p.add(%s, %q)", at, fmt.Sprintf("must have at least %d %s", *p.Min, unit(*p.Min))))
	}
	if p.Max != nil {
		out = append(out, fmt.Sprintf("case n > %d:", *p.Max),
			fmt.Sprintf("p.add(%s, %q)", at, fmt.Sprintf("must have at most %d %s", *p.Max, unit(*p.Max))))
	}
	return append(out, "}")
}

// handler writes the handler of one route.
func (g *gen) handler(x *w, rt spec.Route) {
	name := lowerFirst(methodName(rt))
	x.f("func (h *handler) %s(w http.ResponseWriter, r *http.Request) {", name)
	x.f("var req %s", reqName(rt))
	x.f("p := &problems{}")

	if path := g.paramsIn(rt, "path"); len(path) > 0 {
		x.f("p.in = \"path\"")
		for _, q := range path {
			g.pathParam(x, rt, q)
		}
	}

	x.f("p.in = \"query\"")
	x.f("q := r.URL.Query()")
	query := g.paramsIn(rt, "query")
	for _, q := range query {
		g.queryParam(x, rt, q)
	}
	x.f("for _, k := range sortedKeys(q) {")
	if len(query) > 0 {
		names := make([]string, len(query))
		for i, q := range query {
			names[i] = fmt.Sprintf("%q", q.Name)
		}
		x.f("switch k {")
		x.f("case %s:", strings.Join(names, ", "))
		x.f("default:")
		x.f("p.add(k, \"is not a parameter of this route\")")
		x.f("}")
	} else {
		x.f("p.add(k, \"is not a parameter of this route\")")
	}
	x.f("}")

	if body := g.bodyParams(rt); len(body) > 0 {
		allowEmpty := true
		fields := make([]fld, len(body))
		for i, q := range body {
			q := q
			if !strings.HasPrefix(q.Type, "?") {
				allowEmpty = false
			}
			fields[i] = fld{name: q.Name, typ: q.Type, check: func(v, at string) []string {
				return g.constraints(rt, q, v, at)
			}}
		}
		x.f("var body body%s", methodName(rt))
		x.f("if readJSON(w, r, &body, p, %t) {", allowEmpty)
		g.decodeStmts(x, rt.Context, fields, "body", "req", `""`)
		x.f("}")
	}

	x.f("if p.any() {")
	x.f("writeInvalid(w, p)")
	x.f("return")
	x.f("}")
	if rt.Status == 204 {
		x.f("if err := h.svc.%s(r.Context(), req); err != nil {", methodName(rt))
		x.f("writeError(w, err)")
		x.f("return")
		x.f("}")
		x.f("w.WriteHeader(http.StatusNoContent)")
	} else {
		x.f("resp, err := h.svc.%s(r.Context(), req)", methodName(rt))
		x.f("if err != nil {")
		x.f("writeError(w, err)")
		x.f("return")
		x.f("}")
		x.f("writeJSON(w, %d, encodeResp%s(resp))", rt.Status, methodName(rt))
	}
	x.f("}")
	x.f("")
}

func (g *gen) pathParam(x *w, rt spec.Route, q spec.Param) {
	field := pascal(q.Name)
	at := fmt.Sprintf("%q", q.Name)
	x.f("{")
	if q.Type == "int" {
		x.f("v, ok := parseInt(r.PathValue(%q))", q.Name)
		x.f("if !ok {")
		x.f("p.add(%s, \"must be an integer\")", at)
		x.f("} else {")
		for _, s := range g.constraints(rt, q, "v", at) {
			x.f("%s", s)
		}
		x.f("req.%s = v", field)
		x.f("}")
	} else {
		x.f("v := r.PathValue(%q)", q.Name)
		for _, s := range g.constraints(rt, q, "v", at) {
			x.f("%s", s)
		}
		x.f("req.%s = v", field)
	}
	x.f("}")
}

func (g *gen) queryParam(x *w, rt spec.Route, q spec.Param) {
	optional, lists, base := split(q.Type)
	field := pascal(q.Name)
	at := fmt.Sprintf("%q", q.Name)
	parse := map[string]string{"int": "parseInt", "bool": "parseBool"}[base]
	x.f("{")
	x.f("vals := q[%q]", q.Name)
	x.f("switch {")
	x.f("case len(vals) == 0:")
	if !optional {
		x.f("p.add(%s, \"is required\")", at)
	}
	if lists > 0 {
		x.f("default:")
		x.f("list := make(%s, 0, len(vals))", g.goType(rt.Context, q.Type[strings.Index(q.Type, "[]"):]))
		if parse == "" {
			x.f("for _, s := range vals {")
			x.f("list = append(list, s)")
		} else {
			x.f("for i, s := range vals {")
			x.f("v, ok := %s(s)", parse)
			x.f("if !ok {")
			x.f("p.add(fmt.Sprintf(\"%%s[%%d]\", %s, i), %q)", at, wantQuery(base))
			x.f("continue")
			x.f("}")
			x.f("list = append(list, v)")
		}
		x.f("}")
		for _, s := range g.constraints(rt, q, "list", at) {
			x.f("%s", s)
		}
		if optional {
			x.f("req.%s = &list", field)
		} else {
			x.f("req.%s = list", field)
		}
	} else {
		x.f("case len(vals) > 1:")
		x.f("p.add(%s, \"must be given once\")", at)
		x.f("default:")
		if parse == "" {
			x.f("v := vals[0]")
		} else {
			x.f("v, ok := %s(vals[0])", parse)
			x.f("if !ok {")
			x.f("p.add(%s, %q)", at, wantQuery(base))
			x.f("break")
			x.f("}")
		}
		for _, s := range g.constraints(rt, q, "v", at) {
			x.f("%s", s)
		}
		if optional {
			x.f("req.%s = &v", field)
		} else {
			x.f("req.%s = v", field)
		}
	}
	x.f("}")
	x.f("}")
}

func wantQuery(base string) string {
	if base == "int" {
		return "must be an integer"
	}
	return "must be true or false"
}
