package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/amplify"
	amplifytypes "github.com/aws/aws-sdk-go-v2/service/amplify/types"
	"github.com/aws/aws-sdk-go-v2/service/appsync"
	appsynctypes "github.com/aws/aws-sdk-go-v2/service/appsync/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/aws/aws-sdk-go-v2/service/codeartifact"
	codeartifacttypes "github.com/aws/aws-sdk-go-v2/service/codeartifact/types"
	"github.com/aws/aws-sdk-go-v2/service/codecommit"
	codecommittypes "github.com/aws/aws-sdk-go-v2/service/codecommit/types"
	"github.com/aws/aws-sdk-go-v2/service/codepipeline"
	codepipelinetypes "github.com/aws/aws-sdk-go-v2/service/codepipeline/types"
	"github.com/aws/aws-sdk-go-v2/service/servicecatalog"
	sctypes "github.com/aws/aws-sdk-go-v2/service/servicecatalog/types"
	"github.com/aws/aws-sdk-go-v2/service/wellarchitected"
	watypes "github.com/aws/aws-sdk-go-v2/service/wellarchitected/types"
)

type mockAmplifyClient struct {
	pages [][]amplifytypes.App
	err   error
	calls int
}

func (m *mockAmplifyClient) ListApps(ctx context.Context, params *amplify.ListAppsInput, optFns ...func(*amplify.Options)) (*amplify.ListAppsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	page := m.pages[m.calls]
	m.calls++
	out := &amplify.ListAppsOutput{Apps: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

type mockAppSyncClient struct {
	apis []appsynctypes.GraphqlApi
	err  error
}

func (m *mockAppSyncClient) ListGraphqlApis(ctx context.Context, params *appsync.ListGraphqlApisInput, optFns ...func(*appsync.Options)) (*appsync.ListGraphqlApisOutput, error) {
	return &appsync.ListGraphqlApisOutput{GraphqlApis: m.apis}, m.err
}

type mockBedrockClient struct {
	guardrails []bedrocktypes.GuardrailSummary
	err        error
}

func (m *mockBedrockClient) ListGuardrails(ctx context.Context, params *bedrock.ListGuardrailsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListGuardrailsOutput, error) {
	return &bedrock.ListGuardrailsOutput{Guardrails: m.guardrails}, m.err
}

type mockCodeArtifactClient struct {
	domains []codeartifacttypes.DomainSummary
	repos   []codeartifacttypes.RepositorySummary
	err     error
}

func (m *mockCodeArtifactClient) ListDomains(ctx context.Context, params *codeartifact.ListDomainsInput, optFns ...func(*codeartifact.Options)) (*codeartifact.ListDomainsOutput, error) {
	return &codeartifact.ListDomainsOutput{Domains: m.domains}, m.err
}

func (m *mockCodeArtifactClient) ListRepositories(ctx context.Context, params *codeartifact.ListRepositoriesInput, optFns ...func(*codeartifact.Options)) (*codeartifact.ListRepositoriesOutput, error) {
	return &codeartifact.ListRepositoriesOutput{Repositories: m.repos}, m.err
}

type mockCodeCommitClient struct {
	repos []codecommittypes.RepositoryNameIdPair
	err   error
}

func (m *mockCodeCommitClient) ListRepositories(ctx context.Context, params *codecommit.ListRepositoriesInput, optFns ...func(*codecommit.Options)) (*codecommit.ListRepositoriesOutput, error) {
	return &codecommit.ListRepositoriesOutput{Repositories: m.repos}, m.err
}

type mockCodePipelineClient struct {
	pipelines []codepipelinetypes.PipelineSummary
	err       error
}

func (m *mockCodePipelineClient) ListPipelines(ctx context.Context, params *codepipeline.ListPipelinesInput, optFns ...func(*codepipeline.Options)) (*codepipeline.ListPipelinesOutput, error) {
	return &codepipeline.ListPipelinesOutput{Pipelines: m.pipelines}, m.err
}

type mockServiceCatalogClient struct {
	portfolios []sctypes.PortfolioDetail
	err        error
}

func (m *mockServiceCatalogClient) ListPortfolios(ctx context.Context, params *servicecatalog.ListPortfoliosInput, optFns ...func(*servicecatalog.Options)) (*servicecatalog.ListPortfoliosOutput, error) {
	return &servicecatalog.ListPortfoliosOutput{PortfolioDetails: m.portfolios}, m.err
}

type mockWellArchitectedClient struct {
	workloads []watypes.WorkloadSummary
	err       error
}

func (m *mockWellArchitectedClient) ListWorkloads(ctx context.Context, params *wellarchitected.ListWorkloadsInput, optFns ...func(*wellarchitected.Options)) (*wellarchitected.ListWorkloadsOutput, error) {
	return &wellarchitected.ListWorkloadsOutput{WorkloadSummaries: m.workloads}, m.err
}

func TestListAmplifyApps_Paginates(t *testing.T) {
	mock := &mockAmplifyClient{pages: [][]amplifytypes.App{
		{{AppId: aws.String("a1"), Name: aws.String("web"), AppArn: aws.String("arn:amplify:a1"), Tags: map[string]string{"owner": "x"}}},
		{{AppId: aws.String("a2"), Name: aws.String("docs"), AppArn: aws.String("arn:amplify:a2")}},
	}}
	resources, err := testProvider().listAmplifyAppsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || mock.calls != 2 || resources[0].Tags["owner"] != "x" || resources[1].Tags == nil {
		t.Errorf("resources = %+v, calls = %d, err = %v", resources, mock.calls, err)
	}
}

func TestListGraphQLAPIs(t *testing.T) {
	mock := &mockAppSyncClient{apis: []appsynctypes.GraphqlApi{{ApiId: aws.String("g1"), Name: aws.String("orders"), Arn: aws.String("arn:appsync:g1"), Tags: map[string]string{"environment": envProd}}}}
	resources, err := testProvider().listGraphQLAPIsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_appsync_graphql_api" || resources[0].Tags["environment"] != envProd {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListGuardrails(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:bedrock:g1": {"owner": "x"}})
	mock := &mockBedrockClient{guardrails: []bedrocktypes.GuardrailSummary{{Id: aws.String("g1"), Name: aws.String("pii"), Arn: aws.String("arn:bedrock:g1")}}}
	resources, err := p.listGuardrailsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListCodeArtifactResources(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:ca:repo": {"owner": "x"}})
	mock := &mockCodeArtifactClient{
		domains: []codeartifacttypes.DomainSummary{{Name: aws.String("corp"), Arn: aws.String("arn:ca:domain")}},
		repos:   []codeartifacttypes.RepositorySummary{{Name: aws.String("npm"), DomainName: aws.String("corp"), Arn: aws.String("arn:ca:repo")}},
	}
	resources, err := p.listCodeArtifactResourcesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || resources[0].Type != "aws_codeartifact_domain" || resources[1].ID != "corp/npm" || resources[1].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListCodeCommitRepositories(t *testing.T) {
	arn := "arn:aws:codecommit:us-east-1:123456789012:api"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	mock := &mockCodeCommitClient{repos: []codecommittypes.RepositoryNameIdPair{{RepositoryName: aws.String("api")}}}
	resources, err := p.listCodeCommitRepositoriesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != arn || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListPipelines(t *testing.T) {
	arn := "arn:aws:codepipeline:us-east-1:123456789012:deploy"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	mock := &mockCodePipelineClient{pipelines: []codepipelinetypes.PipelineSummary{{Name: aws.String("deploy")}}}
	resources, err := p.listPipelinesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != arn || resources[0].Type != "aws_codepipeline" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListPortfolios(t *testing.T) {
	p := bulkProvider(nil)
	mock := &mockServiceCatalogClient{portfolios: []sctypes.PortfolioDetail{{Id: aws.String("port-1"), DisplayName: aws.String("Platform"), ARN: aws.String("arn:catalog:port-1")}}}
	resources, err := p.listPortfoliosFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Name != "Platform" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListWorkloads(t *testing.T) {
	p := bulkProvider(nil)
	mock := &mockWellArchitectedClient{workloads: []watypes.WorkloadSummary{{WorkloadId: aws.String("w1"), WorkloadName: aws.String("shop"), WorkloadArn: aws.String("arn:wa:w1")}}}
	resources, err := p.listWorkloadsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_wellarchitected_workload" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestDevToolsListers_SkipWithoutBulkTags(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	for name, list := range map[string]func() (int, error){
		"bedrock": func() (int, error) {
			r, err := p.listGuardrailsFrom(ctx, &mockBedrockClient{guardrails: []bedrocktypes.GuardrailSummary{{Id: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"codeartifact": func() (int, error) {
			r, err := p.listCodeArtifactResourcesFrom(ctx, &mockCodeArtifactClient{domains: []codeartifacttypes.DomainSummary{{Name: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"codecommit": func() (int, error) {
			r, err := p.listCodeCommitRepositoriesFrom(ctx, &mockCodeCommitClient{repos: []codecommittypes.RepositoryNameIdPair{{RepositoryName: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"codepipeline": func() (int, error) {
			r, err := p.listPipelinesFrom(ctx, &mockCodePipelineClient{pipelines: []codepipelinetypes.PipelineSummary{{Name: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"servicecatalog": func() (int, error) {
			r, err := p.listPortfoliosFrom(ctx, &mockServiceCatalogClient{portfolios: []sctypes.PortfolioDetail{{Id: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"wellarchitected": func() (int, error) {
			r, err := p.listWorkloadsFrom(ctx, &mockWellArchitectedClient{workloads: []watypes.WorkloadSummary{{WorkloadId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
	} {
		if n, err := list(); err != nil || n != 0 {
			t.Errorf("%s: want no resources and no error without bulk tags, got %d, %v", name, n, err)
		}
	}
}

func TestDevToolsListers_Error(t *testing.T) {
	boom := errors.New("boom")
	p := bulkProvider(nil)
	ctx := context.Background()
	for name, list := range map[string]func() error{
		"amplify": func() error {
			_, err := p.listAmplifyAppsFrom(ctx, &mockAmplifyClient{err: boom}, defaultRegion)
			return err
		},
		"appsync": func() error {
			_, err := p.listGraphQLAPIsFrom(ctx, &mockAppSyncClient{err: boom}, defaultRegion)
			return err
		},
		"bedrock": func() error {
			_, err := p.listGuardrailsFrom(ctx, &mockBedrockClient{err: boom}, defaultRegion)
			return err
		},
		"codeartifact": func() error {
			_, err := p.listCodeArtifactResourcesFrom(ctx, &mockCodeArtifactClient{err: boom}, defaultRegion)
			return err
		},
		"codecommit": func() error {
			_, err := p.listCodeCommitRepositoriesFrom(ctx, &mockCodeCommitClient{err: boom}, defaultRegion)
			return err
		},
		"codepipeline": func() error {
			_, err := p.listPipelinesFrom(ctx, &mockCodePipelineClient{err: boom}, defaultRegion)
			return err
		},
		"servicecatalog": func() error {
			_, err := p.listPortfoliosFrom(ctx, &mockServiceCatalogClient{err: boom}, defaultRegion)
			return err
		},
		"wellarchitected": func() error {
			_, err := p.listWorkloadsFrom(ctx, &mockWellArchitectedClient{err: boom}, defaultRegion)
			return err
		},
	} {
		if list() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
