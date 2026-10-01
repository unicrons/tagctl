// Package aws provides AWS cloud resource discovery and tagging.
package aws

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/backup"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kafka"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/logging"

	cfgpkg "github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// maxConcurrentAPICalls bounds each per-resource fan-out (forEachConcurrently,
// S3 bucket tags).
const maxConcurrentAPICalls = 16

// maxConcurrentListers bounds the listers running at once across all regions.
// It is a separate pool from maxConcurrentAPICalls, so a lister never waits
// for a slot its own fan-out holds.
const maxConcurrentListers = 32

// maxRetryAttempts lets 16 in-flight calls on one client all retry to the limit
// inside its 500-token retry quota: 16 calls x 6 retries x 5 tokens = 480.
const maxRetryAttempts = 7

// dialTimeout bounds the TCP connect to an AWS endpoint. The SDK default is
// 30s, which turns one unreachable regional endpoint into a 90s stall after
// retries. Credential resolution keeps the SDK default.
const dialTimeout = 5 * time.Second

// defaultRegion is used for the initial API calls when neither the SDK nor
// the config names a region, before the partition is known.
const defaultRegion = "us-east-1"

// providerName is the Provider field of every AWS resource.
const providerName = "aws"

// regionGlobal marks resources that are not bound to a region.
const regionGlobal = "global"

// Provider implements the provider.Provider interface for AWS.
type Provider struct {
	cfg       aws.Config
	account   cfgpkg.AWSAccount
	accountID string
	// partition comes from the caller identity; empty means partitionAWS.
	partition string
	regions   []string
	mu        sync.Mutex

	// Service clients per region
	ec2Clients         map[string]*ec2.Client
	s3Client           *s3.Client
	s3RegionalClients  map[string]*s3.Client // Cache for regional S3 clients
	rdsClients         map[string]*rds.Client
	lambdaClients      map[string]*lambda.Client
	snsClients         map[string]*sns.Client
	sqsClients         map[string]*sqs.Client
	elbv2Clients       map[string]*elbv2.Client
	autoscalingClients map[string]*autoscaling.Client
	dynamodbClients    map[string]*dynamodb.Client
	ecsClients         map[string]*ecs.Client
	eksClients         map[string]*eks.Client
	elasticacheClients map[string]*elasticache.Client
	efsClients         map[string]*efs.Client
	ecrClients         map[string]*ecr.Client
	kmsClients         map[string]*kms.Client
	kinesisClients     map[string]*kinesis.Client
	logsClients        map[string]*cloudwatchlogs.Client
	taggingClients     map[string]*resourcegroupstaggingapi.Client
	classicELBClients  map[string]*elb.Client
	apigwClients       map[string]*apigateway.Client
	apigwv2Clients     map[string]*apigatewayv2.Client
	sfnClients         map[string]*sfn.Client
	secretsClients     map[string]*secretsmanager.Client
	cfnClients         map[string]*cloudformation.Client
	cloudwatchClients  map[string]*cloudwatch.Client
	eventsClients      map[string]*eventbridge.Client
	firehoseClients    map[string]*firehose.Client
	redshiftClients    map[string]*redshift.Client
	opensearchClients  map[string]*opensearch.Client
	mskClients         map[string]*kafka.Client
	sagemakerClients   map[string]*sagemaker.Client
	glueClients        map[string]*glue.Client
	acmClients         map[string]*acm.Client
	cognitoClients     map[string]*cognitoidentityprovider.Client
	codebuildClients   map[string]*codebuild.Client
	backupClients      map[string]*backup.Client
	fsxClients         map[string]*fsx.Client
	beanstalkClients   map[string]*elasticbeanstalk.Client
	route53Client      *route53.Client
	cloudfrontClient   *cloudfront.Client
	iamClient          *iam.Client

	// tagSources holds the per-region bulk tag fetch started by ListResources.
	tagSources map[string]*tagSource

	// skipped records what the current discovery left out for unreadable tags.
	skipped skipLog

	// clients caches the SDK clients created through regionalClient, keyed by
	// client type and region.
	clients map[string]any

	// costClient is global: Cost Explorer is reached through the partition's
	// global region.
	costClient *costexplorer.Client
}

// New creates a new AWS provider with the given account configuration.
//
// Credentials and the default region are resolved by the SDK chain
// (environment, shared config, SSO, container or instance role); the account
// entry only selects a profile and, optionally, a role to assume.
func New(ctx context.Context, account cfgpkg.AWSAccount) (*Provider, error) {
	log.Debug("AWS: Creating provider for profile=%q role=%q regions=%v", account.Profile, account.RoleARN, account.Regions)

	var opts []func(*config.LoadOptions) error
	if account.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(account.Profile))
	}
	if log.GetLevel() >= log.LevelDebug {
		opts = append(opts,
			config.WithClientLogMode(aws.LogRetries),
			config.WithLogger(logging.LoggerFunc(func(_ logging.Classification, format string, v ...interface{}) {
				log.Debug("AWS SDK: "+format, v...)
			})),
		)
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		log.Error("AWS: Failed to load config: %v", err)
		return nil, provider.NewProviderError(providerName, "load_config", "", err)
	}
	cfg = withRetryDefaults(cfg)
	cfg.Region = initialRegion(cfg.Region, account.Regions)
	log.Debug("AWS: Config loaded, default region=%s", cfg.Region)

	if account.RoleARN != "" {
		cfg = assumeRole(cfg, account)
	}
	// Get account ID
	log.Debug("AWS: Calling STS GetCallerIdentity...")
	stsClient := sts.NewFromConfig(cfg)
	stsStart := time.Now()
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		log.Error("AWS: Failed to get caller identity: %v", err)
		return nil, provider.NewProviderError(providerName, "get_identity", "", err)
	}
	log.Info("AWS: Authenticated as account %s (ARN: %s) in %s",
		aws.ToString(identity.Account), aws.ToString(identity.Arn), time.Since(stsStart).Round(time.Millisecond))

	partition, err := partitionOf(aws.ToString(identity.Arn))
	if err != nil {
		return nil, provider.NewProviderError(providerName, "get_identity", "", err)
	}

	cfg.HTTPClient = scanHTTPClient(cfg.HTTPClient)

	regions := account.Regions
	if len(regions) == 0 {
		// Discover all available regions
		log.Info("AWS: No regions specified, discovering all available regions...")
		ec2Client := ec2.NewFromConfig(cfg)
		regionsStart := time.Now()
		regionsOutput, err := ec2Client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
			AllRegions: aws.Bool(false), // Only enabled regions
		})
		if err != nil {
			log.Error("AWS: Failed to discover regions: %v", err)
			return nil, provider.NewProviderError(providerName, "describe_regions", "", err)
		}
		for _, r := range regionsOutput.Regions {
			regions = append(regions, aws.ToString(r.RegionName))
		}
		log.Info("AWS: Discovered %d available regions in %s", len(regions), time.Since(regionsStart).Round(time.Millisecond))
	}

	p := &Provider{
		cfg:                cfg,
		account:            account,
		accountID:          aws.ToString(identity.Account),
		partition:          partition,
		regions:            regions,
		clients:            make(map[string]any),
		ec2Clients:         make(map[string]*ec2.Client),
		s3RegionalClients:  make(map[string]*s3.Client),
		rdsClients:         make(map[string]*rds.Client),
		lambdaClients:      make(map[string]*lambda.Client),
		snsClients:         make(map[string]*sns.Client),
		sqsClients:         make(map[string]*sqs.Client),
		elbv2Clients:       make(map[string]*elbv2.Client),
		autoscalingClients: make(map[string]*autoscaling.Client),
		dynamodbClients:    make(map[string]*dynamodb.Client),
		ecsClients:         make(map[string]*ecs.Client),
		eksClients:         make(map[string]*eks.Client),
		elasticacheClients: make(map[string]*elasticache.Client),
		efsClients:         make(map[string]*efs.Client),
		ecrClients:         make(map[string]*ecr.Client),
		kmsClients:         make(map[string]*kms.Client),
		kinesisClients:     make(map[string]*kinesis.Client),
		logsClients:        make(map[string]*cloudwatchlogs.Client),
		taggingClients:     make(map[string]*resourcegroupstaggingapi.Client),
		classicELBClients:  make(map[string]*elb.Client),
		apigwClients:       make(map[string]*apigateway.Client),
		apigwv2Clients:     make(map[string]*apigatewayv2.Client),
		sfnClients:         make(map[string]*sfn.Client),
		secretsClients:     make(map[string]*secretsmanager.Client),
		cfnClients:         make(map[string]*cloudformation.Client),
		cloudwatchClients:  make(map[string]*cloudwatch.Client),
		eventsClients:      make(map[string]*eventbridge.Client),
		firehoseClients:    make(map[string]*firehose.Client),
		redshiftClients:    make(map[string]*redshift.Client),
		opensearchClients:  make(map[string]*opensearch.Client),
		mskClients:         make(map[string]*kafka.Client),
		sagemakerClients:   make(map[string]*sagemaker.Client),
		glueClients:        make(map[string]*glue.Client),
		acmClients:         make(map[string]*acm.Client),
		cognitoClients:     make(map[string]*cognitoidentityprovider.Client),
		codebuildClients:   make(map[string]*codebuild.Client),
		backupClients:      make(map[string]*backup.Client),
		fsxClients:         make(map[string]*fsx.Client),
		beanstalkClients:   make(map[string]*elasticbeanstalk.Client),
	}

	// Route 53, CloudFront and IAM are called through the partition's global region.
	globalCfg := cfg.Copy()
	globalCfg.Region = p.globalRegion()
	p.s3Client = s3.NewFromConfig(cfg)
	p.route53Client = route53.NewFromConfig(globalCfg)
	p.cloudfrontClient = cloudfront.NewFromConfig(globalCfg)
	p.iamClient = iam.NewFromConfig(globalCfg)

	// Initialize regional clients
	for _, region := range regions {
		log.Debug("AWS: Initializing clients for region %s", region)
		regionalCfg := cfg.Copy()
		regionalCfg.Region = region

		p.ec2Clients[region] = ec2.NewFromConfig(regionalCfg)
		p.rdsClients[region] = rds.NewFromConfig(regionalCfg)
		p.lambdaClients[region] = lambda.NewFromConfig(regionalCfg)
		p.snsClients[region] = sns.NewFromConfig(regionalCfg)
		p.sqsClients[region] = sqs.NewFromConfig(regionalCfg)
		p.elbv2Clients[region] = elbv2.NewFromConfig(regionalCfg)
		p.autoscalingClients[region] = autoscaling.NewFromConfig(regionalCfg)
	}

	log.Info("AWS: Provider initialized for %d region(s): %v", len(regions), regions)
	return p, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return providerName
}

// AccountID returns the AWS account ID.
func (p *Provider) AccountID() string {
	return p.accountID
}

// withRetryDefaults makes throttled calls back off in adaptive mode unless
// AWS_RETRY_MODE, AWS_MAX_ATTEMPTS or the profile already chose otherwise.
func withRetryDefaults(cfg aws.Config) aws.Config {
	if cfg.RetryMode == "" {
		cfg.RetryMode = aws.RetryModeAdaptive
	}
	if cfg.RetryMaxAttempts == 0 {
		cfg.RetryMaxAttempts = maxRetryAttempts
	}
	return cfg
}

// scanHTTPClient returns the client for the service calls of a scan: base with
// the short dial timeout. The credential providers built before it keep base,
// so a slow SSO or STS endpoint is not cut at dialTimeout.
func scanHTTPClient(base aws.HTTPClient) aws.HTTPClient {
	buildable, ok := base.(*awshttp.BuildableClient)
	if !ok {
		return base
	}
	return buildable.WithDialerOptions(func(d *net.Dialer) {
		d.Timeout = dialTimeout
	})
}

// initialRegion picks the region for the STS and region-discovery calls: the
// one the SDK resolved (environment or profile), else the first configured
// region, else us-east-1.
func initialRegion(resolved string, configured []string) string {
	switch {
	case resolved != "":
		return resolved
	case len(configured) > 0:
		return configured[0]
	default:
		return defaultRegion
	}
}

// Profile returns the AWS profile name.
func (p *Provider) Profile() string {
	return p.account.Profile
}

// regionalLister names a discovery function that runs once per region.
type regionalLister struct {
	label string
	list  func(ctx context.Context, region string) ([]types.Resource, error)
}

// regionalListers returns the discovery functions run for every configured region.
// S3 is absent on purpose: buckets are global and discovered once.
func (p *Provider) regionalListers() []regionalLister {
	return []regionalLister{
		{"EC2 instances", p.listEC2Instances},
		{"EBS volumes", p.listEBSVolumes},
		{"security groups", p.listSecurityGroups},
		{"VPCs", p.listVPCs},
		{"subnets", p.listSubnets},
		{"RDS instances", p.listRDSInstances},
		{"Lambda functions", p.listLambdaFunctions},
		{"SNS topics", p.listSNSTopics},
		{"SQS queues", p.listSQSQueues},
		{"load balancers", p.listLoadBalancers},
		{"Auto Scaling groups", p.listAutoScalingGroups},
		{"EBS snapshots", p.listEBSSnapshots},
		{"DynamoDB tables", p.listDynamoDBTables},
		{"ECS clusters and services", p.listECSResources},
		{"EKS clusters", p.listEKSClusters},
		{"ElastiCache clusters", p.listElastiCacheClusters},
		{"EFS file systems", p.listEFSFileSystems},
		{"ECR repositories", p.listECRRepositories},
		{"KMS keys", p.listKMSKeys},
		{"Kinesis streams", p.listKinesisStreams},
		{"CloudWatch log groups", p.listLogGroups},
		{"AMIs", p.listAMIs},
		{"Elastic IPs", p.listElasticIPs},
		{"NAT gateways", p.listNATGateways},
		{"internet gateways", p.listInternetGateways},
		{"VPC endpoints", p.listVPCEndpoints},
		{"launch templates", p.listLaunchTemplates},
		{"RDS clusters", p.listRDSClusters},
		{"RDS snapshots", p.listRDSSnapshots},
		{"classic load balancers", p.listClassicLoadBalancers},
		{"target groups", p.listTargetGroups},
		{"API Gateway REST APIs", p.listRestAPIs},
		{"API Gateway HTTP APIs", p.listHTTPAPIs},
		{"Step Functions state machines", p.listStateMachines},
		{"Secrets Manager secrets", p.listSecrets},
		{"CloudFormation stacks", p.listStacks},
		{"CloudWatch alarms", p.listAlarms},
		{"EventBridge rules", p.listEventRules},
		{"Firehose delivery streams", p.listFirehoseStreams},
		{"Redshift clusters", p.listRedshiftClusters},
		{"OpenSearch domains", p.listOpenSearchDomains},
		{"MSK clusters", p.listMSKClusters},
		{"SageMaker endpoints and notebooks", p.listSageMakerResources},
		{"Glue jobs", p.listGlueJobs},
		{"ACM certificates", p.listCertificates},
		{"Cognito user pools", p.listUserPools},
		{"CodeBuild projects", p.listCodeBuildProjects},
		{"Backup vaults", p.listBackupVaults},
		{"FSx file systems", p.listFSxFileSystems},
		{"Elastic Beanstalk environments", p.listBeanstalkEnvironments},
		{"Access Analyzer analyzers", p.listAnalyzers},
		{"ACM PCA certificate authorities", p.listCertificateAuthorities},
		{"CloudTrail trails", p.listTrails},
		{"Config rules", p.listConfigRules},
		{"Directory Service directories", p.listDirectories},
		{"Firewall Manager policies", p.listFMSPolicies},
		{"GuardDuty detectors", p.listDetectors},
		{"Network Firewall firewalls", p.listFirewalls},
		{"Roles Anywhere trust anchors", p.listTrustAnchors},
		{"WAF Classic regional web ACLs", p.listRegionalWebACLs},
		{"WAFv2 regional web ACLs", p.listWAFv2RegionalWebACLs},
		{"Amplify apps", p.listAmplifyApps},
		{"AppSync GraphQL APIs", p.listGraphQLAPIs},
		{"Bedrock guardrails", p.listGuardrails},
		{"CodeArtifact domains and repositories", p.listCodeArtifactResources},
		{"CodeCommit repositories", p.listCodeCommitRepositories},
		{"CodePipeline pipelines", p.listPipelines},
		{"Service Catalog portfolios", p.listPortfolios},
		{"Well-Architected workloads", p.listWorkloads},
		{"Athena workgroups", p.listWorkGroups},
		{"DMS replication instances", p.listReplicationInstances},
		{"Data Pipeline pipelines", p.listDataPipelines},
		{"DataSync tasks", p.listDataSyncTasks},
		{"EMR clusters", p.listEMRClusters},
		{"Glacier vaults", p.listVaults},
		{"MemoryDB clusters", p.listMemoryDBClusters},
		{"MQ brokers", p.listBrokers},
		{"SES identities and configuration sets", p.listSESResources},
		{"Storage Gateway gateways", p.listGateways},
		{"Transfer Family servers", p.listTransferServers},
		{"AppStream fleets and stacks", p.listAppStreamResources},
		{"Batch compute environments and job queues", p.listBatchResources},
		{"Direct Connect connections", p.listDirectConnectConnections},
		{"DLM lifecycle policies", p.listLifecyclePolicies},
		{"DRS source servers", p.listSourceServers},
		{"Lightsail instances", p.listLightsailInstances},
		{"SSM parameters and documents", p.listSSMResources},
		{"Incident Manager response plans", p.listResponsePlans},
		{"WorkSpaces", p.listWorkSpaces},
	}
}

// ListResources discovers all taggable resources across configured regions.
func (p *Provider) ListResources(ctx context.Context) ([]types.Resource, error) {
	log.Info("AWS: Starting resource discovery for account %s", p.accountID)
	log.Debug("AWS: Scanning regions: %v", p.regions)
	discoveryStart := time.Now()

	// Bulk tags are fetched while discovery runs; listers block on them only
	// when they resolve a resource's tags.
	p.startTagSources(ctx)

	globals := append(p.globalListers(), globalLister{"S3 buckets", p.listS3Buckets})
	resources, err := p.discover(ctx, globals, p.regionalListers())

	p.logBulkTagCoverage(resources)
	log.Info("AWS: Discovery complete - found %d total resources in %s", len(resources), time.Since(discoveryStart).Round(time.Millisecond))
	return resources, err
}

// discover runs every global lister once and every regional lister per
// region. It returns what was found together with every lister error and a
// summary of the resources and services skipped for unreadable tags. Once ctx
// is cancelled no queued lister starts.
func (p *Provider) discover(ctx context.Context, globals []globalLister, listers []regionalLister) ([]types.Resource, error) {
	p.skipped.reset()

	var allResources []types.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(p.regions)*len(listers)+len(globals))
	slots := make(chan struct{}, maxConcurrentListers)
	notStarted := 0

	run := func(region, label string, list func(context.Context) ([]types.Resource, error)) {
		if !acquireSlot(ctx, slots) {
			notStarted++
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			log.Debug("AWS: Listing %s in %s...", label, region)
			start := time.Now()
			resources, err := list(ctx)
			if err != nil {
				errChan <- p.listerError(region, label, err)
				return
			}
			log.Debug("AWS: %s in %s: %d found in %s", label, region, len(resources), time.Since(start).Round(time.Millisecond))
			mu.Lock()
			allResources = append(allResources, resources...)
			mu.Unlock()
		}()
	}

	for _, g := range globals {
		run(regionGlobal, g.label, g.list)
	}
	for _, region := range p.regions {
		for _, lister := range listers {
			run(region, lister.label, func(ctx context.Context) ([]types.Resource, error) {
				return lister.list(ctx, region)
			})
		}
	}

	wg.Wait()
	close(errChan)

	// errChan is closed after every writer finished, so len is the exact count.
	errs := make([]error, 0, len(errChan)+3)
	for err := range errChan {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		log.Error("AWS: %d lister error(s) occurred during discovery", len(errs))
	}
	// Cancelled tag reads return nothing and record nothing: without this the
	// interrupted discovery would look complete.
	if err := ctx.Err(); err != nil {
		interrupted := "discovery interrupted"
		if notStarted > 0 {
			interrupted += fmt.Sprintf(", %d lister(s) not started", notStarted)
		}
		errs = append(errs, fmt.Errorf("account %s: %s: %w", p.accountID, interrupted, err))
	}
	for _, err := range p.skipped.errs() {
		errs = append(errs, fmt.Errorf("account %s: %w", p.accountID, err))
	}
	return allResources, errors.Join(errs...)
}

// listerError logs a discovery error and names the account, region and lister
// it came from, so errors from several accounts stay distinguishable.
func (p *Provider) listerError(region, label string, err error) error {
	err = fmt.Errorf("account %s, region %s: list %s: %w", p.accountID, region, label, err)
	log.Error("AWS: %v", err)
	return err
}

// acquireSlot waits for a free lister slot. It reports false, holding none,
// once ctx is cancelled, even when a slot and the cancel arrive together.
func acquireSlot(ctx context.Context, slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		if ctx.Err() != nil {
			<-slots
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}

// routeLightsail is the tagging route of Lightsail resources.
const routeLightsail = "lightsail"

// tagApplier writes tags to one resource through its service API.
type tagApplier func(ctx context.Context, resourceID string, tags map[string]string) error

// tagAppliers maps the tagging route returned by getResourceType to the
// service-specific applier. region is the resource's region as the plan
// recorded it, for the routes whose identifier does not carry one.
func (p *Provider) tagAppliers(region string) map[string]tagApplier {
	appliers := map[string]tagApplier{
		"classic_load_balancer": p.applyClassicELBTags,
		"target_group":          p.applyELBv2Tags,
		"load_balancer":         p.applyELBv2Tags,
		"tagging_api":           p.applyTagsViaTaggingAPI,
		"dynamodb_table":        p.applyDynamoDBTags,
		"ecs_cluster":           p.applyECSTags,
		"ecs_service":           p.applyECSTags,
		"eks_cluster":           p.applyEKSTags,
		"elasticache_cluster":   p.applyElastiCacheTags,
		"efs_file_system":       p.applyEFSTags,
		"ecr_repository":        p.applyECRTags,
		"kms_key":               p.applyKMSTags,
		"kinesis_stream":        p.applyKinesisTags,
		"cloudwatch_log_group":  p.applyLogGroupTags,
		"s3_bucket": func(ctx context.Context, resourceID string, tags map[string]string) error {
			return p.applyS3Tags(ctx, resourceID, region, tags)
		},
		"rds_instance":       p.applyRDSTags,
		"lambda_function":    p.applyLambdaTags,
		"sns_topic":          p.applySNSTags,
		"sqs_queue":          p.applySQSTags,
		"autoscaling_group":  p.applyAutoScalingTags,
		routeLightsail:       p.applyLightsailTags,
		"global_accelerator": p.applyGlobalAcceleratorTags,
	}
	for _, t := range []string{"ec2_instance", "ebs_volume", "ebs_snapshot", "security_group", "vpc", "subnet",
		"ami", "elastic_ip", "nat_gateway", "internet_gateway", "vpc_endpoint", "launch_template"} {
		appliers[t] = func(ctx context.Context, resourceID string, tags map[string]string) error {
			return p.applyEC2Tags(ctx, resourceID, region, tags)
		}
	}
	return appliers
}

// ApplyTags applies tags to an AWS resource addressed by ARN.
func (p *Provider) ApplyTags(ctx context.Context, resourceID string, tags map[string]string) error {
	return p.ApplyTagsInRegion(ctx, resourceID, "", tags)
}

// ApplyTagsInRegion applies tags to an AWS resource. region is where the
// resource lives; EC2 resources, addressed by bare ID, cannot be tagged
// without it and S3 buckets need a GetBucketLocation call.
func (p *Provider) ApplyTagsInRegion(ctx context.Context, resourceID, region string, tags map[string]string) error {
	apply, ok := p.tagAppliers(region)[p.getResourceType(resourceID)]
	if !ok {
		return provider.NewProviderError(providerName, "apply_tags", resourceID,
			errors.New("unknown resource type: expected an ARN or an EC2 resource ID"))
	}
	return apply(ctx, resourceID, tags)
}

// ec2IDPrefixes maps the ID prefix of the EC2 resources tagged by bare ID to
// their tagging route.
var ec2IDPrefixes = []struct{ prefix, route string }{
	{"i-", "ec2_instance"},
	{"vol-", "ebs_volume"},
	{"snap-", "ebs_snapshot"},
	{"ami-", "ami"},
	{"eipalloc-", "elastic_ip"},
	{"nat-", "nat_gateway"},
	{"igw-", "internet_gateway"},
	{"vpce-", "vpc_endpoint"},
	{"lt-", "launch_template"},
	{"sg-", "security_group"},
	{"vpc-", "vpc"},
	{"subnet-", "subnet"},
}

// arnServiceRoutes maps the service segment of an ARN to the tagging route of
// the services tagged through their own API. ECS and Elastic Load Balancing
// also depend on the resource segment and are routed in getResourceType.
var arnServiceRoutes = map[string]string{
	"rds":               "rds_instance",
	"lambda":            "lambda_function",
	"sns":               "sns_topic",
	"sqs":               "sqs_queue",
	"autoscaling":       "autoscaling_group",
	"dynamodb":          "dynamodb_table",
	"eks":               "eks_cluster",
	"elasticache":       "elasticache_cluster",
	"elasticfilesystem": "efs_file_system",
	"ecr":               "ecr_repository",
	"kms":               "kms_key",
	"kinesis":           "kinesis_stream",
	"logs":              "cloudwatch_log_group",
	"lightsail":         routeLightsail,
	"globalaccelerator": "global_accelerator",
}

// getResourceType returns the tagging route of a resource identifier, or ""
// when it is neither a well-formed ARN nor an EC2 resource ID. ARNs of any
// partition are routed by their service and resource segments, falling back
// to the Resource Groups Tagging API; EC2 resources by their ID prefix.
func (p *Provider) getResourceType(resourceID string) string {
	if !strings.HasPrefix(resourceID, "arn:") {
		for _, entry := range ec2IDPrefixes {
			if strings.HasPrefix(resourceID, entry.prefix) {
				return entry.route
			}
		}
		return ""
	}
	parsed, err := arn.Parse(resourceID)
	if err != nil || parsed.Partition == "" || parsed.Service == "" || parsed.Resource == "" {
		return ""
	}
	switch parsed.Service {
	case "s3":
		if isBucketARN(parsed) {
			return "s3_bucket"
		}
	case "ecs":
		if strings.HasPrefix(parsed.Resource, "service/") {
			return "ecs_service"
		}
		return "ecs_cluster"
	case "elasticloadbalancing":
		switch {
		case strings.HasPrefix(parsed.Resource, "targetgroup/"):
			return "target_group"
		case strings.HasPrefix(parsed.Resource, "loadbalancer/app/"),
			strings.HasPrefix(parsed.Resource, "loadbalancer/net/"),
			strings.HasPrefix(parsed.Resource, "loadbalancer/gwy/"):
			return "load_balancer"
		default:
			return "classic_load_balancer"
		}
	}
	if route, ok := arnServiceRoutes[parsed.Service]; ok {
		return route
	}
	return "tagging_api"
}

// getEC2Client returns the EC2 client for a region.
func (p *Provider) getEC2Client(region string) *ec2.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.ec2Clients[region]; ok {
		return client
	}

	// Create client for unknown region
	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := ec2.NewFromConfig(regionalCfg)
	p.ec2Clients[region] = client
	return client
}

// getRDSClient returns the RDS client for a region.
func (p *Provider) getRDSClient(region string) *rds.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.rdsClients[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := rds.NewFromConfig(regionalCfg)
	p.rdsClients[region] = client
	return client
}

// getLambdaClient returns the Lambda client for a region.
func (p *Provider) getLambdaClient(region string) *lambda.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.lambdaClients[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := lambda.NewFromConfig(regionalCfg)
	p.lambdaClients[region] = client
	return client
}

// getS3RegionalClient returns a cached S3 client for a specific region.
func (p *Provider) getS3RegionalClient(region string) *s3.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.s3RegionalClients[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := s3.NewFromConfig(regionalCfg)
	p.s3RegionalClients[region] = client
	return client
}

// getSNSClient returns the SNS client for a region.
func (p *Provider) getSNSClient(region string) *sns.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.snsClients[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := sns.NewFromConfig(regionalCfg)
	p.snsClients[region] = client
	return client
}

// getSQSClient returns the SQS client for a region.
func (p *Provider) getSQSClient(region string) *sqs.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.sqsClients[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := sqs.NewFromConfig(regionalCfg)
	p.sqsClients[region] = client
	return client
}

// getELBv2Client returns the ELBv2 client for a region.
func (p *Provider) getELBv2Client(region string) *elbv2.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.elbv2Clients[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := elbv2.NewFromConfig(regionalCfg)
	p.elbv2Clients[region] = client
	return client
}

// getAutoScalingClient returns the Auto Scaling client for a region.
func (p *Provider) getAutoScalingClient(region string) *autoscaling.Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.autoscalingClients[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := autoscaling.NewFromConfig(regionalCfg)
	p.autoscalingClients[region] = client
	return client
}

func (p *Provider) getDynamoDBClient(region string) *dynamodb.Client {
	return cachedClient(p, p.dynamodbClients, region, dynamodb.NewFromConfig)
}

func (p *Provider) getECSClient(region string) *ecs.Client {
	return cachedClient(p, p.ecsClients, region, ecs.NewFromConfig)
}

func (p *Provider) getEKSClient(region string) *eks.Client {
	return cachedClient(p, p.eksClients, region, eks.NewFromConfig)
}

func (p *Provider) getElastiCacheClient(region string) *elasticache.Client {
	return cachedClient(p, p.elasticacheClients, region, elasticache.NewFromConfig)
}

func (p *Provider) getEFSClient(region string) *efs.Client {
	return cachedClient(p, p.efsClients, region, efs.NewFromConfig)
}

func (p *Provider) getECRClient(region string) *ecr.Client {
	return cachedClient(p, p.ecrClients, region, ecr.NewFromConfig)
}

func (p *Provider) getKMSClient(region string) *kms.Client {
	return cachedClient(p, p.kmsClients, region, kms.NewFromConfig)
}

func (p *Provider) getKinesisClient(region string) *kinesis.Client {
	return cachedClient(p, p.kinesisClients, region, kinesis.NewFromConfig)
}

func (p *Provider) getLogsClient(region string) *cloudwatchlogs.Client {
	return cachedClient(p, p.logsClients, region, cloudwatchlogs.NewFromConfig)
}

// logBulkTagCoverage reports, per region, how many discovered resources
// carried tags when bulk tags were used. Zero matches with tagged resources
// present would point at an ARN mismatch.
func (p *Provider) logBulkTagCoverage(resources []types.Resource) {
	if log.GetLevel() < log.LevelDebug {
		return
	}
	tagged := make(map[string]int)
	total := make(map[string]int)
	for _, r := range resources {
		total[r.Region]++
		if len(r.Tags) > 0 {
			tagged[r.Region]++
		}
	}
	for _, region := range p.regions {
		src := p.tagsFor(region)
		if !src.available() {
			log.Debug("AWS Tagging: bulk tags unavailable in %s, per-resource calls used", region)
			continue
		}
		log.Debug("AWS Tagging: %s: %d of %d discovered resources have tags (bulk source: %d tagged ARNs)",
			region, tagged[region], total[region], len(src.tags))
	}
}

func (p *Provider) getClassicELBClient(region string) *elb.Client {
	return cachedClient(p, p.classicELBClients, region, elb.NewFromConfig)
}

func (p *Provider) getAPIGatewayClient(region string) *apigateway.Client {
	return cachedClient(p, p.apigwClients, region, apigateway.NewFromConfig)
}

func (p *Provider) getAPIGatewayV2Client(region string) *apigatewayv2.Client {
	return cachedClient(p, p.apigwv2Clients, region, apigatewayv2.NewFromConfig)
}

func (p *Provider) getStepFunctionsClient(region string) *sfn.Client {
	return cachedClient(p, p.sfnClients, region, sfn.NewFromConfig)
}

func (p *Provider) getSecretsManagerClient(region string) *secretsmanager.Client {
	return cachedClient(p, p.secretsClients, region, secretsmanager.NewFromConfig)
}

func (p *Provider) getCloudFormationClient(region string) *cloudformation.Client {
	return cachedClient(p, p.cfnClients, region, cloudformation.NewFromConfig)
}

func (p *Provider) getCloudWatchClient(region string) *cloudwatch.Client {
	return cachedClient(p, p.cloudwatchClients, region, cloudwatch.NewFromConfig)
}

func (p *Provider) getEventBridgeClient(region string) *eventbridge.Client {
	return cachedClient(p, p.eventsClients, region, eventbridge.NewFromConfig)
}

func (p *Provider) getFirehoseClient(region string) *firehose.Client {
	return cachedClient(p, p.firehoseClients, region, firehose.NewFromConfig)
}

func (p *Provider) getRedshiftClient(region string) *redshift.Client {
	return cachedClient(p, p.redshiftClients, region, redshift.NewFromConfig)
}

func (p *Provider) getOpenSearchClient(region string) *opensearch.Client {
	return cachedClient(p, p.opensearchClients, region, opensearch.NewFromConfig)
}

func (p *Provider) getMSKClient(region string) *kafka.Client {
	return cachedClient(p, p.mskClients, region, kafka.NewFromConfig)
}

func (p *Provider) getSageMakerClient(region string) *sagemaker.Client {
	return cachedClient(p, p.sagemakerClients, region, sagemaker.NewFromConfig)
}

func (p *Provider) getGlueClient(region string) *glue.Client {
	return cachedClient(p, p.glueClients, region, glue.NewFromConfig)
}

func (p *Provider) getACMClient(region string) *acm.Client {
	return cachedClient(p, p.acmClients, region, acm.NewFromConfig)
}

func (p *Provider) getCognitoClient(region string) *cognitoidentityprovider.Client {
	return cachedClient(p, p.cognitoClients, region, cognitoidentityprovider.NewFromConfig)
}

func (p *Provider) getCodeBuildClient(region string) *codebuild.Client {
	return cachedClient(p, p.codebuildClients, region, codebuild.NewFromConfig)
}

func (p *Provider) getBackupClient(region string) *backup.Client {
	return cachedClient(p, p.backupClients, region, backup.NewFromConfig)
}

func (p *Provider) getFSxClient(region string) *fsx.Client {
	return cachedClient(p, p.fsxClients, region, fsx.NewFromConfig)
}

func (p *Provider) getBeanstalkClient(region string) *elasticbeanstalk.Client {
	return cachedClient(p, p.beanstalkClients, region, elasticbeanstalk.NewFromConfig)
}

func (p *Provider) getRoute53Client() *route53.Client       { return p.route53Client }
func (p *Provider) getCloudFrontClient() *cloudfront.Client { return p.cloudfrontClient }
func (p *Provider) getIAMClient() *iam.Client               { return p.iamClient }
