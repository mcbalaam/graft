package commands

import (
	"fmt"
	"os"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
)

// Init creates the main graft repo: git init, write config, initial commit, push.
func Init(remote, baseURL, repoPath string) error {
	if git.IsRepo(repoPath) {
		return fmt.Errorf("✗ repo already exists at %s", repoPath)
	}

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

	if _, err := config.Init(remote, repoPath, baseURL); err != nil {
		return fmt.Errorf("✗ cannot create config: %w", err)
	}

	// create a marker file so the initial commit is not empty
	marker := repoPath + "/.graft"
	if err := os.WriteFile(marker, []byte("# managed by graft\n"), 0644); err != nil {
		return fmt.Errorf("✗ cannot create marker file: %w", err)
	}

	if err := run("add", ".graft"); err != nil {
		return fmt.Errorf("✗ git add: %w", err)
	}
	if err := run("commit", "-m", "graft: init"); err != nil {
		return fmt.Errorf("✗ git commit: %w", err)
	}

	if err := run("remote", "add", "origin", remote); err != nil {
		return fmt.Errorf("✗ git remote add: %w", err)
	}
	if err := run("push", "--set-upstream", "origin", "master"); err != nil {
		return fmt.Errorf("✗ git push: %w", err)
	}

	fmt.Printf("✓ graft initialised at %s\n", repoPath)
	fmt.Printf("  remote:   %s\n", remote)
	fmt.Printf("  base_url: %s\n", baseURL)
	return nil
}
