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
	"github.com/aws/aws-sdk-go-v2/service/ec2"
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

	// tagSources holds the per-region bulk tag fetch started by ListResources.
	tagSources map[string]*tagSource

	// skipped records what the current discovery left out for unreadable tags.
	skipped skipLog

	// clients caches the SDK clients created through regionalClient, keyed by
	// client type and region.
	clients map[string]any
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
		cfg:       cfg,
		account:   account,
		accountID: aws.ToString(identity.Account),
		partition: partition,
		regions:   regions,
		clients:   make(map[string]any),
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

	p.logBulkTagCoverage(ctx, resources)
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

// tagApplier writes tags to a resource addressed by ARN through its service
// API; the region comes from the ARN.
type tagApplier func(p *Provider, ctx context.Context, arn string, tags map[string]string) error

// regionTagApplier writes tags to a resource whose identifier does not carry
// its region; region is the one the plan recorded.
type regionTagApplier func(p *Provider, ctx context.Context, resourceID, region string, tags map[string]string) error

// tagAppliers maps the tagging route returned by getResourceType to the
// service-specific applier of the ARN-addressed resources.
var tagAppliers = map[string]tagApplier{
	"classic_load_balancer": (*Provider).applyClassicELBTags,
	"target_group":          (*Provider).applyELBv2Tags,
	"load_balancer":         (*Provider).applyELBv2Tags,
	"tagging_api":           (*Provider).applyTagsViaTaggingAPI,
	"dynamodb_table":        (*Provider).applyDynamoDBTags,
	"ecs_cluster":           (*Provider).applyECSTags,
	"ecs_service":           (*Provider).applyECSTags,
	"eks_cluster":           (*Provider).applyEKSTags,
	"elasticache_cluster":   (*Provider).applyElastiCacheTags,
	"efs_file_system":       (*Provider).applyEFSTags,
	"ecr_repository":        (*Provider).applyECRTags,
	"kms_key":               (*Provider).applyKMSTags,
	"kinesis_stream":        (*Provider).applyKinesisTags,
	"cloudwatch_log_group":  (*Provider).applyLogGroupTags,
	"rds_instance":          (*Provider).applyRDSTags,
	"lambda_function":       (*Provider).applyLambdaTags,
	"sns_topic":             (*Provider).applySNSTags,
	"sqs_queue":             (*Provider).applySQSTags,
	"autoscaling_group":     (*Provider).applyAutoScalingTags,
	routeLightsail:          (*Provider).applyLightsailTags,
	"global_accelerator":    (*Provider).applyGlobalAcceleratorTags,
}

// regionTagAppliers maps the routes that need the plan's region to their
// applier: S3 buckets and every EC2 route of ec2IDPrefixes.
var regionTagAppliers = func() map[string]regionTagApplier {
	appliers := map[string]regionTagApplier{"s3_bucket": (*Provider).applyS3Tags}
	for _, entry := range ec2IDPrefixes {
		appliers[entry.route] = (*Provider).applyEC2Tags
	}
	return appliers
}()

// ApplyTags applies tags to an AWS resource addressed by ARN.
func (p *Provider) ApplyTags(ctx context.Context, resourceID string, tags map[string]string) error {
	return p.ApplyTagsInRegion(ctx, resourceID, "", tags)
}

// ApplyTagsInRegion applies tags to an AWS resource. region is where the
// resource lives; EC2 resources, addressed by bare ID, cannot be tagged
// without it and S3 buckets need a GetBucketLocation call.
func (p *Provider) ApplyTagsInRegion(ctx context.Context, resourceID, region string, tags map[string]string) error {
	route := p.getResourceType(resourceID)
	if apply, ok := tagAppliers[route]; ok {
		return apply(p, ctx, resourceID, tags)
	}
	if apply, ok := regionTagAppliers[route]; ok {
		return apply(p, ctx, resourceID, region, tags)
	}
	return provider.NewProviderError(providerName, "apply_tags", resourceID,
		errors.New("unknown resource type: expected an ARN or an EC2 resource ID"))
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

// logBulkTagCoverage reports, per region, how many discovered resources
// carried tags when bulk tags were used. Zero matches with tagged resources
// present would point at an ARN mismatch.
func (p *Provider) logBulkTagCoverage(ctx context.Context, resources []types.Resource) {
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
		if !src.available(ctx) {
			log.Debug("AWS Tagging: bulk tags unavailable in %s, per-resource calls used", region)
			continue
		}
		log.Debug("AWS Tagging: %s: %d of %d discovered resources have tags (bulk source: %d tagged ARNs)",
			region, tagged[region], total[region], len(src.tags))
	}
}
