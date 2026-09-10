package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestLoadResourcesFromJSON(t *testing.T) {
	// Create a temp directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		content   string
		wantErr   bool
		wantCount int
	}{
		{
			name: "valid single resource",
			content: `[{
				"id": "i-123",
				"type": "aws_instance",
				"name": "test-instance",
				"region": "us-east-1",
				"account": "123456789",
				"provider": "aws",
				"tags": {"environment": "prod"}
			}]`,
			wantErr:   false,
			wantCount: 1,
		},
		{
			name: "valid multiple resources",
			content: `[
				{"id": "i-123", "type": "aws_instance", "name": "test-1", "tags": {}},
				{"id": "i-456", "type": "aws_instance", "name": "test-2", "tags": {"team": "backend"}}
			]`,
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:      "empty array",
			content:   `[]`,
			wantErr:   false,
			wantCount: 0,
		},
		{
			name:    "invalid json",
			content: `{invalid}`,
			wantErr: true,
		},
		{
			name:    "not an array",
			content: `{"id": "i-123"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test content to file
			filePath := filepath.Join(tmpDir, tt.name+".json")
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			resources, err := loadResourcesFromJSON(filePath)

			if tt.wantErr {
				if err == nil {
					t.Error("loadResourcesFromJSON() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("loadResourcesFromJSON() unexpected error: %v", err)
				return
			}

			if len(resources) != tt.wantCount {
				t.Errorf("loadResourcesFromJSON() got %d resources, want %d", len(resources), tt.wantCount)
			}
		})
	}
}

func TestLoadResourcesFromJSON_FileNotFound(t *testing.T) {
	_, err := loadResourcesFromJSON("/nonexistent/path.json")
	if err == nil {
		t.Error("loadResourcesFromJSON() expected error for nonexistent file, got nil")
	}
}

func TestEvaluateOutput_JSONFormat(t *testing.T) {
	output := EvaluateOutput{
		Findings: []EvaluateFinding{
			{
				Resource: EvaluateResource{
					ID:   "i-123",
					Type: "aws_instance",
					Name: "test-instance",
				},
				Tag:    "environment",
				Status: "PASS",
				Reason: "compliant",
				Actual: valueProd,
			},
			{
				Resource: EvaluateResource{
					ID:   "i-123",
					Type: "aws_instance",
					Name: "test-instance",
				},
				Tag:      "cost-center",
				Status:   "FAILED",
				Reason:   "missing",
				Expected: "",
				Actual:   "",
			},
		},
		Summary: EvaluateSummary{
			TotalResources: 1,
			TotalFindings:  2,
			Passed:         1,
			Failed:         1,
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal EvaluateOutput: %v", err)
	}

	// Unmarshal back
	var decoded EvaluateOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal EvaluateOutput: %v", err)
	}

	// Verify structure
	if len(decoded.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(decoded.Findings))
	}

	if decoded.Summary.TotalResources != 1 {
		t.Errorf("expected TotalResources=1, got %d", decoded.Summary.TotalResources)
	}

	if decoded.Summary.Failed != 1 {
		t.Errorf("expected Failed=1, got %d", decoded.Summary.Failed)
	}
}

func TestEvaluateResource_OmitEmpty(t *testing.T) {
	// Test that empty ARN is omitted from JSON
	resource := EvaluateResource{
		ID:   "i-123",
		Type: "aws_instance",
		Name: "test",
		ARN:  "", // empty - should be omitted
	}

	data, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if bytes.Contains(data, []byte(`"arn"`)) {
		t.Error("expected empty ARN to be omitted from JSON")
	}

	// Test that non-empty ARN is included
	resource.ARN = "arn:aws:ec2:us-east-1:123:instance/i-123"
	data, err = json.Marshal(resource)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if !bytes.Contains(data, []byte(`"arn"`)) {
		t.Error("expected non-empty ARN to be included in JSON")
	}
}

func TestResourceTypes_Compatibility(t *testing.T) {
	// Verify that types.Resource can be decoded from expected JSON format
	jsonInput := `{
		"id": "i-0abc123def456",
		"arn": "arn:aws:ec2:us-east-1:123456789:instance/i-0abc123def456",
		"type": "aws_instance",
		"name": "web-prod-api-1",
		"region": "us-east-1",
		"account": "123456789",
		"provider": "aws",
		"tags": {
			"environment": "prod",
			"team": "backend"
		}
	}`

	var resource types.Resource
	if err := json.Unmarshal([]byte(jsonInput), &resource); err != nil {
		t.Fatalf("failed to unmarshal types.Resource: %v", err)
	}

	if resource.ID != "i-0abc123def456" {
		t.Errorf("expected ID=i-0abc123def456, got %s", resource.ID)
	}

	if resource.Type != "aws_instance" {
		t.Errorf("expected Type=aws_instance, got %s", resource.Type)
	}

	if resource.Tags["environment"] != valueProd {
		t.Errorf("expected environment=prod, got %s", resource.Tags["environment"])
	}

	if resource.Tags["team"] != "backend" {
		t.Errorf("expected team=backend, got %s", resource.Tags["team"])
	}
}

func TestLoadResourcesFromJSON_ProwlerFormat(t *testing.T) {
	// Test loading resources in Prowler-like format
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "prowler_resources.json")

	prowlerJSON := `[
		{
			"id": "i-0abc123def456",
			"arn": "arn:aws:ec2:us-east-1:123456789:instance/i-0abc123def456",
			"type": "aws_instance",
			"name": "web-prod-api-1",
			"region": "us-east-1",
			"account": "123456789",
			"provider": "aws",
			"tags": {
				"environment": "prod",
				"team": "backend"
			}
		},
		{
			"id": "my-bucket",
			"arn": "arn:aws:s3:::my-bucket",
			"type": "aws_s3_bucket",
			"name": "my-bucket",
			"region": "us-east-1",
			"account": "123456789",
			"provider": "aws",
			"tags": {}
		}
	]`

	if err := os.WriteFile(filePath, []byte(prowlerJSON), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	resources, err := loadResourcesFromJSON(filePath)
	if err != nil {
		t.Fatalf("loadResourcesFromJSON() error: %v", err)
	}

	if len(resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(resources))
	}

	// Verify first resource
	if resources[0].ID != "i-0abc123def456" {
		t.Errorf("expected first resource ID=i-0abc123def456, got %s", resources[0].ID)
	}
	if resources[0].ARN != "arn:aws:ec2:us-east-1:123456789:instance/i-0abc123def456" {
		t.Errorf("unexpected ARN: %s", resources[0].ARN)
	}

	// Verify second resource (S3 bucket with no tags)
	if resources[1].Type != "aws_s3_bucket" {
		t.Errorf("expected second resource Type=aws_s3_bucket, got %s", resources[1].Type)
	}
	if len(resources[1].Tags) != 0 {
		t.Errorf("expected second resource to have no tags, got %d", len(resources[1].Tags))
	}
}
