package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/prompt"
)

// resolveToken finds a GitHub access token: config first, then $GRAFT_TOKEN,
// then `gh auth token` if the GitHub CLI is installed and authenticated.
func resolveToken(cfg *config.Config) string {
	if cfg.AccessToken != "" {
		return cfg.AccessToken
	}
	if t := strings.TrimSpace(os.Getenv("GRAFT_TOKEN")); t != "" {
		return t
	}
	return ghAuthToken()
}

func ghAuthToken() string {
	path, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}
	out, err := exec.Command(path, "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// promptAndSaveToken offers an interactive hidden prompt for a GitHub token
// when none can be resolved, validates it, and stores it in the local config.
// Non-fatal: callers work without a token (blob repos then need manual creation).
func promptAndSaveToken(cfg *config.Config) {
	if resolveToken(cfg) != "" {
		return
	}
	fmt.Println("● optional: a GitHub access token lets graft auto-create blob repos")
	fmt.Println("  create one at https://github.com/settings/tokens (needs 'repo' scope), or leave blank to skip")
	tok, err := prompt.Secret("access token: ")
	if err != nil {
		fmt.Printf("✗ token prompt failed: %v\n", err)
		return
	}
	if tok == "" {
		fmt.Println("● skipped — 'graft this' will ask you to create remote repos manually")
		return
	}
	login, err := githubAuthUser(tok)
	if err != nil {
		fmt.Printf("✗ token rejected: %v — not saving\n", err)
		return
	}
	if err := cfg.SetAccessToken(tok); err != nil {
		fmt.Printf("✗ cannot save token: %v\n", err)
		return
	}
	fmt.Printf("✓ token stored in %s (authenticated as %s)\n", cfg.LocalConfigPath(), login)
}

// ensureTokenForThis returns a token usable for auto-creating blob repos.
// Without any token it falls back to an interactive choice: store a token now
// or use a manually created remote repo URL (returned via manualURL).
// When non-interactive and no token is available it returns an error instead.
func ensureTokenForThis(cfg *config.Config) (token string, manualURL string, err error) {
	token = resolveToken(cfg)
	if token != "" {
		return token, "", nil
	}

	fmt.Println("● no GitHub access token found (config, $GRAFT_TOKEN, or 'gh auth token')")
	fmt.Println("  a token lets graft create the blob's remote repo automatically")
	choice, err := prompt.Query(
		"● how should graft proceed?",
		[]string{
			"paste an access token now (stored in config)",
			"paste the URL of a remote repo I created manually",
			"cancel",
		},
		-1,
	)
	if err != nil {
		return "", "", fmt.Errorf("✗ prompt: %w", err)
	}
	switch choice {
	case 0:
		tok, err := prompt.Secret("access token: ")
		if err != nil {
			return "", "", fmt.Errorf("✗ token prompt failed: %w", err)
		}
		if tok == "" {
			return "", "", fmt.Errorf("✗ no token entered")
		}
		login, err := githubAuthUser(tok)
		if err != nil {
			return "", "", fmt.Errorf("✗ %v", err)
		}
		if err := cfg.SetAccessToken(tok); err != nil {
			return "", "", fmt.Errorf("✗ cannot save token: %w", err)
		}
		fmt.Printf("✓ token stored (authenticated as %s)\n", login)
		return tok, "", nil
	case 1:
		u, err := prompt.Ask("remote repo URL: ")
		if err != nil {
			return "", "", fmt.Errorf("✗ prompt failed: %w", err)
		}
		u = strings.TrimSpace(u)
		if u == "" {
			return "", "", fmt.Errorf("✗ empty URL")
		}
		return "", u, nil
	default:
		return "", "", fmt.Errorf("cancelled")
	}
}
