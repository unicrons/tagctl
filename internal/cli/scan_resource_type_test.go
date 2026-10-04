package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

const awsPolicy = "clouds:\n  aws:\n    - profile: default\n      regions: [us-east-1]\n" + demoPolicy

func stubScanProviders(t *testing.T, resources ...types.Resource) {
	t.Helper()
	original := scanProviders
	scanProviders = func(context.Context, *config.Config, []string) ([]provider.Provider, error) {
		return []provider.Provider{stubProvider{resources: resources}}, nil
	}
	t.Cleanup(func() { scanProviders = original })
}

func stubResource(id, resourceType string) types.Resource {
	return types.Resource{ID: id, Type: resourceType, Provider: providerAWS, Account: "123456789012", Region: "us-east-1"}
}

func TestScan_ResourceTypeRestrictsTheScan(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", awsPolicy)
	resources := []types.Resource{
		stubResource("i-1", "aws_instance"),
		stubResource("b-1", "aws_s3_bucket"),
		stubResource("ap-1", "aws_s3_access_point"),
		stubResource("q-1", "aws_sqs_queue"),
	}

	tests := []struct {
		name        string
		args        []string
		want        int
		wantWarning bool
	}{
		{name: "no flag scans every type", want: 4},
		{name: "one glob", args: []string{"--resource-type", "aws_s3_*"}, want: 2},
		{name: "repeated flag", args: []string{"--resource-type", "aws_s3_*", "--resource-type", "aws_instance"}, want: 3},
		{name: "a comma is part of the glob", args: []string{"--resource-type", "aws_s3_*,aws_instance"}, want: 0, wantWarning: true},
		{name: "no match warns", args: []string{"--resource-type", "aws_rds_*"}, want: 0, wantWarning: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubScanProviders(t, resources...)

			run := execute(t, append([]string{"-c", policy, "scan", "-o", "json"}, tt.args...)...)
			if run.err != nil {
				t.Fatalf("scan error = %v", run.err)
			}

			var result types.ScanResult
			if err := json.Unmarshal([]byte(run.stdout), &result); err != nil {
				t.Fatalf("stdout is not a scan: %v\n%s", err, run.stdout)
			}
			if result.TotalResources != tt.want || len(result.Findings) != tt.want {
				t.Errorf("total = %d, findings = %d, want %d of each", result.TotalResources, len(result.Findings), tt.want)
			}
			if warned := strings.Contains(run.stderr, "no discovered resource matches --resource-type"); warned != tt.wantWarning {
				t.Errorf("no-match warning on stderr = %v, want %v:\n%s", warned, tt.wantWarning, run.stderr)
			}
		})
	}
}

func TestScan_ResourceTypeRejectsBadGlobsBeforeScanning(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", awsPolicy)

	for _, glob := range []string{"aws_[", ""} {
		t.Run(glob, func(t *testing.T) {
			original := scanProviders
			scanProviders = func(context.Context, *config.Config, []string) ([]provider.Provider, error) {
				t.Error("providers were initialised for an invalid --resource-type")
				return nil, nil
			}
			t.Cleanup(func() { scanProviders = original })

			run := execute(t, "-c", policy, "scan", "--resource-type", glob)

			if run.err == nil || !strings.Contains(run.err.Error(), "--resource-type") {
				t.Fatalf("scan error = %v, want one naming --resource-type", run.err)
			}
			if code := ExitCode(run.err); code != exitError {
				t.Errorf("exit code = %d, want %d", code, exitError)
			}
			if run.stdout != "" || len(fileNames(t, OutputDir)) != 0 {
				t.Errorf("scan produced output before rejecting the flag:\n%s", run.stdout)
			}
		})
	}
}

func TestScan_ResourceTypeIsIgnoredInDemoMode(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)

	run := execute(t, "-c", policy, "scan", "--mock", "--resource-type", "aws_s3_*")

	if run.err != nil {
		t.Fatalf("scan error = %v", run.err)
	}
	if !strings.Contains(run.stderr, "--resource-type is ignored in demo mode") {
		t.Errorf("stderr does not say the filter was ignored:\n%s", run.stderr)
	}
}
