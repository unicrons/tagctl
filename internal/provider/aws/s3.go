package aws

import (
	"context"
	"errors"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// s3API is the subset of the S3 API used for discovery.
type s3API interface {
	ListBuckets(ctx context.Context, params *s3.ListBucketsInput, optFns ...func(*s3.Options)) (*s3.ListBucketsOutput, error)
	GetBucketLocation(ctx context.Context, params *s3.GetBucketLocationInput, optFns ...func(*s3.Options)) (*s3.GetBucketLocationOutput, error)
	GetBucketTagging(ctx context.Context, params *s3.GetBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.GetBucketTaggingOutput, error)
}

// s3ListPageSize is the ListBuckets page size. Sending it also makes S3
// include each bucket's region in the response.
const s3ListPageSize = 1000

// s3BucketResult holds the result of processing a single bucket.
type s3BucketResult struct {
	resource types.Resource
	skip     bool
}

// listS3Buckets lists all S3 buckets and their tags using parallel processing.
func (p *Provider) listS3Buckets(ctx context.Context) ([]types.Resource, error) {
	regional := func(region string) s3API { return p.getS3RegionalClient(region) }
	return p.listS3BucketsFrom(ctx, p.s3Client, regional)
}

// listS3BucketsFrom lists buckets through the global client and reads each
// bucket's tags through the client for its region.
func (p *Provider) listS3BucketsFrom(ctx context.Context, global s3API, regional func(region string) s3API) ([]types.Resource, error) {
	log.Debug("AWS S3: Listing buckets...")
	var buckets []s3types.Bucket
	paginator := s3.NewListBucketsPaginator(global, &s3.ListBucketsInput{}, func(o *s3.ListBucketsPaginatorOptions) {
		o.Limit = s3ListPageSize
	})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_s3_buckets", "", err)
		}
		buckets = append(buckets, output.Buckets...)
	}
	log.Debug("AWS S3: Found %d total buckets", len(buckets))

	if len(buckets) == 0 {
		return nil, nil
	}

	// Use semaphore to limit concurrent API calls
	sem := make(chan struct{}, maxConcurrentAPICalls)
	results := make(chan s3BucketResult, len(buckets))
	var wg sync.WaitGroup

	// Process buckets in parallel
	for _, bucket := range buckets {
		bucket := bucket // capture loop variable
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			bucketName := aws.ToString(bucket.Name)

			region := aws.ToString(bucket.BucketRegion)
			if region == "" {
				region = getBucketRegion(ctx, global, bucketName)
			}
			log.Debug("AWS S3: Bucket %s is in region %s", bucketName, region)

			if !p.isConfiguredRegion(region) {
				log.Debug("AWS S3: Skipping bucket %s (region %s not configured)", bucketName, region)
				results <- s3BucketResult{skip: true}
				return
			}

			tags, err := p.resourceTags(region, "arn:aws:s3:::"+bucketName, func() (map[string]string, error) {
				return getBucketTags(ctx, regional(region), bucketName)
			})
			if err != nil {
				// A bucket whose tags cannot be read is not a finding: skip it
				// rather than report it as untagged.
				log.Error("AWS S3: Skipping bucket %s (%s): cannot read tags: %v", bucketName, region, err)
				results <- s3BucketResult{skip: true}
				return
			}

			resource := types.Resource{
				ID:       bucketName,
				Name:     bucketName,
				Type:     "aws_s3_bucket",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     tags,
				ARN:      "arn:aws:s3:::" + bucketName,
			}

			if bucket.CreationDate != nil {
				resource.CreatedAt = bucket.CreationDate
			}

			results <- s3BucketResult{resource: resource}
		}()
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var resources []types.Resource
	for result := range results {
		if !result.skip {
			resources = append(resources, result.resource)
		}
	}

	log.Debug("AWS S3: Returning %d buckets after filtering", len(resources))
	return resources, nil
}

// isConfiguredRegion reports whether region is scanned. No configured
// regions means every region.
func (p *Provider) isConfiguredRegion(region string) bool {
	if len(p.regions) == 0 {
		return true
	}
	for _, r := range p.regions {
		if r == region {
			return true
		}
	}
	return false
}

// getBucketRegion resolves a bucket's region when ListBuckets did not report it.
func getBucketRegion(ctx context.Context, client s3API, bucketName string) string {
	output, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		// Default to us-east-1 if we can't determine the region
		return defaultRegion
	}

	// Empty location constraint means us-east-1
	if output.LocationConstraint == "" {
		return defaultRegion
	}

	return string(output.LocationConstraint)
}

// getBucketTags reads a bucket's tags. A bucket without a tag set yields an
// empty map; any other failure is returned so the caller can skip the bucket.
func getBucketTags(ctx context.Context, client s3API, bucketName string) (map[string]string, error) {
	tags := make(map[string]string)

	output, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchTagSet" {
			return tags, nil
		}
		return nil, err
	}

	for _, tag := range output.TagSet {
		if tag.Key != nil && tag.Value != nil {
			tags[*tag.Key] = *tag.Value
		}
	}

	log.Debug("AWS S3: Bucket %s has %d tags", bucketName, len(tags))
	return tags, nil
}

// applyS3Tags applies tags to an S3 bucket.
func (p *Provider) applyS3Tags(ctx context.Context, bucketName string, tags map[string]string) error {
	region := getBucketRegion(ctx, p.s3Client, bucketName)
	log.Debug("AWS S3: Applying tags to bucket %s (region: %s)", bucketName, region)

	regionalClient := p.getS3RegionalClient(region)

	// PutBucketTagging replaces the whole tag set, so the merge must start
	// from the real existing tags or it would wipe them.
	existingTags, err := getBucketTags(ctx, regionalClient, bucketName)
	if err != nil {
		return provider.NewProviderError(providerName, "get_bucket_tagging", bucketName, err)
	}

	// Merge tags (new tags override existing)
	for k, v := range tags {
		existingTags[k] = v
	}

	// Convert to S3 tag set
	tagSet := make([]s3types.Tag, 0, len(existingTags))
	for k, v := range existingTags {
		tagSet = append(tagSet, s3types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err = regionalClient.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: aws.String(bucketName),
		Tagging: &s3types.Tagging{
			TagSet: tagSet,
		},
	})

	if err != nil {
		return provider.NewProviderError(providerName, "put_bucket_tagging", bucketName, err)
	}

	log.Debug("AWS S3: Successfully applied %d tags to bucket %s", len(tagSet), bucketName)
	return nil
}
