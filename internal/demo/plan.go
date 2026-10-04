package demo

import (
	"fmt"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

// Plan returns the example plan shown when no provider is configured,
// stamped with the current time.
func Plan() *types.Plan {
	instance := types.Resource{ID: "i-0abc123", Name: "web-prod-api-1", Type: "aws_instance", Account: "production", Provider: "aws"}
	volume := types.Resource{ID: "vol-xyz789", Type: "aws_ebs_volume", Account: "production", Provider: "aws"}
	bucket := types.Resource{ID: "legacy-bucket", Name: "legacy-data-2019", Type: "aws_s3_bucket", Account: "production", Provider: "aws"}

	return &types.Plan{
		ID:        fmt.Sprintf("plan-%s", time.Now().Format("20060102-150405")),
		CreatedAt: time.Now(),
		Changes: []types.TagChange{
			{Resource: instance, Tag: "environment", Action: types.ActionAdd, NewValue: "prod", Reason: types.ReasonInferred, Source: "name contains '-prod-'"},
			{Resource: instance, Tag: "team", Action: types.ActionAdd, NewValue: "backend", Reason: types.ReasonInherited, Source: "from ASG 'backend-asg'"},
			{Resource: volume, Tag: "environment", Action: types.ActionAdd, NewValue: "prod", Reason: types.ReasonInherited, Source: "from attached instance i-0abc123"},
			{Resource: bucket, Tag: "owner", Action: types.ActionAdd, NewValue: "platform-team@company.com", Reason: types.ReasonDefault, Source: "default for untagged S3 buckets"},
			{Resource: bucket, Tag: "needs-review", Action: types.ActionAdd, NewValue: "true", Reason: types.ReasonDefault, Source: "default for untagged S3 buckets"},
		},
		Summary: types.PlanSummary{
			TotalResources: 3,
			TotalChanges:   5,
			TagsAdded:      5,
		},
	}
}
