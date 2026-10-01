package aws

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/unicrons/tagctl/internal/types"
)

const (
	partitionChina = "aws-cn"
	partitionGov   = "aws-us-gov"
	regionBeijing  = "cn-north-1"
	regionNingxia  = "cn-northwest-1"
	regionGovWest  = "us-gov-west-1"
)

func partitionProvider(partition string, regions ...string) *Provider {
	return &Provider{accountID: "123456789012", partition: partition, regions: regions}
}

func TestPartitionOf(t *testing.T) {
	tests := []struct {
		arn     string
		want    string
		wantErr bool
	}{
		{"arn:aws:iam::123456789012:user/alice", "aws", false},
		{"arn:aws:sts::123456789012:assumed-role/TagctlScan/tagctl", "aws", false},
		{"arn:aws-cn:sts::123456789012:assumed-role/TagctlScan/tagctl", partitionChina, false},
		{"arn:aws-us-gov:iam::123456789012:user/alice", partitionGov, false},
		{"arn::iam::123456789012:user/alice", "", true},
		{"123456789012", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.arn, func(t *testing.T) {
			got, err := partitionOf(tt.arn)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("partitionOf(%q) = %q, %v; want %q, error %v", tt.arn, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestGlobalRegion_FollowsThePartition(t *testing.T) {
	tests := []struct {
		name string
		p    *Provider
		want string
	}{
		{"no partition recorded is commercial", &Provider{}, "us-east-1"},
		{"commercial", partitionProvider("aws", "eu-west-1"), "us-east-1"},
		{"China", partitionProvider(partitionChina, regionBeijing), regionNingxia},
		{"GovCloud", partitionProvider(partitionGov, "us-gov-east-1"), regionGovWest},
		{"unknown partition stays inside it", partitionProvider("aws-new", "new-region-1"), "new-region-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.globalRegion(); got != tt.want {
				t.Errorf("globalRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTagSweepRegions_AddsTheGlobalRegionOfThePartition(t *testing.T) {
	tests := []struct {
		name string
		p    *Provider
		want []string
	}{
		{"commercial without us-east-1", partitionProvider("aws", "eu-west-1"), []string{"eu-west-1", "us-east-1"}},
		{"commercial with us-east-1", partitionProvider("aws", "us-east-1"), []string{"us-east-1"}},
		{"China never sweeps us-east-1", partitionProvider(partitionChina, regionBeijing), []string{regionBeijing, regionNingxia}},
		{"GovCloud with its global region configured", partitionProvider(partitionGov, regionGovWest), []string{regionGovWest}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configured := slices.Clone(tt.p.regions)
			if got := tt.p.tagSweepRegions(); !slices.Equal(got, tt.want) {
				t.Errorf("tagSweepRegions() = %v, want %v", got, tt.want)
			}
			if !slices.Equal(tt.p.regions, configured) {
				t.Errorf("configured regions changed to %v", tt.p.regions)
			}
		})
	}
}

func TestGetResourceType_RoutesARNsOfEveryPartition(t *testing.T) {
	p := &Provider{}
	commercial := []struct{ arn, want string }{
		{"arn:aws:rds:us-east-1:123456789012:db:mydb", "rds_instance"},
		{"arn:aws:lambda:us-east-1:123456789012:function:fn", "lambda_function"},
		{"arn:aws:sns:us-east-1:123456789012:alerts", "sns_topic"},
		{"arn:aws:sqs:us-east-1:123456789012:jobs", "sqs_queue"},
		{"arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web", "autoscaling_group"},
		{"arn:aws:dynamodb:us-east-1:123456789012:table/orders", "dynamodb_table"},
		{"arn:aws:ecs:us-east-1:123456789012:cluster/web", "ecs_cluster"},
		{"arn:aws:ecs:us-east-1:123456789012:service/web/api", "ecs_service"},
		{"arn:aws:eks:us-east-1:123456789012:cluster/prod", "eks_cluster"},
		{"arn:aws:elasticache:us-east-1:123456789012:cluster:sessions", "elasticache_cluster"},
		{"arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-0abc", "efs_file_system"},
		{"arn:aws:ecr:us-east-1:123456789012:repository/api", "ecr_repository"},
		{"arn:aws:kms:us-east-1:123456789012:key/1234abcd", "kms_key"},
		{"arn:aws:kinesis:us-east-1:123456789012:stream/events", "kinesis_stream"},
		{"arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/fn", "cloudwatch_log_group"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc", "load_balancer"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/net/api/abc", "load_balancer"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/gwy/fw/abc", "load_balancer"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/classic-web", "classic_load_balancer"},
		{"arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web/abc", "target_group"},
		{"arn:aws:apigateway:us-east-1::/restapis/abc", "tagging_api"},
		{"arn:aws:iam::123456789012:role/admin", "tagging_api"},
		{"arn:aws:route53:::hostedzone/Z1", "tagging_api"},
		{"arn:aws:ec2:us-east-1:123456789012:transit-gateway/tgw-0abc", "tagging_api"},
	}
	regions := map[string]string{partitionChina: regionBeijing, partitionGov: regionGovWest}
	for partition, region := range regions {
		for _, tt := range commercial {
			id := strings.Replace(strings.Replace(tt.arn, "arn:aws:", "arn:"+partition+":", 1), "us-east-1", region, 1)
			if got := p.getResourceType(id); got != tt.want {
				t.Errorf("getResourceType(%q) = %q, want %q", id, got, tt.want)
			}
		}
	}
}

func TestGetResourceType_RoutesByARNSegmentsNotSubstrings(t *testing.T) {
	p := &Provider{}
	tests := []struct{ name, id, want string }{
		{"service word inside another service's resource", "arn:aws-cn:states:cn-north-1:123456789012:stateMachine:service/ecs", "tagging_api"},
		{"ECS cluster named like a service path", "arn:aws-us-gov:ecs:us-gov-west-1:123456789012:cluster/service", "ecs_cluster"},
		{"target group word in a classic balancer name", "arn:aws-cn:elasticloadbalancing:cn-north-1:123456789012:loadbalancer/targetgroup", "classic_load_balancer"},
		{"ARN with too few segments", "arn:aws-cn:rds:cn-north-1", ""},
		{"ARN without a partition", "arn::rds:cn-north-1:123456789012:db:mydb", ""},
		{"ARN without a service", "arn:aws-us-gov::us-gov-west-1:123456789012:db:mydb", ""},
		{"ARN without a resource", "arn:aws-cn:rds:cn-north-1:123456789012:", ""},
		{"bare arn prefix", "arn:", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.getResourceType(tt.id); got != tt.want {
				t.Errorf("getResourceType(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestApplyTags_RejectsAMalformedARN(t *testing.T) {
	err := testProvider().ApplyTags(context.Background(), "arn:aws-cn:rds:cn-north-1", map[string]string{"owner": "x"})
	if err == nil || !strings.Contains(err.Error(), "arn:aws-cn:rds:cn-north-1") {
		t.Errorf("err = %v, want an error naming the malformed ARN", err)
	}
}

func TestListers_BuildARNsInTheAccountPartition(t *testing.T) {
	ctx := context.Background()

	t.Run("DynamoDB tables in China", func(t *testing.T) {
		p := partitionProvider(partitionChina, regionBeijing)
		resources, err := p.listDynamoDBTablesFrom(ctx, &mockDynamoDBClient{pages: [][]string{{"orders"}}}, regionBeijing)
		wantARNs(t, resources, err, "arn:aws-cn:dynamodb:cn-north-1:123456789012:table/orders")
	})

	t.Run("security groups in GovCloud", func(t *testing.T) {
		p := partitionProvider(partitionGov, regionGovWest)
		mock := &mockSecurityGroupsClient{pages: [][]ec2types.SecurityGroup{{{GroupId: aws.String("sg-111"), GroupName: aws.String("web")}}}}
		resources, err := p.listSecurityGroupsFrom(ctx, mock, regionGovWest)
		wantARNs(t, resources, err, "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:security-group/sg-111")
	})

	t.Run("API Gateway REST APIs in China", func(t *testing.T) {
		p := partitionProvider(partitionChina, regionBeijing)
		mock := &mockRestAPIsClient{items: []apigwtypes.RestApi{{Id: aws.String("abc123"), Name: aws.String("orders")}}}
		resources, err := p.listRestAPIsFrom(ctx, mock, regionBeijing)
		wantARNs(t, resources, err, "arn:aws-cn:apigateway:cn-north-1::/restapis/abc123")
	})

	t.Run("S3 buckets in GovCloud read the bulk tags of their ARN", func(t *testing.T) {
		p := partitionProvider(partitionGov, regionGovWest)
		arn := "arn:aws-us-gov:s3:::logs"
		p.tagSources = map[string]*tagSource{regionGovWest: staticTagSource(map[string]map[string]string{arn: {"owner": "x"}})}
		mock := &mockS3Client{
			buckets:         []s3types.Bucket{{Name: aws.String("logs")}},
			bucketLocations: map[string]s3types.BucketLocationConstraint{"logs": regionGovWest},
		}
		resources, err := p.listS3BucketsFrom(ctx, mock, func(string) s3API { return mock })
		wantARNs(t, resources, err, arn)
		if len(resources) == 1 && resources[0].Tags["owner"] != "x" {
			t.Errorf("tags = %v, want the bulk tags of %s", resources[0].Tags, arn)
		}
		if len(mock.taggingCalls) != 0 {
			t.Errorf("GetBucketTagging called for %v, want the bulk source to answer", mock.taggingCalls)
		}
	})

	t.Run("Route 53 zones in China read the sweep of the partition's global region", func(t *testing.T) {
		p := partitionProvider(partitionChina, regionBeijing)
		arn := "arn:aws-cn:route53:::hostedzone/Z123"
		p.tagSources = map[string]*tagSource{regionNingxia: staticTagSource(map[string]map[string]string{arn: {"owner": "x"}})}
		mock := &mockRoute53Client{zones: []r53types.HostedZone{{Id: aws.String("/hostedzone/Z123"), Name: aws.String("example.cn.")}}}
		resources, err := p.listHostedZonesFrom(ctx, mock)
		wantARNs(t, resources, err, arn)
		if len(resources) == 1 && resources[0].Tags["owner"] != "x" {
			t.Errorf("tags = %v, want the tags swept in cn-northwest-1", resources[0].Tags)
		}
	})

	t.Run("Route 53 is skipped when only a us-east-1 sweep exists in China", func(t *testing.T) {
		p := partitionProvider(partitionChina, regionBeijing)
		p.tagSources = map[string]*tagSource{"us-east-1": staticTagSource(nil)}
		mock := &mockRoute53Client{zones: []r53types.HostedZone{{Id: aws.String("/hostedzone/Z123"), Name: aws.String("example.cn.")}}}
		resources, err := p.listHostedZonesFrom(ctx, mock)
		if err != nil || len(resources) != 0 {
			t.Errorf("resources = %+v, err = %v; want the service skipped", resources, err)
		}
	})
}

func wantARNs(t *testing.T, resources []types.Resource, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := make([]string, 0, len(resources))
	for _, r := range resources {
		got = append(got, r.ARN)
	}
	if !slices.Equal(got, want) {
		t.Errorf("ARNs = %v, want %v", got, want)
	}
}

func TestApplyTagsViaTaggingAPI_GlobalARNsUseTheGlobalRegionOfThePartition(t *testing.T) {
	tests := []struct {
		name, partition, arn, want string
	}{
		{"commercial IAM role", "aws", "arn:aws:iam::123456789012:role/admin", "us-east-1"},
		{"China IAM role", partitionChina, "arn:aws-cn:iam::123456789012:role/admin", regionNingxia},
		{"GovCloud Route 53 zone", partitionGov, "arn:aws-us-gov:route53:::hostedzone/Z1", regionGovWest},
		{"regional ARN keeps its region", partitionChina, "arn:aws-cn:states:cn-north-1:123456789012:stateMachine:flow", regionBeijing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := partitionProvider(tt.partition, regionBeijing)
			if got := p.taggingRegion(tt.arn); got != tt.want {
				t.Errorf("taggingRegion(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}

func TestListAccelerators_SkippedOutsideTheCommercialPartition(t *testing.T) {
	for _, partition := range []string{partitionChina, partitionGov} {
		resources, err := partitionProvider(partition).listAccelerators(context.Background())
		if err != nil || len(resources) != 0 {
			t.Errorf("%s: resources = %+v, err = %v; want none without an API call", partition, resources, err)
		}
	}
}

func TestEC2ARN_UsesTheAccountPartition(t *testing.T) {
	tests := []struct {
		p    *Provider
		want string
	}{
		{testProvider(), "arn:aws:ec2:eu-west-1:123456789012:volume/vol-0abc123"},
		{partitionProvider(partitionChina), "arn:aws-cn:ec2:eu-west-1:123456789012:volume/vol-0abc123"},
		{partitionProvider(partitionGov), "arn:aws-us-gov:ec2:eu-west-1:123456789012:volume/vol-0abc123"},
	}
	for _, tt := range tests {
		if got := tt.p.ec2ARN("eu-west-1", "volume", "vol-0abc123"); got != tt.want {
			t.Errorf("ec2ARN() = %q, want %q", got, tt.want)
		}
	}
}
