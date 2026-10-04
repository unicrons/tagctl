package aws

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/unicrons/tagctl/internal/types"
)

// mockS3Client implements s3API for testing. Like S3, it reports each
// bucket's region only when the request carries a parameter, and it pages
// the bucket list when pageSize is set.
type mockS3Client struct {
	buckets         []s3types.Bucket
	bucketLocations map[string]s3types.BucketLocationConstraint
	bucketTags      map[string][]s3types.Tag
	taggingErrs     map[string]error
	listBucketsErr  error
	getLocationErr  error
	pageSize        int
	omitRegion      bool

	mu            sync.Mutex
	listCalls     int
	locationCalls []string
	taggingCalls  []string
}

func (m *mockS3Client) ListBuckets(ctx context.Context, input *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls++
	if m.listBucketsErr != nil {
		return nil, m.listBucketsErr
	}

	start := 0
	if input.ContinuationToken != nil {
		start, _ = strconv.Atoi(*input.ContinuationToken)
	}
	end := len(m.buckets)
	if m.pageSize > 0 && start+m.pageSize < end {
		end = start + m.pageSize
	}

	page := make([]s3types.Bucket, 0, end-start)
	for _, b := range m.buckets[start:end] {
		if input.MaxBuckets != nil && !m.omitRegion {
			region := string(m.bucketLocations[aws.ToString(b.Name)])
			if region == "" {
				region = defaultRegion
			}
			b.BucketRegion = aws.String(region)
		}
		page = append(page, b)
	}

	out := &s3.ListBucketsOutput{Buckets: page}
	if end < len(m.buckets) {
		out.ContinuationToken = aws.String(strconv.Itoa(end))
	}
	return out, nil
}

func (m *mockS3Client) GetBucketLocation(ctx context.Context, input *s3.GetBucketLocationInput, opts ...func(*s3.Options)) (*s3.GetBucketLocationOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.locationCalls = append(m.locationCalls, aws.ToString(input.Bucket))
	if m.getLocationErr != nil {
		return nil, m.getLocationErr
	}
	location := m.bucketLocations[aws.ToString(input.Bucket)]
	return &s3.GetBucketLocationOutput{LocationConstraint: location}, nil
}

func (m *mockS3Client) GetBucketTagging(ctx context.Context, input *s3.GetBucketTaggingInput, opts ...func(*s3.Options)) (*s3.GetBucketTaggingOutput, error) {
	name := aws.ToString(input.Bucket)
	m.mu.Lock()
	m.taggingCalls = append(m.taggingCalls, name)
	m.mu.Unlock()
	if err := m.taggingErrs[name]; err != nil {
		return nil, err
	}
	return &s3.GetBucketTaggingOutput{TagSet: m.bucketTags[name]}, nil
}

func listWithMock(t *testing.T, mock *mockS3Client, regions []string) ([]types.Resource, error) {
	t.Helper()
	p := &Provider{accountID: "123456789012", regions: regions}
	return p.listS3BucketsFrom(context.Background(), mock, func(string) s3API { return mock })
}

func noSuchTagSet() error {
	return &smithy.GenericAPIError{Code: "NoSuchTagSet", Message: "The TagSet does not exist"}
}

func bucketNames(resources []types.Resource) []string {
	names := make([]string, 0, len(resources))
	for _, r := range resources {
		names = append(names, r.ID)
	}
	sort.Strings(names)
	return names
}

func threeRegionMock() *mockS3Client {
	return &mockS3Client{
		buckets: []s3types.Bucket{
			{Name: aws.String("bucket-us-east-1")},
			{Name: aws.String("bucket-eu-west-1")},
			{Name: aws.String("bucket-ap-south-1")},
		},
		bucketLocations: map[string]s3types.BucketLocationConstraint{
			"bucket-us-east-1":  "",
			"bucket-eu-west-1":  s3types.BucketLocationConstraintEuWest1,
			"bucket-ap-south-1": s3types.BucketLocationConstraintApSouth1,
		},
		bucketTags: map[string][]s3types.Tag{
			"bucket-eu-west-1": {{Key: aws.String("environment"), Value: aws.String("prod")}},
		},
	}
}

func TestListS3Buckets_NoRegionsConfiguredReturnsEveryBucket(t *testing.T) {
	mock := threeRegionMock()

	resources, err := listWithMock(t, mock, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"bucket-ap-south-1", "bucket-eu-west-1", "bucket-us-east-1"}
	if got := bucketNames(resources); !equalStrings(got, want) {
		t.Errorf("buckets = %v, want %v", got, want)
	}

	regions := map[string]string{}
	for _, r := range resources {
		regions[r.ID] = r.Region
		if r.ARN != "arn:aws:s3:::"+r.ID {
			t.Errorf("ARN for %s = %q", r.ID, r.ARN)
		}
		if r.Type != "aws_s3_bucket" || r.Account != "123456789012" {
			t.Errorf("unexpected resource shape: %+v", r)
		}
	}
	if regions["bucket-us-east-1"] != "us-east-1" || regions["bucket-ap-south-1"] != "ap-south-1" {
		t.Errorf("regions = %v", regions)
	}
	if len(mock.locationCalls) != 0 {
		t.Errorf("GetBucketLocation called for %v; ListBuckets already reported the region", mock.locationCalls)
	}
}

func TestListS3Buckets_ConfiguredRegionsFilterBeforeReadingTags(t *testing.T) {
	mock := threeRegionMock()

	resources, err := listWithMock(t, mock, []string{"us-east-1", "eu-west-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"bucket-eu-west-1", "bucket-us-east-1"}
	if got := bucketNames(resources); !equalStrings(got, want) {
		t.Errorf("buckets = %v, want %v", got, want)
	}
	sort.Strings(mock.taggingCalls)
	if !equalStrings(mock.taggingCalls, want) {
		t.Errorf("GetBucketTagging called for %v, want only configured regions %v", mock.taggingCalls, want)
	}
}

func TestListS3Buckets_FallsBackToGetBucketLocation(t *testing.T) {
	mock := threeRegionMock()
	mock.omitRegion = true

	resources, err := listWithMock(t, mock, []string{"ap-south-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := bucketNames(resources); !equalStrings(got, []string{"bucket-ap-south-1"}) {
		t.Errorf("buckets = %v", got)
	}
	if len(mock.locationCalls) != 3 {
		t.Errorf("GetBucketLocation calls = %v, want one per bucket", mock.locationCalls)
	}
}

func TestListS3Buckets_Paginates(t *testing.T) {
	mock := threeRegionMock()
	mock.pageSize = 2

	resources, err := listWithMock(t, mock, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 3 {
		t.Errorf("got %d buckets across pages, want 3", len(resources))
	}
	if mock.listCalls != 2 {
		t.Errorf("ListBuckets calls = %d, want 2", mock.listCalls)
	}
}

func TestListS3Buckets_NoTagSetIsAnUntaggedBucket(t *testing.T) {
	mock := &mockS3Client{
		buckets:     []s3types.Bucket{{Name: aws.String("untagged")}},
		taggingErrs: map[string]error{"untagged": noSuchTagSet()},
	}

	resources, err := listWithMock(t, mock, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if len(resources[0].Tags) != 0 {
		t.Errorf("tags = %v, want empty", resources[0].Tags)
	}
}

func TestListS3Buckets_UnreadableBucketIsSkippedNotReportedUntagged(t *testing.T) {
	mock := &mockS3Client{
		buckets: []s3types.Bucket{
			{Name: aws.String("reachable")},
			{Name: aws.String("unreachable")},
		},
		bucketTags: map[string][]s3types.Tag{
			"reachable": {{Key: aws.String("owner"), Value: aws.String("team@example.com")}},
		},
		taggingErrs: map[string]error{"unreachable": errors.New("dial tcp: i/o timeout")},
	}

	resources, err := listWithMock(t, mock, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := bucketNames(resources); !equalStrings(got, []string{"reachable"}) {
		t.Errorf("buckets = %v, want only the reachable one", got)
	}
	if resources[0].Tags["owner"] != "team@example.com" {
		t.Errorf("tags = %v", resources[0].Tags)
	}
}

func TestListS3Buckets_ListError(t *testing.T) {
	mock := &mockS3Client{listBucketsErr: errors.New("access denied")}

	if _, err := listWithMock(t, mock, nil); err == nil {
		t.Fatal("expected error from ListBuckets")
	}
}

func TestGetBucketRegion(t *testing.T) {
	mock := &mockS3Client{
		bucketLocations: map[string]s3types.BucketLocationConstraint{
			"virginia": "",
			"ireland":  s3types.BucketLocationConstraintEuWest1,
		},
	}
	p := testProvider()

	cases := map[string]string{"virginia": "us-east-1", "ireland": "eu-west-1"}
	for bucket, want := range cases {
		if got := p.getBucketRegion(context.Background(), mock, bucket); got != want {
			t.Errorf("%s: region = %q, want %q", bucket, got, want)
		}
	}

	failing := &mockS3Client{getLocationErr: errors.New("boom")}
	if got := p.getBucketRegion(context.Background(), failing, "any"); got != "us-east-1" {
		t.Errorf("on error region = %q, want us-east-1 fallback", got)
	}
	china := &Provider{partition: "aws-cn", regions: []string{"cn-north-1"}}
	if got := china.getBucketRegion(context.Background(), failing, "any"); got != "cn-northwest-1" {
		t.Errorf("on error region = %q, want the aws-cn global region", got)
	}
}

func TestApplyS3Tags_SendsOnlyTheNewTagsThroughTheTaggingAPI(t *testing.T) {
	const bucketARN = "arn:aws:s3:::logs"
	tests := []struct {
		name          string
		region        string
		wantRegion    string
		wantLocations int
	}{
		{"region from the plan", "eu-west-1", "eu-west-1", 0},
		{"no region looks the bucket up", "", "ap-south-1", 1},
		{"global is not a bucket region", regionGlobal, "ap-south-1", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3mock := &mockS3Client{
				bucketLocations: map[string]s3types.BucketLocationConstraint{"logs": "ap-south-1"},
				bucketTags:      map[string][]s3types.Tag{"logs": {{Key: aws.String("existing"), Value: aws.String("kept")}}},
			}
			tagging := &mockTaggingClient{}
			var regions []string
			taggingFor := func(region string) taggingAPI {
				regions = append(regions, region)
				return tagging
			}

			err := testProvider().applyS3TagsWith(context.Background(), s3mock, taggingFor, bucketARN, tt.region, map[string]string{"owner": "x"})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !equalStrings(regions, []string{tt.wantRegion}) {
				t.Errorf("tagging clients requested for %v, want %s", regions, tt.wantRegion)
			}
			if got := tagging.tagged[bucketARN]; len(got) != 1 || got["owner"] != "x" {
				t.Errorf("TagResources tags = %v, want only the new tag", got)
			}
			if len(s3mock.taggingCalls) != 0 {
				t.Errorf("GetBucketTagging called for %v, want no read before the write", s3mock.taggingCalls)
			}
			if len(s3mock.locationCalls) != tt.wantLocations {
				t.Errorf("GetBucketLocation called %d times, want %d", len(s3mock.locationCalls), tt.wantLocations)
			}
		})
	}
}

func TestApplyS3Tags_ReportsATaggingAPIFailure(t *testing.T) {
	const bucketARN = "arn:aws:s3:::logs"
	tagging := &mockTaggingClient{failing: map[string]string{bucketARN: "access denied"}}
	taggingFor := func(string) taggingAPI { return tagging }

	err := testProvider().applyS3TagsWith(context.Background(), &mockS3Client{}, taggingFor, bucketARN, "eu-west-1", map[string]string{"owner": "x"})

	var failure *taggingFailure
	if !errors.As(err, &failure) || failure.message != "access denied" {
		t.Errorf("err = %v, want the per-resource failure", err)
	}
}

func TestApplyS3Tags_RejectsAnIdentifierThatIsNotAnARN(t *testing.T) {
	taggingFor := func(string) taggingAPI {
		t.Error("tagging client requested for a malformed identifier")
		return &mockTaggingClient{}
	}
	if err := testProvider().applyS3TagsWith(context.Background(), &mockS3Client{}, taggingFor, "logs", "eu-west-1", nil); err == nil {
		t.Error("err = nil, want an error")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
