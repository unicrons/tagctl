package aws

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// ec2ExtraAPI is the subset of the EC2 API used to discover the networking
// and image resources that carry tags inline in their Describe call.
type ec2ExtraAPI interface {
	DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
	DescribeAddresses(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	DescribeNatGateways(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error)
	DescribeInternetGateways(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error)
	DescribeVpcEndpoints(ctx context.Context, params *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error)
	DescribeLaunchTemplates(ctx context.Context, params *ec2.DescribeLaunchTemplatesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeLaunchTemplatesOutput, error)
}

// ec2Resource builds a Resource for an EC2-family object identified by ID.
func (p *Provider) ec2Resource(region, arnType, resourceType, id string, tags []ec2types.Tag, created *time.Time) types.Resource {
	r := types.Resource{
		ID:        id,
		Name:      id,
		ARN:       buildEC2ARN(p.accountID, region, arnType, id),
		Type:      resourceType,
		Region:    region,
		Account:   p.accountID,
		Provider:  providerName,
		Tags:      ec2TagsToMap(tags),
		CreatedAt: created,
	}
	if name, ok := r.Tags["Name"]; ok {
		r.Name = name
	}
	return r
}

// listAMIs lists the AMIs owned by the account in a region.
func (p *Provider) listAMIs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listAMIsFrom(ctx, p.getEC2Client(region), region)
}

func (p *Provider) listAMIsFrom(ctx context.Context, client ec2ExtraAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := ec2.NewDescribeImagesPaginator(client, &ec2.DescribeImagesInput{Owners: []string{"self"}})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_amis", "", err)
		}
		for _, img := range output.Images {
			r := p.ec2Resource(region, "image", "aws_ami", aws.ToString(img.ImageId), img.Tags, parseRFC3339(img.CreationDate))
			if r.Name == r.ID && img.Name != nil {
				r.Name = *img.Name
			}
			resources = append(resources, r)
		}
	}
	log.Debug("AWS EC2: Found %d AMIs in %s", len(resources), region)
	return resources, nil
}

// listElasticIPs lists the Elastic IP allocations in a region.
func (p *Provider) listElasticIPs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listElasticIPsFrom(ctx, p.getEC2Client(region), region)
}

func (p *Provider) listElasticIPsFrom(ctx context.Context, client ec2ExtraAPI, region string) ([]types.Resource, error) {
	output, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_elastic_ips", "", err)
	}
	resources := make([]types.Resource, 0, len(output.Addresses))
	for _, addr := range output.Addresses {
		id := aws.ToString(addr.AllocationId)
		if id == "" {
			continue // EC2-Classic address, not taggable
		}
		r := p.ec2Resource(region, "elastic-ip", "aws_eip", id, addr.Tags, nil)
		if r.Name == r.ID && addr.PublicIp != nil {
			r.Name = *addr.PublicIp
		}
		resources = append(resources, r)
	}
	log.Debug("AWS EC2: Found %d Elastic IPs in %s", len(resources), region)
	return resources, nil
}

// listNATGateways lists the NAT gateways in a region.
func (p *Provider) listNATGateways(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listNATGatewaysFrom(ctx, p.getEC2Client(region), region)
}

func (p *Provider) listNATGatewaysFrom(ctx context.Context, client ec2ExtraAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := ec2.NewDescribeNatGatewaysPaginator(client, &ec2.DescribeNatGatewaysInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_nat_gateways", "", err)
		}
		for _, gw := range output.NatGateways {
			if gw.State == ec2types.NatGatewayStateDeleted {
				continue
			}
			resources = append(resources, p.ec2Resource(region, "natgateway", "aws_nat_gateway", aws.ToString(gw.NatGatewayId), gw.Tags, gw.CreateTime))
		}
	}
	log.Debug("AWS EC2: Found %d NAT gateways in %s", len(resources), region)
	return resources, nil
}

// listInternetGateways lists the internet gateways in a region.
func (p *Provider) listInternetGateways(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listInternetGatewaysFrom(ctx, p.getEC2Client(region), region)
}

func (p *Provider) listInternetGatewaysFrom(ctx context.Context, client ec2ExtraAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := ec2.NewDescribeInternetGatewaysPaginator(client, &ec2.DescribeInternetGatewaysInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_internet_gateways", "", err)
		}
		for _, gw := range output.InternetGateways {
			resources = append(resources, p.ec2Resource(region, "internet-gateway", "aws_internet_gateway", aws.ToString(gw.InternetGatewayId), gw.Tags, nil))
		}
	}
	log.Debug("AWS EC2: Found %d internet gateways in %s", len(resources), region)
	return resources, nil
}

// listVPCEndpoints lists the VPC endpoints in a region.
func (p *Provider) listVPCEndpoints(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listVPCEndpointsFrom(ctx, p.getEC2Client(region), region)
}

func (p *Provider) listVPCEndpointsFrom(ctx context.Context, client ec2ExtraAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := ec2.NewDescribeVpcEndpointsPaginator(client, &ec2.DescribeVpcEndpointsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_vpc_endpoints", "", err)
		}
		for _, ep := range output.VpcEndpoints {
			resources = append(resources, p.ec2Resource(region, "vpc-endpoint", "aws_vpc_endpoint", aws.ToString(ep.VpcEndpointId), ep.Tags, ep.CreationTimestamp))
		}
	}
	log.Debug("AWS EC2: Found %d VPC endpoints in %s", len(resources), region)
	return resources, nil
}

// listLaunchTemplates lists the launch templates in a region.
func (p *Provider) listLaunchTemplates(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listLaunchTemplatesFrom(ctx, p.getEC2Client(region), region)
}

func (p *Provider) listLaunchTemplatesFrom(ctx context.Context, client ec2ExtraAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := ec2.NewDescribeLaunchTemplatesPaginator(client, &ec2.DescribeLaunchTemplatesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_launch_templates", "", err)
		}
		for _, lt := range output.LaunchTemplates {
			r := p.ec2Resource(region, "launch-template", "aws_launch_template", aws.ToString(lt.LaunchTemplateId), lt.Tags, lt.CreateTime)
			if r.Name == r.ID && lt.LaunchTemplateName != nil {
				r.Name = *lt.LaunchTemplateName
			}
			resources = append(resources, r)
		}
	}
	log.Debug("AWS EC2: Found %d launch templates in %s", len(resources), region)
	return resources, nil
}

// parseRFC3339 converts the string timestamps some EC2 objects carry.
func parseRFC3339(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}
