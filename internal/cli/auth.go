package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/config"
)

// addAWSAuthFlags registers the AWS authentication flags shared by the
// commands that talk to AWS. They mirror the account fields in tagctl.yaml.
func addAWSAuthFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringP("profile", "p", "", "AWS profile from ~/.aws/config (default: SDK credential chain)")
	f.String("role", "", "IAM role ARN to assume")
	f.String("external-id", "", "external ID for --role")
	f.Int("session-duration", 0, "assumed-role session duration in seconds (default 3600)")
	f.String("role-session-name", "", "session name for --role (default tagctl)")
	f.String("mfa-serial", "", "MFA device ARN for --role; the code is read from stdin")
}

// readAWSAuthFlags returns the account fields set through flags.
func readAWSAuthFlags(cmd *cobra.Command) config.AWSAccount {
	f := cmd.Flags()
	profile, _ := f.GetString("profile")
	role, _ := f.GetString("role")
	externalID, _ := f.GetString("external-id")
	duration, _ := f.GetInt("session-duration")
	sessionName, _ := f.GetString("role-session-name")
	mfaSerial, _ := f.GetString("mfa-serial")
	return config.AWSAccount{
		Profile: profile, RoleARN: role, ExternalID: externalID,
		SessionDuration: duration, RoleSessionName: sessionName, MFASerial: mfaSerial,
	}
}

// applyAWSAuthFlags overrides the single configured AWS account with the
// flag values, creating it when the config has none.
func applyAWSAuthFlags(cfg *config.Config, flags config.AWSAccount) error {
	if flags.Profile == "" && flags.RoleARN == "" && flags.ExternalID == "" &&
		flags.SessionDuration == 0 && flags.RoleSessionName == "" && flags.MFASerial == "" {
		return nil
	}
	if len(cfg.Clouds.AWS) > 1 {
		return fmt.Errorf("--profile and --role apply to a single account; tagctl.yaml configures %d", len(cfg.Clouds.AWS))
	}
	if len(cfg.Clouds.AWS) == 0 {
		cfg.Clouds.AWS = []config.AWSAccount{{}}
	}
	acc := &cfg.Clouds.AWS[0]
	if flags.Profile != "" {
		acc.Profile = flags.Profile
	}
	if flags.RoleARN != "" {
		acc.RoleARN = flags.RoleARN
	}
	if flags.ExternalID != "" {
		acc.ExternalID = flags.ExternalID
	}
	if flags.SessionDuration != 0 {
		acc.SessionDuration = flags.SessionDuration
	}
	if flags.RoleSessionName != "" {
		acc.RoleSessionName = flags.RoleSessionName
	}
	if flags.MFASerial != "" {
		acc.MFASerial = flags.MFASerial
	}
	return cfg.Validate()
}
