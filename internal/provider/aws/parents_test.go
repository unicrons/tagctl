package aws

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/unicrons/tagctl/internal/types"
)

type mockInstancesClient struct {
	instances []ec2types.Instance
	err       error
}

func (m *mockInstancesClient) DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: m.instances}}}, m.err
}

type mockVolumesClient struct {
	volumes []ec2types.Volume
	err     error
}

func (m *mockVolumesClient) DescribeVolumes(ctx context.Context, params *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	return &ec2.DescribeVolumesOutput{Volumes: m.volumes}, m.err
}

const (
	testInstanceARN = "arn:aws:ec2:us-east-1:123456789012:instance/i-1"
	testVPCARN      = "arn:aws:ec2:us-east-1:123456789012:vpc/vpc-1"
)

func parentsByID(t *testing.T, resources []types.Resource, err error) map[string]map[string]string {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parents := make(map[string]map[string]string, len(resources))
	for _, r := range resources {
		parents[r.ID] = r.Parents
	}
	return parents
}

func assertParents(t *testing.T, got, want map[string]map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got resources %v, want %v", got, want)
	}
	for id, wantParents := range want {
		if !maps.Equal(got[id], wantParents) {
			t.Errorf("%s parents = %v, want %v", id, got[id], wantParents)
		}
	}
}

func TestListEBSVolumes_RecordsTheAttachedInstance(t *testing.T) {
	attached := func(instances ...string) []ec2types.VolumeAttachment {
		attachments := make([]ec2types.VolumeAttachment, 0, len(instances))
		for _, id := range instances {
			attachments = append(attachments, ec2types.VolumeAttachment{InstanceId: aws.String(id)})
		}
		return attachments
	}
	mock := &mockVolumesClient{volumes: []ec2types.Volume{
		{VolumeId: aws.String("vol-attached"), Attachments: attached("i-1"), Tags: nameTag("data")},
		{VolumeId: aws.String("vol-free")},
		{VolumeId: aws.String("vol-multi"), Attachments: attached("i-1", "i-2")},
	}}

	resources, err := testProvider().listEBSVolumesFrom(context.Background(), mock, defaultRegion)
	assertParents(t, parentsByID(t, resources, err), map[string]map[string]string{
		"vol-attached": {types.RelationAttachedInstance: testInstanceARN},
		"vol-free":     nil,
		"vol-multi":    nil,
	})
	if resources[0].Name != "data" || resources[0].Type != "aws_ebs_volume" || resources[0].ARN != "arn:aws:ec2:us-east-1:123456789012:volume/vol-attached" {
		t.Errorf("unexpected volume: %+v", resources[0])
	}
}

func TestListEBSVolumes_ListError(t *testing.T) {
	mock := &mockVolumesClient{err: errors.New("boom")}
	if _, err := testProvider().listEBSVolumesFrom(context.Background(), mock, defaultRegion); err == nil {
		t.Fatal("expected error")
	}
}

func TestListEC2Instances_RecordsTheVPC(t *testing.T) {
	mock := &mockInstancesClient{instances: []ec2types.Instance{
		{InstanceId: aws.String("i-1"), VpcId: aws.String("vpc-1"), Tags: nameTag("web")},
		{InstanceId: aws.String("i-gone"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameTerminated}},
	}}

	resources, err := testProvider().listEC2InstancesFrom(context.Background(), mock, defaultRegion)
	assertParents(t, parentsByID(t, resources, err), map[string]map[string]string{
		"i-1": {types.RelationVPC: testVPCARN},
	})
	if resources[0].Name != "web" || resources[0].ARN != testInstanceARN {
		t.Errorf("unexpected instance: %+v", resources[0])
	}
}

func TestListEC2Instances_ListError(t *testing.T) {
	mock := &mockInstancesClient{err: errors.New("boom")}
	if _, err := testProvider().listEC2InstancesFrom(context.Background(), mock, defaultRegion); err == nil {
		t.Fatal("expected error")
	}
}

func TestListEBSSnapshots_RecordsTheSourceVolume(t *testing.T) {
	mock := &mockSnapshotsClient{pages: [][]ec2types.Snapshot{{
		{SnapshotId: aws.String("snap-1"), VolumeId: aws.String("vol-1")},
		{SnapshotId: aws.String("snap-copied"), VolumeId: aws.String("vol-ffffffff")},
	}}}

	resources, err := testProvider().listEBSSnapshotsFrom(context.Background(), mock, defaultRegion)
	assertParents(t, parentsByID(t, resources, err), map[string]map[string]string{
		"snap-1":      {types.RelationSourceVolume: "arn:aws:ec2:us-east-1:123456789012:volume/vol-1"},
		"snap-copied": nil,
	})
}

func TestNetworkListers_RecordTheVPC(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	vpc := map[string]string{types.RelationVPC: testVPCARN}

	subnets, err := p.listSubnetsFrom(ctx, &mockSubnetsClient{subnets: []ec2types.Subnet{
		{SubnetId: aws.String("subnet-1"), VpcId: aws.String("vpc-1")},
	}}, defaultRegion)
	assertParents(t, parentsByID(t, subnets, err), map[string]map[string]string{"subnet-1": vpc})

	groups, err := p.listSecurityGroupsFrom(ctx, &mockSecurityGroupsClient{pages: [][]ec2types.SecurityGroup{{
		{GroupId: aws.String("sg-1"), VpcId: aws.String("vpc-1")},
	}}}, defaultRegion)
	assertParents(t, parentsByID(t, groups, err), map[string]map[string]string{"sg-1": vpc})

	extra := &mockEC2ExtraClient{
		addresses: []ec2types.Address{
			{AllocationId: aws.String("eipalloc-used"), InstanceId: aws.String("i-1")},
			{AllocationId: aws.String("eipalloc-free")},
		},
		nats: []ec2types.NatGateway{{NatGatewayId: aws.String("nat-1"), VpcId: aws.String("vpc-1")}},
		igws: []ec2types.InternetGateway{
			{InternetGatewayId: aws.String("igw-attached"), Attachments: []ec2types.InternetGatewayAttachment{{VpcId: aws.String("vpc-1")}}},
			{InternetGatewayId: aws.String("igw-detached")},
		},
		endpoints: []ec2types.VpcEndpoint{{VpcEndpointId: aws.String("vpce-1"), VpcId: aws.String("vpc-1")}},
	}

	eips, err := p.listElasticIPsFrom(ctx, extra, defaultRegion)
	assertParents(t, parentsByID(t, eips, err), map[string]map[string]string{
		"eipalloc-used": {types.RelationAttachedInstance: testInstanceARN},
		"eipalloc-free": nil,
	})

	nats, err := p.listNATGatewaysFrom(ctx, extra, defaultRegion)
	assertParents(t, parentsByID(t, nats, err), map[string]map[string]string{"nat-1": vpc})

	igws, err := p.listInternetGatewaysFrom(ctx, extra, defaultRegion)
	assertParents(t, parentsByID(t, igws, err), map[string]map[string]string{"igw-attached": vpc, "igw-detached": nil})

	endpoints, err := p.listVPCEndpointsFrom(ctx, extra, defaultRegion)
	assertParents(t, parentsByID(t, endpoints, err), map[string]map[string]string{"vpce-1": vpc})
}
