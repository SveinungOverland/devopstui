package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The shipped template must parse and produce the documented defaults.
func TestExampleConfigLoads(t *testing.T) {
	t.Setenv("AZURE_DEVOPS_ORG_URL", "")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "")
	t.Setenv("DEVOPSTUI_PAT", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	c, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Org != "https://dev.azure.com/contoso" || c.Project != "Platform" || c.Team != "Team Blue" {
		t.Errorf("context = %q %q %q", c.Org, c.Project, c.Team)
	}
	if c.PAT != "" {
		t.Error("template must not ship a PAT")
	}
	if c.Stale() != DefaultStaleDays {
		t.Errorf("stale = %d", c.Stale())
	}
	if c.Confirm() || c.RefreshSeconds != 0 || c.EditorCommand() != "" || c.WriteHTML() || c.HideDone || c.DashShowDone {
		t.Errorf("defaults: confirm=%v refresh=%d editor=%q html=%v hidedone=%v dashShowDone=%v", c.Confirm(), c.RefreshSeconds, c.EditorCommand(), c.WriteHTML(), c.HideDone, c.DashShowDone)
	}
}

func TestStaleDays(t *testing.T) {
	var c Config
	if c.Stale() != DefaultStaleDays || c.StaleAfter() != DefaultStaleDays*24*time.Hour {
		t.Errorf("unset: %d %v", c.Stale(), c.StaleAfter())
	}
	off, neg := 0, -3
	c.StaleDays = &off
	if c.Stale() != 0 || c.StaleAfter() != 0 {
		t.Errorf("off: %d", c.Stale())
	}
	c.StaleDays = &neg
	if c.Stale() != 0 {
		t.Errorf("negative: %d", c.Stale())
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("AZURE_DEVOPS_ORG_URL", "https://dev.azure.com/other")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "secret")
	c, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Org != "https://dev.azure.com/other" || c.PAT != "secret" {
		t.Errorf("env override failed: %q %q", c.Org, c.PAT)
	}
}

func TestMissingFileIsFine(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
}

func TestPathPrefersDevopstuiConfigEnv(t *testing.T) {
	t.Setenv("DEVOPSTUI_CONFIG", "/explicit/path/config.yaml")
	if got := Path(); got != "/explicit/path/config.yaml" {
		t.Errorf("Path() = %q, want explicit override", got)
	}
}

func TestPathDefaultsToXDGConfigWhenNothingExists(t *testing.T) {
	t.Setenv("DEVOPSTUI_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	want := filepath.Join(home, ".config", "devopstui", "config.yaml")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathHonoursXDGConfigHome(t *testing.T) {
	t.Setenv("DEVOPSTUI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))

	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "devopstui", "config.yaml")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathFallsBackToPlatformDirWhenOnlyItExists(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only darwin's UserConfigDir diverges from ~/.config")
	}
	t.Setenv("DEVOPSTUI_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	platformPath := filepath.Join(home, "Library", "Application Support", "devopstui", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(platformPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(platformPath, []byte("org: https://dev.azure.com/contoso\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := Path(); got != platformPath {
		t.Errorf("Path() = %q, want existing platform-dir file %q", got, platformPath)
	}
}
