package project

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func pilot(t *testing.T) (*Project, map[string]*Manifest) {
	t.Helper()
	p, err := LoadProject("testdata/project.json")
	if err != nil {
		t.Fatal(err)
	}
	ms := map[string]*Manifest{}
	for _, name := range []string{"domain", "http", "sqlite"} {
		m, err := LoadManifest("testdata/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		ms[name] = m
	}
	return p, ms
}

func TestPilotIsValid(t *testing.T) {
	p, ms := pilot(t)
	if err := p.Resolve(ms); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeIsStrict(t *testing.T) {
	data, _ := os.ReadFile("testdata/project.json")
	bad := strings.Replace(string(data), `"project_id"`, `"Project_ID"`, 1)
	if _, err := DecodeProject([]byte(bad)); err == nil || !strings.Contains(err.Error(), `unknown field "Project_ID"`) {
		t.Fatalf("got %v", err)
	}
	data, _ = os.ReadFile("testdata/http.json")
	bad = strings.Replace(string(data), `"ports": [],`, `"ports": [], "ports": ["x"],`, 1)
	if _, err := DecodeManifest([]byte(bad)); err == nil || !strings.Contains(err.Error(), `duplicate key "ports"`) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRules(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(p *Project)
		want   string
	}{
		{"format", func(p *Project) { p.Format = "" }, `format: must be "v0"`},
		{"project id", func(p *Project) { p.ProjectID = "Acme Inc" }, "project_id: must be a lower-case slug"},
		{"name", func(p *Project) { p.Name = "" }, "name: must be one non-empty line"},
		{"go module", func(p *Project) { p.GoModule = "acme" }, "go_module: must be a module path"},
		{"plugin name", func(p *Project) { p.Plugins[0].Name = "Domain" }, `plugins[0]: name must be a lower-case slug, got "Domain"`},
		{"plugin twice", func(p *Project) { p.Plugins[1].Name = "domain" }, `plugins[1]: plugin "domain" is picked twice`},
		{"plugin version", func(p *Project) { p.Plugins[0].Version = "v1" }, `plugins[0]: version must be MAJOR.MINOR.PATCH, got "v1"`},
		{"options not object", func(p *Project) { p.Plugins[0].Options = json.RawMessage(`[]`) }, "plugins[0].options: must be a JSON object"},
		{"options null", func(p *Project) { p.Plugins[0].Options = json.RawMessage(`null`) }, "plugins[0].options: must be a JSON object"},
		{"routing job", func(p *Project) { p.Routing["Escalation"] = "top" }, `routing: job "Escalation" must be dotted snake_case`},
		{"routing tier", func(p *Project) { p.Routing["escalation.first"] = "max" }, `routing[escalation.first]: tier must be coder, routine or top, got "max"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, _ := pilot(t)
			c.break_(p)
			err := p.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant error containing %q", err, c.want)
			}
		})
	}
}

func TestResolveRules(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(p *Project, ms map[string]*Manifest)
		want   string
	}{
		{"no manifest", func(p *Project, ms map[string]*Manifest) { delete(ms, "sqlite") }, `plugins[sqlite]: no manifest for plugin "sqlite"`},
		{"version mismatch", func(p *Project, ms map[string]*Manifest) { ms["http"].Version = "0.2.0" }, "plugins[http]: picks version 0.1.0 but the manifest is 0.2.0"},
		{"requirement not picked", func(p *Project, ms map[string]*Manifest) { p.Plugins = p.Plugins[1:]; delete(ms, "domain") }, `plugins[http]: requires plugin "domain", which the project does not pick`},
		{"requirement too old", func(p *Project, ms map[string]*Manifest) { ms["http"].Requires[0].MinVersion = "0.10.0" }, "requires domain >= 0.10.0, the project picks 0.1.0"},
		{"unknown option", func(p *Project, ms map[string]*Manifest) { p.Plugins[1].Options = json.RawMessage(`{"prefix":"/"}`) }, `plugins[http].options: "prefix" is not an option of http`},
		{"option type", func(p *Project, ms map[string]*Manifest) {
			p.Plugins[2].Options = json.RawMessage(`{"open_database_cap":"64"}`)
		}, `plugins[sqlite].options[open_database_cap]: must be of type int, got "64"`},
		{"spec kind twice", func(p *Project, ms map[string]*Manifest) { ms["sqlite"].SpecKinds[0].Kind = "routes" }, `plugins[sqlite]: spec kind "routes" is also contributed by http`},
		{"cycle", func(p *Project, ms map[string]*Manifest) {
			ms["domain"].Requires = []Requirement{{Plugin: "http", MinVersion: "0.1.0"}}
		}, "plugins: requirements form a cycle: domain -> http -> domain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ms := pilot(t)
			c.break_(p, ms)
			err := p.Resolve(ms)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant error containing %q", err, c.want)
			}
		})
	}
}

func TestManifestRules(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(m *Manifest)
		want   string
	}{
		{"format", func(m *Manifest) { m.Format = "1" }, `format: must be "v0"`},
		{"name", func(m *Manifest) { m.Name = "my plugin" }, "name: must be a lower-case slug"},
		{"version", func(m *Manifest) { m.Version = "1.0" }, `version: must be MAJOR.MINOR.PATCH, got "1.0"`},
		{"purpose", func(m *Manifest) { m.Purpose = "" }, "purpose: must be one non-empty line"},
		{"option name", func(m *Manifest) { m.Options[0].Name = "basePath" }, "options[basePath]: name must be snake_case"},
		{"option twice", func(m *Manifest) { m.Options[1].Name = "base_path" }, "options[base_path]: option is declared twice"},
		{"option type", func(m *Manifest) { m.Options[0].Type = "path" }, `options[base_path]: type must be string, int, bool or []string, got "path"`},
		{"option default type", func(m *Manifest) { m.Options[0].Default = json.RawMessage(`1`) }, "options[base_path].default: must be of type string, got 1"},
		{"option default missing", func(m *Manifest) { m.Options[0].Default = json.RawMessage(`null`) }, "options[base_path].default: must be of type string, got null"},
		{"port name", func(m *Manifest) { m.Ports = []string{"Object-Storage"} }, `ports: "Object-Storage" must be snake_case`},
		{"port twice", func(m *Manifest) { m.Ports = []string{"mail", "mail"} }, `ports: "mail" is listed twice`},
		{"requires self", func(m *Manifest) { m.Requires[0].Plugin = "http" }, "requires[http]: a plugin cannot require itself"},
		{"requires twice", func(m *Manifest) { m.Requires = append(m.Requires, m.Requires[0]) }, "requires[domain]: required twice"},
		{"requires version", func(m *Manifest) { m.Requires[0].MinVersion = "latest" }, `requires[domain]: min_version must be MAJOR.MINOR.PATCH, got "latest"`},
		{"spec kind name", func(m *Manifest) { m.SpecKinds[0].Kind = "Routes" }, "spec_kinds[Routes]: kind must be snake_case"},
		{"spec kind twice", func(m *Manifest) { m.SpecKinds = append(m.SpecKinds, m.SpecKinds[0]) }, "spec_kinds[routes]: kind is declared twice"},
		{"spec kind glob", func(m *Manifest) { m.SpecKinds[0].Files = "spec/[routes" }, `spec_kinds[routes].files: must be a relative glob, got "spec/[routes"`},
		{"spec kind glob outside", func(m *Manifest) { m.SpecKinds[0].Files = "../spec/*.json" }, "spec_kinds[routes].files: must be a relative glob"},
		{"generator path", func(m *Manifest) { m.SpecKinds[0].Generator = "/usr/bin/gen" }, "spec_kinds[routes].generator: must be a relative path inside the plugin"},
		{"template path", func(m *Manifest) { m.Templates[0] = "../x.tmpl" }, `templates: "../x.tmpl" must be a relative path inside the plugin`},
		{"template twice", func(m *Manifest) { m.Templates = append(m.Templates, m.Templates[0]) }, `templates: "templates/error.json.tmpl" is listed twice`},
		{"fragment path", func(m *Manifest) { m.SpecFragments = []string{"/abs.json"} }, `spec_fragments: "/abs.json" must be a relative path`},
		{"web module path", func(m *Manifest) { m.WebModules = []string{"web/./x.js"} }, `web_modules: "web/./x.js" must be a relative path`},
		{"migration name", func(m *Manifest) { m.Migrations = []string{"migrations/1_init.sql"} }, `migrations: "migrations/1_init.sql" must be migrations/NNNN_name.sql`},
		{"migration order", func(m *Manifest) { m.Migrations = []string{"migrations/0002_b.sql", "migrations/0002_a.sql"} }, `migrations: "migrations/0002_a.sql" must be numbered after the one before it`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ms := pilot(t)
			m := ms["http"]
			c.break_(m)
			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant error containing %q", err, c.want)
			}
		})
	}
}
