package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Pull fetches updates for all blobs. On merge conflict, offers interactive resolution.
func Pull() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	type result struct {
		name string
		err  error
		msg  string
	}
	var results []result

	for name, blob := range cfg.Blobs {
		run := git.Run
		if blob.Sudo {
			run = git.RunSudo
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
				results = append(results, result{name, fmt.Errorf("git pull: %w", err), ""})
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
	fmt.Println("pull summary:")
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
			"skip (I'll fix it myself)",
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
	case 2: // auto-merge: stage everything and commit
		run(path, "add", ".")
		_, err = run(path, "commit", "--no-edit")
		if err != nil {
			fmt.Printf("✗ auto-merge failed for %s\n", name)
			fmt.Printf("  fix manually: cd %s && git status\n", path)
			return fmt.Errorf("auto-merge failed")
		}
	case 3: // balling out, yout are on your own now. good luck.
		fmt.Printf("  fix manually: cd %s && git status\n", path)
		return fmt.Errorf("skipped by user")
	}

	return err
}
