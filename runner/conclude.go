package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/briolink/briolink/port/codehost"
)

// Labels the runner moves on a task's issue (docs/2-architecture.md § Work tracking).
const (
	labelNeedsFounder = "state:needs-founder"
	labelQueued       = "state:queued"
	labelRunning      = "state:running"
)

// conclude reports a finished run on the code host (steps 5 and 7): a pass is
// pushed and opened as a pull request that closes the issue; a task nobody
// could pass goes to the founder. Both post the attempt summary on the issue.
func (w *work) conclude(ctx context.Context) error {
	r := w.r
	if r.Host == nil {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	number := int(w.issue)

	if !w.res.Passed {
		if w.res.HandOff == "" {
			w.res.HandOff = fmt.Sprintf("the coder stopped (%s) and no reasoner is configured", w.res.Stop)
		}
		if _, err := r.Host.AddLabels(ctx, number, labelNeedsFounder); err != nil {
			return fmt.Errorf("runner: label issue %d: %w", number, err)
		}
		if _, err := r.Host.RemoveLabels(ctx, number, labelQueued, labelRunning); err != nil {
			return fmt.Errorf("runner: label issue %d: %w", number, err)
		}
		text := w.summary() + "\n\nHanded to the founder: " + w.res.HandOff + "."
		if r.History != nil {
			text += "\n\nFull history: " + r.History(w.issue)
		}
		if _, err := r.Host.Comment(ctx, number, text); err != nil {
			return fmt.Errorf("runner: comment on issue %d: %w", number, err)
		}
		return nil
	}

	pusher, ok := w.ws.(Pusher)
	if !ok {
		return errors.New("runner: the workspace cannot push, so no pull request can be opened")
	}
	passing := w.res.Attempts[len(w.res.Attempts)-1]
	branch := fmt.Sprintf("task/%d-%s", w.issue, w.c.TaskID)
	title := fmt.Sprintf("%s: %s", w.c.TaskID, w.c.Objective)
	if err := pusher.Push(ctx, branch, fmt.Sprintf("%s\n\nTask %s, issue #%d", title, w.c.TaskID, w.issue), passing.Files); err != nil {
		return fmt.Errorf("runner: push %s: %w", branch, err)
	}
	base := r.Base
	if base == "" {
		base = "main"
	}
	pr, err := r.Host.CreatePullRequest(ctx, codehost.NewPullRequest{
		Title: title,
		Body:  w.summary() + fmt.Sprintf("\n\nCloses #%d", w.issue),
		Head:  branch,
		Base:  base,
	})
	if err != nil {
		return fmt.Errorf("runner: open pull request: %w", err)
	}
	w.res.PullRequest = pr.Number
	if _, err := r.Host.Comment(ctx, number, w.summary()+fmt.Sprintf("\n\nPull request #%d is open.", pr.Number)); err != nil {
		return fmt.Errorf("runner: comment on issue %d: %w", number, err)
	}
	return nil
}

// summary is the attempt summary: who answered, and how each attempt ended.
// It carries no cost figures, as client repos are visible to the client.
func (w *work) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Attempt summary for `%s`:\n\n", w.c.TaskID)
	for _, a := range w.res.Attempts {
		result := "passed"
		if a.Check != "" {
			result = "failed check `" + a.Check + "`"
		}
		fmt.Fprintf(&b, "%d. %s (`%s`): %s\n", a.N, a.Tier, a.Model, result)
		if a.Diagnosis != "" {
			fmt.Fprintf(&b, "   Diagnosis: %s\n", a.Diagnosis)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
