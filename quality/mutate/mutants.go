package mutate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Mutant is one small change to one function: an operator swapped, a
// boundary moved or a constant altered.
type Mutant struct {
	File        string // path as given by the directory plus the file name
	Line, Col   int
	Func        string
	Original    string
	Replacement string

	apply, revert func()
	src           *source
}

// String is the form a survivor is reported in.
func (m Mutant) String() string {
	return fmt.Sprintf("%s:%d:%d %s: %s -> %s", m.File, m.Line, m.Col, m.Func, m.Original, m.Replacement)
}

// source is one parsed file, mutated in place while a mutant is rendered.
type source struct {
	path string
	fset *token.FileSet
	file *ast.File
}

// render prints the file with m applied, then restores it.
func (m *Mutant) render() ([]byte, error) {
	m.apply()
	defer m.revert()
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, m.src.fset, m.src.file); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

// swaps maps an operator to the operators it is mutated into. A comparison
// moves across its boundary and is negated; arithmetic and logic are flipped.
var swaps = map[token.Token][]token.Token{
	token.ADD:  {token.SUB},
	token.SUB:  {token.ADD},
	token.MUL:  {token.QUO},
	token.QUO:  {token.MUL},
	token.REM:  {token.MUL},
	token.EQL:  {token.NEQ},
	token.NEQ:  {token.EQL},
	token.LSS:  {token.LEQ, token.GEQ},
	token.LEQ:  {token.LSS, token.GTR},
	token.GTR:  {token.GEQ, token.LEQ},
	token.GEQ:  {token.GTR, token.LSS},
	token.LAND: {token.LOR},
	token.LOR:  {token.LAND},

	token.ADD_ASSIGN: {token.SUB_ASSIGN},
	token.SUB_ASSIGN: {token.ADD_ASSIGN},
	token.MUL_ASSIGN: {token.QUO_ASSIGN},
	token.QUO_ASSIGN: {token.MUL_ASSIGN},
	token.INC:        {token.DEC},
	token.DEC:        {token.INC},
}

// Generate parses the non-test Go files of dir and returns every mutant of
// the function named fn (a function or method name; "" means every function),
// in file, then source order. A name that matches nothing is an error, so a
// typo cannot pass as "no survivors".
func Generate(dir, fn string) ([]Mutant, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []Mutant
	found := false
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		src := &source{path: path, fset: fset, file: file}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !matches(fd, fn) {
				continue
			}
			found = true
			out = append(out, funcMutants(src, fd)...)
		}
	}
	if !found {
		if fn == "" {
			return nil, fmt.Errorf("%s: no function with a body", dir)
		}
		return nil, fmt.Errorf("%s: no function %q", dir, fn)
	}
	return out, nil
}

func matches(fd *ast.FuncDecl, fn string) bool {
	return fn == "" || fd.Name.Name == fn
}

func funcMutants(src *source, fd *ast.FuncDecl) []Mutant {
	var out []Mutant
	add := func(pos token.Pos, orig, repl string, apply, revert func()) {
		p := src.fset.Position(pos)
		out = append(out, Mutant{
			File: src.path, Line: p.Line, Col: p.Column, Func: fd.Name.Name,
			Original: orig, Replacement: repl, apply: apply, revert: revert, src: src,
		})
	}

	var stack []ast.Node
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		var parent ast.Node = fd.Body
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, n)

		switch n := n.(type) {
		case *ast.BinaryExpr:
			for _, to := range swaps[n.Op] {
				from, to := n.Op, to
				add(n.OpPos, from.String(), to.String(),
					func() { n.Op = to }, func() { n.Op = from })
			}
		case *ast.AssignStmt:
			for _, to := range swaps[n.Tok] {
				from, to := n.Tok, to
				add(n.TokPos, from.String(), to.String(),
					func() { n.Tok = to }, func() { n.Tok = from })
			}
		case *ast.IncDecStmt:
			for _, to := range swaps[n.Tok] {
				from, to := n.Tok, to
				add(n.TokPos, from.String(), to.String(),
					func() { n.Tok = to }, func() { n.Tok = from })
			}
		case *ast.UnaryExpr:
			// !x and -x lose their operator.
			if n.Op == token.NOT || n.Op == token.SUB {
				n := n
				orig := n.Op.String() + "x"
				if set, restore, ok := replacer(parent, n, n.X); ok {
					add(n.OpPos, orig, "x", set, restore)
				}
			}
		case *ast.Ident:
			if n.Name == "true" || n.Name == "false" {
				n := n
				from, to := n.Name, map[string]string{"true": "false", "false": "true"}[n.Name]
				add(n.NamePos, from, to,
					func() { n.Name = to }, func() { n.Name = from })
			}
		case *ast.BasicLit:
			for _, to := range literalSwaps(n) {
				n := n
				from, to := n.Value, to
				add(n.ValuePos, from, to,
					func() { n.Value = to }, func() { n.Value = from })
			}
		}
		return true
	})
	return out
}

// literalSwaps gives the values a literal is mutated into: an integer moves
// one either way (a boundary), a non-raw string is emptied or filled.
func literalSwaps(l *ast.BasicLit) []string {
	switch l.Kind {
	case token.INT:
		v, err := strconv.ParseInt(l.Value, 0, 64)
		if err != nil {
			return nil
		}
		return []string{signed(v + 1), signed(v - 1)}
	case token.STRING:
		if strings.HasPrefix(l.Value, "`") {
			return nil
		}
		if l.Value == `""` {
			return []string{`"mutant"`}
		}
		return []string{`""`}
	}
	return nil
}

// signed prints a negative value in parentheses, so it stays valid after a
// binary minus.
func signed(v int64) string {
	if v < 0 {
		return "(" + strconv.FormatInt(v, 10) + ")"
	}
	return strconv.FormatInt(v, 10)
}

// replacer swaps child for repl inside parent, found by reflection over the
// parent's expression fields.
func replacer(parent, child ast.Node, repl ast.Expr) (set, restore func(), ok bool) {
	v := reflect.ValueOf(parent)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, nil, false
	}
	exprType := reflect.TypeOf((*ast.Expr)(nil)).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch {
		case f.Type() == exprType && !f.IsNil() && f.Interface() == child:
			return func() { f.Set(reflect.ValueOf(repl)) }, func() { f.Set(reflect.ValueOf(child)) }, true
		case f.Kind() == reflect.Slice && f.Type().Elem() == exprType:
			for j := 0; j < f.Len(); j++ {
				if f.Index(j).Interface() == child {
					e := f.Index(j)
					return func() { e.Set(reflect.ValueOf(repl)) }, func() { e.Set(reflect.ValueOf(child)) }, true
				}
			}
		}
	}
	return nil, nil, false
}
