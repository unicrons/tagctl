package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// eksAPI is the subset of the EKS API used for discovery and tagging.
type eksAPI interface {
	ListClusters(ctx context.Context, params *eks.ListClustersInput, optFns ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	TagResource(ctx context.Context, params *eks.TagResourceInput, optFns ...func(*eks.Options)) (*eks.TagResourceOutput, error)
}

// listEKSClusters lists all EKS clusters in a region.
func (p *Provider) listEKSClusters(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listEKSClustersFrom(ctx, p.getEKSClient(region), region)
}

// listEKSClustersFrom lists EKS clusters using the given client. DescribeCluster
// returns the tags, so it doubles as the tag call.
func (p *Provider) listEKSClustersFrom(ctx context.Context, client eksAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS EKS: Listing clusters in region %s...", region)

	var names []string
	paginator := eks.NewListClustersPaginator(client, &eks.ListClustersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_eks_clusters", "", err)
		}
		names = append(names, output.Clusters...)
	}

	resources := forEachConcurrently(ctx, names, func(name string) []types.Resource {
		output, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
		if err != nil || output.Cluster == nil {
			p.skipResource(ctx, "EKS", region, "cluster "+name, err)
			return nil
		}
		c := output.Cluster
		tags := c.Tags
		if tags == nil {
			tags = map[string]string{}
		}
		return one(types.Resource{
			ID:        name,
			Name:      name,
			ARN:       aws.ToString(c.Arn),
			Type:      "aws_eks_cluster",
			Region:    region,
			Account:   p.accountID,
			Provider:  providerName,
			Tags:      tags,
			CreatedAt: c.CreatedAt,
		})
	})

	log.Debug("AWS EKS: Found %d clusters in %s", len(resources), region)
	return resources, nil
}

// applyEKSTags applies tags to an EKS cluster addressed by ARN.
func (p *Provider) applyEKSTags(ctx context.Context, arn string, tags map[string]string) error {
	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_eks_tags", arn, err)
	}
	client := p.getEKSClient(region)
	_, err = client.TagResource(ctx, &eks.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tags,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_eks_tags", arn, err)
	}

	log.Debug("AWS EKS: Applied %d tags to %s", len(tags), arn)
	return nil
}
