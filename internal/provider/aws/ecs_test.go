package aws

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type mockECSClient struct {
	clusters      map[string]ecstypes.Cluster   // by ARN
	services      map[string][]ecstypes.Service // by cluster ARN
	listErr       error
	servicesErrOn string
	describeBatch []int
}

func (m *mockECSClient) ListClusters(ctx context.Context, params *ecs.ListClustersInput, optFns ...func(*ecs.Options)) (*ecs.ListClustersOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	arns := make([]string, 0, len(m.clusters))
	for arn := range m.clusters {
		arns = append(arns, arn)
	}
	sort.Strings(arns)
	return &ecs.ListClustersOutput{ClusterArns: arns}, nil
}

func (m *mockECSClient) DescribeClusters(ctx context.Context, params *ecs.DescribeClustersInput, optFns ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
	if len(params.Include) != 1 || params.Include[0] != ecstypes.ClusterFieldTags {
		return nil, errors.New("tags not requested")
	}
	m.describeBatch = append(m.describeBatch, len(params.Clusters))
	out := &ecs.DescribeClustersOutput{}
	for _, arn := range params.Clusters {
		out.Clusters = append(out.Clusters, m.clusters[arn])
	}
	return out, nil
}

func (m *mockECSClient) ListServices(ctx context.Context, params *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error) {
	cluster := aws.ToString(params.Cluster)
	if cluster == m.servicesErrOn {
		return nil, errors.New("access denied")
	}
	out := &ecs.ListServicesOutput{}
	for _, s := range m.services[cluster] {
		out.ServiceArns = append(out.ServiceArns, aws.ToString(s.ServiceArn))
	}
	return out, nil
}

func (m *mockECSClient) DescribeServices(ctx context.Context, params *ecs.DescribeServicesInput, optFns ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	if len(params.Services) > ecsServicesPerDescribe {
		return nil, errors.New("too many services in one describe")
	}
	out := &ecs.DescribeServicesOutput{}
	for _, s := range m.services[aws.ToString(params.Cluster)] {
		for _, want := range params.Services {
			if aws.ToString(s.ServiceArn) == want {
				out.Services = append(out.Services, s)
			}
		}
	}
	return out, nil
}

func (m *mockECSClient) TagResource(ctx context.Context, params *ecs.TagResourceInput, optFns ...func(*ecs.Options)) (*ecs.TagResourceOutput, error) {
	return &ecs.TagResourceOutput{}, nil
}

const webCluster = "web"

func ecsService(cluster, name string) ecstypes.Service {
	return ecstypes.Service{
		ServiceName: aws.String(name),
		ServiceArn:  aws.String("arn:aws:ecs:us-east-1:123456789012:service/" + cluster + "/" + name),
	}
}

func TestListECSResources(t *testing.T) {
	web := "arn:aws:ecs:us-east-1:123456789012:cluster/web"
	batch := "arn:aws:ecs:us-east-1:123456789012:cluster/batch"

	services := make([]ecstypes.Service, 0, 12)
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		services = append(services, ecsService(webCluster, n))
	}
	services[0].Tags = []ecstypes.Tag{{Key: aws.String("owner"), Value: aws.String("web@example.com")}}

	mock := &mockECSClient{
		clusters: map[string]ecstypes.Cluster{
			web:   {ClusterName: aws.String(webCluster), ClusterArn: aws.String(web), Tags: []ecstypes.Tag{{Key: aws.String("environment"), Value: aws.String(envProd)}}},
			batch: {ClusterName: aws.String("batch"), ClusterArn: aws.String(batch)},
		},
		services: map[string][]ecstypes.Service{web: services},
	}

	resources, err := testProvider().listECSResourcesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var clusters, svcs int
	for _, r := range resources {
		switch r.Type {
		case "aws_ecs_cluster":
			clusters++
			if r.ID == webCluster && r.Tags["environment"] != envProd {
				t.Errorf("cluster tags = %v", r.Tags)
			}
		case "aws_ecs_service":
			svcs++
			if r.ID == "web/a" && r.Tags["owner"] != "web@example.com" {
				t.Errorf("service tags = %v", r.Tags)
			}
			if r.Name == r.ID {
				t.Errorf("service ID %q should be prefixed with its cluster", r.ID)
			}
		default:
			t.Errorf("unexpected type %q", r.Type)
		}
	}
	if clusters != 2 || svcs != 12 {
		t.Errorf("got %d clusters and %d services, want 2 and 12 (services described in batches of 10)", clusters, svcs)
	}
}

func TestListECSResources_ServiceErrorSkipsOnlyThatCluster(t *testing.T) {
	web := "arn:aws:ecs:us-east-1:123456789012:cluster/web"
	broken := "arn:aws:ecs:us-east-1:123456789012:cluster/broken"
	mock := &mockECSClient{
		clusters: map[string]ecstypes.Cluster{
			web:    {ClusterName: aws.String(webCluster), ClusterArn: aws.String(web)},
			broken: {ClusterName: aws.String("broken"), ClusterArn: aws.String(broken)},
		},
		services: map[string][]ecstypes.Service{
			web:    {ecsService(webCluster, "api")},
			broken: {ecsService("broken", "hidden")},
		},
		servicesErrOn: broken,
	}

	resources, err := testProvider().listECSResourcesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := make([]string, 0, len(resources))
	for _, r := range resources {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	want := []string{"broken", webCluster, "web/api"}
	if !equalStrings(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

func TestListECSResources_ListError(t *testing.T) {
	mock := &mockECSClient{listErr: errors.New("boom")}
	if _, err := testProvider().listECSResourcesFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestChunk(t *testing.T) {
	got := chunk([]string{"a", "b", "c", "d", "e"}, 2)
	if len(got) != 3 || len(got[0]) != 2 || len(got[2]) != 1 {
		t.Errorf("chunk = %v", got)
	}
	if chunk(nil, 2) != nil {
		t.Error("chunk(nil) should be nil")
	}
}
