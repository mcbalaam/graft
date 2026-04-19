package meta

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Collect walks blobPath and returns metadata for every file/dir/symlink.
// .git and .graft-meta.toml are skipped.
// Errors on individual files are non-fatal: the entry is skipped with a warning printed.
func Collect(blobPath string) (*BlobMeta, error) {
	m := &BlobMeta{Files: make(map[string]FileMeta)}

	err := filepath.Walk(blobPath, func(abs string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("  ✗ meta: cannot stat %s: %v\n", abs, err)
			return nil
		}

		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		rel, _ := filepath.Rel(blobPath, abs)
		if rel == FileName {
			return nil
		}

		fm, err := collectOne(abs, info)
		if err != nil {
			fmt.Printf("  ✗ meta: %s: %v\n", rel, err)
			return nil
		}
		m.Files[rel] = fm
		return nil
	})
	return m, err
}

func collectOne(abs string, info os.FileInfo) (FileMeta, error) {
	fm := FileMeta{}

	fm.Mode = fmt.Sprintf("%04o", info.Mode().Perm())

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileMeta{}, fmt.Errorf("cannot read stat_t")
	}
	fm.User = resolveUID(stat.Uid)
	fm.Group = resolveGID(stat.Gid)

	// symlinks: mode+owner only, xattr/acl/caps don't apply
	if info.Mode()&os.ModeSymlink != 0 {
		return fm, nil
	}

	xattr, err := collectXAttr(abs)
	if err == nil && len(xattr) > 0 {
		fm.XAttr = xattr
	}

	if acl := collectACL(abs); acl != "" {
		fm.ACL = acl
	}

	if info.Mode().IsRegular() {
		if caps := collectCaps(abs); caps != "" {
			fm.Caps = caps
		}
	}

	return fm, nil
}

// collectXAttr reads all xattrs via lgetxattr (does not follow symlinks).
// Skips posix_acl entries — those are managed separately via getfacl/setfacl.
// Values are base64-encoded because xattr values are arbitrary bytes.
func collectXAttr(abs string) (map[string]string, error) {
	size, err := unix.Llistxattr(abs, nil)
	if err != nil || size == 0 {
		return nil, err
	}
	buf := make([]byte, size)
	n, err := unix.Llistxattr(abs, buf)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, name := range strings.Split(string(buf[:n]), "\x00") {
		if name == "" ||
			name == "system.posix_acl_access" ||
			name == "system.posix_acl_default" {
			continue
		}
		vsize, err := unix.Lgetxattr(abs, name, nil)
		if err != nil || vsize == 0 {
			continue
		}
		vbuf := make([]byte, vsize)
		vn, err := unix.Lgetxattr(abs, name, vbuf)
		if err != nil {
			continue
		}
		result[name] = base64.StdEncoding.EncodeToString(vbuf[:vn])
	}
	return result, nil
}

// collectACL calls getfacl --omit-header --skip-base.
// --skip-base omits the base user/group/other entries that duplicate mode bits,
// so a non-empty return means there are genuine extended ACL entries.
// Returns "" if getfacl is unavailable or no extended ACL exists.
func collectACL(abs string) string {
	out, err := exec.Command("getfacl", "--omit-header", "--skip-base", abs).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// collectCaps calls getcap and strips the path prefix from its output.
// getcap prints "path = cap_xxx+ep" or nothing.
// Returns "" if getcap is unavailable or the file has no capabilities.
func collectCaps(abs string) string {
	out, err := exec.Command("getcap", abs).Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return ""
	}
	if i := strings.Index(line, " = "); i != -1 {
		return line[i+3:]
	}
	return line
}

func resolveUID(uid uint32) string {
	if u, err := user.LookupId(fmt.Sprintf("%d", uid)); err == nil {
		return u.Username
	}
	return fmt.Sprintf("%d", uid)
}

func resolveGID(gid uint32) string {
	if g, err := user.LookupGroupId(fmt.Sprintf("%d", gid)); err == nil {
		return g.Name
	}
	return fmt.Sprintf("%d", gid)
}
