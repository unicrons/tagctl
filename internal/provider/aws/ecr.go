package aws

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// ecrAPI is the subset of the ECR API used for discovery and tagging.
type ecrAPI interface {
	DescribeRepositories(ctx context.Context, params *ecr.DescribeRepositoriesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error)
	ListTagsForResource(ctx context.Context, params *ecr.ListTagsForResourceInput, optFns ...func(*ecr.Options)) (*ecr.ListTagsForResourceOutput, error)
	TagResource(ctx context.Context, params *ecr.TagResourceInput, optFns ...func(*ecr.Options)) (*ecr.TagResourceOutput, error)
}

// ecrRepository is the subset of a repository needed to build a resource.
type ecrRepository struct {
	name    string
	arn     string
	created *time.Time
}

// listECRRepositories lists all ECR repositories in a region.
func (p *Provider) listECRRepositories(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listECRRepositoriesFrom(ctx, p.getECRClient(region), region)
}

// listECRRepositoriesFrom lists ECR repositories using the given client.
func (p *Provider) listECRRepositoriesFrom(ctx context.Context, client ecrAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS ECR: Listing repositories in region %s...", region)

	var repos []ecrRepository
	paginator := ecr.NewDescribeRepositoriesPaginator(client, &ecr.DescribeRepositoriesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ecr_repositories", "", err)
		}
		for _, r := range output.Repositories {
			repos = append(repos, ecrRepository{
				name:    aws.ToString(r.RepositoryName),
				arn:     aws.ToString(r.RepositoryArn),
				created: r.CreatedAt,
			})
		}
	}

	resources := forEachConcurrently(ctx, repos, func(r ecrRepository) []types.Resource {
		tags, err := p.resourceTags(ctx, region, r.arn, func() (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &ecr.ListTagsForResourceInput{ResourceArn: aws.String(r.arn)})
			if err != nil {
				return nil, err
			}
			return ecrTagsToMap(output.Tags), nil
		})
		if err != nil {
			p.skipResource(ctx, "ECR", region, "repository "+r.name, err)
			return nil
		}
		return one(types.Resource{
			ID:        r.name,
			Name:      r.name,
			ARN:       r.arn,
			Type:      "aws_ecr_repository",
			Region:    region,
			Account:   p.accountID,
			Provider:  providerName,
			Tags:      tags,
			CreatedAt: r.created,
		})
	})

	log.Debug("AWS ECR: Found %d repositories in %s", len(resources), region)
	return resources, nil
}

// applyECRTags applies tags to an ECR repository addressed by ARN.
func (p *Provider) applyECRTags(ctx context.Context, arn string, tags map[string]string) error {
	tagList := make([]ecrtypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, ecrtypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_ecr_tags", arn, err)
	}
	client := p.getECRClient(region)
	_, err = client.TagResource(ctx, &ecr.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_ecr_tags", arn, err)
	}

	log.Debug("AWS ECR: Applied %d tags to %s", len(tags), arn)
	return nil
}

// ecrTagsToMap converts ECR tags to a map.
func ecrTagsToMap(tags []ecrtypes.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}
