package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
)

// Push commits and pushes blob(s), then updates submodule refs in the main repo.
// If blobName is empty, all blobs are pushed.
func Push(blobName string) error {
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

		// push always runs as current user; for sudo blobs the .git is root-owned
		// so we pass safe.directory to allow it
		pushBlob := func(path string) (string, error) {
			args := []string{"push", "--set-upstream", "origin", "master"}
			if blob.Sudo {
				args = append([]string{"-c", "safe.directory=" + path}, args...)
			}
			return git.Run(path, args...)
		}

		runf := func(args ...string) error {
			out, err := run(blob.Path, args...)
			if err != nil {
				return fmt.Errorf("%w: %s", err, out)
			}
			return nil
		}

		out, err := run(blob.Path, "status", "--porcelain")
		if err != nil {
			results = append(results, result{name, fmt.Errorf("✗ git status: %w: %s", err, out), ""})
			continue
		}
		if strings.TrimSpace(out) == "" {
			results = append(results, result{name, nil, "nothing to commit"})
			continue
		}

		if err := runf("add", "-A"); err != nil {
			results = append(results, result{name, fmt.Errorf("✗ git add: %w", err), ""})
			continue
		}
		if err := runf("commit", "-m", "graft: push"); err != nil {
			results = append(results, result{name, fmt.Errorf("✗ git commit: %w", err), ""})
			continue
		}
		if out, err := pushBlob(blob.Path); err != nil {
			results = append(results, result{name, fmt.Errorf("✗ git push: %w: %s", err, out), ""})
			continue
		}

		results = append(results, result{name, nil, "pushed"})
	}

	// update submodule refs in main repo and push (only when pushing all blobs)
	if blobName == "" {
		submodules, _ := git.ListSubmodules(cfg.Repo)
		if len(submodules) > 0 {
			git.Run(cfg.Repo, "submodule", "update", "--remote")
			out, _ := git.Run(cfg.Repo, "status", "--porcelain")
			if strings.TrimSpace(out) != "" {
				git.Run(cfg.Repo, "add", "-A")
				git.Run(cfg.Repo, "commit", "-m", "graft: push refs")
				if _, err := git.Run(cfg.Repo, "push"); err != nil {
					fmt.Printf("✗ could not push main repo: %v\n", err)
				}
			}
		}
	}

	fmt.Println()
	fmt.Printf("[%s] push summary:\n", cfg.ActiveName())
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
		return fmt.Errorf("✗ %d blob(s) failed to push", failed)
	}
	return nil
}
