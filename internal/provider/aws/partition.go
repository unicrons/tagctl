package aws

import (
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
)

// partitionAWS is the commercial partition, also assumed by a Provider built
// without New, which never read the caller identity.
const partitionAWS = "aws"

// partitionInfo is what tagctl needs to address one AWS partition.
type partitionInfo struct {
	// globalRegion is where the global services are called and where the
	// bulk sweep returns their tags.
	globalRegion string
	dnsSuffix    string
}

// partitions copies implicitGlobalRegion and dnsSuffix from the SDK's
// internal/endpoints/awsrulesfn/partitions.json.
var partitions = map[string]partitionInfo{
	"aws":        {"us-east-1", "amazonaws.com"},
	"aws-cn":     {"cn-northwest-1", "amazonaws.com.cn"},
	"aws-eusc":   {"eusc-de-east-1", "amazonaws.eu"},
	"aws-iso":    {"us-iso-east-1", "c2s.ic.gov"},
	"aws-iso-b":  {"us-isob-east-1", "sc2s.sgov.gov"},
	"aws-iso-e":  {"eu-isoe-west-1", "cloud.adc-e.uk"},
	"aws-iso-f":  {"us-isof-south-1", "csp.hci.ic.gov"},
	"aws-us-gov": {"us-gov-west-1", "amazonaws.com"},
}

// partitionOf returns the partition of an ARN.
func partitionOf(identityARN string) (string, error) {
	parsed, err := arn.Parse(identityARN)
	if err != nil {
		return "", fmt.Errorf("caller identity %q: %w", identityARN, err)
	}
	if parsed.Partition == "" {
		return "", fmt.Errorf("caller identity %q has no partition", identityARN)
	}
	return parsed.Partition, nil
}

// partitionID returns the partition of the account's caller identity.
func (p *Provider) partitionID() string {
	if p.partition == "" {
		return partitionAWS
	}
	return p.partition
}

// globalRegion returns the region of the account's partition that serves the
// global services. A partition missing from the table falls back to the first
// configured region, which at least stays inside the partition.
func (p *Provider) globalRegion() string {
	if info, ok := partitions[p.partitionID()]; ok {
		return info.globalRegion
	}
	if len(p.regions) > 0 {
		return p.regions[0]
	}
	return defaultRegion
}

// buildARN builds an ARN in the account's partition. account is empty for
// the services whose ARNs omit it (S3, Route 53, API Gateway).
func (p *Provider) buildARN(service, region, account, resource string) string {
	return arn.ARN{Partition: p.partitionID(), Service: service, Region: region, AccountID: account, Resource: resource}.String()
}

// tagSweepRegions returns the regions swept for bulk tags: the configured ones
// plus the global region, whose sweep holds the global services' tags.
func (p *Provider) tagSweepRegions() []string {
	global := p.globalRegion()
	if p.isConfiguredRegion(global) {
		return p.regions
	}
	return append(slices.Clone(p.regions), global)
}

// dnsSuffix returns the endpoint domain of a partition.
func dnsSuffix(partition string) string {
	if info, ok := partitions[partition]; ok {
		return info.dnsSuffix
	}
	return partitions[partitionAWS].dnsSuffix
}

// ec2ARN builds the ARN of an EC2 resource.
func (p *Provider) ec2ARN(region, resourceType, resourceID string) string {
	return p.buildARN("ec2", region, p.accountID, resourceType+"/"+resourceID)
}
