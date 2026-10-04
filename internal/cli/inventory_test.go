package cli

import (
	"bytes"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestWarnNoInventory(t *testing.T) {
	failed := types.Finding{Resource: types.Resource{ID: "i-1"}, Tag: "owner", Status: types.StatusFailed}

	tests := []struct {
		name string
		scan *types.ScanResult
		want string
	}{
		{
			name: "resources counted but none named",
			scan: &types.ScanResult{TotalResources: 3},
			want: "Warning: scan.json counts 3 resource(s) but names none (no inventory and no findings); new and removed resources may be wrong\n",
		},
		{
			name: "inventory names the resources",
			scan: &types.ScanResult{TotalResources: 1, Resources: []types.ResourceRef{{Identity: "aws/111/i-1", ID: "i-1"}}},
			want: "",
		},
		{
			name: "findings name the resources",
			scan: &types.ScanResult{TotalResources: 1, Findings: []types.Finding{failed}},
			want: "",
		},
		{
			name: "legacy violations name the resources",
			scan: &types.ScanResult{TotalResources: 1, Violations: []types.Violation{types.Violation(failed)}},
			want: "",
		},
		{
			name: "empty scan",
			scan: &types.ScanResult{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			warnNoInventory(&buf, "scan.json", tt.scan)
			if got := buf.String(); got != tt.want {
				t.Errorf("warnNoInventory() wrote %q, want %q", got, tt.want)
			}
		})
	}
}
