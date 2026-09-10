package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type mockEKSClient struct {
	clusters    map[string]*ekstypes.Cluster
	listErr     error
	describeErr map[string]bool
}

func (m *mockEKSClient) ListClusters(ctx context.Context, params *eks.ListClustersInput, optFns ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := &eks.ListClustersOutput{}
	for name := range m.clusters {
		out.Clusters = append(out.Clusters, name)
	}
	return out, nil
}

func (m *mockEKSClient) DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	name := aws.ToString(params.Name)
	if m.describeErr[name] {
		return nil, errors.New("access denied")
	}
	return &eks.DescribeClusterOutput{Cluster: m.clusters[name]}, nil
}

func (m *mockEKSClient) TagResource(ctx context.Context, params *eks.TagResourceInput, optFns ...func(*eks.Options)) (*eks.TagResourceOutput, error) {
	return &eks.TagResourceOutput{}, nil
}

func TestListEKSClusters(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mock := &mockEKSClient{
		clusters: map[string]*ekstypes.Cluster{
			"prod": {
				Name:      aws.String("prod"),
				Arn:       aws.String("arn:aws:eks:us-east-1:123456789012:cluster/prod"),
				Tags:      map[string]string{"environment": envProd},
				CreatedAt: &created,
			},
			"bare": {Name: aws.String("bare"), Arn: aws.String("arn:aws:eks:us-east-1:123456789012:cluster/bare")},
		},
	}

	resources, err := testProvider().listEKSClustersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d clusters, want 2", len(resources))
	}
	for _, r := range resources {
		if r.Type != "aws_eks_cluster" || r.ARN == "" || r.Tags == nil {
			t.Errorf("unexpected resource: %+v", r)
		}
		if r.ID == "prod" && (r.Tags["environment"] != envProd || r.CreatedAt == nil || !r.CreatedAt.Equal(created)) {
			t.Errorf("prod cluster = %+v", r)
		}
	}
}

func TestListEKSClusters_DescribeErrorSkipsCluster(t *testing.T) {
	mock := &mockEKSClient{
		clusters: map[string]*ekstypes.Cluster{
			"ok":     {Name: aws.String("ok"), Arn: aws.String("arn:aws:eks:us-east-1:123456789012:cluster/ok")},
			"denied": {Name: aws.String("denied")},
		},
		describeErr: map[string]bool{"denied": true},
	}

	resources, err := testProvider().listEKSClustersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "ok" {
		t.Errorf("got %+v, want only the describable cluster", resources)
	}
}

func TestListEKSClusters_ListError(t *testing.T) {
	mock := &mockEKSClient{listErr: errors.New("boom")}
	if _, err := testProvider().listEKSClustersFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}
