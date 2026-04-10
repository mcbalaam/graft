package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const defaultConfigName = "graft.toml"

type Blob struct {
	Path      string
	Sudo      bool
	Immutable bool
}

type Master struct {
	Remote          string `toml:"remote"`
	Repo            string `toml:"repo"`
	SubmoduleNaming string `toml:"submodule_naming"`
	BaseURL         string `toml:"base_url"`
}

type Config struct {
	Master Master          `toml:"master"`
	Blobs  map[string]Blob `toml:"-"`

	path string
}

// raw для парсинга — BurntSushi не умеет кастомные строки в map
type rawConfig struct {
	Master Master            `toml:"master"`
	Blobs  map[string]string `toml:"blobs"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", defaultConfigName), nil
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found at %s, run 'graft init' first", path)
		}
		return nil, fmt.Errorf("cannot read config: %w", err)
	}

	var raw rawConfig
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("cannot parse config: %w", err)
	}

	cfg := &Config{
		Master: raw.Master,
		Blobs:  make(map[string]Blob),
		path:   path,
	}

	// применяем дефолты
	if cfg.Master.SubmoduleNaming == "" {
		cfg.Master.SubmoduleNaming = "config_{name}"
	}

	for name, raw := range raw.Blobs {
		blob, err := parseBlob(raw)
		if err != nil {
			return nil, fmt.Errorf("blob '%s': %w", name, err)
		}
		cfg.Blobs[name] = blob
	}

	return cfg, nil
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
	c.Blobs[name] = Blob{
		Path:      path,
		Sudo:      sudo,
		Immutable: immutable,
	}
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

func (c *Config) Save() error {
	return SaveTo(c, c.path)
}

func SaveTo(cfg *Config, path string) error {
	var sb strings.Builder

	sb.WriteString("[master]\n")
	sb.WriteString(fmt.Sprintf("remote           = %q\n", cfg.Master.Remote))
	sb.WriteString(fmt.Sprintf("repo             = %q\n", cfg.Master.Repo))
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

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// Init создаёт новый конфиг с дефолтными значениями
func Init(remote, repo, baseURL string) (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("config already exists at %s", path)
	}

	cfg := &Config{
		Master: Master{
			Remote:          remote,
			Repo:            repo,
			BaseURL:         baseURL,
			SubmoduleNaming: "config_{name}",
		},
		Blobs: make(map[string]Blob),
		path:  path,
	}

	if err := SaveTo(cfg, path); err != nil {
		return nil, err
	}

	return cfg, nil
}

func configPath() (string, error) {
	if env := os.Getenv("GRAFT_CONFIG"); env != "" {
		return env, nil
	}
	return DefaultPath()
}
