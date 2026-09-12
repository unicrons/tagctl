package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// outputFormatFor resolves the -o value for a command that prints the given
// formats: case-insensitive when set, the first supported format otherwise.
func outputFormatFor(cmd *cobra.Command, supported ...string) (string, error) {
	if !cmd.Flags().Changed("output") {
		return supported[0], nil
	}

	value, _ := cmd.Flags().GetString("output")
	format := strings.ToLower(value)
	if !slices.Contains(supported, format) {
		return "", fmt.Errorf("unsupported output format %q for %s (supported: %s)", value, cmd.Name(), strings.Join(supported, ", "))
	}
	return format, nil
}
