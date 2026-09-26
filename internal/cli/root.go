// Package cli implements the command-line interface for tagctl.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/unicrons/tagctl/internal/log"
)

var (
	cfgFile      string
	outputFormat string
	logLevel     string
)

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

const banner = `
  ████████╗ █████╗  ██████╗  ██████╗████████╗██╗
  ╚══██╔══╝██╔══██╗██╔════╝ ██╔════╝╚══██╔══╝██║
     ██║   ███████║██║  ███╗██║        ██║   ██║
     ██║   ██╔══██║██║   ██║██║        ██║   ██║
     ██║   ██║  ██║╚██████╔╝╚██████╗   ██║   ███████╗
     ╚═╝   ╚═╝  ╚═╝ ╚═════╝  ╚═════╝   ╚═╝   ╚══════╝

        Audit, fix, and enforce cloud resource tags
`

var rootCmd = &cobra.Command{
	Use:   "tagctl",
	Short: "Audit, fix, and enforce cloud resource tags",
	Long: banner + `
It helps you:
  • Audit tag compliance across multiple cloud accounts
  • Generate smart fix plans using inference and inheritance
  • Apply tag changes safely with dry-run preview

Example workflow:
  $ tagctl scan          # See what's wrong
  $ tagctl plan          # Generate fix plan
  $ tagctl apply         # Apply the fixes

Exit codes:
  0  success
  1  a policy gate failed (--fail-under, --fail-on-new, --fail-on-regression, --fail-on-drift)
  2  any other error (bad flag, invalid config, unreadable input, partial scan, failed apply)`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default: ./tagctl.yaml)")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "output format (table, json, csv)")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "error", "log level (error, info, debug)")

	// Bind flags to viper
	// The flag is defined just above, so BindPFlag cannot fail here.
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))

	// Add subcommands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(evaluateCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(normalizeCmd)
	rootCmd.AddCommand(terraformCmd)
	rootCmd.AddCommand(costCmd)
}

func initConfig() {
	// Set log level
	if err := log.SetLevelFromString(logLevel); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v, using 'info'\n", err)
	}

	viper.SetConfigType("yaml")
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("tagctl")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.tagctl")
	}

	viper.SetEnvPrefix("TAGCTL")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Debug("reading config: %v", err)
	}
}
