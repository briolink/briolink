// Package wire is the part of the composition root that cmd/control and
// cmd/runner share: the name each port's adapter has in operator.json, how an
// adapter is built from the config, and how the master key is read. Like
// cmd/*, it is the only kind of package that names adapters (A1, A2); being
// under cmd/internal, nothing else can import it.
package wire

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/briolink/briolink/adapter/codehost/github"
	"github.com/briolink/briolink/adapter/coder/ollama"
	"github.com/briolink/briolink/adapter/reasoner/claudecode"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/vault"
	"github.com/briolink/briolink/port/codehost"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/port/reasoner"
	"github.com/briolink/briolink/runner"
)

// Ports are the adapters an installation holds, one per port.
type Ports struct {
	CodeHost codehost.Host
	Coder    coder.Coder
	Reasoner reasoner.Reasoner
}

// Deps is what an adapter is built from: the operator config, the routing
// table, and the secret store's Use for the secrets it needs (each use is
// audited under the actor given, the port's name).
type Deps struct {
	Cfg    *operator.Config
	Table  *routing.Table
	Secret func(ctx context.Context, actor, name string) ([]byte, error)
}

// The adapters each port can be bound to, by the name operator.json gives.
// Real adapters are added here as they pass their port's contract tests.
var (
	CodeHosts = map[string]func(Deps) (codehost.Host, error){
		"fake":   func(Deps) (codehost.Host, error) { return codehost.NewFake(), nil },
		"github": GitHub,
	}
	Coders = map[string]func(Deps) (coder.Coder, error){
		"fake":   func(Deps) (coder.Coder, error) { return &coder.Fake{}, nil },
		"ollama": Ollama,
	}
	Reasoners = map[string]func(Deps) (reasoner.Reasoner, error){
		"fake":       func(Deps) (reasoner.Reasoner, error) { return &reasoner.Fake{}, nil },
		"claudecode": ClaudeCode,
	}
)

// GitHub binds the code host to the repository in operator.json's github
// section, acting as the App (A22): its key is read from the secret store on
// each token mint, never kept.
func GitHub(d Deps) (codehost.Host, error) {
	g := d.Cfg.GitHub
	if g == nil {
		return nil, errors.New(`the "github" section is required`)
	}
	return &github.Host{Repo: g.Repo, Tokens: &github.TokenSource{
		AppID:          g.AppID,
		InstallationID: g.InstallationID,
		Key:            func(ctx context.Context) ([]byte, error) { return d.Secret(ctx, "codehost", g.KeySecret) },
	}}, nil
}

// Ollama binds the coder to the model the routing table gives the runner's
// job; Ollama is on the ops host.
func Ollama(d Deps) (coder.Coder, error) {
	model, ok := d.Table.Tiers[d.Table.Jobs[runner.Job]]
	if !ok {
		return nil, fmt.Errorf("the routing table has no model for the job %q", runner.Job)
	}
	return &ollama.Coder{Model: model}, nil
}

// ClaudeCode binds the reasoner to headless Claude Code on the founder's
// login: each reasoner tier of the routing table (routine, top) is the model
// the table names for it.
func ClaudeCode(d Deps) (reasoner.Reasoner, error) {
	models := map[reasoner.Tier]string{}
	for _, tier := range []reasoner.Tier{reasoner.Routine, reasoner.Top} {
		model, ok := d.Table.Tiers[string(tier)]
		if !ok || model == "" {
			return nil, fmt.Errorf("the routing table has no model for the tier %q", tier)
		}
		models[tier] = model
	}
	return &claudecode.Reasoner{Models: models}, nil
}

// Pick builds the adapter the config names for a port, or says which names
// there are.
func Pick[T any](port, name string, from map[string]func(Deps) (T, error), d Deps) (T, error) {
	var zero T
	f, ok := from[name]
	if !ok {
		known := make([]string, 0, len(from))
		for k := range from {
			known = append(known, k)
		}
		sort.Strings(known)
		return zero, fmt.Errorf("adapters.%s: no adapter %q (known: %s)", port, name, strings.Join(known, ", "))
	}
	v, err := f(d)
	if err != nil {
		return zero, fmt.Errorf("adapters.%s: %s: %w", port, name, err)
	}
	return v, nil
}

// All builds the adapter of every port.
func All(d Deps) (Ports, error) {
	var p Ports
	var err error
	a := d.Cfg.Adapters
	if p.CodeHost, err = Pick("codehost", a.CodeHost, CodeHosts, d); err != nil {
		return p, err
	}
	if p.Coder, err = Pick("coder", a.Coder, Coders, d); err != nil {
		return p, err
	}
	p.Reasoner, err = Pick("reasoner", a.Reasoner, Reasoners, d)
	return p, err
}

// LoadKey reads the master key from the systemd credential of that name. A
// file other users can read is refused: the key is root-only (A20).
func LoadKey(getenv func(string) string, credential string) ([]byte, error) {
	dir := getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return nil, fmt.Errorf("CREDENTIALS_DIRECTORY is not set: the master key (%s) comes from a systemd credential", credential)
	}
	path := filepath.Join(dir, credential)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master key %s is readable by other users (mode %v): it must be root-only", path, info.Mode().Perm())
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	key, err := vault.ParseKey(text)
	if err != nil {
		return nil, fmt.Errorf("master key %s: %w", path, err)
	}
	return key, nil
}
