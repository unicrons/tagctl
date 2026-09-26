package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata, set at link time with -ldflags -X.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

const versionTemplate = banner + `
  Version: %s
  Commit:  %s
  Built:   %s

`

func versionText() string {
	return fmt.Sprintf(versionTemplate, Version, Commit, Date)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), versionText())
		return err
	},
}

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(versionText())
}
