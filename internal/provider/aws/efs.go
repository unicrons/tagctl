package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// efsAPI is the subset of the EFS API used for discovery and tagging.
type efsAPI interface {
	DescribeFileSystems(ctx context.Context, params *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error)
	TagResource(ctx context.Context, params *efs.TagResourceInput, optFns ...func(*efs.Options)) (*efs.TagResourceOutput, error)
}

// listEFSFileSystems lists all EFS file systems in a region.
func (p *Provider) listEFSFileSystems(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listEFSFileSystemsFrom(ctx, p.getEFSClient(region), region)
}

// listEFSFileSystemsFrom lists EFS file systems using the given client.
// DescribeFileSystems returns tags inline.
func (p *Provider) listEFSFileSystemsFrom(ctx context.Context, client efsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS EFS: Listing file systems in region %s...", region)

	var resources []types.Resource
	paginator := efs.NewDescribeFileSystemsPaginator(client, &efs.DescribeFileSystemsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_efs_file_systems", "", err)
		}
		for _, fs := range output.FileSystems {
			id := aws.ToString(fs.FileSystemId)
			name := aws.ToString(fs.Name)
			if name == "" {
				name = id
			}
			resources = append(resources, types.Resource{
				ID:        id,
				Name:      name,
				ARN:       aws.ToString(fs.FileSystemArn),
				Type:      "aws_efs_file_system",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      efsTagsToMap(fs.Tags),
				CreatedAt: fs.CreationTime,
			})
		}
	}

	log.Debug("AWS EFS: Found %d file systems in %s", len(resources), region)
	return resources, nil
}

// applyEFSTags applies tags to an EFS file system addressed by ARN. The API
// wants the file system ID, which is the last ARN segment.
func (p *Provider) applyEFSTags(ctx context.Context, arn string, tags map[string]string) error {
	tagList := make([]efstypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, efstypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_efs_tags", arn, err)
	}
	client := p.getEFSClient(region)
	_, err = client.TagResource(ctx, &efs.TagResourceInput{
		ResourceId: aws.String(nameFromARN(arn)),
		Tags:       tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_efs_tags", arn, err)
	}

	log.Debug("AWS EFS: Applied %d tags to %s", len(tags), arn)
	return nil
}

// efsTagsToMap converts EFS tags to a map.
func efsTagsToMap(tags []efstypes.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}
