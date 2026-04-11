package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// RepoAdd clones a remote graft repo locally and registers it.
func RepoAdd(remote string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	name, err := prompt.Ask("repo name (leave empty for 'default'): ")
	if err != nil {
		return fmt.Errorf("✗ prompt failed: %w", err)
	}
	if name == "" {
		name = "default"
	}

	repos := cfg.Repos()
	if _, exists := repos[name]; exists {
		return fmt.Errorf("✗ repo '%s' already registered", name)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("✗ cannot determine home directory: %w", err)
	}
	localPath := filepath.Join(home, ".local", "share", "graft-"+name)

	if err := os.MkdirAll(localPath, 0755); err != nil {
		return fmt.Errorf("✗ cannot create directory: %w", err)
	}

	out, err := git.Run(localPath, "clone", remote, ".")
	if err != nil {
		_ = os.RemoveAll(localPath)
		return fmt.Errorf("✗ git clone: %w: %s", err, out)
	}

	if err := cfg.AddRepo(name, localPath); err != nil {
		return fmt.Errorf("✗ cannot save config: %w", err)
	}

	fmt.Printf("✓ repo '%s' added at %s\n", name, localPath)
	return nil
}

// RepoRemove unregisters a repo and optionally deletes its local directory.
func RepoRemove(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	repos := cfg.Repos()
	localPath, ok := repos[name]
	if !ok {
		return fmt.Errorf("✗ repo '%s' not found", name)
	}

	if cfg.ActiveName() == name {
		return fmt.Errorf("✗ cannot remove active repo — switch to another first")
	}

	if err := cfg.RemoveRepo(name); err != nil {
		return fmt.Errorf("✗ cannot update config: %w", err)
	}

	choice, err := prompt.Query(
		fmt.Sprintf("● delete local directory %s?", localPath),
		[]string{"yes, delete", "no, keep"},
		0,
	)
	if err != nil {
		return fmt.Errorf("✗ prompt failed: %w", err)
	}
	if choice == 0 {
		if err := os.RemoveAll(localPath); err != nil {
			return fmt.Errorf("✗ cannot delete directory: %w", err)
		}
		fmt.Printf("✓ repo '%s' removed and directory deleted\n", name)
	} else {
		fmt.Printf("✓ repo '%s' removed (directory kept at %s)\n", name, localPath)
	}

	return nil
}

// RepoList prints all registered repos, marking the active one.
func RepoList() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	repos := cfg.Repos()
	active := cfg.ActiveName()

	fmt.Println("repos:")
	for name, path := range repos {
		if name == active {
			fmt.Printf("  ● %s  %s  (active)\n", name, path)
		} else {
			fmt.Printf("    %s  %s\n", name, path)
		}
	}
	return nil
}
