package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	cwd, err := filepath.Abs(".")
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
			tok, u, err := ensureTokenForThis(cfg)
			if err != nil {
				if errors.Is(err, errCancelled) {
					fmt.Println("cancelled")
					return nil
				}
				return err
			}
			token = tok
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

	tr := newBlobTracker(cfg, blobName, cwd, submoduleName, remoteURL, token, githubHosted && !manualRemote, sudo, public, metaFlag)
	if err := tr.perform(); err != nil {
		tr.tx.undo()
		if errors.Is(err, errCancelled) {
			fmt.Println("cancelled")
			return nil
		}
		return err
	}
	tr.tx.commit()

	if tr.autoCreate {
		visibility := "private"
		if cfg.Master.Public || public {
			visibility = "public"
		}
		fmt.Printf("✓ blob '%s' registered as %s, now tracking (%s)\n", blobName, tr.submoduleName, visibility)
	} else {
		fmt.Printf("✓ blob '%s' registered as %s, now tracking\n", blobName, tr.submoduleName)
	}
	return nil
}

// blobTracker carries the state and helpers of a single `graft this` run.
type blobTracker struct {
	cfg           *config.Config
	name          string
	cwd           string
	submoduleName string
	remoteURL     string
	token         string
	autoCreate    bool
	sudo          bool
	public        bool
	metaFlag      bool

	metaEnabled bool
	tx          *rollback

	gitRun func(dir string, args ...string) (string, error)
	run    func(args ...string) error
	runNet func(args ...string) error
	runIn  func(dir string, args ...string) error
}

func newBlobTracker(cfg *config.Config, name, cwd, submoduleName, remoteURL, token string, autoCreate, sudo, public, metaFlag bool) *blobTracker {
	t := &blobTracker{
		cfg: cfg, name: name, cwd: cwd, submoduleName: submoduleName,
		remoteURL: remoteURL, token: token, autoCreate: autoCreate,
		sudo: sudo, public: public, metaFlag: metaFlag,
		tx: &rollback{verbose: cfg.Verbose},
	}
	t.gitRun = gitExecFor(sudo)
	// run uses sudo when needed (filesystem ops: init, add, commit, remote config)
	t.run = func(args ...string) error { return runE(t.gitRun, ".", args...) }
	// runNet always runs as the current user — push/fetch use SSH keys, not root.
	// When sudo is set the .git dir is root-owned, so we pass safe.directory to allow it.
	t.runNet = func(args ...string) error {
		if sudo {
			args = append([]string{"-c", "safe.directory=" + cwd}, args...)
		}
		return runE(git.Run, ".", args...)
	}
	t.runIn = func(dir string, args ...string) error { return runE(git.Run, dir, args...) }
	return t
}

func (t *blobTracker) perform() error {
	steps := []func() error{
		t.backupConfig,
		t.initRepoState,
		t.prepareMeta,
		t.commitInitial,
		t.configureRemote,
		t.ensureRemoteRepo,
		t.pushBlob,
		t.registerSubmodule,
		t.saveConfig,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// backupConfig captures the repo config: the blob registry is written last,
// so every earlier failure is undone by restoring it.
func (t *blobTracker) backupConfig() error {
	undo, err := backupFile(t.cfg.RepoConfigPath())
	if err != nil {
		return fmt.Errorf("✗ cannot backup repo config: %w", err)
	}
	t.tx.push("config "+t.cfg.RepoConfigPath(), undo)
	return nil
}

// initRepoState prepares the blob's git repo: fresh init, or adopting an
// existing one (use as-is / reinitialize with .git stashed for rollback).
func (t *blobTracker) initRepoState() error {
	// default undo for a fresh repo: remove the .git we create
	undoHistory := removeGitDir(t.cwd, t.sudo)

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
				oldHead, err := t.gitRun(".", "rev-parse", "HEAD")
				if err != nil {
					return fmt.Errorf("✗ cannot read HEAD: %w", err)
				}
				head := strings.TrimSpace(oldHead)
				undoHistory = func() error { return t.run("reset", "--hard", head) }
			}
		case 1: // purging the old .git folder
			restore, discard, err := stashGitDir(t.cwd, t.sudo)
			if err != nil {
				return fmt.Errorf("✗ %w", err)
			}
			t.tx.push("restored previous .git", restore)
			t.tx.onCommit(func() { _ = discard() })
			undoHistory = nil // the stash restore covers the reinitialized repo
			if err := t.run("init"); err != nil {
				return fmt.Errorf("✗ git init: %w", err)
			}
		case 2:
			return errCancelled
		}
	} else {
		if err := t.run("init"); err != nil {
			return fmt.Errorf("✗ git init: %w", err)
		}
	}
	if undoHistory != nil {
		t.tx.push("blob repository state", undoHistory)
	}
	return nil
}

// prepareMeta collects filesystem metadata and writes .graft-meta.toml,
// asking the user first when non-default attributes exist.
func (t *blobTracker) prepareMeta() error {
	enabled, err := resolveMetaFlag(t.cwd, t.metaFlag)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	undoMeta, err := backupFile(filepath.Join(t.cwd, meta.FileName))
	if err != nil {
		return fmt.Errorf("✗ cannot backup %s: %w", meta.FileName, err)
	}
	t.tx.push("removed "+meta.FileName, undoMeta)

	m, err := meta.Collect(t.cwd)
	if err != nil {
		return fmt.Errorf("✗ meta collect: %w", err)
	}
	if err := meta.Save(t.cwd, m); err != nil {
		return fmt.Errorf("✗ meta save: %w", err)
	}
	t.metaEnabled = true
	return nil
}

// commitInitial creates the blob's first commit, or commits the meta file
// separately for an adopted repo.
func (t *blobTracker) commitInitial() error {
	if !git.HasCommits(".") {
		if err := t.run("add", "."); err != nil {
			return fmt.Errorf("✗ git add: %w", err)
		}
		if err := t.run("commit", "-m", "graft: init "+t.name); err != nil {
			return fmt.Errorf("✗ git commit: %w", err)
		}
		return nil
	}
	if t.metaEnabled {
		if err := t.run("add", meta.FileName); err != nil {
			return fmt.Errorf("✗ git add meta: %w", err)
		}
		if err := t.run("commit", "-m", "graft: add meta"); err != nil {
			return fmt.Errorf("✗ git commit meta: %w", err)
		}
	}
	return nil
}

// configureRemote points origin at the blob's remote, asking before
// clobbering an existing one. Both mutations register their own undo.
func (t *blobTracker) configureRemote() error {
	if !git.HasRemote(".") {
		if err := t.run("remote", "add", "origin", t.remoteURL); err != nil {
			return fmt.Errorf("✗ git remote add: %w", err)
		}
		t.tx.push("removed blob remote", func() error { return t.run("remote", "remove", "origin") })
		return nil
	}

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
		oldURL, err := t.gitRun(".", "remote", "get-url", "origin")
		if err != nil {
			return fmt.Errorf("✗ cannot read remote URL: %w", err)
		}
		old := strings.TrimSpace(oldURL)
		if err := t.run("remote", "set-url", "origin", t.remoteURL); err != nil {
			return fmt.Errorf("✗ git remote set-url: %w", err)
		}
		t.tx.push("restored original remote URL", func() error {
			return t.run("remote", "set-url", "origin", old)
		})
	case 1:
		// keep as-is (this totally isn't going to break anything...)
	default:
		return errCancelled
	}
	return nil
}

// ensureRemoteRepo auto-creates the GitHub remote and registers its deletion
// as compensation.
func (t *blobTracker) ensureRemoteRepo() error {
	if !t.autoCreate {
		return nil
	}
	created, err := createRemoteRepo(t.token, t.submoduleName, t.cfg.Master.Public || t.public)
	if err != nil {
		return fmt.Errorf("✗ create remote repo: %w", err)
	}
	if !created {
		return nil
	}
	owner := ownerFromBaseURL(t.cfg.Master.BaseURL)
	if owner == "" {
		fmt.Printf("● could not derive repo owner from %s — auto-created repo won't be deleted on rollback\n", t.cfg.Master.BaseURL)
		return nil
	}
	t.tx.push(fmt.Sprintf("deleted auto-created remote repo %s/%s", owner, t.submoduleName), func() error {
		return deleteRemoteRepo(t.token, owner, t.submoduleName)
	})
	return nil
}

func (t *blobTracker) pushBlob() error {
	if err := t.runNet("push", "--force", "--set-upstream", "origin", "HEAD"); err != nil {
		return fmt.Errorf("✗ git push: %w", err)
	}
	return nil
}

// registerSubmodule adds the blob to the main repo and pushes the registration.
func (t *blobTracker) registerSubmodule() error {
	oldMain, err := git.Run(t.cfg.Repo, "rev-parse", "HEAD")
	if err == nil {
		t.tx.push("main repo submodule registration", undoSubmodule(t.cfg.Repo, strings.TrimSpace(oldMain), t.submoduleName))
	}
	steps := [][]string{
		{"submodule", "add", t.remoteURL, t.submoduleName},
		{"add", ".gitmodules"},
		{"commit", "-m", "graft: add submodule " + t.submoduleName},
		{"push"},
	}
	labels := []string{"git submodule add", "git add .gitmodules", "git commit", "git push master repo"}
	for i, args := range steps {
		if err := t.runIn(t.cfg.Repo, args...); err != nil {
			return fmt.Errorf("✗ %s: %w", labels[i], err)
		}
	}
	return nil
}

func (t *blobTracker) saveConfig() error {
	if err := t.cfg.AddBlob(t.name, t.cwd, t.sudo, false, t.metaEnabled); err != nil {
		return fmt.Errorf("✗ cannot save config: %w", err)
	}
	return nil
}

// undoSubmodule returns an undo func reverting a submodule add/commit in the
// main repo: hard-reset to the pre-add commit plus leftover cleanup.
func undoSubmodule(repoPath, oldHead, submoduleName string) func() error {
	return func() error {
		if _, err := git.Run(repoPath, "submodule", "deinit", "-f", submoduleName); err != nil {
			// deinit fails when the submodule was never fully registered — not fatal
			_ = err
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
