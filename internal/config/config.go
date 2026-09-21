// Package config loads and persists user settings and the last-used context.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration.
type Config struct {
	Org     string `yaml:"org"` // e.g. https://dev.azure.com/contoso
	PAT     string `yaml:"pat,omitempty"`
	Project string `yaml:"project,omitempty"`
	Team    string `yaml:"team,omitempty"`
	// FilterTeam narrows all views to that team's area paths. Use it when
	// a sub-team shares the sprints of a parent team (team).
	FilterTeam string `yaml:"filter_team,omitempty"`
	// ConfirmWrites asks before single-item edits too. Bulk changes and
	// re-parenting always ask. Default false.
	ConfirmWrites *bool `yaml:"confirm_writes,omitempty"`
	// RefreshSeconds enables auto refresh when > 0.
	RefreshSeconds int `yaml:"refresh_seconds,omitempty"`
	// HideDone hides Done/Closed/Removed/Resolved/Completed items in the
	// Sprint view. Default false: they show, and `c` toggles them away.
	HideDone bool `yaml:"hide_done,omitempty"`
	// Editor for descriptions. Empty falls back to $VISUAL, then $EDITOR,
	// then the built-in editor. "inline" forces the built-in one.
	Editor string `yaml:"editor,omitempty"`
	// DescriptionFormat is how descriptions are written back: "markdown"
	// (default; the field is switched to Azure DevOps' native Markdown mode)
	// or "html" (Markdown is converted to HTML, for organisations that
	// have not enabled Markdown on work items).
	DescriptionFormat string `yaml:"description_format,omitempty"`
}

// WriteHTML reports whether descriptions should be sent as HTML.
func (c Config) WriteHTML() bool { return c.DescriptionFormat == "html" }

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

// Path returns the config file location: the first of the known candidate
// locations that already has a file, or the platform default if none do
// yet. DEVOPSTUI_CONFIG always wins outright.
func Path() string {
	if p := os.Getenv("DEVOPSTUI_CONFIG"); p != "" {
		return p
	}
	def := defaultPath()
	for _, p := range candidatePaths() {
		if p == def {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return def
}

// defaultPath is where a fresh config is written when none exists yet:
// ~/.config on macOS/Linux (matching Lazygit and K9s, even on macOS where
// os.UserConfigDir() disagrees), %AppData% on Windows (matching
// os.UserConfigDir() there too).
func defaultPath() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "devopstui", "config.yaml")
		}
	}
	return xdgConfigPath()
}

// candidatePaths lists known config locations besides defaultPath, in the
// order they should be searched: ~/.config first (the documented default,
// and where defaultPath itself points on macOS/Linux), then the
// platform-specific directory os.UserConfigDir() reports, as a fallback for
// files that landed there before this search existed.
func candidatePaths() []string {
	paths := []string{xdgConfigPath()}
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "devopstui", "config.yaml"))
	}
	return paths
}

// xdgConfigPath is ~/.config/devopstui/config.yaml, honouring
// XDG_CONFIG_HOME. It's the default on macOS/Linux, matching the
// convention of Lazygit and K9s (which use it even on macOS, unlike
// os.UserConfigDir()) and the location documented in the README.
func xdgConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".config")
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
