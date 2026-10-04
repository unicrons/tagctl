package engine

import (
	"context"
	"errors"
	"path"
	"slices"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

func typed(id, resourceType string) types.Resource {
	r := instance(id)
	r.Type = resourceType
	return r
}

func scannedTypes(t *testing.T, scanner *RealScanner) []string {
	t.Helper()
	result, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		found = append(found, finding.Resource.Type)
	}
	slices.Sort(found)
	if result.TotalResources != len(found) {
		t.Errorf("TotalResources = %d, want one per finding (%d)", result.TotalResources, len(found))
	}
	return found
}

func TestRealScanner_OnlyTypes(t *testing.T) {
	resources := []types.Resource{
		typed("i-1", "aws_instance"),
		typed("b-1", "aws_s3_bucket"),
		typed("ap-1", "aws_s3_access_point"),
		typed("q-1", "aws_sqs_queue"),
	}
	policy := config.PolicyConfig{Required: []config.TagRequirement{{Name: "owner"}}}

	tests := []struct {
		name     string
		patterns []string
		ignore   []string
		want     []string
	}{
		{name: "no pattern keeps every type", want: []string{"aws_instance", "aws_s3_access_point", "aws_s3_bucket", "aws_sqs_queue"}},
		{name: "glob", patterns: []string{"aws_s3_*"}, want: []string{"aws_s3_access_point", "aws_s3_bucket"}},
		{name: "exact type", patterns: []string{"aws_instance"}, want: []string{"aws_instance"}},
		{name: "patterns are a union", patterns: []string{"aws_s3_*", "aws_sqs_queue"}, want: []string{"aws_s3_access_point", "aws_s3_bucket", "aws_sqs_queue"}},
		{name: "ignore.resources still applies", patterns: []string{"aws_s3_*"}, ignore: []string{"aws_s3_bucket"}, want: []string{"aws_s3_access_point"}},
		{name: "no match", patterns: []string{"aws_rds_*"}, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner, err := NewScanner([]provider.Provider{listProvider{resources: resources}}, policy, config.IgnoreConfig{Resources: tt.ignore})
			if err != nil {
				t.Fatal(err)
			}
			if err = scanner.OnlyTypes(tt.patterns); err != nil {
				t.Fatal(err)
			}

			if got := scannedTypes(t, scanner); !slices.Equal(got, tt.want) {
				t.Errorf("scanned types = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRealScanner_OnlyTypesRejectsBadPatterns(t *testing.T) {
	scanner := newTestScanner(t)

	if err := scanner.OnlyTypes([]string{"aws_s3_*", "aws_["}); !errors.Is(err, path.ErrBadPattern) {
		t.Errorf("OnlyTypes(malformed) error = %v, want path.ErrBadPattern", err)
	}
	if err := scanner.OnlyTypes([]string{""}); err == nil {
		t.Error("OnlyTypes(empty pattern) error = nil")
	}
}
