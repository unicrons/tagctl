package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/aws-sdk-go-v2/service/backup"
	backuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
)

type mockStepFunctionsClient struct {
	machines []sfntypes.StateMachineListItem
	err      error
}

func (m *mockStepFunctionsClient) ListStateMachines(ctx context.Context, params *sfn.ListStateMachinesInput, optFns ...func(*sfn.Options)) (*sfn.ListStateMachinesOutput, error) {
	return &sfn.ListStateMachinesOutput{StateMachines: m.machines}, m.err
}

type mockSecretsClient struct {
	secrets []smtypes.SecretListEntry
	err     error
}

func (m *mockSecretsClient) ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	return &secretsmanager.ListSecretsOutput{SecretList: m.secrets}, m.err
}

type mockCloudFormationClient struct {
	stacks []cfntypes.Stack
	err    error
}

func (m *mockCloudFormationClient) DescribeStacks(ctx context.Context, params *cloudformation.DescribeStacksInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DescribeStacksOutput, error) {
	return &cloudformation.DescribeStacksOutput{Stacks: m.stacks}, m.err
}

type mockCloudWatchClient struct {
	metric    []cwtypes.MetricAlarm
	composite []cwtypes.CompositeAlarm
	err       error
}

func (m *mockCloudWatchClient) DescribeAlarms(ctx context.Context, params *cloudwatch.DescribeAlarmsInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error) {
	return &cloudwatch.DescribeAlarmsOutput{MetricAlarms: m.metric, CompositeAlarms: m.composite}, m.err
}

type mockEventBridgeClient struct {
	pages [][]ebridgetypes.Rule
	err   error
	calls int
}

func (m *mockEventBridgeClient) ListRules(ctx context.Context, params *eventbridge.ListRulesInput, optFns ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	page := m.pages[m.calls]
	m.calls++
	out := &eventbridge.ListRulesOutput{Rules: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

type mockACMClient struct {
	certs []acmtypes.CertificateSummary
	err   error
}

func (m *mockACMClient) ListCertificates(ctx context.Context, params *acm.ListCertificatesInput, optFns ...func(*acm.Options)) (*acm.ListCertificatesOutput, error) {
	return &acm.ListCertificatesOutput{CertificateSummaryList: m.certs}, m.err
}

type mockCognitoClient struct {
	pools []cognitotypes.UserPoolDescriptionType
	err   error
}

func (m *mockCognitoClient) ListUserPools(ctx context.Context, params *cognitoidentityprovider.ListUserPoolsInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListUserPoolsOutput, error) {
	return &cognitoidentityprovider.ListUserPoolsOutput{UserPools: m.pools}, m.err
}

type mockCodeBuildClient struct {
	names []string
	err   error
}

func (m *mockCodeBuildClient) ListProjects(ctx context.Context, params *codebuild.ListProjectsInput, optFns ...func(*codebuild.Options)) (*codebuild.ListProjectsOutput, error) {
	return &codebuild.ListProjectsOutput{Projects: m.names}, m.err
}

type mockBackupClient struct {
	vaults []backuptypes.BackupVaultListMember
	err    error
}

func (m *mockBackupClient) ListBackupVaults(ctx context.Context, params *backup.ListBackupVaultsInput, optFns ...func(*backup.Options)) (*backup.ListBackupVaultsOutput, error) {
	return &backup.ListBackupVaultsOutput{BackupVaultList: m.vaults}, m.err
}

type mockFSxClient struct {
	systems []fsxtypes.FileSystem
	err     error
}

func (m *mockFSxClient) DescribeFileSystems(ctx context.Context, params *fsx.DescribeFileSystemsInput, optFns ...func(*fsx.Options)) (*fsx.DescribeFileSystemsOutput, error) {
	return &fsx.DescribeFileSystemsOutput{FileSystems: m.systems}, m.err
}

type mockBeanstalkClient struct {
	envs []ebtypes.EnvironmentDescription
	err  error
}

func (m *mockBeanstalkClient) DescribeEnvironments(ctx context.Context, params *elasticbeanstalk.DescribeEnvironmentsInput, optFns ...func(*elasticbeanstalk.Options)) (*elasticbeanstalk.DescribeEnvironmentsOutput, error) {
	return &elasticbeanstalk.DescribeEnvironmentsOutput{Environments: m.envs}, m.err
}

func TestListStateMachines(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:sfn:order": {"environment": envProd}})
	mock := &mockStepFunctionsClient{machines: []sfntypes.StateMachineListItem{{Name: aws.String("order"), StateMachineArn: aws.String("arn:sfn:order")}}}
	resources, err := p.listStateMachinesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_sfn_state_machine" || resources[0].Tags["environment"] != envProd {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListSecrets(t *testing.T) {
	mock := &mockSecretsClient{secrets: []smtypes.SecretListEntry{
		{Name: aws.String("db/password"), ARN: aws.String("arn:sm:db"), Tags: []smtypes.Tag{{Key: aws.String("owner"), Value: aws.String("x")}}},
		{Name: aws.String("bare"), ARN: aws.String("arn:sm:bare")},
	}}
	resources, err := testProvider().listSecretsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || resources[0].Tags["owner"] != "x" || resources[1].Tags == nil {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListStacks_SkipsDeleted(t *testing.T) {
	mock := &mockCloudFormationClient{stacks: []cfntypes.Stack{
		{StackName: aws.String("live"), StackId: aws.String("arn:cfn:live"), StackStatus: cfntypes.StackStatusCreateComplete, Tags: []cfntypes.Tag{{Key: aws.String("environment"), Value: aws.String(envProd)}}},
		{StackName: aws.String("gone"), StackId: aws.String("arn:cfn:gone"), StackStatus: cfntypes.StackStatusDeleteComplete},
	}}
	resources, err := testProvider().listStacksFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Name != "live" || resources[0].Tags["environment"] != envProd {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListAlarms(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:cw:cpu": {"owner": "x"}})
	mock := &mockCloudWatchClient{
		metric:    []cwtypes.MetricAlarm{{AlarmName: aws.String("cpu"), AlarmArn: aws.String("arn:cw:cpu")}},
		composite: []cwtypes.CompositeAlarm{{AlarmName: aws.String("all"), AlarmArn: aws.String("arn:cw:all")}},
	}
	resources, err := p.listAlarmsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	if resources[0].Type != "aws_cloudwatch_metric_alarm" || resources[0].Tags["owner"] != "x" || resources[1].Type != "aws_cloudwatch_composite_alarm" {
		t.Errorf("got %+v", resources)
	}
}

func TestListEventRules_Paginates(t *testing.T) {
	mock := &mockEventBridgeClient{pages: [][]ebridgetypes.Rule{
		{{Name: aws.String("nightly"), Arn: aws.String("arn:events:nightly")}},
		{{Name: aws.String("hourly"), Arn: aws.String("arn:events:hourly")}},
	}}
	resources, err := bulkProvider(nil).listEventRulesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || mock.calls != 2 || resources[0].Type != "aws_cloudwatch_event_rule" {
		t.Errorf("resources = %+v, calls = %d, err = %v", resources, mock.calls, err)
	}
}

func TestListCertificates(t *testing.T) {
	arn := "arn:aws:acm:us-east-1:123456789012:certificate/abc-123"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	mock := &mockACMClient{certs: []acmtypes.CertificateSummary{{CertificateArn: aws.String(arn), DomainName: aws.String("example.com")}}}
	resources, err := p.listCertificatesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ID != "abc-123" || resources[0].Name != "example.com" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListUserPools(t *testing.T) {
	arn := "arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_abc"
	p := bulkProvider(map[string]map[string]string{arn: {"environment": envProd}})
	mock := &mockCognitoClient{pools: []cognitotypes.UserPoolDescriptionType{{Id: aws.String("us-east-1_abc"), Name: aws.String("customers")}}}
	resources, err := p.listUserPoolsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != arn || resources[0].Tags["environment"] != envProd {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListCodeBuildProjects_ReadsTagsFromBulkSource(t *testing.T) {
	arn := "arn:aws:codebuild:us-east-1:123456789012:project/deploy"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	mock := &mockCodeBuildClient{names: []string{"deploy", "untagged"}}
	resources, err := p.listCodeBuildProjectsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	if resources[0].ARN != arn || resources[0].Type != "aws_codebuild_project" || resources[0].Tags["owner"] != "x" {
		t.Errorf("tagged project = %+v", resources[0])
	}
	if resources[1].Tags == nil || len(resources[1].Tags) != 0 {
		t.Errorf("untagged project tags = %v, want empty", resources[1].Tags)
	}
}

func TestListCodeBuildProjects_SkippedWithoutBulkTags(t *testing.T) {
	mock := &mockCodeBuildClient{names: []string{"deploy"}}
	resources, err := testProvider().listCodeBuildProjectsFrom(context.Background(), mock, defaultRegion)
	if err != nil || resources != nil {
		t.Errorf("resources = %+v, err = %v, want the service skipped", resources, err)
	}
}

func TestListBackupVaults(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:backup:default": {"owner": "x"}})
	mock := &mockBackupClient{vaults: []backuptypes.BackupVaultListMember{{BackupVaultName: aws.String("default"), BackupVaultArn: aws.String("arn:backup:default")}}}
	resources, err := p.listBackupVaultsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_backup_vault" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListFSxFileSystems(t *testing.T) {
	mock := &mockFSxClient{systems: []fsxtypes.FileSystem{{
		FileSystemId: aws.String("fs-1"), ResourceARN: aws.String("arn:fsx:fs-1"),
		Tags: []fsxtypes.Tag{{Key: aws.String("Name"), Value: aws.String("shared")}},
	}}}
	resources, err := testProvider().listFSxFileSystemsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Name != "shared" || resources[0].ID != "fs-1" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListBeanstalkEnvironments(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:eb:env": {"owner": "x"}})
	mock := &mockBeanstalkClient{envs: []ebtypes.EnvironmentDescription{{EnvironmentId: aws.String("e-1"), EnvironmentName: aws.String("web-prod"), EnvironmentArn: aws.String("arn:eb:env")}}}
	resources, err := p.listBeanstalkEnvironmentsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ID != "e-1" || resources[0].Name != "web-prod" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestAppListers_SkipWithoutBulkTags(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	for name, list := range map[string]func() (int, error){
		"sfn": func() (int, error) {
			r, err := p.listStateMachinesFrom(ctx, &mockStepFunctionsClient{machines: []sfntypes.StateMachineListItem{{Name: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"alarms": func() (int, error) {
			r, err := p.listAlarmsFrom(ctx, &mockCloudWatchClient{metric: []cwtypes.MetricAlarm{{AlarmName: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"events": func() (int, error) {
			r, err := p.listEventRulesFrom(ctx, &mockEventBridgeClient{pages: [][]ebridgetypes.Rule{{{Name: aws.String("x")}}}}, defaultRegion)
			return len(r), err
		},
		"acm": func() (int, error) {
			r, err := p.listCertificatesFrom(ctx, &mockACMClient{certs: []acmtypes.CertificateSummary{{CertificateArn: aws.String("arn:x")}}}, defaultRegion)
			return len(r), err
		},
		"cognito": func() (int, error) {
			r, err := p.listUserPoolsFrom(ctx, &mockCognitoClient{pools: []cognitotypes.UserPoolDescriptionType{{Id: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"backup": func() (int, error) {
			r, err := p.listBackupVaultsFrom(ctx, &mockBackupClient{vaults: []backuptypes.BackupVaultListMember{{BackupVaultName: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"beanstalk": func() (int, error) {
			r, err := p.listBeanstalkEnvironmentsFrom(ctx, &mockBeanstalkClient{envs: []ebtypes.EnvironmentDescription{{EnvironmentId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
	} {
		if n, err := list(); err != nil || n != 0 {
			t.Errorf("%s: want no resources and no error without bulk tags, got %d, %v", name, n, err)
		}
	}
}

func TestAppListers_Error(t *testing.T) {
	boom := errors.New("boom")
	p := bulkProvider(nil)
	ctx := context.Background()
	for name, list := range map[string]func() error{
		"sfn": func() error {
			_, err := p.listStateMachinesFrom(ctx, &mockStepFunctionsClient{err: boom}, defaultRegion)
			return err
		},
		"secrets": func() error {
			_, err := p.listSecretsFrom(ctx, &mockSecretsClient{err: boom}, defaultRegion)
			return err
		},
		"cfn": func() error {
			_, err := p.listStacksFrom(ctx, &mockCloudFormationClient{err: boom}, defaultRegion)
			return err
		},
		"alarms": func() error {
			_, err := p.listAlarmsFrom(ctx, &mockCloudWatchClient{err: boom}, defaultRegion)
			return err
		},
		"events": func() error {
			_, err := p.listEventRulesFrom(ctx, &mockEventBridgeClient{err: boom}, defaultRegion)
			return err
		},
		"acm": func() error {
			_, err := p.listCertificatesFrom(ctx, &mockACMClient{err: boom}, defaultRegion)
			return err
		},
		"cognito": func() error {
			_, err := p.listUserPoolsFrom(ctx, &mockCognitoClient{err: boom}, defaultRegion)
			return err
		},
		"codebuild": func() error {
			_, err := p.listCodeBuildProjectsFrom(ctx, &mockCodeBuildClient{err: boom}, defaultRegion)
			return err
		},
		"backup": func() error {
			_, err := p.listBackupVaultsFrom(ctx, &mockBackupClient{err: boom}, defaultRegion)
			return err
		},
		"fsx": func() error {
			_, err := p.listFSxFileSystemsFrom(ctx, &mockFSxClient{err: boom}, defaultRegion)
			return err
		},
		"beanstalk": func() error {
			_, err := p.listBeanstalkEnvironmentsFrom(ctx, &mockBeanstalkClient{err: boom}, defaultRegion)
			return err
		},
	} {
		if list() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
