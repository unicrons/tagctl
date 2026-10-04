package aws

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	taggingtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
)

// bulkTagsPageSize is the maximum GetResources page size.
const bulkTagsPageSize = 100

// bulkTagFilterGroups lists the resource types whose tags are read through
// GetResources, as service[:type] filters in ARN notation. Types whose
// Describe call returns tags inline are left out on purpose. Each group is
// swept by its own paginator so a region with tens of thousands of tagged
// resources takes seconds, not minutes.
var bulkTagFilterGroups = [][]string{
	{"s3", "sns", "sqs", "lambda:function", "logs:log-group"},
	{"rds:db", "rds:cluster", "rds:snapshot", "dynamodb:table", "elasticache:cluster", "es:domain"},
	{"ecr:repository", "elasticbeanstalk:environment"},
	{"elasticloadbalancing:loadbalancer", "elasticloadbalancing:targetgroup", "acm:certificate", "route53:hostedzone", "cloudfront:distribution", "iam:role"},
	{"kinesis:stream", "firehose:deliverystream", "glue:job", "sagemaker:endpoint", "sagemaker:notebook-instance", "states:stateMachine"},
	{"cloudwatch:alarm", "events:rule", "cognito-idp:userpool", "backup:backup-vault", "kms:key"},
	{"acm-pca:certificate-authority", "cloudtrail:trail", "config:config-rule", "ds:directory", "fms:policy", "network-firewall:firewall"},
	{"rolesanywhere:trust-anchor", "waf-regional:webacl", "waf:webacl", "wafv2", "shield:protection", "bedrock:guardrail"},
	{"codeartifact:domain", "codeartifact:repository", "codecommit:repository", "codepipeline", "catalog:portfolio", "wellarchitected:workload"},
	{"athena:workgroup", "dms:rep", "datasync:task", "elasticmapreduce:cluster", "glacier:vaults", "memorydb:cluster"},
	{"mq:broker", "ses:identity", "ses:configuration-set", "storagegateway:gateway", "transfer:server", "appstream:fleet"},
	{"appstream:stack", "ssm:parameter", "ssm-incidents:response-plan", "workspaces:workspace", "codebuild:project"},
}

// taggingAPI is the subset of the Resource Groups Tagging API used to read
// and write tags for any resource type at once.
type taggingAPI interface {
	GetResources(ctx context.Context, params *resourcegroupstaggingapi.GetResourcesInput, optFns ...func(*resourcegroupstaggingapi.Options)) (*resourcegroupstaggingapi.GetResourcesOutput, error)
	TagResources(ctx context.Context, params *resourcegroupstaggingapi.TagResourcesInput, optFns ...func(*resourcegroupstaggingapi.Options)) (*resourcegroupstaggingapi.TagResourcesOutput, error)
}

// tagSource holds the tags of every tagged resource in one region, fetched
// once in the background while discovery runs. GetResources only returns
// resources that have (or had) tags, so a discovered ARN missing from the map
// simply has none.
type tagSource struct {
	done chan struct{}
	tags map[string]map[string]string
	err  error
}

// newTagSource starts fetching in the background.
func newTagSource(fetch func() (map[string]map[string]string, error)) *tagSource {
	t := &tagSource{done: make(chan struct{})}
	go func() {
		defer close(t.done)
		t.tags, t.err = fetch()
	}()
	return t
}

// staticTagSource returns an already-resolved source, for tests.
func staticTagSource(tags map[string]map[string]string) *tagSource {
	t := &tagSource{done: make(chan struct{}), tags: tags}
	close(t.done)
	return t
}

// wait blocks until the fetch finished and reports whether it did before ctx
// was cancelled.
func (t *tagSource) wait(ctx context.Context) bool {
	select {
	case <-t.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// lookup blocks until the fetch finished or ctx is cancelled. ok is false
// when bulk tags are not available (permission denied, API error, cancelled),
// in which case the caller must fall back to the service's own tag call.
func (t *tagSource) lookup(ctx context.Context, arn string) (tags map[string]string, ok bool) {
	if t == nil || !t.wait(ctx) {
		return nil, false
	}
	if t.err != nil {
		return nil, false
	}
	if tags, found := t.tags[arn]; found {
		return tags, true
	}
	return map[string]string{}, true
}

// available reports whether bulk tags were fetched for this source. It is
// false once ctx is cancelled, without waiting for the fetch.
func (t *tagSource) available(ctx context.Context) bool {
	if t == nil || !t.wait(ctx) {
		return false
	}
	return t.err == nil
}

// fetchBulkTags reads the tags of every audited resource type in a region
// through GetResources, one paginator per filter group in parallel.
func fetchBulkTags(ctx context.Context, client taggingAPI, region string) (map[string]map[string]string, error) {
	start := time.Now()
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		tags     = make(map[string]map[string]string)
		firstErr error
	)
	for _, filters := range bulkTagFilterGroups {
		wg.Add(1)
		go func(filters []string) {
			defer wg.Done()
			group, err := fetchBulkTagGroup(ctx, client, filters)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for arn, m := range group {
				tags[arn] = m
			}
		}(filters)
	}
	wg.Wait()
	if firstErr != nil {
		log.Error("AWS Tagging: bulk tag read failed in %s, falling back to per-resource calls: %v", region, firstErr)
		return nil, firstErr
	}
	log.Debug("AWS Tagging: %d tagged resources in %s (%s)", len(tags), region, time.Since(start).Round(time.Millisecond))
	return tags, nil
}

func fetchBulkTagGroup(ctx context.Context, client taggingAPI, filters []string) (map[string]map[string]string, error) {
	tags := make(map[string]map[string]string)
	paginator := resourcegroupstaggingapi.NewGetResourcesPaginator(client, &resourcegroupstaggingapi.GetResourcesInput{
		ResourcesPerPage:    aws.Int32(bulkTagsPageSize),
		ResourceTypeFilters: filters,
	})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, mapping := range output.ResourceTagMappingList {
			arn := aws.ToString(mapping.ResourceARN)
			if arn == "" {
				continue
			}
			tags[arn] = tagsToMap(mapping.Tags,
				func(t taggingtypes.Tag) *string { return t.Key },
				func(t taggingtypes.Tag) *string { return t.Value })
		}
	}
	return tags, nil
}

// startTagSources launches one bulk tag fetch per region of tagSweepRegions.
func (p *Provider) startTagSources(ctx context.Context) {
	regions := p.tagSweepRegions()
	sources := make(map[string]*tagSource, len(regions))
	for _, region := range regions {
		client := regionalClient(p, region, resourcegroupstaggingapi.NewFromConfig)
		sources[region] = newTagSource(func() (map[string]map[string]string, error) {
			return fetchBulkTags(ctx, client, region)
		})
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.tagSources = sources
}

// requireBulkTags reports whether bulk tags are available for region. Services
// with no tag API of their own cannot be audited without them; the caller
// skips the service and this logs and records why, once per service and
// region. A cancelled scan records nothing.
func (p *Provider) requireBulkTags(ctx context.Context, region, label string) bool {
	if p.tagsFor(region).available(ctx) {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	log.Error("AWS %s: skipped in %s: reading its tags needs the tag:GetResources permission", label, region)
	p.skipped.service(label, region)
	return false
}

// skipResource leaves out a resource whose tags cannot be read and records it.
// A resource deleted since it was listed, or a cancelled scan, records nothing.
func (p *Provider) skipResource(ctx context.Context, label, region, resource string, err error) {
	if ctx.Err() != nil {
		return
	}
	if resourceGone(err) {
		log.Debug("AWS %s: %s (%s) was deleted during discovery: %v", label, resource, region, err)
		return
	}
	log.Error("AWS %s: Skipping %s (%s): cannot read tags: %v", label, resource, region, err)
	p.skipped.resource(label)
}

// skipLog counts, per service label, what discovery left out because tags
// could not be read. The zero value is ready to use.
type skipLog struct {
	mu        sync.Mutex
	resources map[string]int
	services  map[string]map[string]bool
}

func (s *skipLog) resource(label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resources == nil {
		s.resources = make(map[string]int)
	}
	s.resources[label]++
}

func (s *skipLog) service(label, region string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.services == nil {
		s.services = make(map[string]map[string]bool)
	}
	if s.services[label] == nil {
		s.services[label] = make(map[string]bool)
	}
	s.services[label][region] = true
}

func (s *skipLog) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources, s.services = nil, nil
}

// errs summarises the skips: one error for resources, one for services.
func (s *skipLog) errs() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	if len(s.resources) > 0 {
		total := 0
		parts := make([]string, 0, len(s.resources))
		for _, label := range slices.Sorted(maps.Keys(s.resources)) {
			total += s.resources[label]
			parts = append(parts, fmt.Sprintf("%s %d", label, s.resources[label]))
		}
		errs = append(errs, fmt.Errorf("skipped %d resource(s) whose tags cannot be read: %s", total, strings.Join(parts, ", ")))
	}
	if len(s.services) > 0 {
		parts := make([]string, 0, len(s.services))
		for _, label := range slices.Sorted(maps.Keys(s.services)) {
			parts = append(parts, fmt.Sprintf("%s in %d region(s)", label, len(s.services[label])))
		}
		errs = append(errs, fmt.Errorf("skipped %d service(s) that need tag:GetResources: %s", len(s.services), strings.Join(parts, ", ")))
	}
	return errs
}

// bulkTags returns the tags of arn from the bulk source; empty when untagged.
// Only valid after requireBulkTags returned true.
func (p *Provider) bulkTags(ctx context.Context, region, arn string) map[string]string {
	tags, _ := p.tagsFor(region).lookup(ctx, arn)
	if tags == nil {
		tags = map[string]string{}
	}
	return tags
}

// tagsFor returns the bulk tag source for a region, or nil when none started.
func (p *Provider) tagsFor(region string) *tagSource {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tagSources[region]
}

// resourceTags resolves a resource's tags from the bulk source when it is
// available and through fallback otherwise. A cancelled ctx is returned as
// the error, without calling fallback.
func (p *Provider) resourceTags(ctx context.Context, region, arn string, fallback func() (map[string]string, error)) (map[string]string, error) {
	if tags, ok := p.tagsFor(region).lookup(ctx, arn); ok {
		return tags, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return fallback()
}

// applyTagsViaTaggingAPI tags any resource by ARN through TagResources.
func (p *Provider) applyTagsViaTaggingAPI(ctx context.Context, arn string, tags map[string]string) error {
	return tagResources(ctx, regionalClient(p, p.taggingRegion(arn), resourcegroupstaggingapi.NewFromConfig), arn, tags)
}

// tagResources adds tags to one ARN through TagResources, which leaves the
// resource's other tags in place.
func tagResources(ctx context.Context, client taggingAPI, arn string, tags map[string]string) error {
	output, err := client.TagResources(ctx, &resourcegroupstaggingapi.TagResourcesInput{
		ResourceARNList: []string{arn},
		Tags:            tags,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "tag_resources", arn, err)
	}
	if failure, failed := output.FailedResourcesMap[arn]; failed {
		return provider.NewProviderError(providerName, "tag_resources", arn,
			&taggingFailure{code: string(failure.ErrorCode), message: aws.ToString(failure.ErrorMessage)})
	}

	log.Debug("AWS Tagging: Applied %d tags to %s", len(tags), arn)
	return nil
}

// taggingRegion returns the region whose Tagging API endpoint tags arn: its
// own, or the partition's global region for ARNs without one (IAM, Route 53,
// CloudFront).
func (p *Provider) taggingRegion(arn string) string {
	if region, err := regionForARN(arn); err == nil {
		return region
	}
	return p.globalRegion()
}

// taggingFailure is a per-resource failure reported inside a successful
// TagResources response.
type taggingFailure struct {
	code, message string
}

func (f *taggingFailure) Error() string {
	return f.code + ": " + f.message
}
