package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mainArgsEnv makes the test binary run main with these arguments, so the
// os.Exit path can be observed from the parent test.
const mainArgsEnv = "TAGCTL_TEST_MAIN_ARGS"

func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(mainArgsEnv); ok {
		os.Args = append([]string{"tagctl"}, strings.Fields(args)...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runMain(t *testing.T, args string) (code int, stderr string) {
	t.Helper()

	// #nosec G204 -- re-runs this test binary.
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), mainArgsEnv+"="+args)
	cmd.Dir = t.TempDir()
	var errOut strings.Builder
	cmd.Stderr = &errOut

	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run main: %v", err)
	}
	if exitErr != nil {
		return exitErr.ExitCode(), errOut.String()
	}
	return 0, errOut.String()
}

func TestMain_ReturnsWithoutExitingOnSuccess(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	originalArgs, originalStdout := os.Args, os.Stdout
	os.Args, os.Stdout = []string{"tagctl", "version"}, devNull
	t.Cleanup(func() {
		os.Args, os.Stdout = originalArgs, originalStdout
		_ = devNull.Close()
	})

	main()
}

func TestMain_ExitsZeroOnSuccess(t *testing.T) {
	code, stderr := runMain(t, "version")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if strings.Contains(stderr, "Error:") {
		t.Errorf("stderr reports an error on success: %s", stderr)
	}
}

func TestMain_ExitsTwoAndPrintsTheErrorOnAUsageError(t *testing.T) {
	code, stderr := runMain(t, "no-such-command")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Error:") || !strings.Contains(stderr, "no-such-command") {
		t.Errorf("stderr = %q, want the error naming the unknown command", stderr)
	}
}

func TestMain_StripsControlCharactersFromTheError(t *testing.T) {
	code, stderr := runMain(t, "apply --output-dir no-plans\x1b]0;owned\x07")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Error: output directory no-plans?]0;owned? not found") {
		t.Errorf("stderr = %q, want the error with its control characters replaced", stderr)
	}
	if strings.ContainsAny(stderr, "\x1b\x07") {
		t.Errorf("stderr carries a control character from the error: %q", stderr)
	}
}
