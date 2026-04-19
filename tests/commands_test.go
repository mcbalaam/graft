package tests_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcbalaam/graft/internal/commands"
	"github.com/mcbalaam/graft/internal/git"
)

// --- undo / reset ---

func TestUndoUnknownBlob(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	setupGitBlob(t, "nginx")

	if err := commands.Undo("nonexistent"); err == nil {
		t.Fatal("expected error for unknown blob")
	}
}

func TestResetUnknownBlob(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	setupGitBlob(t, "nginx")

	if err := commands.Reset("nonexistent"); err == nil {
		t.Fatal("expected error for unknown blob")
	}
}

func TestUndoPromptRequiresConfirmation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	noInteractive(t)
	setupGitBlob(t, "nginx")

	err := commands.Undo("nginx")
	if err == nil {
		t.Fatal("expected error: destructive prompt has no default")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResetPromptRequiresConfirmation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	noInteractive(t)

	_, blobPath, _ := setupGitBlob(t, "nginx")
	os.WriteFile(filepath.Join(blobPath, "test.conf"), []byte("dirty"), 0644)

	err := commands.Reset("nginx")
	if err == nil {
		t.Fatal("expected error: destructive prompt has no default")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- pull ---

func TestPullAlreadyUpToDate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	setupGitBlob(t, "nginx")

	if err := commands.Pull("nginx", false); err != nil {
		t.Fatalf("Pull on up-to-date blob: %v", err)
	}
}

func TestPullForceResetsToRemote(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))

	_, blobPath, _ := setupGitBlob(t, "nginx")
	os.WriteFile(filepath.Join(blobPath, "test.conf"), []byte("dirty"), 0644)

	if err := commands.Pull("nginx", true); err != nil {
		t.Fatalf("Pull --force: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(blobPath, "test.conf"))
	if string(content) != "v1" {
		t.Errorf("after force reset: content = %q, want %q", string(content), "v1")
	}
}

func TestPullConflictDefaultsToSkip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	noInteractive(t)

	blobPath := setupConflict(t)
	setupCfg(t, filepath.Join(tmp, "main"), "nginx", blobPath)

	err := commands.Pull("nginx", false)
	if err == nil {
		t.Fatal("expected error: conflict should not be auto-resolved")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConflictTheirsGitSequence(t *testing.T) {
	blobPath := setupConflict(t)

	exec.Command("git", "-C", blobPath, "fetch", "origin").Run()
	exec.Command("git", "-C", blobPath, "merge", "origin/HEAD").Run()

	run := git.Run
	run(blobPath, "checkout", "--theirs", ".")
	run(blobPath, "add", ".")
	if _, err := run(blobPath, "commit", "-m", "graft: resolve conflict (theirs)"); err != nil {
		t.Fatalf("theirs resolution: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(blobPath, "file.conf"))
	if !strings.Contains(string(content), "remote content") {
		t.Errorf("expected remote content after --theirs, got: %q", string(content))
	}
}

func TestConflictOursGitSequence(t *testing.T) {
	blobPath := setupConflict(t)

	exec.Command("git", "-C", blobPath, "fetch", "origin").Run()
	exec.Command("git", "-C", blobPath, "merge", "origin/HEAD").Run()

	run := git.Run
	run(blobPath, "checkout", "--ours", ".")
	run(blobPath, "add", ".")
	if _, err := run(blobPath, "commit", "-m", "graft: resolve conflict (ours)"); err != nil {
		t.Fatalf("ours resolution: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(blobPath, "file.conf"))
	if !strings.Contains(string(content), "local content") {
		t.Errorf("expected local content after --ours, got: %q", string(content))
	}
}
