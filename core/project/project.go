// Package project is format v0 of spec/project.json (the plugins a project
// picks, their options, its routing overrides) and of plugin.json, the
// manifest every plugin ships (docs/2-architecture.md § App core and
// plugins, A5).
package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/strictjson"
)

// Format is the only value either file's format accepts.
const Format = "v0"

// Tiers are the routing table's model slots (docs/2-architecture.md § Routing).
var Tiers = map[string]bool{"coder": true, "routine": true, "top": true}

var (
	jobName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	module  = regexp.MustCompile(`^[a-z0-9.-]+(/[A-Za-z0-9._~-]+)+$`)
)

// Project is spec/project.json.
type Project struct {
	Format    string            `json:"format"`
	ProjectID string            `json:"project_id"`
	Name      string            `json:"name"`
	GoModule  string            `json:"go_module"`
	Plugins   []PluginRef       `json:"plugins"`
	Routing   map[string]string `json:"routing"`
}

// PluginRef picks one plugin at an exact version; Options is a JSON object
// whose keys the plugin's manifest declares.
type PluginRef struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Options json.RawMessage `json:"options"`
}

// DecodeProject decodes data strictly and validates it.
func DecodeProject(data []byte) (*Project, error) {
	var p Project
	if err := strictjson.Decode(data, &p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// LoadProject reads and decodes a project.json.
func LoadProject(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := DecodeProject(data)
	if err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}
	return p, nil
}

// Validate reports every rule the project breaks on its own; Resolve
// checks it against the manifests.
func (p *Project) Validate() error {
	var l check.List
	if p.Format != Format {
		l.Addf("format", "must be %q, got %q", Format, p.Format)
	}
	if !check.Slug(p.ProjectID) {
		l.Addf("project_id", "must be a lower-case slug, got %q", p.ProjectID)
	}
	if !check.Text(p.Name) {
		l.Addf("name", "must be one non-empty line")
	}
	if !module.MatchString(p.GoModule) {
		l.Addf("go_module", "must be a module path like example.com/app, got %q", p.GoModule)
	}
	seen := map[string]bool{}
	for i, r := range p.Plugins {
		pp := fmt.Sprintf("plugins[%d]", i)
		if !check.Slug(r.Name) {
			l.Addf(pp, "name must be a lower-case slug, got %q", r.Name)
		}
		if seen[r.Name] {
			l.Addf(pp, "plugin %q is picked twice", r.Name)
		}
		seen[r.Name] = true
		if !check.Semver(r.Version) {
			l.Addf(pp, "version must be MAJOR.MINOR.PATCH, got %q", r.Version)
		}
		if _, err := r.options(); err != nil {
			l.Addf(pp+".options", "%v", err)
		}
	}
	for _, job := range sortedKeys(p.Routing) {
		if !jobName.MatchString(job) {
			l.Addf("routing", "job %q must be dotted snake_case", job)
		}
		if !Tiers[p.Routing[job]] {
			l.Addf("routing["+job+"]", "tier must be coder, routine or top, got %q", p.Routing[job])
		}
	}
	return l.Err()
}

func (r PluginRef) options() (map[string]json.RawMessage, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(r.Options), []byte("{")) {
		return nil, fmt.Errorf("must be a JSON object ({} for none)")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(r.Options, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Resolve checks a valid project against the manifests of the plugins it
// picks: each exists at the picked version, its requirements are picked at
// a high enough version without cycles, every option is declared and
// well-typed, and no two plugins contribute the same spec kind.
func (p *Project) Resolve(manifests map[string]*Manifest) error {
	var l check.List
	picked := map[string]PluginRef{}
	for _, r := range p.Plugins {
		picked[r.Name] = r
	}
	kinds := map[string]string{}
	for _, r := range p.Plugins {
		pp := "plugins[" + r.Name + "]"
		m := manifests[r.Name]
		if m == nil {
			l.Addf(pp, "no manifest for plugin %q", r.Name)
			continue
		}
		if m.Version != r.Version {
			l.Addf(pp, "picks version %s but the manifest is %s", r.Version, m.Version)
		}
		for _, req := range m.Requires {
			got, ok := picked[req.Plugin]
			if !ok {
				l.Addf(pp, "requires plugin %q, which the project does not pick", req.Plugin)
			} else if semverLess(got.Version, req.MinVersion) {
				l.Addf(pp, "requires %s >= %s, the project picks %s", req.Plugin, req.MinVersion, got.Version)
			}
		}
		opts, _ := r.options()
		for _, key := range sortedKeys(opts) {
			o := m.option(key)
			if o == nil {
				l.Addf(pp+".options", "%q is not an option of %s", key, r.Name)
			} else if err := typeCheck(o.Type, opts[key]); err != nil {
				l.Addf(pp+".options["+key+"]", "%v", err)
			}
		}
		for _, k := range m.SpecKinds {
			if other, dup := kinds[k.Kind]; dup {
				l.Addf(pp, "spec kind %q is also contributed by %s", k.Kind, other)
			}
			kinds[k.Kind] = r.Name
		}
	}
	if cycle := findCycle(p.Plugins, manifests); cycle != "" {
		l.Addf("plugins", "requirements form a cycle: %s", cycle)
	}
	return l.Err()
}

func findCycle(plugins []PluginRef, manifests map[string]*Manifest) string {
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) string
	visit = func(n string) string {
		switch state[n] {
		case visiting:
			for i, s := range stack {
				if s == n {
					return strings.Join(append(stack[i:], n), " -> ")
				}
			}
		case done:
			return ""
		}
		state[n] = visiting
		stack = append(stack, n)
		if m := manifests[n]; m != nil {
			for _, r := range m.Requires {
				if c := visit(r.Plugin); c != "" {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = done
		return ""
	}
	for _, r := range plugins {
		if c := visit(r.Name); c != "" {
			return c
		}
	}
	return ""
}

func semverLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3 && i < len(pa) && i < len(pb); i++ {
		var x, y int
		fmt.Sscan(pa[i], &x)
		fmt.Sscan(pb[i], &y)
		if x != y {
			return x < y
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
