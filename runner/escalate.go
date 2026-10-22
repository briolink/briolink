package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/reasoner"
)

// kindReasoner is the failure of a reasoner call that gave no usable answer.
const kindReasoner = "reasoner"

// answerSchema is what the reasoner must return: its diagnosis in one line and
// the complete new content of the files it changes. Empty files mean it cannot
// fix the task inside allowed_files.
const answerSchema = `{
  "type": "object",
  "properties": {
    "diagnosis": {"type": "string"},
    "files": {"type": "object", "additionalProperties": {"type": "string"}}
  },
  "required": ["diagnosis", "files"],
  "additionalProperties": false
}`

const escalationInstructions = `# Escalation

The coder could not pass this task. Diagnose first: a wrong or missing test, an
ambiguous model, a task too large, or code the coder cannot write. Then make the
cheapest fix that lasts. Never weaken a check or change acceptance criteria.

Answer with JSON: "diagnosis", one line saying why the coder failed, and
"files", the complete new content of the files you change. Change only the files
in allowed_files; if the task cannot be fixed inside them, return no files and
say so in the diagnosis.`

// errSpent: the reasoner's budget for the task is gone; the founder is next.
var errSpent = errors.New("runner: reasoner_usd spent")

// escalationTiers checks the routing table has a model for both escalation rows.
func (r *Runner) escalationTiers() error {
	for _, job := range []string{JobEscalationFirst, JobEscalationSecond} {
		tier := r.Routing.Jobs[job]
		if tier == "" || r.Routing.Tiers[tier] == "" {
			return fmt.Errorf("runner: routing has no model for job %q", job)
		}
	}
	return nil
}

// escalate asks the reasoner for a first answer, then once for a second, at the
// tiers the routing table gives and within the task's reasoner_usd (step 6).
// The same checks apply to every answer. It returns with the Result passed or
// its HandOff set; an error is for what stopped the run itself.
func (w *work) escalate(ctx context.Context) error {
	r := w.r
	for _, job := range []string{JobEscalationFirst, JobEscalationSecond} {
		tier := r.Routing.Jobs[job]
		att := Attempt{N: len(w.res.Attempts) + 1, Tier: tier, Model: r.Routing.Tiers[tier]}
		kind, output, err := w.ask(ctx, reasoner.Tier(tier), &att)
		switch {
		case errors.Is(err, errSpent):
			w.res.HandOff = "the reasoner's budget for this task is spent"
			return nil
		case err != nil:
			return err
		}
		att.Check, att.Output = kind, output
		w.res.Attempts = append(w.res.Attempts, att)
		outcome := control.RunOutcomePassed
		if kind != "" {
			outcome = control.RunOutcomeEscalated // handed on: to the second answer, then to the founder
		}
		if _, err := r.Records.AddRun(context.WithoutCancel(ctx), store.NewRun{Task: w.issue, Tier: tier, Job: job, Model: att.Model, Outcome: outcome}); err != nil {
			return fmt.Errorf("runner: record attempt %d: %w", att.N, err)
		}
		if kind == "" {
			w.res.Passed = true
			return nil
		}
	}
	w.res.HandOff = "the reasoner's answers did not pass the checks"
	return nil
}

// ask is one reasoner attempt: the prompt, the call, the answer applied and
// checked. A call that fails is a failed attempt of kind reasoner, as a coder
// that errors is one; errSpent says no call could be made within the budget.
func (w *work) ask(ctx context.Context, tier reasoner.Tier, att *Attempt) (kind, output string, err error) {
	remaining := w.c.Budget.ReasonerUSD - w.res.SpentUSD
	if remaining <= 0 { // MaxUSD zero would mean no cap
		return "", "", errSpent
	}
	if err := restore(w.ws, w.base); err != nil {
		return "", "", err
	}
	prompt, err := w.prompt()
	if err != nil {
		return "", "", err
	}
	resp, err := w.r.Reasoner.Reason(ctx, reasoner.Request{Tier: tier, Prompt: prompt, Schema: json.RawMessage(answerSchema), MaxUSD: remaining})
	switch {
	case errors.Is(err, reasoner.ErrBudget):
		return "", "", errSpent
	case err != nil && ctx.Err() != nil:
		return "", "", ctx.Err()
	case err != nil:
		return kindReasoner, err.Error(), nil
	}
	w.res.SpentUSD += resp.CostUSD
	if resp.Model != "" {
		att.Model = resp.Model
	}
	var ans struct {
		Diagnosis string            `json:"diagnosis"`
		Files     map[string]string `json:"files"`
	}
	if err := json.Unmarshal(resp.Output, &ans); err != nil {
		return kindReasoner, "the answer is not {diagnosis, files}: " + err.Error(), nil
	}
	att.Diagnosis = oneLine(ans.Diagnosis)
	return w.apply(ctx, ans.Files, kindReasoner, att)
}

// prompt is the reasoner's input: the contract, the model slice, the tests, the
// last attempt and its trimmed failure, and the earlier attempts' failing test
// names. The files and their helpers are there so an answer compiles.
func (w *work) prompt() (string, error) {
	parts := append([]string{escalationInstructions}, w.prefix...)

	var slice strings.Builder
	slice.WriteString("# Model slice\n")
	for _, ref := range w.c.SpecRefs {
		file, _, _ := strings.Cut(ref, "#")
		b, err := w.ws.Read(file)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			slice.WriteString(block(file, nil))
		case err != nil:
			return "", fmt.Errorf("runner: read %s: %w", file, err)
		default:
			slice.WriteString(block(file, b))
		}
	}
	parts = append(parts, slice.String())

	n := len(w.res.Attempts)
	lastAtt := w.res.Attempts[n-1]
	var last strings.Builder
	fmt.Fprintf(&last, "# Last attempt (%s, check: %s)\n", lastAtt.Tier, lastAtt.Check)
	for _, p := range lastAtt.Files {
		last.WriteString(block(p, []byte(lastAtt.Answer[p])))
	}
	fmt.Fprintf(&last, "\n## Failure\n\n%s\n", trim(lastAtt.Output, w.maxFail))
	parts = append(parts, last.String())

	if n > 1 {
		var earlier strings.Builder
		earlier.WriteString("# Earlier attempts\n\n")
		for _, a := range w.res.Attempts[:n-1] {
			if names := failingTests(a.Output); len(names) > 0 {
				fmt.Fprintf(&earlier, "- attempt %d (%s): failing tests %s\n", a.N, a.Tier, strings.Join(names, ", "))
			} else {
				fmt.Fprintf(&earlier, "- attempt %d (%s): failed check %s\n", a.N, a.Tier, a.Check)
			}
		}
		parts = append(parts, earlier.String())
	}
	return strings.Join(parts, "\n\n"), nil
}

var failLine = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

// failingTests returns the sorted names of the tests a go test output reports as failed.
func failingTests(output string) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range failLine.FindAllStringSubmatch(output, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

// oneLine collapses whitespace and caps a model's text for a comment.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 400 {
		s = strings.Join(strings.Fields(trim(s, 400)), " ")
	}
	return s
}
