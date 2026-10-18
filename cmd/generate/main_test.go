package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepoGenIsUpToDate is the regenerate-and-diff gate on this repo's own gen/.
func TestRepoGenIsUpToDate(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-root", filepath.Join("..", ".."), "-check"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("output = %q", out.String())
	}
}

// tempProject copies what the generators read (and gen/) into a temporary root.
func tempProject(t *testing.T) string {
	t.Helper()
	src, dst := filepath.Join("..", ".."), t.TempDir()
	for _, dir := range []string{"spec", "migrations", "gen"} {
		copyTree(t, filepath.Join(src, dir), filepath.Join(dst, dir), nil)
	}
	copyTree(t, filepath.Join(src, "plugins"), filepath.Join(dst, "plugins"), func(p string) bool { return filepath.Base(p) == "plugin.json" })
	return dst
}

func copyTree(t *testing.T, from, to string, keep func(string) bool) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || (keep != nil && !keep(p)) {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(to, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckFailsOnAStaleFileAndGenerateRepairsIt(t *testing.T) {
	root := tempProject(t)
	stale := filepath.Join(root, "gen", "domain", "control", "types.go")
	f, err := os.OpenFile(stale, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\n// edited by hand\n")
	f.Close()
	if err := os.WriteFile(filepath.Join(root, "gen", "stray.go"), []byte("// Code generated. DO NOT EDIT.\npackage x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = run(context.Background(), []string{"-root", root, "-check"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "domain/control/types.go") || !strings.Contains(err.Error(), "stray.go") {
		t.Fatalf("check err = %v", err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-root", root}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "wrote 12 files") {
		t.Errorf("output = %q", out.String())
	}
	if err := run(context.Background(), []string{"-root", root, "-check"}, &bytes.Buffer{}); err != nil {
		t.Errorf("check after generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "gen", "stray.go")); !os.IsNotExist(err) {
		t.Errorf("generate left the stray file: %v", err)
	}
}

func TestGenerateFailsWithoutAProject(t *testing.T) {
	if err := run(context.Background(), []string{"-root", t.TempDir()}, &bytes.Buffer{}); err == nil {
		t.Fatal("generated without a spec/project.json")
	}
	if err := run(context.Background(), []string{"-bogus"}, &bytes.Buffer{}); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
}
