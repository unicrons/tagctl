package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func loadConfigFrom(t *testing.T, body string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig()
	return err
}

func TestLoadConfig_RejectsStaticKeys(t *testing.T) {
	err := loadConfigFrom(t, "clouds:\n  aws:\n    - access_key_id: AKIA\n      secret_access_key: s\n")
	if err == nil || !strings.Contains(err.Error(), "access_key_id is not read from the config file") {
		t.Fatalf("loadConfig() = %v, want the static-key rejection", err)
	}
}

func TestLoadConfig_RejectsUnknownAccountField(t *testing.T) {
	err := loadConfigFrom(t, "clouds:\n  aws:\n    - profile: dev\n      regoins: [us-east-1]\n")
	if err == nil || !strings.Contains(err.Error(), `unknown field "regoins"`) {
		t.Fatalf("loadConfig() = %v, want the unknown-field rejection", err)
	}
}

func TestLoadConfig_AcceptsAssumeRole(t *testing.T) {
	err := loadConfigFrom(t, "clouds:\n  aws:\n    - profile: base\n      role_arn: arn:aws:iam::111111111111:role/Audit\n      session_duration: 1800\n      regions: [us-east-1]\n")
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
}
