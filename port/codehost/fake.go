package codehost

import (
	"context"
	"slices"
	"sync"
)

// Fake is an in-memory Host for tests of the runner and control plane. It has
// one branch, "main", to start from.
type Fake struct {
	mu         sync.Mutex
	next       int
	issues     map[int]*Issue
	prs        map[int]*PullRequest
	comments   map[int][]Comment
	commentSeq int
	milestones []Milestone
	branches   []string
}

// NewFake returns a Fake holding only the branch "main".
func NewFake() *Fake {
	return &Fake{
		issues:   map[int]*Issue{},
		prs:      map[int]*PullRequest{},
		comments: map[int][]Comment{},
		branches: []string{"main"},
	}
}

func (f *Fake) hasMilestone(title string) bool {
	return slices.ContainsFunc(f.milestones, func(m Milestone) bool { return m.Title == title })
}

func (f *Fake) CreateIssue(ctx context.Context, in NewIssue) (Issue, error) {
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.Title == "" || (in.Milestone != "" && !f.hasMilestone(in.Milestone)) {
		return Issue{}, ErrInvalid
	}
	f.next++
	is := &Issue{Number: f.next, Title: in.Title, Body: in.Body, State: Open, Milestone: in.Milestone}
	is.Labels = addLabels(nil, in.Labels)
	f.issues[is.Number] = is
	return cloneIssue(is), nil
}

func (f *Fake) GetIssue(ctx context.Context, number int) (Issue, error) {
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[number]
	if !ok {
		return Issue{}, ErrNotFound
	}
	return cloneIssue(is), nil
}

func (f *Fake) ListIssues(ctx context.Context, flt IssueFilter) ([]Issue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Issue
	for n := 1; n <= f.next; n++ {
		is, ok := f.issues[n]
		if !ok {
			continue
		}
		if flt.State != "" && is.State != flt.State {
			continue
		}
		if flt.Milestone != "" && is.Milestone != flt.Milestone {
			continue
		}
		if !hasAll(is.Labels, flt.Labels) {
			continue
		}
		out = append(out, cloneIssue(is))
	}
	return out, nil
}

func (f *Fake) UpdateIssue(ctx context.Context, number int, u IssueUpdate) (Issue, error) {
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	if (u.Title != nil && *u.Title == "") || (u.State != nil && *u.State != Open && *u.State != Closed) {
		return Issue{}, ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[number]
	if !ok {
		return Issue{}, ErrNotFound
	}
	if u.Title != nil {
		is.Title = *u.Title
	}
	if u.Body != nil {
		is.Body = *u.Body
	}
	if u.State != nil {
		is.State = *u.State
	}
	return cloneIssue(is), nil
}

func (f *Fake) AddLabels(ctx context.Context, number int, labels ...string) (Issue, error) {
	return f.editLabels(ctx, number, func(cur []string) []string { return addLabels(cur, labels) })
}

func (f *Fake) RemoveLabels(ctx context.Context, number int, labels ...string) (Issue, error) {
	return f.editLabels(ctx, number, func(cur []string) []string {
		return slices.DeleteFunc(cur, func(l string) bool { return slices.Contains(labels, l) })
	})
}

func (f *Fake) editLabels(ctx context.Context, number int, fn func([]string) []string) (Issue, error) {
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[number]
	if !ok {
		return Issue{}, ErrNotFound
	}
	is.Labels = fn(is.Labels)
	return cloneIssue(is), nil
}

func (f *Fake) Comment(ctx context.Context, number int, body string) (Comment, error) {
	if err := ctx.Err(); err != nil {
		return Comment{}, err
	}
	if body == "" {
		return Comment{}, ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issues[number] == nil && f.prs[number] == nil {
		return Comment{}, ErrNotFound
	}
	f.commentSeq++
	c := Comment{ID: f.commentSeq, Body: body}
	f.comments[number] = append(f.comments[number], c)
	return c, nil
}

func (f *Fake) ListComments(ctx context.Context, number int) ([]Comment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issues[number] == nil && f.prs[number] == nil {
		return nil, ErrNotFound
	}
	return slices.Clone(f.comments[number]), nil
}

func (f *Fake) CreateMilestone(ctx context.Context, m Milestone) (Milestone, error) {
	if err := ctx.Err(); err != nil {
		return Milestone{}, err
	}
	if m.Title == "" {
		return Milestone{}, ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasMilestone(m.Title) {
		return Milestone{}, ErrExists
	}
	f.milestones = append(f.milestones, m)
	return m, nil
}

func (f *Fake) ListMilestones(ctx context.Context) ([]Milestone, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.milestones), nil
}

func (f *Fake) CreateBranch(ctx context.Context, name, from string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if name == "" {
		return ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.branches, from) {
		return ErrNotFound
	}
	if slices.Contains(f.branches, name) {
		return ErrExists
	}
	f.branches = append(f.branches, name)
	return nil
}

func (f *Fake) ListBranches(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Sorted(slices.Values(f.branches)), nil
}

func (f *Fake) CreatePullRequest(ctx context.Context, in NewPullRequest) (PullRequest, error) {
	if err := ctx.Err(); err != nil {
		return PullRequest{}, err
	}
	if in.Title == "" || in.Head == "" || in.Base == "" || in.Head == in.Base {
		return PullRequest{}, ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.branches, in.Head) || !slices.Contains(f.branches, in.Base) {
		return PullRequest{}, ErrNotFound
	}
	f.next++
	pr := &PullRequest{Number: f.next, Title: in.Title, Body: in.Body, Head: in.Head, Base: in.Base, State: Open}
	f.prs[pr.Number] = pr
	return *pr, nil
}

func (f *Fake) GetPullRequest(ctx context.Context, number int) (PullRequest, error) {
	if err := ctx.Err(); err != nil {
		return PullRequest{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prs[number]
	if !ok {
		return PullRequest{}, ErrNotFound
	}
	return *pr, nil
}

func addLabels(cur, add []string) []string {
	out := slices.Clone(cur)
	for _, l := range add {
		if l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func cloneIssue(is *Issue) Issue {
	c := *is
	c.Labels = slices.Clone(is.Labels)
	return c
}
