// Package main is the entry point for the tagctl CLI.
package main

import (
	"fmt"
	"os"

	"github.com/unicrons/tagctl/internal/cli"
)

// Build-time variables set by ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// The root command sets SilenceErrors, so the message is printed here.
	if err := cli.Execute(version, buildTime); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(cli.ExitCode(err))
	}
}
