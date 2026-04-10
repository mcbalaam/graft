package commands

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
)

// Apply restores blob(s) by cloning them to the paths recorded in config.
// With sufficient --force applied, missing directories are created recursively.
func Apply(blobName string, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	if blobName != "" {
		blob, ok := cfg.Blobs[blobName]
		if !ok {
			return fmt.Errorf("✗ blob '%s' not found in config", blobName)
		}
		if err := applyOne(cfg, blobName, blob, force); err != nil {
			return fmt.Errorf("✗ %w", err)
		}
		return nil
	}

	// apply all — continue on errors, collect results (wil be displayed later)
	type result struct {
		name string
		err  error
	}
	var results []result
	for name, blob := range cfg.Blobs {
		err := applyOne(cfg, name, blob, force)
		results = append(results, result{name, err})
	}

	fmt.Println()
	fmt.Println("apply summary:")
	ok, failed := 0, 0
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("  ✗ %s: %v\n", r.name, r.err)
			failed++
		} else {
			fmt.Printf("  ✓ %s\n", r.name)
			ok++
		}
	}
	fmt.Printf("  %d ok, %d failed\n", ok, failed)

	if failed > 0 {
		return fmt.Errorf("✗ %d blob(s) failed to apply", failed)
	}
	return nil
}

func applyOne(cfg *config.Config, name string, blob config.Blob, force bool) error {
	path := blob.Path

	_, statErr := os.Stat(path)
	exists := !os.IsNotExist(statErr)

	if exists {
		if git.IsRepo(path) {
			fmt.Printf("  ~ %s: already applied, skipping\n", name)
			return nil
		}
		return fmt.Errorf("path '%s' exists but is not a git repo — remove it manually first", path)
	}

	if !force {
		return fmt.Errorf("path '%s' does not exist, use --force to create", path)
	}
	if blob.Sudo {
		if err := sudoMkdirChown(path); err != nil {
			return fmt.Errorf("sudo mkdir '%s': %w", path, err)
		}
	} else {
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("mkdir '%s': %w", path, err)
		}
	}

	submoduleName := cfg.SubmoduleName(name)
	remoteURL, err := git.SubmoduleURL(cfg.Repo, submoduleName)
	if err != nil {
		return fmt.Errorf("cannot find remote: %w", err)
	}

	if out, err := git.Run(path, "clone", remoteURL, "."); err != nil {
		return fmt.Errorf("git clone: %w: %s", err, out)
	}

	fmt.Printf("  ✓ %s → %s\n", name, path)
	return nil
}

// sudoMkdirChown creates path via sudo then chowns it to the current user,
// so subsequent git operations can run without sudo later.
func sudoMkdirChown(path string) error {
	if out, err := exec.Command("sudo", "mkdir", "-p", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}

	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}

	if out, err := exec.Command("sudo", "chown", u.Username, path).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}
