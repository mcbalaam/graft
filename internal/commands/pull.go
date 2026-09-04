package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Pull fetches updates for blob(s). With --force, resets to remote HEAD discarding local changes.
// If blobName is empty, all blobs are pulled.
func Pull(blobName string, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	blobs, err := selectBlobs(cfg, blobName)
	if err != nil {
		return fmt.Errorf("✗ %w", err)
	}

	var results []blobResult
	for name, blob := range blobs {
		results = append(results, pullOne(name, blob, force))
	}

	if failed := printBlobSummary(cfg.ActiveName(), "pull", results); failed > 0 {
		return failedErr("pull", failed)
	}
	return nil
}

func pullOne(name string, blob config.Blob, force bool) blobResult {
	run := gitExecFor(blob.Sudo)

	if force {
		if out, err := run(blob.Path, "fetch", "origin"); err != nil {
			return blobResult{name, fmt.Errorf("git fetch: %w: %s", err, out), ""}
		}
		if out, err := run(blob.Path, "reset", "--hard", "@{upstream}"); err != nil {
			return blobResult{name, fmt.Errorf("git reset: %w: %s", err, out), ""}
		}
		return blobResult{name, nil, "reset to remote"}
	}

	out, err := run(blob.Path, "pull")
	if err != nil {
		if strings.Contains(out, "CONFLICT") {
			if resolveErr := resolveConflict(run, blob.Path, name); resolveErr != nil {
				return blobResult{name, resolveErr, ""}
			}
			return blobResult{name, nil, "conflict resolved"}
		}
		return blobResult{name, fmt.Errorf("git pull: %w: %s", err, out), ""}
	}

	if strings.Contains(out, "Already up to date") {
		return blobResult{name, nil, "already up to date"}
	}
	return blobResult{name, nil, "updated"}
}

func resolveConflict(run gitFunc, path, name string) error {
	choice, err := prompt.Query(
		fmt.Sprintf("conflict in blob: %s", name),
		[]string{
			"their version (theirs)",
			"my version (ours)",
			"auto-merge (commit as-is)",
			"skip (I'll figure it out)",
		},
		3,
	)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	switch choice {
	case 0: // theirs
		run(path, "checkout", "--theirs", ".")
		run(path, "add", ".")
		_, err = run(path, "commit", "-m", "graft: resolve conflict (theirs)")
	case 1: // ours
		run(path, "checkout", "--ours", ".")
		run(path, "add", ".")
		_, err = run(path, "commit", "-m", "graft: resolve conflict (ours)")
	case 2: // auto-merge
		run(path, "add", ".")
		_, err = run(path, "commit", "--no-edit")
		if err != nil {
			fmt.Printf("✗ auto-merge failed for %s\n", name)
			fmt.Printf("  fix manually: cd %s && git status\n", path)
			return fmt.Errorf("auto-merge failed")
		}
	case 3:
		fmt.Printf("  fix manually: cd %s && git status\n", path)
		return fmt.Errorf("skipped by user")
	}

	return err
}
