package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
)

func TestDefaultConfig_Loads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	if err := os.WriteFile(path, []byte(defaultConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("config.Load(init template) = %v", err)
	}
}
