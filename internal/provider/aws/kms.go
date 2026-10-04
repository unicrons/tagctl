package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// kmsAPI is the subset of the KMS API used for discovery and tagging.
type kmsAPI interface {
	ListKeys(ctx context.Context, params *kms.ListKeysInput, optFns ...func(*kms.Options)) (*kms.ListKeysOutput, error)
	DescribeKey(ctx context.Context, params *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
	ListResourceTags(ctx context.Context, params *kms.ListResourceTagsInput, optFns ...func(*kms.Options)) (*kms.ListResourceTagsOutput, error)
	TagResource(ctx context.Context, params *kms.TagResourceInput, optFns ...func(*kms.Options)) (*kms.TagResourceOutput, error)
}

// listKMSKeys lists customer-managed KMS keys in a region.
func (p *Provider) listKMSKeys(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listKMSKeysFrom(ctx, regionalClient(p, region, kms.NewFromConfig), region)
}

// listKMSKeysFrom lists KMS keys using the given client. AWS-managed keys
// (aws/*) cannot be tagged and are left out.
func (p *Provider) listKMSKeysFrom(ctx context.Context, client kmsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS KMS: Listing keys in region %s...", region)

	var keyIDs []string
	paginator := kms.NewListKeysPaginator(client, &kms.ListKeysInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_kms_keys", "", err)
		}
		for _, k := range output.Keys {
			keyIDs = append(keyIDs, aws.ToString(k.KeyId))
		}
	}

	resources := forEachConcurrently(ctx, keyIDs, func(keyID string) []types.Resource {
		desc, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(keyID)})
		if err != nil || desc.KeyMetadata == nil {
			p.skipResource(ctx, "KMS", region, "key "+keyID, err)
			return nil
		}
		meta := desc.KeyMetadata
		if meta.KeyManager != kmstypes.KeyManagerTypeCustomer {
			return nil
		}

		tags, err := p.resourceTags(ctx, region, aws.ToString(meta.Arn), func() (map[string]string, error) {
			return getKMSTags(ctx, client, keyID)
		})
		if err != nil {
			p.skipResource(ctx, "KMS", region, "key "+keyID, err)
			return nil
		}

		name := aws.ToString(meta.Description)
		if name == "" {
			name = keyID
		}
		return one(types.Resource{
			ID:        keyID,
			Name:      name,
			ARN:       aws.ToString(meta.Arn),
			Type:      "aws_kms_key",
			Region:    region,
			Account:   p.accountID,
			Provider:  providerName,
			Tags:      tags,
			CreatedAt: meta.CreationDate,
		})
	})

	log.Debug("AWS KMS: Found %d customer-managed keys in %s", len(resources), region)
	return resources, nil
}

// getKMSTags reads every page of tags for a key.
func getKMSTags(ctx context.Context, client kmsAPI, keyID string) (map[string]string, error) {
	tags := make(map[string]string)
	paginator := kms.NewListResourceTagsPaginator(client, &kms.ListResourceTagsInput{KeyId: aws.String(keyID)})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, tag := range output.Tags {
			if tag.TagKey != nil && tag.TagValue != nil {
				tags[*tag.TagKey] = *tag.TagValue
			}
		}
	}
	return tags, nil
}

// applyKMSTags applies tags to a KMS key addressed by ARN.
func (p *Provider) applyKMSTags(ctx context.Context, arn string, tags map[string]string) error {
	tagList := make([]kmstypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, kmstypes.Tag{TagKey: aws.String(k), TagValue: aws.String(v)})
	}

	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_kms_tags", arn, err)
	}
	client := regionalClient(p, region, kms.NewFromConfig)
	_, err = client.TagResource(ctx, &kms.TagResourceInput{
		KeyId: aws.String(arn),
		Tags:  tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_kms_tags", arn, err)
	}

	log.Debug("AWS KMS: Applied %d tags to %s", len(tags), arn)
	return nil
}
