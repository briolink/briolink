package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/briolink/briolink/control/logic"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/vault"
	"github.com/briolink/briolink/gen/domain/control"
	api "github.com/briolink/briolink/gen/http"
	"github.com/briolink/briolink/port/codehost"
)

type records struct {
	task  *int64
	limit int64
	err   error
}

func (r *records) ListRuns(_ context.Context, task *int64, limit int64) ([]control.RunRecord, error) {
	r.task, r.limit = task, limit
	return []control.RunRecord{{Id: 1}}, r.err
}

func (r *records) ListAudit(_ context.Context, limit int64) ([]control.AuditEntry, error) {
	r.limit = limit
	return []control.AuditEntry{{Id: 1}}, r.err
}

func newService(t *testing.T) (*Service, *codehost.Fake, *records) {
	t.Helper()
	host := codehost.NewFake()
	ctx := context.Background()
	if _, err := host.CreateMilestone(ctx, codehost.Milestone{Title: "P0"}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []codehost.NewIssue{
		{Title: "one", Labels: []string{"state:queued", "risk:low"}, Milestone: "P0"},
		{Title: "two", Labels: []string{"state:blocked", "kind:spike"}},
		{Title: "three", Labels: []string{"state:queued"}},
		{Title: "four"},
		{Title: "five", Labels: []string{"state:queued", "state:waiting"}},
	} {
		if _, err := host.CreateIssue(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	closed := codehost.Closed
	if _, err := host.UpdateIssue(ctx, 3, codehost.IssueUpdate{State: &closed}); err != nil {
		t.Fatal(err)
	}
	rec := &records{}
	return &Service{
		Routing: &routing.Table{
			Tiers:                   map[string]string{"top": "t", "coder": "c", "routine": "r"},
			Jobs:                    map[string]string{"b": "top", "a": "coder"},
			ProposePromotionBelowBp: 6000,
			OnTopLimit:              "queue-design-jobs",
		},
		Host: host, Records: rec, Funcs: logic.Impl{},
	}, host, rec
}

func numbers(ts []control.Task) []int64 {
	out := []int64{}
	for _, t := range ts {
		out = append(out, t.Number)
	}
	return out
}

func eq(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGetRoutingIsSortedAndComplete(t *testing.T) {
	s, _, _ := newService(t)
	resp, err := s.GetRouting(context.Background(), api.GetRoutingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	r := resp.Routing
	if len(r.Tiers) != 3 || r.Tiers[0] != (control.TierModel{Tier: "coder", Model: "c"}) || r.Tiers[2].Tier != "top" {
		t.Errorf("tiers = %+v", r.Tiers)
	}
	if len(r.Jobs) != 2 || r.Jobs[0] != (control.JobTier{Job: "a", Tier: "coder"}) {
		t.Errorf("jobs = %+v", r.Jobs)
	}
	if r.ProposePromotionBelowBp != 6000 || r.OnTopLimit != "queue-design-jobs" {
		t.Errorf("routing = %+v", r)
	}
}

func TestListTasksMirrorsOpenIssues(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	resp, err := s.ListTasks(ctx, api.ListTasksRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := numbers(resp.Tasks); !eq(got, []int64{1, 2, 4, 5}) { // #3 is closed
		t.Fatalf("tasks = %v", got)
	}
	first := resp.Tasks[0]
	want := control.Task{Number: 1, Title: "one", State: control.TaskStateQueued, Risk: "low", Milestone: "P0"}
	if first != want {
		t.Errorf("first = %+v, want %+v", first, want)
	}

	for state, want := range map[string][]int64{
		"queued": {1}, "blocked": {2}, "unlabelled": {4}, "conflict": {5}, "waiting": {}, "nonsense": {},
	} {
		st := state
		resp, err := s.ListTasks(ctx, api.ListTasksRequest{State: &st})
		if err != nil || !eq(numbers(resp.Tasks), want) {
			t.Errorf("state %s: %v, %v; want %v", state, numbers(resp.Tasks), err, want)
		}
	}

	two := int64(2)
	resp, _ = s.ListTasks(ctx, api.ListTasksRequest{Limit: &two})
	if got := numbers(resp.Tasks); !eq(got, []int64{1, 2}) {
		t.Errorf("limit 2: %v", got)
	}
	queued := "queued"
	resp, _ = s.ListTasks(ctx, api.ListTasksRequest{State: &queued, Limit: &two})
	if got := numbers(resp.Tasks); !eq(got, []int64{1}) {
		t.Errorf("limit applies after the state filter: %v", got)
	}
}

func TestListTasksReturnsAnEmptyListNotNull(t *testing.T) {
	s := &Service{Host: codehost.NewFake(), Funcs: logic.Impl{}}
	resp, err := s.ListTasks(context.Background(), api.ListTasksRequest{})
	if err != nil || resp.Tasks == nil || len(resp.Tasks) != 0 {
		t.Fatalf("tasks = %#v, %v", resp.Tasks, err)
	}
}

func TestGetTask(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	resp, err := s.GetTask(ctx, api.GetTaskRequest{Number: 3}) // closed issues can be read
	if err != nil || resp.Task.Title != "three" || resp.Task.State != control.TaskStateQueued {
		t.Fatalf("task = %+v, %v", resp.Task, err)
	}
	if _, err := s.GetTask(ctx, api.GetTaskRequest{Number: 99}); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("a missing issue: err = %v", err)
	}
}

type failingHost struct{ codehost.Host }

func (failingHost) ListIssues(context.Context, codehost.IssueFilter) ([]codehost.Issue, error) {
	return nil, errors.New("host down")
}
func (failingHost) GetIssue(context.Context, int) (codehost.Issue, error) {
	return codehost.Issue{}, errors.New("host down")
}

func TestHostFailuresAreNotNotFound(t *testing.T) {
	s := &Service{Host: failingHost{}, Funcs: logic.Impl{}}
	if _, err := s.ListTasks(context.Background(), api.ListTasksRequest{}); err == nil || errors.Is(err, api.ErrNotFound) {
		t.Errorf("ListTasks err = %v", err)
	}
	if _, err := s.GetTask(context.Background(), api.GetTaskRequest{Number: 1}); err == nil || errors.Is(err, api.ErrNotFound) {
		t.Errorf("GetTask err = %v", err)
	}
}

func TestRunsAndAuditUseThePageSize(t *testing.T) {
	s, _, rec := newService(t)
	ctx := context.Background()
	task, big := int64(7), int64(5000)
	if _, err := s.ListRuns(ctx, api.ListRunsRequest{Task: &task}); err != nil || rec.limit != 50 || rec.task == nil || *rec.task != 7 {
		t.Errorf("default: limit %d task %v, %v", rec.limit, rec.task, err)
	}
	if _, err := s.ListRuns(ctx, api.ListRunsRequest{Limit: &big}); err != nil || rec.limit != 200 || rec.task != nil {
		t.Errorf("capped: limit %d task %v, %v", rec.limit, rec.task, err)
	}
	if _, err := s.ListAudit(ctx, api.ListAuditRequest{}); err != nil || rec.limit != 50 {
		t.Errorf("audit default: limit %d, %v", rec.limit, err)
	}
	rec.err = errors.New("db down")
	if _, err := s.ListAudit(ctx, api.ListAuditRequest{}); err == nil {
		t.Error("a database failure was swallowed")
	}
	if _, err := s.ListRuns(ctx, api.ListRunsRequest{}); err == nil {
		t.Error("a database failure was swallowed")
	}
}

type creds struct{ calls []string }

func (c *creds) note(s string) error { c.calls = append(c.calls, s); return nil }
func (c *creds) Put(_ context.Context, actor, name string, _ []byte, _ vault.Dates) error {
	return c.note("put " + actor + " " + name)
}
func (c *creds) Rotate(_ context.Context, actor, name string, _ []byte, _ vault.Dates) error {
	return c.note("rotate " + actor + " " + name)
}
func (c *creds) Delete(_ context.Context, actor, name string) error {
	return c.note("delete " + actor + " " + name)
}
func (c *creds) List(context.Context) ([]vault.Entry, error) {
	return []vault.Entry{{Kind: "secret", Name: "s"}, {Kind: "token", Name: "t", Role: "operator"}}, c.note("list")
}
func (c *creds) Expiring(context.Context) ([]vault.Entry, error) {
	return []vault.Entry{{Kind: "token", Name: "soon"}}, c.note("expiring")
}
func (c *creds) CreateToken(_ context.Context, actor, name, role string, _ vault.Dates) (string, error) {
	return "blk_x", c.note("create " + actor + " " + name + " " + role)
}
func (c *creds) RevokeToken(_ context.Context, actor, name string) error {
	return c.note("revoke " + actor + " " + name)
}

func TestCredentialChangesNameTheCallerAndNeedOne(t *testing.T) {
	s, _, _ := newService(t)
	c := &creds{}
	s.Credentials = c
	bg := context.Background()
	as := server.WithActor(bg, "ci")
	d := "2027-01-01"

	changes := map[string]func(context.Context) error{
		"put": func(ctx context.Context) error {
			return s.PutSecret(ctx, api.PutSecretRequest{Name: "A", Value: "v", ExpiresOn: &d})
		},
		"rotate": func(ctx context.Context) error {
			return s.RotateSecret(ctx, api.RotateSecretRequest{Name: "A", Value: "v"})
		},
		"delete": func(ctx context.Context) error { return s.DeleteSecret(ctx, api.DeleteSecretRequest{Name: "A"}) },
		"create": func(ctx context.Context) error {
			_, err := s.IssueToken(ctx, api.IssueTokenRequest{Name: "A", Role: "operator"})
			return err
		},
		"revoke": func(ctx context.Context) error { return s.RevokeToken(ctx, api.RevokeTokenRequest{Name: "A"}) },
	}
	for verb, f := range changes {
		if err := f(bg); !errors.Is(err, errNoActor) {
			t.Errorf("%s without a caller: %v", verb, err)
		}
		if err := f(as); err != nil {
			t.Errorf("%s: %v", verb, err)
		}
	}
	if len(c.calls) != len(changes) {
		t.Fatalf("calls = %v: a change was made without a caller, or one was missed", c.calls)
	}
	for _, call := range c.calls {
		if !strings.Contains(call, " token:ci A") {
			t.Errorf("call %q does not name the caller", call)
		}
	}
}

func TestCredentialRoutesRefuseWhatCredentialProblemRefuses(t *testing.T) {
	s, _, _ := newService(t)
	c := &creds{}
	s.Credentials = c
	ctx := server.WithActor(context.Background(), "ci")
	bad, after := "2027-02-30", "2027-03-01"
	for name, err := range map[string]error{
		"put":    s.PutSecret(ctx, api.PutSecretRequest{Name: "A", Value: "v", ExpiresOn: &bad}),
		"rotate": s.RotateSecret(ctx, api.RotateSecretRequest{Name: "A", Value: "v", ExpiresOn: &after, RemindOn: &after}),
		"delete": s.DeleteSecret(ctx, api.DeleteSecretRequest{Name: "has space"}),
		"revoke": s.RevokeToken(ctx, api.RevokeTokenRequest{Name: ""}),
	} {
		if !errors.Is(err, api.ErrUnprocessable) && name != "rotate" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(c.calls) != 1 { // only the rotate with the reminder on the expiry day is acceptable
		t.Errorf("calls = %v", c.calls)
	}
}

func TestMapVault(t *testing.T) {
	for in, want := range map[error]error{vault.ErrNotFound: api.ErrNotFound, vault.ErrExists: api.ErrConflict, vault.ErrInvalid: api.ErrUnprocessable} {
		if got := mapVault(fmt.Errorf("wrapped: %w", in)); !errors.Is(got, want) {
			t.Errorf("%v became %v, want %v", in, got, want)
		}
	}
	if mapVault(nil) != nil {
		t.Error("nil became an error")
	}
	for _, other := range []error{vault.ErrWrongKey, vault.ErrDenied, errors.New("disk")} {
		if got := mapVault(other); got != other {
			t.Errorf("%v was changed to %v", other, got)
		}
	}
}

func TestCredentialListsKeepTheirKind(t *testing.T) {
	s, _, _ := newService(t)
	c := &creds{}
	s.Credentials = c
	ctx := context.Background()
	yes, no := true, false
	sec, _ := s.ListSecrets(ctx, api.ListSecretsRequest{})
	tok, _ := s.ListTokens(ctx, api.ListTokensRequest{Expiring: &no})
	if len(sec.Secrets) != 1 || sec.Secrets[0].Name != "s" || len(tok.Tokens) != 1 || tok.Tokens[0].Role != "operator" {
		t.Errorf("lists = %+v / %+v", sec, tok)
	}
	soon, _ := s.ListTokens(ctx, api.ListTokensRequest{Expiring: &yes})
	none, _ := s.ListSecrets(ctx, api.ListSecretsRequest{Expiring: &yes})
	if len(soon.Tokens) != 1 || soon.Tokens[0].Name != "soon" || len(none.Secrets) != 0 {
		t.Errorf("expiring = %+v / %+v", soon, none)
	}
	if strings.Join(c.calls, ",") != "list,list,expiring,expiring" {
		t.Errorf("calls = %v", c.calls)
	}
}
