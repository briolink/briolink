// Package codehost is the CodeHost port: issues, labels, milestones, branches,
// pull requests and comments (docs/2-architecture.md § Work tracking). Issues
// and pull requests share one number space and one comment thread model, as on
// the usual hosts. Merging is deliberately absent: only the founder merges
// into protected branches (A22). An adapter must pass Contract before it can
// be bound.
package codehost

import (
	"context"
	"errors"
)

var (
	// ErrNotFound: no such issue, pull request, milestone or branch.
	ErrNotFound = errors.New("codehost: not found")
	// ErrExists: a milestone or branch of that name exists already.
	ErrExists = errors.New("codehost: already exists")
	// ErrInvalid: an empty title, name or body, or an unknown milestone.
	ErrInvalid = errors.New("codehost: invalid argument")
)

// State of an issue or a pull request.
type State string

const (
	Open   State = "open"
	Closed State = "closed"
)

// Issue is a tracked task. Labels are kept sorted.
type Issue struct {
	Number    int
	Title     string
	Body      string
	State     State
	Labels    []string
	Milestone string // title, empty for none
}

// NewIssue creates an issue. Labels that do not exist yet are created by the host.
type NewIssue struct {
	Title     string
	Body      string
	Labels    []string
	Milestone string // must name an existing milestone, or be empty
}

// IssueUpdate changes the fields that are non-nil.
type IssueUpdate struct {
	Title *string
	Body  *string
	State *State
}

// IssueFilter selects issues; zero fields select everything. Labels must all be present.
type IssueFilter struct {
	State     State
	Labels    []string
	Milestone string
}

// Comment is one entry on an issue or pull request.
type Comment struct {
	ID   int
	Body string
}

// Milestone groups issues.
type Milestone struct {
	Title       string
	Description string
}

// NewPullRequest asks to merge Head into Base.
type NewPullRequest struct {
	Title string
	Body  string
	Head  string // an existing branch
	Base  string // an existing branch
}

// PullRequest shares its number space with issues.
type PullRequest struct {
	Number int
	Title  string
	Body   string
	Head   string
	Base   string
	State  State
}

// Host is the port.
type Host interface {
	CreateIssue(ctx context.Context, in NewIssue) (Issue, error)
	GetIssue(ctx context.Context, number int) (Issue, error)
	// ListIssues returns matching issues in ascending number order.
	ListIssues(ctx context.Context, f IssueFilter) ([]Issue, error)
	UpdateIssue(ctx context.Context, number int, u IssueUpdate) (Issue, error)

	// AddLabels is idempotent. RemoveLabels ignores labels not on the issue.
	AddLabels(ctx context.Context, number int, labels ...string) (Issue, error)
	RemoveLabels(ctx context.Context, number int, labels ...string) (Issue, error)

	// Comment works on issue and pull request numbers alike.
	Comment(ctx context.Context, number int, body string) (Comment, error)
	// ListComments returns the thread in the order it was written.
	ListComments(ctx context.Context, number int) ([]Comment, error)

	CreateMilestone(ctx context.Context, m Milestone) (Milestone, error)
	ListMilestones(ctx context.Context) ([]Milestone, error)

	// CreateBranch makes name point at the same commit as from.
	CreateBranch(ctx context.Context, name, from string) error
	ListBranches(ctx context.Context) ([]string, error)

	CreatePullRequest(ctx context.Context, in NewPullRequest) (PullRequest, error)
	GetPullRequest(ctx context.Context, number int) (PullRequest, error)
}
