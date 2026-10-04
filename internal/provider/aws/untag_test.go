package aws

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	"github.com/aws/aws-sdk-go-v2/service/lightsail"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	taggingtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

var errDenied = errors.New("AccessDenied")

type mockUntagClient struct {
	err     error
	failing map[string]string

	arns     []string
	keys     []string
	name     string
	ec2Input *ec2.DeleteTagsInput
	asgInput *autoscaling.DeleteTagsInput
}

func (m *mockUntagClient) UntagResources(ctx context.Context, params *resourcegroupstaggingapi.UntagResourcesInput, optFns ...func(*resourcegroupstaggingapi.Options)) (*resourcegroupstaggingapi.UntagResourcesOutput, error) {
	m.arns, m.keys = params.ResourceARNList, params.TagKeys
	out := &resourcegroupstaggingapi.UntagResourcesOutput{FailedResourcesMap: map[string]taggingtypes.FailureInfo{}}
	for arn, msg := range m.failing {
		out.FailedResourcesMap[arn] = taggingtypes.FailureInfo{ErrorCode: taggingtypes.ErrorCodeInvalidParameterException, ErrorMessage: aws.String(msg)}
	}
	return out, m.err
}

type mockEC2UntagClient struct{ *mockUntagClient }

func (m mockEC2UntagClient) DeleteTags(ctx context.Context, params *ec2.DeleteTagsInput, optFns ...func(*ec2.Options)) (*ec2.DeleteTagsOutput, error) {
	m.ec2Input = params
	return &ec2.DeleteTagsOutput{}, m.err
}

type mockASGUntagClient struct{ *mockUntagClient }

func (m mockASGUntagClient) DeleteTags(ctx context.Context, params *autoscaling.DeleteTagsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DeleteTagsOutput, error) {
	m.asgInput = params
	return &autoscaling.DeleteTagsOutput{}, m.err
}

type mockLightsailUntagClient struct{ *mockUntagClient }

func (m mockLightsailUntagClient) UntagResource(ctx context.Context, params *lightsail.UntagResourceInput, optFns ...func(*lightsail.Options)) (*lightsail.UntagResourceOutput, error) {
	m.arns, m.keys, m.name = []string{aws.ToString(params.ResourceArn)}, params.TagKeys, aws.ToString(params.ResourceName)
	return &lightsail.UntagResourceOutput{}, m.err
}

type mockGAUntagClient struct{ *mockUntagClient }

func (m mockGAUntagClient) UntagResource(ctx context.Context, params *globalaccelerator.UntagResourceInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.UntagResourceOutput, error) {
	m.arns, m.keys = []string{aws.ToString(params.ResourceArn)}, params.TagKeys
	return &globalaccelerator.UntagResourceOutput{}, m.err
}

func assertProviderError(t *testing.T, err error, operation string) {
	t.Helper()
	var perr *provider.ProviderError
	if !errors.As(err, &perr) || perr.Operation != operation || !errors.Is(err, errDenied) {
		t.Errorf("err = %v, want a %s provider error wrapping the API error", err, operation)
	}
}

func TestRemoveTagsViaTaggingAPI(t *testing.T) {
	const arn = "arn:aws:lambda:us-east-1:123456789012:function:fn"
	ctx := context.Background()

	mock := &mockUntagClient{}
	if err := removeTagsViaTaggingAPI(ctx, mock, arn, []string{"Env", "temp"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !equalStrings(mock.arns, []string{arn}) || !equalStrings(mock.keys, []string{"Env", "temp"}) {
		t.Errorf("UntagResources(%v, %v), want the ARN and both keys", mock.arns, mock.keys)
	}

	assertProviderError(t, removeTagsViaTaggingAPI(ctx, &mockUntagClient{err: errDenied}, arn, []string{"Env"}), "untag_resources")

	err := removeTagsViaTaggingAPI(ctx, &mockUntagClient{failing: map[string]string{arn: "not authorized"}}, arn, []string{"Env"})
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("err = %v, want the per-resource failure of a 200 response", err)
	}
}

func TestRemoveEC2Tags_SendsKeysWithoutValues(t *testing.T) {
	mock := &mockUntagClient{}
	if err := removeEC2Tags(context.Background(), mockEC2UntagClient{mock}, "vol-1", []string{"Env", "temp"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !equalStrings(mock.ec2Input.Resources, []string{"vol-1"}) || len(mock.ec2Input.Tags) != 2 {
		t.Fatalf("DeleteTags input = %+v", mock.ec2Input)
	}
	for _, tag := range mock.ec2Input.Tags {
		if tag.Value != nil {
			t.Errorf("tag %s sent with value %q; EC2 would only delete it when the value matches", aws.ToString(tag.Key), *tag.Value)
		}
	}

	assertProviderError(t, removeEC2Tags(context.Background(), mockEC2UntagClient{&mockUntagClient{err: errDenied}}, "vol-1", []string{"Env"}), "delete_tags")
}

func TestRemoveAutoScalingTags(t *testing.T) {
	const arn = "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web"
	mock := &mockUntagClient{}
	if err := removeAutoScalingTags(context.Background(), mockASGUntagClient{mock}, arn, []string{"Env"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tags := mock.asgInput.Tags
	if len(tags) != 1 || aws.ToString(tags[0].ResourceId) != "web" || aws.ToString(tags[0].ResourceType) != "auto-scaling-group" || aws.ToString(tags[0].Key) != "Env" {
		t.Errorf("DeleteTags tags = %+v, want Env on group web", tags)
	}

	assertProviderError(t, removeAutoScalingTags(context.Background(), mockASGUntagClient{&mockUntagClient{err: errDenied}}, arn, []string{"Env"}), "remove_autoscaling_tags")
}

func TestRemoveLightsailAndGlobalAcceleratorTags(t *testing.T) {
	ctx := context.Background()
	const instance = "arn:aws:lightsail:us-east-1:123456789012:Instance/blog"
	mock := &mockUntagClient{}
	if err := removeLightsailTags(ctx, mockLightsailUntagClient{mock}, instance, []string{"Env"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.name != "blog" || !equalStrings(mock.arns, []string{instance}) || !equalStrings(mock.keys, []string{"Env"}) {
		t.Errorf("Lightsail UntagResource(%q, %v, %v)", mock.name, mock.arns, mock.keys)
	}
	assertProviderError(t, removeLightsailTags(ctx, mockLightsailUntagClient{&mockUntagClient{err: errDenied}}, instance, []string{"Env"}), "remove_lightsail_tags")

	const accelerator = "arn:aws:globalaccelerator::123456789012:accelerator/abcd"
	mock = &mockUntagClient{}
	if err := removeGlobalAcceleratorTags(ctx, mockGAUntagClient{mock}, accelerator, []string{"Env"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !equalStrings(mock.arns, []string{accelerator}) || !equalStrings(mock.keys, []string{"Env"}) {
		t.Errorf("Global Accelerator UntagResource(%v, %v)", mock.arns, mock.keys)
	}
	assertProviderError(t, removeGlobalAcceleratorTags(ctx, mockGAUntagClient{&mockUntagClient{err: errDenied}}, accelerator, []string{"Env"}), "remove_global_accelerator_tags")
}

type mockS3UntagClient struct {
	tags   []s3types.Tag
	getErr error
	putErr error

	put     []s3types.Tag
	puts    int
	deletes int
}

func (m *mockS3UntagClient) GetBucketTagging(ctx context.Context, params *s3.GetBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.GetBucketTaggingOutput, error) {
	return &s3.GetBucketTaggingOutput{TagSet: m.tags}, m.getErr
}

func (m *mockS3UntagClient) PutBucketTagging(ctx context.Context, params *s3.PutBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error) {
	m.puts++
	m.put = params.Tagging.TagSet
	return &s3.PutBucketTaggingOutput{}, m.putErr
}

func (m *mockS3UntagClient) DeleteBucketTagging(ctx context.Context, params *s3.DeleteBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketTaggingOutput, error) {
	m.deletes++
	return &s3.DeleteBucketTaggingOutput{}, m.putErr
}

func s3Tags(kv ...string) []s3types.Tag {
	tags := make([]s3types.Tag, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		tags = append(tags, s3types.Tag{Key: aws.String(kv[i]), Value: aws.String(kv[i+1])})
	}
	return tags
}

func TestRemoveS3Tags(t *testing.T) {
	ctx := context.Background()

	t.Run("keeps the other tags", func(t *testing.T) {
		mock := &mockS3UntagClient{tags: s3Tags("Env", "prod", "owner", "team@example.com", "temp", "1")}
		if err := removeS3Tags(ctx, mock, "logs", []string{"Env", "temp"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.puts != 1 || mock.deletes != 0 || len(mock.put) != 1 || aws.ToString(mock.put[0].Key) != "owner" || aws.ToString(mock.put[0].Value) != "team@example.com" {
			t.Errorf("puts = %d, deletes = %d, tag set = %+v; want one put keeping owner", mock.puts, mock.deletes, mock.put)
		}
	})

	t.Run("drops the tag set when nothing is left", func(t *testing.T) {
		mock := &mockS3UntagClient{tags: s3Tags("Env", "prod")}
		if err := removeS3Tags(ctx, mock, "logs", []string{"Env"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.puts != 0 || mock.deletes != 1 {
			t.Errorf("puts = %d, deletes = %d, want only DeleteBucketTagging", mock.puts, mock.deletes)
		}
	})

	t.Run("writes nothing when the keys are absent", func(t *testing.T) {
		mock := &mockS3UntagClient{tags: s3Tags("owner", "team@example.com")}
		if err := removeS3Tags(ctx, mock, "logs", []string{"Env"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.puts != 0 || mock.deletes != 0 {
			t.Errorf("puts = %d, deletes = %d, want no write", mock.puts, mock.deletes)
		}
	})

	t.Run("bucket without a tag set", func(t *testing.T) {
		mock := &mockS3UntagClient{getErr: &smithy.GenericAPIError{Code: "NoSuchTagSet"}}
		if err := removeS3Tags(ctx, mock, "logs", []string{"Env"}); err != nil || mock.puts+mock.deletes != 0 {
			t.Errorf("err = %v, writes = %d; want nil and no write", err, mock.puts+mock.deletes)
		}
	})

	t.Run("never writes when the current tags cannot be read", func(t *testing.T) {
		mock := &mockS3UntagClient{getErr: errDenied}
		assertProviderError(t, removeS3Tags(ctx, mock, "logs", []string{"Env"}), "get_bucket_tagging")
		if mock.puts+mock.deletes != 0 {
			t.Errorf("wrote %d times after a failed read; the other tags would be wiped", mock.puts+mock.deletes)
		}
	})

	t.Run("write error", func(t *testing.T) {
		mock := &mockS3UntagClient{tags: s3Tags("Env", "prod", "owner", "a"), putErr: errDenied}
		assertProviderError(t, removeS3Tags(ctx, mock, "logs", []string{"Env"}), "put_bucket_tagging")
	})
}

func TestRemoveTags_NothingToRemoveOrNoAddress(t *testing.T) {
	p := testProvider()
	if err := p.RemoveTags(context.Background(), types.Resource{ID: "x"}, nil); err != nil {
		t.Errorf("RemoveTags() with no keys = %v, want nil", err)
	}
	err := p.RemoveTags(context.Background(), types.Resource{ID: "fn", Type: "aws_lambda_function"}, []string{"Env"})
	if err == nil || !strings.Contains(err.Error(), "no ARN") {
		t.Errorf("RemoveTags() without an ARN = %v, want an error", err)
	}
}

var _ provider.TagRemover = (*Provider)(nil)

func TestUntagRouteFor(t *testing.T) {
	cases := []struct {
		name       string
		resource   types.Resource
		wantRoute  untagRoute
		wantRegion string
		wantErr    string
	}{
		{"EC2 volume", types.Resource{ID: "vol-1", ARN: "arn:aws:ec2:eu-west-1:123456789012:volume/vol-1", Type: "aws_ebs_volume"}, untagViaEC2, "eu-west-1", ""},
		{"S3 bucket untags by ARN in the scanned region", types.Resource{ID: "logs", ARN: "arn:aws:s3:::logs", Type: "aws_s3_bucket", Region: "eu-west-1"}, untagViaTaggingAPI, "eu-west-1", ""},
		{"S3 bucket whose region has to be looked up", types.Resource{ID: "logs", ARN: "arn:aws:s3:::logs", Type: "aws_s3_bucket"}, untagViaTaggingAPI, "", ""},
		{"S3 bucket without ARN rewrites its tag set", types.Resource{ID: "logs", Type: "aws_s3_bucket", Region: "eu-west-1"}, untagViaS3, "eu-west-1", ""},
		{"EC2 resource without a region", types.Resource{ID: "vol-1", ARN: "arn:aws:ec2::123456789012:volume/vol-1"}, 0, "", "no region"},
		{"Auto Scaling group", types.Resource{ARN: "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web"}, untagViaAutoScaling, "us-east-1", ""},
		{"Lightsail instance", types.Resource{ARN: "arn:aws:lightsail:us-east-1:123456789012:Instance/blog"}, untagViaLightsail, "us-east-1", ""},
		{"Global Accelerator lives in one region", types.Resource{ARN: "arn:aws:globalaccelerator::123456789012:accelerator/abcd"}, untagViaGlobalAccelerator, globalAcceleratorRegion, ""},
		{"Lambda function", types.Resource{ARN: "arn:aws:lambda:eu-west-1:123456789012:function:fn"}, untagViaTaggingAPI, "eu-west-1", ""},
		{"global ARN uses the default region", types.Resource{ARN: "arn:aws:iam::123456789012:role/app"}, untagViaTaggingAPI, defaultRegion, ""},
		{"global resource as the listers record it", types.Resource{ARN: "arn:aws:iam::123456789012:role/app", Region: regionGlobal}, untagViaTaggingAPI, defaultRegion, ""},
		{"S3 bucket recorded as global is looked up", types.Resource{ID: "logs", ARN: "arn:aws:s3:::logs", Type: "aws_s3_bucket", Region: regionGlobal}, untagViaTaggingAPI, "", ""},
		{"global ARN with a scanned region", types.Resource{ARN: "arn:aws:route53:::hostedzone/Z1", Region: "us-east-1"}, untagViaTaggingAPI, "us-east-1", ""},
		{"no ARN", types.Resource{ID: "fn", Type: "aws_lambda_function", Region: "us-east-1"}, 0, "", "no ARN"},
		{"not an ARN", types.Resource{ID: "fn", ARN: "fn", Region: "us-east-1"}, 0, "", "arn: invalid prefix"},
		{"another partition", types.Resource{ARN: "arn:aws-cn:lambda:cn-north-1:123456789012:function:fn"}, untagViaTaggingAPI, "cn-north-1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, region, err := untagRouteFor(tc.resource, defaultRegion)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || route != tc.wantRoute || region != tc.wantRegion {
				t.Errorf("untagRouteFor() = %d, %q, %v; want %d, %q", route, region, err, tc.wantRoute, tc.wantRegion)
			}
		})
	}
}

func TestUntagPolicy_MirrorsEveryApplyAction(t *testing.T) {
	// Write actions that also remove: S3 rewrites the tag set, Route 53 has
	// one call for both, API Gateway untags with DELETE next to PUT.
	selfSufficient := []string{"s3:PutBucketTagging", "route53:ChangeTagsForResource", "apigateway:POST"}

	untagServices := map[string]bool{}
	for a := range actionSet(loadPolicyFile(t, untagPolicyFile)) {
		service, _, _ := strings.Cut(a, ":")
		untagServices[service] = true
	}
	for a := range actionSet(loadPolicyFile(t, applyPolicyFile)) {
		if slices.Contains(selfSufficient, a) {
			continue
		}
		if service, _, _ := strings.Cut(a, ":"); !untagServices[service] {
			t.Errorf("%s can tag %s resources but %s has no action to untag them", applyPolicyFile, service, untagPolicyFile)
		}
	}
}
