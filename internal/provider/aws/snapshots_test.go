package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type mockSnapshotsClient struct {
	pages   [][]ec2types.Snapshot
	listErr error
	calls   int
	owners  []string
}

func (m *mockSnapshotsClient) DescribeSnapshots(ctx context.Context, params *ec2.DescribeSnapshotsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSnapshotsOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.owners = params.OwnerIds
	page := m.pages[m.calls]
	m.calls++
	out := &ec2.DescribeSnapshotsOutput{Snapshots: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func TestListEBSSnapshots(t *testing.T) {
	mock := &mockSnapshotsClient{
		pages: [][]ec2types.Snapshot{
			{{SnapshotId: aws.String("snap-1"), Tags: []ec2types.Tag{
				{Key: aws.String("Name"), Value: aws.String("db-backup")},
				{Key: aws.String("environment"), Value: aws.String(envProd)},
			}}},
			{{SnapshotId: aws.String("snap-2")}},
		},
	}

	resources, err := testProvider().listEBSSnapshotsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d snapshots across 2 pages, want 2", len(resources))
	}
	if !equalStrings(mock.owners, []string{"self"}) {
		t.Errorf("OwnerIds = %v, want [self] so public snapshots are not listed", mock.owners)
	}
	first := resources[0]
	if first.Name != "db-backup" || first.Type != "aws_ebs_snapshot" || first.Tags["environment"] != envProd {
		t.Errorf("unexpected resource: %+v", first)
	}
	if first.ARN != "arn:aws:ec2:us-east-1:123456789012:snapshot/snap-1" {
		t.Errorf("ARN = %q", first.ARN)
	}
	if resources[1].Name != "snap-2" {
		t.Errorf("snapshot without Name tag should use its ID, got %q", resources[1].Name)
	}
}

func TestListEBSSnapshots_ListError(t *testing.T) {
	mock := &mockSnapshotsClient{listErr: errors.New("boom")}
	if _, err := testProvider().listEBSSnapshotsFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}
