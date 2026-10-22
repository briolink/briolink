package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/port/reasoner"
)

var models = map[reasoner.Tier]string{reasoner.Routine: "sonnet", reasoner.Top: "opus"}

// call is what a fake Run saw.
type call struct {
	args  []string
	stdin string
	dir   string
	env   []string
}

// fake returns a Reasoner whose Run prints out (and fails with runErr), and
// records its calls.
func fake(out string, runErr error) (*Reasoner, *[]call) {
	var calls []call
	return &Reasoner{
		Models: models,
		Run: func(_ context.Context, _ string, args []string, stdin, dir string, env []string) ([]byte, error) {
			calls = append(calls, call{args, stdin, dir, env})
			return []byte(out), runErr
		},
		Usage: func() ([]Window, error) { return nil, nil },
	}, &calls
}

const okJSON = `{"type":"result","subtype":"success","is_error":false,"result":"{\"n\":2}","structured_output":{"n":2},` +
	`"session_id":"s-1","total_cost_usd":0.03,"modelUsage":{"claude-sonnet-5-5":{"costUSD":0.03},"claude-haiku-5-5":{"costUSD":0.001}}}`

func TestContract(t *testing.T) {
	reasoner.Contract(t, func(*testing.T) reasoner.Reasoner { r, _ := fake(okJSON, nil); return r },
		[]reasoner.Tier{reasoner.Routine, reasoner.Top}, "give n=2")
}

func TestCallShape(t *testing.T) {
	r, calls := fake(okJSON, nil)
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-secret")
	schema := json.RawMessage(`{"type":"object"}`)
	resp, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Top, Prompt: "p", Schema: schema, MaxUSD: 2})
	if err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	for _, want := range []string{"-p", "--tools", "--no-session-persistence", "--setting-sources", "--json-schema", `{"type":"object"}`, "--max-budget-usd", "2", "opus"} {
		if !slices.Contains(c.args, want) {
			t.Errorf("args lack %q: %q", want, c.args)
		}
	}
	if c.stdin != "p" {
		t.Errorf("stdin %q: with a schema the prompt goes unchanged, on stdin", c.stdin)
	}
	if slices.ContainsFunc(c.args, func(a string) bool { return a == "p" }) {
		t.Error("the prompt must not be an argument")
	}
	if c.dir == "" {
		t.Error("no working directory")
	}
	for _, kv := range c.env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") || strings.HasPrefix(kv, "ANTHROPIC_AUTH_TOKEN=") {
			t.Errorf("credential in env: %s", strings.SplitN(kv, "=", 2)[0])
		}
	}
	if string(resp.Output) != `{"n":2}` || resp.Tier != reasoner.Top || resp.Model != "claude-sonnet-5-5" ||
		resp.CostUSD != 0.03 || resp.SessionID != "s-1" {
		t.Errorf("%+v", resp)
	}
}

func TestNoSchemaReadsResultText(t *testing.T) {
	for name, text := range map[string]string{
		"bare":   `{\"a\":1}`,
		"fenced": "```json\\n{\\\"a\\\":1}\\n```",
		"array":  `[1]`,
	} {
		t.Run(name, func(t *testing.T) {
			r, calls := fake(`{"subtype":"success","result":"`+text+`","total_cost_usd":0}`, nil)
			resp, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p"})
			if err != nil || !json.Valid(resp.Output) {
				t.Fatalf("%q %v", resp.Output, err)
			}
			if !strings.Contains((*calls)[0].stdin, "one JSON value") || slices.Contains((*calls)[0].args, "--json-schema") {
				t.Errorf("%+v", (*calls)[0])
			}
			if resp.Model != "sonnet" {
				t.Errorf("model %q: falls back to the configured one", resp.Model)
			}
		})
	}
	r, _ := fake(`{"subtype":"success","result":"sorry, no"}`, nil)
	if _, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p"}); !errors.Is(err, ErrOutput) {
		t.Errorf("got %v", err)
	}
}

func TestBudget(t *testing.T) {
	t.Run("stopped by the cap", func(t *testing.T) {
		r, _ := fake(`{"subtype":"error_max_budget_usd","is_error":true,"total_cost_usd":0.05}`, errors.New("exit status 1"))
		_, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p", MaxUSD: 0.001})
		if !errors.Is(err, reasoner.ErrBudget) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("overshot by the last turn", func(t *testing.T) {
		r, _ := fake(okJSON, nil)
		_, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p", MaxUSD: 0.01})
		if !errors.Is(err, reasoner.ErrBudget) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("no cap, no flag", func(t *testing.T) {
		r, calls := fake(okJSON, nil)
		if _, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p"}); err != nil {
			t.Fatal(err)
		}
		if slices.Contains((*calls)[0].args, "--max-budget-usd") {
			t.Error("flag set without a cap")
		}
	})
	t.Run("negative cap", func(t *testing.T) {
		r, _ := fake(okJSON, nil)
		if _, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p", MaxUSD: -1}); !errors.Is(err, reasoner.ErrInvalid) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestRunFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		out string
		err error
	}{
		"binary missing":  {"", errors.New("exec: not found")},
		"garbage":         {"oops", nil},
		"error result":    {`{"subtype":"error_during_execution","is_error":true,"result":"Not logged in"}`, errors.New("exit status 1")},
		"is_error always": {`{"subtype":"success","is_error":true,"result":"x"}`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := fake(tc.out, tc.err)
			if _, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p"}); !errors.Is(err, ErrRun) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestQueue(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	var slept []time.Duration
	var polls int
	r, calls := fake(okJSON, nil)
	r.Now = func() time.Time { return now }
	r.Usage = func() ([]Window, error) {
		polls++
		if polls == 1 {
			return []Window{
				{Utilization: 100, ResetsAt: now.Add(30 * time.Minute)},
				{Utilization: 100, ResetsAt: now.Add(2 * time.Hour)},
				{Utilization: 100, ResetsAt: now.Add(-time.Hour)}, // stale: already reset
				{Utilization: 10, ResetsAt: now.Add(3 * time.Hour)},
			}, nil
		}
		return []Window{{Utilization: 4}}, nil
	}
	r.Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	if _, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(slept, []time.Duration{2 * time.Hour}) || polls != 2 || len(*calls) != 1 {
		t.Errorf("slept %v, polls %d, calls %d", slept, polls, len(*calls))
	}

	t.Run("cancelled while queued", func(t *testing.T) {
		r, calls := fake(okJSON, nil)
		r.Usage = func() ([]Window, error) { return []Window{{Utilization: 100}}, nil }
		ctx, cancel := context.WithCancel(context.Background())
		r.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
		if _, err := r.Reason(ctx, reasoner.Request{Tier: reasoner.Routine, Prompt: "p"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
		if len(*calls) != 0 {
			t.Error("called while queued")
		}
	})
	t.Run("custom limit", func(t *testing.T) {
		r, calls := fake(okJSON, nil)
		r.Limit = 90
		r.Usage = func() ([]Window, error) { return []Window{{Utilization: 95}}, nil }
		r.Sleep = func(ctx context.Context, _ time.Duration) error { return context.DeadlineExceeded }
		if _, err := r.Reason(context.Background(), reasoner.Request{Tier: reasoner.Routine, Prompt: "p"}); !errors.Is(err, context.DeadlineExceeded) || len(*calls) != 0 {
			t.Fatalf("got %v", err)
		}
	})
}

func TestReadUsage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "claude.json")
	os.WriteFile(p, []byte(`{"oauthAccount":{"x":"secret"},"cachedUsageUtilization":{"utilization":{
		"five_hour":{"utilization":4,"resets_at":"2026-10-22T02:30:00.044493+00:00"},
		"seven_day":{"utilization":15,"resets_at":"bad"},"seven_day_opus":null,"extra":true}}}`), 0o600)
	ws, err := ReadUsage(p)
	if err != nil || len(ws) != 2 || ws[0].Utilization != 4 || ws[0].ResetsAt.IsZero() || ws[1].Utilization != 15 || !ws[1].ResetsAt.IsZero() {
		t.Fatalf("%+v %v", ws, err)
	}
	if ws, err := ReadUsage(filepath.Join(t.TempDir(), "none")); !errors.Is(err, ErrRun) || ws != nil {
		t.Errorf("missing file: %v", err)
	}
	os.WriteFile(p, []byte(`{}`), 0o600)
	if ws, err := ReadUsage(p); err != nil || len(ws) != 0 {
		t.Errorf("no cache: %v %v", ws, err)
	}
}

func TestExecRun(t *testing.T) {
	out, err := execRun(context.Background(), "sh", []string{"-c", "cat; exit 3"}, "hello", t.TempDir(), os.Environ())
	if string(out) != "hello" || err == nil {
		t.Errorf("%q %v", out, err)
	}
}
