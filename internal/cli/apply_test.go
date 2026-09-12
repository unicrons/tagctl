package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestLoadPlan_RejectsActionsApplyCannotPerform(t *testing.T) {
	resource := types.Resource{ID: "i-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-1", Provider: "aws", Type: "aws_instance"}
	tests := []struct {
		name    string
		action  types.ChangeAction
		wantErr bool
	}{
		{"add", types.ActionAdd, false},
		{"update", types.ActionUpdate, false},
		{"remove", types.ActionRemove, true},
		{"empty action", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plan.json")
			plan := types.Plan{Changes: []types.TagChange{{Resource: resource, Tag: "owner", Action: tt.action, NewValue: "team@example.com"}}}
			data, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}

			_, err = loadPlan(path)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("loadPlan() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), `"owner"`) || !strings.Contains(err.Error(), resource.ARN) {
				t.Errorf("loadPlan() error = %v, want one naming the tag and resource", err)
			}
		})
	}
}
