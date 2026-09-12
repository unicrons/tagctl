package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
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

type listProvider struct {
	resources []types.Resource
	err       error
}

func (p listProvider) Name() string { return "aws" }
func (p listProvider) ListResources(context.Context) ([]types.Resource, error) {
	return p.resources, p.err
}
func (p listProvider) ApplyTags(context.Context, string, map[string]string) error { return nil }

func instance(id string) types.Resource {
	return types.Resource{ID: id, Type: "aws_instance", Provider: "aws", Account: "123456789012", Region: "us-east-1"}
}

func newTestScanner(t *testing.T, providers ...provider.Provider) *RealScanner {
	t.Helper()
	scanner, err := NewScanner(providers, config.PolicyConfig{}, config.IgnoreConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return scanner
}

func TestRealScanner_PartialDiscovery(t *testing.T) {
	errRegion := errors.New("ec2 in eu-west-1: AccessDenied")
	errAccount := errors.New("sts: expired token")
	scanner := newTestScanner(t,
		listProvider{resources: []types.Resource{instance("i-1"), instance("i-2")}},
		listProvider{resources: []types.Resource{instance("i-3")}, err: errRegion},
		listProvider{err: errAccount},
	)

	result, err := scanner.Scan(context.Background())

	if !errors.Is(err, errRegion) || !errors.Is(err, errAccount) {
		t.Fatalf("Scan() error = %v, want both provider errors joined", err)
	}
	if result == nil {
		t.Fatal("Scan() returned no result for a partial scan")
	}
	if !result.Partial {
		t.Error("Partial = false, want true")
	}
	want := []string{"provider aws: " + errRegion.Error(), "provider aws: " + errAccount.Error()}
	if len(result.Errors) != len(want) || result.Errors[0] != want[0] || result.Errors[1] != want[1] {
		t.Errorf("Errors = %q, want %q", result.Errors, want)
	}
	if result.TotalResources != 3 {
		t.Errorf("TotalResources = %d, want 3 (resources of failing providers are kept)", result.TotalResources)
	}
}

func TestRealScanner_CompleteScanIsNotPartial(t *testing.T) {
	scanner := newTestScanner(t, listProvider{resources: []types.Resource{instance("i-1")}}, listProvider{})

	result, err := scanner.Scan(context.Background())

	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if result.Partial || result.Errors != nil {
		t.Errorf("Partial = %v, Errors = %q, want a complete scan", result.Partial, result.Errors)
	}
}

func TestRealScanner_ReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanner := newTestScanner(t, listProvider{resources: []types.Resource{instance("i-1")}})

	result, err := scanner.Scan(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v, want context.Canceled", err)
	}
	if result != nil {
		t.Errorf("Scan() result = %+v, want nil for a cancelled scan", result)
	}
}
