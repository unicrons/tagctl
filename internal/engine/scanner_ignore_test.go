package engine

import (
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "aws_instance", true},
		{"aws_instance", "aws_instance", true},
		{"aws_instance", "aws_instance_profile", false},
		{"aws_iam_*", "aws_iam_role", true},
		{"aws_iam_*", "aws_s3_bucket", false},
		{"*_group", "aws_security_group", true},
		{"aws_*_group", "aws_security_group", true},
		{"aws_*_group", "aws_security_group_rule", false},
		{"aws_s3_bucke?", "aws_s3_bucket", true},
		{"aws_[", "aws_[", false},
	}
	for _, tc := range cases {
		if got := matchGlob(tc.pattern, tc.value); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

func TestFilterIgnored(t *testing.T) {
	role := types.Resource{ID: "r", Type: "aws_iam_role"}
	sg := types.Resource{ID: "sg", Type: "aws_security_group"}
	managed := types.Resource{ID: "i-1", Type: "aws_instance", Tags: map[string]string{"ManagedBy": "terraform"}}
	lowercaseKey := types.Resource{ID: "i-2", Type: "aws_instance", Tags: map[string]string{"managedby": "terraform"}}
	otherValue := types.Resource{ID: "i-3", Type: "aws_instance", Tags: map[string]string{"ManagedBy": "console"}}

	scanner := &RealScanner{ignore: config.IgnoreConfig{
		Resources: []string{"aws_iam_*", "aws_*_group"},
		Tags:      map[string][]string{"ManagedBy": {"terraform"}},
	}}
	got := scanner.filterIgnored([]types.Resource{role, sg, managed, lowercaseKey, otherValue})

	want := []string{"i-2", "i-3"}
	if len(got) != len(want) {
		t.Fatalf("filterIgnored() kept %+v, want ids %v", got, want)
	}
	for i, r := range got {
		if r.ID != want[i] {
			t.Errorf("filterIgnored()[%d] = %s, want %s", i, r.ID, want[i])
		}
	}
}
