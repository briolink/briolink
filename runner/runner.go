// Package runner is the methodology's loop (docs/2-architecture.md § Runner):
// a workspace per task, the coder through its port, the answer applied to the
// allowed files only, the checks, and retries until the budget is spent or two
// attempts fail the same way; then the reasoner at the tiers the routing table
// gives, then the founder; a pull request on a pass and the attempt summary on
// the issue. It changes neither acceptance criteria nor its own routing, and
// never skips a check.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/codehost"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/port/reasoner"
	"github.com/briolink/briolink/runner/contract"
)

// Job is the routing row that chooses the coder's tier for the runner's attempts.
const Job = "pure_function"

// The routing rows that choose the reasoner's tier for the first and the
// second escalation answer.
const (
	JobEscalationFirst  = "escalation.first"
	JobEscalationSecond = "escalation.second"
)

// DefaultMaxFailure caps the failure output sent back to the coder, in bytes.
const DefaultMaxFailure = 8000

// Records is the control plane's run record: one entry per attempt.
type Records interface {
	AddRun(ctx context.Context, in store.NewRun) (control.RunRecord, error)
}

// Runner runs task contracts. All fields but the optional ones are required.
type Runner struct {
	Coder      coder.Coder
	Workspaces Workspaces
	Records    Records
	Routing    *routing.Table

	// Optional.
	Generate   []string         // the regenerate-and-diff command; DefaultGenerate when nil
	MaxFailure int              // DefaultMaxFailure when 0
	Now        func() time.Time // time.Now when nil; measures the minutes budget

	// Reasoner takes a task the coder could not pass (steps 6): without it
	// the founder is next. Its tiers are the routing rows JobEscalationFirst
	// and JobEscalationSecond.
	Reasoner reasoner.Reasoner
	// Host takes the outcome (step 5, 7): the pull request on a pass, the
	// attempt summary on the issue, and state:needs-founder on a hand-off. A
	// pass needs a Workspace that is a Pusher. Without a Host the Result is
	// all there is.
	Host codehost.Host
	// Base is the branch a pull request asks to merge into; "main" when empty.
	Base string
	// History gives the link to a task's full run history, for the hand-off.
	History func(issue int64) string
}

// Stop says why a run that did not pass ended. The next step is escalation.
type Stop string

const (
	StopAttempts Stop = "attempts"   // coder_retries spent
	StopMinutes  Stop = "minutes"    // the wall-time budget spent
	StopSame     Stop = "same-twice" // two attempts failed the same way
)

// Attempt is one try, as recorded: the coder's, then the reasoner's.
type Attempt struct {
	N         int
	Tier      string // the routing tier that answered
	Model     string
	Files     []string          // paths returned
	Answer    map[string]string // the files as returned
	Diagnosis string            // the reasoner's one-line diagnosis
	Check     string            // the failed check's kind; "" when the attempt passed
	Output    string            // that check's output, untrimmed
}

// Result is what a run ended in.
type Result struct {
	Passed   bool
	Stop     Stop // why the coder stopped; set when it did not pass
	Attempts []Attempt
	// SpentUSD is what the reasoner cost, never shown outside the control plane.
	SpentUSD float64
	// HandOff is why the founder has the task; set when !Passed and the
	// reasoner (if any) had its turn.
	HandOff     string
	PullRequest int // the number of the pull request a pass opened
}

// Run works one contract: issue is the task's issue number, the key of its
// run records. A returned error means the run could not be made or kept
// (a workspace that cannot be created, a record that cannot be written, the
// context cancelled, the code host refusing); a task nobody could pass is a
// Result with a HandOff.
func (r *Runner) Run(ctx context.Context, issue int64, c *contract.Contract) (*Result, error) {
	tier, model, err := r.tier()
	if err != nil {
		return nil, err
	}
	if r.Reasoner != nil {
		if err := r.escalationTiers(); err != nil {
			return nil, err
		}
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	budget := time.Duration(c.Budget.Minutes) * time.Minute
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	ws, err := r.Workspaces.Create(ctx, c.TaskID)
	if err != nil {
		return nil, fmt.Errorf("runner: workspace: %w", err)
	}
	defer ws.Close()

	base, err := snapshot(ws, c.AllowedFiles)
	if err != nil {
		return nil, err
	}
	prefix, err := r.prefix(ws, c, base)
	if err != nil {
		return nil, err
	}
	w := &work{
		r: r, issue: issue, c: c, ws: ws, base: base, prefix: prefix,
		checks: r.checks(c), maxFail: r.MaxFailure, res: &Result{},
	}
	if w.maxFail == 0 {
		w.maxFail = DefaultMaxFailure
	}

	// The last failed attempt is recorded only once it is known whether the
	// run goes on, so a hand-up is recorded as escalated.
	var pend *store.NewRun
	pendN := 0
	settle := func(up bool) error {
		if pend == nil {
			return nil
		}
		if up && r.Reasoner != nil && pend.Outcome == control.RunOutcomeFailed {
			pend.Outcome = control.RunOutcomeEscalated
		}
		_, err := r.Records.AddRun(context.WithoutCancel(ctx), *pend)
		pend = nil
		if err != nil {
			return fmt.Errorf("runner: record attempt %d: %w", pendN, err)
		}
		return nil
	}

	var last string // the previous failure, as sent to the coder
	var lastSig string
attempts:
	for n := 1; ; n++ {
		if n > 1+c.Budget.CoderRetries {
			w.res.Stop = StopAttempts
			break
		}
		if now().Sub(start) >= budget {
			w.res.Stop = StopMinutes
			break
		}
		if err := settle(false); err != nil {
			return nil, err
		}
		att := Attempt{N: n, Tier: tier, Model: model}
		parts := prefix
		if last != "" {
			parts = append(append([]string(nil), prefix...), last)
		}

		kind, output, err := r.attempt(runCtx, w, parts, &att)
		if err != nil && runCtx.Err() == nil {
			return nil, err
		}
		outcome := control.RunOutcomePassed
		switch {
		case ctx.Err() != nil:
			outcome = control.RunOutcomeCancelled
		case kind != "" || err != nil:
			outcome = control.RunOutcomeFailed
			if err != nil { // the minutes budget ran out inside the attempt
				kind, output = kindCoder, "time budget spent: "+err.Error()
			}
		}
		att.Check, att.Output = kind, output
		w.res.Attempts = append(w.res.Attempts, att)
		pend, pendN = &store.NewRun{Task: issue, Tier: tier, Job: Job, Model: att.Model, Outcome: outcome}, n

		switch outcome {
		case control.RunOutcomePassed:
			w.res.Passed = true
			if err := settle(false); err != nil {
				return nil, err
			}
			break attempts
		case control.RunOutcomeCancelled:
			if err := settle(false); err != nil {
				return nil, err
			}
			return w.res, ctx.Err()
		}
		if runCtx.Err() != nil {
			w.res.Stop = StopMinutes
			break
		}
		sig := signature(kind, output)
		if sig == lastSig {
			w.res.Stop = StopSame
			break
		}
		last, lastSig = fmt.Sprintf("# Last failure (check: %s)\n\n%s", kind, trim(output, w.maxFail)), sig
	}
	if err := settle(true); err != nil {
		return nil, err
	}

	if !w.res.Passed && r.Reasoner != nil {
		if err := w.escalate(ctx); err != nil {
			return w.res, err
		}
	}
	if err := w.conclude(ctx); err != nil {
		return w.res, err
	}
	return w.res, nil
}

// work is one run in progress.
type work struct {
	r       *Runner
	issue   int64
	c       *contract.Contract
	ws      Workspace
	base    map[string][]byte // the allowed files as they were
	prefix  []string          // what the coder is sent before any failure
	checks  []check
	maxFail int
	res     *Result
}

// attempt restores the allowed files, asks the coder, applies the answer and
// runs the checks. It returns the failed check's kind and output, or "" when
// everything passed; the error is for what stopped the attempt from finishing.
func (r *Runner) attempt(ctx context.Context, w *work, parts []string, att *Attempt) (kind, output string, err error) {
	if err := restore(w.ws, w.base); err != nil {
		return "", "", err
	}
	resp, err := r.Coder.Code(ctx, coder.Request{Parts: parts})
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return kindCoder, err.Error(), nil
	}
	if resp.Model != "" {
		att.Model = resp.Model
	}
	return w.apply(ctx, resp.Files, kindCoder, att)
}

// apply writes an answer to the allowed files and runs the checks on it: the
// same for the coder and the reasoner. An answer naming any path outside
// allowed_files is rejected whole; empty is a failure of kind none.
func (w *work) apply(ctx context.Context, files map[string]string, none string, att *Attempt) (kind, output string, err error) {
	att.Answer = files
	for p := range files {
		att.Files = append(att.Files, p)
	}
	sort.Strings(att.Files)
	if len(files) == 0 {
		return none, "no files returned", nil
	}
	var outside []string
	for _, p := range att.Files {
		if !contains(w.c.AllowedFiles, p) {
			outside = append(outside, p)
		}
	}
	if len(outside) > 0 {
		return kindScope, "rejected, not in allowed_files: " + strings.Join(outside, ", "), nil
	}
	for _, p := range att.Files {
		if err := w.ws.Write(p, []byte(withNewline(files[p]))); err != nil {
			return "", "", fmt.Errorf("runner: write %s: %w", p, err)
		}
	}
	return run(ctx, w.ws, w.checks)
}

// tier resolves the routing row: the tier it names and the model filling it.
func (r *Runner) tier() (tier, model string, err error) {
	if r.Routing == nil {
		return "", "", errors.New("runner: no routing table")
	}
	tier = r.Routing.Jobs[Job]
	if tier == "" || r.Routing.Tiers[tier] == "" {
		return "", "", fmt.Errorf("runner: routing has no model for job %q", Job)
	}
	return tier, r.Routing.Tiers[tier], nil
}

// snapshot reads the allowed files as they are before the first attempt; a
// file that does not exist yet is nil.
func snapshot(ws Workspace, files []string) (map[string][]byte, error) {
	base := map[string][]byte{}
	for _, f := range files {
		b, err := ws.Read(f)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			base[f] = nil
		case err != nil:
			return nil, fmt.Errorf("runner: read %s: %w", f, err)
		default:
			base[f] = b
		}
	}
	return base, nil
}

// restore puts the allowed files back as snapshot found them, so an attempt
// starts clean and never inherits the last one's files.
func restore(ws Workspace, base map[string][]byte) error {
	for f, b := range base {
		var err error
		if b == nil {
			err = ws.Remove(f)
		} else {
			err = ws.Write(f, b)
		}
		if err != nil {
			return fmt.Errorf("runner: restore %s: %w", f, err)
		}
	}
	return nil
}

// prefix is what the coder is sent, stable parts first so a provider can cache
// it (step 2): the contract, the files it may write as they stand (the
// signature and its documentation), the package's other files (the helpers and
// types the function uses), then the tests.
func (r *Runner) prefix(ws Workspace, c *contract.Contract, base map[string][]byte) ([]string, error) {
	js, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	parts := []string{"# Task contract\n\nReturn the complete new content of the allowed files, and no other file.\n\n" + string(js)}

	var files strings.Builder
	files.WriteString("# Files you may write, as they stand\n")
	for _, f := range c.AllowedFiles {
		files.WriteString(block(f, base[f]))
	}
	parts = append(parts, files.String())

	var siblings, tests strings.Builder
	siblings.WriteString("# Other files of the package, for reference\n")
	tests.WriteString("# Tests\n")
	seen := map[string]bool{}
	for _, f := range c.AllowedFiles {
		dir := path.Dir(f)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		names, err := ws.List(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("runner: list %s: %w", dir, err)
		}
		sort.Strings(names)
		for _, n := range names {
			p := path.Join(dir, n)
			if !strings.HasSuffix(n, ".go") || contains(c.AllowedFiles, p) {
				continue
			}
			b, err := ws.Read(p)
			if err != nil {
				return nil, fmt.Errorf("runner: read %s: %w", p, err)
			}
			if strings.HasSuffix(n, "_test.go") {
				tests.WriteString(block(p, b))
			} else {
				siblings.WriteString(block(p, b))
			}
		}
	}
	return append(parts, siblings.String(), tests.String()), nil
}

func block(name string, content []byte) string {
	if content == nil {
		return fmt.Sprintf("\n## %s\n\n(does not exist yet)\n", name)
	}
	return fmt.Sprintf("\n## %s\n\n```\n%s\n```\n", name, strings.TrimRight(string(content), "\n"))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// withNewline ends a file with a newline: a model's JSON string often drops
// it, and gofmt requires it. The only change the runner makes to an answer.
func withNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
