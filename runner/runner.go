// Package runner is the methodology's loop (docs/2-architecture.md § Runner,
// steps 1-5): a workspace per task, the coder through its port, the answer
// applied to the allowed files only, the checks, and retries until the budget
// is spent or two attempts fail the same way. It changes neither acceptance
// criteria nor its own routing, and never skips a check. Escalating to the
// reasoner and the founder (steps 6, 7) takes the Result it returns.
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
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/runner/contract"
)

// Job is the routing row that chooses the coder's tier for the runner's attempts.
const Job = "pure_function"

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
}

// Stop says why a run that did not pass ended. The next step is escalation.
type Stop string

const (
	StopAttempts Stop = "attempts"   // coder_retries spent
	StopMinutes  Stop = "minutes"    // the wall-time budget spent
	StopSame     Stop = "same-twice" // two attempts failed the same way
)

// Attempt is one try, as recorded.
type Attempt struct {
	N      int
	Model  string
	Files  []string // paths the coder returned
	Check  string   // the failed check's kind; "" when the attempt passed
	Output string   // that check's output, untrimmed
}

// Result is what a run ended in.
type Result struct {
	Passed   bool
	Stop     Stop // set when !Passed
	Attempts []Attempt
}

// Run works one contract: issue is the task's issue number, the key of its
// run records. A returned error means the run could not be made or kept
// (a workspace that cannot be created, a record that cannot be written, the
// context cancelled); a task the coder could not pass is a Result.
func (r *Runner) Run(ctx context.Context, issue int64, c *contract.Contract) (*Result, error) {
	tier, model, err := r.tier()
	if err != nil {
		return nil, err
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
	checks := r.checks(c)
	maxFail := r.MaxFailure
	if maxFail == 0 {
		maxFail = DefaultMaxFailure
	}

	res := &Result{}
	var last string // the previous failure, as sent to the coder
	var lastSig string
	for n := 1; n <= 1+c.Budget.CoderRetries; n++ {
		if now().Sub(start) >= budget {
			res.Stop = StopMinutes
			return res, nil
		}
		att := Attempt{N: n, Model: model}
		parts := prefix
		if last != "" {
			parts = append(append([]string(nil), prefix...), last)
		}

		kind, output, err := r.attempt(runCtx, ws, c, base, checks, parts, &att)
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
		res.Attempts = append(res.Attempts, att)
		if _, rerr := r.Records.AddRun(context.WithoutCancel(ctx), store.NewRun{Task: issue, Tier: tier, Job: Job, Model: att.Model, Outcome: outcome}); rerr != nil {
			return nil, fmt.Errorf("runner: record attempt %d: %w", n, rerr)
		}

		switch outcome {
		case control.RunOutcomePassed:
			res.Passed = true
			return res, nil
		case control.RunOutcomeCancelled:
			return res, ctx.Err()
		}
		if runCtx.Err() != nil {
			res.Stop = StopMinutes
			return res, nil
		}
		sig := signature(kind, output)
		if sig == lastSig {
			res.Stop = StopSame
			return res, nil
		}
		last, lastSig = fmt.Sprintf("# Last failure (check: %s)\n\n%s", kind, trim(output, maxFail)), sig
	}
	res.Stop = StopAttempts
	return res, nil
}

// attempt restores the allowed files, asks the coder, applies the answer and
// runs the checks. It returns the failed check's kind and output, or "" when
// everything passed; the error is for what stopped the attempt from finishing.
func (r *Runner) attempt(ctx context.Context, ws Workspace, c *contract.Contract, base map[string][]byte, checks []check, parts []string, att *Attempt) (kind, output string, err error) {
	if err := restore(ws, base); err != nil {
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
	for p := range resp.Files {
		att.Files = append(att.Files, p)
	}
	sort.Strings(att.Files)
	if len(resp.Files) == 0 {
		return kindCoder, "the coder returned no files", nil
	}
	var outside []string
	for _, p := range att.Files {
		if !contains(c.AllowedFiles, p) {
			outside = append(outside, p)
		}
	}
	if len(outside) > 0 {
		return kindScope, "rejected, not in allowed_files: " + strings.Join(outside, ", "), nil
	}
	for _, p := range att.Files {
		if err := ws.Write(p, []byte(resp.Files[p])); err != nil {
			return "", "", fmt.Errorf("runner: write %s: %w", p, err)
		}
	}
	return run(ctx, ws, checks)
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
// signature and its documentation), then the tests in their directories.
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

	var tests strings.Builder
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
			if !strings.HasSuffix(n, "_test.go") {
				continue
			}
			p := path.Join(dir, n)
			b, err := ws.Read(p)
			if err != nil {
				return nil, fmt.Errorf("runner: read %s: %w", p, err)
			}
			tests.WriteString(block(p, b))
		}
	}
	return append(parts, tests.String()), nil
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
