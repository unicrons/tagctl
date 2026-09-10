package aws

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// rdsInstanceInfo holds instance data needed for parallel tag fetching.
type rdsInstanceInfo struct {
	dbInstanceID  string
	dbInstanceARN string
	createTime    *time.Time
}

// listRDSInstances lists all RDS instances in a region using parallel tag fetching.
func (p *Provider) listRDSInstances(ctx context.Context, region string) ([]types.Resource, error) {
	log.Debug("AWS RDS: Listing DB instances in region %s...", region)
	client := p.getRDSClient(region)

	// First, collect all instances from pagination
	var instances []rdsInstanceInfo
	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_rds_instances", "", err)
		}

		for _, instance := range output.DBInstances {
			instances = append(instances, rdsInstanceInfo{
				dbInstanceID:  aws.ToString(instance.DBInstanceIdentifier),
				dbInstanceARN: aws.ToString(instance.DBInstanceArn),
				createTime:    instance.InstanceCreateTime,
			})
		}
	}

	if len(instances) == 0 {
		log.Debug("AWS RDS: Found 0 DB instances in %s", region)
		return nil, nil
	}

	// Fetch tags in parallel using semaphore
	sem := make(chan struct{}, maxConcurrentAPICalls)
	results := make(chan types.Resource, len(instances))
	var wg sync.WaitGroup

	for _, inst := range instances {
		inst := inst // capture loop variable
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Get tags for the instance
			tags, _ := p.resourceTags(region, inst.dbInstanceARN, func() (map[string]string, error) {
				return p.getRDSTags(ctx, region, inst.dbInstanceARN), nil
			})

			resource := types.Resource{
				ID:       inst.dbInstanceID,
				Name:     inst.dbInstanceID,
				ARN:      inst.dbInstanceARN,
				Type:     "aws_db_instance",
				Region:   region,
				Account:  p.accountID,
				Provider: "aws",
				Tags:     tags,
			}

			if inst.createTime != nil {
				resource.CreatedAt = inst.createTime
			}

			log.Debug("AWS RDS: Instance %s in %s has %d tags", inst.dbInstanceID, region, len(tags))
			results <- resource
		}()
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	resources := make([]types.Resource, 0, cap(results))
	for resource := range results {
		resources = append(resources, resource)
	}

	log.Debug("AWS RDS: Found %d DB instances in %s", len(resources), region)
	return resources, nil
}

// getRDSTags gets tags for an RDS resource.
func (p *Provider) getRDSTags(ctx context.Context, region, resourceARN string) map[string]string {
	log.Debug("AWS RDS: Getting tags for %s (region: %s)", resourceARN, region)
	client := p.getRDSClient(region)
	tags := make(map[string]string)

	output, err := client.ListTagsForResource(ctx, &rds.ListTagsForResourceInput{
		ResourceName: aws.String(resourceARN),
	})
	if err != nil {
		log.Debug("AWS RDS: Failed to get tags for %s: %v", resourceARN, err)
		return tags
	}

	for _, tag := range output.TagList {
		if tag.Key != nil && tag.Value != nil {
			tags[*tag.Key] = *tag.Value
			log.Debug("AWS RDS: %s has tag %s=%s", resourceARN, *tag.Key, *tag.Value)
		}
	}

	log.Debug("AWS RDS: %s has %d tags", resourceARN, len(tags))
	return tags
}

// applyRDSTags applies tags to an RDS resource.
func (p *Provider) applyRDSTags(ctx context.Context, resourceARN string, tags map[string]string) error {
	log.Debug("AWS RDS: Applying tags to %s", resourceARN)

	// Extract region from ARN
	region := extractRegionFromARN(resourceARN)
	if region == "" && len(p.regions) > 0 {
		log.Debug("AWS RDS: Could not extract region from ARN, using default %s", p.regions[0])
		region = p.regions[0]
	} else {
		log.Debug("AWS RDS: Extracted region %s from ARN", region)
	}

	client := p.getRDSClient(region)

	rdsTags := make([]rdstypes.Tag, 0, len(tags))
	for k, v := range tags {
		rdsTags = append(rdsTags, rdstypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err := client.AddTagsToResource(ctx, &rds.AddTagsToResourceInput{
		ResourceName: aws.String(resourceARN),
		Tags:         rdsTags,
	})

	if err != nil {
		log.Error("AWS RDS: Failed to apply tags to %s: %v", resourceARN, err)
		return provider.NewProviderError("aws", "add_rds_tags", resourceARN, err)
	}

	log.Debug("AWS RDS: Successfully applied %d tags to %s", len(tags), resourceARN)
	return nil
}

// extractRegionFromARN extracts the region from an AWS ARN.
// ARN format: arn:aws:service:region:account:resource
func extractRegionFromARN(arn string) string {
	parts := splitARN(arn)
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// splitARN splits an ARN into its components.
func splitARN(arn string) []string {
	var parts []string
	current := ""
	for _, c := range arn {
		if c == ':' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
