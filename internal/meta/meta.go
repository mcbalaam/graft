package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const FileName = ".graft-meta.toml"

// FileMeta holds filesystem metadata for one file or directory.
// Paths in BlobMeta.Files are slash-separated, relative to the blob root.
// XAttr values are base64-encoded (xattrs can be arbitrary bytes).
type FileMeta struct {
	Mode  string            `toml:"mode"`
	User  string            `toml:"user"`
	Group string            `toml:"group"`
	Caps  string            `toml:"caps,omitempty"`
	ACL   string            `toml:"acl,omitempty"`
	XAttr map[string]string `toml:"xattr,omitempty"`
}

// BlobMeta is the root of .graft-meta.toml, committed inside each blob's repo.
type BlobMeta struct {
	Files map[string]FileMeta `toml:"files"`
}

func Load(blobPath string) (*BlobMeta, error) {
	data, err := os.ReadFile(filepath.Join(blobPath, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return &BlobMeta{Files: make(map[string]FileMeta)}, nil
		}
		return nil, fmt.Errorf("read %s: %w", FileName, err)
	}
	var m BlobMeta
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", FileName, err)
	}
	if m.Files == nil {
		m.Files = make(map[string]FileMeta)
	}
	return &m, nil
}

func Save(blobPath string, m *BlobMeta) error {
	var sb strings.Builder
	for rel, fm := range m.Files {
		sb.WriteString(fmt.Sprintf("[files.%q]\n", rel))
		sb.WriteString(fmt.Sprintf("mode  = %q\n", fm.Mode))
		sb.WriteString(fmt.Sprintf("user  = %q\n", fm.User))
		sb.WriteString(fmt.Sprintf("group = %q\n", fm.Group))
		if fm.Caps != "" {
			sb.WriteString(fmt.Sprintf("caps  = %q\n", fm.Caps))
		}
		if fm.ACL != "" {
			sb.WriteString(fmt.Sprintf("acl   = %q\n", fm.ACL))
		}
		if len(fm.XAttr) > 0 {
			sb.WriteString(fmt.Sprintf("[files.%q.xattr]\n", rel))
			for k, v := range fm.XAttr {
				sb.WriteString(fmt.Sprintf("%q = %q\n", k, v))
			}
		}
		sb.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(blobPath, FileName), []byte(sb.String()), 0644)
}

// NonDefaultFiles returns paths whose mode, user, or group differ from
// the standard defaults (0644/root:root for files, 0755/root:root for dirs).
// Used to detect whether to suggest enabling meta tracking.
func (m *BlobMeta) NonDefaultFiles() []string {
	var out []string
	for path, fm := range m.Files {
		if fm.User != "root" || fm.Group != "root" ||
			(fm.Mode != "0644" && fm.Mode != "0755" && fm.Mode != "0777") ||
			fm.Caps != "" || fm.ACL != "" || len(fm.XAttr) > 0 {
			out = append(out, path)
		}
	}
	return out
}
