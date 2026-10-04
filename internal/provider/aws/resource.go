package aws

import (
	"context"
	"errors"
	"time"

	logstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	datapipelinetypes "github.com/aws/aws-sdk-go-v2/service/datapipeline/types"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	gatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"
	kinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"

	"github.com/unicrons/tagctl/internal/types"
)

// resource builds an AWS resource with tags already known.
func (p *Provider) resource(region, resourceType, id, name, arn string, tags map[string]string, created *time.Time) types.Resource {
	if tags == nil {
		tags = map[string]string{}
	}
	return types.Resource{
		ID: id, Name: name, ARN: arn, Type: resourceType,
		Region: region, Account: p.accountID, Provider: providerName,
		Tags: tags, CreatedAt: created,
	}
}

// bulkResource builds an AWS resource whose tags come from the region's bulk
// source. Only valid after requireBulkTags returned true.
func (p *Provider) bulkResource(ctx context.Context, region, resourceType, id, name, arn string, created *time.Time) types.Resource {
	return p.resource(region, resourceType, id, name, arn, p.bulkTags(ctx, region, arn), created)
}

// tagsToMap converts an SDK tag slice into a map using the given accessors.
func tagsToMap[T any](tags []T, key, value func(T) *string) map[string]string {
	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		k, v := key(tag), value(tag)
		if k != nil && v != nil {
			result[*k] = *v
		}
	}
	return result
}

// notSubscribedCodes are API error codes that mean the service is not set up
// in the account or region (no subscription, not initialised, not the
// delegated administrator), not that discovery failed.
var notSubscribedCodes = map[string]bool{
	"UninitializedAccountException": true, // DRS
	"ResourceNotFoundException":     true, // Shield without a subscription
	"InvalidOperationException":     true, // FMS outside the admin account
	"SubscriptionRequiredException": true,
	"OptInRequired":                 true,
}

// notSubscribed reports whether err means the service is unavailable here.
func notSubscribed(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && notSubscribedCodes[apiErr.ErrorCode()]
}

// resourceGone reports whether a tag read failed because the resource was
// deleted after it was listed, from the not-found errors those calls model.
func resourceGone(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchBucket" {
		return true // GetBucketTagging models no error types
	}
	return errorAs[*sqstypes.QueueDoesNotExist](err) ||
		errorAs[*lambdatypes.ResourceNotFoundException](err) ||
		errorAs[*rdstypes.DBInstanceNotFoundFault](err) ||
		errorAs[*snstypes.ResourceNotFoundException](err) ||
		errorAs[*elbv2types.LoadBalancerNotFoundException](err) ||
		errorAs[*elbv2types.TargetGroupNotFoundException](err) ||
		errorAs[*dynamodbtypes.ResourceNotFoundException](err) ||
		errorAs[*ecrtypes.RepositoryNotFoundException](err) ||
		errorAs[*elasticachetypes.CacheClusterNotFoundFault](err) ||
		errorAs[*kinesistypes.ResourceNotFoundException](err) ||
		errorAs[*kmstypes.NotFoundException](err) ||
		errorAs[*logstypes.ResourceNotFoundException](err) ||
		errorAs[*ekstypes.ResourceNotFoundException](err) ||
		errorAs[*ecstypes.ClusterNotFoundException](err) ||
		errorAs[*gatypes.AcceleratorNotFoundException](err) ||
		errorAs[*datapipelinetypes.PipelineNotFoundException](err) ||
		errorAs[*datapipelinetypes.PipelineDeletedException](err)
}

// errorAs reports whether err wraps an error of type T.
func errorAs[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}
