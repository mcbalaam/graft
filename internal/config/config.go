package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ~/.config/graft.toml — machine-local, never committed
type localFile struct {
	Repo        string `toml:"repo"`
	GitHubToken string `toml:"github_token"`
}

// <repo>/graft.toml — versioned, lives inside the main git repo
type repoFile struct {
	Master rawMaster         `toml:"master"`
	Blobs  map[string]string `toml:"blobs"`
}

type rawMaster struct {
	Remote          string `toml:"remote"`
	BaseURL         string `toml:"base_url"`
	SubmoduleNaming string `toml:"submodule_naming"`
}

type Master struct {
	Remote          string
	BaseURL         string
	SubmoduleNaming string
}

type Blob struct {
	Path      string
	Sudo      bool
	Immutable bool
}

type Config struct {
	Master      Master
	Blobs       map[string]Blob
	GitHubToken string
	Repo        string // absolute path to the main git repo

	repoConfigPath  string // <repo>/graft.toml
	localConfigPath string // ~/.config/graft.toml
}

// Load reads the local config to find the repo, then loads the repo config.
func Load() (*Config, error) {
	localPath, err := localConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(localPath)
}

func LoadFrom(localPath string) (*Config, error) {
	// 1. read local config
	localData, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found at %s, run 'graft init' first", localPath)
		}
		return nil, fmt.Errorf("cannot read local config: %w", err)
	}

	var lf localFile
	if _, err := toml.Decode(string(localData), &lf); err != nil {
		return nil, fmt.Errorf("cannot parse local config: %w", err)
	}
	if lf.Repo == "" {
		return nil, fmt.Errorf("'repo' not set in %s", localPath)
	}

	// 2. read repo config
	repoConfigPath := filepath.Join(lf.Repo, "graft.toml")
	repoData, err := os.ReadFile(repoConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("repo config not found at %s, run 'graft init' first", repoConfigPath)
		}
		return nil, fmt.Errorf("cannot read repo config: %w", err)
	}

	var rf repoFile
	if _, err := toml.Decode(string(repoData), &rf); err != nil {
		return nil, fmt.Errorf("cannot parse repo config: %w", err)
	}

	cfg := &Config{
		Master: Master{
			Remote:          rf.Master.Remote,
			BaseURL:         rf.Master.BaseURL,
			SubmoduleNaming: rf.Master.SubmoduleNaming,
		},
		Blobs:           make(map[string]Blob),
		GitHubToken:     lf.GitHubToken,
		Repo:            lf.Repo,
		repoConfigPath:  repoConfigPath,
		localConfigPath: localPath,
	}

	if cfg.Master.SubmoduleNaming == "" {
		cfg.Master.SubmoduleNaming = "config_{name}"
	}

	for name, raw := range rf.Blobs {
		blob, err := parseBlob(raw)
		if err != nil {
			return nil, fmt.Errorf("blob '%s': %w", name, err)
		}
		cfg.Blobs[name] = blob
	}

	return cfg, nil
}

// Init creates both config files. Called by graft init.
func Init(remote, repoPath, baseURL, token string) (*Config, error) {
	localPath, err := localConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(localPath); err == nil {
		return nil, fmt.Errorf("local config already exists at %s", localPath)
	}

	repoConfigPath := filepath.Join(repoPath, "graft.toml")

	cfg := &Config{
		Master: Master{
			Remote:          remote,
			BaseURL:         baseURL,
			SubmoduleNaming: "config_{name}",
		},
		Blobs:           make(map[string]Blob),
		GitHubToken:     token,
		Repo:            repoPath,
		repoConfigPath:  repoConfigPath,
		localConfigPath: localPath,
	}

	if err := saveLocalConfig(cfg); err != nil {
		return nil, err
	}
	if err := saveRepoConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Save() error {
	return saveRepoConfig(c)
}

func saveLocalConfig(cfg *Config) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("repo         = %q\n", cfg.Repo))
	sb.WriteString(fmt.Sprintf("github_token = %q\n", cfg.GitHubToken))

	if err := os.MkdirAll(filepath.Dir(cfg.localConfigPath), 0755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}
	return os.WriteFile(cfg.localConfigPath, []byte(sb.String()), 0600)
}

func saveRepoConfig(cfg *Config) error {
	var sb strings.Builder

	sb.WriteString("[master]\n")
	sb.WriteString(fmt.Sprintf("remote           = %q\n", cfg.Master.Remote))
	sb.WriteString(fmt.Sprintf("base_url         = %q\n", cfg.Master.BaseURL))
	sb.WriteString(fmt.Sprintf("submodule_naming = %q\n", cfg.Master.SubmoduleNaming))
	sb.WriteString("\n[blobs]\n")

	for name, blob := range cfg.Blobs {
		var flags []string
		if blob.Sudo {
			flags = append(flags, "sudo")
		}
		if blob.Immutable {
			flags = append(flags, "immutable")
		}
		if len(flags) > 0 {
			sb.WriteString(fmt.Sprintf("%s = %q %s\n", name, blob.Path, strings.Join(flags, " ")))
		} else {
			sb.WriteString(fmt.Sprintf("%s = %q\n", name, blob.Path))
		}
	}

	if err := os.MkdirAll(filepath.Dir(cfg.repoConfigPath), 0755); err != nil {
		return fmt.Errorf("cannot create repo config directory: %w", err)
	}
	return os.WriteFile(cfg.repoConfigPath, []byte(sb.String()), 0644)
}

func (c *Config) SubmoduleName(blobName string) string {
	return strings.ReplaceAll(c.Master.SubmoduleNaming, "{name}", blobName)
}

func (c *Config) HasBlob(name string) bool {
	_, ok := c.Blobs[name]
	return ok
}

func (c *Config) HasBlobPath(path string) (string, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	for name, blob := range c.Blobs {
		blobAbs, err := filepath.Abs(blob.Path)
		if err != nil {
			continue
		}
		if blobAbs == absPath {
			return name, true
		}
	}
	return "", false
}

func (c *Config) AddBlob(name, path string, sudo, immutable bool) error {
	c.Blobs[name] = Blob{Path: path, Sudo: sudo, Immutable: immutable}
	return c.Save()
}

func (c *Config) UpdateBlobPath(name, newPath string) error {
	blob, ok := c.Blobs[name]
	if !ok {
		return fmt.Errorf("blob '%s' not found", name)
	}
	blob.Path = newPath
	c.Blobs[name] = blob
	return c.Save()
}

func (c *Config) RemoveBlob(name string) error {
	if _, ok := c.Blobs[name]; !ok {
		return fmt.Errorf("blob '%s' not found", name)
	}
	delete(c.Blobs, name)
	return c.Save()
}

func (c *Config) RepoConfigPath() string {
	return c.repoConfigPath
}

// parseBlob парсит строку вида: "/path/to/dir" sudo immutable
func parseBlob(raw string) (Blob, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Blob{}, fmt.Errorf("empty blob definition")
	}

	var path string
	var rest string

	if raw[0] == '"' {
		end := strings.Index(raw[1:], `"`)
		if end == -1 {
			return Blob{}, fmt.Errorf("unclosed quote in path")
		}
		path = raw[1 : end+1]
		rest = strings.TrimSpace(raw[end+2:])
	} else {
		parts := strings.SplitN(raw, " ", 2)
		path = parts[0]
		if len(parts) > 1 {
			rest = parts[1]
		}
	}

	blob := Blob{Path: path}
	for _, flag := range strings.Fields(rest) {
		switch flag {
		case "sudo":
			blob.Sudo = true
		case "immutable":
			blob.Immutable = true
		default:
			return Blob{}, fmt.Errorf("unknown flag '%s'", flag)
		}
	}

	return blob, nil
}

func localConfigPath() (string, error) {
	if env := os.Getenv("GRAFT_CONFIG"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "graft.toml"), nil
}
