// Package routing is format v0 of spec/routing.json: which model fills each
// tier and which tier takes each job kind (docs/2-architecture.md § Routing,
// M13). The founder edits the file by pull request; the control plane only
// reads it.
package routing

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/project"
	"github.com/briolink/briolink/core/strictjson"
)

// Format is the only value Table.Format accepts.
const Format = "v0"

// onTopLimits are the values on_top_limit accepts: what to do when the top
// tier's limit is reached.
var onTopLimits = map[string]bool{"queue-design-jobs": true}

var jobName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// Table is spec/routing.json.
type Table struct {
	Format string `json:"format"`
	// Tiers maps each of coder, routine and top to the model that fills it.
	Tiers map[string]string `json:"tiers"`
	// Jobs maps a job kind to the tier that takes it.
	Jobs map[string]string `json:"jobs"`
	// ProposePromotionBelowBp is a basis-point solve rate (A17).
	ProposePromotionBelowBp int    `json:"propose_promotion_below_bp"`
	OnTopLimit              string `json:"on_top_limit"`
}

// Decode decodes data strictly and validates it.
func Decode(data []byte) (*Table, error) {
	var t Table
	if err := strictjson.Decode(data, &t); err != nil {
		return nil, err
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// Load reads and decodes a routing.json.
func Load(path string) (*Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}
	return t, nil
}

// Validate reports every rule the table breaks.
func (t *Table) Validate() error {
	var l check.List
	if t.Format != Format {
		l.Addf("format", "must be %q, got %q", Format, t.Format)
	}
	for tier := range project.Tiers {
		if m, ok := t.Tiers[tier]; !ok {
			l.Addf("tiers", "tier %q has no model", tier)
		} else if !check.Text(m) {
			l.Addf("tiers["+tier+"]", "model must be one non-empty line")
		}
	}
	for _, tier := range sortedKeys(t.Tiers) {
		if !project.Tiers[tier] {
			l.Addf("tiers", "unknown tier %q (coder, routine or top)", tier)
		}
	}
	if len(t.Jobs) == 0 {
		l.Addf("jobs", "must route at least one job")
	}
	for _, job := range sortedKeys(t.Jobs) {
		if !jobName.MatchString(job) {
			l.Addf("jobs", "job %q must be dotted snake_case", job)
		}
		if !project.Tiers[t.Jobs[job]] {
			l.Addf("jobs["+job+"]", "tier must be coder, routine or top, got %q", t.Jobs[job])
		}
	}
	if t.ProposePromotionBelowBp < 0 || t.ProposePromotionBelowBp > 10000 {
		l.Addf("propose_promotion_below_bp", "must be between 0 and 10000 basis points, got %d", t.ProposePromotionBelowBp)
	}
	if !onTopLimits[t.OnTopLimit] {
		l.Addf("on_top_limit", "must be queue-design-jobs, got %q", t.OnTopLimit)
	}
	return l.Err()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
