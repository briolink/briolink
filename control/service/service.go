// Package service implements the generated api.Service for the control
// plane: what each route does between the request and the pure function that
// names it. During the bootstrap it is hand-written (M9); the glue plugin
// (#70) will generate it for client apps.
package service

import (
	"context"
	"errors"

	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/gen/domain/control"
	api "github.com/briolink/briolink/gen/http"
	"github.com/briolink/briolink/port/codehost"
)

// Records is what the service reads from the control plane's database.
type Records interface {
	ListRuns(ctx context.Context, task *int64, limit int64) ([]control.RunRecord, error)
	ListAudit(ctx context.Context, limit int64) ([]control.AuditEntry, error)
}

// Service is the control plane's api.Service. Every method only reads.
type Service struct {
	Routing *routing.Table
	Host    codehost.Host
	Records Records
	Funcs   control.Funcs
}

var _ api.Service = (*Service)(nil)

func (s *Service) GetRouting(_ context.Context, _ api.GetRoutingRequest) (api.GetRoutingResponse, error) {
	r := control.Routing{
		ProposePromotionBelowBp: int64(s.Routing.ProposePromotionBelowBp),
		OnTopLimit:              s.Routing.OnTopLimit,
	}
	for tier, model := range s.Routing.Tiers {
		r.Tiers = append(r.Tiers, control.TierModel{Tier: tier, Model: model})
	}
	for job, tier := range s.Routing.Jobs {
		r.Jobs = append(r.Jobs, control.JobTier{Job: job, Tier: tier})
	}
	return api.GetRoutingResponse{Routing: s.Funcs.RoutingView(r)}, nil
}

// ListTasks mirrors the open issues of the code host, as the issues are
// (ascending number), keeping those in the requested state.
func (s *Service) ListTasks(ctx context.Context, req api.ListTasksRequest) (api.ListTasksResponse, error) {
	issues, err := s.Host.ListIssues(ctx, codehost.IssueFilter{State: codehost.Open})
	if err != nil {
		return api.ListTasksResponse{}, err
	}
	limit := s.Funcs.PageSize(deref(req.Limit))
	tasks := []control.Task{}
	for _, is := range issues {
		t := s.task(is)
		if req.State != nil && string(t.State) != *req.State {
			continue
		}
		if int64(len(tasks)) == limit {
			break
		}
		tasks = append(tasks, t)
	}
	return api.ListTasksResponse{Tasks: tasks}, nil
}

func (s *Service) GetTask(ctx context.Context, req api.GetTaskRequest) (api.GetTaskResponse, error) {
	is, err := s.Host.GetIssue(ctx, int(req.Number))
	if errors.Is(err, codehost.ErrNotFound) {
		return api.GetTaskResponse{}, api.ErrNotFound
	}
	if err != nil {
		return api.GetTaskResponse{}, err
	}
	return api.GetTaskResponse{Task: s.task(is)}, nil
}

func (s *Service) task(is codehost.Issue) control.Task {
	return s.Funcs.TaskFromIssue(int64(is.Number), is.Title, is.Labels, is.Milestone)
}

func (s *Service) ListRuns(ctx context.Context, req api.ListRunsRequest) (api.ListRunsResponse, error) {
	runs, err := s.Records.ListRuns(ctx, req.Task, s.Funcs.PageSize(deref(req.Limit)))
	return api.ListRunsResponse{Runs: runs}, err
}

func (s *Service) ListAudit(ctx context.Context, req api.ListAuditRequest) (api.ListAuditResponse, error) {
	entries, err := s.Records.ListAudit(ctx, s.Funcs.PageSize(deref(req.Limit)))
	return api.ListAuditResponse{Entries: entries}, err
}

// deref is 0 for an absent optional value, which page_size reads as no limit.
func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
