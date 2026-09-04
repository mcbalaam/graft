package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type rollbackStep struct {
	desc string
	undo func() error
}

// rollback collects undo functions in the order the operations happened and
// runs them in reverse when a multi-step command fails halfway through.
// On success commit() must be called so nothing is undone.
// With verbose set (config: verbose_output), each rolled-back step is printed;
// rollback failures are always reported.
type rollback struct {
	steps    []rollbackStep
	cleanups []func()
	done     bool
	verbose  bool
}

// push registers an undo step. A nil undo is ignored.
func (r *rollback) push(desc string, undo func() error) {
	if undo == nil {
		return
	}
	r.steps = append(r.steps, rollbackStep{desc: desc, undo: undo})
}

// onCommit registers a cleanup that runs only on success (e.g. deleting a stash).
func (r *rollback) onCommit(fn func()) {
	if fn != nil {
		r.cleanups = append(r.cleanups, fn)
	}
}

// commit marks the operation successful: no undo will run, cleanups do.
func (r *rollback) commit() {
	if r.done {
		return
	}
	r.done = true
	r.steps = nil
	for _, fn := range r.cleanups {
		fn()
	}
	r.cleanups = nil
}

// undo runs every collected undo function in reverse order and reports each.
func (r *rollback) undo() {
	if r.done {
		return
	}
	r.done = true
	if len(r.steps) > 0 {
		fmt.Println("● something failed, rolling back changes...")
		for i := len(r.steps) - 1; i >= 0; i-- {
			s := r.steps[i]
			if err := s.undo(); err != nil {
				fmt.Printf("  ✗ could not roll back %s: %v\n", s.desc, err)
			} else if r.verbose {
				fmt.Printf("  ↩ rolled back: %s\n", s.desc)
			}
		}
	}
	for _, fn := range r.cleanups {
		fn()
	}
	r.steps, r.cleanups = nil, nil
}

// backupFile captures a file's current state (content+mode, or absence)
// and returns an undo func restoring exactly that state.
func backupFile(path string) (func() error, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		mode := os.FileMode(0600)
		if fi, serr := os.Stat(path); serr == nil {
			mode = fi.Mode().Perm()
		}
		return func() error { return os.WriteFile(path, data, mode) }, nil
	case os.IsNotExist(err):
		return func() error {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}, nil
	default:
		return nil, err
	}
}

// stashGitDir moves an existing .git directory out of the way so it can be
// restored if re-tracking fails. Returns a restore func, a discard func
// (run on success), and any error.
func stashGitDir(dir string, sudo bool) (restore func() error, discard func() error, err error) {
	tmp, err := os.MkdirTemp("", "graft-git-stash-")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create stash directory: %w", err)
	}
	stashed := filepath.Join(tmp, ".git")
	cur := filepath.Join(dir, ".git")

	if sudo {
		if out, err := exec.Command("sudo", "mv", cur, stashed).CombinedOutput(); err != nil {
			os.RemoveAll(tmp)
			return nil, nil, fmt.Errorf("cannot move .git aside: %w: %s", err, out)
		}
	} else if err := movePath(cur, stashed); err != nil {
		os.RemoveAll(tmp)
		return nil, nil, fmt.Errorf("cannot move .git aside: %w", err)
	}

	restore = func() error {
		if _, serr := os.Stat(cur); serr == nil {
			if err := os.RemoveAll(cur); err != nil {
				return fmt.Errorf("cannot remove new .git before restore: %w", err)
			}
		}
		if sudo {
			if out, err := exec.Command("sudo", "mv", stashed, cur).CombinedOutput(); err != nil {
				return fmt.Errorf("%w: %s", err, out)
			}
			return nil
		}
		return movePath(stashed, cur)
	}
	discard = func() error { return os.RemoveAll(tmp) }
	return restore, discard, nil
}

// movePath renames src to dst, falling back to copy+delete across filesystems.
func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// pathExists reports whether path exists (for any reason other than error).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
