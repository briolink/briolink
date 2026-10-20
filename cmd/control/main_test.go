package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
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

const masterKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// credentials writes the master key as a systemd would hand it over, and
// returns the environment that points at it.
func credentials(t *testing.T, key string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "master-key"), []byte(key+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"CREDENTIALS_DIRECTORY": dir, "USER": "founder"}
}

// runAdmin runs a local command and returns what it printed.
func runAdmin(t *testing.T, cfg string, e map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), append([]string{"-config", cfg}, args...), env(e), strings.NewReader(stdin), &out, nil)
	return out.String(), err
}

func TestStartsOnFakesServesAndStops(t *testing.T) {
	cfg, dbPath := operatorJSON(t, func(s string) string { return s })
	e := credentials(t, masterKey)
	issued, err := runAdmin(t, cfg, e, "", "token", "issue", "ci")
	if err != nil {
		t.Fatal(err)
	}
	tok, _, _ := strings.Cut(issued, "\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := make(chan string, 1)
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() {
		done <- run(ctx, []string{"-config", cfg}, env(e), nil, &out, func(a string) { addr <- a })
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
	if status, _ := get("/api/v1/routing", "blt_wrong"); status != 401 {
		t.Errorf("with a wrong token: %d", status)
	}
	status, body := get("/api/v1/routing", tok)
	if status != 200 || body["routing"].(map[string]any)["on_top_limit"] != "queue-design-jobs" {
		t.Errorf("routing: %d %v", status, body)
	}
	status, body = get("/api/v1/audit", tok)
	entries, _ := body["entries"].([]any)
	if status != 200 || len(entries) < 4 {
		t.Fatalf("audit: %d %v", status, body)
	}
	// Newest first: the audit request's own use and the routing one, the
	// refused wrong token, the unauthenticated request not logged, then start.
	var start map[string]any
	for _, x := range entries {
		if e := x.(map[string]any); e["action"] == "start" {
			start = e
		}
	}
	if start == nil || !strings.Contains(start["detail"].(string), "codehost=fake") || !strings.Contains(start["detail"].(string), "secrets=0") {
		t.Errorf("start entry = %v", start)
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
	log, err := s.ListAudit(context.Background(), 100)
	if err != nil || len(log) < 3 || log[0].Action != "stop" {
		t.Fatalf("audit after stop = %+v, %v", log, err)
	}
	for _, e := range log {
		if strings.Contains(e.Detail, tok) || strings.Contains(e.Subject, tok) {
			t.Errorf("the token is in the audit log: %+v", e)
		}
	}
}

func TestRefusesToStart(t *testing.T) {
	for name, tc := range map[string]struct {
		mod   func(string) string
		creds bool // the master key is handed over
		want  string
	}{
		"no credentials":   {func(s string) string { return s }, false, "CREDENTIALS_DIRECTORY is not set"},
		"unknown adapter":  {func(s string) string { return strings.Replace(s, `"coder":"fake"`, `"coder":"ollama"`, 1) }, true, `adapters.coder: no adapter "ollama" (known: fake)`},
		"wildcard listen":  {func(s string) string { return strings.Replace(s, "@LISTEN@", "0.0.0.0:8443", 1) }, true, "would listen publicly"},
		"missing routing":  {func(s string) string { return strings.Replace(s, "routing.json", "gone.json", 1) }, true, "gone.json"},
		"address taken":    {nil, true, "address already in use"},
		"unknown json key": {func(s string) string { return strings.Replace(s, `"format"`, `"extra":1,"format"`, 1) }, true, `unknown field "extra"`},
	} {
		t.Run(name, func(t *testing.T) {
			var e map[string]string
			if tc.creds {
				e = credentials(t, masterKey)
			}
			mod := tc.mod
			if mod == nil { // bind a port first, then ask for it
				ln := listen(t)
				defer ln.Close()
				mod = func(s string) string { return strings.Replace(s, "@LISTEN@", ln.Addr().String(), 1) }
			}
			cfg, _ := operatorJSON(t, mod)
			err := run(context.Background(), []string{"-config", cfg}, env(e), nil, &bytes.Buffer{}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	if err := run(context.Background(), []string{"-config", filepath.Join(t.TempDir(), "none.json")}, env(nil), nil, &bytes.Buffer{}, nil); err == nil {
		t.Error("a missing config started")
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
