package commands

import (
	"errors"
	"fmt"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/git"
)

// errCancelled marks a user-initiated cancellation: the operation rolls back
// its own steps and exits 0 without an error banner.
var errCancelled = errors.New("cancelled")

// gitFunc mirrors git.Run's signature so the plain and sudo runners are interchangeable.
type gitFunc func(dir string, args ...string) (string, error)

// gitExecFor returns the git runner matching the blob's sudo flag.
func gitExecFor(sudo bool) gitFunc {
	if sudo {
		return git.RunSudo
	}
	return git.Run
}

// runE executes a git command and folds the combined output into the error.
func runE(fn gitFunc, dir string, args ...string) error {
	out, err := fn(dir, args...)
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// selectBlobs returns the blobs to operate on: all of them, or just the named one.
func selectBlobs(cfg *config.Config, name string) (map[string]config.Blob, error) {
	if name == "" {
		return cfg.Blobs, nil
	}
	blob, ok := cfg.Blobs[name]
	if !ok {
		return nil, fmt.Errorf("blob '%s' not found in config", name)
	}
	return map[string]config.Blob{name: blob}, nil
}

// blobResult is one outcome in a per-blob summary; msg is the success note.
type blobResult struct {
	name string
	err  error
	msg  string
}

// printBlobSummary prints the shared ok/failed report and returns the failure count.
func printBlobSummary(active, verb string, results []blobResult) int {
	fmt.Println()
	fmt.Printf("[%s] %s summary:\n", active, verb)
	ok, failed := 0, 0
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("  ✗ %s: %v\n", r.name, r.err)
			failed++
		} else if r.msg != "" {
			fmt.Printf("  ✓ %s: %s\n", r.name, r.msg)
			ok++
		} else {
			fmt.Printf("  ✓ %s\n", r.name)
			ok++
		}
	}
	fmt.Printf("  %d ok, %d failed\n", ok, failed)
	return failed
}

// failedErr is the command-level error for N failed blobs.
func failedErr(verb string, n int) error {
	return fmt.Errorf("✗ %d blob(s) failed to %s", n, verb)
}
