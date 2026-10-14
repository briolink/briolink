package spec

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func pilot(t *testing.T) *Context {
	t.Helper()
	c, err := Load("testdata/acme.json")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPilotIsValid(t *testing.T) {
	c := pilot(t)
	if c.Entity("Invoice").Statechart.Initial != "draft" {
		t.Fatal("pilot did not decode")
	}
}

func TestLoadNameMustMatchFile(t *testing.T) {
	data, _ := os.ReadFile("testdata/acme.json")
	path := t.TempDir() + "/billing.json"
	os.WriteFile(path, data, 0o600)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `context "acme" must be in acme.json`) {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeIsStrict(t *testing.T) {
	data, _ := os.ReadFile("testdata/acme.json")
	bad := strings.Replace(string(data), `"format": "v0",`, `"format": "v0", "colour": "red",`, 1)
	if _, err := Decode([]byte(bad)); err == nil || !strings.Contains(err.Error(), `unknown field "colour"`) {
		t.Fatalf("got %v", err)
	}
}

// Each case breaks one rule of the pilot and names the error it must give.
func TestRulesRejectBrokenPilot(t *testing.T) {
	sc := func(c *Context) *Statechart { return c.Entity("Invoice").Statechart }
	fn := func(c *Context, name string) *Function { return c.Function(name) }
	cases := []struct {
		name   string
		break_ func(c *Context)
		want   string
	}{
		// context
		{"format", func(c *Context) { c.Format = "v1" }, `format: must be "v0"`},
		{"context name", func(c *Context) { c.Context = "Acme" }, "context: must be snake_case"},
		{"glossary empty", func(c *Context) { c.Glossary = nil }, "glossary: must define at least one term"},
		{"glossary duplicate", func(c *Context) { c.Glossary = append(c.Glossary, Term{"invoice", "again"}) }, `term "invoice" is defined twice`},
		{"glossary meaning", func(c *Context) { c.Glossary[0].Meaning = "" }, `meaning of "Invoice" must be one non-empty line`},
		// types
		{"type not in glossary", func(c *Context) { c.Glossary = c.Glossary[1:] }, `type "Invoice" is not a glossary term`},
		{"type name case", func(c *Context) { c.Enums[0].Name = "currency" }, "type name must be PascalCase"},
		{"type declared twice", func(c *Context) { c.Values[0].Name = "Currency" }, `type "Currency" is declared twice`},
		{"enum empty", func(c *Context) { c.Enums[0].Values = nil }, "enums[Currency]: must have at least one value"},
		{"enum value case", func(c *Context) { c.Enums[0].Values[0] = "EUR" }, `value must be snake_case, got "EUR"`},
		{"enum value twice", func(c *Context) { c.Enums[0].Values[1] = "eur" }, `value "eur" is listed twice`},
		{"value no fields", func(c *Context) { c.Values[0].Fields = nil }, "values[Line].fields: must have at least one field"},
		{"field name", func(c *Context) { c.Entities[0].Fields[0].Name = "ID" }, `name must be snake_case, got "ID"`},
		{"field twice", func(c *Context) { c.Entities[0].Fields[1].Name = "id" }, `"id" is declared twice`},
		{"unknown type", func(c *Context) { c.Entities[0].Fields[3].Type = "[]Item" }, `entities[Invoice].fields[lines]: unknown type "[]Item"`},
		{"optional inside list", func(c *Context) { c.Entities[0].Fields[3].Type = "[]?Line" }, `unknown type "[]?Line"`},
		// functions
		{"function twice", func(c *Context) { c.Functions = append(c.Functions, *fn(c, "total")) }, "functions[total]: function is declared twice"},
		{"function name", func(c *Context) { fn(c, "total").Name = "Total" }, "functions[Total]: name must be snake_case"},
		{"function kind", func(c *Context) { fn(c, "total").Kind = "helper" }, `must be guard, action, invariant or pure, got "helper"`},
		{"function no rules", func(c *Context) { fn(c, "total").Rules = nil }, "functions[total].rules: must state at least one rule"},
		{"function blank rule", func(c *Context) { fn(c, "total").Rules[0] = "" }, "functions[total].rules[0]: must be one non-empty line"},
		{"function no returns", func(c *Context) { fn(c, "total").Returns = nil }, "functions[total].returns: must have at least one field"},
		{"guard returns bool", func(c *Context) { fn(c, "unpaid").Returns[0].Type = "int" }, "functions[unpaid].returns: a guard returns exactly one bool"},
		{"guard takes entity", func(c *Context) { fn(c, "unpaid").Params[0].Type = "Line" }, "functions[unpaid].params: a guard takes its entity as the first param"},
		{"action returns entity", func(c *Context) { fn(c, "void").Returns[0].Type = "bool" }, "functions[void].returns: an action returns exactly its entity"},
		{"invariant single param", func(c *Context) {
			f := fn(c, "lines_valid")
			f.Params = append(f.Params, Field{Name: "x", Type: "int"})
		}, "an invariant takes only its entity"},
		{"unused guard", func(c *Context) {
			c.Functions = append(c.Functions, Function{Name: "spare", Kind: KindGuard, Params: fn(c, "unpaid").Params, Returns: fn(c, "unpaid").Returns, Rules: []string{"x"}})
		}, "functions[spare]: guard is never used by an entity"},
		// invariants
		{"invariant not a function", func(c *Context) { c.Entities[0].Invariants[0] = "nope" }, `entities[Invoice].invariants: "nope" is not an invariant function`},
		{"invariant wrong kind", func(c *Context) { c.Entities[0].Invariants[0] = "total" }, `"total" is not an invariant function`},
		{"invariant twice", func(c *Context) { c.Entities[0].Invariants[1] = "paid_within_total" }, `"paid_within_total" is listed twice`},
		// statechart: states and events
		{"no states", func(c *Context) { sc(c).States = nil }, "statechart.states: must have at least one state"},
		{"state twice", func(c *Context) { sc(c).States[1].Name = "draft" }, `statechart.states: "draft" is declared twice`},
		{"initial missing", func(c *Context) { sc(c).Initial = "new" }, `statechart.initial: "new" is not a state`},
		{"initial final", func(c *Context) { sc(c).States[0].Final = true }, `statechart.initial: "draft" is final`},
		{"no events", func(c *Context) { sc(c).Events = nil }, "statechart.events: must have at least one event"},
		{"event twice", func(c *Context) { sc(c).Events[1].Name = "add_line" }, `statechart.events: "add_line" is declared twice`},
		{"event param type", func(c *Context) { sc(c).Events[3].Params[0].Type = "Reason" }, `events[void].params[reason]: unknown type "Reason"`},
		// statechart: transitions
		{"transition from unknown", func(c *Context) { sc(c).Transitions[0].From = "open" }, `transitions[0]: from "open" is not a state`},
		{"transition to unknown", func(c *Context) { sc(c).Transitions[0].To = "open" }, `transitions[0]: to "open" is not a state`},
		{"transition unknown event", func(c *Context) { sc(c).Transitions[0].Event = "edit" }, `transitions[0]: event "edit" is not an event`},
		{"transition from final", func(c *Context) {
			sc(c).Transitions = append(sc(c).Transitions, Transition{From: "paid", Event: "void", To: "voided", Action: "void"})
		}, `transitions[6]: from "paid" is final; a final state cannot leave`},
		{"shadowed transition", func(c *Context) { sc(c).Transitions[3].Guard = "" }, `transitions[4]: can never fire: transitions[3] for ("issued", "record_payment") has no guard`},
		// every guard and action names a pure function in the model
		{"guard not a function", func(c *Context) { sc(c).Transitions[1].Guard = "ready" }, `transitions[1].guard: "ready" is not a function in the model`},
		{"action not a function", func(c *Context) { sc(c).Transitions[1].Action = "send" }, `transitions[1].action: "send" is not a function in the model`},
		{"guard wrong kind", func(c *Context) { sc(c).Transitions[1].Guard = "issue" }, `transitions[1].guard: "issue" is of kind action, not guard`},
		{"action wrong kind", func(c *Context) { sc(c).Transitions[1].Action = "has_lines" }, `transitions[1].action: "has_lines" is of kind guard, not action`},
		{"step param missing from event", func(c *Context) { sc(c).Transitions[2].Action = "apply_payment" }, `"apply_payment" needs param "amount", which event "void" does not carry`},
		{"step param type differs", func(c *Context) { sc(c).Events[2].Params[0].Type = "string" }, `"settles_balance" param "amount" is int but event "record_payment" gives string`},
		// statechart: coverage, leaving, reachability
		{"pair not covered", func(c *Context) { sc(c).Forbidden[0].Events = nil }, `("draft", "record_payment") is neither a transition nor forbidden`},
		{"pair both", func(c *Context) { sc(c).Forbidden[0].Events = append(sc(c).Forbidden[0].Events, "issue") }, `("draft", "issue") is both a transition and forbidden`},
		{"forbidden unknown state", func(c *Context) { sc(c).Forbidden[0].State = "open" }, `forbidden[0]: "open" is not a state`},
		{"forbidden unknown event", func(c *Context) { sc(c).Forbidden[0].Events[0] = "pay" }, `forbidden[0]: "pay" is not an event`},
		{"forbidden on final", func(c *Context) {
			sc(c).Forbidden = append(sc(c).Forbidden, Forbidden{State: "paid", Events: []string{"void"}})
		}, `forbidden[2]: "paid" is final and already forbids every event`},
		{"forbidden state twice", func(c *Context) {
			sc(c).Forbidden = append(sc(c).Forbidden, Forbidden{State: "draft"})
		}, `forbidden[2]: "draft" has a second forbidden entry`},
		{"forbidden event twice", func(c *Context) {
			sc(c).Forbidden[1].Events = append(sc(c).Forbidden[1].Events, "issue")
		}, `forbidden[1]: "issue" is listed twice`},
		{"state cannot leave", func(c *Context) {
			s := sc(c)
			s.Transitions = s.Transitions[:3]
			s.Forbidden[1].Events = []string{"add_line", "issue", "record_payment", "void"}
		}, `state "issued" is not final but no transition leaves it`},
		{"self-loop does not leave", func(c *Context) {
			s := sc(c)
			s.Transitions = append(s.Transitions[:1], s.Transitions[3:]...)
			s.Forbidden[0].Events = []string{"record_payment", "issue", "void"}
		}, `state "draft" is not final but no transition leaves it`},
		{"state unreachable", func(c *Context) {
			s := sc(c)
			s.States = append(s.States, State{Name: "archived", Final: true})
		}, `state "archived" is unreachable from "draft"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := clone(t, pilot(t))
			tc.break_(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v\nwant error containing %q", err, tc.want)
			}
		})
	}
}

func clone(t *testing.T, c *Context) *Context {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var out Context
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}
