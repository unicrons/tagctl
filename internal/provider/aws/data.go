package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/kafka"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Data and analytics services. Those whose Describe call returns tags inline
// need nothing else; the rest read tags from the bulk source.

type redshiftAPI interface {
	DescribeClusters(ctx context.Context, params *redshift.DescribeClustersInput, optFns ...func(*redshift.Options)) (*redshift.DescribeClustersOutput, error)
}

type openSearchAPI interface {
	ListDomainNames(ctx context.Context, params *opensearch.ListDomainNamesInput, optFns ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error)
	DescribeDomains(ctx context.Context, params *opensearch.DescribeDomainsInput, optFns ...func(*opensearch.Options)) (*opensearch.DescribeDomainsOutput, error)
}

type mskAPI interface {
	ListClustersV2(ctx context.Context, params *kafka.ListClustersV2Input, optFns ...func(*kafka.Options)) (*kafka.ListClustersV2Output, error)
}

type glueAPI interface {
	GetJobs(ctx context.Context, params *glue.GetJobsInput, optFns ...func(*glue.Options)) (*glue.GetJobsOutput, error)
}

type firehoseAPI interface {
	ListDeliveryStreams(ctx context.Context, params *firehose.ListDeliveryStreamsInput, optFns ...func(*firehose.Options)) (*firehose.ListDeliveryStreamsOutput, error)
}

type sageMakerAPI interface {
	ListEndpoints(ctx context.Context, params *sagemaker.ListEndpointsInput, optFns ...func(*sagemaker.Options)) (*sagemaker.ListEndpointsOutput, error)
	ListNotebookInstances(ctx context.Context, params *sagemaker.ListNotebookInstancesInput, optFns ...func(*sagemaker.Options)) (*sagemaker.ListNotebookInstancesOutput, error)
}

// openSearchDescribeBatch is the maximum number of domains per DescribeDomains call.
const openSearchDescribeBatch = 5

func (p *Provider) listRedshiftClusters(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listRedshiftClustersFrom(ctx, regionalClient(p, region, redshift.NewFromConfig), region)
}

func (p *Provider) listRedshiftClustersFrom(ctx context.Context, client redshiftAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := redshift.NewDescribeClustersPaginator(client, &redshift.DescribeClustersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_redshift_clusters", "", err)
		}
		for _, c := range output.Clusters {
			id := aws.ToString(c.ClusterIdentifier)
			resources = append(resources, types.Resource{
				ID:        id,
				Name:      id,
				ARN:       p.buildARN("redshift", region, p.accountID, "cluster:"+id),
				Type:      "aws_redshift_cluster",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      redshiftTagsToMap(c.Tags),
				CreatedAt: c.ClusterCreateTime,
			})
		}
	}
	log.Debug("AWS Redshift: Found %d clusters in %s", len(resources), region)
	return resources, nil
}

func redshiftTagsToMap(tags []redshifttypes.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

func (p *Provider) listOpenSearchDomains(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listOpenSearchDomainsFrom(ctx, regionalClient(p, region, opensearch.NewFromConfig), region)
}

func (p *Provider) listOpenSearchDomainsFrom(ctx context.Context, client openSearchAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "OpenSearch") {
		return nil, nil
	}
	listed, err := client.ListDomainNames(ctx, &opensearch.ListDomainNamesInput{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_opensearch_domains", "", err)
	}
	names := make([]string, 0, len(listed.DomainNames))
	for _, d := range listed.DomainNames {
		names = append(names, aws.ToString(d.DomainName))
	}

	var resources []types.Resource
	for _, batch := range chunk(names, openSearchDescribeBatch) {
		output, err := client.DescribeDomains(ctx, &opensearch.DescribeDomainsInput{DomainNames: batch})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "describe_opensearch_domains", "", err)
		}
		for _, d := range output.DomainStatusList {
			name := aws.ToString(d.DomainName)
			arn := aws.ToString(d.ARN)
			resources = append(resources, types.Resource{
				ID:       name,
				Name:     name,
				ARN:      arn,
				Type:     "aws_opensearch_domain",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     p.bulkTags(ctx, region, arn),
			})
		}
	}
	log.Debug("AWS OpenSearch: Found %d domains in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listMSKClusters(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listMSKClustersFrom(ctx, regionalClient(p, region, kafka.NewFromConfig), region)
}

func (p *Provider) listMSKClustersFrom(ctx context.Context, client mskAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := kafka.NewListClustersV2Paginator(client, &kafka.ListClustersV2Input{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_msk_clusters", "", err)
		}
		for _, c := range output.ClusterInfoList {
			tags := c.Tags
			if tags == nil {
				tags = map[string]string{}
			}
			name := aws.ToString(c.ClusterName)
			resources = append(resources, types.Resource{
				ID:        name,
				Name:      name,
				ARN:       aws.ToString(c.ClusterArn),
				Type:      "aws_msk_cluster",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      tags,
				CreatedAt: c.CreationTime,
			})
		}
	}
	log.Debug("AWS MSK: Found %d clusters in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listGlueJobs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listGlueJobsFrom(ctx, regionalClient(p, region, glue.NewFromConfig), region)
}

func (p *Provider) listGlueJobsFrom(ctx context.Context, client glueAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "Glue") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := glue.NewGetJobsPaginator(client, &glue.GetJobsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_glue_jobs", "", err)
		}
		for _, j := range output.Jobs {
			name := aws.ToString(j.Name)
			arn := p.buildARN("glue", region, p.accountID, "job/"+name)
			resources = append(resources, types.Resource{
				ID:        name,
				Name:      name,
				ARN:       arn,
				Type:      "aws_glue_job",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      p.bulkTags(ctx, region, arn),
				CreatedAt: j.CreatedOn,
			})
		}
	}
	log.Debug("AWS Glue: Found %d jobs in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listFirehoseStreams(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listFirehoseStreamsFrom(ctx, regionalClient(p, region, firehose.NewFromConfig), region)
}

func (p *Provider) listFirehoseStreamsFrom(ctx context.Context, client firehoseAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "Firehose") {
		return nil, nil
	}
	var resources []types.Resource
	var startAfter *string
	for {
		output, err := client.ListDeliveryStreams(ctx, &firehose.ListDeliveryStreamsInput{ExclusiveStartDeliveryStreamName: startAfter})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_firehose_streams", "", err)
		}
		for _, name := range output.DeliveryStreamNames {
			arn := p.buildARN("firehose", region, p.accountID, "deliverystream/"+name)
			resources = append(resources, types.Resource{
				ID:       name,
				Name:     name,
				ARN:      arn,
				Type:     "aws_kinesis_firehose_delivery_stream",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     p.bulkTags(ctx, region, arn),
			})
		}
		if !aws.ToBool(output.HasMoreDeliveryStreams) || len(output.DeliveryStreamNames) == 0 {
			break
		}
		startAfter = aws.String(output.DeliveryStreamNames[len(output.DeliveryStreamNames)-1])
	}
	log.Debug("AWS Firehose: Found %d delivery streams in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listSageMakerResources(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSageMakerResourcesFrom(ctx, regionalClient(p, region, sagemaker.NewFromConfig), region)
}

func (p *Provider) listSageMakerResourcesFrom(ctx context.Context, client sageMakerAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(ctx, region, "SageMaker") {
		return nil, nil
	}
	var resources []types.Resource

	endpoints := sagemaker.NewListEndpointsPaginator(client, &sagemaker.ListEndpointsInput{})
	for endpoints.HasMorePages() {
		output, err := endpoints.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_sagemaker_endpoints", "", err)
		}
		for _, e := range output.Endpoints {
			name := aws.ToString(e.EndpointName)
			arn := aws.ToString(e.EndpointArn)
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: arn, Type: "aws_sagemaker_endpoint",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn), CreatedAt: e.CreationTime,
			})
		}
	}

	notebooks := sagemaker.NewListNotebookInstancesPaginator(client, &sagemaker.ListNotebookInstancesInput{})
	for notebooks.HasMorePages() {
		output, err := notebooks.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_sagemaker_notebooks", "", err)
		}
		for _, n := range output.NotebookInstances {
			name := aws.ToString(n.NotebookInstanceName)
			arn := aws.ToString(n.NotebookInstanceArn)
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: arn, Type: "aws_sagemaker_notebook_instance",
				Region: region, Account: p.accountID, Provider: providerName,
				Tags: p.bulkTags(ctx, region, arn), CreatedAt: n.CreationTime,
			})
		}
	}
	log.Debug("AWS SageMaker: Found %d endpoints and notebooks in %s", len(resources), region)
	return resources, nil
}
