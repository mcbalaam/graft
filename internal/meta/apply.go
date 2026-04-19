package meta

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Apply restores filesystem metadata for every entry in m.
// When sudo is true, chown/chmod/xattr/acl/caps are executed via sudo
// because the blob is root-owned and the process runs as a regular user.
func Apply(blobPath string, m *BlobMeta, sudo bool) error {
	var errs []string
	for rel, fm := range m.Files {
		abs := filepath.Join(blobPath, rel)
		if err := applyOne(abs, fm, sudo); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", rel, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n  "))
	}
	return nil
}

func applyOne(abs string, fm FileMeta, sudo bool) error {
	// chown before caps: Linux clears capabilities on ownership change
	if err := applyChown(abs, fm.User, fm.Group, sudo); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	if err := applyChmod(abs, fm.Mode, sudo); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	for name, b64val := range fm.XAttr {
		if err := applyXAttr(abs, name, b64val, sudo); err != nil {
			return fmt.Errorf("xattr %s: %w", name, err)
		}
	}
	if fm.ACL != "" {
		if err := applyACL(abs, fm.ACL, sudo); err != nil {
			return fmt.Errorf("acl: %w", err)
		}
	}
	if fm.Caps != "" {
		if err := applyCaps(abs, fm.Caps, sudo); err != nil {
			return fmt.Errorf("caps: %w", err)
		}
	}
	return nil
}

func applyChown(abs, userName, groupName string, sudo bool) error {
	spec := userName + ":" + groupName
	if sudo {
		return run("chown", spec, abs)
	}
	uid, err := lookupUID(userName)
	if err != nil {
		return err
	}
	gid, err := lookupGID(groupName)
	if err != nil {
		return err
	}
	return unix.Lchown(abs, uid, gid)
}

func applyChmod(abs, modeStr string, sudo bool) error {
	// don't chmod symlinks — Linux ignores symlink mode and lchmod is not portable
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if sudo {
		return run("chmod", modeStr, abs)
	}
	mode, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil {
		return fmt.Errorf("invalid mode %q: %w", modeStr, err)
	}
	return os.Chmod(abs, os.FileMode(mode))
}

// applyXAttr decodes the base64 value and writes it via lsetxattr (no symlink follow).
// For sudo blobs, shells out to setfattr with hex-encoded value.
func applyXAttr(abs, name, b64val string, sudo bool) error {
	val, err := base64.StdEncoding.DecodeString(b64val)
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	if sudo {
		// setfattr accepts --value=0xHEXSTRING for binary values
		return run("setfattr", "-n", name, "--value=0x"+hex.EncodeToString(val), abs)
	}
	return unix.Lsetxattr(abs, name, val, 0)
}

// applyACL passes the stored getfacl --skip-base output to setfacl via stdin.
func applyACL(abs, acl string, sudo bool) error {
	args := []string{"setfacl", "-M", "-", abs}
	if sudo {
		args = append([]string{"sudo"}, args...)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(acl)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func applyCaps(abs, caps string, sudo bool) error {
	if sudo {
		return run("setcap", caps, abs)
	}
	if out, err := exec.Command("setcap", caps, abs).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{name}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func lookupUID(name string) (int, error) {
	if u, err := user.Lookup(name); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		return uid, nil
	}
	uid, err := strconv.Atoi(name)
	if err != nil {
		return -1, fmt.Errorf("unknown user %q", name)
	}
	return uid, nil
}

func lookupGID(name string) (int, error) {
	if g, err := user.LookupGroup(name); err == nil {
		gid, _ := strconv.Atoi(g.Gid)
		return gid, nil
	}
	gid, err := strconv.Atoi(name)
	if err != nil {
		return -1, fmt.Errorf("unknown group %q", name)
	}
	return gid, nil
}
