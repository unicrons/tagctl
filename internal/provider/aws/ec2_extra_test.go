package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type mockEC2ExtraClient struct {
	images     []ec2types.Image
	addresses  []ec2types.Address
	nats       []ec2types.NatGateway
	igws       []ec2types.InternetGateway
	endpoints  []ec2types.VpcEndpoint
	templates  []ec2types.LaunchTemplate
	err        error
	imageOwner []string
}

func (m *mockEC2ExtraClient) DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	m.imageOwner = params.Owners
	return &ec2.DescribeImagesOutput{Images: m.images}, m.err
}

func (m *mockEC2ExtraClient) DescribeAddresses(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	return &ec2.DescribeAddressesOutput{Addresses: m.addresses}, m.err
}

func (m *mockEC2ExtraClient) DescribeNatGateways(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
	return &ec2.DescribeNatGatewaysOutput{NatGateways: m.nats}, m.err
}

func (m *mockEC2ExtraClient) DescribeInternetGateways(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
	return &ec2.DescribeInternetGatewaysOutput{InternetGateways: m.igws}, m.err
}

func (m *mockEC2ExtraClient) DescribeVpcEndpoints(ctx context.Context, params *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
	return &ec2.DescribeVpcEndpointsOutput{VpcEndpoints: m.endpoints}, m.err
}

func (m *mockEC2ExtraClient) DescribeLaunchTemplates(ctx context.Context, params *ec2.DescribeLaunchTemplatesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeLaunchTemplatesOutput, error) {
	return &ec2.DescribeLaunchTemplatesOutput{LaunchTemplates: m.templates}, m.err
}

func nameTag(name string) []ec2types.Tag {
	return []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String(name)}}
}

func TestEC2ExtraListers(t *testing.T) {
	mock := &mockEC2ExtraClient{
		images:    []ec2types.Image{{ImageId: aws.String("ami-1"), Name: aws.String("golden"), CreationDate: aws.String("2026-01-02T03:04:05.000Z")}},
		addresses: []ec2types.Address{{AllocationId: aws.String("eipalloc-1"), PublicIp: aws.String("1.2.3.4")}, {PublicIp: aws.String("5.6.7.8")}},
		nats: []ec2types.NatGateway{
			{NatGatewayId: aws.String("nat-1"), Tags: nameTag("egress"), State: ec2types.NatGatewayStateAvailable},
			{NatGatewayId: aws.String("nat-2"), State: ec2types.NatGatewayStateDeleted},
		},
		igws:      []ec2types.InternetGateway{{InternetGatewayId: aws.String("igw-1")}},
		endpoints: []ec2types.VpcEndpoint{{VpcEndpointId: aws.String("vpce-1")}},
		templates: []ec2types.LaunchTemplate{{LaunchTemplateId: aws.String("lt-1"), LaunchTemplateName: aws.String(webCluster)}},
	}
	p := testProvider()
	ctx := context.Background()

	amis, err := p.listAMIsFrom(ctx, mock, defaultRegion)
	if err != nil || len(amis) != 1 || amis[0].Name != "golden" || amis[0].Type != "aws_ami" || amis[0].CreatedAt == nil {
		t.Errorf("AMIs = %+v, err = %v", amis, err)
	}
	if !equalStrings(mock.imageOwner, []string{"self"}) {
		t.Errorf("DescribeImages Owners = %v, want [self]", mock.imageOwner)
	}
	if amis[0].ARN != "arn:aws:ec2:us-east-1:123456789012:image/ami-1" {
		t.Errorf("AMI ARN = %q", amis[0].ARN)
	}

	eips, err := p.listElasticIPsFrom(ctx, mock, defaultRegion)
	if err != nil || len(eips) != 1 || eips[0].ID != "eipalloc-1" || eips[0].Name != "1.2.3.4" {
		t.Errorf("EIPs = %+v (classic address without allocation must be dropped), err = %v", eips, err)
	}

	nats, err := p.listNATGatewaysFrom(ctx, mock, defaultRegion)
	if err != nil || len(nats) != 1 || nats[0].Name != "egress" {
		t.Errorf("NAT gateways = %+v (deleted one must be dropped), err = %v", nats, err)
	}

	igws, err := p.listInternetGatewaysFrom(ctx, mock, defaultRegion)
	if err != nil || len(igws) != 1 || igws[0].Type != "aws_internet_gateway" {
		t.Errorf("IGWs = %+v, err = %v", igws, err)
	}

	vpces, err := p.listVPCEndpointsFrom(ctx, mock, defaultRegion)
	if err != nil || len(vpces) != 1 || vpces[0].Type != "aws_vpc_endpoint" {
		t.Errorf("VPC endpoints = %+v, err = %v", vpces, err)
	}

	lts, err := p.listLaunchTemplatesFrom(ctx, mock, defaultRegion)
	if err != nil || len(lts) != 1 || lts[0].Name != webCluster || lts[0].Type != "aws_launch_template" {
		t.Errorf("launch templates = %+v, err = %v", lts, err)
	}
}

func TestEC2ExtraListers_Error(t *testing.T) {
	mock := &mockEC2ExtraClient{err: errors.New("boom")}
	p := testProvider()
	ctx := context.Background()
	for name, list := range map[string]func() error{
		"amis":      func() error { _, err := p.listAMIsFrom(ctx, mock, defaultRegion); return err },
		"eips":      func() error { _, err := p.listElasticIPsFrom(ctx, mock, defaultRegion); return err },
		"nat":       func() error { _, err := p.listNATGatewaysFrom(ctx, mock, defaultRegion); return err },
		"igw":       func() error { _, err := p.listInternetGatewaysFrom(ctx, mock, defaultRegion); return err },
		"vpce":      func() error { _, err := p.listVPCEndpointsFrom(ctx, mock, defaultRegion); return err },
		"templates": func() error { _, err := p.listLaunchTemplatesFrom(ctx, mock, defaultRegion); return err },
	} {
		if list() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseRFC3339(t *testing.T) {
	if parseRFC3339(nil) != nil || parseRFC3339(aws.String("garbage")) != nil {
		t.Error("nil or invalid input must yield nil")
	}
	if got := parseRFC3339(aws.String("2026-01-02T03:04:05.000Z")); got == nil || got.Year() != 2026 {
		t.Errorf("got %v", got)
	}
}
