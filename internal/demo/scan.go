// Package demo holds the example data behind scan --mock, the demo mode of
// scan and plan, and the simulated apply. Nothing in it reaches a cloud.
package demo

import (
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/types"
)

// Scan returns the fixed example scan, stamped with the current time.
func Scan() *types.ScanResult {
	log.Debug("Scanner: Creating mock scanner for demo mode")
	log.Debug("Scanner: Generating mock scan results")
	result := types.NewScanResult()
	result.TotalResources = 150
	result.CompliantCount = 98
	result.ViolationCount = 52
	result.CompliancePct = 65.3

	result.ByAccount["production"] = &types.AccountStats{
		Account:       "production",
		Provider:      "aws",
		Total:         100,
		Compliant:     85,
		CompliancePct: 85.0,
	}
	result.ByAccount["staging"] = &types.AccountStats{
		Account:       "staging",
		Provider:      "aws",
		Total:         50,
		Compliant:     13,
		CompliancePct: 26.0,
	}

	result.ByTag["environment"] = &types.TagStats{
		Tag:           "environment",
		Required:      true,
		Present:       140,
		Missing:       10,
		Invalid:       0,
		CompliancePct: 93.3,
	}
	result.ByTag["cost-center"] = &types.TagStats{
		Tag:           "cost-center",
		Required:      true,
		Present:       98,
		Missing:       52,
		Invalid:       5,
		CompliancePct: 62.0,
	}
	result.ByTag["owner"] = &types.TagStats{
		Tag:           "owner",
		Required:      true,
		Present:       75,
		Missing:       75,
		Invalid:       10,
		CompliancePct: 43.3,
	}

	webProd := types.Resource{ID: "i-0prod123", Name: "web-prod-1", Type: "aws_instance", Account: "production", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0prod123"}
	apiProd := types.Resource{ID: "i-0prod456", Name: "api-prod-1", Type: "aws_instance", Account: "production", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0prod456"}
	webStaging := types.Resource{ID: "i-0abc123", Name: "web-staging-1", Type: "aws_instance", Account: "staging", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123"}
	apiStaging := types.Resource{ID: "i-0def456", Name: "api-staging-2", Type: "aws_instance", Account: "staging", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0def456"}
	bucketLegacy := types.Resource{ID: "bucket-legacy", Name: "legacy-data-bucket", Type: "aws_s3_bucket", Account: "production", Provider: "aws", Region: "us-west-2", ARN: "arn:aws:s3:::legacy-data-bucket"}

	result.Violations = []types.Violation{
		{Resource: webStaging, Tag: "cost-center", Reason: types.ReasonMissing},
		{Resource: apiStaging, Tag: "owner", Reason: types.ReasonMissing},
		{Resource: bucketLegacy, Tag: "owner", Reason: types.ReasonInvalidFormat, Actual: "john", Expected: "^.+@company\\.com$"},
	}

	result.Findings = []types.Finding{
		// web-prod-1: all tags pass
		{Resource: webProd, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "prod"},
		{Resource: webProd, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "TECH-001"},
		{Resource: webProd, Tag: "owner", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "team@company.com"},
		// api-prod-1: all tags pass
		{Resource: apiProd, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "prod"},
		{Resource: apiProd, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "TECH-002"},
		{Resource: apiProd, Tag: "owner", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "api@company.com"},
		// web-staging-1: cost-center missing
		{Resource: webStaging, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "staging"},
		{Resource: webStaging, Tag: "cost-center", Status: types.StatusFailed, Reason: types.ReasonMissing},
		{Resource: webStaging, Tag: "owner", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "dev@company.com"},
		// api-staging-2: owner missing
		{Resource: apiStaging, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "staging"},
		{Resource: apiStaging, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "DEV-001"},
		{Resource: apiStaging, Tag: "owner", Status: types.StatusFailed, Reason: types.ReasonMissing},
		// legacy-data-bucket: owner invalid
		{Resource: bucketLegacy, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "prod"},
		{Resource: bucketLegacy, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "LEGACY-001"},
		{Resource: bucketLegacy, Tag: "owner", Status: types.StatusFailed, Reason: types.ReasonInvalidFormat, Actual: "john", Expected: "^.+@company\\.com$"},
	}

	return result
}
