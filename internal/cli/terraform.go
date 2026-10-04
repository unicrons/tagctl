package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/terraform"
	"github.com/unicrons/tagctl/internal/types"
)

var terraformCmd = &cobra.Command{
	Use:   "terraform",
	Short: "Check a Terraform plan or state against the tag policy",
	Long: `Terraform evaluates the tags in a Terraform plan or state file against the
same policy the live scan uses, before anything is created.

Cost allocation tags are not retroactive: a resource that runs untagged for two
weeks has two weeks of spend that can never be attributed to anyone. Catching a
missing tag in a pull request costs nothing; catching it after a quarter of
unattributable spend costs a quarter.

Tags are read from tags_all when Terraform provides it, so the provider's
default_tags count; when tags_all is null or only known after apply, tags is
read instead. A tag whose value is only known after apply counts as present
and its value is not checked; a resource whose tags are only known after apply
is skipped. Both are reported on stderr. Data sources and resource types that
carry no tag attribute are skipped, since neither can be tagged. Only
format_version 1.x is read.

Examples:
  # Check a plan
  terraform plan -out=tfplan
  terraform show -json tfplan | tagctl terraform

  # Or from a file
  terraform show -json tfplan > plan.json
  tagctl terraform --plan plan.json

  # Only what this change creates or updates
  tagctl terraform --plan plan.json --changed-only

  # Check what is already deployed
  terraform show -json > state.json
  tagctl terraform --state state.json

  # Fail the pull request, and annotate it
  tagctl terraform --plan plan.json --changed-only --fail-under 100 --sarif tf.sarif`,
	RunE: runTerraform,
}

func init() {
	terraformCmd.Flags().String("plan", "-", "terraform show -json output for a plan (- for stdin)")
	terraformCmd.Flags().String("state", "", "terraform show -json output for a state file")
	terraformCmd.Flags().Bool("changed-only", false, "only evaluate resources the plan creates or updates")
	addGateFlags(terraformCmd)
}

func runTerraform(cmd *cobra.Command, args []string) error {
	planPath, _ := cmd.Flags().GetString("plan")
	statePath, _ := cmd.Flags().GetString("state")
	changedOnly, _ := cmd.Flags().GetBool("changed-only")
	gateOpts := readGateFlags(cmd)
	format, err := gateOpts.stdoutFormat(cmd, formatTable, formatJSON)
	if err != nil {
		return err
	}

	if statePath != "" && cmd.Flags().Changed("plan") {
		return fmt.Errorf("use --plan or --state, not both")
	}

	source := planPath
	if statePath != "" {
		source = statePath
		if changedOnly {
			return fmt.Errorf("--changed-only needs a plan: a state file records no pending changes")
		}
	}

	parsed, err := readTerraformResources(source, terraform.Options{ChangedOnly: changedOnly})
	if err != nil {
		return err
	}
	logUncheckedTags(parsed)
	resources := parsed.Resources

	if len(resources) == 0 {
		fmt.Fprintln(os.Stderr, "No taggable resources found in the Terraform input.")
		if len(parsed.Unreadable) > 0 {
			fmt.Fprintf(os.Stderr, "%d resources were skipped because their tags are only known after apply.\n", len(parsed.Unreadable))
		}
		if changedOnly {
			fmt.Fprintln(os.Stderr, "With --changed-only, only resources being created or updated are checked.")
		}
		empty := types.NewScanResult()
		if format == formatJSON {
			if err = printJSON(empty); err != nil {
				return err
			}
		}
		return gateOpts.writeReports(empty, viper.ConfigFileUsed())
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}

	evaluator, err := engine.NewEvaluator(cfg.Policy)
	if err != nil {
		return fmt.Errorf("failed to create evaluator: %w", err)
	}

	result := evaluator.EvaluateResources(resources)

	switch format {
	case formatJSON:
		if err := printJSON(result); err != nil {
			return err
		}
	case formatTable:
		printTerraformResult(result, source, changedOnly)
	}

	if reportErr := gateOpts.writeReports(result, viper.ConfigFileUsed()); reportErr != nil {
		return reportErr
	}

	return gateOpts.check(result)
}

// readTerraformResources reads terraform JSON from a path or stdin.
func readTerraformResources(path string, opts terraform.Options) (terraform.Result, error) {
	var reader io.Reader

	if path == "-" || path == "" {
		stat, err := os.Stdin.Stat()
		if err != nil {
			return terraform.Result{}, fmt.Errorf("failed to read stdin: %w", err)
		}
		if stat.Mode()&os.ModeCharDevice != 0 {
			return terraform.Result{}, fmt.Errorf("no input on stdin. Pipe 'terraform show -json <plan>' in, or pass --plan/--state")
		}
		reader = os.Stdin
	} else {
		// #nosec G304 -- the terraform file is supplied by the user running the CLI.
		file, err := os.Open(filepath.Clean(path))
		if err != nil {
			return terraform.Result{}, fmt.Errorf("failed to open %s: %w", path, err)
		}
		defer func() { _ = file.Close() }()
		reader = file
	}

	return terraform.Parse(reader, opts)
}

// logUncheckedTags names the resources and tag values the policy cannot check
// until apply.
func logUncheckedTags(parsed terraform.Result) {
	for _, address := range parsed.Unreadable {
		log.Error("%s: tags are only known after apply, skipping it", address)
	}
	for _, resource := range parsed.Resources {
		if len(resource.UnknownTags) > 0 {
			log.Error("%s: values of %s are only known after apply, not checked",
				resource.ID, strings.Join(resource.UnknownTags, ", "))
		}
	}
}

func printTerraformResult(result *types.ScanResult, source string, changedOnly bool) {
	scope := "all resources"
	if changedOnly {
		scope = "resources being created or updated"
	}

	fmt.Printf("Source: %s (%s)\n", printable(source), scope)
	fmt.Printf("Checked %d resources against the tag policy\n\n", result.TotalResources)

	failures := result.FailedFindings()

	if len(failures) == 0 {
		fmt.Printf("All %d resources satisfy the policy.\n", result.TotalResources)
		return
	}

	fmt.Printf("%d of %d resources would be non-compliant (%.1f%% compliant):\n\n",
		result.TotalResources-result.CompliantCount, result.TotalResources, result.CompliancePct)

	// Group by terraform address, which is what the author has to go and edit.
	byAddress := make(map[string][]types.Finding)
	addresses := make([]string, 0, len(failures))
	for _, finding := range failures {
		if _, seen := byAddress[finding.Resource.ID]; !seen {
			addresses = append(addresses, finding.Resource.ID)
		}
		byAddress[finding.Resource.ID] = append(byAddress[finding.Resource.ID], finding)
	}
	sort.Strings(addresses)

	for _, address := range addresses {
		fmt.Printf("  %s\n", printable(address))
		for _, finding := range byAddress[address] {
			fmt.Printf("      ✗ %s\n", printable(finding.Message()))
		}
	}

	fmt.Println()
	fmt.Println("Fix the tags in your Terraform, or set them with the provider's default_tags.")
}
