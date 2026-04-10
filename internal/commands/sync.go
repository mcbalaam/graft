package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
)

// Sync commits and pushes all blobs, then updates submodule refs in the main repo.
func Sync() error {
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

		out, err := run(blob.Path, "status", "--porcelain")
		if err != nil {
			results = append(results, result{name, fmt.Errorf("git status: %w", err), ""})
			continue
		}
		if strings.TrimSpace(out) == "" {
			results = append(results, result{name, nil, "nothing to commit"})
			continue
		}

		if _, err := run(blob.Path, "add", "-A"); err != nil {
			results = append(results, result{name, fmt.Errorf("git add: %w", err), ""})
			continue
		}
		if _, err := run(blob.Path, "commit", "-m", "graft: sync"); err != nil {
			results = append(results, result{name, fmt.Errorf("git commit: %w", err), ""})
			continue
		}
		if _, err := run(blob.Path, "push"); err != nil {
			results = append(results, result{name, fmt.Errorf("git push: %w", err), ""})
			continue
		}

		results = append(results, result{name, nil, "pushed"})
	}

	// update submodule refs in main repo and push
	submodules, _ := git.ListSubmodules(cfg.Repo)
	if len(submodules) > 0 {
		git.Run(cfg.Repo, "submodule", "update", "--remote")
		out, _ := git.Run(cfg.Repo, "status", "--porcelain")
		if strings.TrimSpace(out) != "" {
			git.Run(cfg.Repo, "add", "-A")
			git.Run(cfg.Repo, "commit", "-m", "graft: sync refs")
			if _, err := git.Run(cfg.Repo, "push"); err != nil {
				fmt.Printf("✗ could not push main repo: %v\n", err)
			}
		}
	}

	fmt.Println()
	fmt.Println("sync summary:")
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
		return fmt.Errorf("✗ %d blob(s) failed to sync", failed)
	}
	return nil
}
