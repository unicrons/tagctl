package aws

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	dms "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice"
	dmstypes "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice/types"
	"github.com/aws/aws-sdk-go-v2/service/datapipeline"
	datapipelinetypes "github.com/aws/aws-sdk-go-v2/service/datapipeline/types"
	"github.com/aws/aws-sdk-go-v2/service/datasync"
	datasynctypes "github.com/aws/aws-sdk-go-v2/service/datasync/types"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
	glaciertypes "github.com/aws/aws-sdk-go-v2/service/glacier/types"
	"github.com/aws/aws-sdk-go-v2/service/memorydb"
	memorydbtypes "github.com/aws/aws-sdk-go-v2/service/memorydb/types"
	"github.com/aws/aws-sdk-go-v2/service/mq"
	mqtypes "github.com/aws/aws-sdk-go-v2/service/mq/types"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/aws-sdk-go-v2/service/storagegateway"
	sgwtypes "github.com/aws/aws-sdk-go-v2/service/storagegateway/types"
	"github.com/aws/aws-sdk-go-v2/service/transfer"
	transfertypes "github.com/aws/aws-sdk-go-v2/service/transfer/types"
)

type mockAthenaClient struct {
	groups []athenatypes.WorkGroupSummary
	err    error
}

func (m *mockAthenaClient) ListWorkGroups(ctx context.Context, params *athena.ListWorkGroupsInput, optFns ...func(*athena.Options)) (*athena.ListWorkGroupsOutput, error) {
	return &athena.ListWorkGroupsOutput{WorkGroups: m.groups}, m.err
}

type mockDMSClient struct {
	instances []dmstypes.ReplicationInstance
	err       error
}

func (m *mockDMSClient) DescribeReplicationInstances(ctx context.Context, params *dms.DescribeReplicationInstancesInput, optFns ...func(*dms.Options)) (*dms.DescribeReplicationInstancesOutput, error) {
	return &dms.DescribeReplicationInstancesOutput{ReplicationInstances: m.instances}, m.err
}

type mockDataPipelineClient struct {
	mu      sync.Mutex
	ids     []string
	tags    map[string][]datapipelinetypes.Tag
	err     error
	batches []int
}

func (m *mockDataPipelineClient) ListPipelines(ctx context.Context, params *datapipeline.ListPipelinesInput, optFns ...func(*datapipeline.Options)) (*datapipeline.ListPipelinesOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := &datapipeline.ListPipelinesOutput{}
	for _, id := range m.ids {
		out.PipelineIdList = append(out.PipelineIdList, datapipelinetypes.PipelineIdName{Id: aws.String(id), Name: aws.String("p-" + id)})
	}
	return out, nil
}

func (m *mockDataPipelineClient) DescribePipelines(ctx context.Context, params *datapipeline.DescribePipelinesInput, optFns ...func(*datapipeline.Options)) (*datapipeline.DescribePipelinesOutput, error) {
	m.mu.Lock()
	m.batches = append(m.batches, len(params.PipelineIds))
	m.mu.Unlock()
	out := &datapipeline.DescribePipelinesOutput{}
	for _, id := range params.PipelineIds {
		out.PipelineDescriptionList = append(out.PipelineDescriptionList, datapipelinetypes.PipelineDescription{PipelineId: aws.String(id), Name: aws.String("p-" + id), Tags: m.tags[id]})
	}
	return out, nil
}

type mockDataSyncClient struct {
	tasks []datasynctypes.TaskListEntry
	err   error
}

func (m *mockDataSyncClient) ListTasks(ctx context.Context, params *datasync.ListTasksInput, optFns ...func(*datasync.Options)) (*datasync.ListTasksOutput, error) {
	return &datasync.ListTasksOutput{Tasks: m.tasks}, m.err
}

type mockEMRClient struct {
	clusters []emrtypes.ClusterSummary
	err      error
	states   []emrtypes.ClusterState
}

func (m *mockEMRClient) ListClusters(ctx context.Context, params *emr.ListClustersInput, optFns ...func(*emr.Options)) (*emr.ListClustersOutput, error) {
	m.states = params.ClusterStates
	return &emr.ListClustersOutput{Clusters: m.clusters}, m.err
}

type mockGlacierClient struct {
	vaults []glaciertypes.DescribeVaultOutput
	err    error
}

func (m *mockGlacierClient) ListVaults(ctx context.Context, params *glacier.ListVaultsInput, optFns ...func(*glacier.Options)) (*glacier.ListVaultsOutput, error) {
	return &glacier.ListVaultsOutput{VaultList: m.vaults}, m.err
}

type mockMemoryDBClient struct {
	clusters []memorydbtypes.Cluster
	err      error
}

func (m *mockMemoryDBClient) DescribeClusters(ctx context.Context, params *memorydb.DescribeClustersInput, optFns ...func(*memorydb.Options)) (*memorydb.DescribeClustersOutput, error) {
	return &memorydb.DescribeClustersOutput{Clusters: m.clusters}, m.err
}

type mockMQClient struct {
	brokers []mqtypes.BrokerSummary
	err     error
}

func (m *mockMQClient) ListBrokers(ctx context.Context, params *mq.ListBrokersInput, optFns ...func(*mq.Options)) (*mq.ListBrokersOutput, error) {
	return &mq.ListBrokersOutput{BrokerSummaries: m.brokers}, m.err
}

type mockSESClient struct {
	identities []sesv2types.IdentityInfo
	sets       []string
	err        error
}

func (m *mockSESClient) ListEmailIdentities(ctx context.Context, params *sesv2.ListEmailIdentitiesInput, optFns ...func(*sesv2.Options)) (*sesv2.ListEmailIdentitiesOutput, error) {
	return &sesv2.ListEmailIdentitiesOutput{EmailIdentities: m.identities}, m.err
}

func (m *mockSESClient) ListConfigurationSets(ctx context.Context, params *sesv2.ListConfigurationSetsInput, optFns ...func(*sesv2.Options)) (*sesv2.ListConfigurationSetsOutput, error) {
	return &sesv2.ListConfigurationSetsOutput{ConfigurationSets: m.sets}, m.err
}

type mockStorageGatewayClient struct {
	gateways []sgwtypes.GatewayInfo
	err      error
}

func (m *mockStorageGatewayClient) ListGateways(ctx context.Context, params *storagegateway.ListGatewaysInput, optFns ...func(*storagegateway.Options)) (*storagegateway.ListGatewaysOutput, error) {
	return &storagegateway.ListGatewaysOutput{Gateways: m.gateways}, m.err
}

type mockTransferClient struct {
	servers []transfertypes.ListedServer
	err     error
}

func (m *mockTransferClient) ListServers(ctx context.Context, params *transfer.ListServersInput, optFns ...func(*transfer.Options)) (*transfer.ListServersOutput, error) {
	return &transfer.ListServersOutput{Servers: m.servers}, m.err
}

func TestListWorkGroups(t *testing.T) {
	arn := "arn:aws:athena:us-east-1:123456789012:workgroup/primary"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	resources, err := p.listWorkGroupsFrom(context.Background(), &mockAthenaClient{groups: []athenatypes.WorkGroupSummary{{Name: aws.String("primary")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != arn || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListReplicationInstances(t *testing.T) {
	p := bulkProvider(nil)
	mock := &mockDMSClient{instances: []dmstypes.ReplicationInstance{{ReplicationInstanceIdentifier: aws.String("mig"), ReplicationInstanceArn: aws.String("arn:dms:mig")}}}
	resources, err := p.listReplicationInstancesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_dms_replication_instance" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListDataPipelines_Batches(t *testing.T) {
	ids := make([]string, 0, dataPipelineDescribeBatch+1)
	for i := 0; i <= dataPipelineDescribeBatch; i++ {
		ids = append(ids, "df-"+string(rune('a'+i)))
	}
	mock := &mockDataPipelineClient{ids: ids, tags: map[string][]datapipelinetypes.Tag{"df-a": {{Key: aws.String("owner"), Value: aws.String("x")}}}}
	resources, err := testProvider().listDataPipelinesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != dataPipelineDescribeBatch+1 {
		t.Fatalf("resources = %d, err = %v", len(resources), err)
	}
	if len(mock.batches) != 2 {
		t.Errorf("DescribePipelines batches = %v, want two", mock.batches)
	}
	tagged := 0
	for _, r := range resources {
		if r.Tags["owner"] == "x" {
			tagged++
		}
	}
	if tagged != 1 {
		t.Errorf("tagged pipelines = %d, want 1", tagged)
	}
}

func TestListDataSyncTasks(t *testing.T) {
	arn := "arn:aws:datasync:us-east-1:123456789012:task/task-1"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	resources, err := p.listDataSyncTasksFrom(context.Background(), &mockDataSyncClient{tasks: []datasynctypes.TaskListEntry{{TaskArn: aws.String(arn), Name: aws.String("nightly")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ID != "task-1" || resources[0].Name != "nightly" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListEMRClusters_ActiveOnly(t *testing.T) {
	p := bulkProvider(nil)
	mock := &mockEMRClient{clusters: []emrtypes.ClusterSummary{{Id: aws.String("j-1"), Name: aws.String("spark"), ClusterArn: aws.String("arn:emr:j-1")}}}
	resources, err := p.listEMRClustersFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_emr_cluster" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
	if len(mock.states) != len(emrActiveStates) {
		t.Errorf("ListClusters states = %v, want the active set", mock.states)
	}
}

func TestListVaults(t *testing.T) {
	p := bulkProvider(nil)
	mock := &mockGlacierClient{vaults: []glaciertypes.DescribeVaultOutput{{VaultName: aws.String("archive"), VaultARN: aws.String("arn:glacier:archive"), CreationDate: aws.String("2026-01-02T03:04:05.000Z")}}}
	resources, err := p.listVaultsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].CreatedAt == nil {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListMemoryDBClusters(t *testing.T) {
	p := bulkProvider(nil)
	resources, err := p.listMemoryDBClustersFrom(context.Background(), &mockMemoryDBClient{clusters: []memorydbtypes.Cluster{{Name: aws.String("cache"), ARN: aws.String("arn:memorydb:cache")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_memorydb_cluster" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListBrokers(t *testing.T) {
	p := bulkProvider(nil)
	resources, err := p.listBrokersFrom(context.Background(), &mockMQClient{brokers: []mqtypes.BrokerSummary{{BrokerId: aws.String("b-1"), BrokerName: aws.String("events"), BrokerArn: aws.String("arn:mq:b-1")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ID != "b-1" || resources[0].Name != "events" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListSESResources(t *testing.T) {
	identity := "arn:aws:ses:us-east-1:123456789012:identity/example.com"
	p := bulkProvider(map[string]map[string]string{identity: {"owner": "x"}})
	mock := &mockSESClient{identities: []sesv2types.IdentityInfo{{IdentityName: aws.String("example.com")}}, sets: []string{"transactional"}}
	resources, err := p.listSESResourcesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	if resources[0].ARN != identity || resources[0].Tags["owner"] != "x" || resources[1].ARN != "arn:aws:ses:us-east-1:123456789012:configuration-set/transactional" {
		t.Errorf("got %+v", resources)
	}
}

func TestListGateways(t *testing.T) {
	p := bulkProvider(nil)
	resources, err := p.listGatewaysFrom(context.Background(), &mockStorageGatewayClient{gateways: []sgwtypes.GatewayInfo{{GatewayId: aws.String("sgw-1"), GatewayName: aws.String("office"), GatewayARN: aws.String("arn:sgw:1")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Name != "office" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListTransferServers(t *testing.T) {
	p := bulkProvider(nil)
	resources, err := p.listTransferServersFrom(context.Background(), &mockTransferClient{servers: []transfertypes.ListedServer{{ServerId: aws.String("s-1"), Arn: aws.String("arn:transfer:s-1")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_transfer_server" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestDataExtraListers_SkipWithoutBulkTags(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	for name, list := range map[string]func() (int, error){
		"athena": func() (int, error) {
			r, err := p.listWorkGroupsFrom(ctx, &mockAthenaClient{groups: []athenatypes.WorkGroupSummary{{Name: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"dms": func() (int, error) {
			r, err := p.listReplicationInstancesFrom(ctx, &mockDMSClient{instances: []dmstypes.ReplicationInstance{{ReplicationInstanceIdentifier: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"datasync": func() (int, error) {
			r, err := p.listDataSyncTasksFrom(ctx, &mockDataSyncClient{tasks: []datasynctypes.TaskListEntry{{TaskArn: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"emr": func() (int, error) {
			r, err := p.listEMRClustersFrom(ctx, &mockEMRClient{clusters: []emrtypes.ClusterSummary{{Id: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"glacier": func() (int, error) {
			r, err := p.listVaultsFrom(ctx, &mockGlacierClient{vaults: []glaciertypes.DescribeVaultOutput{{VaultName: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"memorydb": func() (int, error) {
			r, err := p.listMemoryDBClustersFrom(ctx, &mockMemoryDBClient{clusters: []memorydbtypes.Cluster{{Name: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"mq": func() (int, error) {
			r, err := p.listBrokersFrom(ctx, &mockMQClient{brokers: []mqtypes.BrokerSummary{{BrokerId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"ses": func() (int, error) {
			r, err := p.listSESResourcesFrom(ctx, &mockSESClient{sets: []string{"x"}}, defaultRegion)
			return len(r), err
		},
		"storagegateway": func() (int, error) {
			r, err := p.listGatewaysFrom(ctx, &mockStorageGatewayClient{gateways: []sgwtypes.GatewayInfo{{GatewayId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"transfer": func() (int, error) {
			r, err := p.listTransferServersFrom(ctx, &mockTransferClient{servers: []transfertypes.ListedServer{{ServerId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
	} {
		if n, err := list(); err != nil || n != 0 {
			t.Errorf("%s: want no resources and no error without bulk tags, got %d, %v", name, n, err)
		}
	}
}

func TestDataExtraListers_Error(t *testing.T) {
	boom := errors.New("boom")
	p := bulkProvider(nil)
	ctx := context.Background()
	for name, list := range map[string]func() error{
		"athena": func() error {
			_, err := p.listWorkGroupsFrom(ctx, &mockAthenaClient{err: boom}, defaultRegion)
			return err
		},
		"dms": func() error {
			_, err := p.listReplicationInstancesFrom(ctx, &mockDMSClient{err: boom}, defaultRegion)
			return err
		},
		"datapipeline": func() error {
			_, err := p.listDataPipelinesFrom(ctx, &mockDataPipelineClient{err: boom}, defaultRegion)
			return err
		},
		"datasync": func() error {
			_, err := p.listDataSyncTasksFrom(ctx, &mockDataSyncClient{err: boom}, defaultRegion)
			return err
		},
		"emr": func() error {
			_, err := p.listEMRClustersFrom(ctx, &mockEMRClient{err: boom}, defaultRegion)
			return err
		},
		"glacier": func() error {
			_, err := p.listVaultsFrom(ctx, &mockGlacierClient{err: boom}, defaultRegion)
			return err
		},
		"memorydb": func() error {
			_, err := p.listMemoryDBClustersFrom(ctx, &mockMemoryDBClient{err: boom}, defaultRegion)
			return err
		},
		"mq": func() error { _, err := p.listBrokersFrom(ctx, &mockMQClient{err: boom}, defaultRegion); return err },
		"ses": func() error {
			_, err := p.listSESResourcesFrom(ctx, &mockSESClient{err: boom}, defaultRegion)
			return err
		},
		"storagegateway": func() error {
			_, err := p.listGatewaysFrom(ctx, &mockStorageGatewayClient{err: boom}, defaultRegion)
			return err
		},
		"transfer": func() error {
			_, err := p.listTransferServersFrom(ctx, &mockTransferClient{err: boom}, defaultRegion)
			return err
		},
	} {
		if list() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
