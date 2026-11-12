package wire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/briolink/briolink/adapter/codehost/github"
	"github.com/briolink/briolink/adapter/coder/ollama"
	"github.com/briolink/briolink/adapter/reasoner/claudecode"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/port/reasoner"
	"github.com/briolink/briolink/runner"
)

func table() *routing.Table {
	return &routing.Table{
		Tiers: map[string]string{"coder": "qwen", "routine": "sonnet", "top": "opus"},
		Jobs:  map[string]string{runner.Job: "coder"},
	}
}

func TestClaudeCodeTakesTheModelsOfTheReasonerTiers(t *testing.T) {
	r, err := ClaudeCode(Deps{Table: table()})
	if err != nil {
		t.Fatal(err)
	}
	m := r.(*claudecode.Reasoner).Models
	if len(m) != 2 || m[reasoner.Routine] != "sonnet" || m[reasoner.Top] != "opus" {
		t.Errorf("models %v", m)
	}
	tb := table()
	delete(tb.Tiers, "top")
	if _, err := ClaudeCode(Deps{Table: tb}); err == nil || !strings.Contains(err.Error(), `no model for the tier "top"`) {
		t.Errorf("err = %v", err)
	}
}

func TestOllamaTakesTheModelOfTheRunnersJob(t *testing.T) {
	c, err := Ollama(Deps{Table: table()})
	if err != nil || c.(*ollama.Coder).Model != "qwen" {
		t.Errorf("coder %v, %v", c, err)
	}
}

func TestAllBindsEveryPortAndNamesTheKnownOnesOnAMistake(t *testing.T) {
	cfg := &operator.Config{
		Adapters: operator.Adapters{CodeHost: "github", Coder: "ollama", Reasoner: "claudecode"},
		GitHub:   &operator.GitHub{Repo: "acme/site", AppID: 1, InstallationID: 2, KeySecret: "BOT_APP_KEY"},
	}
	var asked []string
	secret := func(ctx context.Context, actor, name string) ([]byte, error) {
		asked = append(asked, actor+" "+name)
		return nil, errors.New("sealed")
	}
	p, err := All(Deps{Cfg: cfg, Table: table(), Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.CodeHost.(*github.Host); !ok {
		t.Errorf("code host %T", p.CodeHost)
	}
	if _, ok := p.Coder.(*ollama.Coder); !ok {
		t.Errorf("coder %T", p.Coder)
	}
	if _, ok := p.Reasoner.(*claudecode.Reasoner); !ok {
		t.Errorf("reasoner %T", p.Reasoner)
	}
	if len(asked) != 0 {
		t.Errorf("a secret was read while binding: %v", asked)
	}

	for port, tc := range map[string]struct {
		set  func(*operator.Adapters)
		want string
	}{
		"codehost": {func(a *operator.Adapters) { a.CodeHost = "x" }, `adapters.codehost: no adapter "x" (known: fake, github)`},
		"coder":    {func(a *operator.Adapters) { a.Coder = "x" }, `adapters.coder: no adapter "x" (known: fake, ollama)`},
		"reasoner": {func(a *operator.Adapters) { a.Reasoner = "x" }, `adapters.reasoner: no adapter "x" (known: claudecode, fake)`},
	} {
		c := *cfg
		tc.set(&c.Adapters)
		if _, err := All(Deps{Cfg: &c, Table: table(), Secret: secret}); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", port, err, tc.want)
		}
	}
	c := *cfg
	c.GitHub = nil
	if _, err := All(Deps{Cfg: &c, Table: table(), Secret: secret}); err == nil || !strings.Contains(err.Error(), `adapters.codehost: github: the "github" section is required`) {
		t.Errorf("err = %v", err)
	}
}

func TestLoadKey(t *testing.T) {
	dir := t.TempDir()
	env := func(k string) string {
		if k == "CREDENTIALS_DIRECTORY" {
			return dir
		}
		return ""
	}
	path := filepath.Join(dir, "k")
	if err := os.WriteFile(path, []byte(strings.Repeat("0a", 32)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(env, "k"); err == nil || !strings.Contains(err.Error(), "readable by other users") {
		t.Errorf("a world-readable key: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if key, err := LoadKey(env, "k"); err != nil || len(key) != 32 {
		t.Errorf("key %d bytes, %v", len(key), err)
	}
	if _, err := LoadKey(func(string) string { return "" }, "k"); err == nil || !strings.Contains(err.Error(), "CREDENTIALS_DIRECTORY is not set") {
		t.Errorf("no directory: %v", err)
	}
	if _, err := LoadKey(env, "missing"); err == nil {
		t.Error("a missing credential was accepted")
	}
}
