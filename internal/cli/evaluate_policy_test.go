package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigFromPath_ValidatesPolicyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yaml")
	body := "policy:\n  required:\n    - name: owner\n      pattern: \"[invalid(regex\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfigFromPath(path)
	if err == nil || !strings.Contains(err.Error(), "policy.required[0]: invalid pattern") {
		t.Fatalf("loadConfigFromPath() = %v, want the invalid pattern rejection", err)
	}
}

func TestLoadConfigFromPath_UsesDiscoveredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	if err := os.WriteFile(path, []byte("policy:\n  optional:\n    - name: Team\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	useConfigFile(t, path)
	cfg, err := loadConfigFromPath("")
	if err != nil || cfg.Policy.Optional[0].Name != "Team" {
		t.Fatalf("loadConfigFromPath() = %+v, %v; want the discovered policy", cfg, err)
	}
}
