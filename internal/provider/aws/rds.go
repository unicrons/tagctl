package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// rdsInstanceAPI is the subset of the RDS API used to discover DB instances.
type rdsInstanceAPI interface {
	DescribeDBInstances(ctx context.Context, params *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
	ListTagsForResource(ctx context.Context, params *rds.ListTagsForResourceInput, optFns ...func(*rds.Options)) (*rds.ListTagsForResourceOutput, error)
}

// listRDSInstances lists all RDS instances in a region.
func (p *Provider) listRDSInstances(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listRDSInstancesFrom(ctx, p.getRDSClient(region), region)
}

// listRDSInstancesFrom lists RDS instances using the given client.
func (p *Provider) listRDSInstancesFrom(ctx context.Context, client rdsInstanceAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS RDS: Listing DB instances in region %s...", region)

	var instances []rdstypes.DBInstance
	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_rds_instances", "", err)
		}
		instances = append(instances, output.DBInstances...)
	}

	resources := forEachConcurrently(instances, func(inst rdstypes.DBInstance) []types.Resource {
		id, arn := aws.ToString(inst.DBInstanceIdentifier), aws.ToString(inst.DBInstanceArn)
		tags, err := p.resourceTags(ctx, region, arn, func() (map[string]string, error) {
			return getRDSTags(ctx, client, arn)
		})
		if err != nil {
			p.skipResource(ctx, "RDS", region, "DB instance "+id, err)
			return nil
		}
		return one(p.resource(region, "aws_db_instance", id, id, arn, tags, inst.InstanceCreateTime))
	})

	log.Debug("AWS RDS: Found %d DB instances in %s", len(resources), region)
	return resources, nil
}

// getRDSTags reads the tags of an RDS resource.
func getRDSTags(ctx context.Context, client rdsInstanceAPI, arn string) (map[string]string, error) {
	output, err := client.ListTagsForResource(ctx, &rds.ListTagsForResourceInput{
		ResourceName: aws.String(arn),
	})
	if err != nil {
		return nil, err
	}
	return rdsTagListToMap(output.TagList), nil
}

// applyRDSTags applies tags to an RDS resource.
func (p *Provider) applyRDSTags(ctx context.Context, resourceARN string, tags map[string]string) error {
	log.Debug("AWS RDS: Applying tags to %s", resourceARN)

	region, err := regionForARN(resourceARN)
	if err != nil {
		return provider.NewProviderError(providerName, "add_rds_tags", resourceARN, err)
	}

	client := p.getRDSClient(region)

	rdsTags := make([]rdstypes.Tag, 0, len(tags))
	for k, v := range tags {
		rdsTags = append(rdsTags, rdstypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err = client.AddTagsToResource(ctx, &rds.AddTagsToResourceInput{
		ResourceName: aws.String(resourceARN),
		Tags:         rdsTags,
	})

	if err != nil {
		log.Error("AWS RDS: Failed to apply tags to %s: %v", resourceARN, err)
		return provider.NewProviderError(providerName, "add_rds_tags", resourceARN, err)
	}

	log.Debug("AWS RDS: Successfully applied %d tags to %s", len(tags), resourceARN)
	return nil
}

// regionForARN returns the region segment of an ARN. An ARN that does not
// parse or has no region is an error, never an empty region.
func regionForARN(resourceARN string) (string, error) {
	parsed, err := arn.Parse(resourceARN)
	if err != nil {
		return "", fmt.Errorf("region of %q: %w", resourceARN, err)
	}
	if parsed.Region == "" {
		return "", fmt.Errorf("ARN %q has no region", resourceARN)
	}
	return parsed.Region, nil
}
