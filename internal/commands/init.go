package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Init creates the main graft repo: git init, write configs, initial commit, push.
func Init(remote, repoPath string) error {
	if git.IsRepo(repoPath) {
		return fmt.Errorf("✗ repo already exists at %s", repoPath)
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

	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("✗ cannot create repo directory: %w", err)
	}

	run := func(args ...string) error {
		out, err := git.Run(repoPath, args...)
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}

	if err := run("init"); err != nil {
		return fmt.Errorf("✗ git init: %w", err)
	}

	cfg, err := config.Init(remote, repoPath, name, defaultPublic)
	if err != nil {
		return fmt.Errorf("✗ cannot create config: %w", err)
	}

	if err := run("add", "graft.toml"); err != nil {
		return fmt.Errorf("✗ git add: %w", err)
	}
	if err := run("commit", "-m", "graft: init"); err != nil {
		return fmt.Errorf("✗ git commit: %w", err)
	}
	if err := run("remote", "add", "origin", remote); err != nil {
		return fmt.Errorf("✗ git remote add: %w", err)
	}
	if err := run("push", "--set-upstream", "origin", "HEAD"); err != nil {
		return fmt.Errorf("✗ git push: %w", err)
	}

	fmt.Printf("✓ graft initialized at %s\n", repoPath)
	fmt.Printf("  name:     %s\n", name)
	fmt.Printf("  remote:   %s\n", remote)
	fmt.Printf("  base_url: %s\n", cfg.Master.BaseURL)
	return nil
}
