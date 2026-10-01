// Package cli implements the command-line interface for tagctl.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/unicrons/tagctl/internal/log"
)

var (
	cfgFile      string
	logLevel     string
	appVersion   string
	appBuildTime string

	// logLevelErr is the --log-level error initConfig cannot return itself.
	logLevelErr error
)

// Execute runs the root command.
func Execute(version, buildTime string) error {
	appVersion = version
	appBuildTime = buildTime

	// Enable --version flag with custom template
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(banner + `
  Version: {{.Version}}
  Built:   ` + buildTime + `

`)
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
	PersistentPreRunE: func(*cobra.Command, []string) error {
		return logLevelErr
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default: ./tagctl.yaml)")
	rootCmd.PersistentFlags().StringP("output", "o", "table", "output format on stdout: table, json or csv, as the command supports")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "error", "log level (error, info, debug)")

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
	if logLevelErr = log.SetLevelFromString(logLevel); logLevelErr != nil {
		logLevelErr = fmt.Errorf("--log-level: %w", logLevelErr)
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

func printVersion() {
	fmt.Print(banner)
	fmt.Printf("  Version: %s\n", appVersion)
	fmt.Printf("  Built:   %s\n\n", appBuildTime)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := outputFormatFor(cmd, formatTable); err != nil {
			return err
		}
		printVersion()
		return nil
	},
}
