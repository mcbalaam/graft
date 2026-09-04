package commands

import (
	"testing"

	"github.com/mcbalaam/graft/internal/config"
)

func TestResolveTokenPriority(t *testing.T) {
	t.Setenv("GRAFT_TOKEN", "env-token")

	cfg := &config.Config{AccessToken: "cfg-token"}
	if got := resolveToken(cfg); got != "cfg-token" {
		t.Fatalf("config should win over env, got %q", got)
	}

	cfg = &config.Config{}
	if got := resolveToken(cfg); got != "env-token" {
		t.Fatalf("env should be second, got %q", got)
	}

	t.Setenv("GRAFT_TOKEN", "")
	if got := resolveToken(&config.Config{}); got == "env-token" {
		t.Fatal("empty env must not be used")
	}
}

func TestOwnerFromBaseURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:user":      "user",
		"https://github.com/user":  "user",
		"https://github.com/user/": "user",
		"git@github.com:":          "",
	}
	for base, want := range cases {
		if got := ownerFromBaseURL(base); got != want {
			t.Errorf("ownerFromBaseURL(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestGitExecFor(t *testing.T) {
	if gitExecFor(true) == nil || gitExecFor(false) == nil {
		t.Fatal("runner must not be nil")
	}
}

func TestSelectBlobs(t *testing.T) {
	cfg := &config.Config{Blobs: map[string]config.Blob{
		"nvim": {Path: "/a"},
		"tmux": {Path: "/b"},
	}}

	all, err := selectBlobs(cfg, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("empty name should return all blobs: %v, %v", all, err)
	}
	one, err := selectBlobs(cfg, "nvim")
	if err != nil || len(one) != 1 {
		t.Fatalf("named lookup failed: %v, %v", one, err)
	}
	if _, err := selectBlobs(cfg, "missing"); err == nil {
		t.Fatal("missing blob should error")
	}
}
