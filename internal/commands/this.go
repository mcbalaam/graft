package commands

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/meta"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Here begins tracking the current directory as a new blob:
// git init, commit, remote, push, submodule add, write to config.
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

	// handle existing .git setup:
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
		case 1: // purging the old .git folder
			if sudo {
				cmd := exec.Command("sudo", "rm", "-rf", ".git")
				cmd.Dir = cwd
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("✗ cannot remove .git: %w: %s", err, out)
				}
			} else if err := os.RemoveAll(".git"); err != nil {
				return fmt.Errorf("✗ cannot remove .git: %w", err)
			}
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

	// collect metadata and optionally write .graft-meta.toml before the first commit
	metaEnabled, err := resolveMetaFlag(cwd, metaFlag)
	if err != nil {
		return err
	}
	if metaEnabled {
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
			if err := run("remote", "set-url", "origin", remoteURL); err != nil {
				return fmt.Errorf("✗ git remote set-url: %w", err)
			}
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
	}

	if err := createRemoteRepo(cfg, submoduleName, cfg.Master.Public || public); err != nil {
		return fmt.Errorf("✗ create remote repo: %w", err)
	}

	if err := runNet("push", "--force", "--set-upstream", "origin", "HEAD"); err != nil {
		return fmt.Errorf("✗ git push: %w", err)
	}

	// register as submodule in the main repo
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

	visibility := "private"
	if cfg.Master.Public || public {
		visibility = "public"
	}
	fmt.Printf("✓ blob '%s' registered as %s, now tracking (%s)\n", blobName, submoduleName, visibility)
	return nil
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

// createRemoteRepo creates a private repo via GitHub API.
// Skips silently if the repo already exists (422).
func createRemoteRepo(cfg *config.Config, name string, public bool) error {
	token := cfg.AccessToken
	if token == "" {
		return fmt.Errorf("access_token not set in config — add it to graft.toml or create the remote repo manually")
	}

	body := fmt.Sprintf(`{"name":%q,"private":%t}`, name, !public)
	req, err := http.NewRequest("POST", "https://api.github.com/user/repos", bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	// 201 Created — ok, 422 Unprocessable — already exists, both are fine
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusUnprocessableEntity {
		return fmt.Errorf("unexpected response: %s", resp.Status)
	}
	return nil
}
