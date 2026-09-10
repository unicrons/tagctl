package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// rdsExtraAPI is the subset of the RDS API used to discover clusters and
// snapshots, which return their tags inline.
type rdsExtraAPI interface {
	DescribeDBClusters(ctx context.Context, params *rds.DescribeDBClustersInput, optFns ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error)
	DescribeDBSnapshots(ctx context.Context, params *rds.DescribeDBSnapshotsInput, optFns ...func(*rds.Options)) (*rds.DescribeDBSnapshotsOutput, error)
}

// listRDSClusters lists Aurora, DocumentDB and Neptune clusters in a region.
func (p *Provider) listRDSClusters(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listRDSClustersFrom(ctx, p.getRDSClient(region), region)
}

func (p *Provider) listRDSClustersFrom(ctx context.Context, client rdsExtraAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := rds.NewDescribeDBClustersPaginator(client, &rds.DescribeDBClustersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_rds_clusters", "", err)
		}
		for _, c := range output.DBClusters {
			id := aws.ToString(c.DBClusterIdentifier)
			resources = append(resources, types.Resource{
				ID:        id,
				Name:      id,
				ARN:       aws.ToString(c.DBClusterArn),
				Type:      dbClusterType(aws.ToString(c.Engine)),
				Region:    region,
				Account:   p.accountID,
				Provider:  "aws",
				Tags:      rdsTagListToMap(c.TagList),
				CreatedAt: c.ClusterCreateTime,
			})
		}
	}
	log.Debug("AWS RDS: Found %d clusters in %s", len(resources), region)
	return resources, nil
}

// listRDSSnapshots lists the manual DB snapshots in a region. Automated
// snapshots inherit their tags from the instance and cannot be tagged.
func (p *Provider) listRDSSnapshots(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listRDSSnapshotsFrom(ctx, p.getRDSClient(region), region)
}

func (p *Provider) listRDSSnapshotsFrom(ctx context.Context, client rdsExtraAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := rds.NewDescribeDBSnapshotsPaginator(client, &rds.DescribeDBSnapshotsInput{SnapshotType: aws.String("manual")})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_rds_snapshots", "", err)
		}
		for _, s := range output.DBSnapshots {
			id := aws.ToString(s.DBSnapshotIdentifier)
			resources = append(resources, types.Resource{
				ID:        id,
				Name:      id,
				ARN:       aws.ToString(s.DBSnapshotArn),
				Type:      "aws_db_snapshot",
				Region:    region,
				Account:   p.accountID,
				Provider:  "aws",
				Tags:      rdsTagListToMap(s.TagList),
				CreatedAt: s.SnapshotCreateTime,
			})
		}
	}
	log.Debug("AWS RDS: Found %d manual snapshots in %s", len(resources), region)
	return resources, nil
}

// rdsTagListToMap converts an RDS tag list to a map.
func rdsTagListToMap(tags []rdstypes.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

// dbClusterType maps the engine of a DescribeDBClusters entry to its resource
// type: the RDS API also serves DocumentDB and Neptune clusters.
func dbClusterType(engine string) string {
	switch engine {
	case "docdb":
		return "aws_docdb_cluster"
	case "neptune":
		return "aws_neptune_cluster"
	default:
		return "aws_rds_cluster"
	}
}
