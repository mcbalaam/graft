package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
)

// List prints all blobs tracked in the active repo.
func List() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	fmt.Printf("[%s] blobs:\n", cfg.ActiveName())

	if len(cfg.Blobs) == 0 {
		fmt.Println("  (none)")
		return nil
	}

	// measure longest name for alignment
	maxLen := 0
	for name := range cfg.Blobs {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	for name, blob := range cfg.Blobs {
		var flags []string
		if blob.Sudo {
			flags = append(flags, "sudo")
		}
		if blob.Meta {
			flags = append(flags, "meta")
		}
		if blob.Immutable {
			flags = append(flags, "immutable")
		}

		flagStr := ""
		if len(flags) > 0 {
			flagStr = "  " + strings.Join(flags, " ")
		}

		fmt.Printf("  %-*s  %s%s\n", maxLen, name, blob.Path, flagStr)
	}
	return nil
}
