package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	dms "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice"
	"github.com/aws/aws-sdk-go-v2/service/datapipeline"
	datapipelinetypes "github.com/aws/aws-sdk-go-v2/service/datapipeline/types"
	"github.com/aws/aws-sdk-go-v2/service/datasync"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
	"github.com/aws/aws-sdk-go-v2/service/memorydb"
	"github.com/aws/aws-sdk-go-v2/service/mq"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/storagegateway"
	"github.com/aws/aws-sdk-go-v2/service/transfer"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Data movement, analytics, messaging and storage services beyond data.go.

type athenaAPI interface {
	ListWorkGroups(ctx context.Context, params *athena.ListWorkGroupsInput, optFns ...func(*athena.Options)) (*athena.ListWorkGroupsOutput, error)
}

type dmsAPI interface {
	DescribeReplicationInstances(ctx context.Context, params *dms.DescribeReplicationInstancesInput, optFns ...func(*dms.Options)) (*dms.DescribeReplicationInstancesOutput, error)
}

type dataPipelineAPI interface {
	ListPipelines(ctx context.Context, params *datapipeline.ListPipelinesInput, optFns ...func(*datapipeline.Options)) (*datapipeline.ListPipelinesOutput, error)
	DescribePipelines(ctx context.Context, params *datapipeline.DescribePipelinesInput, optFns ...func(*datapipeline.Options)) (*datapipeline.DescribePipelinesOutput, error)
}

type dataSyncAPI interface {
	ListTasks(ctx context.Context, params *datasync.ListTasksInput, optFns ...func(*datasync.Options)) (*datasync.ListTasksOutput, error)
}

type emrAPI interface {
	ListClusters(ctx context.Context, params *emr.ListClustersInput, optFns ...func(*emr.Options)) (*emr.ListClustersOutput, error)
}

type glacierAPI interface {
	ListVaults(ctx context.Context, params *glacier.ListVaultsInput, optFns ...func(*glacier.Options)) (*glacier.ListVaultsOutput, error)
}

type memoryDBAPI interface {
	DescribeClusters(ctx context.Context, params *memorydb.DescribeClustersInput, optFns ...func(*memorydb.Options)) (*memorydb.DescribeClustersOutput, error)
}

type mqAPI interface {
	ListBrokers(ctx context.Context, params *mq.ListBrokersInput, optFns ...func(*mq.Options)) (*mq.ListBrokersOutput, error)
}

type sesAPI interface {
	ListEmailIdentities(ctx context.Context, params *sesv2.ListEmailIdentitiesInput, optFns ...func(*sesv2.Options)) (*sesv2.ListEmailIdentitiesOutput, error)
	ListConfigurationSets(ctx context.Context, params *sesv2.ListConfigurationSetsInput, optFns ...func(*sesv2.Options)) (*sesv2.ListConfigurationSetsOutput, error)
}

type storageGatewayAPI interface {
	ListGateways(ctx context.Context, params *storagegateway.ListGatewaysInput, optFns ...func(*storagegateway.Options)) (*storagegateway.ListGatewaysOutput, error)
}

type transferAPI interface {
	ListServers(ctx context.Context, params *transfer.ListServersInput, optFns ...func(*transfer.Options)) (*transfer.ListServersOutput, error)
}

// dataPipelineDescribeBatch is the maximum number of pipelines per DescribePipelines call.
const dataPipelineDescribeBatch = 25

// emrActiveStates limits ListClusters to clusters that still exist.
var emrActiveStates = []emrtypes.ClusterState{
	emrtypes.ClusterStateStarting, emrtypes.ClusterStateBootstrapping,
	emrtypes.ClusterStateRunning, emrtypes.ClusterStateWaiting,
}

func (p *Provider) listWorkGroups(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listWorkGroupsFrom(ctx, regionalClient(p, region, athena.NewFromConfig), region)
}

func (p *Provider) listWorkGroupsFrom(ctx context.Context, client athenaAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Athena") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := athena.NewListWorkGroupsPaginator(client, &athena.ListWorkGroupsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_work_groups", "", err)
		}
		for _, wg := range output.WorkGroups {
			name := aws.ToString(wg.Name)
			arn := fmt.Sprintf("arn:aws:athena:%s:%s:workgroup/%s", region, p.accountID, name)
			resources = append(resources, p.bulkResource(region, "aws_athena_workgroup", name, name, arn, wg.CreationTime))
		}
	}
	log.Debug("AWS Athena: Found %d workgroups in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listReplicationInstances(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listReplicationInstancesFrom(ctx, regionalClient(p, region, dms.NewFromConfig), region)
}

func (p *Provider) listReplicationInstancesFrom(ctx context.Context, client dmsAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "DMS") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := dms.NewDescribeReplicationInstancesPaginator(client, &dms.DescribeReplicationInstancesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_replication_instances", "", err)
		}
		for _, ri := range output.ReplicationInstances {
			id := aws.ToString(ri.ReplicationInstanceIdentifier)
			resources = append(resources, p.bulkResource(region, "aws_dms_replication_instance", id, id, aws.ToString(ri.ReplicationInstanceArn), ri.InstanceCreateTime))
		}
	}
	log.Debug("AWS DMS: Found %d replication instances in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listDataPipelines(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listDataPipelinesFrom(ctx, regionalClient(p, region, datapipeline.NewFromConfig), region)
}

func (p *Provider) listDataPipelinesFrom(ctx context.Context, client dataPipelineAPI, region string) ([]types.Resource, error) {
	var ids []string
	var marker *string
	for {
		output, err := client.ListPipelines(ctx, &datapipeline.ListPipelinesInput{Marker: marker})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_data_pipelines", "", err)
		}
		for _, pl := range output.PipelineIdList {
			ids = append(ids, aws.ToString(pl.Id))
		}
		if !output.HasMoreResults || output.Marker == nil {
			break
		}
		marker = output.Marker
	}

	resources := forEachConcurrently(chunk(ids, dataPipelineDescribeBatch), func(batch []string) []types.Resource {
		return p.describePipelines(ctx, client, region, batch)
	})
	log.Debug("AWS Data Pipeline: Found %d pipelines in %s", len(resources), region)
	return resources, nil
}

// describePipelines builds the resources of one DescribePipelines batch.
func (p *Provider) describePipelines(ctx context.Context, client dataPipelineAPI, region string, batch []string) []types.Resource {
	output, err := client.DescribePipelines(ctx, &datapipeline.DescribePipelinesInput{PipelineIds: batch})
	if resourceGone(err) && len(batch) > 1 {
		// One deleted pipeline fails its whole batch: describe the batch one ID at a time.
		described := make([]types.Resource, 0, len(batch))
		for _, id := range batch {
			described = append(described, p.describePipelines(ctx, client, region, []string{id})...)
		}
		return described
	}
	if err != nil {
		for _, id := range batch {
			p.skipResource(ctx, "Data Pipeline", region, "pipeline "+id, err)
		}
		return nil
	}
	described := make([]types.Resource, 0, len(output.PipelineDescriptionList))
	for _, d := range output.PipelineDescriptionList {
		id := aws.ToString(d.PipelineId)
		arn := fmt.Sprintf("arn:aws:datapipeline:%s:%s:pipeline/%s", region, p.accountID, id)
		tags := tagsToMap(d.Tags,
			func(t datapipelinetypes.Tag) *string { return t.Key },
			func(t datapipelinetypes.Tag) *string { return t.Value })
		described = append(described, p.resource(region, "aws_datapipeline_pipeline", id, aws.ToString(d.Name), arn, tags, nil))
	}
	return described
}

func (p *Provider) listDataSyncTasks(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listDataSyncTasksFrom(ctx, regionalClient(p, region, datasync.NewFromConfig), region)
}

func (p *Provider) listDataSyncTasksFrom(ctx context.Context, client dataSyncAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "DataSync") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := datasync.NewListTasksPaginator(client, &datasync.ListTasksInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_datasync_tasks", "", err)
		}
		for _, t := range output.Tasks {
			arn := aws.ToString(t.TaskArn)
			resources = append(resources, p.bulkResource(region, "aws_datasync_task", nameFromARN(arn), aws.ToString(t.Name), arn, nil))
		}
	}
	log.Debug("AWS DataSync: Found %d tasks in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listEMRClusters(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listEMRClustersFrom(ctx, regionalClient(p, region, emr.NewFromConfig), region)
}

func (p *Provider) listEMRClustersFrom(ctx context.Context, client emrAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "EMR") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := emr.NewListClustersPaginator(client, &emr.ListClustersInput{ClusterStates: emrActiveStates})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_emr_clusters", "", err)
		}
		for _, c := range output.Clusters {
			resources = append(resources, p.bulkResource(region, "aws_emr_cluster", aws.ToString(c.Id), aws.ToString(c.Name), aws.ToString(c.ClusterArn), nil))
		}
	}
	log.Debug("AWS EMR: Found %d clusters in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listVaults(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listVaultsFrom(ctx, regionalClient(p, region, glacier.NewFromConfig), region)
}

func (p *Provider) listVaultsFrom(ctx context.Context, client glacierAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Glacier") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := glacier.NewListVaultsPaginator(client, &glacier.ListVaultsInput{AccountId: aws.String("-")})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_glacier_vaults", "", err)
		}
		for _, v := range output.VaultList {
			name := aws.ToString(v.VaultName)
			resources = append(resources, p.bulkResource(region, "aws_glacier_vault", name, name, aws.ToString(v.VaultARN), parseRFC3339(v.CreationDate)))
		}
	}
	log.Debug("AWS Glacier: Found %d vaults in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listMemoryDBClusters(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listMemoryDBClustersFrom(ctx, regionalClient(p, region, memorydb.NewFromConfig), region)
}

func (p *Provider) listMemoryDBClustersFrom(ctx context.Context, client memoryDBAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "MemoryDB") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := memorydb.NewDescribeClustersPaginator(client, &memorydb.DescribeClustersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_memorydb_clusters", "", err)
		}
		for _, c := range output.Clusters {
			name := aws.ToString(c.Name)
			resources = append(resources, p.bulkResource(region, "aws_memorydb_cluster", name, name, aws.ToString(c.ARN), nil))
		}
	}
	log.Debug("AWS MemoryDB: Found %d clusters in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listBrokers(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listBrokersFrom(ctx, regionalClient(p, region, mq.NewFromConfig), region)
}

func (p *Provider) listBrokersFrom(ctx context.Context, client mqAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "MQ") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := mq.NewListBrokersPaginator(client, &mq.ListBrokersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_brokers", "", err)
		}
		for _, b := range output.BrokerSummaries {
			resources = append(resources, p.bulkResource(region, "aws_mq_broker", aws.ToString(b.BrokerId), aws.ToString(b.BrokerName), aws.ToString(b.BrokerArn), b.Created))
		}
	}
	log.Debug("AWS MQ: Found %d brokers in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listSESResources(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSESResourcesFrom(ctx, regionalClient(p, region, sesv2.NewFromConfig), region)
}

func (p *Provider) listSESResourcesFrom(ctx context.Context, client sesAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "SES") {
		return nil, nil
	}
	var resources []types.Resource
	identities := sesv2.NewListEmailIdentitiesPaginator(client, &sesv2.ListEmailIdentitiesInput{})
	for identities.HasMorePages() {
		output, err := identities.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ses_identities", "", err)
		}
		for _, id := range output.EmailIdentities {
			name := aws.ToString(id.IdentityName)
			arn := fmt.Sprintf("arn:aws:ses:%s:%s:identity/%s", region, p.accountID, name)
			resources = append(resources, p.bulkResource(region, "aws_ses_email_identity", name, name, arn, nil))
		}
	}
	sets := sesv2.NewListConfigurationSetsPaginator(client, &sesv2.ListConfigurationSetsInput{})
	for sets.HasMorePages() {
		output, err := sets.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ses_configuration_sets", "", err)
		}
		for _, name := range output.ConfigurationSets {
			arn := fmt.Sprintf("arn:aws:ses:%s:%s:configuration-set/%s", region, p.accountID, name)
			resources = append(resources, p.bulkResource(region, "aws_ses_configuration_set", name, name, arn, nil))
		}
	}
	log.Debug("AWS SES: Found %d identities and configuration sets in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listGateways(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listGatewaysFrom(ctx, regionalClient(p, region, storagegateway.NewFromConfig), region)
}

func (p *Provider) listGatewaysFrom(ctx context.Context, client storageGatewayAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Storage Gateway") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := storagegateway.NewListGatewaysPaginator(client, &storagegateway.ListGatewaysInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_storage_gateways", "", err)
		}
		for _, g := range output.Gateways {
			resources = append(resources, p.bulkResource(region, "aws_storagegateway_gateway", aws.ToString(g.GatewayId), aws.ToString(g.GatewayName), aws.ToString(g.GatewayARN), nil))
		}
	}
	log.Debug("AWS Storage Gateway: Found %d gateways in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listTransferServers(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listTransferServersFrom(ctx, regionalClient(p, region, transfer.NewFromConfig), region)
}

func (p *Provider) listTransferServersFrom(ctx context.Context, client transferAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Transfer Family") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := transfer.NewListServersPaginator(client, &transfer.ListServersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_transfer_servers", "", err)
		}
		for _, s := range output.Servers {
			id := aws.ToString(s.ServerId)
			resources = append(resources, p.bulkResource(region, "aws_transfer_server", id, id, aws.ToString(s.Arn), nil))
		}
	}
	log.Debug("AWS Transfer Family: Found %d servers in %s", len(resources), region)
	return resources, nil
}
