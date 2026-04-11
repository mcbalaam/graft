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
	Repo        string            `toml:"repo"`         // legacy: migrated to repos on first load
	Active      string            `toml:"active"`
	AccessToken string            `toml:"access_token"`
	Repos       map[string]string `toml:"repos"`
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
	Public          bool   `toml:"public"`
}

type Master struct {
	Remote          string
	BaseURL         string
	SubmoduleNaming string
	Public          bool
}

type Blob struct {
	Path      string
	Sudo      bool
	Immutable bool
}

type Config struct {
	Master      Master
	Blobs       map[string]Blob
	AccessToken string
	Repo        string // absolute path to the active main git repo

	activeName      string
	repos           map[string]string // name → absolute path
	repoConfigPath  string            // <repo>/graft.toml
	localConfigPath string            // ~/.config/graft.toml
}

// Load reads the local config to find the active repo, then loads the repo config.
func Load() (*Config, error) {
	localPath, err := localConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(localPath)
}

func LoadFrom(localPath string) (*Config, error) {
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

	// migrate legacy single-repo format
	if len(lf.Repos) == 0 && lf.Repo != "" {
		expanded, err := expandPath(lf.Repo)
		if err != nil {
			return nil, fmt.Errorf("cannot expand repo path: %w", err)
		}
		lf.Repos = map[string]string{"default": expanded}
		lf.Active = "default"
		lf.Repo = ""
		// save migrated config
		cfg := &Config{
			AccessToken:     lf.AccessToken,
			activeName:      lf.Active,
			repos:           lf.Repos,
			localConfigPath: localPath,
		}
		if err := saveLocalConfig(cfg); err != nil {
			return nil, fmt.Errorf("cannot migrate config: %w", err)
		}
	}

	if len(lf.Repos) == 0 {
		return nil, fmt.Errorf("no repos configured in %s, run 'graft init' first", localPath)
	}
	if lf.Active == "" {
		return nil, fmt.Errorf("'active' not set in %s", localPath)
	}

	repoPath, ok := lf.Repos[lf.Active]
	if !ok {
		return nil, fmt.Errorf("active repo '%s' not found in [repos]", lf.Active)
	}
	repoPath, err = expandPath(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot expand repo path: %w", err)
	}

	repoConfigPath := filepath.Join(repoPath, "graft.toml")
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

	// expand all repo paths
	repos := make(map[string]string, len(lf.Repos))
	for name, p := range lf.Repos {
		expanded, err := expandPath(p)
		if err != nil {
			return nil, fmt.Errorf("cannot expand path for repo '%s': %w", name, err)
		}
		repos[name] = expanded
	}

	cfg := &Config{
		Master: Master{
			Remote:          rf.Master.Remote,
			BaseURL:         rf.Master.BaseURL,
			SubmoduleNaming: rf.Master.SubmoduleNaming,
			Public:          rf.Master.Public,
		},
		Blobs:           make(map[string]Blob),
		AccessToken:     lf.AccessToken,
		Repo:            repoPath,
		activeName:      lf.Active,
		repos:           repos,
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

// DeriveBaseURL extracts the base URL from a remote URL by stripping the last path segment.
// git@github.com:user/repo.git  →  git@github.com:user
// https://github.com/user/repo  →  https://github.com/user
func DeriveBaseURL(remote string) string {
	remote = strings.TrimSuffix(remote, ".git")
	if i := strings.LastIndex(remote, "/"); i != -1 {
		return remote[:i]
	}
	return remote
}

// Init creates both config files for a new repo. Called by graft init.
func Init(remote, repoPath, name string, public bool) (*Config, error) {
	localPath, err := localConfigPath()
	if err != nil {
		return nil, err
	}

	repoConfigPath := filepath.Join(repoPath, "graft.toml")

	// if local config already exists, add this repo to it; otherwise create fresh
	var existingCfg *Config
	if _, err := os.Stat(localPath); err == nil {
		existingCfg, err = LoadFrom(localPath)
		if err != nil {
			return nil, fmt.Errorf("cannot load existing config: %w", err)
		}
	}

	cfg := &Config{
		Master: Master{
			Remote:          remote,
			BaseURL:         DeriveBaseURL(remote),
			SubmoduleNaming: "config_{name}",
			Public:          public,
		},
		Blobs:           make(map[string]Blob),
		Repo:            repoPath,
		repoConfigPath:  repoConfigPath,
		localConfigPath: localPath,
	}

	if existingCfg != nil {
		cfg.AccessToken = existingCfg.AccessToken
		cfg.repos = existingCfg.repos
	} else {
		cfg.repos = make(map[string]string)
	}

	cfg.repos[name] = repoPath
	cfg.activeName = name

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

func (c *Config) ActiveName() string {
	return c.activeName
}

func (c *Config) Repos() map[string]string {
	out := make(map[string]string, len(c.repos))
	for k, v := range c.repos {
		out[k] = v
	}
	return out
}

func (c *Config) AddRepo(name, path string) error {
	c.repos[name] = path
	return saveLocalConfig(c)
}

func (c *Config) RemoveRepo(name string) error {
	if _, ok := c.repos[name]; !ok {
		return fmt.Errorf("repo '%s' not found", name)
	}
	delete(c.repos, name)
	return saveLocalConfig(c)
}

func (c *Config) SetActive(name string) error {
	if _, ok := c.repos[name]; !ok {
		return fmt.Errorf("repo '%s' not found", name)
	}
	c.activeName = name
	return saveLocalConfig(c)
}

func saveLocalConfig(cfg *Config) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("active       = %q\n", cfg.activeName))
	sb.WriteString(fmt.Sprintf("access_token = %q\n", cfg.AccessToken))
	sb.WriteString("\n[repos]\n")
	for name, path := range cfg.repos {
		sb.WriteString(fmt.Sprintf("%s = %q\n", name, collapsePath(path)))
	}

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
	sb.WriteString(fmt.Sprintf("public           = %t\n", cfg.Master.Public))
	sb.WriteString("\n[blobs]\n")

	for name, blob := range cfg.Blobs {
		var flags []string
		if blob.Sudo {
			flags = append(flags, "sudo")
		}
		if blob.Immutable {
			flags = append(flags, "immutable")
		}
		collapsed := collapsePath(blob.Path)
		if len(flags) > 0 {
			sb.WriteString(fmt.Sprintf("%s = %q\n", name, collapsed+" "+strings.Join(flags, " ")))
		} else {
			sb.WriteString(fmt.Sprintf("%s = %q\n", name, collapsed))
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

	expanded, err := expandPath(path)
	if err != nil {
		return Blob{}, err
	}
	blob := Blob{Path: expanded}
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

// collapsePath replaces the home directory prefix with ~.
func collapsePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// expandPath replaces a leading ~ with the actual home directory.
func expandPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return home + path[1:], nil
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

// repoNameFromPath derives a short name from a repo path (last path segment).
func repoNameFromPath(path string) string {
	base := filepath.Base(path)
	if base == "" || base == "." {
		return "default"
	}
	return base
}
