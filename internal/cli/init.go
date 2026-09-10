package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new tagctl configuration file",
	Long: `Initialize creates a new tagctl.yaml configuration file in the
current directory with example settings.

Examples:
  # Create default config
  tagctl init

  # Create config with specific name
  tagctl init --name production.yaml`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().String("name", "tagctl.yaml", "name of the config file to create")
}

func runInit(cmd *cobra.Command, args []string) error {
	configName, _ := cmd.Flags().GetString("name")

	// Print banner
	printBanner()

	// Check if file already exists
	if _, err := os.Stat(configName); err == nil {
		return fmt.Errorf("config file %s already exists", configName)
	}

	// Create the config file
	if err := os.WriteFile(configName, []byte(defaultConfig), 0o600); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	fmt.Printf("%s✓ Created %s%s\n", colorGreen, configName, colorReset)
	fmt.Println()
	fmt.Printf("%sNext steps:%s\n", colorBold, colorReset)
	fmt.Printf("  1. Edit %s%s%s to add your cloud accounts\n", colorCyan, configName, colorReset)
	fmt.Printf("  2. Define your required tags in the policy section\n")
	fmt.Printf("  3. Run '%stagctl scan%s' to check compliance\n", colorGreen, colorReset)

	return nil
}

const defaultConfig = `# tagctl configuration
# See https://github.com/unicrons/tagctl for documentation

# Cloud accounts to scan
clouds:
  aws:
    # Credentials come from the AWS SDK chain: environment variables, then
    # ~/.aws/config and ~/.aws/credentials (profiles, SSO), then an instance
    # or container role. Never write keys in this file.
    - profile: default
      # regions: [us-east-1, eu-west-1]  # Optional: if omitted, all regions are scanned

    # Omit profile to use whatever the chain resolves (AWS_PROFILE, exported
    # keys, an instance role).
    # - regions: [us-east-1]

    # Assume a role on top of the resolved credentials, one entry per account.
    # - profile: audit-base
    #   role_arn: arn:aws:iam::123456789012:role/TagctlScan   # permissions/aws/ ships the role templates
    #   external_id: my-external-id     # Optional
    #   session_duration: 3600          # Optional, 900-43200 seconds
    #   role_session_name: tagctl       # Optional
    #   mfa_serial: arn:aws:iam::111111111111:mfa/me  # Optional, code read from stdin

  # GCP (coming soon)
  # gcp:
  #   - project: my-project
  #     credentials_file: ~/.config/gcloud/application_default_credentials.json

  # Azure (coming soon)
  # azure:
  #   - subscription: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

# Tag policy definition
policy:
  # Required tags - resources without these are non-compliant
  required:
    - name: environment
      description: Deployment environment
      values:
        - dev
        - staging
        - prod

    - name: cost-center
      description: Finance cost allocation code
      pattern: "^[A-Z]{2,4}-\\d{3,6}$"

    - name: owner
      description: Team or person responsible
      pattern: "^.+@.+$"

  # Optional tags - tracked but not required
  optional:
    - name: project
    - name: team

# Auto-fix rules
rules:
  # Infer tags from resource naming conventions
  infer:
    - tag: environment
      from_name:
        - pattern: "-prod-"
          value: prod
        - pattern: "-staging-|-stg-"
          value: staging
        - pattern: "-dev-"
          value: dev

  # Inherit tags from parent resources (parsed, not applied by plan yet)
  # inherit:
  #   - resource: aws_ebs_volume
  #     from: attached_instance
  #     tags: [environment, cost-center, owner]

  # Default values for untagged resources
  defaults:
    - resource: "*"
      when:
        tag:owner: absent
      set:
        owner: platform-team@company.com
        needs-review: "true"

# Resources to ignore
ignore:
  # Skip these resource types (exact, prefix* or *suffix)
  resources:
    - "aws_cloudwatch_*"
    - "aws_iam_*"

  # Skip resources with these tags
  tags:
    managed-by: [terraform, pulumi]
`
