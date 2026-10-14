package spec

import (
	"fmt"

	"github.com/briolink/briolink/core/check"
)

// validateStatechart applies the checks of docs/1-methodology.md
// § Statecharts: every state reachable, every non-final state can leave,
// every (state, event) pair a transition or forbidden, every guard and
// action a pure function of the right kind.
func (c *Context) validateStatechart(l *check.List, p, entity string, sc *Statechart, used map[string]bool) {
	states := map[string]State{}
	if len(sc.States) == 0 {
		l.Addf(p+".states", "must have at least one state")
	}
	for _, s := range sc.States {
		if !check.Snake(s.Name) {
			l.Addf(p+".states", "name must be snake_case, got %q", s.Name)
		}
		if _, dup := states[s.Name]; dup {
			l.Addf(p+".states", "%q is declared twice", s.Name)
		}
		states[s.Name] = s
	}
	if init, ok := states[sc.Initial]; !ok {
		l.Addf(p+".initial", "%q is not a state", sc.Initial)
	} else if init.Final {
		l.Addf(p+".initial", "%q is final, so nothing could happen", sc.Initial)
	}

	events := map[string]Event{}
	if len(sc.Events) == 0 {
		l.Addf(p+".events", "must have at least one event")
	}
	for _, e := range sc.Events {
		if !check.Snake(e.Name) {
			l.Addf(p+".events", "name must be snake_case, got %q", e.Name)
		}
		if _, dup := events[e.Name]; dup {
			l.Addf(p+".events", "%q is declared twice", e.Name)
		}
		events[e.Name] = e
		c.validateFields(l, p+".events["+e.Name+"].params", e.Params, false)
	}

	type pair struct{ state, event string }
	handled := map[pair]bool{}
	unguarded := map[pair]int{}
	next := map[string][]string{}
	for i, t := range sc.Transitions {
		tp := fmt.Sprintf("%s.transitions[%d]", p, i)
		from, okFrom := states[t.From]
		ev, okEv := events[t.Event]
		if !okFrom {
			l.Addf(tp, "from %q is not a state", t.From)
		} else if from.Final {
			l.Addf(tp, "from %q is final; a final state cannot leave", t.From)
		}
		if !okEv {
			l.Addf(tp, "event %q is not an event", t.Event)
		}
		if _, ok := states[t.To]; !ok {
			l.Addf(tp, "to %q is not a state", t.To)
		}
		pr := pair{t.From, t.Event}
		if j, ok := unguarded[pr]; ok {
			l.Addf(tp, "can never fire: transitions[%d] for (%q, %q) has no guard and is tried first", j, t.From, t.Event)
		}
		if t.Guard == "" {
			unguarded[pr] = i
		}
		handled[pr] = true
		next[t.From] = append(next[t.From], t.To)
		if t.Guard != "" {
			c.checkStep(l, tp+".guard", KindGuard, t.Guard, entity, ev, okEv, used)
		}
		if t.Action != "" {
			c.checkStep(l, tp+".action", KindAction, t.Action, entity, ev, okEv, used)
		}
	}

	forbidden := map[pair]bool{}
	seenState := map[string]bool{}
	for i, f := range sc.Forbidden {
		fp := fmt.Sprintf("%s.forbidden[%d]", p, i)
		s, ok := states[f.State]
		switch {
		case !ok:
			l.Addf(fp, "%q is not a state", f.State)
		case s.Final:
			l.Addf(fp, "%q is final and already forbids every event", f.State)
		case seenState[f.State]:
			l.Addf(fp, "%q has a second forbidden entry; list all its events in one", f.State)
		}
		seenState[f.State] = true
		for _, e := range f.Events {
			pr := pair{f.State, e}
			switch {
			case events[e].Name == "":
				l.Addf(fp, "%q is not an event", e)
			case forbidden[pr]:
				l.Addf(fp, "%q is listed twice", e)
			case handled[pr]:
				l.Addf(fp, "(%q, %q) is both a transition and forbidden", f.State, e)
			}
			forbidden[pr] = true
		}
	}

	for _, s := range sc.States {
		if s.Final {
			continue
		}
		for _, e := range sc.Events {
			pr := pair{s.Name, e.Name}
			if !handled[pr] && !forbidden[pr] {
				l.Addf(p, "(%q, %q) is neither a transition nor forbidden", s.Name, e.Name)
			}
		}
		leaves := false
		for _, to := range next[s.Name] {
			leaves = leaves || to != s.Name
		}
		if !leaves {
			l.Addf(p, "state %q is not final but no transition leaves it", s.Name)
		}
	}

	reached := map[string]bool{sc.Initial: true}
	queue := []string{sc.Initial}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, to := range next[s] {
			if !reached[to] {
				reached[to] = true
				queue = append(queue, to)
			}
		}
	}
	for _, s := range sc.States {
		if !reached[s.Name] {
			l.Addf(p, "state %q is unreachable from %q", s.Name, sc.Initial)
		}
	}
}

// checkStep checks that a transition's guard or action names a function of
// that kind for this entity, whose other params the event supplies.
func (c *Context) checkStep(l *check.List, p, kind, name, entity string, ev Event, evOK bool, used map[string]bool) {
	f := c.Function(name)
	if f == nil {
		l.Addf(p, "%q is not a function in the model", name)
		return
	}
	used[name] = true
	if f.Kind != kind {
		l.Addf(p, "%q is of kind %s, not %s", name, f.Kind, kind)
		return
	}
	if len(f.Params) == 0 {
		return
	}
	if f.Params[0].Type != entity {
		l.Addf(p, "%q takes %s, not %s", name, f.Params[0].Type, entity)
	}
	if !evOK {
		return
	}
	for _, fp := range f.Params[1:] {
		found := false
		for _, ep := range ev.Params {
			if ep.Name == fp.Name {
				found = true
				if ep.Type != fp.Type {
					l.Addf(p, "%q param %q is %s but event %q gives %s", name, fp.Name, fp.Type, ev.Name, ep.Type)
				}
			}
		}
		if !found {
			l.Addf(p, "%q needs param %q, which event %q does not carry", name, fp.Name, ev.Name)
		}
	}
}
