package commands

import (
	"bytes"
	"fmt"
	"net/http"
	"os"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
	"github.com/mcbalaam/graft/internal/prompt"
)

// Here begins tracking the current directory as a new blob:
// git init, commit, remote, push, submodule add, write to config.
func This(blobName string, sudo bool) error {
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

	run := func(args ...string) error {
		out, err := gitRun(".", args...)
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
				"reinitialise (delete .git and start fresh)",
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
			if err := os.RemoveAll(".git"); err != nil {
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

	// initial commit if no commits yet (shouldn't be any? who knows!)
	if !git.HasCommits(".") {
		if err := run("add", "."); err != nil {
			return fmt.Errorf("✗ git add: %w", err)
		}
		if err := run("commit", "-m", "graft: init "+blobName); err != nil {
			return fmt.Errorf("✗ git commit: %w", err)
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

	if err := createRemoteRepo(cfg, submoduleName); err != nil {
		return fmt.Errorf("✗ create remote repo: %w", err)
	}

	if err := run("push", "--set-upstream", "origin", "master"); err != nil {
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

	if err := cfg.AddBlob(blobName, cwd, sudo, false); err != nil {
		return fmt.Errorf("✗ cannot save config: %w", err)
	}

	fmt.Printf("✓ blob '%s' registered as %s, now tracking\n", blobName, submoduleName)
	return nil
}

// createRemoteRepo creates a private repo via GitHub API.
// Skips silently if the repo already exists (422).
func createRemoteRepo(cfg *config.Config, name string) error {
	token := cfg.GitHubToken
	if token == "" {
		return fmt.Errorf("github_token not set in config — add it to graft.toml or create the remote repo manually")
	}

	body := fmt.Sprintf(`{"name":%q,"private":true}`, name)
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
