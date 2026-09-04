package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackRunsStepsInReverse(t *testing.T) {
	var order []string
	tx := &rollback{}
	tx.push("first", func() error { order = append(order, "first"); return nil })
	tx.push("second", func() error { order = append(order, "second"); return nil })
	tx.undo()

	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("expected [second first], got %v", order)
	}
}

func TestRollbackCommitSkipsUndo(t *testing.T) {
	called := false
	tx := &rollback{}
	tx.push("thing", func() error { called = true; return nil })
	tx.commit()
	tx.undo()
	if called {
		t.Fatal("undo ran after commit")
	}
}

func TestRollbackUndoIdempotent(t *testing.T) {
	calls := 0
	tx := &rollback{}
	tx.push("thing", func() error { calls++; return nil })
	tx.undo()
	tx.undo()
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRollbackRunsCleanupsOnUndo(t *testing.T) {
	cleaned := false
	failed := false
	tx := &rollback{}
	tx.push("failing", func() error { failed = true; return errors.New("boom") })
	tx.onCommit(func() { cleaned = true })
	tx.undo()
	if !failed || !cleaned {
		t.Fatalf("undo must run step and cleanup: failed=%v cleaned=%v", failed, cleaned)
	}
}

func TestBackupFileRestoresContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.toml")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	undo, err := backupFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("expected old content, got %q", data)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0644 {
		t.Fatalf("expected mode 644, got %v", fi.Mode().Perm())
	}
}

func TestBackupFileRemovesCreatedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.toml")
	undo, err := backupFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("created"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should have been removed by undo")
	}
}

func TestStashGitDirRestores(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, ".git", "HEAD")
	if err := os.WriteFile(marker, []byte("ref: refs/heads/main"), 0644); err != nil {
		t.Fatal(err)
	}

	restore, discard, err := stashGitDir(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatal("stash should have moved .git aside")
	}
	// a new .git appears (simulating the fresh reinit)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if string(data) != "ref: refs/heads/main" {
		t.Fatalf("unexpected restored content: %q", data)
	}
	if err := discard(); err != nil {
		t.Fatal(err)
	}
}
