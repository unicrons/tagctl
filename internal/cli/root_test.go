package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestExecute_Version(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})

	Version, Commit, Date = "1.0.0", "abc1234", "2024-01-01"
	t.Cleanup(func() { Version, Commit, Date = "dev", "none", "unknown" })

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, want := range []string{"1.0.0", "abc1234", "2024-01-01"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("version output should contain %q, got %q", want, buf.String())
		}
	}
}

func TestRootCmd_Help(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Check that help contains expected content
	expectedStrings := []string{
		"tagctl",
		"scan",
		"plan",
		"apply",
		"--config",
		"--output",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("help output should contain %q", expected)
		}
	}
}

func TestRootCmd_SubCommands(t *testing.T) {
	// Verify all expected subcommands are registered
	expectedCommands := []string{
		"version",
		"scan",
		"plan",
		"apply",
		"init",
		"validate",
	}

	commands := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		commands[cmd.Name()] = true
	}

	for _, expected := range expectedCommands {
		if !commands[expected] {
			t.Errorf("expected subcommand %q not found", expected)
		}
	}
}
