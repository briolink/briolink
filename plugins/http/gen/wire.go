package gen

import (
	"fmt"
	"strings"

	domain "github.com/briolink/briolink/plugins/domain/spec"
)

// fld is one field of something that is decoded from or encoded to JSON:
// a domain value or entity, a route's body or a route's response. Check, if
// set, returns statements that test the decoded value v at path at.
type fld struct {
	name, typ string
	check     func(v, at string) []string
}

func fldsOf(fields []domain.Field) []fld {
	out := make([]fld, len(fields))
	for i, f := range fields {
		out[i] = fld{name: f.Name, typ: f.Type}
	}
	return out
}

// decFn is a Go expression of type func(W, at string, *problems) D that
// decodes a non-optional type expression.
func (g *gen) decFn(ctx, t string) string {
	_, lists, base := split(t)
	if lists > 0 {
		return fmt.Sprintf("func(in %s, at string, p *problems) %s { return decodeList(in, at, p, %s) }",
			g.wireGo(ctx, t), g.goType(ctx, t), g.decFn(ctx, elem(t)))
	}
	switch base {
	case "string", "bool", "time":
		return "keep[" + g.goType(ctx, base) + "]"
	case "int":
		return "keep[int64]"
	}
	return "decode" + pascal(ctx) + base
}

// encFn is a Go expression of type func(D) W that encodes a non-optional
// type expression.
func (g *gen) encFn(ctx, t string) string {
	_, lists, base := split(t)
	if lists > 0 {
		return fmt.Sprintf("func(in %s) %s { return encodeList(in, %s) }",
			g.goType(ctx, t), g.wireGo(ctx, t), g.encFn(ctx, elem(t)))
	}
	switch base {
	case "string", "bool":
		return "same[" + base + "]"
	case "int":
		return "same[int64]"
	case "time":
		return "utc"
	}
	if g.kindOf(ctx, base) == kEnum {
		return "same[" + g.goType(ctx, base) + "]"
	}
	return "encode" + pascal(ctx) + base
}

// wireStruct declares a JSON shape: one field per fld, a pointer unless it
// is a list, so absence shows.
func (g *gen) wireStruct(x *w, ctx, name string, fields []fld) {
	x.f("type %s struct {", name)
	for _, f := range fields {
		wt := g.wireGo(ctx, f.typ)
		if !isList(f.typ) {
			wt = "*" + wt
		}
		x.f("\t%s %s `json:%q`", pascal(f.name), wt, f.name)
	}
	x.f("}")
}

// decodeStmts writes statements that fill out from wv: absent required
// fields are problems, present ones are decoded and checked. at is a Go
// expression for the path of the object.
func (g *gen) decodeStmts(x *w, ctx string, fields []fld, wv, out, at string) {
	for _, f := range fields {
		optional, _, _ := split(f.typ)
		list := isList(f.typ)
		field := pascal(f.name)
		path := fmt.Sprintf("join(%s, %q)", at, f.name)
		src := wv + "." + field
		arg := src
		if !list {
			arg = "*" + src
		}
		nonOpt := strings.TrimPrefix(f.typ, "?")
		body := func() {
			x.f("v := %s(%s, %s, p)", g.decFn(ctx, nonOpt), arg, path)
			if f.check != nil {
				for _, s := range f.check("v", path) {
					x.f("%s", s)
				}
			}
			if optional {
				x.f("%s.%s = &v", out, field)
			} else {
				x.f("%s.%s = v", out, field)
			}
		}
		if optional {
			x.f("if %s != nil {", src)
			body()
			x.f("}")
			continue
		}
		x.f("if %s == nil {", src)
		x.f("p.add(%s, \"is required\")", path)
		x.f("} else {")
		body()
		x.f("}")
	}
}

// encodeStmts writes statements that fill out (a wire struct) from v (the
// domain value).
func (g *gen) encodeStmts(x *w, ctx string, fields []fld, v, out string) {
	for _, f := range fields {
		optional, _, _ := split(f.typ)
		list := isList(f.typ)
		field := pascal(f.name)
		src := v + "." + field
		nonOpt := strings.TrimPrefix(f.typ, "?")
		enc := g.encFn(ctx, nonOpt)
		arg := src
		if optional {
			arg = "*" + src
		}
		assign := fmt.Sprintf("%s.%s = ptr(%s(%s))", out, field, enc, arg)
		if list {
			assign = fmt.Sprintf("%s.%s = %s(%s)", out, field, enc, arg)
		}
		if optional {
			x.f("if %s != nil {", src)
			x.f("%s", assign)
			x.f("}")
		} else {
			x.f("%s", assign)
		}
	}
}

// wireFile declares the JSON shape of every domain type the routes carry,
// with the functions that decode it (checking it) and encode it.
func (g *gen) wireFile() string {
	x := &w{}
	if len(g.wires) == 0 {
		x.f("// No route carries a domain type, so nothing is declared here.")
		return x.String()
	}
	for _, t := range g.wires {
		pkg := pkgOf(t.ctx)
		name := wireName(t.ctx, t.name)
		dn := "decode" + pascal(t.ctx) + t.name
		if t.enum {
			e := g.enumOf(t.ctx, t.name)
			cases := make([]string, len(e.Values))
			for i, v := range e.Values {
				cases[i] = fmt.Sprintf("%q", v)
			}
			x.f("// %s checks v is one of %s's values.", dn, t.name)
			x.f("func %s(v %s.%s, at string, p *problems) %s.%s {", dn, pkg, t.name, pkg, t.name)
			x.f("switch string(v) {")
			x.f("case %s:", strings.Join(cases, ", "))
			x.f("return v")
			x.f("}")
			x.f("p.add(at, %q)", "must be one of "+strings.Join(e.Values, ", "))
			x.f("return v")
			x.f("}")
			x.f("")
			continue
		}
		fields := fldsOf(g.fieldsOf(t.ctx, t.name))
		x.f("// %s is the JSON shape of %s.%s.", name, pkg, t.name)
		g.wireStruct(x, t.ctx, name, fields)
		x.f("")
		x.f("func %s(w %s, at string, p *problems) %s.%s {", dn, name, pkg, t.name)
		x.f("var out %s.%s", pkg, t.name)
		g.decodeStmts(x, t.ctx, fields, "w", "out", "at")
		x.f("return out")
		x.f("}")
		x.f("")
		x.f("func encode%s%s(v %s.%s) %s {", pascal(t.ctx), t.name, pkg, t.name, name)
		x.f("var out %s", name)
		g.encodeStmts(x, t.ctx, fields, "v", "out")
		x.f("return out")
		x.f("}")
		x.f("")
	}
	return x.String()
}
