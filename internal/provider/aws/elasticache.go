package aws

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	ectypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// elastiCacheAPI is the subset of the ElastiCache API used for discovery and tagging.
type elastiCacheAPI interface {
	DescribeCacheClusters(ctx context.Context, params *elasticache.DescribeCacheClustersInput, optFns ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error)
	ListTagsForResource(ctx context.Context, params *elasticache.ListTagsForResourceInput, optFns ...func(*elasticache.Options)) (*elasticache.ListTagsForResourceOutput, error)
	AddTagsToResource(ctx context.Context, params *elasticache.AddTagsToResourceInput, optFns ...func(*elasticache.Options)) (*elasticache.AddTagsToResourceOutput, error)
}

// elastiCacheCluster is the subset of a cache cluster needed to build a resource.
type elastiCacheCluster struct {
	id      string
	arn     string
	created *time.Time
}

// listElastiCacheClusters lists all ElastiCache clusters in a region.
func (p *Provider) listElastiCacheClusters(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listElastiCacheClustersFrom(ctx, p.getElastiCacheClient(region), region)
}

// listElastiCacheClustersFrom lists ElastiCache clusters using the given client.
func (p *Provider) listElastiCacheClustersFrom(ctx context.Context, client elastiCacheAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS ElastiCache: Listing cache clusters in region %s...", region)

	var clusters []elastiCacheCluster
	paginator := elasticache.NewDescribeCacheClustersPaginator(client, &elasticache.DescribeCacheClustersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_elasticache_clusters", "", err)
		}
		for _, c := range output.CacheClusters {
			clusters = append(clusters, elastiCacheCluster{
				id:      aws.ToString(c.CacheClusterId),
				arn:     aws.ToString(c.ARN),
				created: c.CacheClusterCreateTime,
			})
		}
	}

	resources := forEachConcurrently(clusters, func(c elastiCacheCluster) []types.Resource {
		tags, err := p.resourceTags(region, c.arn, func() (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &elasticache.ListTagsForResourceInput{ResourceName: aws.String(c.arn)})
			if err != nil {
				return nil, err
			}
			return elastiCacheTagsToMap(output.TagList), nil
		})
		if err != nil {
			log.Error("AWS ElastiCache: Skipping cluster %s (%s): cannot read tags: %v", c.id, region, err)
			return nil
		}
		return one(types.Resource{
			ID:        c.id,
			Name:      c.id,
			ARN:       c.arn,
			Type:      "aws_elasticache_cluster",
			Region:    region,
			Account:   p.accountID,
			Provider:  providerName,
			Tags:      tags,
			CreatedAt: c.created,
		})
	})

	log.Debug("AWS ElastiCache: Found %d cache clusters in %s", len(resources), region)
	return resources, nil
}

// applyElastiCacheTags applies tags to an ElastiCache resource addressed by ARN.
func (p *Provider) applyElastiCacheTags(ctx context.Context, arn string, tags map[string]string) error {
	tagList := make([]ectypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, ectypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := p.getElastiCacheClient(extractRegionFromARN(arn))
	_, err := client.AddTagsToResource(ctx, &elasticache.AddTagsToResourceInput{
		ResourceName: aws.String(arn),
		Tags:         tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_elasticache_tags", arn, err)
	}

	log.Debug("AWS ElastiCache: Applied %d tags to %s", len(tags), arn)
	return nil
}

// elastiCacheTagsToMap converts ElastiCache tags to a map.
func elastiCacheTagsToMap(tags []ectypes.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}
