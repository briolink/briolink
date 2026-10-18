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
	if err := run(context.Background(), []string{"-version"}, nil, &out, nil); err != nil {
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
		"routing_file": routingPath, "token_env": "CONTROL_TEST_TOKEN",
		"adapters": map[string]string{"codehost": "fake", "coder": "fake", "reasoner": "fake"},
	})
	path = filepath.Join(dir, "operator.json")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(mod(string(cfg)), "@LISTEN@", freeAddr(t))), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, dbPath
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestStartsOnFakesServesAndStops(t *testing.T) {
	cfg, dbPath := operatorJSON(t, func(s string) string { return s })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := make(chan string, 1)
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() {
		done <- run(ctx, []string{"-config", cfg}, env(map[string]string{"CONTROL_TEST_TOKEN": "tok"}), &out, func(a string) { addr <- a })
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
	status, body := get("/api/v1/routing", "tok")
	if status != 200 || body["routing"].(map[string]any)["on_top_limit"] != "queue-design-jobs" {
		t.Errorf("routing: %d %v", status, body)
	}
	status, body = get("/api/v1/audit", "tok")
	entries, _ := body["entries"].([]any)
	if status != 200 || len(entries) != 1 {
		t.Fatalf("audit: %d %v", status, body)
	}
	if e := entries[0].(map[string]any); e["action"] != "start" || !strings.Contains(e["detail"].(string), "codehost=fake") {
		t.Errorf("start entry = %v", e)
	}
	if !strings.Contains(out.String(), "listening on 127.0.0.1:") {
		t.Errorf("output = %q", out.String())
	}

	// An open event stream must not hold the shutdown up.
	req, _ := http.NewRequest("GET", base+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer tok")
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
	if err != nil || len(log) != 2 || log[0].Action != "stop" || log[1].Action != "start" {
		t.Errorf("audit after stop = %+v, %v", log, err)
	}
}

func TestRefusesToStart(t *testing.T) {
	for name, tc := range map[string]struct {
		mod  func(string) string
		env  map[string]string
		want string
	}{
		"no token":         {func(s string) string { return s }, nil, "CONTROL_TEST_TOKEN (token_env) is empty"},
		"unknown adapter":  {func(s string) string { return strings.Replace(s, `"coder":"fake"`, `"coder":"ollama"`, 1) }, map[string]string{"CONTROL_TEST_TOKEN": "t"}, `adapters.coder: no adapter "ollama" (known: fake)`},
		"wildcard listen":  {func(s string) string { return strings.Replace(s, "@LISTEN@", "0.0.0.0:8443", 1) }, map[string]string{"CONTROL_TEST_TOKEN": "t"}, "would listen publicly"},
		"missing routing":  {func(s string) string { return strings.Replace(s, "routing.json", "gone.json", 1) }, map[string]string{"CONTROL_TEST_TOKEN": "t"}, "gone.json"},
		"address taken":    {nil, map[string]string{"CONTROL_TEST_TOKEN": "t"}, "address already in use"},
		"unknown json key": {func(s string) string { return strings.Replace(s, `"format"`, `"extra":1,"format"`, 1) }, map[string]string{"CONTROL_TEST_TOKEN": "t"}, `unknown field "extra"`},
	} {
		t.Run(name, func(t *testing.T) {
			mod := tc.mod
			if mod == nil { // bind a port first, then ask for it
				ln := listen(t)
				defer ln.Close()
				mod = func(s string) string { return strings.Replace(s, "@LISTEN@", ln.Addr().String(), 1) }
			}
			cfg, _ := operatorJSON(t, mod)
			err := run(context.Background(), []string{"-config", cfg}, env(tc.env), &bytes.Buffer{}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	if err := run(context.Background(), []string{"-config", filepath.Join(t.TempDir(), "none.json")}, env(nil), &bytes.Buffer{}, nil); err == nil {
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
