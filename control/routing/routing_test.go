package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepoTable: the founder's real file decodes.
func TestRepoTable(t *testing.T) {
	tb, err := Load(filepath.Join("..", "..", "spec", "routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if tb.Tiers["coder"] == "" || tb.Jobs["escalation.second"] != "top" {
		t.Errorf("unexpected table %+v", tb)
	}
}

const good = `{"format":"v0","tiers":{"coder":"c","routine":"r","top":"t"},"jobs":{"a.b":"top"},"propose_promotion_below_bp":6000,"on_top_limit":"queue-design-jobs"}`

func TestDecode(t *testing.T) {
	if _, err := Decode([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ in, want string }{
		"format":         {strings.Replace(good, `"v0"`, `"v1"`, 1), `format: must be "v0"`},
		"missing tier":   {strings.Replace(good, `"top":"t"`, `"best":"t"`, 1), `tier "top" has no model`},
		"unknown tier":   {strings.Replace(good, `"top":"t"`, `"top":"t","best":"x"`, 1), `unknown tier "best"`},
		"blank model":    {strings.Replace(good, `"coder":"c"`, `"coder":" "`, 1), "tiers[coder]: model must be one non-empty line"},
		"no jobs":        {strings.Replace(good, `"a.b":"top"`, ``, 1), "must route at least one job"},
		"bad job name":   {strings.Replace(good, `"a.b"`, `"A.b"`, 1), `job "A.b" must be dotted snake_case`},
		"bad job tier":   {strings.Replace(good, `"a.b":"top"`, `"a.b":"best"`, 1), `jobs[a.b]: tier must be coder, routine or top`},
		"threshold high": {strings.Replace(good, `6000`, `10001`, 1), "propose_promotion_below_bp: must be between 0 and 10000"},
		"threshold low":  {strings.Replace(good, `6000`, `-1`, 1), "propose_promotion_below_bp: must be between 0 and 10000"},
		"on_top_limit":   {strings.Replace(good, `queue-design-jobs`, `drop`, 1), `on_top_limit: must be queue-design-jobs`},
		"unknown field":  {strings.Replace(good, `"jobs"`, `"extra":1,"jobs"`, 1), `unknown field "extra"`},
		"wrong case":     {strings.Replace(good, `"jobs"`, `"Jobs"`, 1), "unknown field"},
		"duplicate key":  {strings.Replace(good, `"jobs"`, `"format":"v0","jobs"`, 1), "duplicate"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	// Boundaries are valid.
	for _, bp := range []string{"0", "10000"} {
		if _, err := Decode([]byte(strings.Replace(good, "6000", bp, 1))); err != nil {
			t.Errorf("%s: %v", bp, err)
		}
	}
}

func TestLoadNamesTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "routing.json")
	if err := os.WriteFile(p, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Load(p + ".missing"); err == nil {
		t.Fatal("a missing file loaded")
	}
}
