package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/strictjson"
)

// OptionTypes are the types a plugin option may have.
var OptionTypes = map[string]bool{"string": true, "int": true, "bool": true, "[]string": true}

var migration = regexp.MustCompile(`^migrations/([0-9]{4})_[a-z0-9_]+\.sql$`)

// Manifest is plugins/<name>/plugin.json. Every path in it is relative to
// the plugin's directory.
type Manifest struct {
	Format        string        `json:"format"`
	Name          string        `json:"name"`
	Version       string        `json:"version"`
	Purpose       string        `json:"purpose"`
	Options       []Option      `json:"options"`
	Ports         []string      `json:"ports"`
	Requires      []Requirement `json:"requires"`
	SpecKinds     []SpecKind    `json:"spec_kinds"`
	SpecFragments []string      `json:"spec_fragments"`
	Migrations    []string      `json:"migrations"`
	Templates     []string      `json:"templates"`
	WebModules    []string      `json:"web_modules"`
}

// Option is one setting a project may give the plugin.
type Option struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Default json.RawMessage `json:"default"`
	Doc     string          `json:"doc"`
}

// Requirement names another plugin this one needs, at MinVersion or later.
type Requirement struct {
	Plugin     string `json:"plugin"`
	MinVersion string `json:"min_version"`
}

// SpecKind is a kind of spec file the plugin owns and its generator: Files
// is a glob from the project root, Generator a Go package in the plugin.
type SpecKind struct {
	Kind      string `json:"kind"`
	Files     string `json:"files"`
	Generator string `json:"generator"`
}

// DecodeManifest decodes data strictly and validates it.
func DecodeManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := strictjson.Decode(data, &m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadManifest reads and decodes a plugin.json.
func LoadManifest(file string) (*Manifest, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	m, err := DecodeManifest(data)
	if err != nil {
		return nil, fmt.Errorf("%s:\n%w", file, err)
	}
	return m, nil
}

func (m *Manifest) option(name string) *Option {
	for i := range m.Options {
		if m.Options[i].Name == name {
			return &m.Options[i]
		}
	}
	return nil
}

// Validate reports every rule the manifest breaks.
func (m *Manifest) Validate() error {
	var l check.List
	if m.Format != Format {
		l.Addf("format", "must be %q, got %q", Format, m.Format)
	}
	if !check.Slug(m.Name) {
		l.Addf("name", "must be a lower-case slug, got %q", m.Name)
	}
	if !check.Semver(m.Version) {
		l.Addf("version", "must be MAJOR.MINOR.PATCH, got %q", m.Version)
	}
	if !check.Text(m.Purpose) {
		l.Addf("purpose", "must be one non-empty line")
	}
	seen := map[string]bool{}
	for _, o := range m.Options {
		p := "options[" + o.Name + "]"
		if !check.Snake(o.Name) {
			l.Addf(p, "name must be snake_case")
		}
		if seen[o.Name] {
			l.Addf(p, "option is declared twice")
		}
		seen[o.Name] = true
		if !OptionTypes[o.Type] {
			l.Addf(p, "type must be string, int, bool or []string, got %q", o.Type)
		} else if err := typeCheck(o.Type, o.Default); err != nil {
			l.Addf(p+".default", "%v", err)
		}
	}
	unique(&l, "ports", m.Ports, check.Snake, "snake_case")
	seen = map[string]bool{}
	for _, r := range m.Requires {
		p := "requires[" + r.Plugin + "]"
		if !check.Slug(r.Plugin) {
			l.Addf(p, "plugin must be a lower-case slug")
		}
		if r.Plugin == m.Name {
			l.Addf(p, "a plugin cannot require itself")
		}
		if seen[r.Plugin] {
			l.Addf(p, "required twice")
		}
		seen[r.Plugin] = true
		if !check.Semver(r.MinVersion) {
			l.Addf(p, "min_version must be MAJOR.MINOR.PATCH, got %q", r.MinVersion)
		}
	}
	seen = map[string]bool{}
	for _, k := range m.SpecKinds {
		p := "spec_kinds[" + k.Kind + "]"
		if !check.Snake(k.Kind) {
			l.Addf(p, "kind must be snake_case")
		}
		if seen[k.Kind] {
			l.Addf(p, "kind is declared twice")
		}
		seen[k.Kind] = true
		if _, err := path.Match(k.Files, ""); err != nil || !check.RelPath(k.Files) {
			l.Addf(p+".files", "must be a relative glob, got %q", k.Files)
		}
		if !check.RelPath(k.Generator) {
			l.Addf(p+".generator", "must be a relative path inside the plugin, got %q", k.Generator)
		}
	}
	unique(&l, "spec_fragments", m.SpecFragments, check.RelPath, "a relative path inside the plugin")
	unique(&l, "templates", m.Templates, check.RelPath, "a relative path inside the plugin")
	unique(&l, "web_modules", m.WebModules, check.RelPath, "a relative path inside the plugin")
	last := 0
	for _, f := range m.Migrations {
		match := migration.FindStringSubmatch(f)
		if match == nil {
			l.Addf("migrations", "%q must be migrations/NNNN_name.sql", f)
			continue
		}
		n, _ := strconv.Atoi(match[1])
		if n <= last {
			l.Addf("migrations", "%q must be numbered after the one before it", f)
		}
		last = n
	}
	return l.Err()
}

func unique(l *check.List, p string, items []string, ok func(string) bool, what string) {
	seen := map[string]bool{}
	for _, s := range items {
		if !ok(s) {
			l.Addf(p, "%q must be %s", s, what)
		}
		if seen[s] {
			l.Addf(p, "%q is listed twice", s)
		}
		seen[s] = true
	}
}

// typeCheck reports whether raw is a JSON value of the option type t.
func typeCheck(t string, raw json.RawMessage) error {
	var err error
	switch t {
	case "string":
		var v string
		err = strictValue(raw, &v)
	case "int":
		var v int
		err = strictValue(raw, &v)
	case "bool":
		var v bool
		err = strictValue(raw, &v)
	case "[]string":
		var v []string
		err = strictValue(raw, &v)
	}
	if err != nil {
		return fmt.Errorf("must be of type %s, got %s", t, raw)
	}
	return nil
}

func strictValue(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return fmt.Errorf("null")
	}
	return json.Unmarshal(raw, v)
}
