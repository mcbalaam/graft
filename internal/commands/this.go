package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/meta"
	"github.com/mcbalaam/graft/internal/prompt"
)

// This begins tracking the current directory as a new blob:
// git init, commit, remote, push, submodule add, write to config.
// Pre-flight checks run before any mutation; on mid-flow failure every
// step already performed is rolled back in reverse order.
func This(blobName string, sudo, public, metaFlag bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("✗ unable to read config: %w", err)
	}

	if cfg.HasBlob(blobName) {
		return fmt.Errorf("✗ blob '%s' already exists in config", blobName)
	}

	cwd, err := git.AbsPath(".")
	if err != nil {
		return fmt.Errorf("✗ cannot resolve current directory: %w", err)
	}

	submoduleName := cfg.SubmoduleName(blobName)
	remoteURL := cfg.Master.BaseURL + "/" + submoduleName + ".git"
	githubHosted := strings.Contains(cfg.Master.BaseURL, "github.com")

	// pre-flight: the main repo remote is reachable — a proxy for push access
	// to the host, since the blob's own remote does not exist yet.
	if err := checkRemoteAccess("main", cfg.Repo, cfg.Master.Remote); err != nil {
		return fmt.Errorf("✗ %w", err)
	}

	// token / manual remote decision — before any mutations.
	// Only GitHub supports auto-creating remotes via API; for anything else
	// the blob remote must already exist.
	token := ""
	manualRemote := false
	if githubHosted {
		token = resolveToken(cfg)
		if token == "" {
			t, u, err := ensureTokenForThis(cfg)
			if err != nil {
				return err
			}
			token = t
			if u != "" {
				remoteURL = u
				manualRemote = true
				fmt.Printf("● using manual remote %s — graft will not create or delete it\n", remoteURL)
			}
		}
	} else {
		fmt.Printf("● remote host is not GitHub — graft will not auto-create %s, it must exist\n", remoteURL)
		if err := checkRemoteAccess("blob", cfg.Repo, remoteURL); err != nil {
			return fmt.Errorf("✗ %w", err)
		}
	}

	tx := &rollback{verbose: cfg.Verbose}
	autoCreate := githubHosted && !manualRemote
	if err := trackBlob(cfg, blobName, cwd, submoduleName, remoteURL, token, autoCreate, sudo, public, metaFlag, tx); err != nil {
		tx.undo()
		return err
	}
	tx.commit()

	if autoCreate {
		visibility := "private"
		if cfg.Master.Public || public {
			visibility = "public"
		}
		fmt.Printf("✓ blob '%s' registered as %s, now tracking (%s)\n", blobName, submoduleName, visibility)
	} else {
		fmt.Printf("✓ blob '%s' registered as %s, now tracking\n", blobName, submoduleName)
	}
	return nil
}

func trackBlob(cfg *config.Config, blobName, cwd, submoduleName, remoteURL, token string, autoCreate, sudo, public, metaFlag bool, tx *rollback) error {
	gitRun := git.Run
	if sudo {
		gitRun = git.RunSudo
	}

	// run uses sudo when needed (filesystem ops: init, add, commit, remote config)
	run := func(args ...string) error {
		out, err := gitRun(".", args...)
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}

	// runNet always runs as the current user — push/fetch use SSH keys, not root.
	// When sudo is set the .git dir is root-owned, so we pass safe.directory to allow it.
	runNet := func(args ...string) error {
		var gitArgs []string
		if sudo {
			gitArgs = append(gitArgs, "-c", "safe.directory="+cwd)
		}
		gitArgs = append(gitArgs, args...)
		out, err := git.Run(".", gitArgs...)
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}

	runIn := func(dir string, args ...string) error {
		out, err := git.Run(dir, args...)
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}

	// the blob registry in the repo config is written last (AddBlob)
	undoCfg, err := backupFile(cfg.RepoConfigPath())
	if err != nil {
		return fmt.Errorf("✗ cannot backup repo config: %w", err)
	}
	tx.push("config "+cfg.RepoConfigPath(), undoCfg)

	// handle existing .git setup:
	undoHistory := removeGitDir(cwd, sudo) // default: undo = remove the fresh .git
	if git.IsRepo(".") {
		choice, err := prompt.Query(
			"● directory is already a git repo, what to do?",
			[]string{
				"add remote and use as-is",
				"reinitialize (delete .git and start fresh)",
				"I'll figure it out (cancel)",
			},
			0,
		)
		if err != nil {
			return fmt.Errorf("✗ prompt failed: %w", err)
		}
		switch choice {
		case 0:
			// use as-is, adds remote below. phew.
			if git.HasCommits(".") {
				oldHead, err := gitRun(".", "rev-parse", "HEAD")
				if err != nil {
					return fmt.Errorf("✗ cannot read HEAD: %w", err)
				}
				undoHistory = func() error {
					return run("reset", "--hard", strings.TrimSpace(oldHead))
				}
			}
		case 1: // purging the old .git folder
			restore, discard, err := stashGitDir(cwd, sudo)
			if err != nil {
				return fmt.Errorf("✗ %w", err)
			}
			tx.push("restored previous .git", restore)
			tx.onCommit(func() { _ = discard() })
			undoHistory = nil // the stash restore covers the reinitialized repo
			if err := run("init"); err != nil {
				return fmt.Errorf("✗ git init: %w", err)
			}
		case 2: // leaving the user to deal with it themselves
			fmt.Println("cancelled")
			return nil
		}
	} else {
		if err := run("init"); err != nil {
			return fmt.Errorf("✗ git init: %w", err)
		}
	}
	if undoHistory != nil {
		tx.push("blob repository state", undoHistory)
	}

	// collect metadata and optionally write .graft-meta.toml before the first commit
	metaEnabled, err := resolveMetaFlag(cwd, metaFlag)
	if err != nil {
		return err
	}
	if metaEnabled {
		undoMeta, err := backupFile(filepath.Join(cwd, meta.FileName))
		if err != nil {
			return fmt.Errorf("✗ cannot backup %s: %w", meta.FileName, err)
		}
		tx.push("removed "+meta.FileName, undoMeta)

		m, err := meta.Collect(cwd)
		if err != nil {
			return fmt.Errorf("✗ meta collect: %w", err)
		}
		if err := meta.Save(cwd, m); err != nil {
			return fmt.Errorf("✗ meta save: %w", err)
		}
	}

	// initial commit if no commits yet (shouldn't be any? who knows!)
	hasCommits := git.HasCommits(".")
	if !hasCommits {
		if err := run("add", "."); err != nil {
			return fmt.Errorf("✗ git add: %w", err)
		}
		if err := run("commit", "-m", "graft: init "+blobName); err != nil {
			return fmt.Errorf("✗ git commit: %w", err)
		}
	} else if metaEnabled {
		// existing repo: commit meta file separately
		if err := run("add", meta.FileName); err != nil {
			return fmt.Errorf("✗ git add meta: %w", err)
		}
		if err := run("commit", "-m", "graft: add meta"); err != nil {
			return fmt.Errorf("✗ git commit meta: %w", err)
		}
	}

	// add remote if not present
	if git.HasRemote(".") {
		choice, err := prompt.Query(
			"● remote 'origin' already exists, what to do?",
			[]string{
				"overwrite with graft remote",
				"keep existing remote",
				"I'll figure it out (cancel)",
			},
			1,
		)
		if err != nil {
			return fmt.Errorf("✗ prompt failed: %w", err)
		}
		switch choice {
		case 0:
			oldURL, err := gitRun(".", "remote", "get-url", "origin")
			if err != nil {
				return fmt.Errorf("✗ cannot read remote URL: %w", err)
			}
			if err := run("remote", "set-url", "origin", remoteURL); err != nil {
				return fmt.Errorf("✗ git remote set-url: %w", err)
			}
			tx.push("restored original remote URL", func() error {
				return run("remote", "set-url", "origin", strings.TrimSpace(oldURL))
			})
		case 1:
			// keep as-is (this totally isn't going to break anything...)
		case 2:
			fmt.Println("cancelled")
			return nil
		}
	} else {
		if err := run("remote", "add", "origin", remoteURL); err != nil {
			return fmt.Errorf("✗ git remote add: %w", err)
		}
		tx.push("removed blob remote", func() error { return run("remote", "remove", "origin") })
	}

	if autoCreate {
		created, err := createRemoteRepo(token, submoduleName, cfg.Master.Public || public)
		if err != nil {
			return fmt.Errorf("✗ create remote repo: %w", err)
		}
		if created {
			owner := ownerFromBaseURL(cfg.Master.BaseURL)
			if owner == "" {
				fmt.Printf("● could not derive repo owner from %s — auto-created repo won't be deleted on rollback\n", cfg.Master.BaseURL)
			} else {
				tx.push(fmt.Sprintf("deleted auto-created remote repo %s/%s", owner, submoduleName), func() error {
					return deleteRemoteRepo(token, owner, submoduleName)
				})
			}
		}
	}

	if err := runNet("push", "--force", "--set-upstream", "origin", "HEAD"); err != nil {
		return fmt.Errorf("✗ git push: %w", err)
	}

	// register as submodule in the main repo
	oldMain, err := git.Run(cfg.Repo, "rev-parse", "HEAD")
	if err == nil {
		tx.push("main repo submodule registration", undoSubmodule(cfg.Repo, strings.TrimSpace(oldMain), submoduleName))
	}
	if err := runIn(cfg.Repo, "submodule", "add", remoteURL, submoduleName); err != nil {
		return fmt.Errorf("✗ git submodule add: %w", err)
	}
	if err := runIn(cfg.Repo, "add", ".gitmodules"); err != nil {
		return fmt.Errorf("✗ git add .gitmodules: %w", err)
	}
	if err := runIn(cfg.Repo, "commit", "-m", "graft: add submodule "+submoduleName); err != nil {
		return fmt.Errorf("✗ git commit: %w", err)
	}
	if err := runIn(cfg.Repo, "push"); err != nil {
		return fmt.Errorf("✗ git push master repo: %w", err)
	}

	if err := cfg.AddBlob(blobName, cwd, sudo, false, metaEnabled); err != nil {
		return fmt.Errorf("✗ cannot save config: %w", err)
	}
	return nil
}

// undoSubmodule returns an undo func reverting a submodule add/commit in the
// main repo: hard-reset to the pre-add commit plus leftover cleanup.
func undoSubmodule(repoPath, oldHead, submoduleName string) func() error {
	return func() error {
		if out, err := git.Run(repoPath, "submodule", "deinit", "-f", submoduleName); err != nil {
			// deinit fails when the submodule was never fully registered — not fatal
			_ = out
		}
		if out, err := git.Run(repoPath, "reset", "--hard", oldHead); err != nil {
			return fmt.Errorf("git reset --hard: %w: %s", err, out)
		}
		git.Run(repoPath, "config", "--file", ".gitmodules", "--remove-section", "submodule."+submoduleName)
		git.Run(repoPath, "config", "--remove-section", "submodule."+submoduleName)
		if err := os.RemoveAll(filepath.Join(repoPath, submoduleName)); err != nil {
			return err
		}
		return os.RemoveAll(filepath.Join(repoPath, ".git", "modules", submoduleName))
	}
}

// removeGitDir returns an undo func deleting a .git directory (via sudo when set).
func removeGitDir(dir string, sudo bool) func() error {
	return func() error {
		if sudo {
			out, err := exec.Command("sudo", "rm", "-rf", filepath.Join(dir, ".git")).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%w: %s", err, out)
			}
			return nil
		}
		return os.RemoveAll(filepath.Join(dir, ".git"))
	}
}

// resolveMetaFlag returns whether meta tracking should be enabled.
// If metaFlag is true, returns immediately. Otherwise collects metadata
// and queries the user when non-default attributes are found.
func resolveMetaFlag(dir string, metaFlag bool) (bool, error) {
	if metaFlag {
		return true, nil
	}
	m, err := meta.Collect(dir)
	if err != nil {
		return false, fmt.Errorf("✗ meta collect: %w", err)
	}
	nonDefault := m.NonDefaultFiles()
	if len(nonDefault) == 0 {
		return false, nil
	}

	sort.Strings(nonDefault)
	fmt.Printf("● found files with non-default permissions/ownership:\n")
	for _, p := range nonDefault {
		fm := m.Files[p]
		fmt.Printf("    %-40s %s:%s  %s\n", p, fm.User, fm.Group, fm.Mode)
	}

	choice, err := prompt.Query(
		"● enable meta tracking to preserve ownership/permissions on restore?",
		[]string{
			"yes, enable meta for this blob",
			"no, skip metadata tracking",
			"cancel",
		},
		1,
	)
	if err != nil {
		return false, fmt.Errorf("✗ prompt: %w", err)
	}
	switch choice {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("cancelled")
	}
}
