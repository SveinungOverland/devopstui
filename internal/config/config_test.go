package config

import (
	"path/filepath"
	"testing"
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
	if c.Confirm() || c.RefreshSeconds != 0 || c.EditorCommand() != "" || c.WriteHTML() {
		t.Errorf("defaults: confirm=%v refresh=%d editor=%q html=%v", c.Confirm(), c.RefreshSeconds, c.EditorCommand(), c.WriteHTML())
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
