package tests_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/prompt"
)

// setupGitBlob creates a bare remote and a working clone with one commit.
// Returns (remotePath, blobPath, cfg).
func setupGitBlob(t *testing.T, blobName string) (string, string, *config.Config) {
	t.Helper()
	tmp := t.TempDir()

	remotePath := filepath.Join(tmp, "remote.git")
	blobPath := filepath.Join(tmp, "blob")
	repoPath := filepath.Join(tmp, "main")

	for _, d := range []string{remotePath, blobPath, repoPath} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	git(remotePath, "init", "--bare")
	git(blobPath, "init")
	git(blobPath, "config", "user.email", "test@test.com")
	git(blobPath, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(blobPath, "test.conf"), []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	git(blobPath, "add", ".")
	git(blobPath, "commit", "-m", "init")
	git(blobPath, "remote", "add", "origin", remotePath)
	git(blobPath, "push", "-u", "origin", "HEAD")

	cfg := setupCfg(t, repoPath, blobName, blobPath)
	return remotePath, blobPath, cfg
}

// setupConflict creates a blob where pulling will produce a CONFLICT.
// remote has "remote content", local has "local content" — same line, different values.
func setupConflict(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	remotePath := filepath.Join(tmp, "remote.git")
	blobPath := filepath.Join(tmp, "blob")
	otherPath := filepath.Join(tmp, "other")

	for _, d := range []string{remotePath, blobPath, otherPath} {
		os.MkdirAll(d, 0755)
	}

	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}

	git(remotePath, "init", "--bare")
	git(blobPath, "init")
	git(blobPath, "config", "user.email", "test@test.com")
	git(blobPath, "config", "user.name", "test")
	os.WriteFile(filepath.Join(blobPath, "file.conf"), []byte("base\n"), 0644)
	git(blobPath, "add", ".")
	git(blobPath, "commit", "-m", "base")
	git(blobPath, "remote", "add", "origin", remotePath)
	git(blobPath, "push", "-u", "origin", "HEAD")

	git(otherPath, "clone", remotePath, ".")
	git(otherPath, "config", "user.email", "test@test.com")
	git(otherPath, "config", "user.name", "test")
	os.WriteFile(filepath.Join(otherPath, "file.conf"), []byte("remote content\n"), 0644)
	git(otherPath, "add", ".")
	git(otherPath, "commit", "-m", "remote change")
	git(otherPath, "push")

	os.WriteFile(filepath.Join(blobPath, "file.conf"), []byte("local content\n"), 0644)
	git(blobPath, "add", ".")
	git(blobPath, "commit", "-m", "local change")

	return blobPath
}

func setupCfg(t *testing.T, repoPath, blobName, blobPath string) *config.Config {
	t.Helper()
	localPath := filepath.Join(filepath.Dir(repoPath), "graft.toml")
	t.Setenv("GRAFT_CONFIG", localPath)

	cfg, err := config.Init("git@github.com:user/backup.git", repoPath, "default", false)
	if err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	if err := cfg.AddBlob(blobName, blobPath, false, false, false); err != nil {
		t.Fatalf("AddBlob: %v", err)
	}
	return cfg
}

func noInteractive(t *testing.T) {
	t.Helper()
	prompt.NoInteractive = true
	t.Cleanup(func() { prompt.NoInteractive = false })
}
