package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/briolink/briolink/port/codehost"
)

// ErrAPI: GitHub answered with a status the port has no error for (401, 403,
// a rate limit, a server error). The status and GitHub's message follow it.
var ErrAPI = errors.New("github: request failed")

// maxBody caps what is read of one answer.
const maxBody = 8 << 20

// Tokens gives the token every request carries; *TokenSource is the bot's.
type Tokens interface {
	Token(ctx context.Context) (string, error)
}

// Host is the CodeHost port on one GitHub repository, acting as whoever
// Tokens' tokens belong to: in the control plane, the bot (A22).
type Host struct {
	Repo    string // "owner/name"
	Tokens  Tokens
	BaseURL string       // DefaultBaseURL when empty
	Client  *http.Client // http.DefaultClient when nil
}

var _ codehost.Host = (*Host)(nil)

// The parts of GitHub's answers the port reads.
type (
	ghLabel     struct{ Name string }
	ghMilestone struct {
		Number      int
		Title       string
		Description *string
	}
	ghIssue struct {
		Number      int
		Title       string
		Body        *string
		State       string
		Labels      []ghLabel
		Milestone   *ghMilestone
		PullRequest *struct{} `json:"pull_request"`
	}
	ghComment struct {
		ID   int64
		Body string
	}
	ghRef struct {
		Object struct{ SHA string }
	}
	ghPull struct {
		Number int
		Title  string
		Body   *string
		State  string
		Head   struct{ Ref string }
		Base   struct{ Ref string }
	}
)

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (g ghIssue) issue() codehost.Issue {
	is := codehost.Issue{Number: g.Number, Title: g.Title, Body: str(g.Body), State: codehost.State(g.State)}
	for _, l := range g.Labels {
		is.Labels = append(is.Labels, l.Name)
	}
	slices.Sort(is.Labels)
	if g.Milestone != nil {
		is.Milestone = g.Milestone.Title
	}
	return is
}

func (g ghPull) pull() codehost.PullRequest {
	return codehost.PullRequest{Number: g.Number, Title: g.Title, Body: str(g.Body), Head: g.Head.Ref, Base: g.Base.Ref, State: codehost.State(g.State)}
}

// do sends one request to path (under /repos/{Repo} unless it starts with
// "/"), decodes a 2xx answer into out when not nil, and returns the next
// page's URL from the Link header, if any.
func (h *Host) do(ctx context.Context, method, path string, in, out any) (next string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		base := strings.TrimRight(h.BaseURL, "/")
		if base == "" {
			base = DefaultBaseURL
		}
		u = base + "/repos/" + h.Repo + path
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return "", err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return "", err
	}
	tok, err := h.Tokens.Token(ctx)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	if resp.StatusCode/100 != 2 {
		return "", statusErr(method, path, resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return "", fmt.Errorf("%w: %s %s: unreadable answer: %v", ErrAPI, method, path, err)
		}
	}
	return nextLink(resp.Header.Get("Link")), nil
}

// statusErr maps GitHub's status to the port's errors: 404 and 410 (gone)
// are not found, 422 is invalid unless GitHub says the thing exists already.
func statusErr(method, path string, status int, data []byte) error {
	var e struct {
		Message string
		Errors  []struct{ Code string }
	}
	_ = json.Unmarshal(data, &e)
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return fmt.Errorf("%w: %s %s", codehost.ErrNotFound, method, path)
	case http.StatusUnprocessableEntity:
		exists := strings.Contains(strings.ToLower(e.Message), "already exists")
		for _, x := range e.Errors {
			exists = exists || x.Code == "already_exists"
		}
		if exists {
			return fmt.Errorf("%w: %s %s", codehost.ErrExists, method, path)
		}
		return fmt.Errorf("%w: %s %s: %s", codehost.ErrInvalid, method, path, e.Message)
	}
	return fmt.Errorf("%w: %s %s: status %d: %s", ErrAPI, method, path, status, e.Message)
}

// nextLink reads rel="next" from a Link header.
func nextLink(h string) string {
	for _, part := range strings.Split(h, ",") {
		seg := strings.Split(part, ";")
		if len(seg) < 2 || strings.TrimSpace(seg[1]) != `rel="next"` {
			continue
		}
		return strings.Trim(strings.TrimSpace(seg[0]), "<>")
	}
	return ""
}

// all reads every page of a list.
func all[T any](ctx context.Context, h *Host, path string) ([]T, error) {
	var out []T
	for next := path; next != ""; {
		var page []T
		n, err := h.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		next = n
	}
	return out, nil
}

// refPath is the git ref API path of a branch, each segment escaped.
func refPath(branch string) string {
	seg := strings.Split(branch, "/")
	for i, s := range seg {
		seg[i] = url.PathEscape(s)
	}
	return "/git/ref/heads/" + strings.Join(seg, "/")
}

// milestoneNumber finds a milestone's number by title; 0 when there is none.
func (h *Host) milestoneNumber(ctx context.Context, title string) (int, error) {
	ms, err := all[ghMilestone](ctx, h, "/milestones?state=all&per_page=100")
	if err != nil {
		return 0, err
	}
	for _, m := range ms {
		if m.Title == title {
			return m.Number, nil
		}
	}
	return 0, nil
}

// getIssue reads an issue; a pull request's number is not found, as GitHub
// serves pull requests on its issues API too.
func (h *Host) getIssue(ctx context.Context, number int) (ghIssue, error) {
	var g ghIssue
	if _, err := h.do(ctx, http.MethodGet, "/issues/"+strconv.Itoa(number), nil, &g); err != nil {
		return g, err
	}
	if g.PullRequest != nil {
		return g, fmt.Errorf("%w: #%d is a pull request", codehost.ErrNotFound, number)
	}
	return g, nil
}

func nonEmpty(labels []string) []string {
	return slices.DeleteFunc(slices.Clone(labels), func(l string) bool { return l == "" })
}

func (h *Host) CreateIssue(ctx context.Context, in codehost.NewIssue) (codehost.Issue, error) {
	if err := ctx.Err(); err != nil {
		return codehost.Issue{}, err
	}
	if in.Title == "" {
		return codehost.Issue{}, fmt.Errorf("%w: empty title", codehost.ErrInvalid)
	}
	req := map[string]any{"title": in.Title, "body": in.Body}
	if l := nonEmpty(in.Labels); len(l) > 0 {
		req["labels"] = l
	}
	if in.Milestone != "" {
		n, err := h.milestoneNumber(ctx, in.Milestone)
		if err != nil {
			return codehost.Issue{}, err
		}
		if n == 0 {
			return codehost.Issue{}, fmt.Errorf("%w: no milestone %q", codehost.ErrInvalid, in.Milestone)
		}
		req["milestone"] = n
	}
	var g ghIssue
	if _, err := h.do(ctx, http.MethodPost, "/issues", req, &g); err != nil {
		return codehost.Issue{}, err
	}
	return g.issue(), nil
}

func (h *Host) GetIssue(ctx context.Context, number int) (codehost.Issue, error) {
	g, err := h.getIssue(ctx, number)
	if err != nil {
		return codehost.Issue{}, err
	}
	return g.issue(), nil
}

func (h *Host) ListIssues(ctx context.Context, f codehost.IssueFilter) ([]codehost.Issue, error) {
	q := url.Values{"state": {"all"}, "sort": {"created"}, "direction": {"asc"}, "per_page": {"100"}}
	if f.State != "" {
		q.Set("state", string(f.State))
	}
	if l := nonEmpty(f.Labels); len(l) > 0 {
		q.Set("labels", strings.Join(l, ","))
	}
	if f.Milestone != "" {
		n, err := h.milestoneNumber(ctx, f.Milestone)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, nil
		}
		q.Set("milestone", strconv.Itoa(n))
	}
	gs, err := all[ghIssue](ctx, h, "/issues?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var out []codehost.Issue
	for _, g := range gs {
		if g.PullRequest == nil {
			out = append(out, g.issue())
		}
	}
	slices.SortFunc(out, func(a, b codehost.Issue) int { return a.Number - b.Number })
	return out, nil
}

func (h *Host) UpdateIssue(ctx context.Context, number int, u codehost.IssueUpdate) (codehost.Issue, error) {
	if (u.Title != nil && *u.Title == "") || (u.State != nil && *u.State != codehost.Open && *u.State != codehost.Closed) {
		return codehost.Issue{}, fmt.Errorf("%w: empty title or unknown state", codehost.ErrInvalid)
	}
	g, err := h.getIssue(ctx, number)
	if err != nil {
		return codehost.Issue{}, err
	}
	req := map[string]any{}
	if u.Title != nil {
		req["title"] = *u.Title
	}
	if u.Body != nil {
		req["body"] = *u.Body
	}
	if u.State != nil {
		req["state"] = string(*u.State)
	}
	if len(req) == 0 {
		return g.issue(), nil
	}
	if _, err := h.do(ctx, http.MethodPatch, "/issues/"+strconv.Itoa(number), req, &g); err != nil {
		return codehost.Issue{}, err
	}
	return g.issue(), nil
}

func (h *Host) AddLabels(ctx context.Context, number int, labels ...string) (codehost.Issue, error) {
	g, err := h.getIssue(ctx, number)
	if err != nil {
		return codehost.Issue{}, err
	}
	l := nonEmpty(labels)
	if len(l) == 0 {
		return g.issue(), nil
	}
	// The answer is the issue's whole label list.
	if _, err := h.do(ctx, http.MethodPost, "/issues/"+strconv.Itoa(number)+"/labels", map[string]any{"labels": l}, &g.Labels); err != nil {
		return codehost.Issue{}, err
	}
	return g.issue(), nil
}

func (h *Host) RemoveLabels(ctx context.Context, number int, labels ...string) (codehost.Issue, error) {
	g, err := h.getIssue(ctx, number)
	if err != nil {
		return codehost.Issue{}, err
	}
	var keep []ghLabel
	for _, l := range g.Labels {
		if !slices.Contains(labels, l.Name) {
			keep = append(keep, l)
			continue
		}
		_, err := h.do(ctx, http.MethodDelete, "/issues/"+strconv.Itoa(number)+"/labels/"+url.PathEscape(l.Name), nil, nil)
		if err != nil && !errors.Is(err, codehost.ErrNotFound) {
			return codehost.Issue{}, err
		}
	}
	g.Labels = keep
	return g.issue(), nil
}

func (h *Host) Comment(ctx context.Context, number int, body string) (codehost.Comment, error) {
	if err := ctx.Err(); err != nil {
		return codehost.Comment{}, err
	}
	if body == "" {
		return codehost.Comment{}, fmt.Errorf("%w: empty comment", codehost.ErrInvalid)
	}
	var c ghComment
	if _, err := h.do(ctx, http.MethodPost, "/issues/"+strconv.Itoa(number)+"/comments", map[string]any{"body": body}, &c); err != nil {
		return codehost.Comment{}, err
	}
	return codehost.Comment{ID: int(c.ID), Body: c.Body}, nil
}

func (h *Host) ListComments(ctx context.Context, number int) ([]codehost.Comment, error) {
	// The comments list of a missing number is empty, not an error: ask for
	// the issue (or pull request) first.
	if _, err := h.do(ctx, http.MethodGet, "/issues/"+strconv.Itoa(number), nil, nil); err != nil {
		return nil, err
	}
	cs, err := all[ghComment](ctx, h, "/issues/"+strconv.Itoa(number)+"/comments?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]codehost.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, codehost.Comment{ID: int(c.ID), Body: c.Body})
	}
	return out, nil
}

func (h *Host) CreateMilestone(ctx context.Context, m codehost.Milestone) (codehost.Milestone, error) {
	if err := ctx.Err(); err != nil {
		return codehost.Milestone{}, err
	}
	if m.Title == "" {
		return codehost.Milestone{}, fmt.Errorf("%w: empty title", codehost.ErrInvalid)
	}
	var g ghMilestone
	if _, err := h.do(ctx, http.MethodPost, "/milestones", map[string]any{"title": m.Title, "description": m.Description}, &g); err != nil {
		return codehost.Milestone{}, err
	}
	return codehost.Milestone{Title: g.Title, Description: str(g.Description)}, nil
}

// ListMilestones returns open and closed milestones in the order they were made.
func (h *Host) ListMilestones(ctx context.Context) ([]codehost.Milestone, error) {
	ms, err := all[ghMilestone](ctx, h, "/milestones?state=all&per_page=100")
	if err != nil {
		return nil, err
	}
	slices.SortFunc(ms, func(a, b ghMilestone) int { return a.Number - b.Number })
	out := make([]codehost.Milestone, 0, len(ms))
	for _, m := range ms {
		out = append(out, codehost.Milestone{Title: m.Title, Description: str(m.Description)})
	}
	return out, nil
}

func (h *Host) CreateBranch(ctx context.Context, name, from string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if name == "" || from == "" {
		return fmt.Errorf("%w: empty branch name", codehost.ErrInvalid)
	}
	var ref ghRef
	if _, err := h.do(ctx, http.MethodGet, refPath(from), nil, &ref); err != nil {
		return err
	}
	_, err := h.do(ctx, http.MethodPost, "/git/refs", map[string]any{"ref": "refs/heads/" + name, "sha": ref.Object.SHA}, nil)
	return err
}

func (h *Host) ListBranches(ctx context.Context) ([]string, error) {
	bs, err := all[struct{ Name string }](ctx, h, "/branches?per_page=100")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Name)
	}
	slices.Sort(out)
	return out, nil
}

func (h *Host) CreatePullRequest(ctx context.Context, in codehost.NewPullRequest) (codehost.PullRequest, error) {
	if err := ctx.Err(); err != nil {
		return codehost.PullRequest{}, err
	}
	if in.Title == "" || in.Head == "" || in.Base == "" || in.Head == in.Base {
		return codehost.PullRequest{}, fmt.Errorf("%w: a title and two different branches are required", codehost.ErrInvalid)
	}
	// GitHub answers 422 for a missing branch; the port says not found.
	for _, b := range []string{in.Head, in.Base} {
		if _, err := h.do(ctx, http.MethodGet, refPath(b), nil, nil); err != nil {
			return codehost.PullRequest{}, err
		}
	}
	var g ghPull
	if _, err := h.do(ctx, http.MethodPost, "/pulls", map[string]any{"title": in.Title, "body": in.Body, "head": in.Head, "base": in.Base}, &g); err != nil {
		return codehost.PullRequest{}, err
	}
	return g.pull(), nil
}

func (h *Host) GetPullRequest(ctx context.Context, number int) (codehost.PullRequest, error) {
	var g ghPull
	if _, err := h.do(ctx, http.MethodGet, "/pulls/"+strconv.Itoa(number), nil, &g); err != nil {
		return codehost.PullRequest{}, err
	}
	return g.pull(), nil
}
