package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/logic"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/service"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/codehost"
)

const token = "s3cret-token"

type rig struct {
	url    string
	store  *store.Store
	broker *events.Broker
	host   *codehost.Fake
}

func newRig(t *testing.T, heartbeat time.Duration) rig {
	t.Helper()
	ctx := context.Background()
	broker := events.New()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"), broker, func() time.Time {
		return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	host := codehost.NewFake()
	if _, err := host.CreateIssue(ctx, codehost.NewIssue{Title: "First task", Labels: []string{"state:queued", "risk:low", "kind:infra"}}); err != nil {
		t.Fatal(err)
	}
	svc := &service.Service{
		Routing: &routing.Table{
			Tiers: map[string]string{"coder": "c", "routine": "r", "top": "t"},
			Jobs:  map[string]string{"pure_function": "coder"}, ProposePromotionBelowBp: 6000, OnTopLimit: "queue-design-jobs",
		},
		Host: host, Records: st, Funcs: logic.Impl{},
	}
	srv := httptest.NewServer(server.Handler(server.Config{Service: svc, Events: broker, Token: token, Heartbeat: heartbeat}))
	t.Cleanup(srv.Close)
	return rig{srv.URL, st, broker, host}
}

func (r rig) get(t *testing.T, path, bearer string) (int, http.Header, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("GET", r.url+path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("%s: not a JSON object: %q", path, body)
	}
	return resp.StatusCode, resp.Header, out
}

func TestEveryEndpointNeedsTheToken(t *testing.T) {
	r := newRig(t, 0)
	for _, path := range []string{"/api/v1/routing", "/api/v1/tasks", "/api/v1/tasks/1", "/api/v1/runs", "/api/v1/audit", "/api/v1/events"} {
		for name, bearer := range map[string]string{"none": "", "wrong": "nope", "prefix": token[:5], "longer": token + "x"} {
			status, hdr, body := r.get(t, path, bearer)
			if status != 401 || body["status"] != float64(401) {
				t.Errorf("%s with %s token: %d %v", path, name, status, body)
			}
			if hdr.Get("Cache-Control") != "no-store" {
				t.Errorf("%s: Cache-Control = %q", path, hdr.Get("Cache-Control"))
			}
		}
	}
	// A different scheme is not a bearer token.
	req, _ := http.NewRequest("GET", r.url+"/api/v1/routing", nil)
	req.Header.Set("Authorization", "Basic "+token)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("basic scheme: %d", resp.StatusCode)
	}
}

func TestNoConfiguredTokenAuthenticatesNobody(t *testing.T) {
	auth := server.BearerAuth("")
	for _, h := range []string{"", "Bearer ", "Bearer x"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", h)
		if _, ok := auth(req); ok {
			t.Errorf("header %q authenticated against an empty token", h)
		}
	}
}

func TestRoutingAndTasks(t *testing.T) {
	r := newRig(t, 0)
	status, hdr, body := r.get(t, "/api/v1/routing", token)
	routing, _ := body["routing"].(map[string]any)
	if status != 200 || routing["propose_promotion_below_bp"] != float64(6000) || routing["on_top_limit"] != "queue-design-jobs" {
		t.Fatalf("routing: %d %v", status, body)
	}
	if hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", hdr.Get("Cache-Control"))
	}
	if tiers, _ := routing["tiers"].([]any); len(tiers) != 3 {
		t.Errorf("tiers = %v", routing["tiers"])
	}

	status, _, body = r.get(t, "/api/v1/tasks", token)
	tasks, _ := body["tasks"].([]any)
	if status != 200 || len(tasks) != 1 {
		t.Fatalf("tasks: %d %v", status, body)
	}
	task := tasks[0].(map[string]any)
	if task["number"] != float64(1) || task["state"] != "queued" || task["risk"] != "low" || task["kind"] != "infra" || task["milestone"] != "" {
		t.Errorf("task = %v", task)
	}
	if status, _, body := r.get(t, "/api/v1/tasks?state=blocked", token); status != 200 || len(body["tasks"].([]any)) != 0 {
		t.Errorf("filter: %d %v", status, body)
	}
	if status, _, body := r.get(t, "/api/v1/tasks/1", token); status != 200 || body["task"].(map[string]any)["title"] != "First task" {
		t.Errorf("one task: %d %v", status, body)
	}
	if status, _, _ := r.get(t, "/api/v1/tasks/99", token); status != 404 {
		t.Errorf("missing task: %d", status)
	}
	if status, _, _ := r.get(t, "/api/v1/tasks/0", token); status != 400 {
		t.Errorf("task 0: %d", status)
	}
	if status, _, _ := r.get(t, "/api/v1/tasks?limit=0", token); status != 400 {
		t.Errorf("limit 0: %d", status)
	}
	if status, _, _ := r.get(t, "/api/v1/tasks?bogus=1", token); status != 400 {
		t.Errorf("unknown query parameter: %d", status)
	}
}

func TestRunsAndAudit(t *testing.T) {
	r := newRig(t, 0)
	ctx := context.Background()
	for _, task := range []int64{1, 2, 1} {
		if _, err := r.store.AddRun(ctx, store.NewRun{Task: task, Tier: "coder", Job: "pure_function", Model: "c", Outcome: control.RunOutcomePassed}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.store.AddAudit(ctx, "control", "start", "", ""); err != nil {
		t.Fatal(err)
	}

	_, _, body := r.get(t, "/api/v1/runs", token)
	runs := body["runs"].([]any)
	if len(runs) != 3 || runs[0].(map[string]any)["id"] != float64(3) {
		t.Fatalf("runs = %v", runs)
	}
	run := runs[0].(map[string]any)
	if run["started_at"] != "2026-10-17T12:00:00Z" || run["outcome"] != "passed" || run["job"] != "pure_function" {
		t.Errorf("run = %v", run)
	}
	if _, _, body := r.get(t, "/api/v1/runs?task=1", token); len(body["runs"].([]any)) != 2 {
		t.Errorf("runs of task 1 = %v", body)
	}
	if _, _, body := r.get(t, "/api/v1/runs?limit=1", token); len(body["runs"].([]any)) != 1 {
		t.Errorf("runs limit 1 = %v", body)
	}
	if _, _, body := r.get(t, "/api/v1/runs?task=9", token); body["runs"] == nil || len(body["runs"].([]any)) != 0 {
		t.Errorf("no runs must be [] not null: %v", body)
	}
	_, _, body = r.get(t, "/api/v1/audit", token)
	entries := body["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["action"] != "start" {
		t.Errorf("audit = %v", body)
	}
}

// openStream opens the stream and returns a channel of its lines.
func openStream(t *testing.T, r rig, lastID string) (*http.Response, <-chan string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", r.url+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return resp, lines
}

func next(t *testing.T, lines <-chan string) string {
	t.Helper()
	select {
	case l, ok := <-lines:
		if !ok {
			t.Fatal("stream ended")
		}
		return l
	case <-time.After(3 * time.Second):
		t.Fatal("no line within 3 s")
	}
	return ""
}

func TestEventStreamDeliversWrites(t *testing.T) {
	r := newRig(t, time.Hour)
	// An event from before the connection is not replayed to a client that
	// did not ask to resume.
	if _, err := r.store.AddAudit(context.Background(), "control", "earlier", "", ""); err != nil {
		t.Fatal(err)
	}
	resp, lines := openStream(t, r, "")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("stream: %d %v", resp.StatusCode, resp.Header)
	}
	if l := next(t, lines); l != ": connected" {
		t.Fatalf("first line = %q", l)
	}
	next(t, lines) // the blank line ending it

	if _, err := r.store.AddAudit(context.Background(), "control", "start", "plane", ""); err != nil {
		t.Fatal(err)
	}
	if l := next(t, lines); l != "id: 2" {
		t.Errorf("line = %q", l)
	}
	if l := next(t, lines); l != "event: audit" {
		t.Errorf("line = %q", l)
	}
	l := next(t, lines)
	if !strings.HasPrefix(l, "data: ") {
		t.Fatalf("line = %q", l)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(l, "data: ")), &data); err != nil || data["action"] != "start" || data["at"] != "2026-10-17T12:00:00Z" {
		t.Errorf("data = %v, %v", data, err)
	}
	if l := next(t, lines); l != "" {
		t.Errorf("event not terminated by a blank line: %q", l)
	}
}

func TestEventStreamResumesAfterLastEventID(t *testing.T) {
	r := newRig(t, time.Hour)
	ctx := context.Background()
	r.store.AddAudit(ctx, "a", "one", "", "")
	r.store.AddAudit(ctx, "a", "two", "", "")
	r.store.AddRun(ctx, store.NewRun{Task: 1, Tier: "t", Job: "j", Model: "m", Outcome: control.RunOutcomeRunning})

	_, lines := openStream(t, r, "1")
	next(t, lines)
	next(t, lines)
	got := []string{next(t, lines), next(t, lines)}
	if got[0] != "id: 2" || got[1] != "event: audit" {
		t.Errorf("first replayed = %v", got)
	}
	next(t, lines)
	next(t, lines)
	if l := next(t, lines); l != "id: 3" {
		t.Errorf("second replayed = %q", l)
	}
	if l := next(t, lines); l != "event: run" {
		t.Errorf("line = %q", l)
	}
}

func TestEventStreamRefusesABadLastEventID(t *testing.T) {
	r := newRig(t, 0)
	for _, id := range []string{"abc", "-1", "1.5"} {
		req, _ := http.NewRequest("GET", r.url+"/api/v1/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Last-Event-ID", id)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("Last-Event-ID %q: %d", id, resp.StatusCode)
		}
	}
}

func TestEventStreamHeartbeat(t *testing.T) {
	r := newRig(t, 20*time.Millisecond)
	_, lines := openStream(t, r, "")
	next(t, lines)
	next(t, lines)
	if l := next(t, lines); l != ": ping" {
		t.Errorf("idle line = %q", l)
	}
}
