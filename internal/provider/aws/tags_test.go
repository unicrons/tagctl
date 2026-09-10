package aws

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	taggingtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
)

// mockTaggingClient serves pages for the filter group whose first filter is
// pagesFor; every other group gets an empty page.
type mockTaggingClient struct {
	mu       sync.Mutex
	pagesFor string
	pages    [][]taggingtypes.ResourceTagMapping
	getErr   error
	calls    int
	filters  [][]string
	tagged   map[string]map[string]string
	failing  map[string]string
	nextPage int
}

func (m *mockTaggingClient) GetResources(ctx context.Context, params *resourcegroupstaggingapi.GetResourcesInput, optFns ...func(*resourcegroupstaggingapi.Options)) (*resourcegroupstaggingapi.GetResourcesOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.calls++
	m.filters = append(m.filters, params.ResourceTypeFilters)
	if len(params.ResourceTypeFilters) == 0 || params.ResourceTypeFilters[0] != m.pagesFor {
		return &resourcegroupstaggingapi.GetResourcesOutput{}, nil
	}
	page := m.pages[m.nextPage]
	m.nextPage++
	out := &resourcegroupstaggingapi.GetResourcesOutput{ResourceTagMappingList: page}
	if m.nextPage < len(m.pages) {
		out.PaginationToken = aws.String("next")
	}
	return out, nil
}

func (m *mockTaggingClient) TagResources(ctx context.Context, params *resourcegroupstaggingapi.TagResourcesInput, optFns ...func(*resourcegroupstaggingapi.Options)) (*resourcegroupstaggingapi.TagResourcesOutput, error) {
	if m.tagged == nil {
		m.tagged = map[string]map[string]string{}
	}
	out := &resourcegroupstaggingapi.TagResourcesOutput{FailedResourcesMap: map[string]taggingtypes.FailureInfo{}}
	for _, arn := range params.ResourceARNList {
		if msg, failed := m.failing[arn]; failed {
			out.FailedResourcesMap[arn] = taggingtypes.FailureInfo{ErrorCode: taggingtypes.ErrorCodeInvalidParameterException, ErrorMessage: aws.String(msg)}
			continue
		}
		m.tagged[arn] = params.Tags
	}
	return out, nil
}

func mapping(arn string, kv ...string) taggingtypes.ResourceTagMapping {
	m := taggingtypes.ResourceTagMapping{ResourceARN: aws.String(arn)}
	for i := 0; i+1 < len(kv); i += 2 {
		m.Tags = append(m.Tags, taggingtypes.Tag{Key: aws.String(kv[i]), Value: aws.String(kv[i+1])})
	}
	return m
}

func TestFetchBulkTags_Paginates(t *testing.T) {
	mock := &mockTaggingClient{pagesFor: "s3", pages: [][]taggingtypes.ResourceTagMapping{
		{mapping("arn:a", "environment", envProd)},
		{mapping("arn:b", "owner", "x@example.com"), mapping("", "ignored", "1")},
	}}

	tags, err := fetchBulkTags(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 2 || tags["arn:a"]["environment"] != envProd || tags["arn:b"]["owner"] != "x@example.com" {
		t.Errorf("tags = %v", tags)
	}
	if want := len(bulkTagFilterGroups) + 1; mock.calls != want {
		t.Errorf("GetResources calls = %d, want %d (one per group plus one extra page)", mock.calls, want)
	}
	for _, f := range mock.filters {
		if len(f) == 0 {
			t.Fatal("GetResources called without ResourceTypeFilters: an unfiltered sweep returns every tagged resource in the region")
		}
	}
}

func TestFetchBulkTags_GroupErrorFailsRegion(t *testing.T) {
	mock := &mockTaggingClient{getErr: errors.New("AccessDenied")}
	if _, err := fetchBulkTags(context.Background(), mock, defaultRegion); err == nil {
		t.Fatal("expected error")
	}
}

func TestBulkTagFilterGroups_CoverBulkReadTypes(t *testing.T) {
	seen := map[string]bool{}
	for _, group := range bulkTagFilterGroups {
		for _, f := range group {
			if seen[f] {
				t.Errorf("filter %q listed twice", f)
			}
			seen[f] = true
		}
	}
	for _, want := range []string{"s3", "logs:log-group", "elasticloadbalancing:targetgroup", "route53:hostedzone", "iam:role", "states:stateMachine", "cloudwatch:alarm"} {
		if !seen[want] {
			t.Errorf("filter %q missing", want)
		}
	}
}

func TestTagSource_Lookup(t *testing.T) {
	src := staticTagSource(map[string]map[string]string{"arn:tagged": {"k": "v"}})

	if tags, ok := src.lookup("arn:tagged"); !ok || tags["k"] != "v" {
		t.Errorf("tagged lookup = %v, %v", tags, ok)
	}
	if tags, ok := src.lookup("arn:never-tagged"); !ok || len(tags) != 0 {
		t.Errorf("never-tagged resource must resolve to empty tags with ok=true, got %v, %v", tags, ok)
	}

	failed := newTagSource(func() (map[string]map[string]string, error) { return nil, errors.New("denied") })
	if _, ok := failed.lookup("arn:x"); ok {
		t.Error("failed fetch must report ok=false so callers fall back")
	}
	var none *tagSource
	if _, ok := none.lookup("arn:x"); ok {
		t.Error("nil source must report ok=false")
	}
}

func TestResourceTags_PrefersBulkAndFallsBack(t *testing.T) {
	p := testProvider()
	p.tagSources = map[string]*tagSource{
		defaultRegion: staticTagSource(map[string]map[string]string{"arn:x": {"environment": envProd}}),
	}
	fallbackCalls := 0
	fallback := func() (map[string]string, error) {
		fallbackCalls++
		return map[string]string{"from": "fallback"}, nil
	}

	tags, err := p.resourceTags(defaultRegion, "arn:x", fallback)
	if err != nil || tags["environment"] != envProd || fallbackCalls != 0 {
		t.Errorf("bulk path: tags=%v err=%v fallbackCalls=%d", tags, err, fallbackCalls)
	}

	tags, err = p.resourceTags("eu-west-1", "arn:x", fallback)
	if err != nil || tags["from"] != "fallback" || fallbackCalls != 1 {
		t.Errorf("region without bulk source must use fallback: tags=%v err=%v calls=%d", tags, err, fallbackCalls)
	}
}

// With bulk tags available a lister must not call its own tag API at all.
func TestListDynamoDBTables_UsesBulkTagsWithoutPerResourceCalls(t *testing.T) {
	orders := "arn:aws:dynamodb:us-east-1:123456789012:table/orders"
	p := testProvider()
	p.tagSources = map[string]*tagSource{
		defaultRegion: staticTagSource(map[string]map[string]string{orders: {"environment": envProd}}),
	}
	mock := &mockDynamoDBClient{
		pages:      [][]string{{"orders", "sessions"}},
		tagsErrFor: map[string]bool{orders: true, "arn:aws:dynamodb:us-east-1:123456789012:table/sessions": true},
	}

	resources, err := p.listDynamoDBTablesFrom(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d tables, want 2 (per-resource tag API would have failed)", len(resources))
	}
	for _, r := range resources {
		if r.ID == "orders" && r.Tags["environment"] != envProd {
			t.Errorf("orders tags = %v", r.Tags)
		}
		if r.ID == "sessions" && len(r.Tags) != 0 {
			t.Errorf("sessions tags = %v, want none", r.Tags)
		}
	}
}

func TestTaggingFailureError(t *testing.T) {
	err := &taggingFailure{code: "InvalidParameterException", message: "bad arn"}
	if err.Error() != "InvalidParameterException: bad arn" {
		t.Errorf("Error() = %q", err.Error())
	}
}
