package codehost

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// Contract is the suite every Host must pass. newHost returns a host with an
// empty tracker and one branch, "main" (a scratch repository per call).
func Contract(t *testing.T, newHost func(t *testing.T) Host) {
	ctx := context.Background()
	run := func(name string, fn func(t *testing.T, h Host)) {
		t.Run(name, func(t *testing.T) { fn(t, newHost(t)) })
	}
	must := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	run("issue lifecycle", func(t *testing.T, h Host) {
		_, err := h.CreateMilestone(ctx, Milestone{Title: "P0", Description: "core"})
		must(t, err)
		is, err := h.CreateIssue(ctx, NewIssue{Title: "t", Body: "b", Labels: []string{"kind:logic", "risk:low"}, Milestone: "P0"})
		must(t, err)
		if is.Number <= 0 || is.State != Open || is.Title != "t" || is.Body != "b" || is.Milestone != "P0" {
			t.Fatalf("created %+v", is)
		}
		if !slices.Equal(is.Labels, []string{"kind:logic", "risk:low"}) {
			t.Fatalf("labels %v", is.Labels)
		}
		got, err := h.GetIssue(ctx, is.Number)
		must(t, err)
		if got.Title != "t" {
			t.Fatalf("got %+v", got)
		}
		title, closed := "t2", Closed
		up, err := h.UpdateIssue(ctx, is.Number, IssueUpdate{Title: &title, State: &closed})
		must(t, err)
		if up.Title != "t2" || up.State != Closed || up.Body != "b" {
			t.Fatalf("updated %+v (untouched fields must stay)", up)
		}
	})

	run("invalid and missing issues", func(t *testing.T, h Host) {
		if _, err := h.CreateIssue(ctx, NewIssue{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("empty title: %v", err)
		}
		if _, err := h.CreateIssue(ctx, NewIssue{Title: "t", Milestone: "none"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("unknown milestone: %v", err)
		}
		if _, err := h.GetIssue(ctx, 999); !errors.Is(err, ErrNotFound) {
			t.Errorf("get: %v", err)
		}
		if _, err := h.UpdateIssue(ctx, 999, IssueUpdate{}); !errors.Is(err, ErrNotFound) {
			t.Errorf("update: %v", err)
		}
		if _, err := h.AddLabels(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
			t.Errorf("label: %v", err)
		}
	})

	run("labels add and remove are idempotent", func(t *testing.T, h Host) {
		is, _ := h.CreateIssue(ctx, NewIssue{Title: "t"})
		for i := 0; i < 2; i++ {
			got, err := h.AddLabels(ctx, is.Number, "state:queued")
			must(t, err)
			if !slices.Equal(got.Labels, []string{"state:queued"}) {
				t.Fatalf("add %d: %v", i, got.Labels)
			}
		}
		for i := 0; i < 2; i++ {
			got, err := h.RemoveLabels(ctx, is.Number, "state:queued", "never-there")
			must(t, err)
			if len(got.Labels) != 0 {
				t.Fatalf("remove %d: %v", i, got.Labels)
			}
		}
	})

	run("list filters by state, labels and milestone, in number order", func(t *testing.T, h Host) {
		_, err := h.CreateMilestone(ctx, Milestone{Title: "P0"})
		must(t, err)
		a, _ := h.CreateIssue(ctx, NewIssue{Title: "a", Labels: []string{"x", "y"}, Milestone: "P0"})
		b, _ := h.CreateIssue(ctx, NewIssue{Title: "b", Labels: []string{"x"}})
		c, _ := h.CreateIssue(ctx, NewIssue{Title: "c", Labels: []string{"x", "y"}})
		closed := Closed
		_, err = h.UpdateIssue(ctx, c.Number, IssueUpdate{State: &closed})
		must(t, err)

		nums := func(f IssueFilter) []int {
			got, err := h.ListIssues(ctx, f)
			must(t, err)
			var n []int
			for _, i := range got {
				n = append(n, i.Number)
			}
			return n
		}
		for _, tc := range []struct {
			name string
			f    IssueFilter
			want []int
		}{
			{"all", IssueFilter{}, []int{a.Number, b.Number, c.Number}},
			{"open", IssueFilter{State: Open}, []int{a.Number, b.Number}},
			{"closed", IssueFilter{State: Closed}, []int{c.Number}},
			{"all labels must match", IssueFilter{Labels: []string{"x", "y"}}, []int{a.Number, c.Number}},
			{"milestone", IssueFilter{Milestone: "P0"}, []int{a.Number}},
			{"combined", IssueFilter{State: Open, Labels: []string{"y"}}, []int{a.Number}},
		} {
			if got := nums(tc.f); !slices.Equal(got, tc.want) {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	})

	run("comments keep order", func(t *testing.T, h Host) {
		is, _ := h.CreateIssue(ctx, NewIssue{Title: "t"})
		first, err := h.Comment(ctx, is.Number, "one")
		must(t, err)
		second, err := h.Comment(ctx, is.Number, "two")
		must(t, err)
		if first.ID == second.ID {
			t.Fatal("comment IDs must differ")
		}
		got, err := h.ListComments(ctx, is.Number)
		must(t, err)
		if len(got) != 2 || got[0].Body != "one" || got[1].Body != "two" {
			t.Fatalf("got %+v", got)
		}
		if _, err := h.Comment(ctx, is.Number, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("empty comment: %v", err)
		}
		if _, err := h.Comment(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
			t.Errorf("missing target: %v", err)
		}
		if _, err := h.ListComments(ctx, 999); !errors.Is(err, ErrNotFound) {
			t.Errorf("list on missing target: %v", err)
		}
	})

	run("milestones", func(t *testing.T, h Host) {
		_, err := h.CreateMilestone(ctx, Milestone{Title: "P0", Description: "d"})
		must(t, err)
		if _, err := h.CreateMilestone(ctx, Milestone{Title: "P0"}); !errors.Is(err, ErrExists) {
			t.Errorf("duplicate: %v", err)
		}
		if _, err := h.CreateMilestone(ctx, Milestone{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("empty title: %v", err)
		}
		got, err := h.ListMilestones(ctx)
		must(t, err)
		if len(got) != 1 || got[0].Title != "P0" || got[0].Description != "d" {
			t.Fatalf("got %+v", got)
		}
	})

	run("branches", func(t *testing.T, h Host) {
		must(t, h.CreateBranch(ctx, "task/1-x", "main"))
		if err := h.CreateBranch(ctx, "task/1-x", "main"); !errors.Is(err, ErrExists) {
			t.Errorf("duplicate: %v", err)
		}
		if err := h.CreateBranch(ctx, "task/2-y", "nope"); !errors.Is(err, ErrNotFound) {
			t.Errorf("missing source: %v", err)
		}
		if err := h.CreateBranch(ctx, "", "main"); !errors.Is(err, ErrInvalid) {
			t.Errorf("empty name: %v", err)
		}
		got, err := h.ListBranches(ctx)
		must(t, err)
		if !slices.Contains(got, "main") || !slices.Contains(got, "task/1-x") {
			t.Fatalf("got %v", got)
		}
	})

	run("pull requests share the number space and take comments", func(t *testing.T, h Host) {
		must(t, h.CreateBranch(ctx, "task/1-x", "main"))
		is, _ := h.CreateIssue(ctx, NewIssue{Title: "t"})
		pr, err := h.CreatePullRequest(ctx, NewPullRequest{Title: "p", Body: "Closes #1", Head: "task/1-x", Base: "main"})
		must(t, err)
		if pr.Number == is.Number || pr.State != Open || pr.Head != "task/1-x" || pr.Base != "main" {
			t.Fatalf("pr %+v, issue %d", pr, is.Number)
		}
		got, err := h.GetPullRequest(ctx, pr.Number)
		must(t, err)
		if got.Title != "p" || got.Body != "Closes #1" {
			t.Fatalf("got %+v", got)
		}
		_, err = h.Comment(ctx, pr.Number, "attempt 1")
		must(t, err)
		cs, _ := h.ListComments(ctx, pr.Number)
		if len(cs) != 1 {
			t.Fatalf("comments %+v", cs)
		}
		if _, err := h.GetPullRequest(ctx, is.Number); !errors.Is(err, ErrNotFound) {
			t.Errorf("an issue is not a pull request: %v", err)
		}
	})

	run("pull request validation", func(t *testing.T, h Host) {
		must(t, h.CreateBranch(ctx, "b", "main"))
		for name, in := range map[string]NewPullRequest{
			"no title":  {Head: "b", Base: "main"},
			"same head": {Title: "p", Head: "main", Base: "main"},
		} {
			if _, err := h.CreatePullRequest(ctx, in); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if _, err := h.CreatePullRequest(ctx, NewPullRequest{Title: "p", Head: "ghost", Base: "main"}); !errors.Is(err, ErrNotFound) {
			t.Errorf("missing head: %v", err)
		}
	})

	run("results are copies", func(t *testing.T, h Host) {
		is, _ := h.CreateIssue(ctx, NewIssue{Title: "t", Labels: []string{"a"}})
		is.Labels[0] = "mutated"
		got, _ := h.GetIssue(ctx, is.Number)
		if got.Labels[0] != "a" {
			t.Fatalf("host aliased a caller's slice: %v", got.Labels)
		}
	})

	run("cancelled context", func(t *testing.T, h Host) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := h.CreateIssue(c, NewIssue{Title: "t"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
}
