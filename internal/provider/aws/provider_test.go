package aws

import (
	"context"
	"errors"
	"slices"
	"testing"

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
		{"my-bucket-name", "s3_bucket"},
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

func TestExtractRegionFromARN(t *testing.T) {
	tests := []struct {
		arn            string
		expectedRegion string
	}{
		{"arn:aws:rds:us-east-1:123456789012:db:mydb", "us-east-1"},
		{"arn:aws:lambda:eu-west-1:123456789012:function:myfunction", "eu-west-1"},
		{"arn:aws:ec2:ap-southeast-2:123456789012:instance/i-0abc123", "ap-southeast-2"},
		{"invalid-arn", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.arn, func(t *testing.T) {
			result := extractRegionFromARN(tt.arn)
			if result != tt.expectedRegion {
				t.Errorf("extractRegionFromARN(%q) = %q, want %q", tt.arn, result, tt.expectedRegion)
			}
		})
	}
}

func TestSplitARN(t *testing.T) {
	tests := []struct {
		arn           string
		expectedParts []string
	}{
		{
			"arn:aws:rds:us-east-1:123456789012:db:mydb",
			[]string{"arn", "aws", "rds", "us-east-1", "123456789012", "db", "mydb"},
		},
		{
			"arn:aws:s3:::mybucket",
			[]string{"arn", "aws", "s3", "", "", "mybucket"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.arn, func(t *testing.T) {
			result := splitARN(tt.arn)
			if len(result) != len(tt.expectedParts) {
				t.Errorf("splitARN(%q) returned %d parts, want %d", tt.arn, len(result), len(tt.expectedParts))
				return
			}
			for i, part := range result {
				if part != tt.expectedParts[i] {
					t.Errorf("splitARN(%q)[%d] = %q, want %q", tt.arn, i, part, tt.expectedParts[i])
				}
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

func TestBuildEC2ARN(t *testing.T) {
	tests := []struct {
		accountID    string
		region       string
		resourceType string
		resourceID   string
		expectedARN  string
	}{
		{
			"123456789012", "us-east-1", "instance", "i-0abc123",
			"arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123",
		},
		{
			"123456789012", "eu-west-1", "volume", "vol-0abc123",
			"arn:aws:ec2:eu-west-1:123456789012:volume/vol-0abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expectedARN, func(t *testing.T) {
			result := buildEC2ARN(tt.accountID, tt.region, tt.resourceType, tt.resourceID)
			if result != tt.expectedARN {
				t.Errorf("buildEC2ARN() = %q, want %q", result, tt.expectedARN)
			}
		})
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
		{"my-bucket-name", "s3_bucket"},
	}

	appliers := p.tagAppliers()
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
