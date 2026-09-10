package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/appstream"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/directconnect"
	directconnecttypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
	"github.com/aws/aws-sdk-go-v2/service/dlm"
	"github.com/aws/aws-sdk-go-v2/service/drs"
	"github.com/aws/aws-sdk-go-v2/service/lightsail"
	lightsailtypes "github.com/aws/aws-sdk-go-v2/service/lightsail/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/ssmincidents"
	"github.com/aws/aws-sdk-go-v2/service/workspaces"
	workspacestypes "github.com/aws/aws-sdk-go-v2/service/workspaces/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Compute platforms, end-user computing, connectivity and operations tooling.

type appStreamAPI interface {
	DescribeFleets(ctx context.Context, params *appstream.DescribeFleetsInput, optFns ...func(*appstream.Options)) (*appstream.DescribeFleetsOutput, error)
	DescribeStacks(ctx context.Context, params *appstream.DescribeStacksInput, optFns ...func(*appstream.Options)) (*appstream.DescribeStacksOutput, error)
}

type batchAPI interface {
	DescribeComputeEnvironments(ctx context.Context, params *batch.DescribeComputeEnvironmentsInput, optFns ...func(*batch.Options)) (*batch.DescribeComputeEnvironmentsOutput, error)
	DescribeJobQueues(ctx context.Context, params *batch.DescribeJobQueuesInput, optFns ...func(*batch.Options)) (*batch.DescribeJobQueuesOutput, error)
}

type directConnectAPI interface {
	DescribeConnections(ctx context.Context, params *directconnect.DescribeConnectionsInput, optFns ...func(*directconnect.Options)) (*directconnect.DescribeConnectionsOutput, error)
}

type dlmAPI interface {
	GetLifecyclePolicies(ctx context.Context, params *dlm.GetLifecyclePoliciesInput, optFns ...func(*dlm.Options)) (*dlm.GetLifecyclePoliciesOutput, error)
}

type drsAPI interface {
	DescribeSourceServers(ctx context.Context, params *drs.DescribeSourceServersInput, optFns ...func(*drs.Options)) (*drs.DescribeSourceServersOutput, error)
}

type lightsailAPI interface {
	GetInstances(ctx context.Context, params *lightsail.GetInstancesInput, optFns ...func(*lightsail.Options)) (*lightsail.GetInstancesOutput, error)
	TagResource(ctx context.Context, params *lightsail.TagResourceInput, optFns ...func(*lightsail.Options)) (*lightsail.TagResourceOutput, error)
}

type ssmAPI interface {
	DescribeParameters(ctx context.Context, params *ssm.DescribeParametersInput, optFns ...func(*ssm.Options)) (*ssm.DescribeParametersOutput, error)
	ListDocuments(ctx context.Context, params *ssm.ListDocumentsInput, optFns ...func(*ssm.Options)) (*ssm.ListDocumentsOutput, error)
}

type ssmIncidentsAPI interface {
	ListResponsePlans(ctx context.Context, params *ssmincidents.ListResponsePlansInput, optFns ...func(*ssmincidents.Options)) (*ssmincidents.ListResponsePlansOutput, error)
}

type workSpacesAPI interface {
	DescribeWorkspaces(ctx context.Context, params *workspaces.DescribeWorkspacesInput, optFns ...func(*workspaces.Options)) (*workspaces.DescribeWorkspacesOutput, error)
}

// lightsailRegions are the regions where Lightsail has an endpoint; calling
// it anywhere else fails at DNS.
var lightsailRegions = map[string]bool{
	"us-east-1": true, "us-east-2": true, "us-west-2": true,
	"ca-central-1": true,
	"eu-west-1":    true, "eu-west-2": true, "eu-west-3": true, "eu-central-1": true, "eu-north-1": true,
	"ap-south-1": true, "ap-northeast-1": true, "ap-northeast-2": true,
	"ap-southeast-1": true, "ap-southeast-2": true,
}

func (p *Provider) listAppStreamResources(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listAppStreamResourcesFrom(ctx, regionalClient(p, region, appstream.NewFromConfig), region)
}

func (p *Provider) listAppStreamResourcesFrom(ctx context.Context, client appStreamAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "AppStream") {
		return nil, nil
	}
	var resources []types.Resource
	var next *string
	for {
		output, err := client.DescribeFleets(ctx, &appstream.DescribeFleetsInput{NextToken: next})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_appstream_fleets", "", err)
		}
		for _, f := range output.Fleets {
			name := aws.ToString(f.Name)
			resources = append(resources, p.bulkResource(region, "aws_appstream_fleet", name, name, aws.ToString(f.Arn), f.CreatedTime))
		}
		if output.NextToken == nil || len(output.Fleets) == 0 {
			break
		}
		next = output.NextToken
	}
	next = nil
	for {
		output, err := client.DescribeStacks(ctx, &appstream.DescribeStacksInput{NextToken: next})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_appstream_stacks", "", err)
		}
		for _, s := range output.Stacks {
			name := aws.ToString(s.Name)
			resources = append(resources, p.bulkResource(region, "aws_appstream_stack", name, name, aws.ToString(s.Arn), s.CreatedTime))
		}
		if output.NextToken == nil || len(output.Stacks) == 0 {
			break
		}
		next = output.NextToken
	}
	log.Debug("AWS AppStream: Found %d fleets and stacks in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listBatchResources(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listBatchResourcesFrom(ctx, regionalClient(p, region, batch.NewFromConfig), region)
}

func (p *Provider) listBatchResourcesFrom(ctx context.Context, client batchAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	envs := batch.NewDescribeComputeEnvironmentsPaginator(client, &batch.DescribeComputeEnvironmentsInput{})
	for envs.HasMorePages() {
		output, err := envs.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_batch_compute_environments", "", err)
		}
		for _, ce := range output.ComputeEnvironments {
			name := aws.ToString(ce.ComputeEnvironmentName)
			resources = append(resources, p.resource(region, "aws_batch_compute_environment", name, name, aws.ToString(ce.ComputeEnvironmentArn), ce.Tags, nil))
		}
	}
	queues := batch.NewDescribeJobQueuesPaginator(client, &batch.DescribeJobQueuesInput{})
	for queues.HasMorePages() {
		output, err := queues.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_batch_job_queues", "", err)
		}
		for _, q := range output.JobQueues {
			name := aws.ToString(q.JobQueueName)
			resources = append(resources, p.resource(region, "aws_batch_job_queue", name, name, aws.ToString(q.JobQueueArn), q.Tags, nil))
		}
	}
	log.Debug("AWS Batch: Found %d compute environments and job queues in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listDirectConnectConnections(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listDirectConnectConnectionsFrom(ctx, regionalClient(p, region, directconnect.NewFromConfig), region)
}

func (p *Provider) listDirectConnectConnectionsFrom(ctx context.Context, client directConnectAPI, region string) ([]types.Resource, error) {
	output, err := client.DescribeConnections(ctx, &directconnect.DescribeConnectionsInput{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_direct_connect_connections", "", err)
	}
	resources := make([]types.Resource, 0, len(output.Connections))
	for _, c := range output.Connections {
		id := aws.ToString(c.ConnectionId)
		arn := fmt.Sprintf("arn:aws:directconnect:%s:%s:dxcon/%s", region, p.accountID, id)
		tags := tagsToMap(c.Tags,
			func(t directconnecttypes.Tag) *string { return t.Key },
			func(t directconnecttypes.Tag) *string { return t.Value })
		resources = append(resources, p.resource(region, "aws_dx_connection", id, aws.ToString(c.ConnectionName), arn, tags, nil))
	}
	log.Debug("AWS Direct Connect: Found %d connections in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listLifecyclePolicies(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listLifecyclePoliciesFrom(ctx, regionalClient(p, region, dlm.NewFromConfig), region)
}

func (p *Provider) listLifecyclePoliciesFrom(ctx context.Context, client dlmAPI, region string) ([]types.Resource, error) {
	output, err := client.GetLifecyclePolicies(ctx, &dlm.GetLifecyclePoliciesInput{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_lifecycle_policies", "", err)
	}
	resources := make([]types.Resource, 0, len(output.Policies))
	for _, pol := range output.Policies {
		id := aws.ToString(pol.PolicyId)
		arn := fmt.Sprintf("arn:aws:dlm:%s:%s:policy/%s", region, p.accountID, id)
		name := aws.ToString(pol.Description)
		if name == "" {
			name = id
		}
		resources = append(resources, p.resource(region, "aws_dlm_lifecycle_policy", id, name, arn, pol.Tags, nil))
	}
	log.Debug("AWS DLM: Found %d lifecycle policies in %s", len(resources), region)
	return resources, nil
}

// listSourceServers lists Elastic Disaster Recovery source servers. Regions
// where DRS was never initialised reject the call, which is not an error.
func (p *Provider) listSourceServers(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSourceServersFrom(ctx, regionalClient(p, region, drs.NewFromConfig), region)
}

func (p *Provider) listSourceServersFrom(ctx context.Context, client drsAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := drs.NewDescribeSourceServersPaginator(client, &drs.DescribeSourceServersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			if notSubscribed(err) {
				log.Debug("AWS DRS: not initialised in %s, skipping", region)
				return nil, nil
			}
			return nil, provider.NewProviderError(providerName, "list_drs_source_servers", "", err)
		}
		for _, s := range output.Items {
			id := aws.ToString(s.SourceServerID)
			name := id
			if s.SourceProperties != nil && s.SourceProperties.IdentificationHints != nil {
				if hostname := aws.ToString(s.SourceProperties.IdentificationHints.Hostname); hostname != "" {
					name = hostname
				}
			}
			resources = append(resources, p.resource(region, "aws_drs_source_server", id, name, aws.ToString(s.Arn), s.Tags, nil))
		}
	}
	log.Debug("AWS DRS: Found %d source servers in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listLightsailInstances(ctx context.Context, region string) ([]types.Resource, error) {
	if !lightsailRegions[region] {
		return nil, nil
	}
	return p.listLightsailInstancesFrom(ctx, regionalClient(p, region, lightsail.NewFromConfig), region)
}

func (p *Provider) listLightsailInstancesFrom(ctx context.Context, client lightsailAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	var next *string
	for {
		output, err := client.GetInstances(ctx, &lightsail.GetInstancesInput{PageToken: next})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_lightsail_instances", "", err)
		}
		for _, inst := range output.Instances {
			name := aws.ToString(inst.Name)
			tags := tagsToMap(inst.Tags,
				func(t lightsailtypes.Tag) *string { return t.Key },
				func(t lightsailtypes.Tag) *string { return t.Value })
			resources = append(resources, p.resource(region, "aws_lightsail_instance", name, name, aws.ToString(inst.Arn), tags, inst.CreatedAt))
		}
		if output.NextPageToken == nil || len(output.Instances) == 0 {
			break
		}
		next = output.NextPageToken
	}
	log.Debug("AWS Lightsail: Found %d instances in %s", len(resources), region)
	return resources, nil
}

// applyLightsailTags tags a Lightsail resource, which the Resource Groups
// Tagging API does not cover. Lightsail addresses resources by name.
func (p *Provider) applyLightsailTags(ctx context.Context, arn string, tags map[string]string) error {
	region := extractRegionFromARN(arn)
	if region == "" {
		region = defaultRegion
	}
	return applyLightsailTagsWith(ctx, regionalClient(p, region, lightsail.NewFromConfig), arn, tags)
}

func applyLightsailTagsWith(ctx context.Context, client lightsailAPI, arn string, tags map[string]string) error {
	lsTags := make([]lightsailtypes.Tag, 0, len(tags))
	for k, v := range tags {
		lsTags = append(lsTags, lightsailtypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	_, err := client.TagResource(ctx, &lightsail.TagResourceInput{
		ResourceName: aws.String(nameFromARN(arn)),
		ResourceArn:  aws.String(arn),
		Tags:         lsTags,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_lightsail_tags", arn, err)
	}
	log.Debug("AWS Lightsail: Applied %d tags to %s", len(tags), arn)
	return nil
}

func (p *Provider) listSSMResources(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSSMResourcesFrom(ctx, regionalClient(p, region, ssm.NewFromConfig), region)
}

// listSSMResourcesFrom lists SSM parameters (tags from the bulk source) and
// the account's own documents (tags inline).
func (p *Provider) listSSMResourcesFrom(ctx context.Context, client ssmAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	if p.requireBulkTags(region, "SSM parameters") {
		params := ssm.NewDescribeParametersPaginator(client, &ssm.DescribeParametersInput{})
		for params.HasMorePages() {
			output, err := params.NextPage(ctx)
			if err != nil {
				return nil, provider.NewProviderError(providerName, "list_ssm_parameters", "", err)
			}
			for _, prm := range output.Parameters {
				name := aws.ToString(prm.Name)
				resources = append(resources, p.bulkResource(region, "aws_ssm_parameter", name, name, aws.ToString(prm.ARN), prm.LastModifiedDate))
			}
		}
	}

	docs := ssm.NewListDocumentsPaginator(client, &ssm.ListDocumentsInput{
		Filters: []ssmtypes.DocumentKeyValuesFilter{{Key: aws.String("Owner"), Values: []string{"Self"}}},
	})
	for docs.HasMorePages() {
		output, err := docs.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_ssm_documents", "", err)
		}
		for _, d := range output.DocumentIdentifiers {
			name := aws.ToString(d.Name)
			arn := fmt.Sprintf("arn:aws:ssm:%s:%s:document/%s", region, p.accountID, name)
			tags := tagsToMap(d.Tags,
				func(t ssmtypes.Tag) *string { return t.Key },
				func(t ssmtypes.Tag) *string { return t.Value })
			resources = append(resources, p.resource(region, "aws_ssm_document", name, name, arn, tags, d.CreatedDate))
		}
	}
	log.Debug("AWS SSM: Found %d parameters and documents in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listResponsePlans(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listResponsePlansFrom(ctx, regionalClient(p, region, ssmincidents.NewFromConfig), region)
}

func (p *Provider) listResponsePlansFrom(ctx context.Context, client ssmIncidentsAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Incident Manager") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := ssmincidents.NewListResponsePlansPaginator(client, &ssmincidents.ListResponsePlansInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_response_plans", "", err)
		}
		for _, rp := range output.ResponsePlanSummaries {
			name := aws.ToString(rp.Name)
			resources = append(resources, p.bulkResource(region, "aws_ssmincidents_response_plan", name, name, aws.ToString(rp.Arn), nil))
		}
	}
	log.Debug("AWS Incident Manager: Found %d response plans in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listWorkSpaces(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listWorkSpacesFrom(ctx, regionalClient(p, region, workspaces.NewFromConfig), region)
}

func (p *Provider) listWorkSpacesFrom(ctx context.Context, client workSpacesAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "WorkSpaces") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := workspaces.NewDescribeWorkspacesPaginator(client, &workspaces.DescribeWorkspacesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_workspaces", "", err)
		}
		for _, ws := range output.Workspaces {
			if ws.State == workspacestypes.WorkspaceStateTerminated {
				continue
			}
			id := aws.ToString(ws.WorkspaceId)
			arn := fmt.Sprintf("arn:aws:workspaces:%s:%s:workspace/%s", region, p.accountID, id)
			name := aws.ToString(ws.UserName)
			if name == "" {
				name = id
			}
			resources = append(resources, p.bulkResource(region, "aws_workspaces_workspace", id, name, arn, nil))
		}
	}
	log.Debug("AWS WorkSpaces: Found %d workspaces in %s", len(resources), region)
	return resources, nil
}
