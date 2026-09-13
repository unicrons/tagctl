package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/types"
)

func TestExitCode(t *testing.T) {
	gate := gateFailed(errors.New("compliance gate failed"))

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: exitOK},
		{name: "gate failure", err: gate, want: exitGateFailed},
		{name: "wrapped gate failure", err: fmt.Errorf("scan: %w", gate), want: exitGateFailed},
		{name: "any other error", err: errors.New("failed to load config"), want: exitError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// resetFlags restores every flag the root command tree parsed, since cobra
// keeps flag values between executions.
func resetFlags(cmd *cobra.Command) {
	reset := func(f *pflag.Flag) {
		if !f.Changed {
			return
		}
		if slice, ok := f.Value.(pflag.SliceValue); ok {
			_ = slice.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	cmd.PersistentFlags().VisitAll(reset)
	cmd.Flags().VisitAll(reset)
	for _, sub := range cmd.Commands() {
		resetFlags(sub)
	}
}

// executeExitCode runs tagctl with args and returns the code main exits with.
func executeExitCode(t *testing.T, args ...string) int {
	t.Helper()

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	originalStdout, originalDir := os.Stdout, OutputDir
	os.Stdout, OutputDir = devNull, t.TempDir()
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		os.Stdout, OutputDir = originalStdout, originalDir
		_ = devNull.Close()
		log.SetOutput(os.Stderr)
		viper.Reset()
		resetFlags(rootCmd)
		rootCmd.SetArgs(nil)
	})

	rootCmd.SetArgs(args)
	return ExitCode(rootCmd.Execute())
}

func TestExecute_ExitCodes(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: owner\n")
	invalid := writeFixture(t, dir, "invalid.yaml", "clouds:\n  aws:\n    - profile: default\n      regions: [eu-west1]\n")
	untagged := writeFixture(t, dir, "untagged.json", `[{"id": "i-1", "type": "aws_instance", "provider": "aws", "tags": {}}]`)
	drifted := writeFixture(t, dir, "drifted.json", `[
		{"id": "i-1", "type": "aws_instance", "provider": "aws", "tags": {"environment": "prod"}},
		{"id": "i-2", "type": "aws_instance", "provider": "aws", "tags": {"environment": "Prod"}}
	]`)
	tfPlan := writeFixture(t, dir, "plan.json", terraformPlanFixture)
	passing := failedScan(100)
	passing.Findings[0].Status, passing.Findings[0].Reason = types.StatusPass, types.ReasonCompliant
	baseline := writeScan(t, dir, "baseline.json", passing)
	regressed := writeScan(t, dir, "current.json", failedScan(0))
	missing := filepath.Join(dir, "missing.json")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "scan succeeds", args: []string{"-c", policy, "scan", "--mock"}, want: exitOK},
		{name: "scan below --fail-under", args: []string{"-c", policy, "scan", "--mock", "--fail-under", "100"}, want: exitGateFailed},
		{name: "scan regressed with --fail-on-new", args: []string{"-c", policy, "scan", "--mock", "--fail-on-new", "--baseline", baseline}, want: exitGateFailed},
		{name: "scan --fail-on-new without --baseline", args: []string{"-c", policy, "scan", "--mock", "--fail-on-new"}, want: exitError},
		{name: "scan unknown flag", args: []string{"-c", policy, "scan", "--no-such-flag"}, want: exitError},
		{name: "scan invalid config", args: []string{"-c", invalid, "scan", "--mock"}, want: exitError},
		{name: "evaluate below --fail-under", args: []string{"-c", policy, "evaluate", "--resources", untagged, "--policy", policy, "--fail-under", "100"}, want: exitGateFailed},
		{name: "evaluate unreadable resources", args: []string{"-c", policy, "evaluate", "--resources", missing, "--policy", policy}, want: exitError},
		{name: "terraform below --fail-under", args: []string{"-c", policy, "terraform", "--plan", tfPlan, "--fail-under", "100"}, want: exitGateFailed},
		{name: "diff with --fail-on-regression", args: []string{"-c", policy, "diff", baseline, regressed, "--fail-on-regression"}, want: exitGateFailed},
		{name: "diff too many arguments", args: []string{"-c", policy, "diff", baseline, regressed, regressed}, want: exitError},
		{name: "normalize with --fail-on-drift", args: []string{"-c", policy, "normalize", "--resources", drifted, "--fail-on-drift"}, want: exitGateFailed},
		{name: "validate invalid config", args: []string{"-c", invalid, "validate"}, want: exitError},
		{name: "apply unreadable plan", args: []string{"-c", policy, "apply", "--plan", missing, "--auto-approve"}, want: exitError},
		{name: "cost with an invalid window", args: []string{"-c", policy, "cost", "--days", "0"}, want: exitError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := executeExitCode(t, tt.args...); got != tt.want {
				t.Errorf("tagctl %v exited %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}
