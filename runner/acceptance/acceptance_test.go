// Package acceptance holds the P0 acceptance set: the acme project the
// runner must complete unattended (docs/5-roadmap.md, P0 exit criteria).
package acceptance

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/briolink/briolink/core/project"
	domain "github.com/briolink/briolink/plugins/domain/spec"
	http "github.com/briolink/briolink/plugins/http/spec"
	"github.com/briolink/briolink/runner/contract"
)

const root = "acme"

type model struct {
	project *project.Project
	domain  *domain.Context
	routes  *http.Routes
	tasks   []*contract.Contract
}

func load(t *testing.T) model {
	t.Helper()
	var m model
	var err error
	if m.project, err = project.LoadProject(filepath.Join(root, "spec/project.json")); err != nil {
		t.Fatal(err)
	}
	manifests := map[string]*project.Manifest{}
	for _, name := range []string{"domain", "http"} {
		if manifests[name], err = project.LoadManifest("../../core/project/testdata/" + name + ".json"); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.project.Resolve(manifests); err != nil {
		t.Fatal(err)
	}
	if m.domain, err = domain.Load(filepath.Join(root, "spec/domain/acme.json")); err != nil {
		t.Fatal(err)
	}
	if m.routes, err = http.Load(filepath.Join(root, "spec/routes.json")); err != nil {
		t.Fatal(err)
	}
	if err := m.routes.CheckAgainst(map[string]*domain.Context{m.domain.Context: m.domain}); err != nil {
		t.Fatal(err)
	}
	if m.tasks, err = contract.LoadDir(filepath.Join(root, "tasks")); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSetIsValid(t *testing.T) {
	m := load(t)
	for _, c := range m.tasks {
		if c.ProjectID != m.project.ProjectID {
			t.Errorf("%s: project_id %q, want %q", c.TaskID, c.ProjectID, m.project.ProjectID)
		}
	}
}

func TestSpecRefsResolve(t *testing.T) {
	m := load(t)
	for _, c := range m.tasks {
		for _, ref := range c.SpecRefs {
			if err := m.resolve(ref); err != "" {
				t.Errorf("%s: %s: %s", c.TaskID, ref, err)
			}
		}
	}
}

func (m model) resolve(ref string) string {
	file, part, _ := strings.Cut(ref, "#")
	head, name, ok := strings.Cut(part, ".")
	switch {
	case file == "spec/domain/acme.json" && head == "functions":
		if m.domain.Function(name) == nil {
			return "no such function"
		}
	case file == "spec/domain/acme.json" && ok:
		e := m.domain.Entity(head)
		if e == nil || e.Statechart == nil || !slices.ContainsFunc(e.Statechart.Events, func(ev domain.Event) bool { return ev.Name == name }) {
			return "no such entity event"
		}
	case file == "spec/routes.json" && head == "routes":
		if !slices.ContainsFunc(m.routes.Routes, func(r http.Route) bool { return r.Name == name }) {
			return "no such route"
		}
	default:
		return "not a part this set uses"
	}
	return ""
}

// Every function is written by exactly one logic task, after the test task
// that writes its tests.
func TestEveryFunctionHasItsTasks(t *testing.T) {
	m := load(t)
	writers := map[string][]*contract.Contract{}
	for _, c := range m.tasks {
		for _, a := range c.ExpectedArtifacts {
			writers[a] = append(writers[a], c)
		}
	}
	for _, f := range m.domain.Functions {
		code, tests := writers["domain/acme/"+f.Name+".go"], writers["domain/acme/"+f.Name+"_test.go"]
		if len(code) != 1 || code[0].Kind != "logic" {
			t.Errorf("%s: want one logic task writing its code, got %d", f.Name, len(code))
			continue
		}
		if len(tests) != 1 || tests[0].Kind != "test" {
			t.Errorf("%s: want one test task writing its tests, got %d", f.Name, len(tests))
			continue
		}
		if !slices.Contains(code[0].Dependencies, tests[0].TaskID) {
			t.Errorf("%s: %s must depend on %s", f.Name, code[0].TaskID, tests[0].TaskID)
		}
	}
}
