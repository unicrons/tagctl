package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

type mockRDSExtraClient struct {
	clusters     []rdstypes.DBCluster
	snapshots    []rdstypes.DBSnapshot
	err          error
	snapshotType *string
}

func (m *mockRDSExtraClient) DescribeDBClusters(ctx context.Context, params *rds.DescribeDBClustersInput, optFns ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error) {
	return &rds.DescribeDBClustersOutput{DBClusters: m.clusters}, m.err
}

func (m *mockRDSExtraClient) DescribeDBSnapshots(ctx context.Context, params *rds.DescribeDBSnapshotsInput, optFns ...func(*rds.Options)) (*rds.DescribeDBSnapshotsOutput, error) {
	m.snapshotType = params.SnapshotType
	return &rds.DescribeDBSnapshotsOutput{DBSnapshots: m.snapshots}, m.err
}

func TestListRDSClustersAndSnapshots(t *testing.T) {
	mock := &mockRDSExtraClient{
		clusters: []rdstypes.DBCluster{{
			DBClusterIdentifier: aws.String("aurora"),
			DBClusterArn:        aws.String("arn:aws:rds:us-east-1:123456789012:cluster:aurora"),
			TagList:             []rdstypes.Tag{{Key: aws.String("environment"), Value: aws.String(envProd)}},
		}},
		snapshots: []rdstypes.DBSnapshot{{
			DBSnapshotIdentifier: aws.String("pre-migration"),
			DBSnapshotArn:        aws.String("arn:aws:rds:us-east-1:123456789012:snapshot:pre-migration"),
		}},
	}
	p := testProvider()

	clusters, err := p.listRDSClustersFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(clusters) != 1 || clusters[0].Type != "aws_rds_cluster" || clusters[0].Tags["environment"] != envProd {
		t.Errorf("clusters = %+v, err = %v", clusters, err)
	}

	snaps, err := p.listRDSSnapshotsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(snaps) != 1 || snaps[0].Type != "aws_db_snapshot" || len(snaps[0].Tags) != 0 {
		t.Errorf("snapshots = %+v, err = %v", snaps, err)
	}
	if aws.ToString(mock.snapshotType) != "manual" {
		t.Errorf("SnapshotType = %v, want manual (automated snapshots cannot be tagged)", mock.snapshotType)
	}
}

func TestListRDSClustersAndSnapshots_Error(t *testing.T) {
	mock := &mockRDSExtraClient{err: errors.New("boom")}
	if _, err := testProvider().listRDSClustersFrom(context.Background(), mock, defaultRegion); err == nil {
		t.Error("clusters: expected error")
	}
	if _, err := testProvider().listRDSSnapshotsFrom(context.Background(), mock, defaultRegion); err == nil {
		t.Error("snapshots: expected error")
	}
}
