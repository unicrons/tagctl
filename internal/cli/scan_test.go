package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		width   int
		want    string
	}{
		{
			name:    "0 percent",
			percent: 0,
			width:   10,
			want:    "░░░░░░░░░░",
		},
		{
			name:    "50 percent",
			percent: 50,
			width:   10,
			want:    "█████░░░░░",
		},
		{
			name:    "100 percent",
			percent: 100,
			width:   10,
			want:    "██████████",
		},
		{
			name:    "25 percent",
			percent: 25,
			width:   20,
			want:    "█████░░░░░░░░░░░░░░░",
		},
		{
			name:    "over 100 percent capped",
			percent: 150,
			width:   10,
			want:    "██████████",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderProgressBar(tt.percent, tt.width)
			if got != tt.want {
				t.Errorf("renderProgressBar(%v, %v) = %v, want %v", tt.percent, tt.width, got, tt.want)
			}
		})
	}
}

func TestMockScanner(t *testing.T) {
	scanner := engine.NewMockScanner()
	result, err := scanner.Scan(context.Background())

	if err != nil {
		t.Fatalf("MockScanner.Scan() error = %v", err)
	}

	if result == nil {
		t.Fatal("MockScanner.Scan() returned nil")
	}

	if result.TotalResources == 0 {
		t.Error("TotalResources should not be 0")
	}

	if len(result.ByAccount) == 0 {
		t.Error("ByAccount should not be empty")
	}

	if len(result.ByTag) == 0 {
		t.Error("ByTag should not be empty")
	}

	if len(result.Violations) == 0 {
		t.Error("Violations should not be empty for mock data")
	}
}

type stubProvider struct {
	resources []types.Resource
	err       error
}

func (p stubProvider) Name() string { return providerAWS }
func (p stubProvider) ListResources(context.Context) ([]types.Resource, error) {
	return p.resources, p.err
}
func (p stubProvider) ApplyTags(context.Context, string, map[string]string) error { return nil }

func runPartialScan(t *testing.T, flags map[string]string) (*types.ScanResult, error) {
	t.Helper()

	cfgYAML := "clouds:\n  aws:\n    - profile: default\n      regions: [us-east-1]\npolicy:\n  required:\n    - name: owner\n"
	if err := loadConfigFrom(t, cfgYAML); err != nil {
		t.Fatal(err)
	}

	originalDir, originalProviders := OutputDir, scanProviders
	OutputDir = t.TempDir()
	scanProviders = func(context.Context, *config.Config, []string) ([]provider.Provider, error) {
		return []provider.Provider{
			stubProvider{resources: []types.Resource{{ID: "i-1", Type: "aws_instance", Provider: providerAWS, Account: "123456789012", Region: "us-east-1"}}},
			stubProvider{err: errors.New("ec2 in eu-west-1: AccessDenied")},
		}, nil
	}
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		OutputDir, scanProviders = originalDir, originalProviders
		log.SetOutput(os.Stderr)
	})

	for name, value := range flags {
		defValue := scanCmd.Flags().Lookup(name).DefValue
		if err := scanCmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = scanCmd.Flags().Set(name, defValue) })
	}

	runErr := runScan(scanCmd, nil)

	scans, err := findRecentScans(1)
	if err != nil || len(scans) != 1 {
		t.Fatalf("scan JSON not written: %v", err)
	}
	written, err := LoadScanFile(scans[0])
	if err != nil {
		t.Fatal(err)
	}
	return written, runErr
}

func TestRunScan_PartialDiscovery(t *testing.T) {
	const partialErr, gateErr = "--allow-partial", "compliance gate failed"
	tests := []struct {
		name     string
		flags    map[string]string
		wantErr  string
		wantCode int
	}{
		{name: "fails after writing reports", wantErr: partialErr, wantCode: exitError},
		{name: "fails before the gate", flags: map[string]string{"fail-under": "100"}, wantErr: partialErr, wantCode: exitError},
		{name: "succeeds with --allow-partial", flags: map[string]string{"allow-partial": "true"}, wantCode: exitOK},
		{name: "runs the gate with --allow-partial", flags: map[string]string{"allow-partial": "true", "fail-under": "100"}, wantErr: gateErr, wantCode: exitGateFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written, err := runPartialScan(t, tt.flags)

			if code := ExitCode(err); code != tt.wantCode {
				t.Errorf("ExitCode(%v) = %d, want %d", err, code, tt.wantCode)
			}

			switch {
			case tt.wantErr == "":
				if err != nil {
					t.Errorf("runScan() error = %v, want nil", err)
				}
			case err == nil || !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("runScan() error = %v, want one containing %q", err, tt.wantErr)
			case tt.wantErr == partialErr && !strings.Contains(err.Error(), "AccessDenied"):
				t.Errorf("runScan() error = %v, want it to name the discovery failure", err)
			}

			if !written.Partial || len(written.Errors) != 1 || written.TotalResources != 1 {
				t.Errorf("scan JSON partial = %v, errors = %q, total = %d; want a partial scan keeping the healthy provider's resource",
					written.Partial, written.Errors, written.TotalResources)
			}
		})
	}
}
