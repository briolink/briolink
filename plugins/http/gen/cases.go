package gen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/briolink/briolink/plugins/http/spec"
)

// This file derives, from the spec alone, the requests the generated tests
// send: one valid baseline per route, and every way to break it that the
// spec rules out.

// rcase is one request to one route.
type rcase struct {
	path  map[string]string
	query [][2]string
	body  map[string]any // nil when the route has no body params
	raw   *string        // replaces the body text when set
	ctype string         // Content-Type; "" means application/json when there is a body
	none  bool           // send no Content-Type at all
}

func (c rcase) clone() rcase {
	out := c
	out.path = map[string]string{}
	for k, v := range c.path {
		out.path[k] = v
	}
	out.query = append([][2]string(nil), c.query...)
	if c.body != nil {
		out.body = cloneTree(c.body).(map[string]any)
	}
	return out
}

func cloneTree(v any) any {
	b, _ := json.Marshal(v)
	var out any
	json.Unmarshal(b, &out)
	return out
}

func (c rcase) dropQuery(name string) rcase {
	out := c.clone()
	out.query = nil
	for _, kv := range c.query {
		if kv[0] != name {
			out.query = append(out.query, kv)
		}
	}
	return out
}

// request is a rendered case.
type request struct {
	method, target, ctype, body string
}

func (g *gen) render(rt spec.Route, c rcase) request {
	target := g.fullPath(rt)
	for name, v := range c.path {
		target = strings.ReplaceAll(target, "{"+name+"}", pathEscape(v))
	}
	if len(c.query) > 0 {
		parts := make([]string, len(c.query))
		for i, kv := range c.query {
			parts[i] = queryEscape(kv[0]) + "=" + queryEscape(kv[1])
		}
		target += "?" + strings.Join(parts, "&")
	}
	r := request{method: rt.Method, target: target}
	switch {
	case c.raw != nil:
		r.body = *c.raw
	case c.body != nil:
		b, _ := json.Marshal(c.body)
		r.body = string(b)
	}
	if c.body != nil || c.raw != nil {
		r.ctype = "application/json"
	}
	if c.ctype != "" {
		r.ctype = c.ctype
	}
	if c.none {
		r.ctype = ""
	}
	return r
}

func pathEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 && (r == '-' || r == '_' || r == '.' || r == '~' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}

func queryEscape(s string) string { return pathEscape(s) }

// ---- samples ----

var stringPool = []string{"a", "abc", "a1", "abc-123", "x", "1", "user", "ab12cd", "acme-1", "A", "ABC", "hello"}
var badPool = []string{"", "!", "A!", "~~~", " ", "UPPER CASE", "a b", "@@", "../x", "é", "-", "0"}

func withinLength(s string, p *spec.Param) bool {
	n := utf8.RuneCountInString(s)
	return (p.Min == nil || n >= *p.Min) && (p.Max == nil || n <= *p.Max)
}

func matches(s string, p *spec.Param) bool {
	return p.Pattern == "" || regexp.MustCompile(p.Pattern).MatchString(s)
}

// sampleString finds a string the param accepts.
func sampleString(p *spec.Param) (string, bool) {
	var try []string
	try = append(try, stringPool...)
	lo := 1
	if p.Min != nil && *p.Min > lo {
		lo = *p.Min
	}
	for n := lo; n < lo+4; n++ {
		for _, seed := range []string{"a", "1", "A"} {
			try = append(try, strings.Repeat(seed, n))
		}
	}
	for _, s := range try {
		if withinLength(s, p) && matches(s, p) {
			return s, true
		}
	}
	return "", false
}

// sample builds a valid JSON-shaped value of type t. p, when set, holds the
// constraints of the outermost value.
func (g *gen) sample(ctx, t string, p *spec.Param, depth int) (any, error) {
	if depth > 8 {
		return nil, fmt.Errorf("type %s nests too deep to build a sample", t)
	}
	_, lists, base := split(t)
	if lists > 0 {
		n := 1
		if p != nil && p.Min != nil && *p.Min > n {
			n = *p.Min
		}
		if p != nil && p.Max != nil && *p.Max < n {
			n = *p.Max
		}
		out := make([]any, n)
		for i := range out {
			v, err := g.sample(ctx, elem(t), nil, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	switch base {
	case "string":
		if p != nil {
			s, ok := sampleString(p)
			if !ok {
				return nil, fmt.Errorf("no sample string satisfies min %v, max %v and pattern %q; simplify the pattern", deref(p.Min), deref(p.Max), p.Pattern)
			}
			return s, nil
		}
		return "a", nil
	case "int":
		v := int64(1)
		if p != nil {
			if p.Min != nil && v < int64(*p.Min) {
				v = int64(*p.Min)
			}
			if p.Max != nil && v > int64(*p.Max) {
				v = int64(*p.Max)
			}
		}
		return v, nil
	case "bool":
		return true, nil
	case "time":
		return "2026-01-01T00:00:00Z", nil
	}
	if g.kindOf(ctx, base) == kEnum {
		return g.enumOf(ctx, base).Values[0], nil
	}
	m := map[string]any{}
	for _, f := range g.fieldsOf(ctx, base) {
		v, err := g.sample(ctx, f.Type, nil, depth+1)
		if err != nil {
			return nil, err
		}
		m[f.Name] = v
	}
	return m, nil
}

func deref(p *int) any {
	if p == nil {
		return "none"
	}
	return *p
}

// text renders a scalar sample as it appears in a path or query.
func text(v any) string { return fmt.Sprint(v) }

// baseline is a request that carries every param with a valid value.
func (g *gen) baseline(rt spec.Route) (rcase, error) {
	c := rcase{path: map[string]string{}}
	if len(g.bodyParams(rt)) > 0 {
		c.body = map[string]any{}
	}
	for i := range rt.Request {
		q := rt.Request[i]
		v, err := g.sample(rt.Context, q.Type, &q, 0)
		if err != nil {
			return c, fmt.Errorf("routes[%s].request[%s]: %w", rt.Name, q.Name, err)
		}
		switch q.In {
		case "path":
			c.path[q.Name] = text(v)
		case "query":
			if l, ok := v.([]any); ok {
				for _, e := range l {
					c.query = append(c.query, [2]string{q.Name, text(e)})
				}
			} else {
				c.query = append(c.query, [2]string{q.Name, text(v)})
			}
		case "body":
			c.body[q.Name] = v
		}
	}
	return c, nil
}

// minimal is the baseline without its optional params.
func (g *gen) minimal(rt spec.Route, b rcase) (rcase, bool) {
	c := b.clone()
	any := false
	for _, q := range rt.Request {
		if !strings.HasPrefix(q.Type, "?") {
			continue
		}
		any = true
		switch q.In {
		case "query":
			c = c.dropQuery(q.Name)
		case "body":
			delete(c.body, q.Name)
		}
	}
	return c, any
}

// ---- breaking a request ----

type bad struct {
	what string
	c    rcase
	want int
}

// variant is a value of some type with one fault in it.
type variant struct {
	what string
	val  any
}

// variants lists the ways the value v of type t can be wrong: the wrong
// JSON type, an unknown or missing field, a value outside an enum, in each
// place it can happen. inList is set for list elements, which cannot be
// told apart from absent when null.
func (g *gen) variants(ctx, t string, v any, at string, depth int, inList bool) []variant {
	optional, lists, base := split(t)
	var out []variant
	add := func(what string, val any) { out = append(out, variant{at + " " + what, val}) }
	if !optional && !inList {
		add("is null", nil)
	}
	if lists > 0 {
		add("is not a list", "x")
		arr, _ := v.([]any)
		if len(arr) > 0 && depth < 6 {
			for _, sub := range g.variants(ctx, elem(t), arr[0], at+"[0]", depth+1, true) {
				cp := cloneTree(arr).([]any)
				cp[0] = sub.val
				out = append(out, variant{sub.what, cp})
			}
		}
		return out
	}
	switch g.kindOf(ctx, base) {
	case kScalar:
		switch base {
		case "string":
			add("is a number", int64(7))
		case "int":
			add("is a string", "x")
			add("is not whole", 1.5)
		case "bool":
			add("is a string", "x")
		case "time":
			add("is not a time", "yesterday")
			add("is a number", int64(7))
		}
	case kEnum:
		add("is outside the enum", "not_a_value")
		add("is a number", int64(7))
	case kStruct:
		add("is not an object", "x")
		add("is a list", []any{})
		m, _ := v.(map[string]any)
		if m == nil {
			return out
		}
		cp := cloneTree(m).(map[string]any)
		cp["zz_unknown"] = int64(1)
		add("has an unknown field", cp)
		if depth >= 6 {
			return out
		}
		for _, f := range g.fieldsOf(ctx, base) {
			fo, _, _ := split(f.Type)
			if !fo {
				cp := cloneTree(m).(map[string]any)
				delete(cp, f.Name)
				out = append(out, variant{at + "." + f.Name + " is missing", cp})
			}
			for _, sub := range g.variants(ctx, f.Type, m[f.Name], at+"."+f.Name, depth+1, false) {
				cp := cloneTree(m).(map[string]any)
				cp[f.Name] = sub.val
				out = append(out, variant{sub.what, cp})
			}
		}
	}
	return out
}

// violations are the values that break a param's own min, max or pattern.
func (g *gen) violations(rt spec.Route, q spec.Param) []variant {
	_, lists, base := split(q.Type)
	var out []variant
	at := q.Name
	switch {
	case lists > 0:
		el, err := g.sample(rt.Context, elem(q.Type), nil, 0)
		if err != nil {
			return nil
		}
		mk := func(n int) []any {
			l := make([]any, n)
			for i := range l {
				l[i] = el
			}
			return l
		}
		if q.Min != nil && *q.Min > 0 {
			out = append(out, variant{at + " has too few items", mk(*q.Min - 1)})
		}
		if q.Max != nil {
			out = append(out, variant{at + " has too many items", mk(*q.Max + 1)})
		}
	case base == "string":
		if q.Min != nil && *q.Min > 0 {
			out = append(out, variant{at + " is too short", strings.Repeat("a", *q.Min-1)})
		}
		if q.Max != nil {
			out = append(out, variant{at + " is too long", strings.Repeat("a", *q.Max+1)})
		}
		if q.Pattern != "" {
			pick := ""
			found := false
			for _, s := range badPool {
				if q.In == "path" && s == "" {
					continue
				}
				if !matches(s, &q) && withinLength(s, &q) {
					pick, found = s, true
					break
				}
			}
			if !found {
				for _, s := range badPool {
					if !matches(s, &q) && !(q.In == "path" && s == "") {
						pick, found = s, true
						break
					}
				}
			}
			if found {
				out = append(out, variant{at + " does not match the pattern", pick})
			}
		}
	case base == "int":
		if q.Min != nil {
			out = append(out, variant{at + " is below the minimum", int64(*q.Min - 1)})
		}
		if q.Max != nil {
			out = append(out, variant{at + " is above the maximum", int64(*q.Max + 1)})
		}
	}
	return out
}

// breakages lists every invalid request for a route, built from its
// baseline.
func (g *gen) breakages(rt spec.Route, b rcase) []bad {
	var out []bad
	put := func(what string, c rcase) { out = append(out, bad{what, c, 400}) }
	for _, q := range rt.Request {
		optional, lists, base := split(q.Type)
		for _, v := range g.violations(rt, q) {
			if s, isStr := v.val.(string); q.In == "path" && isStr && (s == "" || s == "." || s == "..") {
				continue
			}
			c := b.clone()
			g.set(&c, q, v.val)
			put(v.what, c)
		}
		switch q.In {
		case "path":
			if base == "int" {
				c := b.clone()
				c.path[q.Name] = "abc"
				put(q.Name+" is not an integer", c)
			}
		case "query":
			if !optional {
				put(q.Name+" is missing", b.dropQuery(q.Name))
			}
			if base == "int" || base == "bool" {
				c := b.clone()
				g.set(&c, q, map[string]string{"int": "abc", "bool": "maybe"}[base])
				put(q.Name+" is the wrong type", c)
			}
			if lists == 0 {
				c := b.clone()
				c.query = append(c.query, [2]string{q.Name, text(firstQuery(b, q.Name))})
				put(q.Name+" is given twice", c)
			}
		case "body":
			for _, v := range g.variants(rt.Context, q.Type, b.body[q.Name], q.Name, 0, false) {
				c := b.clone()
				c.body[q.Name] = v.val
				put(v.what, c)
			}
			if !optional {
				c := b.clone()
				delete(c.body, q.Name)
				put(q.Name+" is missing", c)
			}
		}
	}
	// the request as a whole
	c := b.clone()
	c.query = append(c.query, [2]string{"zz_unknown", "1"})
	put("an unknown query parameter", c)
	if b.body != nil {
		c = b.clone()
		c.body["zz_unknown"] = int64(1)
		put("an unknown body field", c)
		raws := map[string]string{"the body is not JSON": "{", "the body is a list": "[]", "the body holds two values": "{}{}"}
		if !g.allOptionalBody(rt) {
			raws["the body is null"] = "null"
		}
		for what, raw := range raws {
			c = b.clone()
			c.raw = &raw
			put(what, c)
		}
		c = b.clone()
		c.ctype = "text/plain"
		out = append(out, bad{"the body is not declared as JSON", c, 415})
		c = b.clone()
		c.none = true
		out = append(out, bad{"the body has no content type", c, 415})
		if !g.allOptionalBody(rt) {
			c = b.clone()
			empty := ""
			c.raw = &empty
			put("the body is empty", c)
		}
	}
	sortBad(out)
	return out
}

func sortBad(out []bad) {
	// stable order: by name; the map iteration above is the only source of disorder
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].what > out[j].what; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
}

func (g *gen) allOptionalBody(rt spec.Route) bool {
	for _, q := range g.bodyParams(rt) {
		if !strings.HasPrefix(q.Type, "?") {
			return false
		}
	}
	return true
}

func firstQuery(c rcase, name string) string {
	for _, kv := range c.query {
		if kv[0] == name {
			return kv[1]
		}
	}
	return ""
}

// set replaces the value of param q in c.
func (g *gen) set(c *rcase, q spec.Param, val any) {
	switch q.In {
	case "path":
		c.path[q.Name] = text(val)
	case "query":
		*c = c.dropQuery(q.Name)
		if l, ok := val.([]any); ok {
			for _, e := range l {
				c.query = append(c.query, [2]string{q.Name, text(e)})
			}
		} else {
			c.query = append(c.query, [2]string{q.Name, text(val)})
		}
	case "body":
		c.body[q.Name] = val
	}
}
