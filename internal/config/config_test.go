package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/test/testutil"
)

func TestConfig_Structure(t *testing.T) {
	// Test that Config struct can be created with all fields
	cfg := Config{
		Clouds: CloudsConfig{
			AWS: []AWSAccount{
				{
					Profile: "default",
					Regions: []string{"us-east-1"},
				},
			},
		},
		Policy: PolicyConfig{
			Required: []TagRequirement{
				{
					Name:   "environment",
					Values: []string{"dev", "staging", "prod"},
				},
				{
					Name:    "owner",
					Pattern: "^.+@.+$",
				},
			},
			Optional: []TagRequirement{
				{Name: "project"},
			},
		},
		Rules: RulesConfig{
			Infer: []InferRule{
				{
					Tag: "environment",
					FromName: []NamePattern{
						{Pattern: "-prod-", Value: "prod"},
					},
				},
			},
			Inherit: []InheritRule{
				{
					Resource: "aws_ebs_volume",
					From:     "attached_instance",
					Tags:     []string{"environment", "owner"},
				},
			},
			Defaults: []DefaultRule{
				{
					Resource: "*",
					When:     map[string]string{"tag:owner": "absent"},
					Set:      map[string]string{"owner": "platform@company.com"},
				},
			},
		},
		Ignore: IgnoreConfig{
			Resources: []string{"aws_iam_*"},
			Tags:      map[string][]string{"managed-by": {"terraform"}},
		},
	}

	// Verify required tags
	if len(cfg.Policy.Required) != 2 {
		t.Errorf("Expected 2 required tags, got %d", len(cfg.Policy.Required))
	}

	// Verify AWS accounts
	if len(cfg.Clouds.AWS) != 1 {
		t.Errorf("Expected 1 AWS account, got %d", len(cfg.Clouds.AWS))
	}

	// Verify infer rules
	if len(cfg.Rules.Infer) != 1 {
		t.Errorf("Expected 1 infer rule, got %d", len(cfg.Rules.Infer))
	}
}

func TestTagRequirement_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     TagRequirement
		hasEnum bool
	}{
		{
			name: "values based",
			req: TagRequirement{
				Name:   "environment",
				Values: []string{"dev", "prod"},
			},
			hasEnum: true,
		},
		{
			name: "pattern based",
			req: TagRequirement{
				Name:    "email",
				Pattern: "^.+@.+$",
			},
			hasEnum: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasValues := len(tt.req.Values) > 0
			if hasValues != tt.hasEnum {
				t.Errorf("hasValues = %v, want %v", hasValues, tt.hasEnum)
			}
		})
	}
}

// Tests using testdata fixtures

func TestLoad_ValidConfig(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("valid.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify AWS accounts loaded
	if len(cfg.Clouds.AWS) != 2 {
		t.Errorf("Expected 2 AWS accounts, got %d", len(cfg.Clouds.AWS))
	}

	// Verify K8s clusters loaded
	if len(cfg.Clouds.Kubernetes) != 2 {
		t.Errorf("Expected 2 K8s clusters, got %d", len(cfg.Clouds.Kubernetes))
	}

	// Verify policy loaded
	if len(cfg.Policy.Required) != 3 {
		t.Errorf("Expected 3 required tags, got %d", len(cfg.Policy.Required))
	}

	// Verify rules loaded
	if len(cfg.Rules.Infer) != 1 {
		t.Errorf("Expected 1 infer rule, got %d", len(cfg.Rules.Infer))
	}
	if len(cfg.Rules.Inherit) != 1 {
		t.Errorf("Expected 1 inherit rule, got %d", len(cfg.Rules.Inherit))
	}
	if len(cfg.Rules.Defaults) != 1 {
		t.Errorf("Expected 1 default rule, got %d", len(cfg.Rules.Defaults))
	}
}

func TestLoad_MinimalConfig(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("minimal.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Clouds.AWS) != 1 {
		t.Errorf("Expected 1 AWS account, got %d", len(cfg.Clouds.AWS))
	}

	if cfg.Clouds.AWS[0].Profile != "default" {
		t.Errorf("Expected profile 'default', got %q", cfg.Clouds.AWS[0].Profile)
	}

	if len(cfg.Policy.Required) != 1 {
		t.Errorf("Expected 1 required tag, got %d", len(cfg.Policy.Required))
	}
}

func TestLoad_AWSOnlyConfig(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("aws-only.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Clouds.AWS) != 2 {
		t.Errorf("Expected 2 AWS accounts, got %d", len(cfg.Clouds.AWS))
	}

	if len(cfg.Clouds.Kubernetes) != 0 {
		t.Errorf("Expected 0 K8s clusters, got %d", len(cfg.Clouds.Kubernetes))
	}

	// Check first AWS account has correct regions
	if len(cfg.Clouds.AWS[0].Regions) != 3 {
		t.Errorf("Expected 3 regions for first account, got %d", len(cfg.Clouds.AWS[0].Regions))
	}
}

func TestLoad_K8sOnlyConfig(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("k8s-only.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Clouds.AWS) != 0 {
		t.Errorf("Expected 0 AWS accounts, got %d", len(cfg.Clouds.AWS))
	}

	if len(cfg.Clouds.Kubernetes) != 2 {
		t.Errorf("Expected 2 K8s clusters, got %d", len(cfg.Clouds.Kubernetes))
	}

	// Check first K8s cluster
	if cfg.Clouds.Kubernetes[0].Name != "production" {
		t.Errorf("Expected cluster name 'production', got %q", cfg.Clouds.Kubernetes[0].Name)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Load() expected error for non-existent file")
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("valid.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestValidate_NoCloudsIsAllowed(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("invalid-missing-clouds.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v; scan runs in demo mode and evaluate reads JSON, neither needs a cloud", err)
	}
}

func TestValidate_AWSAccounts(t *testing.T) {
	cases := []struct {
		name    string
		aws     []AWSAccount
		wantErr string
	}{
		{"default credential chain with no regions", []AWSAccount{{}}, ""},
		{"default credential chain with regions", []AWSAccount{{Regions: []string{"us-east-1", "eu-west-1"}}}, ""},
		{"profile", []AWSAccount{{Profile: "dev", Regions: []string{"us-gov-west-1"}}}, ""},
		{"two profiles", []AWSAccount{{Profile: "a"}, {Profile: "b"}}, ""},
		{"role per account from one base credential", []AWSAccount{{RoleARN: "arn:aws:iam::111111111111:role/Audit"}, {RoleARN: "arn:aws:iam::222222222222:role/Audit"}}, ""},
		{"role with every option", []AWSAccount{{Profile: "base", RoleARN: "arn:aws-us-gov:iam::111111111111:role/path/Audit", ExternalID: "x", SessionDuration: 900, RoleSessionName: "audit", MFASerial: "arn:aws:iam::111111111111:mfa/pedro"}}, ""},
		{"static keys in the file", []AWSAccount{{Unknown: map[string]any{"access_key_id": "AKIA", "secret_access_key": "s"}}}, "access_key_id is not read from the config file"},
		{"unknown field", []AWSAccount{{Unknown: map[string]any{"regoins": []string{"us-east-1"}}}}, `unknown field "regoins"`},
		{"external_id without role", []AWSAccount{{ExternalID: "x"}}, "external_id requires role_arn"},
		{"session_duration without role", []AWSAccount{{SessionDuration: 3600}}, "session_duration requires role_arn"},
		{"role_session_name without role", []AWSAccount{{RoleSessionName: "x"}}, "role_session_name requires role_arn"},
		{"mfa_serial without role", []AWSAccount{{MFASerial: "arn:aws:iam::111111111111:mfa/p"}}, "mfa_serial requires role_arn"},
		{"role arn is not a role", []AWSAccount{{RoleARN: "arn:aws:iam::111111111111:user/pedro"}}, "invalid role_arn"},
		{"session too short", []AWSAccount{{RoleARN: "arn:aws:iam::111111111111:role/A", SessionDuration: 899}}, "between 900 and 43200"},
		{"session too long", []AWSAccount{{RoleARN: "arn:aws:iam::111111111111:role/A", SessionDuration: 43201}}, "between 900 and 43200"},
		{"mfa serial is not a device", []AWSAccount{{RoleARN: "arn:aws:iam::111111111111:role/A", MFASerial: "123456"}}, "invalid mfa_serial"},
		{"regions split into a second entry", []AWSAccount{{Profile: "a"}, {Regions: []string{"us-east-1"}}}, "nest them under the previous account"},
		{"region typo", []AWSAccount{{Regions: []string{"eu-west1"}}}, `invalid region "eu-west1"`},
		{"region upper case", []AWSAccount{{Regions: []string{"EU-WEST-1"}}}, `invalid region "EU-WEST-1"`},
		{"region twice", []AWSAccount{{Regions: []string{"us-east-1", "us-east-1"}}}, "listed twice"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Clouds: CloudsConfig{AWS: tc.aws}}
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidate_BadRegex(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("invalid-bad-regex.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Error("Validate() expected error for bad regex")
	}
}

func TestLoad_VerifyIgnoreConfig(t *testing.T) {
	cfg, err := Load(testutil.ConfigPath("valid.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Ignore.Resources) != 2 {
		t.Errorf("Expected 2 ignore resource patterns, got %d", len(cfg.Ignore.Resources))
	}

	if len(cfg.Ignore.Tags) != 1 {
		t.Errorf("Expected 1 ignore tag pattern, got %d", len(cfg.Ignore.Tags))
	}
}

func TestLoad_RejectsStaticKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	body := "clouds:\n  aws:\n    - access_key_id: AKIA\n      secret_access_key: s\n      regions: [us-east-1]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "access_key_id is not read from the config file") {
		t.Fatalf("Validate() = %v, want the static-key rejection", err)
	}
}

func TestLoad_AssumeRoleFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	body := "clouds:\n  aws:\n    - profile: base\n      role_arn: arn:aws:iam::111111111111:role/Audit\n      external_id: ext\n      session_duration: 1800\n      role_session_name: audit\n      mfa_serial: arn:aws:iam::111111111111:mfa/pedro\n      regions: [us-east-1]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	acc := cfg.Clouds.AWS[0]
	if acc.Profile != "base" || acc.RoleARN != "arn:aws:iam::111111111111:role/Audit" || acc.ExternalID != "ext" ||
		acc.SessionDuration != 1800 || acc.RoleSessionName != "audit" || acc.MFASerial != "arn:aws:iam::111111111111:mfa/pedro" || len(acc.Unknown) != 0 {
		t.Errorf("account = %+v", acc)
	}
}
