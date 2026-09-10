package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/aws-sdk-go-v2/service/kafka"
	kafkatypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	ostypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
	sagemakertypes "github.com/aws/aws-sdk-go-v2/service/sagemaker/types"
)

// bulkProvider returns a test provider whose us-east-1 bulk tag source resolves to tags.
func bulkProvider(tags map[string]map[string]string) *Provider {
	p := testProvider()
	p.tagSources = map[string]*tagSource{defaultRegion: staticTagSource(tags)}
	return p
}

type mockRedshiftClient struct {
	clusters []redshifttypes.Cluster
	err      error
}

func (m *mockRedshiftClient) DescribeClusters(ctx context.Context, params *redshift.DescribeClustersInput, optFns ...func(*redshift.Options)) (*redshift.DescribeClustersOutput, error) {
	return &redshift.DescribeClustersOutput{Clusters: m.clusters}, m.err
}

type mockOpenSearchClient struct {
	names   []string
	err     error
	batches []int
}

func (m *mockOpenSearchClient) ListDomainNames(ctx context.Context, params *opensearch.ListDomainNamesInput, optFns ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := &opensearch.ListDomainNamesOutput{}
	for _, n := range m.names {
		out.DomainNames = append(out.DomainNames, ostypes.DomainInfo{DomainName: aws.String(n)})
	}
	return out, nil
}

func (m *mockOpenSearchClient) DescribeDomains(ctx context.Context, params *opensearch.DescribeDomainsInput, optFns ...func(*opensearch.Options)) (*opensearch.DescribeDomainsOutput, error) {
	m.batches = append(m.batches, len(params.DomainNames))
	out := &opensearch.DescribeDomainsOutput{}
	for _, n := range params.DomainNames {
		out.DomainStatusList = append(out.DomainStatusList, ostypes.DomainStatus{
			DomainName: aws.String(n),
			ARN:        aws.String("arn:aws:es:us-east-1:123456789012:domain/" + n),
		})
	}
	return out, nil
}

type mockMSKClient struct {
	clusters []kafkatypes.Cluster
	err      error
}

func (m *mockMSKClient) ListClustersV2(ctx context.Context, params *kafka.ListClustersV2Input, optFns ...func(*kafka.Options)) (*kafka.ListClustersV2Output, error) {
	return &kafka.ListClustersV2Output{ClusterInfoList: m.clusters}, m.err
}

type mockGlueClient struct {
	jobs []gluetypes.Job
	err  error
}

func (m *mockGlueClient) GetJobs(ctx context.Context, params *glue.GetJobsInput, optFns ...func(*glue.Options)) (*glue.GetJobsOutput, error) {
	return &glue.GetJobsOutput{Jobs: m.jobs}, m.err
}

type mockFirehoseClient struct {
	pages [][]string
	err   error
	calls int
}

func (m *mockFirehoseClient) ListDeliveryStreams(ctx context.Context, params *firehose.ListDeliveryStreamsInput, optFns ...func(*firehose.Options)) (*firehose.ListDeliveryStreamsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	page := m.pages[m.calls]
	m.calls++
	return &firehose.ListDeliveryStreamsOutput{
		DeliveryStreamNames:    page,
		HasMoreDeliveryStreams: aws.Bool(m.calls < len(m.pages)),
	}, nil
}

type mockSageMakerClient struct {
	endpoints []sagemakertypes.EndpointSummary
	notebooks []sagemakertypes.NotebookInstanceSummary
	err       error
}

func (m *mockSageMakerClient) ListEndpoints(ctx context.Context, params *sagemaker.ListEndpointsInput, optFns ...func(*sagemaker.Options)) (*sagemaker.ListEndpointsOutput, error) {
	return &sagemaker.ListEndpointsOutput{Endpoints: m.endpoints}, m.err
}

func (m *mockSageMakerClient) ListNotebookInstances(ctx context.Context, params *sagemaker.ListNotebookInstancesInput, optFns ...func(*sagemaker.Options)) (*sagemaker.ListNotebookInstancesOutput, error) {
	return &sagemaker.ListNotebookInstancesOutput{NotebookInstances: m.notebooks}, m.err
}

func TestListRedshiftClusters(t *testing.T) {
	mock := &mockRedshiftClient{clusters: []redshifttypes.Cluster{{
		ClusterIdentifier: aws.String("warehouse"),
		Tags:              []redshifttypes.Tag{{Key: aws.String("environment"), Value: aws.String(envProd)}},
	}}}
	resources, err := testProvider().listRedshiftClustersFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	if resources[0].ARN != "arn:aws:redshift:us-east-1:123456789012:cluster:warehouse" || resources[0].Tags["environment"] != envProd {
		t.Errorf("got %+v", resources[0])
	}
}

func TestListOpenSearchDomains(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	mock := &mockOpenSearchClient{names: names}
	p := bulkProvider(map[string]map[string]string{"arn:aws:es:us-east-1:123456789012:domain/a": {"owner": "x"}})

	resources, err := p.listOpenSearchDomainsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 7 {
		t.Fatalf("resources = %d, err = %v", len(resources), err)
	}
	if len(mock.batches) != 2 || mock.batches[0] != openSearchDescribeBatch || mock.batches[1] != 2 {
		t.Errorf("DescribeDomains batches = %v, want [5 2]", mock.batches)
	}
	if resources[0].Tags["owner"] != "x" || len(resources[1].Tags) != 0 {
		t.Errorf("tags: %+v / %+v", resources[0].Tags, resources[1].Tags)
	}
}

func TestListMSKClusters(t *testing.T) {
	mock := &mockMSKClient{clusters: []kafkatypes.Cluster{
		{ClusterName: aws.String("events"), ClusterArn: aws.String("arn:msk:events"), Tags: map[string]string{"owner": "x"}},
		{ClusterName: aws.String("bare"), ClusterArn: aws.String("arn:msk:bare")},
	}}
	resources, err := testProvider().listMSKClustersFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || resources[0].Tags["owner"] != "x" || resources[1].Tags == nil {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListGlueJobs(t *testing.T) {
	arn := "arn:aws:glue:us-east-1:123456789012:job/etl"
	p := bulkProvider(map[string]map[string]string{arn: {"environment": envProd}})
	resources, err := p.listGlueJobsFrom(context.Background(), &mockGlueClient{jobs: []gluetypes.Job{{Name: aws.String("etl")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != arn || resources[0].Tags["environment"] != envProd {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListFirehoseStreams_Paginates(t *testing.T) {
	mock := &mockFirehoseClient{pages: [][]string{{"s1", "s2"}, {"s3"}}}
	resources, err := bulkProvider(nil).listFirehoseStreamsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 3 || mock.calls != 2 {
		t.Fatalf("resources = %d, calls = %d, err = %v", len(resources), mock.calls, err)
	}
	if resources[2].ARN != "arn:aws:firehose:us-east-1:123456789012:deliverystream/s3" {
		t.Errorf("ARN = %q", resources[2].ARN)
	}
}

func TestListSageMakerResources(t *testing.T) {
	mock := &mockSageMakerClient{
		endpoints: []sagemakertypes.EndpointSummary{{EndpointName: aws.String("infer"), EndpointArn: aws.String("arn:sm:endpoint/infer")}},
		notebooks: []sagemakertypes.NotebookInstanceSummary{{NotebookInstanceName: aws.String("lab"), NotebookInstanceArn: aws.String("arn:sm:notebook/lab")}},
	}
	p := bulkProvider(map[string]map[string]string{"arn:sm:notebook/lab": {"owner": "x"}})
	resources, err := p.listSageMakerResourcesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	if resources[0].Type != "aws_sagemaker_endpoint" || resources[1].Type != "aws_sagemaker_notebook_instance" || resources[1].Tags["owner"] != "x" {
		t.Errorf("got %+v", resources)
	}
}

func TestDataListers_SkipWithoutBulkTags(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	for name, list := range map[string]func() ([]int, error){
		"opensearch": func() ([]int, error) {
			r, err := p.listOpenSearchDomainsFrom(ctx, &mockOpenSearchClient{names: []string{"a"}}, defaultRegion)
			return make([]int, len(r)), err
		},
		"glue": func() ([]int, error) {
			r, err := p.listGlueJobsFrom(ctx, &mockGlueClient{jobs: []gluetypes.Job{{Name: aws.String("x")}}}, defaultRegion)
			return make([]int, len(r)), err
		},
		"firehose": func() ([]int, error) {
			r, err := p.listFirehoseStreamsFrom(ctx, &mockFirehoseClient{pages: [][]string{{"s"}}}, defaultRegion)
			return make([]int, len(r)), err
		},
		"sagemaker": func() ([]int, error) {
			r, err := p.listSageMakerResourcesFrom(ctx, &mockSageMakerClient{}, defaultRegion)
			return make([]int, len(r)), err
		},
	} {
		if r, err := list(); err != nil || len(r) != 0 {
			t.Errorf("%s: want no resources and no error without bulk tags, got %d, %v", name, len(r), err)
		}
	}
}

func TestDataListers_Error(t *testing.T) {
	boom := errors.New("boom")
	p := bulkProvider(nil)
	ctx := context.Background()
	for name, list := range map[string]func() error{
		"redshift": func() error {
			_, err := p.listRedshiftClustersFrom(ctx, &mockRedshiftClient{err: boom}, defaultRegion)
			return err
		},
		"opensearch": func() error {
			_, err := p.listOpenSearchDomainsFrom(ctx, &mockOpenSearchClient{err: boom}, defaultRegion)
			return err
		},
		"msk": func() error {
			_, err := p.listMSKClustersFrom(ctx, &mockMSKClient{err: boom}, defaultRegion)
			return err
		},
		"glue": func() error { _, err := p.listGlueJobsFrom(ctx, &mockGlueClient{err: boom}, defaultRegion); return err },
		"firehose": func() error {
			_, err := p.listFirehoseStreamsFrom(ctx, &mockFirehoseClient{err: boom}, defaultRegion)
			return err
		},
		"sagemaker": func() error {
			_, err := p.listSageMakerResourcesFrom(ctx, &mockSageMakerClient{err: boom}, defaultRegion)
			return err
		},
	} {
		if list() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
