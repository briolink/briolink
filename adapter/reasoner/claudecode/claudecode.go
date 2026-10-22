// Package claudecode is the Reasoner port on headless Claude Code (E15), on
// the founder's subscription login, never an API key. Deep: it relies on
// `claude -p` with JSON output, a JSON schema, a budget cap and no tools (the
// feature list, fallback and exit are in docs/4-choices.md § Reasoner).
package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/briolink/briolink/port/reasoner"
)

var (
	// ErrRun: Claude Code failed, or answered with an error. The subtype and a
	// clipped message follow it.
	ErrRun = errors.New("claudecode: run failed")
	// ErrOutput: Claude Code answered, but not with one JSON value.
	ErrOutput = errors.New("claudecode: output is not JSON")
)

// DefaultLimit is the utilization percentage at which a window counts as full.
const DefaultLimit = 100

// Window is one of the plan's usage windows, as Claude Code caches it.
type Window struct {
	Utilization float64   // percent of the window used
	ResetsAt    time.Time // when it empties; zero when unknown
}

// Reasoner is the port on the local `claude` binary.
type Reasoner struct {
	// Models maps each tier to a model alias or ID (the routing table's
	// `routine` and `top`); a tier absent here is ErrUnknownTier.
	Models map[reasoner.Tier]string
	Binary string // "claude" when empty
	// Dir is where Claude Code runs; empty means an empty temporary directory
	// per call, so no repo settings, hooks or files reach the model.
	Dir string
	// Limit is the percentage at which a call queues; DefaultLimit when zero.
	Limit float64

	// Run, Usage, Now and Sleep are seams for tests; the defaults run the
	// binary, read ~/.claude.json, and use the clock.
	Run   func(ctx context.Context, bin string, args []string, stdin, dir string, env []string) ([]byte, error)
	Usage func() ([]Window, error)
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// result is the part of `--output-format json` this adapter reads.
type result struct {
	IsError          bool                       `json:"is_error"`
	Subtype          string                     `json:"subtype"`
	Result           string                     `json:"result"`
	StructuredOutput json.RawMessage            `json:"structured_output"`
	SessionID        string                     `json:"session_id"`
	TotalCostUSD     float64                    `json:"total_cost_usd"`
	ModelUsage       map[string]json.RawMessage `json:"modelUsage"`
}

// Reason queues while a usage window is full, then makes one `claude -p` call.
// CostUSD is Claude Code's list-price estimate, not a bill on a flat plan.
func (r *Reasoner) Reason(ctx context.Context, req reasoner.Request) (reasoner.Response, error) {
	if err := ctx.Err(); err != nil {
		return reasoner.Response{}, err
	}
	if strings.TrimSpace(req.Prompt) == "" || req.MaxUSD < 0 ||
		(len(req.Schema) > 0 && !json.Valid(req.Schema)) {
		return reasoner.Response{}, reasoner.ErrInvalid
	}
	model, ok := r.Models[req.Tier]
	if !ok || model == "" {
		return reasoner.Response{}, reasoner.ErrUnknownTier
	}
	if err := r.queue(ctx); err != nil {
		return reasoner.Response{}, err
	}

	prompt := req.Prompt
	args := []string{"-p", "--output-format", "json", "--model", model,
		"--tools", "", "--no-session-persistence", "--disable-slash-commands", "--setting-sources", ""}
	if len(req.Schema) > 0 {
		args = append(args, "--json-schema", string(req.Schema))
	} else {
		prompt += "\n\nAnswer with one JSON value and nothing else."
	}
	if req.MaxUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprint(req.MaxUSD))
	}
	dir := r.Dir
	if dir == "" {
		tmp, err := os.MkdirTemp("", "claudecode-")
		if err != nil {
			return reasoner.Response{}, err
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	}
	run := r.Run
	if run == nil {
		run = execRun
	}
	bin := r.Binary
	if bin == "" {
		bin = "claude"
	}
	out, runErr := run(ctx, bin, args, prompt, dir, env())
	if err := ctx.Err(); err != nil {
		return reasoner.Response{}, err
	}

	// A run that stops on its budget exits non-zero but still prints its
	// result, so the output is read before the exit status is judged.
	var res result
	if err := json.Unmarshal(bytes.TrimSpace(out), &res); err != nil {
		if runErr != nil {
			return reasoner.Response{}, fmt.Errorf("%w: %v", ErrRun, runErr)
		}
		return reasoner.Response{}, fmt.Errorf("%w: reply is not JSON", ErrRun)
	}
	if res.Subtype == "error_max_budget_usd" {
		return reasoner.Response{}, reasoner.ErrBudget
	}
	if res.IsError || res.Subtype != "success" {
		return reasoner.Response{}, fmt.Errorf("%w: %s: %s", ErrRun, res.Subtype, clip(res.Result))
	}
	if req.MaxUSD > 0 && res.TotalCostUSD > req.MaxUSD {
		return reasoner.Response{}, reasoner.ErrBudget
	}
	output, err := res.output(len(req.Schema) > 0)
	if err != nil {
		return reasoner.Response{}, err
	}
	if res.TotalCostUSD < 0 {
		res.TotalCostUSD = 0
	}
	return reasoner.Response{
		Output:    output,
		Tier:      req.Tier,
		Model:     res.model(model),
		CostUSD:   res.TotalCostUSD,
		SessionID: res.SessionID,
	}, nil
}

// output is the structured output when a schema was given, else the result
// text read as one JSON value (a fenced block is unwrapped).
func (res result) output(schema bool) (json.RawMessage, error) {
	if schema && len(res.StructuredOutput) > 0 && string(res.StructuredOutput) != "null" {
		return res.StructuredOutput, nil
	}
	text := strings.TrimSpace(res.Result)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimPrefix(text, "json")
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "```"))
	}
	if !json.Valid([]byte(text)) {
		return nil, ErrOutput
	}
	return json.RawMessage(text), nil
}

// model names the model that answered: the one with the highest cost in
// modelUsage, else the configured one.
func (res result) model(configured string) string {
	best, top := "", -1.0
	for name, raw := range res.ModelUsage {
		var u struct {
			CostUSD float64 `json:"costUSD"`
		}
		json.Unmarshal(raw, &u)
		if u.CostUSD > top || (u.CostUSD == top && name < best) {
			best, top = name, u.CostUSD
		}
	}
	if best == "" {
		return configured
	}
	return best
}

// queue waits while a window is full, until the latest such window resets, then
// asks again: Claude Code refreshes its cache on every call, so a window that
// is past its reset is treated as empty. A window with no reset time is polled
// every minute. Behaviour at a real plan limit is not yet measured (#76).
func (r *Reasoner) queue(ctx context.Context) error {
	usage := r.Usage
	if usage == nil {
		usage = func() ([]Window, error) { return ReadUsage("") }
	}
	limit := r.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	for {
		windows, err := usage()
		if err != nil {
			return err
		}
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		var wait time.Duration
		for _, w := range windows {
			if w.Utilization < limit {
				continue
			}
			d := time.Minute
			if !w.ResetsAt.IsZero() {
				if !w.ResetsAt.After(now) {
					continue
				}
				d = w.ResetsAt.Sub(now)
			}
			wait = max(wait, d)
		}
		if wait == 0 {
			return nil
		}
		sleep := r.Sleep
		if sleep == nil {
			sleep = sleepCtx
		}
		if err := sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// ReadUsage reads the two windows Claude Code caches in its state file (path
// empty: ~/.claude.json). Only those fields are decoded; nothing else in the
// file, and no credential, is read.
func ReadUsage(path string) ([]Window, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".claude.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: usage: %v", ErrRun, err)
	}
	var f struct {
		Cached struct {
			Utilization map[string]json.RawMessage `json:"utilization"`
		} `json:"cachedUsageUtilization"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%w: usage: not JSON", ErrRun)
	}
	var ws []Window
	for _, name := range []string{"five_hour", "seven_day"} {
		// Other entries have other shapes; only these two are decoded.
		var u struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}
		if json.Unmarshal(f.Cached.Utilization[name], &u) != nil {
			continue
		}
		w := Window{Utilization: u.Utilization}
		if t, err := time.Parse(time.RFC3339, u.ResetsAt); err == nil {
			w.ResetsAt = t
		}
		ws = append(ws, w)
	}
	return ws, nil
}

// env is the process environment without any API credential: this binding runs
// on the subscription login only (E15).
func env() []string {
	var out []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); k == "ANTHROPIC_API_KEY" || k == "ANTHROPIC_AUTH_TOKEN" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// execRun runs the binary with the prompt on stdin and returns its stdout. A
// non-zero exit comes back as an error beside whatever it printed.
func execRun(ctx context.Context, bin string, args []string, stdin, dir string, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.Bytes(), err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// clip keeps an error short.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
