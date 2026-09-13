package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/unicrons/tagctl/internal/config"
)

func useConfigFile(t *testing.T, path string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(path)
}

func loadConfigFrom(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	useConfigFile(t, path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	return loadConfig()
}

func TestLoadConfig_RejectsStaticKeys(t *testing.T) {
	_, err := loadConfigFrom(t, "clouds:\n  aws:\n    - access_key_id: AKIA\n      secret_access_key: s\n")
	if err == nil || !strings.Contains(err.Error(), "access_key_id is not read from the config file") {
		t.Fatalf("loadConfig() = %v, want the static-key rejection", err)
	}
}

func TestLoadConfig_RejectsUnknownAccountField(t *testing.T) {
	_, err := loadConfigFrom(t, "clouds:\n  aws:\n    - profile: dev\n      regoins: [us-east-1]\n")
	if err == nil || !strings.Contains(err.Error(), `unknown field "regoins"`) {
		t.Fatalf("loadConfig() = %v, want the unknown-field rejection", err)
	}
}

func TestLoadConfig_AcceptsAssumeRole(t *testing.T) {
	_, err := loadConfigFrom(t, "clouds:\n  aws:\n    - profile: base\n      role_arn: arn:aws:iam::111111111111:role/Audit\n      session_duration: 1800\n      regions: [us-east-1]\n")
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
}

func TestLoadConfig_RejectsUnknownTopLevelKey(t *testing.T) {
	_, err := loadConfigFrom(t, "polcy:\n  required:\n    - name: owner\n")
	if err == nil || !strings.Contains(err.Error(), "line 1: field polcy not found") {
		t.Fatalf("loadConfig() = %v, want the unknown key with its line", err)
	}
}

func TestLoadConfig_PreservesMixedCaseTagKeys(t *testing.T) {
	cfg, err := loadConfigFrom(t, `policy:
  required:
    - name: CostCenter
rules:
  defaults:
    - resource: "*"
      when:
        tag:Owner: absent
      set:
        CostCenter: CC-1
ignore:
  tags:
    ManagedBy: [terraform]
`)
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	rule := cfg.Rules.Defaults[0]
	if _, ok := rule.When["tag:Owner"]; !ok {
		t.Errorf("when = %v, want key tag:Owner", rule.When)
	}
	if rule.Set["CostCenter"] != "CC-1" {
		t.Errorf("set = %v, want CostCenter: CC-1", rule.Set)
	}
	if _, ok := cfg.Ignore.Tags["ManagedBy"]; !ok {
		t.Errorf("ignore.tags = %v, want key ManagedBy", cfg.Ignore.Tags)
	}
}

func TestLoadConfig_MissingConfigFileFails(t *testing.T) {
	useConfigFile(t, filepath.Join(t.TempDir(), "missing.yaml"))
	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig() = nil, want an error for a --config file that does not exist")
	}
}

func TestInitConfig_ReadsAnyFileNameAsYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml.example")
	if err := os.WriteFile(path, []byte("policy:\n  required:\n    - name: owner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = previous })
	viper.Reset()
	t.Cleanup(viper.Reset)

	initConfig()

	if len(viper.GetStringMap("policy")) == 0 {
		t.Fatalf("viper read no policy from %s, so validate would report none", path)
	}
}

func TestLoadConfig_NoConfigFileIsEmptyConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cfg, err := loadConfig()
	if err != nil || len(cfg.Clouds.AWS) != 0 {
		t.Fatalf("loadConfig() = %+v, %v; want an empty config", cfg, err)
	}
}
