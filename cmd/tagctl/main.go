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
	// Without this a failing command exits 1 saying nothing, which is useless
	// in a pipeline.
	if err := cli.Execute(version, buildTime); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
