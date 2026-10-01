package aws

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/unicrons/tagctl/internal/types"
)

func TestDiscover_KeepsResourcesAndReportsEachErrorWithItsAccountAndRegion(t *testing.T) {
	p := testProvider()
	iamErr := errors.New("iam: access denied")
	rdsErr := errors.New("rds: access denied")
	found := func(id string) []types.Resource { return []types.Resource{{ID: id}} }

	globals := []globalLister{
		{"Route 53", func(context.Context) ([]types.Resource, error) { return found("zone"), nil }},
		{"IAM", func(context.Context) ([]types.Resource, error) { return nil, iamErr }},
	}
	regional := []regionalLister{
		{"RDS instances", func(context.Context, string) ([]types.Resource, error) { return nil, rdsErr }},
		{"SQS queues", func(ctx context.Context, region string) ([]types.Resource, error) {
			p.skipResource(ctx, "SQS", region, "queue denied", errors.New("access denied"))
			return found("jobs"), nil
		}},
	}

	resources, err := p.discover(context.Background(), globals, regional)

	if len(resources) != 2 {
		t.Errorf("got %d resources, want the 2 that were listed", len(resources))
	}
	if !errors.Is(err, iamErr) || !errors.Is(err, rdsErr) {
		t.Errorf("err = %v, want every lister error", err)
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("err = %v, want joined errors", err)
	}
	var got []string
	for _, e := range joined.Unwrap() {
		got = append(got, e.Error())
	}
	slices.Sort(got)
	want := []string{
		"account 123456789012, region global: list IAM: iam: access denied",
		"account 123456789012, region us-east-1: list RDS instances: rds: access denied",
		"account 123456789012: skipped 1 resource(s) whose tags cannot be read: SQS 1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("errors = %q\nwant %q", got, want)
	}
}

func TestDiscover_CancelledScanReturnsTheContextError(t *testing.T) {
	p := testProvider()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	regional := []regionalLister{
		{"SQS queues", func(ctx context.Context, region string) ([]types.Resource, error) {
			cancel()
			p.skipResource(ctx, "SQS", region, "queue jobs", ctx.Err())
			return nil, nil
		}},
	}

	_, err := p.discover(ctx, nil, regional)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context error", err)
	}
	if want := "account 123456789012: discovery interrupted: context canceled"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestDiscover_ForgetsSkipsOfAPreviousRun(t *testing.T) {
	p := testProvider()
	p.skipResource(context.Background(), "SQS", defaultRegion, "queue denied", errors.New("access denied"))

	if _, err := p.discover(context.Background(), nil, nil); err != nil {
		t.Errorf("err = %v, want nil for a run that skipped nothing", err)
	}
}

func TestDiscover_BoundsListersAcrossRegionsAndFinishesTheirFanOuts(t *testing.T) {
	p := testProvider()
	p.regions = make([]string, 17)
	for i := range p.regions {
		p.regions[i] = fmt.Sprintf("region-%d", i)
	}
	items := make([]int, 2*maxConcurrentAPICalls)

	var inFlight, peak atomic.Int64
	var fillOnce sync.Once
	filled := make(chan struct{})
	list := func() ([]types.Resource, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		raiseTo(&peak, n)
		if n == maxConcurrentListers {
			fillOnce.Do(func() { close(filled) })
		}
		select {
		case <-filled:
		case <-time.After(5 * time.Second):
			return nil, errors.New("lister slots never filled")
		}
		time.Sleep(time.Millisecond)
		return forEachConcurrently(context.Background(), items, func(int) []types.Resource { return one(types.Resource{}) }), nil
	}

	var globals []globalLister
	for i := range 3 {
		globals = append(globals, globalLister{fmt.Sprintf("global %d", i), func(context.Context) ([]types.Resource, error) { return list() }})
	}
	var regional []regionalLister
	for i := range 10 {
		regional = append(regional, regionalLister{fmt.Sprintf("regional %d", i), func(context.Context, string) ([]types.Resource, error) { return list() }})
	}

	resources, err := p.discover(context.Background(), globals, regional)

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := (len(globals) + len(regional)*len(p.regions)) * len(items); len(resources) != want {
		t.Errorf("got %d resources, want %d from every lister and fan-out item", len(resources), want)
	}
	if got := peak.Load(); got != maxConcurrentListers {
		t.Errorf("peak of %d listers in flight, want exactly %d", got, maxConcurrentListers)
	}
}

func TestDiscover_StartsNoQueuedListerOnceCancelled(t *testing.T) {
	p := testProvider()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var started atomic.Int64
	regional := make([]regionalLister, 2*maxConcurrentListers)
	for i := range regional {
		regional[i] = regionalLister{fmt.Sprintf("regional %d", i), func(listCtx context.Context, _ string) ([]types.Resource, error) {
			if started.Add(1) == maxConcurrentListers {
				cancel()
			}
			<-listCtx.Done()
			return nil, listCtx.Err()
		}}
	}

	_, err := p.discover(ctx, nil, regional)

	if got := started.Load(); got != maxConcurrentListers {
		t.Errorf("%d listers started, want only the %d that held a slot before the cancel", got, maxConcurrentListers)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("err = %v, want joined errors", err)
	}
	var accountErrs []string
	for _, e := range joined.Unwrap() {
		if msg := e.Error(); strings.HasPrefix(msg, "account 123456789012: ") {
			accountErrs = append(accountErrs, msg)
		}
	}
	want := []string{fmt.Sprintf("account 123456789012: discovery interrupted, %d lister(s) not started: context canceled", maxConcurrentListers)}
	if !slices.Equal(accountErrs, want) {
		t.Errorf("account-level errors = %q\nwant one context error %q", accountErrs, want)
	}
}

func TestForEachConcurrently_StopsDispatchingOnceCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := make([]int, 10*maxConcurrentAPICalls)

	var started atomic.Int64
	resources := forEachConcurrently(ctx, items, func(int) []types.Resource {
		if started.Add(1) == maxConcurrentAPICalls {
			cancel()
		}
		<-ctx.Done()
		return one(types.Resource{})
	})

	if got := started.Load(); got != maxConcurrentAPICalls {
		t.Errorf("%d items dispatched, want only the %d in flight at the cancel", got, maxConcurrentAPICalls)
	}
	if len(resources) != maxConcurrentAPICalls {
		t.Errorf("got %d resources, want the %d the in-flight calls returned", len(resources), maxConcurrentAPICalls)
	}
}

func TestForEachConcurrently_CancelledBeforeTheFirstItemRunsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resources := forEachConcurrently(ctx, []int{1, 2, 3}, func(int) []types.Resource {
		t.Error("item dispatched after the cancel")
		return nil
	})

	if len(resources) != 0 {
		t.Errorf("got %d resources, want none", len(resources))
	}
}

func TestForEachConcurrently_BoundsCallsInFlightAndReturnsEveryResult(t *testing.T) {
	items := make([]int, 5*maxConcurrentAPICalls)
	var inFlight, peak atomic.Int64

	resources := forEachConcurrently(context.Background(), items, func(int) []types.Resource {
		raiseTo(&peak, inFlight.Add(1))
		defer inFlight.Add(-1)
		time.Sleep(time.Millisecond)
		return one(types.Resource{})
	})

	if len(resources) != len(items) {
		t.Errorf("got %d resources, want %d", len(resources), len(items))
	}
	if got := peak.Load(); got > maxConcurrentAPICalls {
		t.Errorf("peak of %d calls in flight, want at most %d", got, maxConcurrentAPICalls)
	}
}

func raiseTo(peak *atomic.Int64, n int64) {
	for {
		old := peak.Load()
		if n <= old || peak.CompareAndSwap(old, n) {
			return
		}
	}
}

// loadSharedConfig loads the default profile from the given shared config
// file content, isolated from the environment and ~/.aws.
func loadSharedConfig(content string) func(*testing.T) aws.Config {
	return func(t *testing.T) aws.Config {
		t.Helper()
		dir := t.TempDir()
		configFile := filepath.Join(dir, "config")
		if err := os.WriteFile(configFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_RETRY_MODE", "AWS_MAX_ATTEMPTS"} {
			t.Setenv(key, "")
		}
		cfg, err := config.LoadDefaultConfig(context.Background(),
			config.WithSharedConfigFiles([]string{configFile}),
			config.WithSharedCredentialsFiles([]string{filepath.Join(dir, "credentials")}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
}

func TestWithRetryDefaults(t *testing.T) {
	literal := func(cfg aws.Config) func(*testing.T) aws.Config {
		return func(*testing.T) aws.Config { return cfg }
	}
	tests := []struct {
		name         string
		cfg          func(*testing.T) aws.Config
		wantMode     aws.RetryMode
		wantAttempts int
	}{
		{"nothing set retries adaptively", literal(aws.Config{}), aws.RetryModeAdaptive, maxRetryAttempts},
		{"a mode from the environment keeps the higher attempts", literal(aws.Config{RetryMode: aws.RetryModeStandard}), aws.RetryModeStandard, maxRetryAttempts},
		{"environment values win", literal(aws.Config{RetryMode: aws.RetryModeStandard, RetryMaxAttempts: 2}), aws.RetryModeStandard, 2},
		{"a loaded profile without retry keys gets the defaults", loadSharedConfig("[default]\nregion = us-east-1\n"), aws.RetryModeAdaptive, maxRetryAttempts},
		{"retry keys in a loaded profile win", loadSharedConfig("[default]\nretry_mode = standard\nmax_attempts = 2\n"), aws.RetryModeStandard, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := sts.NewFromConfig(withRetryDefaults(tt.cfg(t))).Options()

			if opts.RetryMode != tt.wantMode {
				t.Errorf("RetryMode = %q, want %q", opts.RetryMode, tt.wantMode)
			}
			if got := opts.Retryer.MaxAttempts(); got != tt.wantAttempts {
				t.Errorf("MaxAttempts = %d, want %d", got, tt.wantAttempts)
			}
		})
	}
}

func TestGetResourceType(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		resourceID   string
		expectedType string
	}{
		{"i-0abc123def456", "ec2_instance"},
		{"vol-0abc123def456", "ebs_volume"},
		{"arn:aws:rds:us-east-1:123456789012:db:mydb", "rds_instance"},
		{"arn:aws:lambda:us-east-1:123456789012:function:myfunction", "lambda_function"},
		{"arn:aws:s3:::my-bucket-name", "s3_bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.resourceID, func(t *testing.T) {
			result := p.getResourceType(tt.resourceID)
			if result != tt.expectedType {
				t.Errorf("getResourceType(%q) = %q, want %q", tt.resourceID, result, tt.expectedType)
			}
		})
	}
}

func TestRegionForARN(t *testing.T) {
	tests := []struct {
		arn     string
		want    string
		wantErr bool
	}{
		{"arn:aws:rds:us-east-1:123456789012:db:mydb", "us-east-1", false},
		{"arn:aws:lambda:eu-west-1:123456789012:function:myfunction", "eu-west-1", false},
		{"arn:aws-cn:rds:cn-north-1:123456789012:db:mydb", "cn-north-1", false},
		{"arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:fn", "us-gov-west-1", false},
		{"arn:aws:iam::123456789012:role/admin", "", true},
		{"arn:aws:s3:::my-bucket", "", true},
		{"arn:aws:rds:us-east-1", "", true},
		{"invalid-arn", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.arn, func(t *testing.T) {
			got, err := regionForARN(tt.arn)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("regionForARN(%q) = %q, %v; want %q, error %v", tt.arn, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestApplyTags_ServiceAppliersRejectAnARNWithoutARegion(t *testing.T) {
	ids := []string{
		"arn:aws:rds::123456789012:db:mydb",
		"arn:aws:lambda::123456789012:function:fn",
		"arn:aws:sns::123456789012:alerts",
		"arn:aws:sqs::123456789012:jobs",
		"arn:aws:autoscaling::123456789012:autoScalingGroup:uuid:autoScalingGroupName/web",
		"arn:aws:dynamodb::123456789012:table/orders",
		"arn:aws:ecs::123456789012:cluster/web",
		"arn:aws:ecs::123456789012:service/web/api",
		"arn:aws:eks::123456789012:cluster/prod",
		"arn:aws:elasticache::123456789012:cluster:sessions",
		"arn:aws:elasticfilesystem::123456789012:file-system/fs-0abc",
		"arn:aws:ecr::123456789012:repository/api",
		"arn:aws:kms::123456789012:key/1234abcd",
		"arn:aws:kinesis::123456789012:stream/events",
		"arn:aws:logs::123456789012:log-group:/aws/lambda/fn",
		"arn:aws:lightsail::123456789012:Instance/abc",
		"arn:aws:elasticloadbalancing::123456789012:loadbalancer/app/web/abc",
		"arn:aws:elasticloadbalancing::123456789012:loadbalancer/classic-web",
		"arn:aws:elasticloadbalancing::123456789012:targetgroup/web/abc",
	}
	// A zero Provider has no client caches: creating a client would panic.
	p := &Provider{}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			err := p.ApplyTags(context.Background(), id, map[string]string{"owner": "x"})
			if err == nil || !strings.Contains(err.Error(), id) {
				t.Errorf("ApplyTags() err = %v, want an error naming the ARN", err)
			}
		})
	}
}

func TestEC2TagsToMap(t *testing.T) {
	// Test with nil tags
	result := ec2TagsToMap(nil)
	if len(result) != 0 {
		t.Errorf("ec2TagsToMap(nil) returned %d tags, want 0", len(result))
	}
}

// TestGetResourceType_AllSupportedServices covers routing for every service
// tagctl can discover, so a new service cannot be added to discovery without
// also being routable in ApplyTags.
func TestGetResourceType_AllSupportedServices(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		resourceID   string
		expectedType string
	}{
		{"i-0abc123def456", "ec2_instance"},
		{"vol-0abc123def456", "ebs_volume"},
		{"sg-0abc123def456", "security_group"},
		{"vpc-0abc123def456", "vpc"},
		{"subnet-0abc123def456", "subnet"},
		{"arn:aws:rds:us-east-1:123456789012:db:mydb", "rds_instance"},
		{"arn:aws:lambda:us-east-1:123456789012:function:myfunction", "lambda_function"},
		{"arn:aws:sns:us-east-1:123456789012:alerts", "sns_topic"},
		{"arn:aws:sqs:us-east-1:123456789012:jobs", "sqs_queue"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc", "load_balancer"},
		{"arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web", "autoscaling_group"},
		{"snap-0abc123def456", "ebs_snapshot"},
		{"arn:aws:dynamodb:us-east-1:123456789012:table/orders", "dynamodb_table"},
		{"arn:aws:ecs:us-east-1:123456789012:cluster/web", "ecs_cluster"},
		{"arn:aws:ecs:us-east-1:123456789012:service/web/api", "ecs_service"},
		{"arn:aws:eks:us-east-1:123456789012:cluster/prod", "eks_cluster"},
		{"arn:aws:elasticache:us-east-1:123456789012:cluster:sessions", "elasticache_cluster"},
		{"arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-0abc", "efs_file_system"},
		{"arn:aws:ecr:us-east-1:123456789012:repository/api", "ecr_repository"},
		{"arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab", "kms_key"},
		{"arn:aws:kinesis:us-east-1:123456789012:stream/events", "kinesis_stream"},
		{"arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/fn", "cloudwatch_log_group"},
		{"ami-0abc", "ami"},
		{"eipalloc-0abc", "elastic_ip"},
		{"nat-0abc", "nat_gateway"},
		{"igw-0abc", "internet_gateway"},
		{"vpce-0abc", "vpc_endpoint"},
		{"lt-0abc", "launch_template"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/net/api/abc", "load_balancer"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/classic-web", "classic_load_balancer"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web/abc", "target_group"},
		{"arn:aws:rds:us-east-1:123456789012:cluster:aurora", "rds_instance"},
		{"arn:aws:apigateway:us-east-1::/restapis/abc", "tagging_api"},
		{"arn:aws:states:us-east-1:123456789012:stateMachine:flow", "tagging_api"},
		{"arn:aws:iam::123456789012:role/admin", "tagging_api"},
		{"arn:aws:route53:::hostedzone/Z1", "tagging_api"},
		{"arn:aws:rds:us-east-1:123456789012:cluster:docs", "rds_instance"},
		{"arn:aws:cloudtrail:us-east-1:123456789012:trail/main", "tagging_api"},
		{"arn:aws:wafv2:us-east-1:123456789012:regional/webacl/web/abc", "tagging_api"},
		{"arn:aws:lightsail:us-east-1:123456789012:Instance/abc", "lightsail"},
		{"arn:aws:globalaccelerator::123456789012:accelerator/abc", "global_accelerator"},
		{"arn:aws:s3:::my-bucket-name", "s3_bucket"},
	}

	appliers := p.tagAppliers("")
	for _, tt := range tests {
		t.Run(tt.expectedType, func(t *testing.T) {
			if result := p.getResourceType(tt.resourceID); result != tt.expectedType {
				t.Errorf("getResourceType(%q) = %q, want %q", tt.resourceID, result, tt.expectedType)
			}
			if appliers[tt.expectedType] == nil {
				t.Errorf("no tag applier registered for %q", tt.expectedType)
			}
		})
	}
}

// TestRegionalListers pins the set of services scanned in every region.
// S3 is deliberately absent: buckets are global and discovered once.
func TestRegionalListers(t *testing.T) {
	p := &Provider{}
	listers := p.regionalListers()

	want := []string{
		"EC2 instances",
		"EBS volumes",
		"security groups",
		"VPCs",
		"subnets",
		"RDS instances",
		"Lambda functions",
		"SNS topics",
		"SQS queues",
		"load balancers",
		"Auto Scaling groups",
		"EBS snapshots",
		"DynamoDB tables",
		"ECS clusters and services",
		"EKS clusters",
		"ElastiCache clusters",
		"EFS file systems",
		"ECR repositories",
		"KMS keys",
		"Kinesis streams",
		"CloudWatch log groups",
		"AMIs",
		"Elastic IPs",
		"NAT gateways",
		"internet gateways",
		"VPC endpoints",
		"launch templates",
		"RDS clusters",
		"RDS snapshots",
		"classic load balancers",
		"target groups",
		"API Gateway REST APIs",
		"API Gateway HTTP APIs",
		"Step Functions state machines",
		"Secrets Manager secrets",
		"CloudFormation stacks",
		"CloudWatch alarms",
		"EventBridge rules",
		"Firehose delivery streams",
		"Redshift clusters",
		"OpenSearch domains",
		"MSK clusters",
		"SageMaker endpoints and notebooks",
		"Glue jobs",
		"ACM certificates",
		"Cognito user pools",
		"CodeBuild projects",
		"Backup vaults",
		"FSx file systems",
		"Elastic Beanstalk environments",
		"Access Analyzer analyzers",
		"ACM PCA certificate authorities",
		"CloudTrail trails",
		"Config rules",
		"Directory Service directories",
		"Firewall Manager policies",
		"GuardDuty detectors",
		"Network Firewall firewalls",
		"Roles Anywhere trust anchors",
		"WAF Classic regional web ACLs",
		"WAFv2 regional web ACLs",
		"Amplify apps",
		"AppSync GraphQL APIs",
		"Bedrock guardrails",
		"CodeArtifact domains and repositories",
		"CodeCommit repositories",
		"CodePipeline pipelines",
		"Service Catalog portfolios",
		"Well-Architected workloads",
		"Athena workgroups",
		"DMS replication instances",
		"Data Pipeline pipelines",
		"DataSync tasks",
		"EMR clusters",
		"Glacier vaults",
		"MemoryDB clusters",
		"MQ brokers",
		"SES identities and configuration sets",
		"Storage Gateway gateways",
		"Transfer Family servers",
		"AppStream fleets and stacks",
		"Batch compute environments and job queues",
		"Direct Connect connections",
		"DLM lifecycle policies",
		"DRS source servers",
		"Lightsail instances",
		"SSM parameters and documents",
		"Incident Manager response plans",
		"WorkSpaces",
	}

	if len(listers) != len(want) {
		t.Fatalf("got %d regional listers, want %d", len(listers), len(want))
	}

	for i, label := range want {
		if listers[i].label != label {
			t.Errorf("lister %d = %q, want %q", i, listers[i].label, label)
		}
		if listers[i].list == nil {
			t.Errorf("lister %q has a nil list function", label)
		}
	}
}

// TestGlobalListers pins the account-wide services discovered once per scan.
func TestGlobalListers(t *testing.T) {
	p := &Provider{}
	want := []string{
		"Route 53 hosted zones", "CloudFront distributions", "IAM roles",
		"Shield protections", "WAF Classic global web ACLs", "WAFv2 CloudFront web ACLs", "Global Accelerator accelerators",
	}
	listers := p.globalListers()
	if len(listers) != len(want) {
		t.Fatalf("got %d global listers, want %d", len(listers), len(want))
	}
	for i, label := range want {
		if listers[i].label != label {
			t.Errorf("lister %d = %q, want %q", i, listers[i].label, label)
		}
	}
}
