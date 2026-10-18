package service

import (
	"context"
	"errors"
	"testing"

	"github.com/briolink/briolink/control/logic"
	"github.com/briolink/briolink/control/routing"
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
