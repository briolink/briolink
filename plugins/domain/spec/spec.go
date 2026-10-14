// Package spec is format v0 of a bounded context's model,
// spec/domain/<context>.json: glossary, enums, values, entities with their
// invariants and flat statecharts, and the pure functions behind them
// (docs/1-methodology.md § Guardrails, M3, M4).
package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/strictjson"
)

// Format is the only value Context.Format accepts.
const Format = "v0"

// Function kinds. A guard decides whether a transition may fire, an action
// computes the entity after it, an invariant must hold after every action,
// and a pure function is anything else a route can call.
const (
	KindGuard     = "guard"
	KindAction    = "action"
	KindInvariant = "invariant"
	KindPure      = "pure"
)

// Primitives are the built-in types; time and random values are always
// passed in, never read by a pure function.
var Primitives = map[string]bool{"string": true, "int": true, "bool": true, "time": true}

// Context is one bounded context.
type Context struct {
	Format    string     `json:"format"`
	Context   string     `json:"context"`
	Doc       string     `json:"doc"`
	Glossary  []Term     `json:"glossary"`
	Enums     []Enum     `json:"enums"`
	Values    []Value    `json:"values"`
	Entities  []Entity   `json:"entities"`
	Functions []Function `json:"functions"`
}

// Term is one word of the context's vocabulary. Every enum, value and
// entity name must be a term.
type Term struct {
	Term    string `json:"term"`
	Meaning string `json:"meaning"`
}

// Enum is a closed set of snake_case values.
type Enum struct {
	Name   string   `json:"name"`
	Doc    string   `json:"doc"`
	Values []string `json:"values"`
}

// Value is an immutable record without identity or lifecycle.
type Value struct {
	Name   string  `json:"name"`
	Doc    string  `json:"doc"`
	Fields []Field `json:"fields"`
}

// Field is a named, typed slot. Type grammar: an optional leading "?"
// (may be absent), then any number of "[]" (list of), then a primitive or a
// name declared in the same context.
type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc"`
}

// Entity has identity; Invariants name invariant functions; Statechart is
// null when the entity has no lifecycle.
type Entity struct {
	Name       string      `json:"name"`
	Doc        string      `json:"doc"`
	Fields     []Field     `json:"fields"`
	Invariants []string    `json:"invariants"`
	Statechart *Statechart `json:"statechart"`
}

// Statechart is flat in v0: no nested or parallel states.
type Statechart struct {
	Initial     string       `json:"initial"`
	States      []State      `json:"states"`
	Events      []Event      `json:"events"`
	Transitions []Transition `json:"transitions"`
	Forbidden   []Forbidden  `json:"forbidden"`
}

// State is final when nothing may leave it; a final state implicitly
// forbids every event.
type State struct {
	Name  string `json:"name"`
	Doc   string `json:"doc"`
	Final bool   `json:"final"`
}

// Event carries the values its guards and actions may take.
type Event struct {
	Name   string  `json:"name"`
	Doc    string  `json:"doc"`
	Params []Field `json:"params"`
}

// Transition fires on Event in From when Guard (if any) holds, applying
// Action (if any). Several transitions may share (From, Event): they are
// tried in file order, so every one but the last needs a guard.
type Transition struct {
	From   string `json:"from"`
	Event  string `json:"event"`
	To     string `json:"to"`
	Guard  string `json:"guard"`
	Action string `json:"action"`
}

// Forbidden lists the events a non-final state rejects outright.
type Forbidden struct {
	State  string   `json:"state"`
	Events []string `json:"events"`
}

// Function is a pure function's signature and the rules its tests pin down.
// Guards and invariants take the entity first and return one bool; actions
// take the entity first and return the entity; a guard's or action's other
// params must be params of every event it serves, same name and type.
type Function struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Doc     string   `json:"doc"`
	Params  []Field  `json:"params"`
	Returns []Field  `json:"returns"`
	Rules   []string `json:"rules"`
}

// Decode decodes data strictly and validates it.
func Decode(data []byte) (*Context, error) {
	var c Context
	if err := strictjson.Decode(data, &c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Load reads spec/domain/<context>.json; the file name must match the
// context's name.
func Load(path string) (*Context, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}
	if want := strings.TrimSuffix(filepath.Base(path), ".json"); c.Context != want {
		return nil, fmt.Errorf("%s: context %q must be in %s.json", path, c.Context, c.Context)
	}
	return c, nil
}

// Function returns the named function, or nil.
func (c *Context) Function(name string) *Function {
	for i := range c.Functions {
		if c.Functions[i].Name == name {
			return &c.Functions[i]
		}
	}
	return nil
}

// Entity returns the named entity, or nil.
func (c *Context) Entity(name string) *Entity {
	for i := range c.Entities {
		if c.Entities[i].Name == name {
			return &c.Entities[i]
		}
	}
	return nil
}

// TypeOK reports whether t is a valid type expression in this context.
func (c *Context) TypeOK(t string) bool {
	t = strings.TrimPrefix(t, "?")
	for strings.HasPrefix(t, "[]") {
		t = t[2:]
	}
	if Primitives[t] {
		return true
	}
	for _, e := range c.Enums {
		if e.Name == t {
			return true
		}
	}
	for _, v := range c.Values {
		if v.Name == t {
			return true
		}
	}
	return c.Entity(t) != nil
}

// Validate reports every rule the context breaks.
func (c *Context) Validate() error {
	var l check.List
	if c.Format != Format {
		l.Addf("format", "must be %q, got %q", Format, c.Format)
	}
	if !check.Snake(c.Context) {
		l.Addf("context", "must be snake_case, got %q", c.Context)
	}
	c.validateGlossary(&l)
	c.validateTypes(&l)
	c.validateFunctions(&l)
	used := map[string]bool{}
	for _, e := range c.Entities {
		c.validateEntity(&l, e, used)
	}
	for _, f := range c.Functions {
		if f.Kind != KindPure && !used[f.Name] {
			l.Addf("functions["+f.Name+"]", "%s is never used by an entity", f.Kind)
		}
	}
	return l.Err()
}

func (c *Context) validateGlossary(l *check.List) {
	if len(c.Glossary) == 0 {
		l.Addf("glossary", "must define at least one term")
	}
	seen := map[string]bool{}
	for i, t := range c.Glossary {
		p := fmt.Sprintf("glossary[%d]", i)
		if !check.Text(t.Term) {
			l.Addf(p, "term must be one non-empty line")
		}
		if key := strings.ToLower(t.Term); seen[key] {
			l.Addf(p, "term %q is defined twice", t.Term)
		} else {
			seen[key] = true
		}
		if !check.Text(t.Meaning) {
			l.Addf(p, "meaning of %q must be one non-empty line", t.Term)
		}
	}
}

func (c *Context) validateTypes(l *check.List) {
	terms := map[string]bool{}
	for _, t := range c.Glossary {
		terms[t.Term] = true
	}
	names := map[string]bool{}
	declare := func(p, name string) {
		switch {
		case !check.Pascal(name):
			l.Addf(p, "type name must be PascalCase, got %q", name)
		case names[name]:
			l.Addf(p, "type %q is declared twice", name)
		case !terms[name]:
			l.Addf(p, "type %q is not a glossary term", name)
		}
		names[name] = true
	}
	for _, e := range c.Enums {
		p := "enums[" + e.Name + "]"
		declare(p, e.Name)
		if len(e.Values) == 0 {
			l.Addf(p, "must have at least one value")
		}
		seen := map[string]bool{}
		for _, v := range e.Values {
			if !check.Snake(v) {
				l.Addf(p, "value must be snake_case, got %q", v)
			}
			if seen[v] {
				l.Addf(p, "value %q is listed twice", v)
			}
			seen[v] = true
		}
	}
	for _, v := range c.Values {
		p := "values[" + v.Name + "]"
		declare(p, v.Name)
		c.validateFields(l, p+".fields", v.Fields, true)
	}
	for _, e := range c.Entities {
		p := "entities[" + e.Name + "]"
		declare(p, e.Name)
		c.validateFields(l, p+".fields", e.Fields, true)
	}
}

func (c *Context) validateFields(l *check.List, p string, fields []Field, nonEmpty bool) {
	if nonEmpty && len(fields) == 0 {
		l.Addf(p, "must have at least one field")
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if !check.Snake(f.Name) {
			l.Addf(p, "name must be snake_case, got %q", f.Name)
		}
		if seen[f.Name] {
			l.Addf(p, "%q is declared twice", f.Name)
		}
		seen[f.Name] = true
		if !c.TypeOK(f.Type) {
			l.Addf(p+"["+f.Name+"]", "unknown type %q", f.Type)
		}
	}
}

func (c *Context) validateFunctions(l *check.List) {
	seen := map[string]bool{}
	for _, f := range c.Functions {
		p := "functions[" + f.Name + "]"
		if !check.Snake(f.Name) {
			l.Addf(p, "name must be snake_case")
		}
		if seen[f.Name] {
			l.Addf(p, "function is declared twice")
		}
		seen[f.Name] = true
		c.validateFields(l, p+".params", f.Params, false)
		c.validateFields(l, p+".returns", f.Returns, true)
		if len(f.Rules) == 0 {
			l.Addf(p+".rules", "must state at least one rule for the tests to pin down")
		}
		for i, r := range f.Rules {
			if !check.Text(r) {
				l.Addf(fmt.Sprintf("%s.rules[%d]", p, i), "must be one non-empty line")
			}
		}
		switch f.Kind {
		case KindGuard, KindInvariant, KindAction:
			if len(f.Params) == 0 || c.Entity(f.Params[0].Type) == nil {
				l.Addf(p+".params", "a %s takes its entity as the first param", f.Kind)
			}
			if f.Kind == KindInvariant && len(f.Params) > 1 {
				l.Addf(p+".params", "an invariant takes only its entity")
			}
			if f.Kind == KindAction {
				if len(f.Returns) != 1 || len(f.Params) == 0 || f.Returns[0].Type != f.Params[0].Type {
					l.Addf(p+".returns", "an action returns exactly its entity")
				}
			} else if len(f.Returns) != 1 || f.Returns[0].Type != "bool" {
				l.Addf(p+".returns", "a %s returns exactly one bool", f.Kind)
			}
		case KindPure:
		default:
			l.Addf(p+".kind", "must be guard, action, invariant or pure, got %q", f.Kind)
		}
	}
}

func (c *Context) validateEntity(l *check.List, e Entity, used map[string]bool) {
	p := "entities[" + e.Name + "]"
	seen := map[string]bool{}
	for _, name := range e.Invariants {
		if seen[name] {
			l.Addf(p+".invariants", "%q is listed twice", name)
		}
		seen[name] = true
		f := c.Function(name)
		if f == nil || f.Kind != KindInvariant {
			l.Addf(p+".invariants", "%q is not an invariant function", name)
			continue
		}
		if len(f.Params) > 0 && f.Params[0].Type != e.Name {
			l.Addf(p+".invariants", "%q takes %s, not %s", name, f.Params[0].Type, e.Name)
		}
		used[name] = true
	}
	if e.Statechart != nil {
		c.validateStatechart(l, p+".statechart", e.Name, e.Statechart, used)
	}
}
