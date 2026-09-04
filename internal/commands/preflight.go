package commands

import (
	"fmt"
	"strings"

	"github.com/mcbalaam/graft/internal/git"
)

// accessEnv makes git pre-flight checks non-interactive so an auth problem
// fails fast instead of hanging on a password or host-key prompt.
var accessEnv = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
}

// checkRemoteAccess verifies a remote is reachable with current credentials
// before any local mutation is attempted. It hard-fails on clear access
// problems (missing key/repo) and warns-then-continues on ambiguous ones,
// so a flaky check can't block work that would have succeeded.
func checkRemoteAccess(label, dir, remote string) error {
	out, err := git.RunWithEnv(dir, accessEnv, "ls-remote", remote, "HEAD")
	if err == nil {
		return nil
	}
	low := strings.ToLower(out)
	blocking := strings.Contains(low, "permission denied") ||
		strings.Contains(low, "authentication failed") ||
		strings.Contains(low, "access denied") ||
		strings.Contains(low, "invalid username or password") ||
		strings.Contains(low, "terminal prompts disabled") ||
		strings.Contains(low, "host key verification failed") ||
		strings.Contains(low, "repository not found") ||
		strings.Contains(low, "does not appear to be a git repository") ||
		strings.Contains(low, "unable to access") ||
		strings.Contains(low, "could not read from remote")
	if !blocking {
		fmt.Printf("● could not pre-check %s remote %s: %s — continuing anyway\n", label, remote, out)
		return nil
	}
	return fmt.Errorf("cannot access %s remote %s: %s\n  check that the repository exists and your SSH keys/credentials grant access", label, remote, out)
}
