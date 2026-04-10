package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
)

// Remove deinits and removes a submodule from the main repo, cleans up
// .git/modules, and removes the blob from config.
func Remove(blobName string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	if !cfg.HasBlob(blobName) {
		return fmt.Errorf("✗ blob '%s' not found in config", blobName)
	}

	submoduleName := cfg.SubmoduleName(blobName)

	if _, err := git.Run(cfg.Master.Repo, "submodule", "deinit", "-f", submoduleName); err != nil {
		return fmt.Errorf("✗ git submodule deinit: %w", err)
	}

	if _, err := git.Run(cfg.Master.Repo, "rm", "-f", submoduleName); err != nil {
		return fmt.Errorf("✗ git rm: %w", err)
	}

	modulesPath := filepath.Join(cfg.Master.Repo, ".git", "modules", submoduleName)
	if err := os.RemoveAll(modulesPath); err != nil {
		fmt.Printf("✗ could not clean .git/modules/%s: %v\n", submoduleName, err)
	}

	if _, err := git.Run(cfg.Master.Repo, "commit", "-m", "graft: remove submodule "+submoduleName); err != nil {
		return fmt.Errorf("✗ git commit: %w", err)
	}
	if _, err := git.Run(cfg.Master.Repo, "push"); err != nil {
		fmt.Printf("✗ could not push after remove: %v\n", err)
	}

	if err := cfg.RemoveBlob(blobName); err != nil {
		return fmt.Errorf("✗ cannot update config: %w", err)
	}

	fmt.Printf("✓ blob '%s' removed\n", blobName)
	return nil
}
