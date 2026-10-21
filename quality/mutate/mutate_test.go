package mutate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func descriptions(ms []Mutant) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Original+"->"+m.Replacement)
	}
	return out
}

func TestGenerateOperatorsBoundariesConstants(t *testing.T) {
	ms, err := Generate("testdata/weak", "settles_balance")
	if err != nil {
		t.Fatal(err)
	}
	// amount > 0 && amount == balance
	want := []string{">->>=", ">-><=", "0->1", "0->(-1)", "&&->||", "==->!="}
	got := descriptions(ms)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("missing mutant %s in %v", w, got)
		}
	}
	for _, m := range ms {
		if m.Func != "settles_balance" || m.Line != 6 || filepath.Base(m.File) != "balance.go" {
			t.Errorf("wrong position or name: %s", m)
		}
	}
}

func TestGenerateStringsAndAllFunctions(t *testing.T) {
	ms, err := Generate("testdata/strong", "label")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(descriptions(ms), " ")
	for _, w := range []string{`"payment"->""`, `""->"mutant"`, "<=-><", "<=->>"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %s in %s", w, got)
		}
	}
	all, err := Generate("testdata/strong", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) <= len(ms) {
		t.Errorf("all functions gave %d mutants, label alone %d", len(all), len(ms))
	}
}

func TestGenerateUnaryAndIncDec(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc f(ok bool, n int) int {\n\tif !ok {\n\t\tn++\n\t}\n\tn += 2\n\treturn -n\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := Generate(dir, "f")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(descriptions(ms), " ")
	for _, w := range []string{"!x->x", "-x->x", "++->--", "+=->-=", "2->3", "2->1"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %s in %s", w, got)
		}
	}
	// Every mutant must print as valid Go that differs from the original.
	for _, m := range ms {
		out, err := m.render()
		if err != nil {
			t.Errorf("%s: %v", m, err)
		}
		if string(out) == src {
			t.Errorf("%s left the source unchanged", m)
		}
	}
	// Rendering restores the tree: a second pass gives the same results.
	again, _ := ms[0].render()
	first, _ := ms[0].render()
	if string(again) != string(first) {
		t.Error("render is not repeatable")
	}
}

func TestGenerateUnknownFunction(t *testing.T) {
	if _, err := Generate("testdata/strong", "nope"); err == nil {
		t.Fatal("a function that does not exist must be an error")
	}
	if _, err := Generate("testdata/missing", ""); err == nil {
		t.Fatal("a missing directory must be an error")
	}
}

func TestRunStrongTestsKillEverything(t *testing.T) {
	rep, err := Run(context.Background(), Options{Dir: "./testdata/strong"})
	if err != nil {
		t.Fatal(err)
	}
	if s := rep.Survivors(); len(s) != 0 {
		t.Errorf("survivors: %v", s)
	}
	if len(rep.Results) == 0 || rep.Count(Killed) == 0 {
		t.Errorf("nothing was killed: %+v", rep.Results)
	}
}

func TestRunWeakTestsLeaveSurvivors(t *testing.T) {
	rep, err := Run(context.Background(), Options{Dir: "./testdata/weak", Func: "settles_balance"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, s := range rep.Survivors() {
		got[s.Original+"->"+s.Replacement] = true
	}
	// Without amount 0 or 1 in the tests, the boundary mutants live.
	for _, w := range []string{">->>=", "0->1", "0->(-1)"} {
		if !got[w] {
			t.Errorf("%s should have survived; survivors %v", w, got)
		}
	}
	// The cases that are tried still kill.
	for _, w := range []string{"&&->||", "==->!="} {
		if got[w] {
			t.Errorf("%s should have been killed", w)
		}
	}
}

func TestGenerateDeletesStatements(t *testing.T) {
	ms, err := Generate("testdata/untested", "tally")
	if err != nil {
		t.Fatal(err)
	}
	// double := 0 is a definition and return is not removable: one deletion,
	// of the assignment.
	var del []Mutant
	for _, m := range ms {
		if m.Original == "statement" {
			del = append(del, m)
		}
	}
	if len(del) != 1 || del[0].Replacement != "deleted" || del[0].Line != 7 {
		t.Fatalf("want one deletion at line 7, got %v", del)
	}
	out, err := del[0].render()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "double = amount") {
		t.Errorf("the assignment is still in the mutant:\n%s", out)
	}
}

func TestRunUntestedAssignmentSurvivesDeletion(t *testing.T) {
	rep, err := Run(context.Background(), Options{Dir: "./testdata/untested"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range rep.Survivors() {
		if s.Original == "statement" && s.Replacement == "deleted" {
			found = true
		}
	}
	if !found {
		t.Errorf("deleting the untested assignment should survive; survivors %v", rep.Survivors())
	}
}

func TestRunRefusesFailingTests(t *testing.T) {
	_, err := Run(context.Background(), Options{Dir: "./testdata/broken"})
	if err == nil || !strings.Contains(err.Error(), "before any mutation") {
		t.Fatalf("want a refusal before mutation, got %v", err)
	}
}

func TestRunCountsInvalidAndLoopingMutants(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "a" + "b": the - mutant does not compile. The loop never ends when
	// the bound is mutated to a larger one.
	files := map[string]string{
		"go.mod":    "module p\n\ngo 1.27\n",
		"p.go":      "package p\n\nfunc join() string { return \"a\" + \"b\" }\n\nfunc spin(n int) int {\n\tvar i int\n\tfor i != n {\n\t\ti++\n\t}\n\treturn i\n}\n",
		"p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestAll(t *testing.T) {\n\tif join() != \"ab\" || spin(3) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(context.Background(), Options{Dir: ".", Timeout: 5e9})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Count(Invalid) == 0 {
		t.Errorf("the string subtraction should be invalid: %+v", rep.Results)
	}
	if rep.Count(TimedOut) == 0 {
		t.Errorf("the looping mutant should time out: %+v", rep.Results)
	}
	if len(rep.Survivors()) != 0 {
		t.Errorf("survivors: %v", rep.Survivors())
	}
}
