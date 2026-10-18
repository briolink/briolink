// Command generate runs the generators of the plugins spec/project.json picks
// over the repo's own specs into gen/, or with -check fails when gen/ is out
// of date (the regenerate-and-diff gate, M1). It is the composition root for
// the generators: the host carries none, so they are registered here.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/briolink/briolink/core/project"
	"github.com/briolink/briolink/generate"
	domaingen "github.com/briolink/briolink/plugins/domain/gen"
	httpgen "github.com/briolink/briolink/plugins/http/gen"
	sqlitegen "github.com/briolink/briolink/plugins/sqlite/gen"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	root := fs.String("root", ".", "the project root, holding spec/ and plugins/")
	check := fs.Bool("check", false, "fail when gen/ differs from the generated files instead of writing them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := project.LoadProject(filepath.Join(*root, "spec", "project.json"))
	if err != nil {
		return err
	}
	manifests, err := generate.LoadManifests(filepath.Join(*root, "plugins"), p)
	if err != nil {
		return err
	}
	reg := generate.Registry{
		generate.Key("domain", "domain"):  domaingen.Host{},
		generate.Key("http", "routes"):    httpgen.Host{},
		generate.Key("sqlite", "queries"): sqlitegen.Host{},
	}
	if *check {
		if err := generate.Check(ctx, *root, p, manifests, reg); err != nil {
			return err
		}
		fmt.Fprintln(out, "gen/ is up to date")
		return nil
	}
	files, err := generate.Run(ctx, *root, p, manifests, reg)
	if err != nil {
		return err
	}
	if err := generate.Write(*root, files); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %d files to gen/\n", len(files))
	return nil
}
