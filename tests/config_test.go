package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcbalaam/graft/internal/config"
)

func TestDeriveBaseURL(t *testing.T) {
	cases := []struct{ remote, want string }{
		{"git@github.com:user/repo.git", "git@github.com:user"},
		{"git@github.com:user/repo", "git@github.com:user"},
		{"https://github.com/user/repo.git", "https://github.com/user"},
		{"https://github.com/user/repo", "https://github.com/user"},
	}
	for _, c := range cases {
		if got := config.DeriveBaseURL(c.remote); got != c.want {
			t.Errorf("DeriveBaseURL(%q) = %q, want %q", c.remote, got, c.want)
		}
	}
}

func TestInitAndLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	repoPath := filepath.Join(tmp, "repo")
	os.MkdirAll(repoPath, 0755)

	remote := "git@github.com:user/backup.git"
	cfg, err := config.Init(remote, repoPath, "default", false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if cfg.Master.Remote != remote {
		t.Errorf("Master.Remote = %q, want %q", cfg.Master.Remote, remote)
	}
	if cfg.ActiveName() != "default" {
		t.Errorf("ActiveName = %q, want %q", cfg.ActiveName(), "default")
	}
}

func TestAddRemoveBlob(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	repoPath := filepath.Join(tmp, "repo")
	os.MkdirAll(repoPath, 0755)

	cfg, _ := config.Init("git@github.com:user/backup.git", repoPath, "default", false)

	if err := cfg.AddBlob("nginx", "/etc/nginx", true, false, true); err != nil {
		t.Fatalf("AddBlob: %v", err)
	}
	b, ok := cfg.Blobs["nginx"]
	if !ok {
		t.Fatal("blob 'nginx' not found after AddBlob")
	}
	if b.Path != "/etc/nginx" || !b.Sudo || !b.Meta {
		t.Errorf("unexpected blob: %+v", b)
	}

	if err := cfg.RemoveBlob("nginx"); err != nil {
		t.Fatalf("RemoveBlob: %v", err)
	}
	if _, ok := cfg.Blobs["nginx"]; ok {
		t.Error("blob still present after RemoveBlob")
	}
	if err := cfg.RemoveBlob("nonexistent"); err == nil {
		t.Error("RemoveBlob(nonexistent): expected error, got nil")
	}
}

func TestSetMeta(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	repoPath := filepath.Join(tmp, "repo")
	os.MkdirAll(repoPath, 0755)

	cfg, _ := config.Init("git@github.com:user/backup.git", repoPath, "default", false)
	cfg.AddBlob("nvim", "/etc/nvim", false, false, false)

	if err := cfg.SetMeta("nvim", true); err != nil {
		t.Fatalf("SetMeta true: %v", err)
	}
	if !cfg.Blobs["nvim"].Meta {
		t.Error("Meta should be true")
	}
	if err := cfg.SetMeta("nvim", false); err != nil {
		t.Fatalf("SetMeta false: %v", err)
	}
	if cfg.Blobs["nvim"].Meta {
		t.Error("Meta should be false")
	}
	if err := cfg.SetMeta("nonexistent", true); err == nil {
		t.Error("SetMeta(nonexistent): expected error")
	}
}

func TestBlobFlagsRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	localPath := filepath.Join(tmp, "graft.toml")
	t.Setenv("GRAFT_CONFIG", localPath)
	repoPath := filepath.Join(tmp, "repo")
	os.MkdirAll(repoPath, 0755)

	cfg, _ := config.Init("git@github.com:user/backup.git", repoPath, "default", false)
	cfg.AddBlob("waybar", "/etc/xdg/waybar", true, true, true)

	loaded, err := config.LoadFrom(localPath)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	b, ok := loaded.Blobs["waybar"]
	if !ok {
		t.Fatal("blob 'waybar' missing after reload")
	}
	if b.Path != "/etc/xdg/waybar" || !b.Sudo || !b.Immutable || !b.Meta {
		t.Errorf("flags not preserved: %+v", b)
	}
}

func TestBlobFlagsInvalidFlagErrors(t *testing.T) {
	tmp := t.TempDir()
	localPath := filepath.Join(tmp, "graft.toml")
	t.Setenv("GRAFT_CONFIG", localPath)
	repoPath := filepath.Join(tmp, "repo")
	os.MkdirAll(repoPath, 0755)

	// write a repo config with an unknown flag
	repoConfigPath := filepath.Join(repoPath, "graft.toml")
	os.WriteFile(repoConfigPath, []byte(`
[master]
remote           = "git@github.com:user/backup.git"
base_url         = "git@github.com:user"
submodule_naming = "config_{name}"
public           = false

[blobs]
nginx = "/etc/nginx bogus"
`), 0644)
	os.WriteFile(localPath, []byte(`
active       = "default"
access_token = ""

[repos]
default = "`+repoPath+`"
`), 0644)

	_, err := config.LoadFrom(localPath)
	if err == nil {
		t.Fatal("expected error for unknown blob flag, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should mention unknown flag, got: %v", err)
	}
}

func TestSubmoduleName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GRAFT_CONFIG", filepath.Join(tmp, "graft.toml"))
	repoPath := filepath.Join(tmp, "repo")
	os.MkdirAll(repoPath, 0755)

	cfg, _ := config.Init("git@github.com:user/backup.git", repoPath, "default", false)
	if got := cfg.SubmoduleName("nginx"); got != "config_nginx" {
		t.Errorf("SubmoduleName = %q, want %q", got, "config_nginx")
	}
}
