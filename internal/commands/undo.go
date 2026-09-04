package commands

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Undo reverts the last commit on a blob and pushes the revert commit.
// Without name: resolves blob from current working directory.
func Undo(blobName string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	name, blob, err := resolveBlobByNameOrCwd(cfg, blobName)
	if err != nil {
		return err
	}

	run := gitExecFor(blob.Sudo)

	head, err := run(blob.Path, "log", "-1", "--oneline")
	if err != nil {
		return fmt.Errorf("✗ cannot read HEAD: %w", err)
	}

	fmt.Printf("● %s: will revert: %s\n", name, head)

	choice, err := prompt.Query(
		"● create a revert commit and push?",
		[]string{"yes, revert and push", "cancel"},
		-1,
	)
	if err != nil {
		return fmt.Errorf("✗ prompt: %w", err)
	}
	if choice != 0 {
		fmt.Println("cancelled")
		return nil
	}

	if out, err := run(blob.Path, "revert", "HEAD", "--no-edit"); err != nil {
		return fmt.Errorf("✗ git revert: %w: %s", err, out)
	}

	pushArgs := []string{"push"}
	if blob.Sudo {
		pushArgs = append([]string{"-c", "safe.directory=" + blob.Path}, pushArgs...)
	}
	if out, err := git.Run(blob.Path, pushArgs...); err != nil {
		return fmt.Errorf("✗ git push: %w: %s", err, out)
	}

	if err := updateMainRepoRef(cfg); err != nil {
		return fmt.Errorf("✗ %s: revert pushed, but main repo ref update failed: %w", name, err)
	}

	fmt.Printf("✓ %s: reverted and pushed\n", name)
	return nil
}

// Reset hard-resets a blob to remote HEAD, discarding any local changes.
// Without name: resolves blob from current working directory.
func Reset(blobName string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	name, blob, err := resolveBlobByNameOrCwd(cfg, blobName)
	if err != nil {
		return err
	}

	run := gitExecFor(blob.Sudo)

	out, _ := run(blob.Path, "status", "--porcelain")
	if strings.TrimSpace(out) != "" {
		fmt.Printf("● %s: uncommitted changes will be lost:\n", name)
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			fmt.Printf("    %s\n", line)
		}
	}

	choice, err := prompt.Query(
		fmt.Sprintf("● %s: reset to remote HEAD?", name),
		[]string{"yes, discard local changes", "cancel"},
		-1,
	)
	if err != nil {
		return fmt.Errorf("✗ prompt: %w", err)
	}
	if choice != 0 {
		fmt.Println("cancelled")
		return nil
	}

	if out, err := run(blob.Path, "fetch", "origin"); err != nil {
		return fmt.Errorf("✗ git fetch: %w: %s", err, out)
	}
	if out, err := run(blob.Path, "reset", "--hard", "@{upstream}"); err != nil {
		return fmt.Errorf("✗ git reset: %w: %s", err, out)
	}

	fmt.Printf("✓ %s: reset to remote HEAD\n", name)
	return nil
}

// resolveBlobByNameOrCwd returns the blob for the given name, or looks up
// the blob whose configured path matches the current working directory.
func resolveBlobByNameOrCwd(cfg *config.Config, name string) (string, config.Blob, error) {
	if name != "" {
		blob, ok := cfg.Blobs[name]
		if !ok {
			return "", config.Blob{}, fmt.Errorf("✗ blob '%s' not found in config", name)
		}
		return name, blob, nil
	}
	cwd, err := filepath.Abs(".")
	if err != nil {
		return "", config.Blob{}, fmt.Errorf("✗ cannot resolve current directory: %w", err)
	}
	blobName, ok := cfg.HasBlobPath(cwd)
	if !ok {
		return "", config.Blob{}, fmt.Errorf("✗ no blob configured for this path, specify a name or cd into a tracked directory")
	}
	return blobName, cfg.Blobs[blobName], nil
}

// updateMainRepoRef updates submodule refs in the main repo after a blob push.
// Returns an error if the ref update or its push fails (the local commit is
// kept and reported so the user can recover manually).
func updateMainRepoRef(cfg *config.Config) error {
	submodules, _ := git.ListSubmodules(cfg.Repo)
	if len(submodules) == 0 {
		return nil
	}
	if out, err := git.Run(cfg.Repo, "submodule", "update", "--remote"); err != nil {
		// not fatal: one broken submodule shouldn't block ref refresh of the others
		fmt.Printf("● warning: submodule update --remote: %s\n", out)
	}
	out, err := git.Run(cfg.Repo, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w: %s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	if out, err := git.Run(cfg.Repo, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w: %s", err, out)
	}
	if out, err := git.Run(cfg.Repo, "commit", "-m", "graft: update refs"); err != nil {
		return fmt.Errorf("git commit: %w: %s", err, out)
	}
	if out, err := git.Run(cfg.Repo, "push"); err != nil {
		return fmt.Errorf("git push: %w: %s (refs are committed locally — push manually in %s)", err, out, cfg.Repo)
	}
	return nil
}
