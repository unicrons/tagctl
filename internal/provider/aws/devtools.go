package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/amplify"
	"github.com/aws/aws-sdk-go-v2/service/appsync"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/codeartifact"
	"github.com/aws/aws-sdk-go-v2/service/codecommit"
	"github.com/aws/aws-sdk-go-v2/service/codepipeline"
	"github.com/aws/aws-sdk-go-v2/service/servicecatalog"
	"github.com/aws/aws-sdk-go-v2/service/wellarchitected"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Developer tools, application platforms and governance catalogs.

type amplifyAPI interface {
	ListApps(ctx context.Context, params *amplify.ListAppsInput, optFns ...func(*amplify.Options)) (*amplify.ListAppsOutput, error)
}

type appSyncAPI interface {
	ListGraphqlApis(ctx context.Context, params *appsync.ListGraphqlApisInput, optFns ...func(*appsync.Options)) (*appsync.ListGraphqlApisOutput, error)
}

type bedrockAPI interface {
	ListGuardrails(ctx context.Context, params *bedrock.ListGuardrailsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListGuardrailsOutput, error)
}

type codeArtifactAPI interface {
	ListDomains(ctx context.Context, params *codeartifact.ListDomainsInput, optFns ...func(*codeartifact.Options)) (*codeartifact.ListDomainsOutput, error)
	ListRepositories(ctx context.Context, params *codeartifact.ListRepositoriesInput, optFns ...func(*codeartifact.Options)) (*codeartifact.ListRepositoriesOutput, error)
}

type codeCommitAPI interface {
	ListRepositories(ctx context.Context, params *codecommit.ListRepositoriesInput, optFns ...func(*codecommit.Options)) (*codecommit.ListRepositoriesOutput, error)
}

type codePipelineAPI interface {
	ListPipelines(ctx context.Context, params *codepipeline.ListPipelinesInput, optFns ...func(*codepipeline.Options)) (*codepipeline.ListPipelinesOutput, error)
}

type serviceCatalogAPI interface {
	ListPortfolios(ctx context.Context, params *servicecatalog.ListPortfoliosInput, optFns ...func(*servicecatalog.Options)) (*servicecatalog.ListPortfoliosOutput, error)
}

type wellArchitectedAPI interface {
	ListWorkloads(ctx context.Context, params *wellarchitected.ListWorkloadsInput, optFns ...func(*wellarchitected.Options)) (*wellarchitected.ListWorkloadsOutput, error)
}

func (p *Provider) listAmplifyApps(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listAmplifyAppsFrom(ctx, regionalClient(p, region, amplify.NewFromConfig), region)
}

func (p *Provider) listAmplifyAppsFrom(ctx context.Context, client amplifyAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	err := paginate(func(token *string) (*string, error) {
		output, err := client.ListApps(ctx, &amplify.ListAppsInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, app := range output.Apps {
			resources = append(resources, p.resource(region, "aws_amplify_app", aws.ToString(app.AppId), aws.ToString(app.Name), aws.ToString(app.AppArn), app.Tags, app.CreateTime))
		}
		return output.NextToken, nil
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_amplify_apps", "", err)
	}
	log.Debug("AWS Amplify: Found %d apps in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listGraphQLAPIs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listGraphQLAPIsFrom(ctx, regionalClient(p, region, appsync.NewFromConfig), region)
}

func (p *Provider) listGraphQLAPIsFrom(ctx context.Context, client appSyncAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := appsync.NewListGraphqlApisPaginator(client, &appsync.ListGraphqlApisInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_graphql_apis", "", err)
		}
		for _, api := range output.GraphqlApis {
			resources = append(resources, p.resource(region, "aws_appsync_graphql_api", aws.ToString(api.ApiId), aws.ToString(api.Name), aws.ToString(api.Arn), api.Tags, nil))
		}
	}
	log.Debug("AWS AppSync: Found %d GraphQL APIs in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listGuardrails(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listGuardrailsFrom(ctx, regionalClient(p, region, bedrock.NewFromConfig), region)
}

func (p *Provider) listGuardrailsFrom(ctx context.Context, client bedrockAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Bedrock") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := bedrock.NewListGuardrailsPaginator(client, &bedrock.ListGuardrailsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_guardrails", "", err)
		}
		for _, g := range output.Guardrails {
			resources = append(resources, p.bulkResource(region, "aws_bedrock_guardrail", aws.ToString(g.Id), aws.ToString(g.Name), aws.ToString(g.Arn), g.CreatedAt))
		}
	}
	log.Debug("AWS Bedrock: Found %d guardrails in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listCodeArtifactResources(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listCodeArtifactResourcesFrom(ctx, regionalClient(p, region, codeartifact.NewFromConfig), region)
}

func (p *Provider) listCodeArtifactResourcesFrom(ctx context.Context, client codeArtifactAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "CodeArtifact") {
		return nil, nil
	}
	var resources []types.Resource
	domains := codeartifact.NewListDomainsPaginator(client, &codeartifact.ListDomainsInput{})
	for domains.HasMorePages() {
		output, err := domains.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_codeartifact_domains", "", err)
		}
		for _, d := range output.Domains {
			name := aws.ToString(d.Name)
			resources = append(resources, p.bulkResource(region, "aws_codeartifact_domain", name, name, aws.ToString(d.Arn), d.CreatedTime))
		}
	}
	repos := codeartifact.NewListRepositoriesPaginator(client, &codeartifact.ListRepositoriesInput{})
	for repos.HasMorePages() {
		output, err := repos.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_codeartifact_repositories", "", err)
		}
		for _, r := range output.Repositories {
			name := aws.ToString(r.Name)
			resources = append(resources, p.bulkResource(region, "aws_codeartifact_repository", aws.ToString(r.DomainName)+"/"+name, name, aws.ToString(r.Arn), nil))
		}
	}
	log.Debug("AWS CodeArtifact: Found %d domains and repositories in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listCodeCommitRepositories(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listCodeCommitRepositoriesFrom(ctx, regionalClient(p, region, codecommit.NewFromConfig), region)
}

func (p *Provider) listCodeCommitRepositoriesFrom(ctx context.Context, client codeCommitAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "CodeCommit") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := codecommit.NewListRepositoriesPaginator(client, &codecommit.ListRepositoriesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_codecommit_repositories", "", err)
		}
		for _, r := range output.Repositories {
			name := aws.ToString(r.RepositoryName)
			arn := p.buildARN("codecommit", region, p.accountID, name)
			resources = append(resources, p.bulkResource(region, "aws_codecommit_repository", name, name, arn, nil))
		}
	}
	log.Debug("AWS CodeCommit: Found %d repositories in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listPipelines(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listPipelinesFrom(ctx, regionalClient(p, region, codepipeline.NewFromConfig), region)
}

func (p *Provider) listPipelinesFrom(ctx context.Context, client codePipelineAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "CodePipeline") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := codepipeline.NewListPipelinesPaginator(client, &codepipeline.ListPipelinesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_pipelines", "", err)
		}
		for _, pl := range output.Pipelines {
			name := aws.ToString(pl.Name)
			arn := p.buildARN("codepipeline", region, p.accountID, name)
			resources = append(resources, p.bulkResource(region, "aws_codepipeline", name, name, arn, pl.Created))
		}
	}
	log.Debug("AWS CodePipeline: Found %d pipelines in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listPortfolios(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listPortfoliosFrom(ctx, regionalClient(p, region, servicecatalog.NewFromConfig), region)
}

func (p *Provider) listPortfoliosFrom(ctx context.Context, client serviceCatalogAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Service Catalog") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := servicecatalog.NewListPortfoliosPaginator(client, &servicecatalog.ListPortfoliosInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_portfolios", "", err)
		}
		for _, pf := range output.PortfolioDetails {
			resources = append(resources, p.bulkResource(region, "aws_servicecatalog_portfolio", aws.ToString(pf.Id), aws.ToString(pf.DisplayName), aws.ToString(pf.ARN), pf.CreatedTime))
		}
	}
	log.Debug("AWS Service Catalog: Found %d portfolios in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listWorkloads(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listWorkloadsFrom(ctx, regionalClient(p, region, wellarchitected.NewFromConfig), region)
}

func (p *Provider) listWorkloadsFrom(ctx context.Context, client wellArchitectedAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Well-Architected") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := wellarchitected.NewListWorkloadsPaginator(client, &wellarchitected.ListWorkloadsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_workloads", "", err)
		}
		for _, w := range output.WorkloadSummaries {
			resources = append(resources, p.bulkResource(region, "aws_wellarchitected_workload", aws.ToString(w.WorkloadId), aws.ToString(w.WorkloadName), aws.ToString(w.WorkloadArn), nil))
		}
	}
	log.Debug("AWS Well-Architected: Found %d workloads in %s", len(resources), region)
	return resources, nil
}
