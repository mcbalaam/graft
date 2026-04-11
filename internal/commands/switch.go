package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Switch changes the active repo, checking for uncommitted changes first.
func Switch(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	if cfg.ActiveName() == name {
		fmt.Printf("already on '%s'\n", name)
		return nil
	}

	repos := cfg.Repos()
	if _, ok := repos[name]; !ok {
		return fmt.Errorf("✗ repo '%s' not found — use 'graft repo list' to see available repos", name)
	}

	// check for uncommitted changes in current blobs
	var dirty []string
	for blobName, blob := range cfg.Blobs {
		run := git.Run
		if blob.Sudo {
			run = git.RunSudo
		}
		out, err := run(blob.Path, "status", "--porcelain")
		if err != nil {
			continue // not a repo yet, skip
		}
		if strings.TrimSpace(out) != "" {
			dirty = append(dirty, blobName)
		}
	}

	if len(dirty) > 0 {
		fmt.Printf("● unsaved changes in: %s\n", strings.Join(dirty, ", "))
		choice, err := prompt.Query(
			"what to do?",
			[]string{
				"overwrite (discard changes)",
				"sync current repo first, then switch",
				"cancel",
			},
			2,
		)
		if err != nil {
			return fmt.Errorf("✗ prompt failed: %w", err)
		}
		switch choice {
		case 0:
			// proceed without syncing
		case 1:
			fmt.Println("pushing current repo...")
			if err := Push(""); err != nil {
				return fmt.Errorf("✗ push failed: %w", err)
			}
		case 2:
			fmt.Println("cancelled")
			return nil
		}
	}

	if err := cfg.SetActive(name); err != nil {
		return fmt.Errorf("✗ cannot switch: %w", err)
	}

	fmt.Printf("✓ switched to '%s'\n", name)
	fmt.Println("  run 'graft apply' to restore blobs from the new repo")
	return nil
}
