// Package contract is format v0 of a task contract, tasks/<task_id>.json
// (docs/1-methodology.md § Tasks, M8): one bounded task, no state.
package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/strictjson"
)

// Format is the only value Contract.Format accepts.
const Format = "v0"

var (
	kinds   = map[string]bool{"logic": true, "spec": true, "test": true, "plugin": true}
	risks   = map[string]bool{"low": true, "medium": true, "high": true}
	specRef = regexp.MustCompile(`^(spec/[^#]+)(#[A-Za-z0-9_.]+)?$`)
	program = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	shells  = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "env": true}
	// Paths no executor may write: its own and other contracts, generated
	// code, CI, and the dependency list (A6). spec/ is open to spec tasks only.
	neverAllowed = []string{"tasks/", "gen/", ".github/", "vendor/", "go.mod", "go.sum"}
)

// Contract is one task.
type Contract struct {
	Format               string     `json:"format"`
	TaskID               string     `json:"task_id"`
	ProjectID            string     `json:"project_id"`
	Objective            string     `json:"objective"`
	Kind                 string     `json:"kind"`
	SpecRefs             []string   `json:"spec_refs"`
	AllowedFiles         []string   `json:"allowed_files"`
	Dependencies         []string   `json:"dependencies"`
	AcceptanceCriteria   []string   `json:"acceptance_criteria"`
	VerificationCommands [][]string `json:"verification_commands"`
	RiskLevel            string     `json:"risk_level"`
	RequiredApprovals    []string   `json:"required_approvals"`
	Budget               Budget     `json:"budget"`
	ExpectedArtifacts    []string   `json:"expected_artifacts"`
}

// Budget bounds one task's attempts, wall time and reasoner spend.
type Budget struct {
	CoderRetries int     `json:"coder_retries"`
	Minutes      int     `json:"minutes"`
	ReasonerUSD  float64 `json:"reasoner_usd"`
}

// Decode decodes data strictly and validates it.
func Decode(data []byte) (*Contract, error) {
	var c Contract
	if err := strictjson.Decode(data, &c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Load reads tasks/<task_id>.json; the file name must match the task id.
func Load(path string) (*Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}
	if want := strings.TrimSuffix(filepath.Base(path), ".json"); c.TaskID != want {
		return nil, fmt.Errorf("%s: task_id %q must be in %s.json", path, c.TaskID, c.TaskID)
	}
	return c, nil
}

// LoadDir loads every contract in dir and checks them as a set.
func LoadDir(dir string) ([]*Contract, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var cs []*Contract
	var l check.List
	for _, f := range files {
		c, err := Load(f)
		if err != nil {
			l.Addf("", "%v", err)
			continue
		}
		cs = append(cs, c)
	}
	if err := CheckSet(cs); err != nil {
		l.Addf("", "%v", err)
	}
	return cs, l.Err()
}

// Validate reports every rule the contract breaks on its own.
func (c *Contract) Validate() error {
	var l check.List
	if c.Format != Format {
		l.Addf("format", "must be %q, got %q", Format, c.Format)
	}
	if !check.Slug(c.TaskID) {
		l.Addf("task_id", "must be a lower-case slug, got %q", c.TaskID)
	}
	if !check.Slug(c.ProjectID) {
		l.Addf("project_id", "must be a lower-case slug, got %q", c.ProjectID)
	}
	if !check.Text(c.Objective) {
		l.Addf("objective", "must be one non-empty line")
	}
	if !kinds[c.Kind] {
		l.Addf("kind", "must be logic, spec, test or plugin, got %q", c.Kind)
	}

	if len(c.SpecRefs) == 0 {
		l.Addf("spec_refs", "must name at least one part of the model")
	}
	for _, r := range c.SpecRefs {
		m := specRef.FindStringSubmatch(r)
		if m == nil || !check.RelPath(m[1]) {
			l.Addf("spec_refs", "%q must be spec/<file>[#Part.part]", r)
		}
	}

	allowed := map[string]bool{}
	if len(c.AllowedFiles) == 0 {
		l.Addf("allowed_files", "must list at least one file")
	}
	for _, f := range c.AllowedFiles {
		switch {
		case !check.RelPath(f) || strings.ContainsAny(f, "*?["):
			l.Addf("allowed_files", "%q must be a plain relative path, no globs", f)
		case allowed[f]:
			l.Addf("allowed_files", "%q is listed twice", f)
		case strings.HasPrefix(f, "spec/") && c.Kind != "spec":
			l.Addf("allowed_files", "%q: only a spec task may write spec/", f)
		default:
			for _, never := range neverAllowed {
				if f == never || strings.HasPrefix(f, never) {
					l.Addf("allowed_files", "%q: no task may write %s", f, never)
				}
			}
		}
		allowed[f] = true
	}

	seen := map[string]bool{}
	for _, d := range c.Dependencies {
		switch {
		case !check.Slug(d):
			l.Addf("dependencies", "%q must be a task id", d)
		case d == c.TaskID:
			l.Addf("dependencies", "a task cannot depend on itself")
		case seen[d]:
			l.Addf("dependencies", "%q is listed twice", d)
		}
		seen[d] = true
	}

	if len(c.AcceptanceCriteria) == 0 {
		l.Addf("acceptance_criteria", "must state at least one criterion")
	}
	for i, a := range c.AcceptanceCriteria {
		if !check.Text(a) {
			l.Addf(fmt.Sprintf("acceptance_criteria[%d]", i), "must be one non-empty line")
		}
	}

	if len(c.VerificationCommands) == 0 {
		l.Addf("verification_commands", "must list at least one command")
	}
	for i, argv := range c.VerificationCommands {
		p := fmt.Sprintf("verification_commands[%d]", i)
		switch {
		case len(argv) == 0:
			l.Addf(p, "must be a non-empty argv list")
		case !program.MatchString(argv[0]):
			l.Addf(p, "program %q must be a bare name found on PATH", argv[0])
		case shells[argv[0]]:
			l.Addf(p, "%q: commands run without a shell", argv[0])
		}
		for _, a := range argv {
			if a == "" || strings.ContainsAny(a, "\n\r\x00") {
				l.Addf(p, "every argument must be non-empty and single-line")
				break
			}
		}
	}

	if !risks[c.RiskLevel] {
		l.Addf("risk_level", "must be low, medium or high, got %q", c.RiskLevel)
	}
	seen = map[string]bool{}
	for _, a := range c.RequiredApprovals {
		if !check.Slug(a) {
			l.Addf("required_approvals", "%q must be a lower-case slug", a)
		}
		if seen[a] {
			l.Addf("required_approvals", "%q is listed twice", a)
		}
		seen[a] = true
	}
	if c.RiskLevel == "medium" && len(c.RequiredApprovals) == 0 {
		l.Addf("required_approvals", "a medium-risk task needs a human review")
	}
	if c.RiskLevel == "high" && !seen["founder"] {
		l.Addf("required_approvals", "a high-risk task needs the founder's approval")
	}

	if c.Budget.CoderRetries < 0 {
		l.Addf("budget.coder_retries", "cannot be negative")
	}
	if c.Budget.Minutes < 1 {
		l.Addf("budget.minutes", "must be at least 1")
	}
	if c.Budget.ReasonerUSD < 0 {
		l.Addf("budget.reasoner_usd", "cannot be negative")
	}

	if len(c.ExpectedArtifacts) == 0 {
		l.Addf("expected_artifacts", "must list at least one file")
	}
	for _, a := range c.ExpectedArtifacts {
		if !allowed[a] {
			l.Addf("expected_artifacts", "%q is not in allowed_files", a)
		}
	}
	return l.Err()
}

// CheckSet checks contracts together: ids unique, every dependency a known
// task, no dependency cycle.
func CheckSet(cs []*Contract) error {
	var l check.List
	byID := map[string]*Contract{}
	for _, c := range cs {
		if byID[c.TaskID] != nil {
			l.Addf("tasks", "task %q is declared twice", c.TaskID)
		}
		byID[c.TaskID] = c
	}
	for _, c := range cs {
		for _, d := range c.Dependencies {
			if byID[d] == nil {
				l.Addf("tasks["+c.TaskID+"].dependencies", "%q is not a task", d)
			}
		}
	}
	state := map[string]int{}
	var stack []string
	var visit func(id string) bool
	visit = func(id string) bool {
		if state[id] == 1 {
			for i, s := range stack {
				if s == id {
					l.Addf("tasks", "dependencies form a cycle: %s", strings.Join(append(stack[i:], id), " -> "))
				}
			}
			return true
		}
		if state[id] == 2 || byID[id] == nil {
			return false
		}
		state[id] = 1
		stack = append(stack, id)
		for _, d := range byID[id].Dependencies {
			if visit(d) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		return false
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if visit(id) {
			break
		}
	}
	return l.Err()
}
