package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
)

type mockClassicELBClient struct {
	names      []string
	tags       map[string][]elbtypes.Tag
	err        error
	tagBatches []int
}

func (m *mockClassicELBClient) DescribeLoadBalancers(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := &elb.DescribeLoadBalancersOutput{}
	for _, n := range m.names {
		out.LoadBalancerDescriptions = append(out.LoadBalancerDescriptions, elbtypes.LoadBalancerDescription{LoadBalancerName: aws.String(n)})
	}
	return out, nil
}

func (m *mockClassicELBClient) DescribeTags(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error) {
	m.tagBatches = append(m.tagBatches, len(params.LoadBalancerNames))
	out := &elb.DescribeTagsOutput{}
	for _, n := range params.LoadBalancerNames {
		out.TagDescriptions = append(out.TagDescriptions, elbtypes.TagDescription{LoadBalancerName: aws.String(n), Tags: m.tags[n]})
	}
	return out, nil
}

func (m *mockClassicELBClient) AddTags(ctx context.Context, params *elb.AddTagsInput, optFns ...func(*elb.Options)) (*elb.AddTagsOutput, error) {
	return &elb.AddTagsOutput{}, nil
}

func TestListClassicLoadBalancers(t *testing.T) {
	names := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		names = append(names, "lb-"+string(rune('a'+i)))
	}
	mock := &mockClassicELBClient{
		names: names,
		tags:  map[string][]elbtypes.Tag{"lb-a": {{Key: aws.String("environment"), Value: aws.String(envProd)}}},
	}

	resources, err := testProvider().listClassicLoadBalancersFrom(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 25 {
		t.Fatalf("got %d load balancers, want 25", len(resources))
	}
	if len(mock.tagBatches) != 2 || mock.tagBatches[0] != elbTagBatchSize {
		t.Errorf("DescribeTags batches = %v, want [20 5]", mock.tagBatches)
	}
	if resources[0].Tags["environment"] != envProd || resources[0].Type != "aws_elb" {
		t.Errorf("first = %+v", resources[0])
	}
	if resources[0].ARN != "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/lb-a" {
		t.Errorf("ARN = %q", resources[0].ARN)
	}
}

func TestListClassicLoadBalancers_BulkTags(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/lb-a"
	p := testProvider()
	p.tagSources = map[string]*tagSource{defaultRegion: staticTagSource(map[string]map[string]string{arn: {"owner": "x"}})}
	mock := &mockClassicELBClient{names: []string{"lb-a"}}

	resources, err := p.listClassicLoadBalancersFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
	if len(mock.tagBatches) != 0 {
		t.Error("DescribeTags called although bulk tags were available")
	}
}

func TestListClassicLoadBalancers_Error(t *testing.T) {
	mock := &mockClassicELBClient{err: errors.New("boom")}
	if _, err := testProvider().listClassicLoadBalancersFrom(context.Background(), mock, defaultRegion); err == nil {
		t.Error("expected error")
	}
}
