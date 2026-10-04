package cli

import (
	"embed"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

//go:embed templates/*.yaml
var configTemplateFS embed.FS

const defaultTemplate = "default"

// configTemplate is a starter tagctl.yaml embedded as templates/<name>.yaml.
type configTemplate struct {
	name        string
	description string
}

var configTemplates = []configTemplate{
	{defaultTemplate, "General starter: environment, cost-center and owner"},
	{"finops", "Cost allocation: cost-center, owner, environment and project"},
	{"security", "Security triage: data-classification, owner, environment and compliance"},
	{"well-architected", "AWS tagging best practices: technical, business and security tags"},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new tagctl configuration file",
	Long: `Initialize creates a new tagctl.yaml configuration file in the
current directory from a built-in template.

Examples:
  # Create default config
  tagctl init

  # Create config with specific name
  tagctl init --name production.yaml

  # Start from a policy template
  tagctl init --template finops

  # Show the available templates
  tagctl init --list-templates`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().String("name", "tagctl.yaml", "name of the config file to create")
	initCmd.Flags().String("template", defaultTemplate, "policy template to start from: "+strings.Join(templateNames(), ", "))
	initCmd.Flags().Bool("list-templates", false, "list the available templates and exit")
}

func templateNames() []string {
	names := make([]string, len(configTemplates))
	for i, t := range configTemplates {
		names[i] = t.name
	}
	return names
}

// readTemplate returns the embedded config for a template name.
func readTemplate(name string) ([]byte, error) {
	for _, t := range configTemplates {
		if t.name != name {
			continue
		}
		data, err := configTemplateFS.ReadFile("templates/" + name + ".yaml")
		if err != nil {
			return nil, fmt.Errorf("failed to read template %s: %w", name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("unknown template %q (available: %s)", name, strings.Join(templateNames(), ", "))
}

func listTemplates() error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range configTemplates {
		fmt.Fprintf(w, "%s\t%s\n", t.name, t.description)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("failed to list templates: %w", err)
	}
	return nil
}

func runInit(cmd *cobra.Command, args []string) error {
	configName, _ := cmd.Flags().GetString("name")
	templateName, _ := cmd.Flags().GetString("template")
	list, _ := cmd.Flags().GetBool("list-templates")

	if _, err := outputFormatFor(cmd, formatTable); err != nil {
		return err
	}

	if list {
		return listTemplates()
	}

	content, err := readTemplate(templateName)
	if err != nil {
		return err
	}

	printBanner()

	if _, err := os.Stat(configName); err == nil {
		return fmt.Errorf("config file %s already exists", configName)
	}

	if err := os.WriteFile(configName, content, 0o600); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	c := paletteFor(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s✓ Created %s from the %s template%s\n\n", c.green, configName, templateName, c.reset)
	fmt.Fprintf(os.Stderr, "%sNext steps:%s\n", c.bold, c.reset)
	fmt.Fprintf(os.Stderr, "  1. Edit %s%s%s to add your cloud accounts\n", c.cyan, configName, c.reset)
	fmt.Fprintf(os.Stderr, "  2. Adjust the required tags in the policy section\n")
	fmt.Fprintf(os.Stderr, "  3. Run '%stagctl validate%s', then '%stagctl scan%s' to check compliance\n",
		c.green, c.reset, c.green, c.reset)

	return nil
}
