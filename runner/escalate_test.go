package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/port/codehost"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/port/reasoner"
)

// pushWS is a fake workspace that can push: it makes the branch on the fake host.
type pushWS struct {
	*fakeWS
	host   *codehost.Fake
	pushes []string // branch + ": " + message
	paths  []string
	err    error
}

func (w *pushWS) Push(ctx context.Context, branch, message string, paths []string) error {
	if w.err != nil {
		return w.err
	}
	w.pushes = append(w.pushes, branch+": "+message)
	w.paths = paths
	return w.host.CreateBranch(ctx, branch, "main")
}

type oneWS struct{ ws Workspace }

func (o oneWS) Create(context.Context, string) (Workspace, error) { return o.ws, nil }

// badFails makes go test fail for any answer that contains "bad".
func badFails(argv []string, files map[string]string) Output {
	if argv[0] == "go" && argv[1] == "test" && strings.Contains(files[target], "bad") {
		return Output{Code: 1, Text: "--- FAIL: TestSettles (0.00s)\n--- FAIL: TestZero (0.00s)\nFAIL"}
	}
	return Output{}
}

func reply(diagnosis string, files map[string]string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"diagnosis": diagnosis, "files": files})
	return b
}

type escRig struct {
	r    *Runner
	ws   *pushWS
	host *codehost.Fake
	rsn  *reasoner.Fake
	rec  *fakeRecords
}

const bad = "package acme // bad\n"
const good = "package acme\n"

// escalating is a runner whose coder always answers a failing function; first
// and second are what the reasoner's tiers answer. Issue 1 is the task's.
func escalating(t *testing.T, first, second json.RawMessage, cost float64) *escRig {
	t.Helper()
	host := codehost.NewFake()
	if _, err := host.CreateIssue(context.Background(), codehost.NewIssue{Title: "task", Labels: []string{"state:queued"}}); err != nil {
		t.Fatal(err)
	}
	ws := &pushWS{fakeWS: newWS(badFails), host: host}
	cd := &scripted{answers: []func() (coder.Response, error){answer("qwen-real", map[string]string{target: bad})}}
	r, rec := setup(ws.fakeWS, cd)
	r.Workspaces = oneWS{ws}
	r.Routing.Jobs["escalation.first"], r.Routing.Jobs["escalation.second"] = "routine", "top"
	rsn := &reasoner.Fake{Tiers: map[reasoner.Tier]json.RawMessage{reasoner.Routine: first, reasoner.Top: second}, Cost: cost}
	r.Reasoner, r.Host = rsn, host
	r.History = func(n int64) string { return "https://control.example/api/v1/tasks/1/runs" }
	return &escRig{r, ws, host, rsn, rec}
}

func (e *escRig) comments(t *testing.T) []string {
	t.Helper()
	cs, err := e.host.ListComments(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Body)
	}
	return out
}

func TestFirstAnswerPassesAndOpensThePullRequest(t *testing.T) {
	e := escalating(t, reply("the test was right, the coder missed the zero case", map[string]string{target: good}), nil, 0.3)
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || len(res.Attempts) != 3 || res.Attempts[2].Tier != "routine" || res.HandOff != "" {
		t.Fatalf("%+v", res)
	}
	if calls := e.rsn.Calls(); len(calls) != 1 || calls[0].Tier != reasoner.Routine || calls[0].MaxUSD != 1 {
		t.Fatalf("calls %+v", calls)
	}
	if ws := e.ws.files[target]; ws != good {
		t.Errorf("answer not applied: %q", ws)
	}

	// The coder's last attempt is handed up; the reasoner's answer passed.
	var got []string
	for _, r := range e.rec.runs {
		got = append(got, r.Tier+"/"+r.Job+"/"+string(r.Outcome))
	}
	want := "coder/pure_function/failed|coder/pure_function/escalated|routine/escalation.first/passed"
	if strings.Join(got, "|") != want {
		t.Errorf("records %v", got)
	}

	// Pushed on a branch of the task, then a pull request that closes the issue, as the bot's host.
	if len(e.ws.pushes) != 1 || !strings.HasPrefix(e.ws.pushes[0], "task/1-acme-0001: acme-0001: settles_balance") || strings.Join(e.ws.paths, ",") != target {
		t.Errorf("pushes %q %q", e.ws.pushes, e.ws.paths)
	}
	pr, err := e.host.GetPullRequest(context.Background(), res.PullRequest)
	if err != nil || pr.Head != "task/1-acme-0001" || pr.Base != "main" || !strings.HasSuffix(pr.Body, "Closes #1") {
		t.Fatalf("%+v %v", pr, err)
	}
	cs := e.comments(t)
	if len(cs) != 1 || !strings.Contains(cs[0], "Pull request #") || !strings.Contains(cs[0], "the coder missed the zero case") {
		t.Errorf("comments %q", cs)
	}
	for _, text := range append(cs, pr.Body) {
		for _, banned := range []string{"$", "USD", "cost", "0.3"} {
			if strings.Contains(text, banned) {
				t.Errorf("%q has a cost figure (%q)", text, banned)
			}
		}
	}
}

func TestSecondAnswerAtTheTopTier(t *testing.T) {
	e := escalating(t,
		reply("first guess", map[string]string{target: bad}),
		reply("second guess", map[string]string{target: good}), 0.2)
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || len(res.Attempts) != 4 || res.Attempts[2].Tier != "routine" || res.Attempts[3].Tier != "top" || res.SpentUSD != 0.4 {
		t.Fatalf("%+v", res)
	}
	var got []string
	for _, r := range e.rec.runs[2:] {
		got = append(got, r.Tier+"/"+r.Job+"/"+string(r.Outcome))
	}
	if strings.Join(got, "|") != "routine/escalation.first/escalated|top/escalation.second/passed" {
		t.Errorf("records %v", got)
	}

	// The second answer sees the first as the last attempt, and every earlier
	// attempt's failing test names, not their output.
	calls := e.rsn.Calls()
	if len(calls) != 2 || calls[1].MaxUSD != 0.8 {
		t.Fatalf("calls %+v", calls)
	}
	p := calls[1].Prompt
	for _, want := range []string{"# Task contract", "# Model slice", "# Last attempt (routine, check: test)", "package acme // bad", "- attempt 1 (coder): failing tests TestSettles, TestZero", "- attempt 2 (coder): failing tests"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "FAIL\n\n# Earlier") || strings.Count(p, "--- FAIL") != 2 {
		t.Errorf("earlier attempts' output is in the prompt:\n%s", p)
	}
}

func TestHandOffToTheFounder(t *testing.T) {
	e := escalating(t,
		reply("first guess", map[string]string{target: bad}),
		reply("it needs a model change", map[string]string{}), 0.1)
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.PullRequest != 0 || !strings.Contains(res.HandOff, "did not pass") || len(e.ws.pushes) != 0 {
		t.Fatalf("%+v", res)
	}
	last := res.Attempts[len(res.Attempts)-1]
	if last.Check != "reasoner" || last.Diagnosis != "it needs a model change" {
		t.Errorf("%+v", last)
	}
	is, _ := e.host.GetIssue(context.Background(), 1)
	if strings.Join(is.Labels, ",") != "state:needs-founder" {
		t.Errorf("labels %v", is.Labels)
	}
	cs := e.comments(t)
	if len(cs) != 1 || !strings.Contains(cs[0], "Handed to the founder") || !strings.Contains(cs[0], "https://control.example/api/v1/tasks/1/runs") || !strings.Contains(cs[0], "it needs a model change") {
		t.Errorf("comments %q", cs)
	}
}

func TestSpentReasonerBudget(t *testing.T) {
	// 0.6 a call against 1 USD: the second call would pass the budget.
	e := escalating(t, reply("g1", map[string]string{target: bad}), reply("g2", map[string]string{target: good}), 0.6)
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || !strings.Contains(res.HandOff, "budget") || len(e.rsn.Calls()) != 1 {
		t.Fatalf("%+v calls %d", res, len(e.rsn.Calls()))
	}
	is, _ := e.host.GetIssue(context.Background(), 1)
	if len(is.Labels) != 1 || is.Labels[0] != "state:needs-founder" {
		t.Errorf("labels %v", is.Labels)
	}
	// The last recorded attempt is the reasoner's, handed on.
	if got := e.rec.runs[len(e.rec.runs)-1]; got.Job != "escalation.first" || got.Outcome != control.RunOutcomeEscalated {
		t.Errorf("%+v", got)
	}

	// No reasoner_usd at all: the reasoner is never called.
	e = escalating(t, reply("g1", map[string]string{target: good}), nil, 0.1)
	ct := testContract(1)
	ct.Budget.ReasonerUSD = 0
	res, err = e.r.Run(context.Background(), 1, ct)
	if err != nil || res.Passed || len(e.rsn.Calls()) != 0 || !strings.Contains(res.HandOff, "budget") {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestReasonerAnswersAreHeldToTheSameRules(t *testing.T) {
	e := escalating(t,
		reply("rewrites the test", map[string]string{"domain/acme/settles_balance_test.go": good}),
		reply("not json shaped", map[string]string{target: good}), 0.1)
	e.rsn.Tiers[reasoner.Top] = json.RawMessage(`"just text"`)
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Fatalf("%+v", res)
	}
	a, b := res.Attempts[2], res.Attempts[3]
	if a.Check != "scope" || b.Check != "reasoner" || !strings.Contains(b.Output, "not {diagnosis, files}") {
		t.Errorf("%+v %+v", a, b)
	}
	if e.ws.files["domain/acme/settles_balance_test.go"] != "package acme\n\nfunc TestX() {}\n" {
		t.Error("a file outside allowed_files was written")
	}
}

func TestReasonerCallFailsAndCancels(t *testing.T) {
	e := escalating(t, nil, nil, 0)
	e.r.Reasoner = &reasoner.Fake{} // no tier is known: every call errors
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil || res.Passed || res.Attempts[2].Check != "reasoner" || !strings.Contains(res.Attempts[2].Output, "unknown tier") {
		t.Fatalf("%v %+v", err, res)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e = escalating(t, reply("x", map[string]string{target: good}), nil, 0)
	e.r.Reasoner = reasonerFunc(func(context.Context, reasoner.Request) (reasoner.Response, error) {
		cancel()
		return reasoner.Response{}, ctx.Err()
	})
	if _, err := e.r.Run(ctx, 1, testContract(1)); !errors.Is(err, context.Canceled) {
		t.Errorf("%v", err)
	}
}

type reasonerFunc func(context.Context, reasoner.Request) (reasoner.Response, error)

func (f reasonerFunc) Reason(ctx context.Context, r reasoner.Request) (reasoner.Response, error) {
	return f(ctx, r)
}

func TestRoutingNeedsBothEscalationRows(t *testing.T) {
	e := escalating(t, nil, nil, 0)
	delete(e.r.Routing.Jobs, "escalation.second")
	if _, err := e.r.Run(context.Background(), 1, testContract(1)); err == nil || !strings.Contains(err.Error(), "escalation.second") {
		t.Errorf("%v", err)
	}
}

func TestPassOnTheCoderStillOpensThePullRequest(t *testing.T) {
	e := escalating(t, nil, nil, 0)
	e.r.Coder = &scripted{answers: []func() (coder.Response, error){answer("qwen-real", map[string]string{target: good})}}
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil || !res.Passed || res.PullRequest == 0 || len(e.rsn.Calls()) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestPassWithoutAPusherIsAnError(t *testing.T) {
	e := escalating(t, nil, nil, 0)
	e.r.Workspaces = &fakeWorkspaces{ws: e.ws.fakeWS}
	e.r.Coder = &scripted{answers: []func() (coder.Response, error){answer("m", map[string]string{target: good})}}
	if _, err := e.r.Run(context.Background(), 1, testContract(1)); err == nil || !strings.Contains(err.Error(), "cannot push") {
		t.Errorf("%v", err)
	}
}

func TestNoReasonerHandsOverDirectly(t *testing.T) {
	e := escalating(t, nil, nil, 0)
	e.r.Reasoner = nil
	res, err := e.r.Run(context.Background(), 1, testContract(1))
	if err != nil || res.Passed || !strings.Contains(res.HandOff, "no reasoner") {
		t.Fatalf("%v %+v", err, res)
	}
	is, _ := e.host.GetIssue(context.Background(), 1)
	if len(is.Labels) != 1 || is.Labels[0] != "state:needs-founder" {
		t.Errorf("labels %v", is.Labels)
	}
	// Without a reasoner the last attempt stays failed, not escalated.
	if got := e.rec.runs[len(e.rec.runs)-1].Outcome; got != control.RunOutcomeFailed {
		t.Errorf("outcome %v", got)
	}
}

func TestFailingTests(t *testing.T) {
	out := "=== RUN   TestA\n--- FAIL: TestB (0.00s)\n    --- FAIL: TestB/sub (0.00s)\n--- FAIL: TestA (0.01s)\n--- FAIL: TestA (0.01s)\nFAIL"
	if got := strings.Join(failingTests(out), ","); got != "TestA,TestB,TestB/sub" {
		t.Error(got)
	}
	if got := failingTests("mutant survived"); len(got) != 0 {
		t.Error(got)
	}
}

// TestPushToARemote runs the real git: the commit holds only the given
// files and lands as the named branch of a bare remote, and a second push
// replaces it.
func TestPushToARemote(t *testing.T) {
	src := repo(t)
	remote := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	git(t, src, "remote", "add", "origin", remote)
	c := &Container{Repo: src, Dir: t.TempDir(), Image: "img", GitName: "briolink-bot", GitEmail: "bot@example.com", Cmd: hostDocker(t, new([][]string))}

	push := func(content string) {
		t.Helper()
		ws, err := c.Create(context.Background(), "push")
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		if err := ws.Write("domain/acme/settles.go", []byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := ws.Write("domain/acme/stray.go", []byte("package acme\n")); err != nil {
			t.Fatal(err)
		}
		if err := ws.(Pusher).Push(context.Background(), "task/1-x", "subject\n\nbody", []string{"domain/acme/settles.go"}); err != nil {
			t.Fatal(err)
		}
	}
	push("package acme // one\n")
	push("package acme // two\n")

	show := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", remote}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	if got := show("show", "task/1-x:domain/acme/settles.go"); got != "package acme // two\n" {
		t.Errorf("remote file %q", got)
	}
	if got := strings.TrimSpace(show("show", "-s", "--format=%an <%ae>|%s", "task/1-x")); got != "briolink-bot <bot@example.com>|subject" {
		t.Errorf("commit %q", got)
	}
	if got := strings.TrimSpace(show("ls-tree", "-r", "--name-only", "task/1-x")); strings.Contains(got, "stray.go") {
		t.Errorf("an unlisted file was committed: %s", got)
	}
	if _, err := os.Stat(filepath.Join(src, "domain/acme/stray.go")); err == nil {
		t.Error("stray file reached the repository")
	}
}
