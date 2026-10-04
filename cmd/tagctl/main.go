// Package main is the entry point for the tagctl CLI.
package main

import (
	"fmt"
	"os"

	"github.com/unicrons/tagctl/internal/cli"
	"github.com/unicrons/tagctl/internal/log"
)

// Build-time variables set by ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// The root command sets SilenceErrors, so the message is printed here.
	if err := cli.Execute(version, buildTime); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", log.Printable(err.Error()))
		os.Exit(cli.ExitCode(err))
	}
}
