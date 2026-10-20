package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/store"
)

const secretValue = "ghp_the-value-nobody-may-see-0123456789"

func TestSecretCommands(t *testing.T) {
	cfg, dbPath := operatorJSON(t, func(s string) string { return s })
	e := credentials(t, masterKey)

	out, err := runAdmin(t, cfg, e, secretValue+"\n", "secret", "put", "MIRROR_GH_TOKEN", "-expires", "2027-10-17", "-remind", "2027-09-17")
	if err != nil || out != "stored MIRROR_GH_TOKEN\n" {
		t.Fatalf("put: %q, %v", out, err)
	}
	if _, err := runAdmin(t, cfg, e, "other", "secret", "put", "MIRROR_GH_TOKEN"); err == nil {
		t.Error("a second put replaced a secret")
	}
	if _, err := runAdmin(t, cfg, e, "x", "secret", "put", "CF_API_TOKEN"); err != nil {
		t.Fatal(err)
	}
	out, err = runAdmin(t, cfg, e, "", "secret", "list")
	if err != nil || strings.Contains(out, secretValue) || !strings.Contains(out, "MIRROR_GH_TOKEN\tok\texpires 2027-10-17\tremind 2027-09-17\n") || !strings.Contains(out, "CF_API_TOKEN\tok\texpires -\tremind -\n") {
		t.Errorf("list: %q, %v", out, err)
	}
	if out, err = runAdmin(t, cfg, e, secretValue+"2", "secret", "rotate", "-expires", "2028-10-17", "MIRROR_GH_TOKEN"); err != nil || out != "rotated MIRROR_GH_TOKEN\n" {
		t.Errorf("rotate: %q, %v", out, err)
	}
	if _, err = runAdmin(t, cfg, e, "x", "secret", "rotate", "NOPE"); err == nil {
		t.Error("rotated a secret that does not exist")
	}
	if out, err = runAdmin(t, cfg, e, "", "secret", "delete", "CF_API_TOKEN"); err != nil || out != "deleted CF_API_TOKEN\n" {
		t.Errorf("delete: %q, %v", out, err)
	}

	// The server starts with them sealed, and its first line counts them.
	s, err := store.Open(context.Background(), dbPath, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	log, _ := s.ListAudit(context.Background(), 100)
	var actions []string
	for _, l := range log {
		if strings.Contains(l.Detail+l.Subject+l.Actor+l.Action, secretValue) {
			t.Errorf("the value is in the audit log: %+v", l)
		}
		actions = append(actions, l.Actor+" "+l.Action+" "+l.Subject)
	}
	want := "local:founder secret.put MIRROR_GH_TOKEN"
	if !strings.Contains(strings.Join(actions, "\n"), want) {
		t.Errorf("no %q in\n%s", want, strings.Join(actions, "\n"))
	}
}

func TestAWrongMasterKeyStopsTheServer(t *testing.T) {
	cfg, _ := operatorJSON(t, func(s string) string { return s })
	if _, err := runAdmin(t, cfg, credentials(t, masterKey), "v", "secret", "put", "K"); err != nil {
		t.Fatal(err)
	}
	other := credentials(t, strings.Repeat("ab", 32))
	// The server opens every secret at start, so another key stops it there.
	err := run(context.Background(), []string{"-config", cfg}, env(other), nil, &bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "master key does not open the secret store") || !strings.Contains(err.Error(), "K") {
		t.Errorf("start under a wrong key: %v", err)
	}
}

func TestTokenCommands(t *testing.T) {
	cfg, _ := operatorJSON(t, func(s string) string { return s })
	e := credentials(t, masterKey)
	out, err := runAdmin(t, cfg, e, "", "token", "issue", "ci", "-expires", "2099-01-01", "-remind", "2098-12-01")
	if err != nil || !strings.HasPrefix(out, "blt_") {
		t.Fatalf("issue: %q, %v", out, err)
	}
	tok, _, _ := strings.Cut(out, "\n")
	list, err := runAdmin(t, cfg, e, "", "token", "list")
	if err != nil || list != "ci\tscope operator\tok\texpires 2099-01-01\tremind 2098-12-01\n" {
		t.Errorf("list: %q, %v", list, err)
	}
	if strings.Contains(list, tok) {
		t.Error("list shows the token")
	}
	if _, err := runAdmin(t, cfg, e, "", "token", "issue", "again", "-scope", "root"); err == nil {
		t.Error("issued a token for a role that does not exist")
	}
	if _, err := runAdmin(t, cfg, e, "", "token", "issue", "late", "-expires", "tomorrow"); err == nil {
		t.Error("accepted a date that is not a day")
	}
	if out, err = runAdmin(t, cfg, e, "", "token", "revoke", "ci"); err != nil || out != "revoked ci\n" {
		t.Errorf("revoke: %q, %v", out, err)
	}
	if list, _ = runAdmin(t, cfg, e, "", "token", "list"); !strings.Contains(list, "ci\tscope operator\trevoked\t") {
		t.Errorf("list after revoke: %q", list)
	}
}

func TestAdminUsage(t *testing.T) {
	cfg, _ := operatorJSON(t, func(s string) string { return s })
	e := credentials(t, masterKey)
	for _, args := range [][]string{{"secret"}, {"nope", "x"}, {"secret", "put"}, {"secret", "put", "a", "b"}, {"token", "revoke"}, {"token", "frob", "x"}, {"secret", "frob", "x"}} {
		if _, err := runAdmin(t, cfg, e, "v", args...); err == nil {
			t.Errorf("%v: accepted", args)
		}
	}
}

func TestTheMasterKeyFileMustBePrivate(t *testing.T) {
	cfg, _ := operatorJSON(t, func(s string) string { return s })
	e := credentials(t, masterKey)
	path := filepath.Join(e["CREDENTIALS_DIRECTORY"], "master-key")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdmin(t, cfg, e, "v", "secret", "put", "K"); err == nil || !strings.Contains(err.Error(), "readable by its owner only") {
		t.Errorf("a world-readable key: %v", err)
	}
	if err := run(context.Background(), []string{"-config", cfg}, env(e), nil, &bytes.Buffer{}, nil); err == nil {
		t.Error("the server started on a world-readable key")
	}
}
