package cli

import (
	"context"
	"testing"

	"github.com/unicrons/tagctl/internal/engine"
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
