// Package spec is format v0 of spec/routes.json: each route's method,
// path, role, request and response fields with validation, and the
// use case it calls (docs/2-architecture.md § Specs and generators).
package spec

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/strictjson"
	domain "github.com/briolink/briolink/plugins/domain/spec"
)

// Format is the only value Routes.Format accepts.
const Format = "v0"

var (
	methods  = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	statuses = map[int]bool{200: true, 201: true, 204: true}
	ins      = map[string]bool{"path": true, "query": true, "body": true}
	literal  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	call     = regexp.MustCompile(`^([A-Z][A-Za-z0-9]*\.)?[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	scalar   = map[string]bool{"string": true, "int": true, "bool": true}
)

// Routes is spec/routes.json.
type Routes struct {
	Format string   `json:"format"`
	Roles  []string `json:"roles"`
	Routes []Route  `json:"routes"`
}

// Route calls one use case in one context: Call is "Entity.event" (the
// generated dispatcher for that statechart event) or the name of a pure
// function.
type Route struct {
	Name     string  `json:"name"`
	Method   string  `json:"method"`
	Path     string  `json:"path"`
	Role     string  `json:"role"`
	Context  string  `json:"context"`
	Call     string  `json:"call"`
	Status   int     `json:"status"`
	Request  []Param `json:"request"`
	Response []Field `json:"response"`
}

// Param is one request value. Its type uses the domain grammar; a leading
// "?" makes it optional. Min and Max bound an int's value or a string's or
// list's length; Pattern (RE2) applies to strings.
type Param struct {
	Name    string `json:"name"`
	In      string `json:"in"`
	Type    string `json:"type"`
	Min     *int   `json:"min"`
	Max     *int   `json:"max"`
	Pattern string `json:"pattern"`
	Doc     string `json:"doc"`
}

// Field is one response value.
type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc"`
}

// Decode decodes data strictly and validates it.
func Decode(data []byte) (*Routes, error) {
	var r Routes
	if err := strictjson.Decode(data, &r); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// Load reads and decodes a routes.json.
func Load(path string) (*Routes, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}
	return r, nil
}

// base strips "?" and "[]" prefixes; list reports whether any "[]" was seen.
func base(t string) (b string, list bool) {
	b = strings.TrimPrefix(t, "?")
	for strings.HasPrefix(b, "[]") {
		b, list = b[2:], true
	}
	return b, list
}

func typeShapeOK(t string) bool {
	b, _ := base(t)
	return domain.Primitives[b] || check.Pascal(b)
}

// Validate reports every rule the file breaks on its own; CheckAgainst
// resolves it against the domain model.
func (r *Routes) Validate() error {
	var l check.List
	if r.Format != Format {
		l.Addf("format", "must be %q, got %q", Format, r.Format)
	}
	roles := map[string]bool{}
	if len(r.Roles) == 0 {
		l.Addf("roles", "must declare at least one role")
	}
	for _, role := range r.Roles {
		if !check.Snake(role) {
			l.Addf("roles", "%q must be snake_case", role)
		}
		if roles[role] {
			l.Addf("roles", "%q is declared twice", role)
		}
		roles[role] = true
	}
	if len(r.Routes) == 0 {
		l.Addf("routes", "must declare at least one route")
	}
	names, shapes := map[string]bool{}, map[string]string{}
	for i, rt := range r.Routes {
		p := fmt.Sprintf("routes[%d]", i)
		if !check.Snake(rt.Name) {
			l.Addf(p, "name must be snake_case, got %q", rt.Name)
		} else {
			p = "routes[" + rt.Name + "]"
		}
		if names[rt.Name] {
			l.Addf(p, "name is used twice")
		}
		names[rt.Name] = true
		if !methods[rt.Method] {
			l.Addf(p, "method must be GET, POST, PUT, PATCH or DELETE, got %q", rt.Method)
		}
		pathParams, shape := parsePath(&l, p+".path", rt.Path)
		key := rt.Method + " " + shape
		if other, dup := shapes[key]; dup {
			l.Addf(p, "%s %s clashes with route %s", rt.Method, rt.Path, other)
		}
		shapes[key] = rt.Name
		if !roles[rt.Role] {
			l.Addf(p, "role %q is not declared in roles", rt.Role)
		}
		if !check.Snake(rt.Context) {
			l.Addf(p, "context must be snake_case, got %q", rt.Context)
		}
		if !call.MatchString(rt.Call) {
			l.Addf(p, "call must be Entity.event or a function name, got %q", rt.Call)
		}
		if !statuses[rt.Status] {
			l.Addf(p, "status must be 200, 201 or 204, got %d", rt.Status)
		} else if (rt.Status == 204) != (len(rt.Response) == 0) {
			l.Addf(p, "status 204 means no response fields, and only 204 may have none")
		}
		validateRequest(&l, p+".request", rt, pathParams)
		seen := map[string]bool{}
		for _, f := range rt.Response {
			fp := p + ".response[" + f.Name + "]"
			if !check.Snake(f.Name) {
				l.Addf(fp, "name must be snake_case")
			}
			if seen[f.Name] {
				l.Addf(fp, "declared twice")
			}
			seen[f.Name] = true
			if !typeShapeOK(f.Type) {
				l.Addf(fp, "invalid type %q", f.Type)
			}
		}
	}
	return l.Err()
}

// parsePath checks a path and returns its {params} and its shape with every
// param replaced by {}, so /a/{id} and /a/{key} clash.
func parsePath(l *check.List, p, path string) (map[string]bool, string) {
	params := map[string]bool{}
	if !strings.HasPrefix(path, "/") || (len(path) > 1 && strings.HasSuffix(path, "/")) {
		l.Addf(p, "%q must start with / and not end with one", path)
		return params, path
	}
	if path == "/" {
		return params, path
	}
	segs := strings.Split(path[1:], "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			name := s[1 : len(s)-1]
			if !check.Snake(name) {
				l.Addf(p, "param {%s} must be snake_case", name)
			}
			if params[name] {
				l.Addf(p, "param {%s} appears twice", name)
			}
			params[name] = true
			segs[i] = "{}"
		} else if !literal.MatchString(s) {
			l.Addf(p, "segment %q must be lower-case words joined by hyphens, or a {param}", s)
		}
	}
	return params, "/" + strings.Join(segs, "/")
}

func validateRequest(l *check.List, p string, rt Route, pathParams map[string]bool) {
	seen := map[string]bool{}
	for _, q := range rt.Request {
		qp := p + "[" + q.Name + "]"
		if !check.Snake(q.Name) {
			l.Addf(qp, "name must be snake_case")
		}
		if seen[q.Name] {
			l.Addf(qp, "declared twice")
		}
		seen[q.Name] = true
		if !typeShapeOK(q.Type) {
			l.Addf(qp, "invalid type %q", q.Type)
		}
		b, list := base(q.Type)
		optional := strings.HasPrefix(q.Type, "?")
		switch q.In {
		case "path":
			if !pathParams[q.Name] {
				l.Addf(qp, "is in the path but the path has no {%s}", q.Name)
			}
			if optional || list || (b != "string" && b != "int") {
				l.Addf(qp, "a path param must be a required string or int, got %q", q.Type)
			}
		case "query":
			if !scalar[b] {
				l.Addf(qp, "a query param must be string, int or bool (or a list of them), got %q", q.Type)
			}
		case "body":
			if rt.Method == "GET" || rt.Method == "DELETE" {
				l.Addf(qp, "%s takes no body", rt.Method)
			}
		default:
			l.Addf(qp, "in must be path, query or body, got %q", q.In)
		}
		bounded := list || b == "string" || b == "int"
		if (q.Min != nil || q.Max != nil) && !bounded {
			l.Addf(qp, "min and max apply only to int, string and list types")
		}
		if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
			l.Addf(qp, "min %d is above max %d", *q.Min, *q.Max)
		}
		if q.Min != nil && *q.Min < 0 && (list || b == "string") {
			l.Addf(qp, "a length bound cannot be negative")
		}
		if q.Pattern != "" {
			if b != "string" || list {
				l.Addf(qp, "pattern applies only to string types")
			} else if _, err := regexp.Compile(q.Pattern); err != nil {
				l.Addf(qp, "pattern does not compile: %v", err)
			}
		}
	}
	for name := range pathParams {
		found := false
		for _, q := range rt.Request {
			found = found || (q.Name == name && q.In == "path")
		}
		if !found {
			l.Addf(p, "path param {%s} has no request field with in \"path\"", name)
		}
	}
}

// CheckAgainst resolves every route against the domain model: its context
// exists, its call is a statechart event or a pure function there, and its
// named types are declared there.
func (r *Routes) CheckAgainst(contexts map[string]*domain.Context) error {
	var l check.List
	for _, rt := range r.Routes {
		p := "routes[" + rt.Name + "]"
		c := contexts[rt.Context]
		if c == nil {
			l.Addf(p, "context %q has no spec/domain/%s.json", rt.Context, rt.Context)
			continue
		}
		if entity, event, ok := strings.Cut(rt.Call, "."); ok {
			e := c.Entity(entity)
			switch {
			case e == nil:
				l.Addf(p, "call %q: %s is not an entity in %s", rt.Call, entity, rt.Context)
			case e.Statechart == nil:
				l.Addf(p, "call %q: %s has no statechart", rt.Call, entity)
			case !hasEvent(e.Statechart, event):
				l.Addf(p, "call %q: %s has no event %q", rt.Call, entity, event)
			}
		} else if f := c.Function(rt.Call); f == nil {
			l.Addf(p, "call %q is not a function in %s", rt.Call, rt.Context)
		} else if f.Kind != domain.KindPure {
			l.Addf(p, "call %q is a %s; a route calls an event or a pure function", rt.Call, f.Kind)
		}
		for _, q := range rt.Request {
			if !c.TypeOK(q.Type) {
				l.Addf(p+".request["+q.Name+"]", "type %q is not declared in %s", q.Type, rt.Context)
			}
		}
		for _, f := range rt.Response {
			if !c.TypeOK(f.Type) {
				l.Addf(p+".response["+f.Name+"]", "type %q is not declared in %s", f.Type, rt.Context)
			}
		}
	}
	return l.Err()
}

func hasEvent(sc *domain.Statechart, name string) bool {
	for _, e := range sc.Events {
		if e.Name == name {
			return true
		}
	}
	return false
}
