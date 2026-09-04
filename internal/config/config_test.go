package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBlob(t *testing.T) {
	cases := []struct {
		raw     string
		path    string
		sudo    bool
		imm     bool
		meta    bool
		wantErr bool
	}{
		{raw: `"/home/u/.config/nvim"`, path: "/home/u/.config/nvim"},
		{raw: `"/etc/x" sudo`, path: "/etc/x", sudo: true},
		{raw: `/etc/y sudo immutable meta`, path: "/etc/y", sudo: true, imm: true, meta: true},
		{raw: "/plain/path", path: "/plain/path"},
		{raw: `"/unclosed sudo`, wantErr: true},
		{raw: "", wantErr: true},
		{raw: `/p bogus`, wantErr: true},
	}
	for _, c := range cases {
		blob, err := parseBlob(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseBlob(%q): expected error, got %+v", c.raw, blob)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseBlob(%q): %v", c.raw, err)
			continue
		}
		if blob.Path != c.path || blob.Sudo != c.sudo || blob.Immutable != c.imm || blob.Meta != c.meta {
			t.Errorf("parseBlob(%q) = %+v", c.raw, blob)
		}
	}
}

func TestParseBlobHomeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	blob, err := parseBlob(`"~/.config/nvim" meta`)
	if err != nil {
		t.Fatal(err)
	}
	if blob.Path != filepath.Join(home, ".config", "nvim") {
		t.Fatalf("expected ~ expansion, got %q", blob.Path)
	}
	if !blob.Meta {
		t.Fatal("meta flag lost")
	}
}

func TestDeriveBaseURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:user/repo.git": "git@github.com:user",
		"https://github.com/user/repo": "https://github.com/user",
		"/local/path.git":              "/local",
	}
	for remote, want := range cases {
		if got := DeriveBaseURL(remote); got != want {
			t.Errorf("DeriveBaseURL(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestLocalConfigRoundTripVerbose(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graft.toml")
	t.Setenv("GRAFT_CONFIG", cfgPath)

	local := &Config{
		AccessToken:     "tok",
		Verbose:         true,
		activeName:      "master",
		repos:           map[string]string{"master": "/repo/main"},
		localConfigPath: cfgPath,
	}
	if err := saveLocalConfig(local); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "verbose_output = true") {
		t.Fatalf("verbose_output not persisted:\n%s", data)
	}

	if fi, err := os.Stat(cfgPath); err == nil && fi.Mode().Perm() != 0600 {
		t.Fatalf("local config must stay 0600, got %v", fi.Mode().Perm())
	}

	if !LoadVerbose() {
		t.Fatal("LoadVerbose should read the saved flag")
	}

	// missing config → default false
	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	if LoadVerbose() {
		t.Fatal("LoadVerbose should default to false without a config")
	}
}
