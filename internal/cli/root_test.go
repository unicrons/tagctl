package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/log"
)

func captureRootOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	return buf
}

func TestExecute_Version(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})

	Version, Commit, Date = "1.0.0", "abc1234", "2024-01-01"
	t.Cleanup(func() { Version, Commit, Date = "dev", "none", "unknown" })

	buf := captureRootOutput(t)

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
	buf := captureRootOutput(t)
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

func TestExecute_InvalidLogLevelIsAUsageError(t *testing.T) {
	run := execute(t, "--log-level", "verbose", "version")
	if ExitCode(run.err) != exitError {
		t.Fatalf("exit code = %d (err %v), want %d", ExitCode(run.err), run.err, exitError)
	}
	for _, want := range []string{"--log-level", `"verbose"`, "error, info, debug"} {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("error %q does not mention %s", run.err, want)
		}
	}
	if run.stdout != "" {
		t.Errorf("command ran despite the invalid level: %q", run.stdout)
	}
}

func TestExecute_LogLevelIsCaseInsensitive(t *testing.T) {
	t.Cleanup(func() { log.SetLevel(log.LevelError) })
	if run := execute(t, "--log-level", "DEBUG", "version"); run.err != nil {
		t.Fatalf("err = %v", run.err)
	}
}
