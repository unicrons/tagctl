package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/backup"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/sfn"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Application, operations and security services.

type stepFunctionsAPI interface {
	ListStateMachines(ctx context.Context, params *sfn.ListStateMachinesInput, optFns ...func(*sfn.Options)) (*sfn.ListStateMachinesOutput, error)
}

type secretsManagerAPI interface {
	ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error)
}

type cloudFormationAPI interface {
	DescribeStacks(ctx context.Context, params *cloudformation.DescribeStacksInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DescribeStacksOutput, error)
}

type cloudWatchAPI interface {
	DescribeAlarms(ctx context.Context, params *cloudwatch.DescribeAlarmsInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error)
}

type eventBridgeAPI interface {
	ListRules(ctx context.Context, params *eventbridge.ListRulesInput, optFns ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error)
}

type acmAPI interface {
	ListCertificates(ctx context.Context, params *acm.ListCertificatesInput, optFns ...func(*acm.Options)) (*acm.ListCertificatesOutput, error)
}

type cognitoAPI interface {
	ListUserPools(ctx context.Context, params *cognitoidentityprovider.ListUserPoolsInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListUserPoolsOutput, error)
}

type codeBuildAPI interface {
	ListProjects(ctx context.Context, params *codebuild.ListProjectsInput, optFns ...func(*codebuild.Options)) (*codebuild.ListProjectsOutput, error)
}

type backupAPI interface {
	ListBackupVaults(ctx context.Context, params *backup.ListBackupVaultsInput, optFns ...func(*backup.Options)) (*backup.ListBackupVaultsOutput, error)
}

type fsxAPI interface {
	DescribeFileSystems(ctx context.Context, params *fsx.DescribeFileSystemsInput, optFns ...func(*fsx.Options)) (*fsx.DescribeFileSystemsOutput, error)
}

type beanstalkAPI interface {
	DescribeEnvironments(ctx context.Context, params *elasticbeanstalk.DescribeEnvironmentsInput, optFns ...func(*elasticbeanstalk.Options)) (*elasticbeanstalk.DescribeEnvironmentsOutput, error)
}

// cognitoPageSize is the maximum ListUserPools page size.
const cognitoPageSize = 60

func (p *Provider) listStateMachines(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listStateMachinesFrom(ctx, regionalClient(p, region, sfn.NewFromConfig), region)
}

func (p *Provider) listStateMachinesFrom(ctx context.Context, client stepFunctionsAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "Step Functions") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := sfn.NewListStateMachinesPaginator(client, &sfn.ListStateMachinesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_state_machines", "", err)
		}
		for _, sm := range output.StateMachines {
			name := aws.ToString(sm.Name)
			arn := aws.ToString(sm.StateMachineArn)
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: arn, Type: "aws_sfn_state_machine",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn), CreatedAt: sm.CreationDate,
			})
		}
	}
	log.Debug("AWS Step Functions: Found %d state machines in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listSecrets(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSecretsFrom(ctx, regionalClient(p, region, secretsmanager.NewFromConfig), region)
}

func (p *Provider) listSecretsFrom(ctx context.Context, client secretsManagerAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := secretsmanager.NewListSecretsPaginator(client, &secretsmanager.ListSecretsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_secrets", "", err)
		}
		for _, s := range output.SecretList {
			name := aws.ToString(s.Name)
			tags := tagsToMap(s.Tags,
				func(t smtypes.Tag) *string { return t.Key },
				func(t smtypes.Tag) *string { return t.Value })
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: aws.ToString(s.ARN), Type: "aws_secretsmanager_secret",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: tags, CreatedAt: s.CreatedDate,
			})
		}
	}
	log.Debug("AWS Secrets Manager: Found %d secrets in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listStacks(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listStacksFrom(ctx, regionalClient(p, region, cloudformation.NewFromConfig), region)
}

func (p *Provider) listStacksFrom(ctx context.Context, client cloudFormationAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := cloudformation.NewDescribeStacksPaginator(client, &cloudformation.DescribeStacksInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_stacks", "", err)
		}
		for _, s := range output.Stacks {
			if s.StackStatus == cfntypes.StackStatusDeleteComplete {
				continue
			}
			name := aws.ToString(s.StackName)
			tags := tagsToMap(s.Tags,
				func(t cfntypes.Tag) *string { return t.Key },
				func(t cfntypes.Tag) *string { return t.Value })
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: aws.ToString(s.StackId), Type: "aws_cloudformation_stack",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: tags, CreatedAt: s.CreationTime,
			})
		}
	}
	log.Debug("AWS CloudFormation: Found %d stacks in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listAlarms(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listAlarmsFrom(ctx, regionalClient(p, region, cloudwatch.NewFromConfig), region)
}

func (p *Provider) listAlarmsFrom(ctx context.Context, client cloudWatchAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "CloudWatch alarms") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := cloudwatch.NewDescribeAlarmsPaginator(client, &cloudwatch.DescribeAlarmsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_alarms", "", err)
		}
		for _, a := range output.MetricAlarms {
			resources = append(resources, p.alarmResource(ctx, region, aws.ToString(a.AlarmName), aws.ToString(a.AlarmArn), "aws_cloudwatch_metric_alarm"))
		}
		for _, a := range output.CompositeAlarms {
			resources = append(resources, p.alarmResource(ctx, region, aws.ToString(a.AlarmName), aws.ToString(a.AlarmArn), "aws_cloudwatch_composite_alarm"))
		}
	}
	log.Debug("AWS CloudWatch: Found %d alarms in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) alarmResource(ctx context.Context, region, name, arn, resourceType string) types.Resource {
	return types.Resource{
		ID: name, Name: name, ARN: arn, Type: resourceType,
		Region: region, Account: p.accountID, Provider: providerName,
		Tags: p.bulkTags(ctx, region, arn),
	}
}

func (p *Provider) listEventRules(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listEventRulesFrom(ctx, regionalClient(p, region, eventbridge.NewFromConfig), region)
}

func (p *Provider) listEventRulesFrom(ctx context.Context, client eventBridgeAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "EventBridge") {
		return nil, nil
	}
	var resources []types.Resource
	err := paginate(func(token *string) (*string, error) {
		output, err := client.ListRules(ctx, &eventbridge.ListRulesInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, r := range output.Rules {
			name := aws.ToString(r.Name)
			arn := aws.ToString(r.Arn)
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: arn, Type: "aws_cloudwatch_event_rule",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn),
			})
		}
		return output.NextToken, nil
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_event_rules", "", err)
	}
	log.Debug("AWS EventBridge: Found %d rules in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listCertificates(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listCertificatesFrom(ctx, regionalClient(p, region, acm.NewFromConfig), region)
}

func (p *Provider) listCertificatesFrom(ctx context.Context, client acmAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "ACM") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := acm.NewListCertificatesPaginator(client, &acm.ListCertificatesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_certificates", "", err)
		}
		for _, c := range output.CertificateSummaryList {
			arn := aws.ToString(c.CertificateArn)
			resources = append(resources, types.Resource{
				ID: nameFromARN(arn), Name: aws.ToString(c.DomainName), ARN: arn, Type: "aws_acm_certificate",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn), CreatedAt: c.CreatedAt,
			})
		}
	}
	log.Debug("AWS ACM: Found %d certificates in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listUserPools(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listUserPoolsFrom(ctx, regionalClient(p, region, cognitoidentityprovider.NewFromConfig), region)
}

func (p *Provider) listUserPoolsFrom(ctx context.Context, client cognitoAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "Cognito") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := cognitoidentityprovider.NewListUserPoolsPaginator(client, &cognitoidentityprovider.ListUserPoolsInput{MaxResults: aws.Int32(cognitoPageSize)})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_user_pools", "", err)
		}
		for _, u := range output.UserPools {
			id := aws.ToString(u.Id)
			arn := p.buildARN("cognito-idp", region, p.accountID, "userpool/"+id)
			resources = append(resources, types.Resource{
				ID: id, Name: aws.ToString(u.Name), ARN: arn, Type: "aws_cognito_user_pool",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn), CreatedAt: u.CreationDate,
			})
		}
	}
	log.Debug("AWS Cognito: Found %d user pools in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listCodeBuildProjects(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listCodeBuildProjectsFrom(ctx, regionalClient(p, region, codebuild.NewFromConfig), region)
}

// BatchGetProjects is avoided: it returns every environment variable in clear.
func (p *Provider) listCodeBuildProjectsFrom(ctx context.Context, client codeBuildAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "CodeBuild") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := codebuild.NewListProjectsPaginator(client, &codebuild.ListProjectsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_codebuild_projects", "", err)
		}
		for _, name := range output.Projects {
			arn := p.buildARN("codebuild", region, p.accountID, "project/"+name)
			resources = append(resources, p.bulkResource(ctx, region, "aws_codebuild_project", name, name, arn, nil))
		}
	}
	log.Debug("AWS CodeBuild: Found %d projects in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listBackupVaults(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listBackupVaultsFrom(ctx, regionalClient(p, region, backup.NewFromConfig), region)
}

func (p *Provider) listBackupVaultsFrom(ctx context.Context, client backupAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "Backup") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := backup.NewListBackupVaultsPaginator(client, &backup.ListBackupVaultsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_backup_vaults", "", err)
		}
		for _, v := range output.BackupVaultList {
			name := aws.ToString(v.BackupVaultName)
			arn := aws.ToString(v.BackupVaultArn)
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: arn, Type: "aws_backup_vault",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn), CreatedAt: v.CreationDate,
			})
		}
	}
	log.Debug("AWS Backup: Found %d vaults in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listFSxFileSystems(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listFSxFileSystemsFrom(ctx, regionalClient(p, region, fsx.NewFromConfig), region)
}

func (p *Provider) listFSxFileSystemsFrom(ctx context.Context, client fsxAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := fsx.NewDescribeFileSystemsPaginator(client, &fsx.DescribeFileSystemsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_fsx_file_systems", "", err)
		}
		for _, fs := range output.FileSystems {
			id := aws.ToString(fs.FileSystemId)
			tags := tagsToMap(fs.Tags,
				func(t fsxtypes.Tag) *string { return t.Key },
				func(t fsxtypes.Tag) *string { return t.Value })
			r := types.Resource{
				ID: id, Name: id, ARN: aws.ToString(fs.ResourceARN), Type: "aws_fsx_file_system",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: tags, CreatedAt: fs.CreationTime,
			}
			if name, ok := r.Tags["Name"]; ok {
				r.Name = name
			}
			resources = append(resources, r)
		}
	}
	log.Debug("AWS FSx: Found %d file systems in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listBeanstalkEnvironments(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listBeanstalkEnvironmentsFrom(ctx, regionalClient(p, region, elasticbeanstalk.NewFromConfig), region)
}

func (p *Provider) listBeanstalkEnvironmentsFrom(ctx context.Context, client beanstalkAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "Elastic Beanstalk") {
		return nil, nil
	}
	var resources []types.Resource
	err := paginate(func(token *string) (*string, error) {
		output, err := client.DescribeEnvironments(ctx, &elasticbeanstalk.DescribeEnvironmentsInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, e := range output.Environments {
			name := aws.ToString(e.EnvironmentName)
			arn := aws.ToString(e.EnvironmentArn)
			resources = append(resources, types.Resource{
				ID: aws.ToString(e.EnvironmentId), Name: name, ARN: arn, Type: "aws_elastic_beanstalk_environment",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn), CreatedAt: e.DateCreated,
			})
		}
		return output.NextToken, nil
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_beanstalk_environments", "", err)
	}
	log.Debug("AWS Elastic Beanstalk: Found %d environments in %s", len(resources), region)
	return resources, nil
}
