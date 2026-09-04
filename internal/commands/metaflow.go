package commands

import (
	"fmt"
	"sort"

	"github.com/mcbalaam/graft/internal/config"
	"github.com/mcbalaam/graft/internal/meta"
	"github.com/mcbalaam/graft/internal/prompt"
)

// confirmMeta shows files with non-default permissions/ownership and asks
// whether to enable meta tracking. name (when set) prefixes the listing.
// Returns enabled=false when there is nothing non-default to preserve.
func confirmMeta(name string, m *meta.BlobMeta, cancelLabel string) (bool, error) {
	nonDefault := m.NonDefaultFiles()
	if len(nonDefault) == 0 {
		return false, nil
	}

	sort.Strings(nonDefault)
	if name != "" {
		fmt.Printf("\n● %s: files with non-default permissions/ownership:\n", name)
	} else {
		fmt.Printf("● found files with non-default permissions/ownership:\n")
	}
	for _, p := range nonDefault {
		fm := m.Files[p]
		fmt.Printf("    %-40s %s:%s  %s\n", p, fm.User, fm.Group, fm.Mode)
	}

	choice, err := prompt.Query(
		"● enable meta tracking to preserve ownership/permissions on restore?",
		[]string{
			"yes, enable meta for this blob",
			"no, skip metadata tracking",
			cancelLabel,
		},
		1,
	)
	if err != nil {
		return false, fmt.Errorf("✗ prompt: %w", err)
	}
	switch choice {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, errCancelled
	}
}

// resolveMetaFlag returns whether meta tracking should be enabled for a new blob:
// immediately when metaFlag is set, otherwise after querying the user when
// non-default attributes exist.
func resolveMetaFlag(dir string, metaFlag bool) (bool, error) {
	if metaFlag {
		return true, nil
	}
	m, err := meta.Collect(dir)
	if err != nil {
		return false, fmt.Errorf("✗ meta collect: %w", err)
	}
	return confirmMeta("", m, "cancel")
}

// pushMeta collects metadata and writes .graft-meta.toml before git add.
// For blobs without the meta flag: detects non-default attributes and queries
// the user. Returns a short status message and any fatal error.
func pushMeta(cfg *config.Config, name string, blob config.Blob) (string, error) {
	m, err := meta.Collect(blob.Path)
	if err != nil {
		return "", fmt.Errorf("meta collect: %w", err)
	}

	enabled := blob.Meta
	if !enabled {
		enabled, err = confirmMeta(name, m, "cancel push")
		if err != nil {
			return "", fmt.Errorf("%w", err)
		}
		if enabled {
			if err := cfg.SetMeta(name, true); err != nil {
				return "", fmt.Errorf("✗ save config: %w", err)
			}
		}
	}

	if !enabled {
		return "", nil
	}
	if err := meta.Save(blob.Path, m); err != nil {
		return "", fmt.Errorf("✗ meta save: %w", err)
	}
	return "(+meta)", nil
}
