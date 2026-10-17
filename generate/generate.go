// Package generate is the generator host: it runs the generators of the
// plugins a project picks, in dependency order, over the project's spec
// files, and returns the files that belong in the project's gen/ directory
// (docs/2-architecture.md § Specs and generators, A5). It carries no
// generator itself; each plugin supplies one per spec kind and cmd/*
// registers them.
package generate

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/project"
)

// Input is what a generator gets for one spec kind of one plugin.
type Input struct {
	Root     string // the project's root directory
	Project  *project.Project
	Plugin   project.PluginRef
	Manifest *project.Manifest
	Kind     project.SpecKind
}

// Generator is the one interface every plugin's generator implements. It
// returns its files keyed by slash-separated name, relative to the
// plugin's own directory in gen/, and writes nothing itself. Each file
// must carry a DO NOT EDIT header.
type Generator interface {
	Generate(ctx context.Context, in Input) (map[string][]byte, error)
}

// Registry maps "<plugin>/<kind>" to the generator for that spec kind.
type Registry map[string]Generator

// Key is the Registry key of a plugin's spec kind.
func Key(plugin, kind string) string { return plugin + "/" + kind }

// headerLines is how far into a file the DO NOT EDIT marker may sit.
const headerLines = 3

// Order returns the picked plugins' names in dependency order: a plugin
// comes after every plugin it requires, and otherwise in the order the
// project lists them. It fails, naming the plugin or the cycle, when a
// manifest is missing, a requirement is not picked, or requirements loop.
func Order(p *project.Project, manifests map[string]*project.Manifest) ([]string, error) {
	if err := p.Resolve(manifests); err != nil {
		return nil, err
	}
	var order []string
	done := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if done[n] {
			return
		}
		done[n] = true
		for _, r := range manifests[n].Requires {
			visit(r.Plugin)
		}
		order = append(order, n)
	}
	for _, r := range p.Plugins {
		visit(r.Name)
	}
	return order, nil
}

// LoadManifests reads plugins/<name>/plugin.json under pluginsDir for each
// plugin the project picks. A plugin without a manifest is left out so
// Resolve reports it by name.
func LoadManifests(pluginsDir string, p *project.Project) (map[string]*project.Manifest, error) {
	out := map[string]*project.Manifest{}
	for _, r := range p.Plugins {
		file := filepath.Join(pluginsDir, r.Name, "plugin.json")
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue
		}
		m, err := project.LoadManifest(file)
		if err != nil {
			return nil, err
		}
		out[r.Name] = m
	}
	return out, nil
}

// Run generates every file of the project at root. Files are keyed by
// slash-separated path relative to the project's gen/ directory, each
// under its plugin's name. Nothing is written; a failing generator, a
// missing one, a file without the header or two files on one path fail the
// whole run with every problem listed.
func Run(ctx context.Context, root string, p *project.Project, manifests map[string]*project.Manifest, reg Registry) (map[string][]byte, error) {
	order, err := Order(p, manifests)
	if err != nil {
		return nil, err
	}
	refs := map[string]project.PluginRef{}
	for _, r := range p.Plugins {
		refs[r.Name] = r
	}
	var l check.List
	files := map[string][]byte{}
	for _, name := range order {
		m := manifests[name]
		for _, k := range m.SpecKinds {
			at := "plugin " + name + ", kind " + k.Kind
			g := reg[Key(name, k.Kind)]
			if g == nil {
				l.Addf(at, "no generator registered")
				continue
			}
			out, err := g.Generate(ctx, Input{Root: root, Project: p, Plugin: refs[name], Manifest: m, Kind: k})
			if err != nil {
				l.Addf(at, "%v", err)
				continue
			}
			for _, rel := range sortedNames(out) {
				if !check.RelPath(rel) || path.Clean(rel) != rel {
					l.Addf(at, "file %q must be a clean relative path", rel)
					continue
				}
				if !hasHeader(out[rel]) {
					l.Addf(at, "file %q has no DO NOT EDIT header in its first %d lines", rel, headerLines)
				}
				full := path.Join(name, rel)
				if _, dup := files[full]; dup {
					l.Addf(at, "file %q is generated twice", full)
				}
				files[full] = out[rel]
			}
		}
	}
	if err := l.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func hasHeader(b []byte) bool {
	lines := bytes.SplitN(b, []byte("\n"), headerLines+1)
	if len(lines) > headerLines {
		lines = lines[:headerLines]
	}
	for _, line := range lines {
		if bytes.Contains(line, []byte("DO NOT EDIT")) {
			return true
		}
	}
	return false
}

// Diff compares files with the project's gen/ directory and returns the
// paths (relative to gen/) that differ: changed, missing from gen/, or in
// gen/ without being generated. Empty means gen/ is current.
func Diff(root string, files map[string][]byte) ([]string, error) {
	dir := filepath.Join(root, "gen")
	var bad []string
	onDisk := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == dir {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		onDisk[rel] = true
		got, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		want, ok := files[rel]
		switch {
		case !ok:
			bad = append(bad, rel+" (not generated)")
		case !bytes.Equal(got, want):
			bad = append(bad, rel+" (differs)")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, rel := range sortedNames(files) {
		if !onDisk[rel] {
			bad = append(bad, rel+" (missing)")
		}
	}
	sort.Strings(bad)
	return bad, nil
}

// Check is the regenerate-and-diff gate the runner uses: it generates in
// memory, never touching gen/, and fails listing the paths that differ.
func Check(ctx context.Context, root string, p *project.Project, manifests map[string]*project.Manifest, reg Registry) error {
	files, err := Run(ctx, root, p, manifests, reg)
	if err != nil {
		return err
	}
	bad, err := Diff(root, files)
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("gen/ is out of date, run the generators:\n  %s", strings.Join(bad, "\n  "))
	}
	return nil
}

// Write replaces the project's gen/ directory with files.
func Write(root string, files map[string][]byte) error {
	dir := filepath.Join(root, "gen")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	for _, rel := range sortedNames(files) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[rel], 0o644); err != nil {
			return err
		}
	}
	return nil
}

func sortedNames(m map[string][]byte) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
