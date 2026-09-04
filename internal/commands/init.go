package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Init creates the main graft repo: git init, write configs, initial commit, push.
// Pre-flight checks run before any mutation; on mid-flow failure everything
// that was already done is rolled back.
func Init(remote, repoPath string) error {
	if git.IsRepo(repoPath) {
		return fmt.Errorf("✗ repo already exists at %s", repoPath)
	}

	if err := checkRemoteAccess("backup", "", remote); err != nil {
		return fmt.Errorf("✗ %w", err)
	}

	input, err := prompt.Ask("repo name [master]: ")
	if err != nil {
		return fmt.Errorf("✗ prompt failed: %w", err)
	}
	name := strings.TrimSpace(input)
	if name == "" {
		name = "master"
	}

	visChoice, err := prompt.Query(
		"● default visibility for new blobs?",
		[]string{"private", "public"},
		0,
	)
	if err != nil {
		return fmt.Errorf("✗ prompt failed: %w", err)
	}
	defaultPublic := visChoice == 1

	tx := &rollback{verbose: config.LoadVerbose()}
	cfg, err := initRepo(remote, repoPath, name, defaultPublic, tx)
	if err != nil {
		tx.undo()
		return err
	}
	tx.commit()

	promptAndSaveToken(cfg)

	fmt.Printf("✓ graft initialized at %s\n", repoPath)
	fmt.Printf("  name:     %s\n", name)
	fmt.Printf("  remote:   %s\n", remote)
	fmt.Printf("  base_url: %s\n", cfg.Master.BaseURL)
	return nil
}

func initRepo(remote, repoPath, name string, defaultPublic bool, tx *rollback) (*config.Config, error) {
	localPath, err := config.LocalPath()
	if err != nil {
		return nil, fmt.Errorf("✗ cannot resolve config path: %w", err)
	}
	undoLocal, err := backupFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("✗ cannot backup config: %w", err)
	}
	tx.push("local config "+localPath, undoLocal)

	repoConfigPath := filepath.Join(repoPath, "graft.toml")
	undoRepoCfg, err := backupFile(repoConfigPath)
	if err != nil {
		return nil, fmt.Errorf("✗ cannot backup repo config: %w", err)
	}
	tx.push("repo config "+repoConfigPath, undoRepoCfg)

	createdDir := !pathExists(repoPath)
	if createdDir {
		tx.push("created directory "+repoPath, func() error { return os.RemoveAll(repoPath) })
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			return nil, fmt.Errorf("✗ cannot create repo directory: %w", err)
		}
	}
	tx.push("git init", func() error { return os.RemoveAll(filepath.Join(repoPath, ".git")) })

	run := func(args ...string) error {
		out, err := git.Run(repoPath, args...)
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}

	if err := run("init"); err != nil {
		return nil, fmt.Errorf("✗ git init: %w", err)
	}

	cfg, err := config.Init(remote, repoPath, name, defaultPublic)
	if err != nil {
		return nil, fmt.Errorf("✗ cannot create config: %w", err)
	}

	if err := run("add", "graft.toml"); err != nil {
		return nil, fmt.Errorf("✗ git add: %w", err)
	}
	if err := run("commit", "-m", "graft: init"); err != nil {
		return nil, fmt.Errorf("✗ git commit: %w", err)
	}
	if err := run("remote", "add", "origin", remote); err != nil {
		return nil, fmt.Errorf("✗ git remote add: %w", err)
	}
	if err := run("push", "--set-upstream", "origin", "HEAD"); err != nil {
		return nil, fmt.Errorf("✗ git push: %w", err)
	}
	return cfg, nil
}
