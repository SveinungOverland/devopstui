// Package config loads and persists user settings and the last-used context.
package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration.
type Config struct {
	Org     string `yaml:"org"` // e.g. https://dev.azure.com/contoso
	PAT     string `yaml:"pat,omitempty"`
	Project string `yaml:"project,omitempty"`
	Team    string `yaml:"team,omitempty"`
	// ConfirmWrites asks before single-item edits too. Bulk changes and
	// re-parenting always ask. Default false.
	ConfirmWrites *bool `yaml:"confirm_writes,omitempty"`
	// RefreshSeconds enables auto refresh when > 0.
	RefreshSeconds int `yaml:"refresh_seconds,omitempty"`
	// Editor for descriptions. Empty falls back to $VISUAL, then $EDITOR,
	// then the built-in editor. "inline" forces the built-in one.
	Editor string `yaml:"editor,omitempty"`
}

// EditorCommand returns the external editor to use, or "" for the built-in.
func (c Config) EditorCommand() string {
	switch {
	case c.Editor == "inline":
		return ""
	case c.Editor != "":
		return c.Editor
	case os.Getenv("VISUAL") != "":
		return os.Getenv("VISUAL")
	default:
		return os.Getenv("EDITOR")
	}
}

// Confirm returns whether single-item writes need confirmation.
func (c Config) Confirm() bool { return c.ConfirmWrites != nil && *c.ConfirmWrites }

// Path returns the config file location.
func Path() string {
	if p := os.Getenv("DEVOPSTUI_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "devopstui", "config.yaml")
}

// Load reads the config file and applies environment overrides. A missing
// file is not an error.
func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	if err == nil {
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, err
		}
	}
	if v := os.Getenv("AZURE_DEVOPS_ORG_URL"); v != "" {
		c.Org = v
	}
	if v := os.Getenv("AZURE_DEVOPS_EXT_PAT"); v != "" {
		c.PAT = v
	}
	if v := os.Getenv("DEVOPSTUI_PAT"); v != "" {
		c.PAT = v
	}
	return c, nil
}

// Save writes the config back (used to remember the last context).
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
