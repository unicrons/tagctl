package aws

import (
	"context"
	"errors"
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
