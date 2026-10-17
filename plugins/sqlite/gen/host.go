package gen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/briolink/briolink/generate"
	"github.com/briolink/briolink/plugins/sqlite/spec"
)

// Package is the name of the Go package the host's output is generated as.
const Package = "sqlitegen"

// Host is the sqlite plugin's generator behind the generator host's
// interface: it reads the project's migrations/*.sql (in file-name order)
// and spec/queries/*.sql from the project root and calls Generate.
type Host struct{}

var _ generate.Generator = Host{}

// Generate implements generate.Generator for the "queries" spec kind.
func (Host) Generate(ctx context.Context, in generate.Input) (map[string][]byte, error) {
	files, err := filepath.Glob(filepath.Join(in.Root, "migrations", "*.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var schema strings.Builder
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		schema.Write(src)
		schema.WriteString("\n")
	}
	queries, err := spec.LoadDir(filepath.Join(in.Root, "spec", "queries"))
	if err != nil {
		return nil, fmt.Errorf("spec/queries:\n%w", err)
	}
	return Generate(ctx, Input{Package: Package, Schema: schema.String(), Queries: queries})
}
