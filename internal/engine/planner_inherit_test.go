package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

const (
	instanceARN = "arn:aws:ec2:us-east-1:123456789012:instance/i-1"
	volumeARN   = "arn:aws:ec2:us-east-1:123456789012:volume/vol-1"
	snapshotARN = "arn:aws:ec2:us-east-1:123456789012:snapshot/snap-1"
)

func compliant(tag string, r types.Resource) types.Finding {
	return types.Finding{Resource: r, Tag: tag, Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: r.Tags[tag]}
}

func inheritPlanner(t *testing.T, rules ...config.InheritRule) *RealPlanner {
	t.Helper()
	planner, err := NewPlanner(config.RulesConfig{Inherit: rules})
	if err != nil {
		t.Fatal(err)
	}
	return planner
}

func planFor(t *testing.T, planner *RealPlanner, findings ...types.Finding) *types.Plan {
	t.Helper()
	plan, err := planner.Plan(context.Background(), &types.ScanResult{Findings: findings})
	if err != nil {
		t.Fatalf("Plan() = %v", err)
	}
	return plan
}

func TestRealPlanner_InheritsFromTheParent(t *testing.T) {
	instance := types.Resource{ID: "i-1", ARN: instanceARN, Type: "aws_instance", Tags: map[string]string{"environment": "prod", "owner": "bob"}}
	volume := types.Resource{
		ID: "vol-1", ARN: volumeARN, Type: "aws_ebs_volume",
		Parents: map[string]string{types.RelationAttachedInstance: instanceARN},
	}
	volumeRule := config.InheritRule{Resource: "aws_ebs_volume", From: types.RelationAttachedInstance, Tags: []string{"environment", "owner"}}
	invalidOwner := types.Finding{Resource: instance, Tag: "owner", Status: types.StatusFailed, Reason: types.ReasonInvalidFormat, Actual: "bob"}

	cases := []struct {
		name     string
		rule     config.InheritRule
		findings []types.Finding
		want     map[string]string
	}{
		{
			"tag the parent carries",
			volumeRule,
			[]types.Finding{compliant("environment", instance), missing("environment", volume)},
			map[string]string{"environment": "prod"},
		},
		{
			"parent value that fails the policy is not handed down",
			volumeRule,
			[]types.Finding{compliant("environment", instance), invalidOwner, missing("environment", volume), missing("owner", volume)},
			map[string]string{"environment": "prod"},
		},
		{
			"tag the rule does not list",
			config.InheritRule{Resource: "aws_ebs_volume", From: types.RelationAttachedInstance, Tags: []string{"owner"}},
			[]types.Finding{compliant("environment", instance), missing("environment", volume)},
			nil,
		},
		{
			"rule for another resource type",
			config.InheritRule{Resource: "aws_ebs_snapshot", Tags: []string{"environment"}},
			[]types.Finding{compliant("environment", instance), missing("environment", volume)},
			nil,
		},
		{
			"rule for a relation the resource does not have",
			config.InheritRule{Resource: "aws_*", From: types.RelationVPC, Tags: []string{"environment"}},
			[]types.Finding{compliant("environment", instance), missing("environment", volume)},
			nil,
		},
		{
			"rule without from follows any recorded parent",
			config.InheritRule{Resource: "aws_*", Tags: []string{"environment"}},
			[]types.Finding{compliant("environment", instance), missing("environment", volume)},
			map[string]string{"environment": "prod"},
		},
		{
			"parent missing from the scan",
			volumeRule,
			[]types.Finding{missing("environment", volume)},
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planFor(t, inheritPlanner(t, tc.rule), tc.findings...)
			got := make(map[string]string)
			for _, c := range plan.Changes {
				if c.Resource.Identity() != volumeARN || c.Action != types.ActionAdd || c.Reason != types.ReasonInherited {
					t.Errorf("unexpected change %+v", c)
				}
				if c.Source != "from attached instance i-1" {
					t.Errorf("Source = %q, want it to name the parent", c.Source)
				}
				got[c.Tag] = c.NewValue
			}
			if len(got) != len(tc.want) {
				t.Fatalf("changes = %v, want %v", got, tc.want)
			}
			for tag, value := range tc.want {
				if got[tag] != value {
					t.Errorf("%s = %q, want %q", tag, got[tag], value)
				}
			}
			if len(plan.Warnings) != 0 {
				t.Errorf("Warnings = %v, want none when the scan records parents", plan.Warnings)
			}
		})
	}
}

func TestRealPlanner_InheritsThroughAParentMissingTheTag(t *testing.T) {
	instance := types.Resource{ID: "i-1", ARN: instanceARN, Type: "aws_instance", Tags: map[string]string{"environment": "prod"}}
	volume := types.Resource{ID: "vol-1", ARN: volumeARN, Type: "aws_ebs_volume", Parents: map[string]string{types.RelationAttachedInstance: instanceARN}}
	snapshot := types.Resource{ID: "snap-1", ARN: snapshotARN, Type: "aws_ebs_snapshot", Parents: map[string]string{types.RelationSourceVolume: volumeARN}}
	planner := inheritPlanner(t, config.InheritRule{Resource: "aws_ebs_*", Tags: []string{"environment"}})

	plan := planFor(t, planner, compliant("environment", instance), missing("environment", volume), missing("environment", snapshot))

	sources := make(map[string]string)
	for _, c := range plan.Changes {
		if c.NewValue != "prod" {
			t.Errorf("%s gets %q, want prod", c.Resource.ID, c.NewValue)
		}
		sources[c.Resource.ID] = c.Source
	}
	want := map[string]string{"vol-1": "from attached instance i-1", "snap-1": "from source volume vol-1"}
	if len(sources) != len(want) || sources["vol-1"] != want["vol-1"] || sources["snap-1"] != want["snap-1"] {
		t.Errorf("sources = %v, want %v", sources, want)
	}
}

func TestRealPlanner_InheritStopsOnAParentCycle(t *testing.T) {
	a := types.Resource{ID: "a", Type: "aws_ebs_volume", Parents: map[string]string{types.RelationSourceVolume: "b"}}
	b := types.Resource{ID: "b", Type: "aws_ebs_volume", Parents: map[string]string{types.RelationSourceVolume: "a"}}
	planner := inheritPlanner(t, config.InheritRule{Resource: "*", Tags: []string{"environment"}})

	if plan := planFor(t, planner, missing("environment", a), missing("environment", b)); len(plan.Changes) != 0 {
		t.Errorf("changes = %+v, want none", plan.Changes)
	}
}

func TestRealPlanner_InferenceWinsOverInheritanceAndInheritanceOverDefaults(t *testing.T) {
	instance := types.Resource{ID: "i-1", ARN: instanceARN, Type: "aws_instance", Tags: map[string]string{"environment": "prod", "owner": "team@example.com"}}
	volume := types.Resource{
		ID: "vol-1", Name: "db-dev-data", ARN: volumeARN, Type: "aws_ebs_volume",
		Parents: map[string]string{types.RelationAttachedInstance: instanceARN},
	}
	planner, err := NewPlanner(config.RulesConfig{
		Infer:    []config.InferRule{{Tag: "environment", FromName: []config.NamePattern{{Pattern: "-dev-", Value: "dev"}}}},
		Inherit:  []config.InheritRule{{Resource: "aws_ebs_volume", Tags: []string{"environment", "owner"}}},
		Defaults: []config.DefaultRule{{Resource: "*", Set: map[string]string{"owner": "platform@example.com"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := planFor(t, planner, compliant("environment", instance), compliant("owner", instance), missing("environment", volume), missing("owner", volume))

	reasons := make(map[string]types.ChangeReason)
	for _, c := range plan.Changes {
		reasons[c.Tag] = c.Reason
	}
	if len(reasons) != 2 || reasons["environment"] != types.ReasonInferred || reasons["owner"] != types.ReasonInherited {
		t.Errorf("reasons = %v, want environment inferred and owner inherited", reasons)
	}
}

func TestRealPlanner_WarnsWhenTheScanRecordsNoParents(t *testing.T) {
	rule := config.InheritRule{Resource: "aws_ebs_volume", From: types.RelationAttachedInstance, Tags: []string{"environment"}}
	volume := types.Resource{ID: "vol-1", ARN: volumeARN, Type: "aws_ebs_volume"}

	var oldScan types.ScanResult
	oldJSON := `{"findings":[{"resource":{"id":"vol-1","arn":"` + volumeARN + `","type":"aws_ebs_volume","tags":{}},"tag":"environment","status":"FAILED","reason":"missing"}]}`
	if err := json.Unmarshal([]byte(oldJSON), &oldScan); err != nil {
		t.Fatal(err)
	}
	plan, err := inheritPlanner(t, rule).Plan(context.Background(), &oldScan)
	if err != nil {
		t.Fatalf("Plan() = %v", err)
	}
	if len(plan.Changes) != 0 || len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "rules.inherit could not be applied") {
		t.Errorf("plan = %+v, want no changes and a warning about inheritance", plan)
	}

	legacy := &types.ScanResult{Violations: []types.Violation{types.Violation(missing("environment", volume))}}
	if plan, _ := inheritPlanner(t, rule).Plan(context.Background(), legacy); len(plan.Warnings) != 1 {
		t.Errorf("Warnings = %v, want one for a scan with only legacy violations", plan.Warnings)
	}

	if plan := planFor(t, inheritPlanner(t), missing("environment", volume)); len(plan.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none without inherit rules", plan.Warnings)
	}
}
