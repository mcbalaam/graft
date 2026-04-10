package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// This clones an existing blob into the current directory.
// Without args: finds blob whose configured path matches cwd.
// With name: clones that blob here; asks to reassign path if it differs
// (unless immutable — then clones anyway and notes that config won't be updated).
func Here(blobName string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	cwd, err := git.AbsPath(".")
	if err != nil {
		return fmt.Errorf("✗ cannot resolve current directory: %w", err)
	}

	if blobName == "" {
		name, ok := cfg.HasBlobPath(cwd)
		if !ok {
			return fmt.Errorf("✗ no blob configured for this path, use 'graft this <name>'")
		}
		blobName = name
	}

	blob, ok := cfg.Blobs[blobName]
	if !ok {
		return fmt.Errorf("✗ blob '%s' not found in config", blobName)
	}

	submoduleName := cfg.SubmoduleName(blobName)
	remoteURL, err := git.SubmoduleURL(cfg.Repo, submoduleName)
	if err != nil {
		return fmt.Errorf("✗ cannot find remote for blob '%s': %w", blobName, err)
	}

	targetDir := cwd

	blobAbsPath, _ := filepath.Abs(blob.Path)
	if blobAbsPath != cwd {
		if blob.Immutable {
			fmt.Printf("blob '%s' is immutable (configured path: %s)\n", blobName, blob.Path)
			fmt.Println("  cloning here, config will not be updated")
		} else {
			choice, err := prompt.Query(
				fmt.Sprintf("● config path differs\n  config:  %s\n  current: %s", blob.Path, cwd),
				[]string{
					"clone here and update config path",
					"clone to config path instead",
					"cancel",
				},
				0,
			)
			if err != nil {
				return fmt.Errorf("✗ prompt failed: %w", err)
			}
			switch choice {
			case 0:
				if err := cfg.UpdateBlobPath(blobName, cwd); err != nil {
					return fmt.Errorf("✗ cannot update config: %w", err)
				}
			case 1:
				if _, err := os.Stat(blobAbsPath); os.IsNotExist(err) {
					return fmt.Errorf("✗ path '%s' does not exist, use 'graft apply --force %s'", blobAbsPath, blobName)
				}
				targetDir = blobAbsPath
			case 2:
				fmt.Println("cancelled")
				return nil
			}
		}
	}

	if _, err := git.Run(targetDir, "clone", remoteURL, "."); err != nil {
		return fmt.Errorf("✗ git clone: %w", err)
	}

	fmt.Printf("✓ blob '%s' cloned to %s\n", blobName, targetDir)
	return nil
}
