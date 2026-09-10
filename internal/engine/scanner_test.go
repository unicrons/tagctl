package engine

import (
	"context"
	"testing"
)

func TestMockScanner_Scan(t *testing.T) {
	scanner := NewMockScanner()
	result, err := scanner.Scan(context.Background())

	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if result == nil {
		t.Fatal("Scan() returned nil result")
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
		t.Error("Violations should not be empty")
	}
}
