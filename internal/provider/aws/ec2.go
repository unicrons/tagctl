package aws

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// listEC2Instances lists all EC2 instances in a region.
func (p *Provider) listEC2Instances(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listEC2InstancesFrom(ctx, regionalClient(p, region, ec2.NewFromConfig), region)
}

func (p *Provider) listEC2InstancesFrom(ctx context.Context, client ec2.DescribeInstancesAPIClient, region string) ([]types.Resource, error) {
	log.Debug("AWS EC2: Listing instances in region %s...", region)
	var resources []types.Resource

	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ec2_instances", "", err)
		}

		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				// Skip terminated instances
				if instance.State != nil && instance.State.Name == ec2types.InstanceStateNameTerminated {
					continue
				}

				resource := types.Resource{
					ID:       aws.ToString(instance.InstanceId),
					Type:     "aws_instance",
					Region:   region,
					Account:  p.accountID,
					Provider: providerName,
					Tags:     ec2TagsToMap(instance.Tags),
				}

				// Set name from tags
				if name, ok := resource.Tags["Name"]; ok {
					resource.Name = name
				} else {
					resource.Name = resource.ID
				}

				// Set ARN
				resource.ARN = p.ec2ARN(region, "instance", resource.ID)

				// Set creation time
				if instance.LaunchTime != nil {
					resource.CreatedAt = instance.LaunchTime
				}
				resource = withEC2Parent(resource, types.RelationVPC, "vpc", aws.ToString(instance.VpcId))

				log.Debug("AWS EC2: Instance %s (%s) in %s has %d tags", resource.ID, resource.Name, region, len(resource.Tags))
				resources = append(resources, resource)
			}
		}
	}

	log.Debug("AWS EC2: Found %d instances in %s", len(resources), region)
	return resources, nil
}

// listEBSVolumes lists all EBS volumes in a region.
func (p *Provider) listEBSVolumes(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listEBSVolumesFrom(ctx, regionalClient(p, region, ec2.NewFromConfig), region)
}

func (p *Provider) listEBSVolumesFrom(ctx context.Context, client ec2.DescribeVolumesAPIClient, region string) ([]types.Resource, error) {
	log.Debug("AWS EBS: Listing volumes in region %s...", region)
	var resources []types.Resource

	paginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ebs_volumes", "", err)
		}

		for _, volume := range output.Volumes {
			resource := types.Resource{
				ID:       aws.ToString(volume.VolumeId),
				Type:     "aws_ebs_volume",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     ec2TagsToMap(volume.Tags),
			}

			// Set name from tags
			if name, ok := resource.Tags["Name"]; ok {
				resource.Name = name
			} else {
				resource.Name = resource.ID
			}

			// Set ARN
			resource.ARN = p.ec2ARN(region, "volume", resource.ID)

			// Set creation time
			if volume.CreateTime != nil {
				resource.CreatedAt = volume.CreateTime
			}
			// A multi-attach volume has no single instance to inherit from.
			if len(volume.Attachments) == 1 {
				resource = withEC2Parent(resource, types.RelationAttachedInstance, "instance", aws.ToString(volume.Attachments[0].InstanceId))
			}

			log.Debug("AWS EBS: Volume %s (%s) in %s has %d tags", resource.ID, resource.Name, region, len(resource.Tags))
			resources = append(resources, resource)
		}
	}

	log.Debug("AWS EBS: Found %d volumes in %s", len(resources), region)
	return resources, nil
}

// ebsSnapshotsAPI is the subset of the EC2 API used to discover snapshots.
type ebsSnapshotsAPI interface {
	DescribeSnapshots(ctx context.Context, params *ec2.DescribeSnapshotsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSnapshotsOutput, error)
}

// listEBSSnapshots lists the EBS snapshots owned by the account in a region.
func (p *Provider) listEBSSnapshots(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listEBSSnapshotsFrom(ctx, regionalClient(p, region, ec2.NewFromConfig), region)
}

// listEBSSnapshotsFrom lists owned EBS snapshots using the given client.
// Public and shared snapshots belong to other accounts and cannot be tagged here.
func (p *Provider) listEBSSnapshotsFrom(ctx context.Context, client ebsSnapshotsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS EBS: Listing snapshots in region %s...", region)

	var resources []types.Resource
	paginator := ec2.NewDescribeSnapshotsPaginator(client, &ec2.DescribeSnapshotsInput{OwnerIds: []string{"self"}})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ebs_snapshots", "", err)
		}
		for _, snap := range output.Snapshots {
			id := aws.ToString(snap.SnapshotId)
			resource := types.Resource{
				ID:        id,
				Name:      id,
				ARN:       p.ec2ARN(region, "snapshot", id),
				Type:      "aws_ebs_snapshot",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      ec2TagsToMap(snap.Tags),
				CreatedAt: snap.StartTime,
			}
			if name, ok := resource.Tags["Name"]; ok {
				resource.Name = name
			}
			resource = withEC2Parent(resource, types.RelationSourceVolume, "volume", aws.ToString(snap.VolumeId))
			resources = append(resources, resource)
		}
	}

	log.Debug("AWS EBS: Found %d snapshots in %s", len(resources), region)
	return resources, nil
}

// ec2CreateTagsAPI is the subset of the EC2 API used to tag a resource.
type ec2CreateTagsAPI interface {
	CreateTags(ctx context.Context, params *ec2.CreateTagsInput, optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
}

// applyEC2Tags tags an EC2 resource addressed by bare ID in the region the
// plan recorded for it.
func (p *Provider) applyEC2Tags(ctx context.Context, resourceID, region string, tags map[string]string) error {
	return applyEC2TagsWith(ctx, func(region string) ec2CreateTagsAPI { return regionalClient(p, region, ec2.NewFromConfig) }, resourceID, region, tags)
}

// applyEC2TagsWith makes the single CreateTags call. An ID says nothing about
// its region, so a missing one is an error, never a search across regions.
func applyEC2TagsWith(ctx context.Context, clientFor func(region string) ec2CreateTagsAPI, resourceID, region string, tags map[string]string) error {
	if region == "" || region == regionGlobal {
		return provider.NewProviderError(providerName, "create_tags", resourceID,
			errors.New("EC2 resources are tagged by ID and need the region of the resource"))
	}

	ec2Tags := make([]ec2types.Tag, 0, len(tags))
	for k, v := range tags {
		ec2Tags = append(ec2Tags, ec2types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	_, err := clientFor(region).CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{resourceID},
		Tags:      ec2Tags,
	})
	if err != nil {
		log.Error("AWS EC2: Failed to apply tags to %s in %s: %v", resourceID, region, err)
		return provider.NewProviderError(providerName, "create_tags", resourceID, err)
	}

	log.Debug("AWS EC2: Applied %d tags to %s in %s", len(tags), resourceID, region)
	return nil
}

// ec2TagsToMap is tagsToMap for the tag type every EC2 lister shares.
func ec2TagsToMap(tags []ec2types.Tag) map[string]string {
	return tagsToMap(tags,
		func(t ec2types.Tag) *string { return t.Key },
		func(t ec2types.Tag) *string { return t.Value })
}

// listSecurityGroups lists all EC2 security groups in a region.
func (p *Provider) listSecurityGroups(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSecurityGroupsFrom(ctx, regionalClient(p, region, ec2.NewFromConfig), region)
}

// listSecurityGroupsFrom lists security groups using the given client.
func (p *Provider) listSecurityGroupsFrom(ctx context.Context, client ec2DescribeSecurityGroupsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS SecurityGroup: Listing security groups in region %s...", region)
	var resources []types.Resource

	paginator := ec2.NewDescribeSecurityGroupsPaginator(client, &ec2.DescribeSecurityGroupsInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_security_groups", "", err)
		}

		for _, sg := range output.SecurityGroups {
			resource := types.Resource{
				ID:       aws.ToString(sg.GroupId),
				Type:     "aws_security_group",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     ec2TagsToMap(sg.Tags),
			}

			// The Name tag wins over the group name, which is the fallback.
			if name, ok := resource.Tags["Name"]; ok {
				resource.Name = name
			} else {
				resource.Name = aws.ToString(sg.GroupName)
			}

			resource.ARN = p.ec2ARN(region, "security-group", resource.ID)

			log.Debug("AWS SecurityGroup: %s (%s) in %s has %d tags", resource.ID, resource.Name, region, len(resource.Tags))
			resources = append(resources, withEC2Parent(resource, types.RelationVPC, "vpc", aws.ToString(sg.VpcId)))
		}
	}

	log.Debug("AWS SecurityGroup: Found %d security groups in %s", len(resources), region)
	return resources, nil
}

// listVPCs lists all VPCs in a region.
func (p *Provider) listVPCs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listVPCsFrom(ctx, regionalClient(p, region, ec2.NewFromConfig), region)
}

// listVPCsFrom lists VPCs using the given client.
func (p *Provider) listVPCsFrom(ctx context.Context, client ec2DescribeVpcsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS VPC: Listing VPCs in region %s...", region)
	var resources []types.Resource

	paginator := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_vpcs", "", err)
		}

		for _, vpc := range output.Vpcs {
			resource := types.Resource{
				ID:       aws.ToString(vpc.VpcId),
				Type:     "aws_vpc",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     ec2TagsToMap(vpc.Tags),
			}

			if name, ok := resource.Tags["Name"]; ok {
				resource.Name = name
			} else {
				resource.Name = resource.ID
			}

			resource.ARN = p.ec2ARN(region, "vpc", resource.ID)

			log.Debug("AWS VPC: %s (%s) in %s has %d tags", resource.ID, resource.Name, region, len(resource.Tags))
			resources = append(resources, resource)
		}
	}

	log.Debug("AWS VPC: Found %d VPCs in %s", len(resources), region)
	return resources, nil
}

// listSubnets lists all subnets in a region.
func (p *Provider) listSubnets(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSubnetsFrom(ctx, regionalClient(p, region, ec2.NewFromConfig), region)
}

// listSubnetsFrom lists subnets using the given client.
func (p *Provider) listSubnetsFrom(ctx context.Context, client ec2DescribeSubnetsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS Subnet: Listing subnets in region %s...", region)
	var resources []types.Resource

	paginator := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_subnets", "", err)
		}

		for _, subnet := range output.Subnets {
			resource := types.Resource{
				ID:       aws.ToString(subnet.SubnetId),
				Type:     "aws_subnet",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     ec2TagsToMap(subnet.Tags),
			}

			if name, ok := resource.Tags["Name"]; ok {
				resource.Name = name
			} else {
				resource.Name = resource.ID
			}

			resource.ARN = p.ec2ARN(region, "subnet", resource.ID)

			log.Debug("AWS Subnet: %s (%s) in %s has %d tags", resource.ID, resource.Name, region, len(resource.Tags))
			resources = append(resources, withEC2Parent(resource, types.RelationVPC, "vpc", aws.ToString(subnet.VpcId)))
		}
	}

	log.Debug("AWS Subnet: Found %d subnets in %s", len(resources), region)
	return resources, nil
}

// ec2DescribeSecurityGroupsAPI is the subset of the EC2 API used to list security groups.
type ec2DescribeSecurityGroupsAPI interface {
	DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
}

// ec2DescribeVpcsAPI is the subset of the EC2 API used to list VPCs.
type ec2DescribeVpcsAPI interface {
	DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
}

// ec2DescribeSubnetsAPI is the subset of the EC2 API used to list subnets.
type ec2DescribeSubnetsAPI interface {
	DescribeSubnets(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
}
