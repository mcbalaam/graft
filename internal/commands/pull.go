package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Pull fetches updates for blob(s). With --force, resets to remote HEAD discarding local changes.
// If blobName is empty, all blobs are pulled.
func Pull(blobName string, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	blobs := cfg.Blobs
	if blobName != "" {
		blob, ok := cfg.Blobs[blobName]
		if !ok {
			return fmt.Errorf("✗ blob '%s' not found in config", blobName)
		}
		blobs = map[string]config.Blob{blobName: blob}
	}

	type result struct {
		name string
		err  error
		msg  string
	}
	var results []result

	for name, blob := range blobs {
		run := git.Run
		if blob.Sudo {
			run = git.RunSudo
		}

		if force {
			if _, err := run(blob.Path, "fetch", "origin"); err != nil {
				results = append(results, result{name, fmt.Errorf("git fetch: %w", err), ""})
				continue
			}
			if _, err := run(blob.Path, "reset", "--hard", "@{upstream}"); err != nil {
				results = append(results, result{name, fmt.Errorf("git reset: %w", err), ""})
				continue
			}
			results = append(results, result{name, nil, "reset to remote"})
			continue
		}

		out, err := run(blob.Path, "pull")
		if err != nil {
			if strings.Contains(out, "CONFLICT") {
				if resolveErr := resolveConflict(run, blob.Path, name); resolveErr != nil {
					results = append(results, result{name, resolveErr, ""})
				} else {
					results = append(results, result{name, nil, "conflict resolved"})
				}
			} else {
				results = append(results, result{name, fmt.Errorf("git pull: %w: %s", err, out), ""})
			}
			continue
		}

		if strings.Contains(out, "Already up to date") {
			results = append(results, result{name, nil, "already up to date"})
		} else {
			results = append(results, result{name, nil, "updated"})
		}
	}

	fmt.Println()
	fmt.Printf("[%s] pull summary:\n", cfg.ActiveName())
	ok, failed := 0, 0
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("  ✗ %s: %v\n", r.name, r.err)
			failed++
		} else {
			fmt.Printf("  ✓ %s: %s\n", r.name, r.msg)
			ok++
		}
	}
	fmt.Printf("  %d ok, %d failed\n", ok, failed)

	if failed > 0 {
		return fmt.Errorf("✗ %d blob(s) failed to pull", failed)
	}
	return nil
}

func resolveConflict(run func(string, ...string) (string, error), path, name string) error {
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
