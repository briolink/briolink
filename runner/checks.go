package runner

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/briolink/briolink/runner/contract"
)

// The kinds of failure an attempt can end in. A check's kind is named after
// it; the contract's verification commands are "test" for go test, "mutate"
// for the mutation check and "verify" for anything else.
const (
	kindCoder    = "coder"    // the coder returned no usable answer
	kindScope    = "scope"    // the answer wrote outside allowed_files
	kindGofmt    = "gofmt"    // a file is not formatted
	kindVet      = "vet"      // go vet found a problem
	kindGenerate = "generate" // gen/ differs from what the generators write
	kindTest     = "test"     // go test failed
	kindMutate   = "mutate"   // a mutant survived
	kindVerify   = "verify"   // another verification command failed
)

// DefaultGenerate is the regenerate-and-diff command (M1), a binary the build
// image carries.
var DefaultGenerate = []string{"generate", "-check", "-root", "."}

type check struct {
	kind string
	argv []string
}

// checks returns the checks one attempt must pass, in order: the formatting diff of
// the files the task may write, go vet of their packages, regenerate-and-diff, then the
// contract's own verification commands (go test, the mutation check).
func (r *Runner) checks(c *contract.Contract) []check {
	gofmt, vet := []string{"gofmt", "-d"}, []string{"go", "vet"}
	seen := map[string]bool{}
	for _, f := range c.AllowedFiles {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		gofmt = append(gofmt, f)
		if dir := "./" + path.Dir(f); !seen[dir] {
			seen[dir] = true
			vet = append(vet, dir)
		}
	}
	if len(gofmt) == 2 {
		gofmt, vet = append(gofmt, "."), append(vet, "./...")
	}
	gen := r.Generate
	if gen == nil {
		gen = DefaultGenerate
	}
	out := []check{{kindGofmt, gofmt}, {kindVet, vet}, {kindGenerate, gen}}
	for _, argv := range c.VerificationCommands {
		out = append(out, check{verificationKind(argv), argv})
	}
	return out
}

func verificationKind(argv []string) string {
	switch {
	case argv[0] == "mutate":
		return kindMutate
	case argv[0] == "go" && len(argv) > 1 && argv[1] == "test":
		return kindTest
	}
	return kindVerify
}

// run executes the checks in order and returns the first failure: its kind and
// output, or an empty kind when every check passed.
func run(ctx context.Context, ws Workspace, checks []check) (kind, output string, err error) {
	for _, ck := range checks {
		out, err := ws.Exec(ctx, ck.argv)
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", strings.Join(ck.argv, " "), err)
		}
		text := strings.TrimSpace(out.Text)
		switch {
		case ck.kind == kindGofmt && (out.Code != 0 || text != ""):
			return ck.kind, "not formatted by gofmt:\n" + text, nil
		case out.Code != 0:
			if text == "" {
				text = fmt.Sprintf("exit status %d, no output", out.Code)
			}
			return ck.kind, text, nil
		}
	}
	return "", "", nil
}

var timing = regexp.MustCompile(`\b\d+(\.\d+)?s\b`)

// signature is what makes two failures "the same way": the failing check and
// its output, without the durations a test run prints.
func signature(kind, output string) string {
	return kind + "\n" + timing.ReplaceAllString(output, "<t>s")
}

// trim cuts output to at most max bytes, keeping the start, which names the
// first failures.
func trim(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n… (truncated)"
}
