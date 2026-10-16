package mutate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Status is what became of one mutant.
type Status string

const (
	// Killed: the tests failed on the mutant, as they must.
	Killed Status = "killed"
	// TimedOut: the tests did not finish in time (a mutant can loop for
	// ever). Counted as killed.
	TimedOut Status = "timed out"
	// Survived: every test passed on the mutant. A missing test.
	Survived Status = "survived"
	// Invalid: the mutant does not compile (a string "+" turned into "-").
	// Reported, never counted either way.
	Invalid Status = "invalid"
)

// Options configure a run.
type Options struct {
	Dir     string        // package directory, as `go test` takes it (./domain/acme)
	Func    string        // function to mutate; "" means all
	Workers int           // mutants tried at once; 0 means half the CPUs
	Timeout time.Duration // per mutant; 0 means 5x the unmutated run, at least 10 s
}

// Result is one mutant and its outcome.
type Result struct {
	Mutant
	Status Status
}

// Report is the outcome of a run.
type Report struct {
	Results []Result
}

// Count returns how many results have status s.
func (r *Report) Count(s Status) int {
	n := 0
	for _, res := range r.Results {
		if res.Status == s {
			n++
		}
	}
	return n
}

// Survivors lists the mutants no test caught.
func (r *Report) Survivors() []Result {
	var out []Result
	for _, res := range r.Results {
		if res.Status == Survived {
			out = append(out, res)
		}
	}
	return out
}

// Run checks that the package's tests pass unmutated, then tries every
// mutant of the chosen function(s) against them. Survivors are in the report;
// the error is for a run that could not be made (no such function, tests
// failing before any mutation, no go tool).
func Run(ctx context.Context, opts Options) (*Report, error) {
	mutants, err := Generate(opts.Dir, opts.Func)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	if out, _, err := goTest(ctx, opts.Dir, "", 0); err != nil {
		return nil, fmt.Errorf("tests fail before any mutation:\n%s", out)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = max(10*time.Second, 5*time.Since(start))
	}

	tmp, err := os.MkdirTemp("", "mutate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	workers := opts.Workers
	if workers <= 0 {
		workers = max(1, runtime.NumCPU()/2)
	}

	rep := &Report{Results: make([]Result, len(mutants))}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex // the AST is mutated in place, so one mutant renders at a time
	var firstErr error
	var errOnce sync.Once
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				m := mutants[i]
				mu.Lock()
				src, err := m.render()
				mu.Unlock()
				var st Status
				if err == nil {
					st, err = try(ctx, opts.Dir, tmp, i, m, src, timeout)
				}
				if err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("%s: %w", m, err) })
					continue
				}
				rep.Results[i] = Result{Mutant: m, Status: st}
			}
		}()
	}
	for i := range mutants {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rep, nil
}

// try runs the package's tests with the mutated file laid over the real one.
func try(ctx context.Context, dir, tmp string, i int, m Mutant, src []byte, timeout time.Duration) (Status, error) {
	abs, err := filepath.Abs(m.File)
	if err != nil {
		return "", err
	}
	file := filepath.Join(tmp, fmt.Sprintf("m%d.go", i))
	if err := os.WriteFile(file, src, 0o600); err != nil {
		return "", err
	}
	overlay := filepath.Join(tmp, fmt.Sprintf("m%d.json", i))
	data, _ := json.Marshal(map[string]map[string]string{"Replace": {abs: file}})
	if err := os.WriteFile(overlay, data, 0o600); err != nil {
		return "", err
	}

	out, timedOut, err := goTest(ctx, dir, overlay, timeout)
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case timedOut:
		return TimedOut, nil
	case err == nil:
		return Survived, nil
	case strings.Contains(out, "[build failed]") || strings.Contains(out, "[setup failed]"):
		return Invalid, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return Killed, nil
	}
	return "", err // the go tool itself could not run
}

// goTest runs `go test` on dir. A non-nil error with exit status means the
// tests failed; timedOut reports a deadline.
func goTest(ctx context.Context, dir, overlay string, timeout time.Duration) (out string, timedOut bool, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	args := []string{"test", "-count=1"}
	if overlay != "" {
		args = append(args, "-overlay="+overlay)
	}
	args = append(args, dir)
	cmd := exec.CommandContext(ctx, "go", args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	return buf.String(), timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded), err
}
