package contract

import (
	"os"
	"strings"
	"testing"
)

func pilot(t *testing.T) *Contract {
	t.Helper()
	c, err := Load("testdata/acme-0001.json")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPilotSetIsValid(t *testing.T) {
	cs, err := LoadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d contracts", len(cs))
	}
}

func TestLoadNameMustMatchFile(t *testing.T) {
	data, _ := os.ReadFile("testdata/acme-0001.json")
	path := t.TempDir() + "/acme-9.json"
	os.WriteFile(path, data, 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), `task_id "acme-0001" must be in acme-0001.json`) {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeIsStrict(t *testing.T) {
	data, _ := os.ReadFile("testdata/acme-0001.json")
	for _, c := range []struct{ from, to, want string }{
		{`"minutes": 30`, `"minutes": 30, "state": "running"`, `unknown field "budget.state"`},
		{`"risk_level": "low",`, `"risk_level": "low", "risk_level": "high",`, `duplicate key "risk_level"`},
		{`"objective"`, `"Objective"`, `unknown field "Objective"`},
	} {
		bad := strings.Replace(string(data), c.from, c.to, 1)
		if _, err := Decode([]byte(bad)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("got %v, want %q", err, c.want)
		}
	}
}

func TestRules(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(c *Contract)
		want   string
	}{
		{"format", func(c *Contract) { c.Format = "" }, `format: must be "v0"`},
		{"task id", func(c *Contract) { c.TaskID = "ACME_1" }, `task_id: must be a lower-case slug, got "ACME_1"`},
		{"project id", func(c *Contract) { c.ProjectID = "" }, "project_id: must be a lower-case slug"},
		{"objective", func(c *Contract) { c.Objective = "do it\nand more" }, "objective: must be one non-empty line"},
		{"kind", func(c *Contract) { c.Kind = "infra" }, `kind: must be logic, spec, test or plugin, got "infra"`},
		{"no spec refs", func(c *Contract) { c.SpecRefs = nil }, "spec_refs: must name at least one part of the model"},
		{"spec ref outside spec", func(c *Contract) { c.SpecRefs = []string{"docs/x.md"} }, `spec_refs: "docs/x.md" must be spec/<file>[#Part.part]`},
		{"spec ref escapes", func(c *Contract) { c.SpecRefs = []string{"spec/../tasks/x.json"} }, `"spec/../tasks/x.json" must be spec/<file>`},
		{"no allowed files", func(c *Contract) { c.AllowedFiles = nil; c.ExpectedArtifacts = nil }, "allowed_files: must list at least one file"},
		{"allowed glob", func(c *Contract) { c.AllowedFiles = []string{"domain/*.go"} }, `allowed_files: "domain/*.go" must be a plain relative path, no globs`},
		{"allowed escapes", func(c *Contract) { c.AllowedFiles = []string{"../x.go"} }, `"../x.go" must be a plain relative path`},
		{"allowed twice", func(c *Contract) { c.AllowedFiles = append(c.AllowedFiles, c.AllowedFiles[0]) }, "is listed twice"},
		{"writes own contract", func(c *Contract) { c.AllowedFiles = append(c.AllowedFiles, "tasks/acme-0001.json") }, `"tasks/acme-0001.json": no task may write tasks/`},
		{"writes generated code", func(c *Contract) { c.AllowedFiles = append(c.AllowedFiles, "gen/acme/table.go") }, "no task may write gen/"},
		{"writes CI", func(c *Contract) { c.AllowedFiles = append(c.AllowedFiles, ".github/workflows/ci.yml") }, "no task may write .github/"},
		{"writes go.mod", func(c *Contract) { c.AllowedFiles = append(c.AllowedFiles, "go.mod") }, `"go.mod": no task may write go.mod`},
		{"logic writes spec", func(c *Contract) { c.AllowedFiles = append(c.AllowedFiles, "spec/domain/acme.json") }, "only a spec task may write spec/"},
		{"dependency id", func(c *Contract) { c.Dependencies = []string{"Task 1"} }, `dependencies: "Task 1" must be a task id`},
		{"dependency self", func(c *Contract) { c.Dependencies = []string{"acme-0001"} }, "dependencies: a task cannot depend on itself"},
		{"dependency twice", func(c *Contract) { c.Dependencies = []string{"acme-0002", "acme-0002"} }, `dependencies: "acme-0002" is listed twice`},
		{"no criteria", func(c *Contract) { c.AcceptanceCriteria = nil }, "acceptance_criteria: must state at least one criterion"},
		{"blank criterion", func(c *Contract) { c.AcceptanceCriteria[1] = " " }, "acceptance_criteria[1]: must be one non-empty line"},
		{"no commands", func(c *Contract) { c.VerificationCommands = nil }, "verification_commands: must list at least one command"},
		{"empty argv", func(c *Contract) { c.VerificationCommands[0] = nil }, "verification_commands[0]: must be a non-empty argv list"},
		{"program path", func(c *Contract) { c.VerificationCommands[0][0] = "/usr/bin/go" }, `program "/usr/bin/go" must be a bare name`},
		{"shell", func(c *Contract) { c.VerificationCommands[0] = []string{"sh", "-c", "go test ./..."} }, `"sh": commands run without a shell`},
		{"blank argument", func(c *Contract) { c.VerificationCommands[0][1] = "" }, "every argument must be non-empty and single-line"},
		{"risk", func(c *Contract) { c.RiskLevel = "none" }, `risk_level: must be low, medium or high, got "none"`},
		{"approval name", func(c *Contract) { c.RequiredApprovals = []string{"The Founder"} }, `required_approvals: "The Founder" must be a lower-case slug`},
		{"approval twice", func(c *Contract) { c.RequiredApprovals = []string{"founder", "founder"} }, `"founder" is listed twice`},
		{"medium without review", func(c *Contract) { c.RiskLevel = "medium" }, "a medium-risk task needs a human review"},
		{"high without founder", func(c *Contract) { c.RiskLevel = "high"; c.RequiredApprovals = []string{"reviewer"} }, "a high-risk task needs the founder's approval"},
		{"retries negative", func(c *Contract) { c.Budget.CoderRetries = -1 }, "budget.coder_retries: cannot be negative"},
		{"minutes zero", func(c *Contract) { c.Budget.Minutes = 0 }, "budget.minutes: must be at least 1"},
		{"usd negative", func(c *Contract) { c.Budget.ReasonerUSD = -0.5 }, "budget.reasoner_usd: cannot be negative"},
		{"no artifacts", func(c *Contract) { c.ExpectedArtifacts = nil }, "expected_artifacts: must list at least one file"},
		{"artifact not allowed", func(c *Contract) { c.ExpectedArtifacts = append(c.ExpectedArtifacts, "README.md") }, `expected_artifacts: "README.md" is not in allowed_files`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := pilot(t)
			tc.break_(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v\nwant error containing %q", err, tc.want)
			}
		})
	}
}

func TestSetRules(t *testing.T) {
	load := func(t *testing.T) []*Contract {
		cs, err := LoadDir("testdata")
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	t.Run("unknown dependency", func(t *testing.T) {
		cs := load(t)
		cs[1].Dependencies = []string{"acme-0009"}
		if err := CheckSet(cs); err == nil || !strings.Contains(err.Error(), `tasks[acme-0002].dependencies: "acme-0009" is not a task`) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		cs := load(t)
		cs[0].Dependencies = []string{"acme-0002"}
		if err := CheckSet(cs); err == nil || !strings.Contains(err.Error(), "dependencies form a cycle: acme-0001 -> acme-0002 -> acme-0001") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("id twice", func(t *testing.T) {
		cs := load(t)
		cs[1].TaskID = "acme-0001"
		if err := CheckSet(cs); err == nil || !strings.Contains(err.Error(), `task "acme-0001" is declared twice`) {
			t.Fatalf("got %v", err)
		}
	})
}
