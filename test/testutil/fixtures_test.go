package testutil

import (
	"os"
	"testing"
)

func TestConfigPath(t *testing.T) {
	path := ConfigPath("valid.yaml")

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("ConfigPath('valid.yaml') returned non-existent path: %s", path)
	}
}

func TestFixturePath(t *testing.T) {
	path := FixturePath("plan-with-changes.json")

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("FixturePath('plan-with-changes.json') returned non-existent path: %s", path)
	}
}

func TestLoadResources(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantErr   bool
		wantCount int
	}{
		{
			name:      "AWS resources",
			fixture:   "resources-aws.json",
			wantErr:   false,
			wantCount: 7,
		},
		{
			name:      "K8s resources",
			fixture:   "resources-k8s.json",
			wantErr:   false,
			wantCount: 6,
		},
		{
			name:    "non-existent file",
			fixture: "does-not-exist.json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources, err := LoadResources(tt.fixture)

			if tt.wantErr {
				if err == nil {
					t.Error("LoadResources() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadResources() unexpected error: %v", err)
			}

			if len(resources) != tt.wantCount {
				t.Errorf("LoadResources() got %d resources, want %d", len(resources), tt.wantCount)
			}
		})
	}
}

func TestMustLoadResources(t *testing.T) {
	// Should not panic
	resources := MustLoadResources("resources-aws.json")
	if len(resources) == 0 {
		t.Error("MustLoadResources() returned empty slice")
	}
}
