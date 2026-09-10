package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/config"
)

func authFlagsFrom(t *testing.T, args ...string) config.AWSAccount {
	t.Helper()
	cmd := &cobra.Command{}
	addAWSAuthFlags(cmd)
	if err := cmd.Flags().Parse(args); err != nil {
		t.Fatal(err)
	}
	return readAWSAuthFlags(cmd)
}

func TestApplyAWSAuthFlags_NoFlagsKeepsConfig(t *testing.T) {
	cfg := &config.Config{Clouds: config.CloudsConfig{AWS: []config.AWSAccount{{Profile: "a"}, {Profile: "b"}}}}
	if err := applyAWSAuthFlags(cfg, authFlagsFrom(t)); err != nil {
		t.Fatalf("applyAWSAuthFlags() = %v", err)
	}
	if len(cfg.Clouds.AWS) != 2 {
		t.Errorf("accounts = %+v, want the two configured ones untouched", cfg.Clouds.AWS)
	}
}

func TestApplyAWSAuthFlags_CreatesAccountWhenConfigHasNone(t *testing.T) {
	cfg := &config.Config{}
	if err := applyAWSAuthFlags(cfg, authFlagsFrom(t, "-p", "dev")); err != nil {
		t.Fatalf("applyAWSAuthFlags() = %v", err)
	}
	if len(cfg.Clouds.AWS) != 1 || cfg.Clouds.AWS[0].Profile != "dev" {
		t.Errorf("accounts = %+v", cfg.Clouds.AWS)
	}
}

func TestApplyAWSAuthFlags_OverridesSingleAccount(t *testing.T) {
	cfg := &config.Config{Clouds: config.CloudsConfig{AWS: []config.AWSAccount{{Profile: "from-config", Regions: []string{"eu-west-1"}}}}}
	flags := authFlagsFrom(t,
		"--profile", "base", "--role", "arn:aws:iam::111111111111:role/Audit", "--external-id", "ext",
		"--session-duration", "1800", "--role-session-name", "audit", "--mfa-serial", "arn:aws:iam::111111111111:mfa/pedro")
	if err := applyAWSAuthFlags(cfg, flags); err != nil {
		t.Fatalf("applyAWSAuthFlags() = %v", err)
	}
	acc := cfg.Clouds.AWS[0]
	if acc.Profile != "base" || acc.RoleARN != "arn:aws:iam::111111111111:role/Audit" || acc.ExternalID != "ext" ||
		acc.SessionDuration != 1800 || acc.RoleSessionName != "audit" || acc.MFASerial != "arn:aws:iam::111111111111:mfa/pedro" {
		t.Errorf("account = %+v", acc)
	}
	if len(acc.Regions) != 1 || acc.Regions[0] != "eu-west-1" {
		t.Errorf("regions = %v, want the configured ones kept", acc.Regions)
	}
}

func TestApplyAWSAuthFlags_RejectsSeveralAccounts(t *testing.T) {
	cfg := &config.Config{Clouds: config.CloudsConfig{AWS: []config.AWSAccount{{Profile: "a"}, {Profile: "b"}}}}
	err := applyAWSAuthFlags(cfg, authFlagsFrom(t, "--profile", "c"))
	if err == nil || !strings.Contains(err.Error(), "single account") {
		t.Fatalf("applyAWSAuthFlags() = %v, want the multi-account rejection", err)
	}
}

func TestApplyAWSAuthFlags_ValidatesResult(t *testing.T) {
	err := applyAWSAuthFlags(&config.Config{}, authFlagsFrom(t, "--external-id", "ext"))
	if err == nil || !strings.Contains(err.Error(), "external_id requires role_arn") {
		t.Fatalf("applyAWSAuthFlags() = %v, want the validation error", err)
	}
}
