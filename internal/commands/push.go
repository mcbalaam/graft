package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/meta"
	"github.com/mcbalaam/graft/internal/prompt"
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
			args := []string{"push", "--set-upstream", "origin", "HEAD"}
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

		// collect and write metadata before git add so it's included in the commit
		metaMsg, metaErr := pushMeta(cfg, name, blob)
		if metaErr != nil {
			results = append(results, result{name, metaErr, ""})
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

		msg := "pushed"
		if metaMsg != "" {
			msg += "  " + metaMsg
		}
		results = append(results, result{name, nil, msg})
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

// pushMeta collects filesystem metadata and writes .graft-meta.toml before git add.
// For blobs without the meta flag: detects non-default attributes and queries the user.
// Returns a short status message and any fatal error.
func pushMeta(cfg *config.Config, name string, blob config.Blob) (msg string, err error) {
	m, err := meta.Collect(blob.Path)
	if err != nil {
		return "", fmt.Errorf("✗ meta collect: %w", err)
	}

	enabled := blob.Meta
	if !enabled {
		nonDefault := m.NonDefaultFiles()
		if len(nonDefault) == 0 {
			return "", nil
		}

		sort.Strings(nonDefault)
		fmt.Printf("\n● %s: files with non-default permissions/ownership:\n", name)
		for _, p := range nonDefault {
			fm := m.Files[p]
			fmt.Printf("    %-40s %s:%s  %s\n", p, fm.User, fm.Group, fm.Mode)
		}

		choice, err := prompt.Query(
			"● enable meta tracking to preserve ownership/permissions on restore?",
			[]string{
				"yes, enable meta for this blob",
				"no, skip metadata tracking",
				"cancel push",
			},
			1,
		)
		if err != nil {
			return "", fmt.Errorf("✗ prompt: %w", err)
		}
		switch choice {
		case 0:
			enabled = true
			if err := cfg.SetMeta(name, true); err != nil {
				return "", fmt.Errorf("✗ save config: %w", err)
			}
		case 1:
			return "", nil
		default:
			return "", fmt.Errorf("cancelled")
		}
	}

	if !enabled {
		return "", nil
	}

	if err := meta.Save(blob.Path, m); err != nil {
		return "", fmt.Errorf("✗ meta save: %w", err)
	}
	return "(+meta)", nil
}
