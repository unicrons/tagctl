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

func TestLoad_ValidatesConfig(t *testing.T) {
	_, err := Load(testutil.ConfigPath("invalid-bad-regex.yaml"))
	if err == nil || !strings.Contains(err.Error(), "policy.required[0]: invalid pattern") {
		t.Fatalf("Load() = %v, want the invalid pattern rejection", err)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_RejectsUnknownKeys(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"top-level typo", "polcy:\n  required:\n    - name: owner\n", "line 1: field polcy not found"},
		{"requirement typo", "policy:\n  required:\n    - name: environment\n      valuse: [dev]\n", "line 4: field valuse not found"},
		{"default rule plural key", "rules:\n  defaults:\n    - resources: \"*\"\n      set: {owner: a}\n", "line 3: field resources not found"},
		{"kubernetes unknown key", "clouds:\n  kubernetes:\n    - name: prod\n      cluster: prod\n", "line 4: field cluster not found"},
		{"aws account typo keeps its own message", "clouds:\n  aws:\n    - profile: dev\n      regoins: [us-east-1]\n", `unknown field "regoins"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoad_ValidatesKubernetesResourceTypes(t *testing.T) {
	body := "clouds:\n  kubernetes:\n    - name: prod\n      resource_types: [k8s_pod, k8s_pods]\n"
	_, err := Load(writeConfig(t, body))
	if err == nil || !strings.Contains(err.Error(), `kubernetes[0]: unknown resource type "k8s_pods"`) {
		t.Fatalf("Load() = %v, want the unknown resource type rejection", err)
	}
}

func TestLoad_RejectsRepeatedKubernetesClusterName(t *testing.T) {
	body := "clouds:\n  kubernetes:\n    - name: prod\n      context: a\n    - name: prod\n      context: b\n"
	_, err := Load(writeConfig(t, body))
	if err == nil || !strings.Contains(err.Error(), `kubernetes[1]: name "prod" is used by another cluster`) {
		t.Fatalf("Load() = %v, want the repeated cluster name rejection", err)
	}
}

func TestLoad_EmptyFileIsEmptyConfig(t *testing.T) {
	cfg, err := Load(writeConfig(t, "# nothing configured yet\n"))
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(cfg.Clouds.AWS) != 0 || len(cfg.Policy.Required) != 0 {
		t.Errorf("config = %+v, want empty", cfg)
	}
}

func TestLoad_AWSMappingDecodesAsOneAccount(t *testing.T) {
	cfg, err := Load(writeConfig(t, "clouds:\n  aws:\n    profile: dev\n    regions: [us-east-1]\n"))
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(cfg.Clouds.AWS) != 1 || cfg.Clouds.AWS[0].Profile != "dev" || len(cfg.Clouds.AWS[0].Regions) != 1 {
		t.Errorf("accounts = %+v, want the single dev account", cfg.Clouds.AWS)
	}
}

func TestLoad_ShippedConfigsStayValid(t *testing.T) {
	paths := []string{filepath.Join("..", "..", "tagctl.yaml.example")}
	for _, name := range []string{"valid.yaml", "minimal.yaml", "aws-only.yaml", "k8s-only.yaml", "invalid-missing-clouds.yaml"} {
		paths = append(paths, testutil.ConfigPath(name))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := Load(path); err != nil {
				t.Fatalf("Load() = %v", err)
			}
		})
	}
}

func TestValidate_PolicyRulesAndIgnore(t *testing.T) {
	awsDefault := func(set, when map[string]string) RulesConfig {
		return RulesConfig{Defaults: []DefaultRule{{Resource: "aws_*", When: when, Set: set}}}
	}
	inferFrom := func(tag string, source TagSource) RulesConfig {
		return RulesConfig{Infer: []InferRule{{Tag: tag, FromTag: []TagSource{source}}}}
	}
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"infer from another tag", Config{Rules: inferFrom("environment", TagSource{Tag: "env", Values: map[string]string{"production": "prod"}})}, ""},
		{"infer from tag without source tag", Config{Rules: inferFrom("environment", TagSource{})}, "rules.infer[0].from_tag[0]: tag is required"},
		{"infer from tag without target tag", Config{Rules: inferFrom("", TagSource{Tag: "env"})}, "rules.infer[0].from_tag[0]: the rule needs a tag to infer"},
		{"infer from the tag being inferred", Config{Rules: inferFrom("env", TagSource{Tag: "env"})}, `rules.infer[0].from_tag[0]: tag "env" is the tag being inferred`},
		{"infer from tag mapping to an empty value", Config{Rules: inferFrom("environment", TagSource{Tag: "env", Values: map[string]string{"production": ""}})}, `rules.infer[0].from_tag[0]: values: "production" maps to ""`},
		{"inherit from a relation", Config{Rules: RulesConfig{Inherit: []InheritRule{{Resource: "aws_ebs_volume", From: "attached_instance", Tags: []string{"owner"}}}}}, ""},
		{"inherit from any relation", Config{Rules: RulesConfig{Inherit: []InheritRule{{Resource: "aws_ebs_*", Tags: []string{"owner"}}}}}, ""},
		{"inherit without resource", Config{Rules: RulesConfig{Inherit: []InheritRule{{Tags: []string{"owner"}}}}}, "rules.inherit[0]: resource is required"},
		{"inherit with malformed glob", Config{Rules: RulesConfig{Inherit: []InheritRule{{Resource: "aws_[", Tags: []string{"owner"}}}}}, `rules.inherit[0]: resource: invalid glob "aws_["`},
		{"inherit from unknown relation", Config{Rules: RulesConfig{Inherit: []InheritRule{{Resource: "aws_instance", From: "asg", Tags: []string{"owner"}}}}}, `rules.inherit[0]: from: unknown relation "asg" (supported: attached_instance, source_volume, vpc)`},
		{"inherit without tags", Config{Rules: RulesConfig{Inherit: []InheritRule{{Resource: "aws_ebs_volume"}}}}, "rules.inherit[0]: tags must name at least one tag"},
		{"inherit with empty tag name", Config{Rules: RulesConfig{Inherit: []InheritRule{{Resource: "aws_ebs_volume", Tags: []string{""}}}}}, "rules.inherit[0]: tags: empty tag name"},
		{"forbidden key", Config{Policy: PolicyConfig{Forbidden: []ForbiddenTag{{Name: "Env"}}}}, ""},
		{"forbidden values of a required tag", Config{Policy: PolicyConfig{Required: []TagRequirement{{Name: "environment"}}, Forbidden: []ForbiddenTag{{Name: "environment", Values: []string{"test"}}}}}, ""},
		{"forbidden tag without name", Config{Policy: PolicyConfig{Forbidden: []ForbiddenTag{{Pattern: "^tmp"}}}}, "policy.forbidden[0]: name is required"},
		{"forbidden tag listed twice", Config{Policy: PolicyConfig{Forbidden: []ForbiddenTag{{Name: "Env"}, {Name: "Env", Values: []string{"x"}}}}}, `policy.forbidden[1]: tag "Env" is already defined at policy.forbidden[0]`},
		{"forbidden tag with bad pattern", Config{Policy: PolicyConfig{Forbidden: []ForbiddenTag{{Name: "Env", Pattern: "[a-"}}}}, `policy.forbidden[0]: invalid pattern "[a-"`},
		{"forbidden tag with empty value", Config{Policy: PolicyConfig{Forbidden: []ForbiddenTag{{Name: "Env", Values: []string{""}}}}}, "policy.forbidden[0]: values: empty value"},
		{"required tag forbidden outright", Config{Policy: PolicyConfig{Required: []TagRequirement{{Name: "owner"}}, Forbidden: []ForbiddenTag{{Name: "owner"}}}}, `policy.forbidden[0]: tag "owner" is defined at policy.required[0]; forbid specific values or a pattern instead`},
		{"optional tag forbidden outright", Config{Policy: PolicyConfig{Optional: []TagRequirement{{Name: "team"}}, Forbidden: []ForbiddenTag{{Name: "team"}}}}, `policy.forbidden[0]: tag "team" is defined at policy.optional[0]`},
		{"rename", Config{Rules: RulesConfig{Rename: []RenameRule{{From: "Env", To: "environment"}, {Resource: "aws_s3_*", From: "env", To: "environment"}}}}, ""},
		{"rename without from", Config{Rules: RulesConfig{Rename: []RenameRule{{To: "environment"}}}}, "rules.rename[0]: from is required"},
		{"rename without to", Config{Rules: RulesConfig{Rename: []RenameRule{{From: "Env"}}}}, "rules.rename[0]: to is required"},
		{"rename to the same key", Config{Rules: RulesConfig{Rename: []RenameRule{{From: "Env", To: "Env"}}}}, `rules.rename[0]: from and to are both "Env"`},
		{"rename with malformed glob", Config{Rules: RulesConfig{Rename: []RenameRule{{Resource: "aws_[", From: "Env", To: "environment"}}}}, `rules.rename[0]: resource: invalid glob "aws_["`},
		{"rename chain", Config{Rules: RulesConfig{Rename: []RenameRule{{From: "Env", To: "env"}, {From: "env", To: "environment"}}}}, `rules.rename[0]: to: tag "env" is renamed again by rules.rename[1]`},
		{"rename to a forbidden key", Config{Policy: PolicyConfig{Forbidden: []ForbiddenTag{{Name: "Env"}}}, Rules: RulesConfig{Rename: []RenameRule{{From: "environment", To: "Env"}}}}, `rules.rename[0]: to: tag "Env" is forbidden by policy.forbidden`},
		{"rename from a forbidden key", Config{Policy: PolicyConfig{Forbidden: []ForbiddenTag{{Name: "Env"}}}, Rules: RulesConfig{Rename: []RenameRule{{From: "Env", To: "environment"}}}}, ""},
		{"tag keys differing only in case", Config{Policy: PolicyConfig{Required: []TagRequirement{{Name: "Owner"}, {Name: "owner"}}}}, ""},
		{"tag required twice", Config{Policy: PolicyConfig{Required: []TagRequirement{{Name: "owner"}, {Name: "owner"}}}}, `policy.required[1]: tag "owner" is already defined at policy.required[0]`},
		{"tag both required and optional", Config{Policy: PolicyConfig{Required: []TagRequirement{{Name: "owner"}}, Optional: []TagRequirement{{Name: "owner"}}}}, `policy.optional[0]: tag "owner" is already defined at policy.required[0]`},
		{"optional tag without name", Config{Policy: PolicyConfig{Optional: []TagRequirement{{}}}}, "policy.optional[0]: name is required"},
		{"optional tag with bad pattern", Config{Policy: PolicyConfig{Optional: []TagRequirement{{Name: "team", Pattern: "[a-"}}}}, "policy.optional[0]: invalid pattern"},
		{"default with absent condition", Config{Rules: awsDefault(map[string]string{"owner": "a"}, map[string]string{"tag:owner": "absent"})}, ""},
		{"default with exact value condition", Config{Rules: awsDefault(map[string]string{"backup": "daily"}, map[string]string{"tag:environment": "prod"})}, ""},
		{"default without resource", Config{Rules: RulesConfig{Defaults: []DefaultRule{{Set: map[string]string{"owner": "a"}}}}}, "rules.defaults[0]: resource is required"},
		{"default with malformed glob", Config{Rules: RulesConfig{Defaults: []DefaultRule{{Resource: "aws_[", Set: map[string]string{"owner": "a"}}}}}, `rules.defaults[0]: resource: invalid glob "aws_["`},
		{"default without set", Config{Rules: awsDefault(nil, nil)}, "rules.defaults[0]: set must name at least one tag"},
		{"condition without tag prefix", Config{Rules: awsDefault(map[string]string{"owner": "a"}, map[string]string{"owner": "absent"})}, `unknown condition "owner"`},
		{"condition without tag name", Config{Rules: awsDefault(map[string]string{"owner": "a"}, map[string]string{"tag:": "absent"})}, `unknown condition "tag:"`},
		{"condition present", Config{Rules: awsDefault(map[string]string{"owner": "a"}, map[string]string{"tag:owner": "present"})}, `tag:owner must be "absent" or the exact tag value, got "present"`},
		{"condition with empty value", Config{Rules: awsDefault(map[string]string{"owner": "a"}, map[string]string{"tag:owner": ""})}, `got ""`},
		{"ignore with middle wildcard", Config{Ignore: IgnoreConfig{Resources: []string{"aws_*_group"}}}, ""},
		{"ignore with malformed glob", Config{Ignore: IgnoreConfig{Resources: []string{"*", "aws_[iam"}}}, `ignore.resources[1]: invalid glob "aws_[iam"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
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
	body := "clouds:\n  aws:\n    - access_key_id: AKIA\n      secret_access_key: s\n      regions: [us-east-1]\n"
	_, err := Load(writeConfig(t, body))
	if err == nil || !strings.Contains(err.Error(), "access_key_id is not read from the config file") {
		t.Fatalf("Load() = %v, want the static-key rejection", err)
	}
}

func TestLoad_AssumeRoleFields(t *testing.T) {
	body := "clouds:\n  aws:\n    - profile: base\n      role_arn: arn:aws:iam::111111111111:role/Audit\n      external_id: ext\n      session_duration: 1800\n      role_session_name: audit\n      mfa_serial: arn:aws:iam::111111111111:mfa/pedro\n      regions: [us-east-1]\n"
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	acc := cfg.Clouds.AWS[0]
	if acc.Profile != "base" || acc.RoleARN != "arn:aws:iam::111111111111:role/Audit" || acc.ExternalID != "ext" ||
		acc.SessionDuration != 1800 || acc.RoleSessionName != "audit" || acc.MFASerial != "arn:aws:iam::111111111111:mfa/pedro" || len(acc.Unknown) != 0 {
		t.Errorf("account = %+v", acc)
	}
}
