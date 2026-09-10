package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/appstream"
	appstreamtypes "github.com/aws/aws-sdk-go-v2/service/appstream/types"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	batchtypes "github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/aws/aws-sdk-go-v2/service/directconnect"
	directconnecttypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
	"github.com/aws/aws-sdk-go-v2/service/dlm"
	dlmtypes "github.com/aws/aws-sdk-go-v2/service/dlm/types"
	"github.com/aws/aws-sdk-go-v2/service/drs"
	drstypes "github.com/aws/aws-sdk-go-v2/service/drs/types"
	"github.com/aws/aws-sdk-go-v2/service/lightsail"
	lightsailtypes "github.com/aws/aws-sdk-go-v2/service/lightsail/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/ssmincidents"
	ssmincidentstypes "github.com/aws/aws-sdk-go-v2/service/ssmincidents/types"
	"github.com/aws/aws-sdk-go-v2/service/workspaces"
	workspacestypes "github.com/aws/aws-sdk-go-v2/service/workspaces/types"
)

type mockAppStreamClient struct {
	fleets []appstreamtypes.Fleet
	stacks []appstreamtypes.Stack
	err    error
}

func (m *mockAppStreamClient) DescribeFleets(ctx context.Context, params *appstream.DescribeFleetsInput, optFns ...func(*appstream.Options)) (*appstream.DescribeFleetsOutput, error) {
	return &appstream.DescribeFleetsOutput{Fleets: m.fleets}, m.err
}

func (m *mockAppStreamClient) DescribeStacks(ctx context.Context, params *appstream.DescribeStacksInput, optFns ...func(*appstream.Options)) (*appstream.DescribeStacksOutput, error) {
	return &appstream.DescribeStacksOutput{Stacks: m.stacks}, m.err
}

type mockBatchClient struct {
	envs   []batchtypes.ComputeEnvironmentDetail
	queues []batchtypes.JobQueueDetail
	err    error
}

func (m *mockBatchClient) DescribeComputeEnvironments(ctx context.Context, params *batch.DescribeComputeEnvironmentsInput, optFns ...func(*batch.Options)) (*batch.DescribeComputeEnvironmentsOutput, error) {
	return &batch.DescribeComputeEnvironmentsOutput{ComputeEnvironments: m.envs}, m.err
}

func (m *mockBatchClient) DescribeJobQueues(ctx context.Context, params *batch.DescribeJobQueuesInput, optFns ...func(*batch.Options)) (*batch.DescribeJobQueuesOutput, error) {
	return &batch.DescribeJobQueuesOutput{JobQueues: m.queues}, m.err
}

type mockDirectConnectClient struct {
	connections []directconnecttypes.Connection
	err         error
}

func (m *mockDirectConnectClient) DescribeConnections(ctx context.Context, params *directconnect.DescribeConnectionsInput, optFns ...func(*directconnect.Options)) (*directconnect.DescribeConnectionsOutput, error) {
	return &directconnect.DescribeConnectionsOutput{Connections: m.connections}, m.err
}

type mockDLMClient struct {
	policies []dlmtypes.LifecyclePolicySummary
	err      error
}

func (m *mockDLMClient) GetLifecyclePolicies(ctx context.Context, params *dlm.GetLifecyclePoliciesInput, optFns ...func(*dlm.Options)) (*dlm.GetLifecyclePoliciesOutput, error) {
	return &dlm.GetLifecyclePoliciesOutput{Policies: m.policies}, m.err
}

type mockDRSClient struct {
	servers []drstypes.SourceServer
	err     error
}

func (m *mockDRSClient) DescribeSourceServers(ctx context.Context, params *drs.DescribeSourceServersInput, optFns ...func(*drs.Options)) (*drs.DescribeSourceServersOutput, error) {
	return &drs.DescribeSourceServersOutput{Items: m.servers}, m.err
}

type mockLightsailClient struct {
	instances []lightsailtypes.Instance
	err       error
	tagged    *lightsail.TagResourceInput
}

func (m *mockLightsailClient) GetInstances(ctx context.Context, params *lightsail.GetInstancesInput, optFns ...func(*lightsail.Options)) (*lightsail.GetInstancesOutput, error) {
	return &lightsail.GetInstancesOutput{Instances: m.instances}, m.err
}

func (m *mockLightsailClient) TagResource(ctx context.Context, params *lightsail.TagResourceInput, optFns ...func(*lightsail.Options)) (*lightsail.TagResourceOutput, error) {
	m.tagged = params
	return &lightsail.TagResourceOutput{}, m.err
}

type mockSSMClient struct {
	parameters []ssmtypes.ParameterMetadata
	documents  []ssmtypes.DocumentIdentifier
	err        error
	docFilters []ssmtypes.DocumentKeyValuesFilter
}

func (m *mockSSMClient) DescribeParameters(ctx context.Context, params *ssm.DescribeParametersInput, optFns ...func(*ssm.Options)) (*ssm.DescribeParametersOutput, error) {
	return &ssm.DescribeParametersOutput{Parameters: m.parameters}, m.err
}

func (m *mockSSMClient) ListDocuments(ctx context.Context, params *ssm.ListDocumentsInput, optFns ...func(*ssm.Options)) (*ssm.ListDocumentsOutput, error) {
	m.docFilters = params.Filters
	return &ssm.ListDocumentsOutput{DocumentIdentifiers: m.documents}, m.err
}

type mockSSMIncidentsClient struct {
	plans []ssmincidentstypes.ResponsePlanSummary
	err   error
}

func (m *mockSSMIncidentsClient) ListResponsePlans(ctx context.Context, params *ssmincidents.ListResponsePlansInput, optFns ...func(*ssmincidents.Options)) (*ssmincidents.ListResponsePlansOutput, error) {
	return &ssmincidents.ListResponsePlansOutput{ResponsePlanSummaries: m.plans}, m.err
}

type mockWorkSpacesClient struct {
	workspaces []workspacestypes.Workspace
	err        error
}

func (m *mockWorkSpacesClient) DescribeWorkspaces(ctx context.Context, params *workspaces.DescribeWorkspacesInput, optFns ...func(*workspaces.Options)) (*workspaces.DescribeWorkspacesOutput, error) {
	return &workspaces.DescribeWorkspacesOutput{Workspaces: m.workspaces}, m.err
}

func TestListAppStreamResources(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:as:fleet": {"owner": "x"}})
	mock := &mockAppStreamClient{
		fleets: []appstreamtypes.Fleet{{Name: aws.String("fleet"), Arn: aws.String("arn:as:fleet")}},
		stacks: []appstreamtypes.Stack{{Name: aws.String("stack"), Arn: aws.String("arn:as:stack")}},
	}
	resources, err := p.listAppStreamResourcesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || resources[0].Type != "aws_appstream_fleet" || resources[0].Tags["owner"] != "x" || resources[1].Type != "aws_appstream_stack" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListBatchResources(t *testing.T) {
	mock := &mockBatchClient{
		envs:   []batchtypes.ComputeEnvironmentDetail{{ComputeEnvironmentName: aws.String("spot"), ComputeEnvironmentArn: aws.String("arn:batch:ce"), Tags: map[string]string{"owner": "x"}}},
		queues: []batchtypes.JobQueueDetail{{JobQueueName: aws.String("default"), JobQueueArn: aws.String("arn:batch:jq")}},
	}
	resources, err := testProvider().listBatchResourcesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || resources[0].Tags["owner"] != "x" || resources[1].Type != "aws_batch_job_queue" || resources[1].Tags == nil {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListDirectConnectConnections(t *testing.T) {
	mock := &mockDirectConnectClient{connections: []directconnecttypes.Connection{{
		ConnectionId: aws.String("dxcon-1"), ConnectionName: aws.String("dc-link"),
		Tags: []directconnecttypes.Tag{{Key: aws.String("owner"), Value: aws.String("x")}},
	}}}
	resources, err := testProvider().listDirectConnectConnectionsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != "arn:aws:directconnect:us-east-1:123456789012:dxcon/dxcon-1" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListLifecyclePolicies(t *testing.T) {
	mock := &mockDLMClient{policies: []dlmtypes.LifecyclePolicySummary{
		{PolicyId: aws.String("policy-1"), Description: aws.String("daily snapshots"), Tags: map[string]string{"owner": "x"}},
		{PolicyId: aws.String("policy-2")},
	}}
	resources, err := testProvider().listLifecyclePoliciesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || resources[0].Name != "daily snapshots" || resources[0].Tags["owner"] != "x" || resources[1].Name != "policy-2" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListSourceServers(t *testing.T) {
	mock := &mockDRSClient{servers: []drstypes.SourceServer{{
		SourceServerID: aws.String("s-1"), Arn: aws.String("arn:drs:s-1"), Tags: map[string]string{"owner": "x"},
		SourceProperties: &drstypes.SourceProperties{IdentificationHints: &drstypes.IdentificationHints{Hostname: aws.String("db01")}},
	}}}
	resources, err := testProvider().listSourceServersFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Name != "db01" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}

	uninitialised := &mockDRSClient{err: apiError{"UninitializedAccountException"}}
	if resources, err := testProvider().listSourceServersFrom(context.Background(), uninitialised, defaultRegion); err != nil || len(resources) != 0 {
		t.Errorf("uninitialised region must be skipped silently, got %+v, %v", resources, err)
	}
}

func TestListLightsailInstances(t *testing.T) {
	mock := &mockLightsailClient{instances: []lightsailtypes.Instance{{
		Name: aws.String("wp"), Arn: aws.String("arn:aws:lightsail:us-east-1:123456789012:Instance/abc"),
		Tags: []lightsailtypes.Tag{{Key: aws.String("owner"), Value: aws.String("x")}},
	}}}
	resources, err := testProvider().listLightsailInstancesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ID != "wp" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}

	if resources, err := testProvider().listLightsailInstances(context.Background(), "me-south-1"); err != nil || resources != nil {
		t.Errorf("regions without a Lightsail endpoint must be skipped, got %+v, %v", resources, err)
	}
}

func TestApplyLightsailTags(t *testing.T) {
	mock := &mockLightsailClient{}
	arn := "arn:aws:lightsail:us-east-1:123456789012:Instance/wp"
	if err := applyLightsailTagsWith(context.Background(), mock, arn, map[string]string{"owner": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.tagged == nil || aws.ToString(mock.tagged.ResourceName) != "wp" || len(mock.tagged.Tags) != 1 {
		t.Errorf("TagResource input = %+v", mock.tagged)
	}
	if err := applyLightsailTagsWith(context.Background(), &mockLightsailClient{err: errors.New("boom")}, arn, nil); err == nil {
		t.Error("expected error")
	}
}

func TestListSSMResources(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:ssm:param": {"owner": "x"}})
	mock := &mockSSMClient{
		parameters: []ssmtypes.ParameterMetadata{{Name: aws.String("/app/db"), ARN: aws.String("arn:ssm:param")}},
		documents:  []ssmtypes.DocumentIdentifier{{Name: aws.String("Deploy"), Tags: []ssmtypes.Tag{{Key: aws.String("owner"), Value: aws.String("y")}}}},
	}
	resources, err := p.listSSMResourcesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	if resources[0].Type != "aws_ssm_parameter" || resources[0].Tags["owner"] != "x" || resources[1].Type != "aws_ssm_document" || resources[1].Tags["owner"] != "y" {
		t.Errorf("got %+v", resources)
	}
	if len(mock.docFilters) != 1 || mock.docFilters[0].Values[0] != "Self" {
		t.Errorf("ListDocuments must filter Owner=Self, got %+v", mock.docFilters)
	}

	// Without bulk tags the parameters are skipped but the documents still list.
	resources, err = testProvider().listSSMResourcesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_ssm_document" {
		t.Errorf("without bulk tags got %+v, %v", resources, err)
	}
}

func TestListResponsePlans(t *testing.T) {
	p := bulkProvider(nil)
	resources, err := p.listResponsePlansFrom(context.Background(), &mockSSMIncidentsClient{plans: []ssmincidentstypes.ResponsePlanSummary{{Name: aws.String("sev1"), Arn: aws.String("arn:ssmi:sev1")}}}, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_ssmincidents_response_plan" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListWorkSpaces_SkipsTerminated(t *testing.T) {
	arn := "arn:aws:workspaces:us-east-1:123456789012:workspace/ws-1"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	mock := &mockWorkSpacesClient{workspaces: []workspacestypes.Workspace{
		{WorkspaceId: aws.String("ws-1"), UserName: aws.String("pedro"), State: workspacestypes.WorkspaceStateAvailable},
		{WorkspaceId: aws.String("ws-2"), State: workspacestypes.WorkspaceStateTerminated},
	}}
	resources, err := p.listWorkSpacesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Name != "pedro" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestComputeExtraListers_SkipWithoutBulkTags(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	for name, list := range map[string]func() (int, error){
		"appstream": func() (int, error) {
			r, err := p.listAppStreamResourcesFrom(ctx, &mockAppStreamClient{fleets: []appstreamtypes.Fleet{{Name: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"ssmincidents": func() (int, error) {
			r, err := p.listResponsePlansFrom(ctx, &mockSSMIncidentsClient{plans: []ssmincidentstypes.ResponsePlanSummary{{Name: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"workspaces": func() (int, error) {
			r, err := p.listWorkSpacesFrom(ctx, &mockWorkSpacesClient{workspaces: []workspacestypes.Workspace{{WorkspaceId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
	} {
		if n, err := list(); err != nil || n != 0 {
			t.Errorf("%s: want no resources and no error without bulk tags, got %d, %v", name, n, err)
		}
	}
}

func TestComputeExtraListers_Error(t *testing.T) {
	boom := errors.New("boom")
	p := bulkProvider(nil)
	ctx := context.Background()
	for name, list := range map[string]func() error{
		"appstream": func() error {
			_, err := p.listAppStreamResourcesFrom(ctx, &mockAppStreamClient{err: boom}, defaultRegion)
			return err
		},
		"batch": func() error {
			_, err := p.listBatchResourcesFrom(ctx, &mockBatchClient{err: boom}, defaultRegion)
			return err
		},
		"directconnect": func() error {
			_, err := p.listDirectConnectConnectionsFrom(ctx, &mockDirectConnectClient{err: boom}, defaultRegion)
			return err
		},
		"dlm": func() error {
			_, err := p.listLifecyclePoliciesFrom(ctx, &mockDLMClient{err: boom}, defaultRegion)
			return err
		},
		"drs": func() error {
			_, err := p.listSourceServersFrom(ctx, &mockDRSClient{err: boom}, defaultRegion)
			return err
		},
		"lightsail": func() error {
			_, err := p.listLightsailInstancesFrom(ctx, &mockLightsailClient{err: boom}, defaultRegion)
			return err
		},
		"ssm": func() error {
			_, err := p.listSSMResourcesFrom(ctx, &mockSSMClient{err: boom}, defaultRegion)
			return err
		},
		"ssmincidents": func() error {
			_, err := p.listResponsePlansFrom(ctx, &mockSSMIncidentsClient{err: boom}, defaultRegion)
			return err
		},
		"workspaces": func() error {
			_, err := p.listWorkSpacesFrom(ctx, &mockWorkSpacesClient{err: boom}, defaultRegion)
			return err
		},
	} {
		if list() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
