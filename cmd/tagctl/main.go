// Package main is the entry point for the tagctl CLI.
package main

import (
	"fmt"
	"os"

	"github.com/unicrons/tagctl/internal/cli"
	"github.com/unicrons/tagctl/internal/log"
)

func main() {
	// The root command sets SilenceErrors, so the message is printed here.
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", log.Printable(err.Error()))
		os.Exit(cli.ExitCode(err))
	}
}
