package aws

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// mockRDSInstanceClient serves DescribeDBInstances pages and per-instance tags.
type mockRDSInstanceClient struct {
	pages   [][]rdstypes.DBInstance
	tags    map[string][]rdstypes.Tag
	listErr error
	tagErr  map[string]bool
	calls   int
}

func (m *mockRDSInstanceClient) DescribeDBInstances(ctx context.Context, params *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &rds.DescribeDBInstancesOutput{DBInstances: page}
	if m.calls < len(m.pages) {
		out.Marker = aws.String("next")
	}
	return out, nil
}

func (m *mockRDSInstanceClient) ListTagsForResource(ctx context.Context, params *rds.ListTagsForResourceInput, optFns ...func(*rds.Options)) (*rds.ListTagsForResourceOutput, error) {
	arn := aws.ToString(params.ResourceName)
	if m.tagErr[arn] {
		return nil, errors.New("access denied")
	}
	return &rds.ListTagsForResourceOutput{TagList: m.tags[arn]}, nil
}

func dbInstance(id string, created *time.Time) rdstypes.DBInstance {
	return rdstypes.DBInstance{
		DBInstanceIdentifier: aws.String(id),
		DBInstanceArn:        aws.String("arn:aws:rds:us-east-1:123456789012:db:" + id),
		InstanceCreateTime:   created,
	}
}

func TestListRDSInstances(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mock := &mockRDSInstanceClient{
		pages: [][]rdstypes.DBInstance{{dbInstance("orders", &created)}, {dbInstance("reports", nil)}},
		tags: map[string][]rdstypes.Tag{
			"arn:aws:rds:us-east-1:123456789012:db:orders": {{Key: aws.String("environment"), Value: aws.String(envProd)}},
		},
	}

	resources, err := testProvider().listRDSInstancesFrom(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("listRDSInstancesFrom() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d instances across 2 pages, want 2", len(resources))
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	orders := resources[0]
	if orders.ID != "orders" || orders.Type != "aws_db_instance" || orders.ARN != "arn:aws:rds:us-east-1:123456789012:db:orders" {
		t.Errorf("orders = %+v", orders)
	}
	if orders.Tags["environment"] != envProd {
		t.Errorf("orders tags = %v", orders.Tags)
	}
	if orders.CreatedAt == nil || !orders.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", orders.CreatedAt, created)
	}
	if resources[1].Tags == nil || len(resources[1].Tags) != 0 {
		t.Errorf("untagged instance tags = %v, want an empty map", resources[1].Tags)
	}
}

func TestListRDSInstances_UnreadableTagsSkipInstanceNotReportUntagged(t *testing.T) {
	mock := &mockRDSInstanceClient{
		pages:  [][]rdstypes.DBInstance{{dbInstance("orders", nil), dbInstance("denied", nil)}},
		tagErr: map[string]bool{"arn:aws:rds:us-east-1:123456789012:db:denied": true},
	}

	p := testProvider()
	resources, err := p.listRDSInstancesFrom(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("listRDSInstancesFrom() error = %v, want nil", err)
	}
	if len(resources) != 1 || resources[0].ID != "orders" {
		t.Fatalf("resources = %+v, want only the readable instance", resources)
	}
	if errors.Join(p.skipped.errs()...) == nil {
		t.Error("the skipped instance was not recorded")
	}
}

func TestListRDSInstances_ListError(t *testing.T) {
	mock := &mockRDSInstanceClient{listErr: errors.New("boom")}

	if _, err := testProvider().listRDSInstancesFrom(context.Background(), mock, defaultRegion); err == nil {
		t.Fatal("listRDSInstancesFrom() error = nil, want an error")
	}
}
