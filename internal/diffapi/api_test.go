package diffapi_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mutech-Innovation/coding-anywhere/internal/diffapi"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	}
	for _, args := range cmds {
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
}

func gitAddCommit(t *testing.T, dir, msg string) {
	t.Helper()
	if err := exec.Command("git", "-C", dir, "add", "-A").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", dir, "commit", "-q", "-m", msg).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestListChangesDetectsAddedAndModified(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddCommit(t, dir, "init")

	// Modify a.txt and add an untracked file.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	api, err := diffapi.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !api.IsRepo(context.Background()) {
		t.Fatal("expected IsRepo=true")
	}
	changes, err := api.ListChanges(context.Background())
	if err != nil {
		t.Fatalf("ListChanges: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(changes), changes)
	}
	kinds := map[string]string{}
	for _, c := range changes {
		kinds[c.Path] = string(c.Kind)
	}
	if kinds["a.txt"] != "modified" {
		t.Errorf("a.txt kind: %s", kinds["a.txt"])
	}
	if kinds["b.txt"] != "untracked" {
		t.Errorf("b.txt kind: %s", kinds["b.txt"])
	}
}

func TestVersionsReturnsHeadAndCurrent(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddCommit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v2\nplus\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	api, err := diffapi.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	original, current, err := api.Versions(context.Background(), "a.txt")
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if !strings.Contains(original, "v1") {
		t.Fatalf("original missing v1: %q", original)
	}
	if !strings.Contains(current, "v2") {
		t.Fatalf("current missing v2: %q", current)
	}
}

func TestVersionsHandlesNewFile(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	// Create initial commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddCommit(t, dir, "seed")
	// New untracked file.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	api, err := diffapi.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	original, current, err := api.Versions(context.Background(), "new.txt")
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if original != "" {
		t.Fatalf("original should be empty for new file, got %q", original)
	}
	if !strings.Contains(current, "brand new") {
		t.Fatalf("current: %q", current)
	}
}

func TestNonRepoReturnsError(t *testing.T) {
	api, err := diffapi.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.ListChanges(context.Background()); !errors.Is(err, diffapi.ErrNotARepo) {
		t.Fatalf("expected ErrNotARepo, got %v", err)
	}
}
