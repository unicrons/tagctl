package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type mockRoute53Client struct {
	zones []r53types.HostedZone
	err   error
}

func (m *mockRoute53Client) ListHostedZones(ctx context.Context, params *route53.ListHostedZonesInput, optFns ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error) {
	return &route53.ListHostedZonesOutput{HostedZones: m.zones}, m.err
}

type mockCloudFrontClient struct {
	items []cftypes.DistributionSummary
	err   error
}

func (m *mockCloudFrontClient) ListDistributions(ctx context.Context, params *cloudfront.ListDistributionsInput, optFns ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &cloudfront.ListDistributionsOutput{DistributionList: &cftypes.DistributionList{Items: m.items, IsTruncated: aws.Bool(false)}}, nil
}

type mockIAMClient struct {
	roles []iamtypes.Role
	err   error
}

func (m *mockIAMClient) ListRoles(ctx context.Context, params *iam.ListRolesInput, optFns ...func(*iam.Options)) (*iam.ListRolesOutput, error) {
	return &iam.ListRolesOutput{Roles: m.roles, IsTruncated: false}, m.err
}

func TestListHostedZones(t *testing.T) {
	arn := "arn:aws:route53:::hostedzone/Z123"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	mock := &mockRoute53Client{zones: []r53types.HostedZone{{Id: aws.String("/hostedzone/Z123"), Name: aws.String("example.com.")}}}

	resources, err := p.listHostedZonesFrom(context.Background(), mock)
	if err != nil || len(resources) != 1 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	r := resources[0]
	if r.ID != "Z123" || r.Name != "example.com" || r.ARN != arn || r.Region != "global" || r.Tags["owner"] != "x" {
		t.Errorf("got %+v", r)
	}
}

func TestListDistributions(t *testing.T) {
	arn := "arn:aws:cloudfront::123456789012:distribution/E1"
	p := bulkProvider(map[string]map[string]string{arn: {"environment": envProd}})
	mock := &mockCloudFrontClient{items: []cftypes.DistributionSummary{{Id: aws.String("E1"), ARN: aws.String(arn), DomainName: aws.String("d1.cloudfront.net")}}}

	resources, err := p.listDistributionsFrom(context.Background(), mock)
	if err != nil || len(resources) != 1 || resources[0].Name != "d1.cloudfront.net" || resources[0].Tags["environment"] != envProd || resources[0].Region != "global" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListIAMRoles_SkipsServiceLinked(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:aws:iam::123456789012:role/deploy": {"owner": "x"}})
	mock := &mockIAMClient{roles: []iamtypes.Role{
		{RoleName: aws.String("deploy"), Arn: aws.String("arn:aws:iam::123456789012:role/deploy"), Path: aws.String("/")},
		{RoleName: aws.String("AWSServiceRoleForECS"), Arn: aws.String("arn:aws:iam::123456789012:role/aws-service-role/ecs.amazonaws.com/AWSServiceRoleForECS"), Path: aws.String("/aws-service-role/ecs.amazonaws.com/")},
	}}

	resources, err := p.listIAMRolesFrom(context.Background(), mock)
	if err != nil || len(resources) != 1 || resources[0].Name != "deploy" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestGlobalListers_SkipWithoutBulkTags(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	zones, err := p.listHostedZonesFrom(ctx, &mockRoute53Client{zones: []r53types.HostedZone{{Id: aws.String("/hostedzone/Z")}}})
	if err != nil || len(zones) != 0 {
		t.Errorf("route53: got %d, %v", len(zones), err)
	}
	dists, err := p.listDistributionsFrom(ctx, &mockCloudFrontClient{items: []cftypes.DistributionSummary{{Id: aws.String("E")}}})
	if err != nil || len(dists) != 0 {
		t.Errorf("cloudfront: got %d, %v", len(dists), err)
	}
	roles, err := p.listIAMRolesFrom(ctx, &mockIAMClient{roles: []iamtypes.Role{{RoleName: aws.String("r")}}})
	if err != nil || len(roles) != 0 {
		t.Errorf("iam: got %d, %v", len(roles), err)
	}
}

func TestGlobalListers_Error(t *testing.T) {
	boom := errors.New("boom")
	p := bulkProvider(nil)
	ctx := context.Background()
	if _, err := p.listHostedZonesFrom(ctx, &mockRoute53Client{err: boom}); err == nil {
		t.Error("route53: expected error")
	}
	if _, err := p.listDistributionsFrom(ctx, &mockCloudFrontClient{err: boom}); err == nil {
		t.Error("cloudfront: expected error")
	}
	if _, err := p.listIAMRolesFrom(ctx, &mockIAMClient{err: boom}); err == nil {
		t.Error("iam: expected error")
	}
}
