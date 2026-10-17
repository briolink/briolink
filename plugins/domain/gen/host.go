package gen

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/briolink/briolink/generate"
	"github.com/briolink/briolink/plugins/domain/spec"
)

// Host is the domain plugin's generator behind the generator host's
// interface: it loads the files the manifest's "domain" spec kind names
// (spec/domain/*.json) and calls Generate.
type Host struct{}

var _ generate.Generator = Host{}

// Generate implements generate.Generator for the "domain" spec kind.
func (Host) Generate(_ context.Context, in generate.Input) (map[string][]byte, error) {
	files, err := filepath.Glob(filepath.Join(in.Root, filepath.FromSlash(in.Kind.Files)))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var contexts []*spec.Context
	for _, f := range files {
		c, err := spec.Load(f)
		if err != nil {
			return nil, err
		}
		contexts = append(contexts, c)
	}
	if len(contexts) == 0 {
		return nil, fmt.Errorf("no files match %s", in.Kind.Files)
	}
	return Generate(Input{Module: in.Project.GoModule, Contexts: contexts})
}
