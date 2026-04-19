package tests_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/mcbalaam/graft/internal/meta"
	"golang.org/x/sys/unix"
)

// --- BlobMeta / Save / Load ---

func TestNonDefaultFiles(t *testing.T) {
	m := &meta.BlobMeta{Files: map[string]meta.FileMeta{
		"nginx.conf":      {Mode: "0644", User: "root", Group: "root"},
		"shadow":          {Mode: "0640", User: "root", Group: "shadow"},
		"sudoers":         {Mode: "0440", User: "root", Group: "root"},
		"ssl/private/key": {Mode: "0600", User: "root", Group: "ssl-cert"},
		"bin/helper":      {Mode: "0755", User: "root", Group: "root", Caps: "cap_net_bind_service+ep"},
		"labeled":         {Mode: "0644", User: "root", Group: "root", XAttr: map[string]string{"security.selinux": "abc"}},
		"dir/":            {Mode: "0755", User: "root", Group: "root"},
	}}

	nonDefault := m.NonDefaultFiles()
	set := make(map[string]bool, len(nonDefault))
	for _, p := range nonDefault {
		set[p] = true
	}

	for _, p := range []string{"shadow", "sudoers", "ssl/private/key", "bin/helper", "labeled"} {
		if !set[p] {
			t.Errorf("%q should be flagged as non-default", p)
		}
	}
	for _, p := range []string{"nginx.conf", "dir/"} {
		if set[p] {
			t.Errorf("%q should not be flagged as non-default", p)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	tmp := t.TempDir()

	original := &meta.BlobMeta{Files: map[string]meta.FileMeta{
		".": {Mode: "0755", User: "root", Group: "root"},
		"ssl/private/server.key": {
			Mode:  "0600",
			User:  "root",
			Group: "ssl-cert",
			ACL:   "user::rw-\ngroup::---\nother::---",
			Caps:  "cap_net_bind_service+ep",
			XAttr: map[string]string{"security.selinux": "dGVzdA=="},
		},
	}}

	if err := meta.Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := meta.Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Files) != len(original.Files) {
		t.Errorf("file count: got %d, want %d", len(loaded.Files), len(original.Files))
	}
	key := loaded.Files["ssl/private/server.key"]
	if key.Mode != "0600" || key.Group != "ssl-cert" || key.Caps != "cap_net_bind_service+ep" {
		t.Errorf("fields not preserved: %+v", key)
	}
	if key.XAttr["security.selinux"] != "dGVzdA==" {
		t.Errorf("xattr not preserved: %q", key.XAttr["security.selinux"])
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	m, err := meta.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load on missing file should not error: %v", err)
	}
	if m == nil || m.Files == nil || len(m.Files) != 0 {
		t.Errorf("expected empty BlobMeta, got: %+v", m)
	}
}

// --- Collect ---

func TestCollectMode(t *testing.T) {
	tmp := t.TempDir()
	cases := map[string]os.FileMode{
		"readable.conf": 0644,
		"private.key":   0600,
		"script.sh":     0755,
		"secret.txt":    0640,
	}
	for name, mode := range cases {
		p := filepath.Join(tmp, name)
		os.WriteFile(p, []byte("x"), mode)
		os.Chmod(p, mode) // override umask
	}

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for name, mode := range cases {
		fm, ok := m.Files[name]
		if !ok {
			t.Errorf("%q not collected", name)
			continue
		}
		if want := fmt.Sprintf("%04o", mode); fm.Mode != want {
			t.Errorf("%q: mode = %q, want %q", name, fm.Mode, want)
		}
	}
}

func TestCollectUserGroup(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "file.conf"), []byte("x"), 0644)

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	fm := m.Files["file.conf"]
	u, _ := user.Current()
	if fm.User != u.Username {
		t.Errorf("User = %q, want %q", fm.User, u.Username)
	}
	g, _ := user.LookupGroupId(u.Gid)
	if fm.Group != g.Name {
		t.Errorf("Group = %q, want %q", fm.Group, g.Name)
	}
}

func TestCollectSkipsGitDir(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(tmp, ".git", "config"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(tmp, "real.conf"), []byte("x"), 0644)

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for p := range m.Files {
		if len(p) >= 4 && p[:4] == ".git" {
			t.Errorf(".git entry leaked: %q", p)
		}
	}
	if _, ok := m.Files["real.conf"]; !ok {
		t.Error("real.conf should be collected")
	}
}

func TestCollectSkipsMetaFile(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, meta.FileName), []byte("[files]"), 0644)
	os.WriteFile(filepath.Join(tmp, "nginx.conf"), []byte("x"), 0644)

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if _, ok := m.Files[meta.FileName]; ok {
		t.Errorf("%q should be skipped", meta.FileName)
	}
}

func TestCollectSymlink(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "nginx.conf"), []byte("x"), 0644)
	os.Symlink("nginx.conf", filepath.Join(tmp, "nginx.conf.bak"))

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if _, ok := m.Files["nginx.conf.bak"]; !ok {
		t.Error("symlink should be collected")
	}
}

func TestCollectSubdir(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "ssl", "private"), 0755)
	p := filepath.Join(tmp, "ssl", "private", "server.key")
	os.WriteFile(p, []byte("KEY"), 0600)
	os.Chmod(p, 0600)

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	fm, ok := m.Files[filepath.Join("ssl", "private", "server.key")]
	if !ok {
		t.Fatal("ssl/private/server.key not collected")
	}
	if fm.Mode != "0600" {
		t.Errorf("mode = %q, want 0600", fm.Mode)
	}
}

func TestCollectRootEntry(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.conf"), []byte("x"), 0644)

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if _, ok := m.Files["."]; !ok {
		t.Error("root entry '.' should be collected")
	}
}

func TestCollectXAttr(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tagged.conf")
	os.WriteFile(p, []byte("x"), 0644)

	val := []byte("test-value")
	if err := unix.Setxattr(p, "user.test", val, 0); err != nil {
		t.Skipf("xattr not supported: %v", err)
	}

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	fm := m.Files["tagged.conf"]
	got, ok := fm.XAttr["user.test"]
	if !ok {
		t.Fatal("user.test xattr not collected")
	}
	if got != base64.StdEncoding.EncodeToString(val) {
		t.Errorf("xattr value = %q, want base64 of %q", got, val)
	}
}

func TestCollectACL(t *testing.T) {
	if _, err := exec.LookPath("getfacl"); err != nil {
		t.Skip("getfacl not available")
	}
	if _, err := exec.LookPath("setfacl"); err != nil {
		t.Skip("setfacl not available")
	}

	tmp := t.TempDir()
	p := filepath.Join(tmp, "acl.conf")
	os.WriteFile(p, []byte("x"), 0644)

	u, _ := user.Current()
	if out, err := exec.Command("setfacl", "-m", fmt.Sprintf("user:%s:r--", u.Username), p).CombinedOutput(); err != nil {
		t.Skipf("setfacl failed: %v: %s", err, out)
	}

	m, err := meta.Collect(tmp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if m.Files["acl.conf"].ACL == "" {
		t.Error("expected non-empty ACL")
	}
}
