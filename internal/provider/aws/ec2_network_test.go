package aws

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// envProd is the environment tag value reused across the discovery tests.
const envProd = "prod"

// testProvider returns a provider wired for discovery tests, with no real clients.
func testProvider() *Provider {
	return &Provider{
		accountID: "123456789012",
		regions:   []string{"us-east-1"},
	}
}

// mockSecurityGroupsClient serves DescribeSecurityGroups pages in order.
type mockSecurityGroupsClient struct {
	pages [][]ec2types.SecurityGroup
	err   error
	calls int
}

func (m *mockSecurityGroupsClient) DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	page := m.pages[m.calls]
	m.calls++
	out := &ec2.DescribeSecurityGroupsOutput{SecurityGroups: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func TestListSecurityGroups(t *testing.T) {
	mock := &mockSecurityGroupsClient{
		pages: [][]ec2types.SecurityGroup{
			{
				{
					GroupId:   aws.String("sg-111"),
					GroupName: aws.String("web-sg"),
					Tags: []ec2types.Tag{
						{Key: aws.String("environment"), Value: aws.String(envProd)},
					},
				},
			},
			{
				{
					GroupId:   aws.String("sg-222"),
					GroupName: aws.String("db-sg"),
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String("database")},
					},
				},
			},
		},
	}

	resources, err := testProvider().listSecurityGroupsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSecurityGroupsFrom() error = %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("got %d resources across 2 pages, want 2", len(resources))
	}

	// Without a Name tag the group name is used.
	if resources[0].Name != "web-sg" {
		t.Errorf("Name = %q, want %q (falls back to GroupName)", resources[0].Name, "web-sg")
	}
	if resources[0].Type != "aws_security_group" {
		t.Errorf("Type = %q, want %q", resources[0].Type, "aws_security_group")
	}
	wantARN := "arn:aws:ec2:us-east-1:123456789012:security-group/sg-111"
	if resources[0].ARN != wantARN {
		t.Errorf("ARN = %q, want %q", resources[0].ARN, wantARN)
	}
	if resources[0].Tags["environment"] != envProd {
		t.Errorf("Tags[environment] = %q, want %q", resources[0].Tags["environment"], envProd)
	}

	// The Name tag wins over the group name.
	if resources[1].Name != "database" {
		t.Errorf("Name = %q, want %q (Name tag wins)", resources[1].Name, "database")
	}
}

func TestListSecurityGroups_Error(t *testing.T) {
	mock := &mockSecurityGroupsClient{err: errors.New("access denied")}

	if _, err := testProvider().listSecurityGroupsFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("listSecurityGroupsFrom() error = nil, want an error")
	}
}

// mockVpcsClient serves a single DescribeVpcs page.
type mockVpcsClient struct {
	vpcs []ec2types.Vpc
	err  error
}

func (m *mockVpcsClient) DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ec2.DescribeVpcsOutput{Vpcs: m.vpcs}, nil
}

func TestListVPCs(t *testing.T) {
	mock := &mockVpcsClient{
		vpcs: []ec2types.Vpc{
			{
				VpcId: aws.String("vpc-abc"),
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("main-vpc")},
					{Key: aws.String("owner"), Value: aws.String("platform")},
				},
			},
			{VpcId: aws.String("vpc-untagged")},
		},
	}

	resources, err := testProvider().listVPCsFrom(context.Background(), mock, "eu-west-1")
	if err != nil {
		t.Fatalf("listVPCsFrom() error = %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(resources))
	}

	if resources[0].Name != "main-vpc" {
		t.Errorf("Name = %q, want %q", resources[0].Name, "main-vpc")
	}
	if resources[0].Type != "aws_vpc" {
		t.Errorf("Type = %q, want %q", resources[0].Type, "aws_vpc")
	}
	wantARN := "arn:aws:ec2:eu-west-1:123456789012:vpc/vpc-abc"
	if resources[0].ARN != wantARN {
		t.Errorf("ARN = %q, want %q", resources[0].ARN, wantARN)
	}
	if len(resources[0].Tags) != 2 {
		t.Errorf("got %d tags, want 2", len(resources[0].Tags))
	}

	// An untagged VPC falls back to its ID as the name.
	if resources[1].Name != "vpc-untagged" {
		t.Errorf("Name = %q, want %q (falls back to ID)", resources[1].Name, "vpc-untagged")
	}
	if len(resources[1].Tags) != 0 {
		t.Errorf("got %d tags for untagged VPC, want 0", len(resources[1].Tags))
	}
}

// mockSubnetsClient serves a single DescribeSubnets page.
type mockSubnetsClient struct {
	subnets []ec2types.Subnet
	err     error
}

func (m *mockSubnetsClient) DescribeSubnets(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ec2.DescribeSubnetsOutput{Subnets: m.subnets}, nil
}

func TestListSubnets(t *testing.T) {
	mock := &mockSubnetsClient{
		subnets: []ec2types.Subnet{
			{
				SubnetId: aws.String("subnet-123"),
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("public-a")},
				},
			},
		},
	}

	resources, err := testProvider().listSubnetsFrom(context.Background(), mock, "us-west-2")
	if err != nil {
		t.Fatalf("listSubnetsFrom() error = %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if resources[0].Type != "aws_subnet" {
		t.Errorf("Type = %q, want %q", resources[0].Type, "aws_subnet")
	}
	if resources[0].Name != "public-a" {
		t.Errorf("Name = %q, want %q", resources[0].Name, "public-a")
	}
	wantARN := "arn:aws:ec2:us-west-2:123456789012:subnet/subnet-123"
	if resources[0].ARN != wantARN {
		t.Errorf("ARN = %q, want %q", resources[0].ARN, wantARN)
	}
	if resources[0].Account != "123456789012" {
		t.Errorf("Account = %q, want %q", resources[0].Account, "123456789012")
	}
	if resources[0].Provider != "aws" {
		t.Errorf("Provider = %q, want %q", resources[0].Provider, "aws")
	}
}

func TestListSubnets_Empty(t *testing.T) {
	resources, err := testProvider().listSubnetsFrom(context.Background(), &mockSubnetsClient{}, "us-east-1")
	if err != nil {
		t.Fatalf("listSubnetsFrom() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("got %d resources, want 0", len(resources))
	}
}

type mockCreateTagsClient struct {
	err    error
	inputs []*ec2.CreateTagsInput
}

func (m *mockCreateTagsClient) CreateTags(_ context.Context, params *ec2.CreateTagsInput, _ ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error) {
	m.inputs = append(m.inputs, params)
	return &ec2.CreateTagsOutput{}, m.err
}

func TestApplyEC2Tags_MakesOneCallInTheResourceRegion(t *testing.T) {
	mock := &mockCreateTagsClient{}
	var regions []string
	clientFor := func(region string) ec2CreateTagsAPI {
		regions = append(regions, region)
		return mock
	}

	err := applyEC2TagsWith(context.Background(), clientFor, "i-0abc", "eu-west-1", map[string]string{"owner": "x"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(regions, []string{"eu-west-1"}) {
		t.Errorf("clients requested for %v, want only eu-west-1", regions)
	}
	if len(mock.inputs) != 1 {
		t.Fatalf("CreateTags called %d times, want 1", len(mock.inputs))
	}
	in := mock.inputs[0]
	if !slices.Equal(in.Resources, []string{"i-0abc"}) || len(in.Tags) != 1 || aws.ToString(in.Tags[0].Key) != "owner" || aws.ToString(in.Tags[0].Value) != "x" {
		t.Errorf("CreateTags input = %+v", in)
	}
}

func TestApplyEC2Tags_NotFoundInItsRegionIsNotRetriedElsewhere(t *testing.T) {
	notFound := errors.New("InvalidInstanceID.NotFound: The instance ID 'i-0abc' does not exist")
	mock := &mockCreateTagsClient{err: notFound}
	calls := 0
	clientFor := func(string) ec2CreateTagsAPI {
		calls++
		return mock
	}

	err := applyEC2TagsWith(context.Background(), clientFor, "i-0abc", "eu-west-1", map[string]string{"owner": "x"})
	if !errors.Is(err, notFound) {
		t.Errorf("err = %v, want the CreateTags error", err)
	}
	if calls != 1 || len(mock.inputs) != 1 {
		t.Errorf("%d clients and %d calls, want one of each", calls, len(mock.inputs))
	}
}

func TestApplyTags_EC2IDWithoutARegionFailsWithoutACall(t *testing.T) {
	p := &Provider{regions: []string{"us-east-1", "eu-west-1"}}
	for _, region := range []string{"", regionGlobal} {
		for _, id := range []string{"i-0abc", "vol-0abc", "sg-0abc", "subnet-0abc"} {
			err := p.applyTagsInRegion(context.Background(), id, region, map[string]string{"owner": "x"})
			if err == nil || !strings.Contains(err.Error(), "need the region") {
				t.Errorf("applyTagsInRegion(%q, %q) err = %v, want a missing region error", id, region, err)
			}
		}
	}
	if err := p.ApplyTags(context.Background(), "i-0abc", map[string]string{"owner": "x"}); err == nil {
		t.Error("ApplyTags(i-0abc) without a region succeeded")
	}
}
