// Package service implements the generated api.Service for the control
// plane: what each route does between the request and the pure function that
// names it. During the bootstrap it is hand-written (M9); the glue plugin
// (#70) will generate it for client apps.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/vault"
	"github.com/briolink/briolink/gen/domain/control"
	api "github.com/briolink/briolink/gen/http"
	"github.com/briolink/briolink/port/codehost"
)

// Records is what the service reads from the control plane's database.
type Records interface {
	ListRuns(ctx context.Context, task *int64, limit int64) ([]control.RunRecord, error)
	ListAudit(ctx context.Context, limit int64) ([]control.AuditEntry, error)
}

// Credentials is the secret store the credential routes act on, as the
// caller named by the audit log. *vault.Vault is one. Nothing here reads a
// secret's value back.
type Credentials interface {
	Put(ctx context.Context, actor, name string, value []byte, d vault.Dates) error
	Rotate(ctx context.Context, actor, name string, value []byte, d vault.Dates) error
	Delete(ctx context.Context, actor, name string) error
	List(ctx context.Context) ([]vault.Entry, error)
	Expiring(ctx context.Context) ([]vault.Entry, error)
	CreateToken(ctx context.Context, actor, name, role string, d vault.Dates) (string, error)
	RevokeToken(ctx context.Context, actor, name string) error
}

// Service is the control plane's api.Service. The credential routes write;
// every other method only reads.
type Service struct {
	Routing     *routing.Table
	Host        codehost.Host
	Records     Records
	Credentials Credentials
	Funcs       control.Funcs
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

// errNoActor is a credential route reached without the token's name in the
// request: the audit line would have no one to name, so nothing is written.
var errNoActor = errors.New("service: the request carries no authenticated token")

func (s *Service) ListSecrets(ctx context.Context, req api.ListSecretsRequest) (api.ListSecretsResponse, error) {
	c, err := s.credentials(ctx, req.Expiring, "secret")
	return api.ListSecretsResponse{Secrets: c}, err
}

func (s *Service) ListTokens(ctx context.Context, req api.ListTokensRequest) (api.ListTokensResponse, error) {
	c, err := s.credentials(ctx, req.Expiring, "token")
	return api.ListTokensResponse{Tokens: c}, err
}

func (s *Service) credentials(ctx context.Context, expiring *bool, kind string) ([]control.Credential, error) {
	list := s.Credentials.List
	if expiring != nil && *expiring {
		list = s.Credentials.Expiring
	}
	entries, err := list(ctx)
	if err != nil {
		return nil, err
	}
	all := make([]control.Credential, len(entries))
	for i, e := range entries {
		all[i] = control.Credential{Kind: e.Kind, Name: e.Name, Role: e.Role, CreatedAt: e.CreatedAt, RotatedAt: e.RotatedAt,
			ExpiresOn: e.ExpiresOn, RemindOn: e.RemindOn, Revoked: e.Revoked}
	}
	return s.Funcs.CredentialsOfKind(all, kind), nil
}

func (s *Service) PutSecret(ctx context.Context, req api.PutSecretRequest) error {
	actor, d, err := s.change(ctx, req.Name, req.ExpiresOn, req.RemindOn)
	if err != nil {
		return err
	}
	return mapVault(s.Credentials.Put(ctx, actor, req.Name, []byte(req.Value), d))
}

func (s *Service) RotateSecret(ctx context.Context, req api.RotateSecretRequest) error {
	actor, d, err := s.change(ctx, req.Name, req.ExpiresOn, req.RemindOn)
	if err != nil {
		return err
	}
	return mapVault(s.Credentials.Rotate(ctx, actor, req.Name, []byte(req.Value), d))
}

func (s *Service) DeleteSecret(ctx context.Context, req api.DeleteSecretRequest) error {
	actor, _, err := s.change(ctx, req.Name, nil, nil)
	if err != nil {
		return err
	}
	return mapVault(s.Credentials.Delete(ctx, actor, req.Name))
}

func (s *Service) IssueToken(ctx context.Context, req api.IssueTokenRequest) (api.IssueTokenResponse, error) {
	actor, d, err := s.change(ctx, req.Name, req.ExpiresOn, req.RemindOn)
	if err != nil {
		return api.IssueTokenResponse{}, err
	}
	token, err := s.Credentials.CreateToken(ctx, actor, req.Name, req.Role, d)
	return api.IssueTokenResponse{Token: token}, mapVault(err)
}

func (s *Service) RevokeToken(ctx context.Context, req api.RevokeTokenRequest) error {
	actor, _, err := s.change(ctx, req.Name, nil, nil)
	if err != nil {
		return err
	}
	return mapVault(s.Credentials.RevokeToken(ctx, actor, req.Name))
}

// change names who is making the change and checks the name and dates with
// credential_problem, before anything is written. The text of a problem is
// not returned to the caller (it says only that the request was refused).
func (s *Service) change(ctx context.Context, name string, expiresOn, remindOn *string) (string, vault.Dates, error) {
	actor := server.Actor(ctx)
	if actor == "" {
		return "", vault.Dates{}, errNoActor
	}
	d := vault.Dates{ExpiresOn: str(expiresOn), RemindOn: str(remindOn)}
	if p := s.Funcs.CredentialProblem(name, d.ExpiresOn, d.RemindOn); p != "" {
		return "", d, fmt.Errorf("%w: %s", api.ErrUnprocessable, p)
	}
	return actor, d, nil
}

// mapVault turns the store's errors into the ones the routes answer with.
// Any other error stays as it is: a 500 that says nothing about its cause.
func mapVault(err error) error {
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return fmt.Errorf("%w: %v", api.ErrNotFound, err)
	case errors.Is(err, vault.ErrExists):
		return fmt.Errorf("%w: %v", api.ErrConflict, err)
	case errors.Is(err, vault.ErrInvalid):
		return fmt.Errorf("%w: %v", api.ErrUnprocessable, err)
	}
	return err
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
