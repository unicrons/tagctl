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

// MustLoadResources loads resources or panics. Use only in tests.
func MustLoadResources(name string) []types.Resource {
	resources, err := LoadResources(name)
	if err != nil {
		panic(err)
	}
	return resources
}
