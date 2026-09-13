package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// ECS describe calls accept at most this many ARNs per request.
const (
	ecsClustersPerDescribe = 100
	ecsServicesPerDescribe = 10
)

// ecsAPI is the subset of the ECS API used for discovery and tagging.
type ecsAPI interface {
	ListClusters(ctx context.Context, params *ecs.ListClustersInput, optFns ...func(*ecs.Options)) (*ecs.ListClustersOutput, error)
	DescribeClusters(ctx context.Context, params *ecs.DescribeClustersInput, optFns ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error)
	ListServices(ctx context.Context, params *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error)
	DescribeServices(ctx context.Context, params *ecs.DescribeServicesInput, optFns ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
	TagResource(ctx context.Context, params *ecs.TagResourceInput, optFns ...func(*ecs.Options)) (*ecs.TagResourceOutput, error)
}

// listECSResources lists ECS clusters and their services in a region.
func (p *Provider) listECSResources(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listECSResourcesFrom(ctx, p.getECSClient(region), region)
}

// listECSResourcesFrom lists ECS clusters and services using the given client.
// Describe calls return tags inline, so no per-resource tag call is needed.
func (p *Provider) listECSResourcesFrom(ctx context.Context, client ecsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS ECS: Listing clusters in region %s...", region)

	var clusterARNs []string
	paginator := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ecs_clusters", "", err)
		}
		clusterARNs = append(clusterARNs, output.ClusterArns...)
	}

	var resources []types.Resource
	for _, batch := range chunk(clusterARNs, ecsClustersPerDescribe) {
		output, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: batch,
			Include:  []ecstypes.ClusterField{ecstypes.ClusterFieldTags},
		})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "describe_ecs_clusters", "", err)
		}
		for _, c := range output.Clusters {
			resources = append(resources, types.Resource{
				ID:       aws.ToString(c.ClusterName),
				Name:     aws.ToString(c.ClusterName),
				ARN:      aws.ToString(c.ClusterArn),
				Type:     "aws_ecs_cluster",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     ecsTagsToMap(c.Tags),
			})
		}
	}
	clusterCount := len(resources)

	resources = append(resources, forEachConcurrently(clusterARNs, func(clusterARN string) []types.Resource {
		svcs, err := p.listECSServices(ctx, client, region, clusterARN)
		if err != nil {
			p.skipResource(ctx, "ECS", region, "services of cluster "+nameFromARN(clusterARN), err)
			return nil
		}
		return svcs
	})...)

	log.Debug("AWS ECS: Found %d clusters and %d services in %s", clusterCount, len(resources)-clusterCount, region)
	return resources, nil
}

// listECSServices lists and describes the services of one cluster.
func (p *Provider) listECSServices(ctx context.Context, client ecsAPI, region, clusterARN string) ([]types.Resource, error) {
	var serviceARNs []string
	paginator := ecs.NewListServicesPaginator(client, &ecs.ListServicesInput{Cluster: aws.String(clusterARN)})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		serviceARNs = append(serviceARNs, output.ServiceArns...)
	}

	clusterName := nameFromARN(clusterARN)
	resources := forEachConcurrently(chunk(serviceARNs, ecsServicesPerDescribe), func(batch []string) []types.Resource {
		output, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  aws.String(clusterARN),
			Services: batch,
			Include:  []ecstypes.ServiceField{ecstypes.ServiceFieldTags},
		})
		if err != nil {
			for _, arn := range batch {
				p.skipResource(ctx, "ECS", region, "service "+clusterName+"/"+nameFromARN(arn), err)
			}
			return nil
		}
		described := make([]types.Resource, 0, len(output.Services))
		for _, s := range output.Services {
			name := aws.ToString(s.ServiceName)
			described = append(described, types.Resource{
				ID:        clusterName + "/" + name,
				Name:      name,
				ARN:       aws.ToString(s.ServiceArn),
				Type:      "aws_ecs_service",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      ecsTagsToMap(s.Tags),
				CreatedAt: s.CreatedAt,
			})
		}
		return described
	})
	return resources, nil
}

// applyECSTags applies tags to an ECS cluster or service addressed by ARN.
func (p *Provider) applyECSTags(ctx context.Context, arn string, tags map[string]string) error {
	tagList := make([]ecstypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, ecstypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := p.getECSClient(extractRegionFromARN(arn))
	_, err := client.TagResource(ctx, &ecs.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_ecs_tags", arn, err)
	}

	log.Debug("AWS ECS: Applied %d tags to %s", len(tags), arn)
	return nil
}

// ecsTagsToMap converts ECS tags to a map.
func ecsTagsToMap(tags []ecstypes.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

// chunk splits items into slices of at most size elements.
func chunk(items []string, size int) [][]string {
	var batches [][]string
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[start:end])
	}
	return batches
}
