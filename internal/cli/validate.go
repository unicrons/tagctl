package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/unicrons/tagctl/internal/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the configuration file",
	Long: `Validate checks the tagctl configuration file for errors
and prints a summary of the configuration.

Examples:
  # Validate default config
  tagctl validate

  # Validate specific config file
  tagctl validate --config production.yaml`,
	RunE: runValidate,
}

func runValidate(cmd *cobra.Command, args []string) error {
	if _, err := outputFormatFor(cmd, formatTable); err != nil {
		return err
	}

	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		return fmt.Errorf("no config file found. Run 'tagctl init' to create one")
	}

	fmt.Printf("Validating %s...\n", configFile)
	fmt.Println()

	report := &validationReport{}
	report.checkClouds()
	report.checkAWSShape()
	report.checkPolicy()
	report.checkRules()
	report.checkIgnore()
	report.checkParsed(configFile)

	return report.print()
}

// validationReport collects the problems found while checking a config file.
// Errors make the config unusable; warnings are worth reporting but not fatal.
type validationReport struct {
	errors   []string
	warnings []string
}

func (r *validationReport) addError(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *validationReport) addWarning(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

// checkClouds verifies that at least one known cloud provider is configured.
func (r *validationReport) checkClouds() {
	clouds := viper.GetStringMap("clouds")
	if len(clouds) == 0 {
		r.addError("no cloud accounts configured")
		return
	}

	cloudCount := 0
	for provider, cfg := range clouds {
		if cfg == nil {
			r.addWarning("cloud provider '%s' has no accounts configured", provider)
			continue
		}
		switch provider {
		case providerAWS, "gcp", "azure", "kubernetes":
			cloudCount++
		default:
			r.addWarning("unknown cloud provider: %s", provider)
		}
	}

	if cloudCount > 0 {
		fmt.Printf("✓ Cloud providers configured: %d\n", cloudCount)
	}
}

// checkAWSShape warns when clouds.aws is written as a single mapping instead
// of a list of accounts. The decoder silently lifts the mapping into a
// one-element list, so it works by accident until a second account is added.
func (r *validationReport) checkAWSShape() {
	if _, isMap := viper.Get("clouds.aws").(map[string]interface{}); isMap {
		r.addWarning("clouds.aws should be a list of accounts: write '- regions:' (or '- profile:') instead of 'regions:'")
	}
}

// checkParsed loads the file with config.Load, which is what every command
// does before using the configuration.
func (r *validationReport) checkParsed(path string) {
	if _, err := config.Load(path); err != nil {
		r.addError("%v", err)
	}
}

// checkPolicy verifies that a tag policy is present and reports its size.
func (r *validationReport) checkPolicy() {
	if len(viper.GetStringMap("policy")) == 0 {
		r.addError("no tag policy defined")
		return
	}

	if viper.Get("policy.required") == nil {
		r.addWarning("no required tags defined")
	} else if n := countConfigList("policy.required"); n > 0 {
		fmt.Printf("✓ Required tags defined: %d\n", n)
	}

	if n := countConfigList("policy.optional"); n > 0 {
		fmt.Printf("✓ Optional tags defined: %d\n", n)
	}
}

// checkRules reports how many auto-fix rules are configured.
func (r *validationReport) checkRules() {
	if len(viper.GetStringMap("rules")) == 0 {
		return
	}

	ruleCount := countConfigList("rules.infer") +
		countConfigList("rules.inherit") +
		countConfigList("rules.defaults")

	if ruleCount > 0 {
		fmt.Printf("✓ Auto-fix rules defined: %d\n", ruleCount)
	}
}

// checkIgnore reports how many ignore rules are configured.
func (r *validationReport) checkIgnore() {
	if len(viper.GetStringMap("ignore")) == 0 {
		return
	}

	if n := countConfigList("ignore.resources"); n > 0 {
		fmt.Printf("✓ Ignore rules defined: %d\n", n)
	}
}

// print writes the collected warnings and errors, returning an error when the
// configuration cannot be used.
func (r *validationReport) print() error {
	fmt.Println()

	if len(r.warnings) > 0 {
		fmt.Println("Warnings:")
		for _, w := range r.warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
		fmt.Println()
	}

	if len(r.errors) > 0 {
		fmt.Println("Errors:")
		for _, e := range r.errors {
			fmt.Printf("  ✗ %s\n", e)
		}
		return fmt.Errorf("configuration has %d error(s)", len(r.errors))
	}

	fmt.Println("Configuration is valid!")
	return nil
}

// countConfigList returns the length of a config value that is expected to be
// a list, and 0 when it is absent or not a list.
func countConfigList(key string) int {
	value := viper.Get(key)
	if value == nil {
		return 0
	}
	if list, ok := value.([]interface{}); ok {
		return len(list)
	}
	return 0
}
