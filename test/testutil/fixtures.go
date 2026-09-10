// Package testutil provides utilities for testing tagctl components.
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/unicrons/tagctl/internal/types"
)

// testdataDir returns the path to the testdata directory.
func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	// Go up from test/testutil to test/, then into testdata
	return filepath.Join(filepath.Dir(filename), "..", "testdata")
}

// ConfigPath returns the full path to a config file in testdata/configs.
func ConfigPath(name string) string {
	return filepath.Join(testdataDir(), "configs", name)
}

// FixturePath returns the full path to a fixture file in testdata/fixtures.
func FixturePath(name string) string {
	return filepath.Join(testdataDir(), "fixtures", name)
}

// LoadPlan loads a plan fixture by name (e.g., "plan-with-changes.json").
func LoadPlan(name string) (*types.Plan, error) {
	data, err := os.ReadFile(FixturePath(name))
	if err != nil {
		return nil, err
	}

	var plan types.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}

	return &plan, nil
}

// LoadResources loads a resources fixture by name (e.g., "resources-aws.json").
func LoadResources(name string) ([]types.Resource, error) {
	data, err := os.ReadFile(FixturePath(name))
	if err != nil {
		return nil, err
	}

	var resources []types.Resource
	if err := json.Unmarshal(data, &resources); err != nil {
		return nil, err
	}

	return resources, nil
}

// LoadViolations loads a violations fixture by name.
func LoadViolations(name string) ([]types.Violation, error) {
	data, err := os.ReadFile(FixturePath(name))
	if err != nil {
		return nil, err
	}

	var violations []types.Violation
	if err := json.Unmarshal(data, &violations); err != nil {
		return nil, err
	}

	return violations, nil
}

// LoadScanResult loads a scan result fixture by name.
func LoadScanResult(name string) (*types.ScanResult, error) {
	data, err := os.ReadFile(FixturePath(name))
	if err != nil {
		return nil, err
	}

	var result types.ScanResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// MustLoadPlan loads a plan or panics. Use only in tests.
func MustLoadPlan(name string) *types.Plan {
	plan, err := LoadPlan(name)
	if err != nil {
		panic(err)
	}
	return plan
}

// MustLoadResources loads resources or panics. Use only in tests.
func MustLoadResources(name string) []types.Resource {
	resources, err := LoadResources(name)
	if err != nil {
		panic(err)
	}
	return resources
}
