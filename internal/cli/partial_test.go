package cli

import (
	"bytes"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestWarnPartialScan(t *testing.T) {
	tests := []struct {
		name string
		scan *types.ScanResult
		want string
	}{
		{
			name: "partial scan",
			scan: &types.ScanResult{Partial: true, Errors: []string{"provider aws: AccessDenied"}},
			want: "Warning: scan.json is a partial scan (1 provider(s) failed discovery); counts may be wrong\n",
		},
		{
			name: "complete scan",
			scan: &types.ScanResult{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			warnPartialScan(&buf, "scan.json", tt.scan, "counts may be wrong")
			if got := buf.String(); got != tt.want {
				t.Errorf("warnPartialScan() wrote %q, want %q", got, tt.want)
			}
		})
	}
}
