package aws

import (
	"context"
	"errors"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	asgtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	"github.com/aws/aws-sdk-go-v2/service/lightsail"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

const (
	bucketType       = "aws_s3_bucket"
	lightsailService = "lightsail"
)

type taggingUntagAPI interface {
	UntagResources(ctx context.Context, params *resourcegroupstaggingapi.UntagResourcesInput, optFns ...func(*resourcegroupstaggingapi.Options)) (*resourcegroupstaggingapi.UntagResourcesOutput, error)
}

type ec2UntagAPI interface {
	DeleteTags(ctx context.Context, params *ec2.DeleteTagsInput, optFns ...func(*ec2.Options)) (*ec2.DeleteTagsOutput, error)
}

type s3UntagAPI interface {
	s3TagReader
	PutBucketTagging(ctx context.Context, params *s3.PutBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error)
	DeleteBucketTagging(ctx context.Context, params *s3.DeleteBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketTaggingOutput, error)
}

type autoScalingUntagAPI interface {
	DeleteTags(ctx context.Context, params *autoscaling.DeleteTagsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DeleteTagsOutput, error)
}

type lightsailUntagAPI interface {
	UntagResource(ctx context.Context, params *lightsail.UntagResourceInput, optFns ...func(*lightsail.Options)) (*lightsail.UntagResourceOutput, error)
}

type globalAcceleratorUntagAPI interface {
	UntagResource(ctx context.Context, params *globalaccelerator.UntagResourceInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.UntagResourceOutput, error)
}

// untagRoute is the API a resource's tags are removed through.
type untagRoute int

const (
	untagViaTaggingAPI untagRoute = iota
	untagViaEC2
	untagViaS3
	untagViaAutoScaling
	untagViaLightsail
	untagViaGlobalAccelerator
)

// ownUntagAPI lists the services the Tagging API does not cover, or that
// address the resource by a bare ID.
var ownUntagAPI = map[string]untagRoute{
	"ec2":            untagViaEC2,
	"autoscaling":    untagViaAutoScaling,
	lightsailService: untagViaLightsail,
}

// untagRouteFor picks the API and region to remove a resource's tags with.
// Everything untags by ARN through tag:UntagResources except EC2, which takes
// a bare ID in the resource's region, the services in ownUntagAPI and a bucket
// that has no ARN. An empty region for a bucket means it has to be looked up;
// any other ARN without a region is untagged in globalRegion.
func untagRouteFor(resource types.Resource, globalRegion string) (untagRoute, string, error) {
	if resource.ARN == "" {
		if resource.Type == bucketType {
			return untagViaS3, resource.Region, nil
		}
		return 0, "", errors.New("the resource has no ARN to address it by")
	}
	parsed, err := arn.Parse(resource.ARN)
	if err != nil {
		return 0, "", err
	}
	region := parsed.Region
	if region == "" && resource.Region != regionGlobal {
		region = resource.Region
	}

	switch parsed.Service {
	case "s3":
		return untagViaTaggingAPI, region, nil
	case "globalaccelerator":
		return untagViaGlobalAccelerator, globalAcceleratorRegion, nil
	}
	if route, own := ownUntagAPI[parsed.Service]; own {
		if region == "" {
			return 0, "", errors.New("the resource has no region")
		}
		return route, region, nil
	}
	if region == "" {
		region = globalRegion
	}
	return untagViaTaggingAPI, region, nil
}

// RemoveTags deletes tag keys from an AWS resource.
func (p *Provider) RemoveTags(ctx context.Context, resource types.Resource, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	route, region, err := untagRouteFor(resource, p.globalRegion())
	if err != nil {
		return provider.NewProviderError(providerName, "remove_tags", resource.Identity(), err)
	}

	resourceARN := resource.ARN
	if region == "" {
		region = p.getBucketRegion(ctx, regionalClient(p, p.cfg.Region, s3.NewFromConfig), resource.ID)
	}
	switch route {
	case untagViaS3:
		return removeS3Tags(ctx, regionalClient(p, region, s3.NewFromConfig), resource.ID, keys)
	case untagViaEC2:
		return removeEC2Tags(ctx, regionalClient(p, region, ec2.NewFromConfig), nameFromARN(resourceARN), keys)
	case untagViaAutoScaling:
		return removeAutoScalingTags(ctx, regionalClient(p, region, autoscaling.NewFromConfig), resourceARN, keys)
	case untagViaLightsail:
		return removeLightsailTags(ctx, regionalClient(p, region, lightsail.NewFromConfig), resourceARN, keys)
	case untagViaGlobalAccelerator:
		return removeGlobalAcceleratorTags(ctx, regionalClient(p, region, globalaccelerator.NewFromConfig), resourceARN, keys)
	default:
		return removeTagsViaTaggingAPI(ctx, regionalClient(p, region, resourcegroupstaggingapi.NewFromConfig), resourceARN, keys)
	}
}

func removeTagsViaTaggingAPI(ctx context.Context, client taggingUntagAPI, resourceARN string, keys []string) error {
	output, err := client.UntagResources(ctx, &resourcegroupstaggingapi.UntagResourcesInput{
		ResourceARNList: []string{resourceARN},
		TagKeys:         keys,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "untag_resources", resourceARN, err)
	}
	if failure, failed := output.FailedResourcesMap[resourceARN]; failed {
		return provider.NewProviderError(providerName, "untag_resources", resourceARN,
			&taggingFailure{code: string(failure.ErrorCode), message: aws.ToString(failure.ErrorMessage)})
	}
	log.Debug("AWS Tagging: Removed %d tags from %s", len(keys), resourceARN)
	return nil
}

func removeEC2Tags(ctx context.Context, client ec2UntagAPI, id string, keys []string) error {
	// A tag without Value is deleted whatever its value; an empty Value
	// would only delete a tag whose value is empty.
	tags := make([]ec2types.Tag, 0, len(keys))
	for _, key := range keys {
		tags = append(tags, ec2types.Tag{Key: aws.String(key)})
	}
	if _, err := client.DeleteTags(ctx, &ec2.DeleteTagsInput{Resources: []string{id}, Tags: tags}); err != nil {
		return provider.NewProviderError(providerName, "delete_tags", id, err)
	}
	log.Debug("AWS EC2: Removed %d tags from %s", len(keys), id)
	return nil
}

// removeS3Tags rewrites the bucket tag set without keys: S3 has no call that
// deletes single tags.
func removeS3Tags(ctx context.Context, client s3UntagAPI, bucket string, keys []string) error {
	tags, err := getBucketTags(ctx, client, bucket)
	if err != nil {
		return provider.NewProviderError(providerName, "get_bucket_tagging", bucket, err)
	}

	tagSet := make([]s3types.Tag, 0, len(tags))
	for key, value := range tags {
		if !slices.Contains(keys, key) {
			tagSet = append(tagSet, s3types.Tag{Key: aws.String(key), Value: aws.String(value)})
		}
	}
	switch {
	case len(tagSet) == len(tags):
		return nil
	case len(tagSet) == 0:
		_, err = client.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{Bucket: aws.String(bucket)})
	default:
		_, err = client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
			Bucket:  aws.String(bucket),
			Tagging: &s3types.Tagging{TagSet: tagSet},
		})
	}
	if err != nil {
		return provider.NewProviderError(providerName, "put_bucket_tagging", bucket, err)
	}
	log.Debug("AWS S3: Removed %d tags from bucket %s", len(tags)-len(tagSet), bucket)
	return nil
}

func removeAutoScalingTags(ctx context.Context, client autoScalingUntagAPI, resourceARN string, keys []string) error {
	name := asgNameFromARN(resourceARN)
	tags := make([]asgtypes.Tag, 0, len(keys))
	for _, key := range keys {
		tags = append(tags, asgtypes.Tag{
			ResourceId:   aws.String(name),
			ResourceType: aws.String("auto-scaling-group"),
			Key:          aws.String(key),
		})
	}
	if _, err := client.DeleteTags(ctx, &autoscaling.DeleteTagsInput{Tags: tags}); err != nil {
		return provider.NewProviderError(providerName, "remove_autoscaling_tags", resourceARN, err)
	}
	log.Debug("AWS AutoScaling: Removed %d tags from %s", len(keys), name)
	return nil
}

func removeLightsailTags(ctx context.Context, client lightsailUntagAPI, resourceARN string, keys []string) error {
	_, err := client.UntagResource(ctx, &lightsail.UntagResourceInput{
		ResourceName: aws.String(nameFromARN(resourceARN)),
		ResourceArn:  aws.String(resourceARN),
		TagKeys:      keys,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "remove_lightsail_tags", resourceARN, err)
	}
	log.Debug("AWS Lightsail: Removed %d tags from %s", len(keys), resourceARN)
	return nil
}

func removeGlobalAcceleratorTags(ctx context.Context, client globalAcceleratorUntagAPI, resourceARN string, keys []string) error {
	_, err := client.UntagResource(ctx, &globalaccelerator.UntagResourceInput{ResourceArn: aws.String(resourceARN), TagKeys: keys})
	if err != nil {
		return provider.NewProviderError(providerName, "remove_global_accelerator_tags", resourceARN, err)
	}
	log.Debug("AWS Global Accelerator: Removed %d tags from %s", len(keys), resourceARN)
	return nil
}
