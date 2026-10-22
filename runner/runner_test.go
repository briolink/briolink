package runner

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/runner/contract"
)

// fakeWS is an in-memory workspace; script decides what each command prints.
type fakeWS struct {
	mu     sync.Mutex
	files  map[string]string
	script func(argv []string, files map[string]string) Output
	execs  [][]string
	closed bool
}

func (w *fakeWS) Read(p string) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.files[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(s), nil
}
func (w *fakeWS) Write(p string, b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.files[p] = string(b)
	return nil
}
func (w *fakeWS) Remove(p string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.files, p)
	return nil
}
func (w *fakeWS) List(dir string) ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for p := range w.files {
		if path.Dir(p) == dir {
			out = append(out, path.Base(p))
		}
	}
	sort.Strings(out)
	return out, nil
}
func (w *fakeWS) Exec(_ context.Context, argv []string) (Output, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.execs = append(w.execs, argv)
	if w.script == nil {
		return Output{}, nil
	}
	return w.script(argv, w.files), nil
}
func (w *fakeWS) Close() error { w.closed = true; return nil }

type fakeWorkspaces struct {
	ws  *fakeWS
	err error
}

func (f *fakeWorkspaces) Create(_ context.Context, _ string) (Workspace, error) {
	return f.ws, f.err
}

type fakeRecords struct {
	runs []store.NewRun
	err  error
}

func (f *fakeRecords) AddRun(_ context.Context, in store.NewRun) (control.RunRecord, error) {
	f.runs = append(f.runs, in)
	return control.RunRecord{}, f.err
}

// scripted answers the n-th call with the n-th function; the last one repeats.
type scripted struct {
	answers []func() (coder.Response, error)
	calls   []coder.Request
	onCall  func()
}

func (s *scripted) Code(_ context.Context, req coder.Request) (coder.Response, error) {
	i := len(s.calls)
	s.calls = append(s.calls, req)
	if s.onCall != nil {
		s.onCall()
	}
	if i >= len(s.answers) {
		i = len(s.answers) - 1
	}
	return s.answers[i]()
}

func answer(model string, files map[string]string) func() (coder.Response, error) {
	return func() (coder.Response, error) { return coder.Response{Files: files, Model: model}, nil }
}

const target = "domain/acme/settles_balance.go"

func testContract(retries int) *contract.Contract {
	return &contract.Contract{
		Format: "v0", TaskID: "acme-0001", ProjectID: "acme", Objective: "settles_balance", Kind: "logic",
		SpecRefs:             []string{"spec/domain/acme.json"},
		AllowedFiles:         []string{target},
		AcceptanceCriteria:   []string{"tests pass"},
		VerificationCommands: [][]string{{"go", "test", "./domain/acme/..."}, {"mutate", "./domain/acme", "-func", "SettlesBalance"}},
		RiskLevel:            "low",
		Budget:               contract.Budget{CoderRetries: retries, Minutes: 30, ReasonerUSD: 1},
		ExpectedArtifacts:    []string{target},
	}
}

func table() *routing.Table {
	return &routing.Table{
		Format: "v0",
		Tiers:  map[string]string{"coder": "qwen-test", "routine": "r", "top": "t"},
		Jobs:   map[string]string{"pure_function": "coder"},
	}
}

func newWS(script func([]string, map[string]string) Output) *fakeWS {
	return &fakeWS{
		files: map[string]string{
			target:                                "package acme\n\n// SettlesBalance doc.\nfunc stub() { panic(\"not implemented\") }\n",
			"domain/acme/settles_balance_test.go": "package acme\n\nfunc TestX() {}\n",
			"domain/acme/notes.txt":               "not a test",
		},
		script: script,
	}
}

func setup(ws *fakeWS, c coder.Coder) (*Runner, *fakeRecords) {
	rec := &fakeRecords{}
	return &Runner{Coder: c, Workspaces: &fakeWorkspaces{ws: ws}, Records: rec, Routing: table(), Generate: []string{"generate", "-check"}}, rec
}

// passAll: every command succeeds.
func passAll([]string, map[string]string) Output { return Output{} }

// failing returns a script failing the command whose program (or "go vet",
// "go test") is kind, with text.
func failing(kind, text string, code int) func([]string, map[string]string) Output {
	return func(argv []string, _ map[string]string) Output {
		name := argv[0]
		if name == "go" {
			name += " " + argv[1]
		}
		if name == kind {
			return Output{Code: code, Text: text}
		}
		return Output{}
	}
}

func TestPassOnFirstAttempt(t *testing.T) {
	ws := newWS(passAll)
	cd := &scripted{answers: []func() (coder.Response, error){answer("qwen-real", map[string]string{target: "package acme\n"})}}
	r, rec := setup(ws, cd)
	res, err := r.Run(context.Background(), 60, testContract(3))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || len(res.Attempts) != 1 || res.Stop != "" {
		t.Fatalf("%+v", res)
	}
	if ws.files[target] != "package acme\n" {
		t.Errorf("answer not applied: %q", ws.files[target])
	}
	if !ws.closed {
		t.Error("workspace not closed")
	}
	want := []string{"gofmt -d " + target, "go vet ./domain/acme", "generate -check", "go test ./domain/acme/...", "mutate ./domain/acme -func SettlesBalance"}
	var got []string
	for _, e := range ws.execs {
		got = append(got, strings.Join(e, " "))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("checks %q, want %q", got, want)
	}
	if len(rec.runs) != 1 || rec.runs[0] != (store.NewRun{Task: 60, Tier: "coder", Job: "pure_function", Model: "qwen-real", Outcome: control.RunOutcomePassed}) {
		t.Errorf("records %+v", rec.runs)
	}
}

func TestPromptOrderAndRetry(t *testing.T) {
	ws := newWS(func(argv []string, files map[string]string) Output {
		if argv[0] == "go" && argv[1] == "test" && strings.Contains(files[target], "wrong") {
			return Output{Code: 1, Text: "--- FAIL: TestX\n    got false, want true\nFAIL"}
		}
		return Output{}
	})
	cd := &scripted{answers: []func() (coder.Response, error){
		answer("m1", map[string]string{target: "package acme // wrong\n"}),
		answer("m2", map[string]string{target: "package acme // right\n"}),
	}}
	r, rec := setup(ws, cd)
	res, err := r.Run(context.Background(), 60, testContract(3))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || len(res.Attempts) != 2 {
		t.Fatalf("%+v", res)
	}
	if a := res.Attempts[0]; a.Check != "test" || !strings.Contains(a.Output, "got false") {
		t.Errorf("attempt 1: %+v", a)
	}
	first, second := cd.calls[0].Parts, cd.calls[1].Parts
	if len(first) != 3 {
		t.Fatalf("first call has %d parts, want contract, files, tests", len(first))
	}
	for i, want := range []string{"# Task contract", "# Files you may write", "# Tests"} {
		if !strings.HasPrefix(first[i], want) {
			t.Errorf("part %d starts %q, want %q", i, first[i][:20], want)
		}
	}
	if !strings.Contains(first[1], "SettlesBalance doc.") || !strings.Contains(first[2], "func TestX()") || strings.Contains(first[2], "not a test") {
		t.Errorf("files and tests parts wrong:\n%s\n%s", first[1], first[2])
	}
	if len(second) != 4 || strings.Join(second[:3], "") != strings.Join(first, "") {
		t.Fatalf("retry must keep the same prefix and add the failure last: %d parts", len(second))
	}
	if !strings.HasPrefix(second[3], "# Last failure (check: test)") || !strings.Contains(second[3], "got false") {
		t.Errorf("failure part: %s", second[3])
	}
	if len(rec.runs) != 2 || rec.runs[0].Outcome != control.RunOutcomeFailed || rec.runs[0].Model != "m1" ||
		rec.runs[1].Outcome != control.RunOutcomePassed || rec.runs[1].Model != "m2" {
		t.Errorf("records %+v", rec.runs)
	}
}

func TestWriteOutsideAllowedFilesIsRejected(t *testing.T) {
	ws := newWS(passAll)
	cd := &scripted{answers: []func() (coder.Response, error){
		answer("m", map[string]string{target: "package acme // x\n", "gen/evil.go": "package gen\n"}),
		answer("m", map[string]string{target: "package acme // ok\n"}),
	}}
	r, _ := setup(ws, cd)
	res, err := r.Run(context.Background(), 60, testContract(2))
	if err != nil {
		t.Fatal(err)
	}
	a := res.Attempts[0]
	if a.Check != "scope" || !strings.Contains(a.Output, "gen/evil.go") || strings.Contains(a.Output, target) {
		t.Errorf("attempt 1: %+v", a)
	}
	if _, ok := ws.files["gen/evil.go"]; ok {
		t.Error("a file outside allowed_files was written")
	}
	if len(ws.execs) != 5 {
		t.Errorf("checks must not run on a rejected answer: %d commands for 2 attempts", len(ws.execs))
	}
	if !res.Passed {
		t.Errorf("second attempt should pass: %+v", res)
	}
}

func TestEachCheckFails(t *testing.T) {
	for _, tc := range []struct{ kind, script, text string }{
		{"gofmt", "gofmt", "domain/acme/settles_balance.go"},
		{"vet", "go vet", "vet: unreachable code"},
		{"generate", "generate", "gen/ is out of date"},
		{"test", "go test", "--- FAIL: TestX"},
		{"mutate", "mutate", "SURVIVED domain/acme/settles_balance.go:5:9"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			code := 1
			if tc.kind == "gofmt" {
				code = 0 // gofmt -d exits 0 and prints the diff
			}
			ws := newWS(failing(tc.script, tc.text, code))
			cd := &scripted{answers: []func() (coder.Response, error){answer("m", map[string]string{target: "package acme\n"})}}
			r, rec := setup(ws, cd)
			res, err := r.Run(context.Background(), 60, testContract(0))
			if err != nil {
				t.Fatal(err)
			}
			if res.Passed || res.Stop != StopAttempts || res.Attempts[0].Check != tc.kind || !strings.Contains(res.Attempts[0].Output, tc.text) {
				t.Errorf("%+v", res)
			}
			if rec.runs[0].Outcome != control.RunOutcomeFailed {
				t.Errorf("record %+v", rec.runs)
			}
			// Checks are in order: a failure stops the later ones.
			if last := strings.Join(ws.execs[len(ws.execs)-1], " "); !strings.HasPrefix(last, tc.script) {
				t.Errorf("last command %q, want it to be the failing %s", last, tc.kind)
			}
		})
	}
}

func TestEarlyStopWhenTwoAttemptsFailTheSameWay(t *testing.T) {
	n := 0
	ws := newWS(func(argv []string, _ map[string]string) Output {
		if argv[0] == "go" && argv[1] == "test" {
			n++
			return Output{Code: 1, Text: "--- FAIL: TestX (0.0" + string(rune('0'+n)) + "s)\nFAIL\tdomain/acme\t0.01" + string(rune('0'+n)) + "s"}
		}
		return Output{}
	})
	cd := &scripted{answers: []func() (coder.Response, error){answer("m", map[string]string{target: "package acme\n"})}}
	r, rec := setup(ws, cd)
	res, err := r.Run(context.Background(), 60, testContract(5))
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Stop != StopSame || len(res.Attempts) != 2 || len(rec.runs) != 2 {
		t.Fatalf("%+v, %d records", res, len(rec.runs))
	}
}

func TestDifferentFailuresKeepGoingUntilBudgetIsSpent(t *testing.T) {
	n := 0
	ws := newWS(func(argv []string, _ map[string]string) Output {
		if argv[0] == "go" && argv[1] == "test" {
			n++
			return Output{Code: 1, Text: "failure " + strings.Repeat("x", n)}
		}
		return Output{}
	})
	cd := &scripted{answers: []func() (coder.Response, error){answer("m", map[string]string{target: "package acme\n"})}}
	r, rec := setup(ws, cd)
	res, err := r.Run(context.Background(), 60, testContract(2))
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Stop != StopAttempts || len(res.Attempts) != 3 || len(rec.runs) != 3 {
		t.Fatalf("%+v, %d records", res, len(rec.runs))
	}
}

func TestMinutesBudgetSpent(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	ws := newWS(failing("go test", "boom", 1))
	cd := &scripted{answers: []func() (coder.Response, error){answer("m", map[string]string{target: "package acme\n"})}}
	cd.onCall = func() { now = now.Add(31 * time.Minute) }
	r, rec := setup(ws, cd)
	r.Now = func() time.Time { return now }
	res, err := r.Run(context.Background(), 60, testContract(5))
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Stop != StopMinutes || len(res.Attempts) != 1 || len(rec.runs) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestEachAttemptStartsFromTheOriginalFiles(t *testing.T) {
	c := testContract(1)
	c.AllowedFiles = []string{target, "domain/acme/helper.go"}
	c.ExpectedArtifacts = []string{target}
	ws := newWS(failing("go test", "no", 1))
	cd := &scripted{answers: []func() (coder.Response, error){
		answer("m", map[string]string{target: "package acme // 1\n", "domain/acme/helper.go": "package acme // helper\n"}),
		answer("m", map[string]string{target: "package acme // 2\n"}),
	}}
	r, _ := setup(ws, cd)
	if _, err := r.Run(context.Background(), 60, c); err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.files["domain/acme/helper.go"]; ok {
		t.Error("the first attempt's helper.go survived into the second")
	}
}

func TestCoderErrorIsAFailedAttempt(t *testing.T) {
	ws := newWS(passAll)
	cd := &scripted{answers: []func() (coder.Response, error){
		func() (coder.Response, error) { return coder.Response{}, errors.New("ollama: down") },
		answer("m", map[string]string{target: "package acme\n"}),
	}}
	r, rec := setup(ws, cd)
	res, err := r.Run(context.Background(), 60, testContract(1))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Attempts[0].Check != "coder" || rec.runs[0].Model != "qwen-test" {
		t.Errorf("%+v %+v", res, rec.runs)
	}
}

func TestCancelledIsRecordedAndReturned(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ws := newWS(passAll)
	cd := &scripted{answers: []func() (coder.Response, error){func() (coder.Response, error) {
		cancel()
		return coder.Response{}, ctx.Err()
	}}}
	r, rec := setup(ws, cd)
	res, err := r.Run(ctx, 60, testContract(3))
	if !errors.Is(err, context.Canceled) || res == nil || res.Passed {
		t.Fatalf("%v %+v", err, res)
	}
	if len(rec.runs) != 1 || rec.runs[0].Outcome != control.RunOutcomeCancelled {
		t.Errorf("%+v", rec.runs)
	}
}

func TestSetupErrors(t *testing.T) {
	ws := newWS(passAll)
	cd := &scripted{answers: []func() (coder.Response, error){answer("m", map[string]string{target: "package acme\n"})}}

	r, _ := setup(ws, cd)
	r.Routing.Jobs = map[string]string{}
	if _, err := r.Run(context.Background(), 60, testContract(0)); err == nil {
		t.Error("a routing table without the job must be refused")
	}

	r, _ = setup(ws, cd)
	r.Workspaces = &fakeWorkspaces{err: errors.New("no worktree")}
	if _, err := r.Run(context.Background(), 60, testContract(0)); err == nil || !strings.Contains(err.Error(), "no worktree") {
		t.Errorf("workspace error: %v", err)
	}

	r, rec := setup(ws, cd)
	rec.err = errors.New("db down")
	if _, err := r.Run(context.Background(), 60, testContract(0)); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Errorf("record error: %v", err)
	}
}

func TestTrim(t *testing.T) {
	if got := trim("short", 10); got != "short" {
		t.Error(got)
	}
	got := trim("héllo wörld", 2)
	if !strings.HasPrefix(got, "h") || !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("%q", got)
	}
}
