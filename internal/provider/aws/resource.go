package aws

import (
	"errors"
	"time"

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
func (p *Provider) bulkResource(region, resourceType, id, name, arn string, created *time.Time) types.Resource {
	return p.resource(region, resourceType, id, name, arn, p.bulkTags(region, arn), created)
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
