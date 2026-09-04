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

	blobs, err := selectBlobs(cfg, blobName)
	if err != nil {
		return fmt.Errorf("✗ %w", err)
	}

	var results []blobResult
	for name, blob := range blobs {
		results = append(results, pushOne(cfg, name, blob))
	}

	// update submodule refs in main repo and push (only when pushing all blobs)
	if blobName == "" {
		if err := updateMainRepoRef(cfg); err != nil {
			results = append(results, blobResult{name: "(main repo refs)", err: err})
		}
	}

	if failed := printBlobSummary(cfg.ActiveName(), "push", results); failed > 0 {
		return failedErr("push", failed)
	}
	return nil
}

func pushOne(cfg *config.Config, name string, blob config.Blob) blobResult {
	run := gitExecFor(blob.Sudo)

	// push always runs as current user; for sudo blobs the .git is root-owned
	// so we pass safe.directory to allow it
	pushArgs := []string{"push", "--set-upstream", "origin", "HEAD"}
	if blob.Sudo {
		pushArgs = append([]string{"-c", "safe.directory=" + blob.Path}, pushArgs...)
	}

	out, err := run(blob.Path, "status", "--porcelain")
	if err != nil {
		return blobResult{name, fmt.Errorf("git status: %w: %s", err, out), ""}
	}
	if strings.TrimSpace(out) == "" {
		return blobResult{name, nil, "nothing to commit"}
	}

	// collect and write metadata before git add so it's included in the commit
	metaMsg, metaErr := pushMeta(cfg, name, blob)
	if metaErr != nil {
		return blobResult{name, metaErr, ""}
	}

	if err := runE(run, blob.Path, "add", "-A"); err != nil {
		return blobResult{name, fmt.Errorf("git add: %w", err), ""}
	}
	if err := runE(run, blob.Path, "commit", "-m", "graft: push"); err != nil {
		return blobResult{name, fmt.Errorf("git commit: %w", err), ""}
	}
	if out, err := git.Run(blob.Path, pushArgs...); err != nil {
		return blobResult{name, fmt.Errorf("git push: %w: %s", err, out), ""}
	}

	msg := "pushed"
	if metaMsg != "" {
		msg += "  " + metaMsg
	}
	return blobResult{name, nil, msg}
}
