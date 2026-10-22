package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/adapter/codehost/github"
	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/store"
)

func TestVersionDefault(t *testing.T) {
	if version == "" {
		t.Fatal("version must not be empty")
	}
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-version"}, nil, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "briolink control "+version+"\n" {
		t.Errorf("output = %q", got)
	}
}

// operatorJSON writes an operator.json for a temporary installation on
// loopback, using the repo's own routing table, and returns its path. The
// text @LISTEN@ in the file is a free loopback address unless mod changes it.
func operatorJSON(t *testing.T, mod func(string) string) (path, dbPath string) {
	t.Helper()
	dir := t.TempDir()
	routingPath, err := filepath.Abs(filepath.Join("..", "..", "spec", "routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	dbPath = filepath.Join(dir, "control.db")
	cfg, _ := json.Marshal(map[string]any{
		"format": "v0", "listen": "@LISTEN@", "tls": nil, "database": dbPath,
		"routing_file": routingPath, "master_key_credential": "master-key",
		"adapters": map[string]string{"codehost": "fake", "coder": "fake", "reasoner": "fake"},
	})
	path = filepath.Join(dir, "operator.json")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(mod(string(cfg)), "@LISTEN@", freeAddr(t))), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, dbPath
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

const goodKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// credentials writes the master key as a systemd credential with the given
// mode and returns the environment that points at it.
func credentials(t *testing.T, key string, mode os.FileMode) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "master-key"), []byte(key), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "master-key"), mode); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"CREDENTIALS_DIRECTORY": dir}
}

// command runs an admin command against a config and returns what it printed.
func command(t *testing.T, cfg string, e map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), append([]string{"-config", cfg}, args...), env(e), strings.NewReader(stdin), &out, nil)
	return out.String(), err
}

func TestStartsOnFakesServesAndStops(t *testing.T) {
	cfg, dbPath := operatorJSON(t, func(s string) string { return s })
	creds := credentials(t, goodKey+"\n", 0o400)
	tok, err := command(t, cfg, creds, "", "token", "create", "test", "-expires", "2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	tok = strings.TrimSpace(tok)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := make(chan string, 1)
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() {
		done <- run(ctx, []string{"-config", cfg}, env(creds), nil, &out, func(a string) { addr <- a })
	}()
	var base string
	select {
	case a := <-addr:
		base = "http://" + a
	case err := <-done:
		t.Fatalf("exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("did not start")
	}

	get := func(path, token string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", base+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body
	}
	if status, _ := get("/api/v1/routing", ""); status != 401 {
		t.Errorf("without a token: %d", status)
	}
	if status, _ := get("/api/v1/routing", "blk_nope"); status != 401 {
		t.Errorf("with an unknown token: %d", status)
	}
	status, body := get("/api/v1/routing", tok)
	if status != 200 || body["routing"].(map[string]any)["on_top_limit"] != "queue-design-jobs" {
		t.Errorf("routing: %d %v", status, body)
	}
	status, body = get("/api/v1/audit", tok)
	entries, _ := body["entries"].([]any)
	if status != 200 || len(entries) != 4 {
		t.Fatalf("audit: %d %v", status, body)
	}
	// newest first: this call, the routing call, start, token.create (the stranger left no line)
	if e := entries[2].(map[string]any); e["action"] != "start" || !strings.Contains(e["detail"].(string), "codehost=fake") {
		t.Errorf("start entry = %v", e)
	}
	if e := entries[0].(map[string]any); e["action"] != "token.use" || e["actor"] != "token:test" || e["subject"] != "GET /api/v1/audit" {
		t.Errorf("use entry = %v", e)
	}
	if !strings.Contains(out.String(), "listening on 127.0.0.1:") {
		t.Errorf("output = %q", out.String())
	}

	// An open event stream must not hold the shutdown up.
	req, _ := http.NewRequest("GET", base+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop")
	}

	s, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	log, err := s.ListAudit(context.Background(), 10)
	if err != nil || len(log) < 3 || log[0].Action != "stop" || log[1].Action != "token.use" {
		t.Errorf("audit after stop = %+v, %v", log, err)
	}
}

func TestRefusesToStart(t *testing.T) {
	good := func(t *testing.T) map[string]string { return credentials(t, goodKey, 0o400) }
	same := func(s string) string { return s }
	for name, tc := range map[string]struct {
		mod  func(string) string
		env  func(t *testing.T) map[string]string
		want string
	}{
		"no credentials directory": {same, func(*testing.T) map[string]string { return nil }, "CREDENTIALS_DIRECTORY is not set"},
		"no key file": {same, func(t *testing.T) map[string]string {
			return map[string]string{"CREDENTIALS_DIRECTORY": t.TempDir()}
		}, "master key"},
		"key readable by others": {same, func(t *testing.T) map[string]string { return credentials(t, goodKey, 0o644) }, "readable by other users"},
		"key too short":          {same, func(t *testing.T) map[string]string { return credentials(t, "abcd", 0o400) }, "64 hexadecimal digits"},
		"key not hex":            {same, func(t *testing.T) map[string]string { return credentials(t, strings.Repeat("zz", 32), 0o400) }, "64 hexadecimal digits"},
		"unknown adapter":        {func(s string) string { return strings.Replace(s, `"coder":"fake"`, `"coder":"bogus"`, 1) }, good, `adapters.coder: no adapter "bogus" (known: fake, ollama)`},
		"github, no section":     {func(s string) string { return strings.Replace(s, `"codehost":"fake"`, `"codehost":"github"`, 1) }, good, `adapters.codehost: github: the "github" section is required`},
		"wildcard listen":        {func(s string) string { return strings.Replace(s, "@LISTEN@", "0.0.0.0:8443", 1) }, good, "would listen publicly"},
		"missing routing":        {func(s string) string { return strings.Replace(s, "routing.json", "gone.json", 1) }, good, "gone.json"},
		"address taken":          {nil, good, "address already in use"},
		"unknown json key":       {func(s string) string { return strings.Replace(s, `"format"`, `"extra":1,"format"`, 1) }, good, `unknown field "extra"`},
	} {
		t.Run(name, func(t *testing.T) {
			mod := tc.mod
			if mod == nil { // bind a port first, then ask for it
				ln := listen(t)
				defer ln.Close()
				mod = func(s string) string { return strings.Replace(s, "@LISTEN@", ln.Addr().String(), 1) }
			}
			cfg, _ := operatorJSON(t, mod)
			_, err := command(t, cfg, tc.env(t), "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	if _, err := command(t, filepath.Join(t.TempDir(), "none.json"), nil, ""); err == nil {
		t.Error("a missing config started")
	}
}

func TestSecretAndTokenCommands(t *testing.T) {
	cfg, dbPath := operatorJSON(t, func(s string) string { return s })
	creds := credentials(t, goodKey, 0o400)
	do := func(stdin string, args ...string) string {
		t.Helper()
		out, err := command(t, cfg, creds, stdin, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	var seen strings.Builder // everything the commands printed

	seen.WriteString(do("ghp_the-value\n", "secret", "put", "MIRROR_GH_TOKEN", "-expires", "2027-10-17", "-remind", "2027-09-17"))
	seen.WriteString(do("ghp_the-value-2", "secret", "rotate", "MIRROR_GH_TOKEN", "-expires", "2028-10-17"))
	tok := strings.TrimSpace(do("", "token", "create", "ci", "-role", "operator"))
	if !strings.HasPrefix(tok, "blk_") {
		t.Fatalf("token = %q", tok)
	}
	list := do("", "credentials")
	seen.WriteString(list)
	for _, want := range []string{"secret", "MIRROR_GH_TOKEN", "2028-10-17", "token", "ci", "operator"} {
		if !strings.Contains(list, want) {
			t.Errorf("credentials lacks %q:\n%s", want, list)
		}
	}
	if exp := do("", "credentials", "-expiring"); strings.Contains(exp, "MIRROR_GH_TOKEN") {
		t.Errorf("a secret expiring in 2028 is listed as expiring:\n%s", exp)
	}
	seen.WriteString(do("", "token", "revoke", "ci"))
	seen.WriteString(do("", "secret", "delete", "MIRROR_GH_TOKEN"))
	if list := do("", "credentials"); strings.Contains(list, "MIRROR_GH_TOKEN") || !strings.Contains(list, "revoked") {
		t.Errorf("after delete and revoke:\n%s", list)
	}

	for name, tc := range map[string]struct {
		stdin string
		args  []string
		want  string
	}{
		"empty value":     {"", []string{"secret", "put", "X"}, "needs a name"},
		"bad date":        {"v", []string{"secret", "put", "X", "-expires", "tomorrow"}, "not a date"},
		"unknown secret":  {"v", []string{"secret", "rotate", "X"}, "not found"},
		"unknown role":    {"", []string{"token", "create", "t", "-role", "root"}, "not a role"},
		"unknown command": {"", []string{"frobnicate"}, "commands:"},
		"missing name":    {"", []string{"secret", "put"}, "commands:"},
	} {
		if _, err := command(t, cfg, creds, tc.stdin, tc.args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}

	if strings.Contains(seen.String(), "ghp_the-value") || strings.Contains(seen.String(), tok) {
		t.Errorf("a command printed a secret or a token:\n%s", seen.String())
	}
	s, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	log, err := s.ListAudit(context.Background(), 20)
	if err != nil || len(log) != 5 {
		t.Fatalf("audit = %+v, %v", log, err)
	}
	var actions []string
	for i := len(log) - 1; i >= 0; i-- {
		actions = append(actions, log[i].Actor+" "+log[i].Action+" "+log[i].Subject)
		if strings.Contains(log[i].Detail+log[i].Subject, "ghp_") || strings.Contains(log[i].Detail, tok) {
			t.Errorf("audit leaks: %+v", log[i])
		}
	}
	want := "cli secret.put MIRROR_GH_TOKEN,cli secret.rotate MIRROR_GH_TOKEN,cli token.create ci,cli token.revoke ci,cli secret.delete MIRROR_GH_TOKEN"
	if strings.Join(actions, ",") != want {
		t.Errorf("audit actions = %v", actions)
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

// freeAddr is a loopback address nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln := listen(t)
	defer ln.Close()
	return ln.Addr().String()
}

func TestWrongMasterKeyIsCaughtBeforeAnythingIsSealedOrServed(t *testing.T) {
	cfg, dbPath := operatorJSON(t, func(s string) string { return s })
	right := credentials(t, goodKey, 0o400)
	wrong := credentials(t, strings.Repeat("ab", 32), 0o400)
	if _, err := command(t, cfg, right, "kept-value", "secret", "put", "KEPT"); err != nil {
		t.Fatal(err)
	}

	for name, args := range map[string][]string{
		"serve":  nil,
		"put":    {"secret", "put", "NEW"},
		"rotate": {"secret", "rotate", "KEPT"},
	} {
		_, err := command(t, cfg, wrong, "new-value", args...)
		if err == nil || !strings.Contains(err.Error(), "1 of the stored secrets do not open (KEPT)") {
			t.Errorf("%s under the wrong key: %v", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "value") {
			t.Errorf("%s: the error holds a value: %v", name, err)
		}
	}

	// What needs no key still works, so a lost key does not lock the operator out.
	if out, err := command(t, cfg, wrong, "", "credentials"); err != nil || !strings.Contains(out, "KEPT") {
		t.Errorf("credentials: %q, %v", out, err)
	}
	if _, err := command(t, cfg, wrong, "", "secret", "delete", "KEPT"); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := command(t, cfg, wrong, "v", "secret", "put", "AFTER"); err != nil {
		t.Errorf("put after the unreadable secret is gone: %v", err)
	}

	// Nothing the refused commands tried to do was kept.
	s, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	log, _ := s.ListAudit(context.Background(), 20)
	var actions []string
	for i := len(log) - 1; i >= 0; i-- {
		actions = append(actions, log[i].Action+" "+log[i].Subject)
	}
	if want := "secret.put KEPT,secret.delete KEPT,secret.put AFTER"; strings.Join(actions, ",") != want {
		t.Errorf("audit = %v", actions)
	}
}

func TestTokenRolesFromTheCommand(t *testing.T) {
	cfg, _ := operatorJSON(t, func(s string) string { return s })
	creds := credentials(t, goodKey, 0o400)
	for _, role := range []string{"operator", "admin"} {
		if _, err := command(t, cfg, creds, "", "token", "create", "t-"+role, "-role", role); err != nil {
			t.Errorf("role %s: %v", role, err)
		}
	}
	out, _ := command(t, cfg, creds, "", "credentials")
	if !strings.Contains(out, "admin") || !strings.Contains(out, "operator") {
		t.Errorf("credentials:\n%s", out)
	}
}

func TestCredentialExpiryCheck(t *testing.T) {
	cfg, _ := operatorJSON(t, func(s string) string { return s })
	creds := credentials(t, goodKey, 0o400)
	defer func(f func() time.Time) { now = f }(now)
	now = func() time.Time { return time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC) }
	do := func(stdin string, args ...string) string {
		t.Helper()
		out, err := command(t, cfg, creds, stdin, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	if out := do("", "credentials", "-check"); !strings.Contains(out, "nothing to report") {
		t.Errorf("empty vault:\n%s", out)
	}
	var seen strings.Builder
	seen.WriteString(do("far-value", "secret", "put", "FAR", "-expires", "2028-01-01", "-remind", "2027-12-01"))
	seen.WriteString(do("due-value", "secret", "put", "DUE", "-expires", "2026-12-31", "-remind", "2026-10-21"))
	seen.WriteString(do("old-value", "secret", "put", "OLD", "-expires", "2026-10-01"))
	tok := strings.TrimSpace(do("", "token", "create", "ci", "-expires", "2026-11-01"))
	seen.WriteString(tok)
	gone := strings.TrimSpace(do("", "token", "create", "gone", "-expires", "2026-10-01"))
	seen.WriteString(gone)
	do("", "token", "revoke", "gone")

	out := do("", "credentials", "-check")
	want := "credentials: expired secret OLD expires 2026-10-01\n" +
		"credentials: remind token ci expires 2026-11-01\n" +
		"credentials: remind secret DUE expires 2026-12-31\n"
	if out != want {
		t.Errorf("check =\n%s\nwant\n%s", out, want)
	}
	for _, secret := range []string{"far-value", "due-value", "old-value", tok, gone} {
		if strings.Contains(out, secret) {
			t.Errorf("the check printed a value or token")
		}
	}
	if _, err := command(t, cfg, creds, "", "credentials", "-check", "-expiring"); err == nil {
		t.Error("-check with -expiring was accepted")
	}
}

func TestGitHubReadsTheBotKeyFromTheStore(t *testing.T) {
	cfg := &operator.Config{GitHub: &operator.GitHub{Repo: "acme/site", AppID: 1, InstallationID: 2, KeySecret: "BOT_APP_KEY"}}
	var asked []string
	h, err := newGitHub(deps{cfg: cfg, secret: func(ctx context.Context, actor, name string) ([]byte, error) {
		asked = append(asked, actor+" "+name)
		return nil, errors.New("sealed")
	}})
	if err != nil {
		t.Fatal(err)
	}
	gh := h.(*github.Host)
	if gh.Repo != "acme/site" {
		t.Errorf("repo %q", gh.Repo)
	}
	if _, err := gh.GetIssue(context.Background(), 1); !errors.Is(err, github.ErrKey) {
		t.Errorf("got %v", err)
	}
	if len(asked) != 1 || asked[0] != "codehost BOT_APP_KEY" {
		t.Errorf("asked %v", asked)
	}
}
