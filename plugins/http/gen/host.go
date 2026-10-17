package gen

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/briolink/briolink/core/strictjson"
	"github.com/briolink/briolink/generate"
	domain "github.com/briolink/briolink/plugins/domain/spec"
	"github.com/briolink/briolink/plugins/http/spec"
)

// domainFiles is where the domain plugin's spec kind keeps its contexts;
// routes are resolved against them (the manifest requires that plugin).
const domainFiles = "spec/domain/*.json"

// Host is the http plugin's generator behind the generator host's
// interface: it loads the file the manifest's "routes" spec kind names and
// the domain contexts it resolves against, reads the plugin's options and
// calls Generate.
type Host struct{}

var _ generate.Generator = Host{}

// Generate implements generate.Generator for the "routes" spec kind.
func (Host) Generate(_ context.Context, in generate.Input) (map[string][]byte, error) {
	opts, err := ParseOptions(in.Plugin.Options)
	if err != nil {
		return nil, fmt.Errorf("options: %w", err)
	}
	routes, err := spec.Load(filepath.Join(in.Root, filepath.FromSlash(in.Kind.Files)))
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(in.Root, filepath.FromSlash(domainFiles)))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	contexts := map[string]*domain.Context{}
	for _, f := range files {
		c, err := domain.Load(f)
		if err != nil {
			return nil, err
		}
		contexts[c.Context] = c
	}
	return Generate(Input{Module: in.Project.GoModule, Options: opts, Routes: routes, Contexts: contexts})
}

// ParseOptions decodes the plugin's options from spec/project.json and
// fills the manifest's defaults.
func ParseOptions(raw []byte) (Options, error) {
	o := Options{BasePath: "/", ErrorFormat: "problem"}
	if len(raw) == 0 {
		return o, nil
	}
	var in struct {
		BasePath    *string `json:"base_path"`
		ErrorFormat *string `json:"error_format"`
	}
	if err := strictjson.Decode(raw, &in); err != nil {
		return o, err
	}
	if in.BasePath != nil {
		o.BasePath = *in.BasePath
	}
	if in.ErrorFormat != nil {
		o.ErrorFormat = *in.ErrorFormat
	}
	return o, o.validate()
}
