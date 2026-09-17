module github.com/unicrons/tagctl

go 1.26.6

require (
	github.com/aws/aws-sdk-go-v2 v1.47.0
	github.com/aws/aws-sdk-go-v2/config v1.33.4
	github.com/aws/aws-sdk-go-v2/credentials v1.20.4
	github.com/aws/aws-sdk-go-v2/service/accessanalyzer v1.56.0
	github.com/aws/aws-sdk-go-v2/service/acm v1.50.0
	github.com/aws/aws-sdk-go-v2/service/acmpca v1.56.0
	github.com/aws/aws-sdk-go-v2/service/amplify v1.48.0
	github.com/aws/aws-sdk-go-v2/service/apigateway v1.47.0
	github.com/aws/aws-sdk-go-v2/service/apigatewayv2 v1.42.0
	github.com/aws/aws-sdk-go-v2/service/appstream v1.70.0
	github.com/aws/aws-sdk-go-v2/service/appsync v1.61.0
	github.com/aws/aws-sdk-go-v2/service/athena v1.66.0
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.78.0
	github.com/aws/aws-sdk-go-v2/service/backup v1.65.0
	github.com/aws/aws-sdk-go-v2/service/batch v1.75.0
	github.com/aws/aws-sdk-go-v2/service/bedrock v1.72.0
	github.com/aws/aws-sdk-go-v2/service/cloudformation v1.81.0
	github.com/aws/aws-sdk-go-v2/service/cloudfront v1.73.0
	github.com/aws/aws-sdk-go-v2/service/cloudtrail v1.65.0
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.72.0
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.87.0
	github.com/aws/aws-sdk-go-v2/service/codeartifact v1.46.0
	github.com/aws/aws-sdk-go-v2/service/codebuild v1.78.0
	github.com/aws/aws-sdk-go-v2/service/codecommit v1.43.0
	github.com/aws/aws-sdk-go-v2/service/codepipeline v1.55.0
	github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider v1.74.0
	github.com/aws/aws-sdk-go-v2/service/configservice v1.74.0
	github.com/aws/aws-sdk-go-v2/service/costexplorer v1.72.0
	github.com/aws/aws-sdk-go-v2/service/databasemigrationservice v1.71.0
	github.com/aws/aws-sdk-go-v2/service/datapipeline v1.38.0
	github.com/aws/aws-sdk-go-v2/service/datasync v1.67.0
	github.com/aws/aws-sdk-go-v2/service/directconnect v1.50.0
	github.com/aws/aws-sdk-go-v2/service/directoryservice v1.47.0
	github.com/aws/aws-sdk-go-v2/service/dlm v1.45.0
	github.com/aws/aws-sdk-go-v2/service/drs v1.50.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.68.0
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.332.0
	github.com/aws/aws-sdk-go-v2/service/ecr v1.65.0
	github.com/aws/aws-sdk-go-v2/service/ecs v1.97.0
	github.com/aws/aws-sdk-go-v2/service/efs v1.49.0
	github.com/aws/aws-sdk-go-v2/service/eks v1.99.0
	github.com/aws/aws-sdk-go-v2/service/elasticache v1.61.0
	github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk v1.42.0
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing v1.41.0
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.63.0
	github.com/aws/aws-sdk-go-v2/service/emr v1.70.0
	github.com/aws/aws-sdk-go-v2/service/eventbridge v1.54.0
	github.com/aws/aws-sdk-go-v2/service/firehose v1.51.0
	github.com/aws/aws-sdk-go-v2/service/fms v1.53.0
	github.com/aws/aws-sdk-go-v2/service/fsx v1.74.0
	github.com/aws/aws-sdk-go-v2/service/glacier v1.41.0
	github.com/aws/aws-sdk-go-v2/service/globalaccelerator v1.44.0
	github.com/aws/aws-sdk-go-v2/service/glue v1.158.0
	github.com/aws/aws-sdk-go-v2/service/guardduty v1.92.0
	github.com/aws/aws-sdk-go-v2/service/iam v1.64.0
	github.com/aws/aws-sdk-go-v2/service/kafka v1.64.0
	github.com/aws/aws-sdk-go-v2/service/kinesis v1.54.0
	github.com/aws/aws-sdk-go-v2/service/kms v1.60.0
	github.com/aws/aws-sdk-go-v2/service/lambda v1.108.0
	github.com/aws/aws-sdk-go-v2/service/lightsail v1.65.0
	github.com/aws/aws-sdk-go-v2/service/memorydb v1.42.0
	github.com/aws/aws-sdk-go-v2/service/mq v1.44.0
	github.com/aws/aws-sdk-go-v2/service/networkfirewall v1.72.0
	github.com/aws/aws-sdk-go-v2/service/opensearch v1.80.0
	github.com/aws/aws-sdk-go-v2/service/rds v1.129.0
	github.com/aws/aws-sdk-go-v2/service/redshift v1.71.0
	github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi v1.41.0
	github.com/aws/aws-sdk-go-v2/service/rolesanywhere v1.31.0
	github.com/aws/aws-sdk-go-v2/service/route53 v1.70.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.113.0
	github.com/aws/aws-sdk-go-v2/service/sagemaker v1.277.0
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.49.0
	github.com/aws/aws-sdk-go-v2/service/servicecatalog v1.47.0
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.73.0
	github.com/aws/aws-sdk-go-v2/service/sfn v1.50.0
	github.com/aws/aws-sdk-go-v2/service/shield v1.43.0
	github.com/aws/aws-sdk-go-v2/service/sns v1.47.0
	github.com/aws/aws-sdk-go-v2/service/sqs v1.52.0
	github.com/aws/aws-sdk-go-v2/service/ssm v1.78.0
	github.com/aws/aws-sdk-go-v2/service/ssmincidents v1.47.0
	github.com/aws/aws-sdk-go-v2/service/storagegateway v1.52.0
	github.com/aws/aws-sdk-go-v2/service/sts v1.50.0
	github.com/aws/aws-sdk-go-v2/service/transfer v1.82.0
	github.com/aws/aws-sdk-go-v2/service/waf v1.38.0
	github.com/aws/aws-sdk-go-v2/service/wafregional v1.38.0
	github.com/aws/aws-sdk-go-v2/service/wafv2 v1.83.0
	github.com/aws/aws-sdk-go-v2/service/wellarchitected v1.49.0
	github.com/aws/aws-sdk-go-v2/service/workspaces v1.80.0
	github.com/aws/smithy-go v1.28.1
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/spf13/viper v1.21.0
	golang.org/x/term v0.46.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.31.4
	k8s.io/apimachinery v0.37.0
	k8s.io/client-go v0.31.4
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.0 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.13.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/go-openapi/jsonreference v1.0.0 // indirect
	github.com/go-openapi/swag v0.27.1 // indirect
	github.com/go-openapi/swag/cmdutils v0.27.1 // indirect
	github.com/go-openapi/swag/conv v0.27.1 // indirect
	github.com/go-openapi/swag/fileutils v0.27.1 // indirect
	github.com/go-openapi/swag/jsonutils v0.27.1 // indirect
	github.com/go-openapi/swag/loading v0.27.1 // indirect
	github.com/go-openapi/swag/mangling v0.27.1 // indirect
	github.com/go-openapi/swag/netutils v0.27.1 // indirect
	github.com/go-openapi/swag/pools v0.27.1 // indirect
	github.com/go-openapi/swag/stringutils v0.27.1 // indirect
	github.com/go-openapi/swag/typeutils v0.27.1 // indirect
	github.com/go-openapi/swag/yamlutils v0.27.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/imdario/mergo v0.3.6 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/oauth2 v0.27.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.2 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
