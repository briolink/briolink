package github

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/briolink/briolink/port/codehost"
)

type staticToken string

func (s staticToken) Token(ctx context.Context) (string, error) { return string(s), nil }

// committing pushes a commit onto every branch it creates, as the runner
// does before it opens a pull request: GitHub refuses one whose head has
// nothing its base lacks, and the port has no commit of its own.
type committing struct{ *Host }

func (c committing) CreateBranch(ctx context.Context, name, from string) error {
	if err := c.Host.CreateBranch(ctx, name, from); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPut, "/contents/contract.txt", map[string]any{
		"message": "contract: a commit on " + name,
		"content": base64.StdEncoding.EncodeToString([]byte(name + "\n")),
		"branch":  name,
	}, nil)
	return err
}

func TestContract(t *testing.T) {
	codehost.Contract(t, func(t *testing.T) codehost.Host {
		f := newFakeAPI(t, "acme/site", "ghs_bot")
		return committing{&Host{Repo: "acme/site", Tokens: staticToken("ghs_bot"), BaseURL: f.srv.URL}}
	})
}

func TestPullRequestWithoutCommitsIsInvalid(t *testing.T) {
	f := newFakeAPI(t, "acme/site", "ghs_bot")
	h := &Host{Repo: "acme/site", Tokens: staticToken("ghs_bot"), BaseURL: f.srv.URL}
	ctx := context.Background()
	if err := h.CreateBranch(ctx, "task/1-x", "main"); err != nil {
		t.Fatal(err)
	}
	_, err := h.CreatePullRequest(ctx, codehost.NewPullRequest{Title: "p", Head: "task/1-x", Base: "main"})
	if !errors.Is(err, codehost.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestIssueAPIDoesNotServePullRequests(t *testing.T) {
	f := newFakeAPI(t, "acme/site", "ghs_bot")
	h := committing{&Host{Repo: "acme/site", Tokens: staticToken("ghs_bot"), BaseURL: f.srv.URL}}
	ctx := context.Background()
	if err := h.CreateBranch(ctx, "b", "main"); err != nil {
		t.Fatal(err)
	}
	pr, err := h.CreatePullRequest(ctx, codehost.NewPullRequest{Title: "p", Head: "b", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	title := "x"
	for name, err := range map[string]error{
		"get":    func() error { _, err := h.GetIssue(ctx, pr.Number); return err }(),
		"update": func() error { _, err := h.UpdateIssue(ctx, pr.Number, codehost.IssueUpdate{Title: &title}); return err }(),
		"label":  func() error { _, err := h.AddLabels(ctx, pr.Number, "x"); return err }(),
	} {
		if !errors.Is(err, codehost.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got, _ := h.ListIssues(ctx, codehost.IssueFilter{}); len(got) != 0 {
		t.Errorf("listed %+v", got)
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("token failure stops the request", func(t *testing.T) {
		h := &Host{Repo: "acme/site", Tokens: failingTokens{}, BaseURL: "http://127.0.0.1:1"}
		if _, err := h.GetIssue(ctx, 1); !errors.Is(err, ErrRefused) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("an unmapped status is ErrAPI with GitHub's message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
		}))
		defer srv.Close()
		h := &Host{Repo: "acme/site", Tokens: staticToken("t"), BaseURL: srv.URL}
		_, err := h.CreateMilestone(ctx, codehost.Milestone{Title: "P0"})
		if !errors.Is(err, ErrAPI) || err.Error() != "github: request failed: POST /milestones: status 403: Resource not accessible by integration" {
			t.Fatalf("got %v", err)
		}
	})
}

type failingTokens struct{}

func (failingTokens) Token(ctx context.Context) (string, error) { return "", ErrRefused }

func TestNextLink(t *testing.T) {
	for in, want := range map[string]string{
		"": "",
		`<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"`: "https://api.github.com/x?page=2",
		`<https://api.github.com/x?page=1>; rel="prev"`:                                                "",
	} {
		if got := nextLink(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}
