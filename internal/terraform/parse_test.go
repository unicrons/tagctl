package terraform

import (
	"slices"
	"strings"
	"testing"
)

// A plan as terraform show -json emits it: nested modules, a data source, a
// resource type that cannot be tagged, and default_tags folded into tags_all.
const planJSON = `{
  "format_version": "1.2",
  "terraform_version": "1.9.5",
  "planned_values": {
    "root_module": {
      "resources": [
        {
          "address": "aws_instance.web",
          "mode": "managed",
          "type": "aws_instance",
          "name": "web",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {
            "instance_type": "t3.micro",
            "tags": {"Name": "web"},
            "tags_all": {"Name": "web", "environment": "prod", "owner": "team@example.com"}
          }
        },
        {
          "address": "aws_s3_bucket.logs",
          "mode": "managed",
          "type": "aws_s3_bucket",
          "name": "logs",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {"bucket": "my-logs", "tags": null}
        },
        {
          "address": "aws_iam_policy.readonly",
          "mode": "managed",
          "type": "aws_iam_policy",
          "name": "readonly",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {"policy": "{}"}
        },
        {
          "address": "data.aws_ami.ubuntu",
          "mode": "data",
          "type": "aws_ami",
          "name": "ubuntu",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {"tags": {"environment": "prod"}}
        }
      ],
      "child_modules": [
        {
          "address": "module.network",
          "resources": [
            {
              "address": "module.network.aws_subnet.private",
              "mode": "managed",
              "type": "aws_subnet",
              "name": "private",
              "provider_name": "registry.terraform.io/hashicorp/aws",
              "values": {"tags": {"environment": "prod"}}
            }
          ],
          "child_modules": [
            {
              "address": "module.network.module.nat",
              "resources": [
                {
                  "address": "module.network.module.nat.aws_eip.nat",
                  "mode": "managed",
                  "type": "aws_eip",
                  "name": "nat",
                  "provider_name": "registry.terraform.io/hashicorp/aws",
                  "values": {"tags": {}}
                }
              ]
            }
          ]
        }
      ]
    }
  },
  "resource_changes": [
    {
      "address": "aws_instance.web",
      "mode": "managed", "type": "aws_instance", "name": "web",
      "change": {"actions": ["create"]}
    },
    {
      "address": "aws_s3_bucket.logs",
      "mode": "managed", "type": "aws_s3_bucket", "name": "logs",
      "change": {"actions": ["no-op"]}
    },
    {
      "address": "module.network.aws_subnet.private",
      "mode": "managed", "type": "aws_subnet", "name": "private",
      "change": {"actions": ["update"]}
    },
    {
      "address": "module.network.module.nat.aws_eip.nat",
      "mode": "managed", "type": "aws_eip", "name": "nat",
      "change": {"actions": ["delete"]}
    }
  ]
}`

// A state file, which puts the module tree under values rather than
// planned_values and carries no resource_changes.
const stateJSON = `{
  "format_version": "1.0",
  "terraform_version": "1.9.5",
  "values": {
    "root_module": {
      "resources": [
        {
          "address": "aws_instance.api",
          "mode": "managed",
          "type": "aws_instance",
          "name": "api",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {"tags": {"environment": "staging"}}
        }
      ]
    }
  }
}`

// A plan where a tag value is only known after apply. An SDKv2 resource hides
// both tag maps; a Plugin Framework resource keeps the known keys of tags.
const unknownPlanJSON = `{
  "format_version": "1.2",
  "planned_values": {
    "root_module": {
      "resources": [
        {
          "address": "aws_sqs_queue.sdkv2",
          "mode": "managed",
          "type": "aws_sqs_queue",
          "name": "sdkv2",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {"name": "sdkv2"}
        },
        {
          "address": "aws_vpc_security_group_ingress_rule.framework",
          "mode": "managed",
          "type": "aws_vpc_security_group_ingress_rule",
          "name": "framework",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {"tags": {"environment": "prod"}}
        }
      ]
    }
  },
  "resource_changes": [
    {
      "address": "aws_sqs_queue.sdkv2",
      "change": {"actions": ["create"], "after_unknown": {"tags": true, "tags_all": true}}
    },
    {
      "address": "aws_vpc_security_group_ingress_rule.framework",
      "change": {"actions": ["create"], "after_unknown": {"tags": {"ref": true}, "tags_all": true}}
    },
    {
      "address": "aws_vpc_security_group_ingress_rule.framework",
      "deposed": "00000001",
      "change": {"actions": ["delete"], "after_unknown": {}}
    }
  ]
}`

// valueProd is the environment value the fixtures use.
const valueProd = "prod"

// parsePlan parses the plan fixture and indexes the result by address.
func parsePlan(t *testing.T, opts Options) map[string]map[string]string {
	t.Helper()

	result, err := Parse(strings.NewReader(planJSON), opts)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	byAddress := make(map[string]map[string]string, len(result.Resources))
	for _, resource := range result.Resources {
		byAddress[resource.ID] = resource.Tags
	}
	return byAddress
}

func TestParse_Plan(t *testing.T) {
	result, err := Parse(strings.NewReader(planJSON), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	resources := result.Resources

	// aws_instance, aws_s3_bucket, aws_subnet, aws_eip. The IAM policy has no
	// tag attribute and the data source is not managed, so both are skipped.
	if len(resources) != 4 {
		var addresses []string
		for _, r := range resources {
			addresses = append(addresses, r.ID)
		}
		t.Fatalf("got %d resources (%v), want 4", len(resources), addresses)
	}

	// Sorted by address, so the root module resources come first.
	if resources[0].ID != "aws_instance.web" {
		t.Errorf("first resource = %q, want aws_instance.web", resources[0].ID)
	}
	if resources[0].Type != "aws_instance" {
		t.Errorf("type = %q, want aws_instance", resources[0].Type)
	}
	if resources[0].Name != "web" {
		t.Errorf("name = %q, want web", resources[0].Name)
	}
	if resources[0].Provider != "aws" {
		t.Errorf("provider = %q, want aws", resources[0].Provider)
	}
}

// tags_all carries the provider's default_tags. Reading tags alone would
// report a violation on resources the provider tags for you.
func TestParse_PrefersTagsAll(t *testing.T) {
	byAddress := parsePlan(t, Options{})

	tags := byAddress["aws_instance.web"]
	if tags["owner"] != "team@example.com" {
		t.Errorf("owner = %q, want the default_tags value from tags_all", tags["owner"])
	}
	if tags["environment"] != valueProd {
		t.Errorf("environment = %q, want prod from tags_all", tags["environment"])
	}
	if len(tags) != 3 {
		t.Errorf("got %d tags, want 3 from tags_all: %v", len(tags), tags)
	}
}

// A resource type that supports tags but has none set must be reported as
// untagged, not skipped: that is exactly the violation to catch.
func TestParse_NullTagsMeansUntagged(t *testing.T) {
	byAddress := parsePlan(t, Options{})

	tags, present := byAddress["aws_s3_bucket.logs"]
	if !present {
		t.Fatal("a resource with null tags was skipped; it should be reported as untagged")
	}
	if len(tags) != 0 {
		t.Errorf("tags = %v, want empty", tags)
	}
}

// A type with no tag attribute at all is not a tagging violation.
func TestParse_SkipsUntaggableTypes(t *testing.T) {
	byAddress := parsePlan(t, Options{})

	if _, present := byAddress["aws_iam_policy.readonly"]; present {
		t.Error("aws_iam_policy has no tag attribute and should have been skipped")
	}
}

// Data sources are read-only lookups and cannot be tagged.
func TestParse_SkipsDataSources(t *testing.T) {
	byAddress := parsePlan(t, Options{})

	if _, present := byAddress["data.aws_ami.ubuntu"]; present {
		t.Error("a data source was returned; only managed resources can be tagged")
	}
}

func TestParse_WalksNestedModules(t *testing.T) {
	byAddress := parsePlan(t, Options{})

	if _, present := byAddress["module.network.aws_subnet.private"]; !present {
		t.Error("a resource in a child module was not collected")
	}
	if _, present := byAddress["module.network.module.nat.aws_eip.nat"]; !present {
		t.Error("a resource in a nested child module was not collected")
	}
}

// A pull request check usually only cares about what is changing.
func TestParse_ChangedOnly(t *testing.T) {
	byAddress := parsePlan(t, Options{ChangedOnly: true})

	if len(byAddress) != 2 {
		t.Fatalf("got %d resources (%v), want 2 (one create, one update)", len(byAddress), byAddress)
	}
	if _, present := byAddress["aws_instance.web"]; !present {
		t.Error("a created resource was not included")
	}
	if _, present := byAddress["module.network.aws_subnet.private"]; !present {
		t.Error("an updated resource was not included")
	}
	if _, present := byAddress["aws_s3_bucket.logs"]; present {
		t.Error("a no-op resource was included; it is already live")
	}
	if _, present := byAddress["module.network.module.nat.aws_eip.nat"]; present {
		t.Error("a resource being destroyed was included; it does not need tags")
	}
}

func TestParse_State(t *testing.T) {
	result, err := Parse(strings.NewReader(stateJSON), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	resources := result.Resources

	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if resources[0].ID != "aws_instance.api" {
		t.Errorf("address = %q, want aws_instance.api", resources[0].ID)
	}
	if resources[0].Tags["environment"] != "staging" {
		t.Errorf("environment = %q, want staging", resources[0].Tags["environment"])
	}
}

// A state file has no resource_changes, so --changed-only must not silently
// return nothing useful; it returns nothing, which the CLI reports.
func TestParse_ChangedOnlyOnStateReturnsNothing(t *testing.T) {
	result, err := Parse(strings.NewReader(stateJSON), Options{ChangedOnly: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result.Resources) != 0 {
		t.Errorf("got %d resources, want 0: a state file records no changes", len(result.Resources))
	}
}

func TestParse_UnknownTagsFromAfterUnknown(t *testing.T) {
	result, err := Parse(strings.NewReader(unknownPlanJSON), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if want := []string{"aws_sqs_queue.sdkv2"}; !slices.Equal(result.Unreadable, want) {
		t.Errorf("Unreadable = %v, want %v", result.Unreadable, want)
	}
	if len(result.Resources) != 1 || result.Resources[0].ID != "aws_vpc_security_group_ingress_rule.framework" {
		t.Fatalf("got %+v, want only the resource with known tags", result.Resources)
	}
	resource := result.Resources[0]
	if want := map[string]string{"environment": valueProd, "ref": ""}; !equalTags(resource.Tags, want) {
		t.Errorf("tags = %v, want %v", resource.Tags, want)
	}
	if want := []string{"ref"}; !slices.Equal(resource.UnknownTags, want) {
		t.Errorf("UnknownTags = %v, want %v", resource.UnknownTags, want)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "malformed JSON",
			input:   "{not json",
			wantErr: "failed to parse terraform JSON",
		},
		{
			name:    "neither planned_values nor values",
			input:   `{"format_version": "1.2"}`,
			wantErr: "terraform show -json",
		},
		{
			name:    "missing format_version",
			input:   `{"planned_values": {"root_module": {}}}`,
			wantErr: "terraform show -json",
		},
		{
			name:    "unsupported format_version major",
			input:   `{"format_version": "2.0", "planned_values": {"root_module": {}}}`,
			wantErr: `format_version "2.0"`,
		},
		{
			name:    "format_version major with a matching prefix",
			input:   `{"format_version": "10.1", "planned_values": {"root_module": {}}}`,
			wantErr: `format_version "10.1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input), Options{})
			if err == nil {
				t.Fatal("Parse() returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestParse_ModuleDepthLimit(t *testing.T) {
	nested := func(depth int) string {
		return `{"format_version": "1.2", "planned_values": {"root_module": ` +
			strings.Repeat(`{"child_modules": [`, depth) + `{}` + strings.Repeat(`]}`, depth) + `}}`
	}

	if _, err := Parse(strings.NewReader(nested(maxModuleDepth)), Options{}); err != nil {
		t.Errorf("Parse() at the depth limit error = %v", err)
	}
	if _, err := Parse(strings.NewReader(nested(maxModuleDepth+1)), Options{}); err == nil {
		t.Error("Parse() beyond the depth limit returned nil error")
	}
}

func TestParse_EmptyPlan(t *testing.T) {
	empty := `{"format_version":"1.2","planned_values":{"root_module":{}}}`

	result, err := Parse(strings.NewReader(empty), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result.Resources) != 0 {
		t.Errorf("got %d resources from an empty plan, want 0", len(result.Resources))
	}
}

func TestExtractTags_NullOrUnknownAttributesFallBack(t *testing.T) {
	prodTags := map[string]any{"environment": valueProd}

	tests := []struct {
		name        string
		values      map[string]any
		unknown     map[string]any
		wantTags    map[string]string
		wantUnknown []string
		wantRead    tagRead
	}{
		{
			name:     "null tags_all reads tags",
			values:   map[string]any{"tags_all": nil, "tags": prodTags},
			wantTags: map[string]string{"environment": valueProd},
			wantRead: tagsRead,
		},
		{
			name:     "null tags_all and tags is untagged",
			values:   map[string]any{"tags_all": nil, "tags": nil},
			wantTags: map[string]string{},
			wantRead: tagsRead,
		},
		{
			name:     "unknown tags_all with null tags is untagged",
			values:   map[string]any{"tags": nil},
			unknown:  map[string]any{"tags_all": true},
			wantTags: map[string]string{},
			wantRead: tagsRead,
		},
		{
			name:        "unknown tags_all reads tags and keeps unknown keys",
			values:      map[string]any{"tags": prodTags},
			unknown:     map[string]any{"tags_all": true, "tags": map[string]any{"ref": true, "app": true}},
			wantTags:    map[string]string{"environment": valueProd, "ref": "", "app": ""},
			wantUnknown: []string{"app", "ref"},
			wantRead:    tagsRead,
		},
		{
			name:     "unknown tags_all and tags cannot be read",
			values:   map[string]any{},
			unknown:  map[string]any{"tags_all": true, "tags": true},
			wantRead: tagsUnknown,
		},
		{
			name:     "null tags_all with unknown tags cannot be read",
			values:   map[string]any{"tags_all": nil},
			unknown:  map[string]any{"tags": true},
			wantRead: tagsUnknown,
		},
		{
			name:     "known values ignore an empty after_unknown",
			values:   map[string]any{"tags_all": prodTags},
			unknown:  map[string]any{"tags_all": map[string]any{}},
			wantTags: map[string]string{"environment": valueProd},
			wantRead: tagsRead,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags, unknownKeys, read := extractTags(tt.values, tt.unknown)
			if read != tt.wantRead {
				t.Errorf("read = %v, want %v", read, tt.wantRead)
			}
			if !equalTags(tags, tt.wantTags) {
				t.Errorf("tags = %v, want %v", tags, tt.wantTags)
			}
			if !slices.Equal(unknownKeys, tt.wantUnknown) {
				t.Errorf("unknown keys = %v, want %v", unknownKeys, tt.wantUnknown)
			}
		})
	}
}

func TestExtractTags_NullAndNonStringValues(t *testing.T) {
	values := map[string]any{
		"tags": map[string]any{
			"environment": "prod",
			"instance-id": nil,
			"port":        8080.0,
		},
	}

	tags, _, read := extractTags(values, nil)
	if read != tagsRead {
		t.Fatalf("extractTags() read = %v, want tagsRead", read)
	}
	if tags["environment"] != valueProd {
		t.Errorf("environment = %q, want prod", tags["environment"])
	}
	if value, present := tags["instance-id"]; !present || value != "" {
		t.Errorf("null value = %q (present %v), want present and empty", value, present)
	}
	if tags["port"] != "8080" {
		t.Errorf("non-string value = %q, want 8080", tags["port"])
	}
}

func TestExtractTags_Labels(t *testing.T) {
	values := map[string]any{"labels": map[string]any{"env": "prod"}}

	tags, _, read := extractTags(values, nil)
	if read != tagsRead {
		t.Fatal("labels were not recognised as a tag attribute")
	}
	if tags["env"] != valueProd {
		t.Errorf("env = %q, want prod", tags["env"])
	}
}

func TestExtractTags_NoTagAttribute(t *testing.T) {
	if _, _, read := extractTags(map[string]any{"policy": "{}"}, nil); read != noTagAttribute {
		t.Error("extractTags() claimed a tag attribute on a resource that has none")
	}
}

func TestProviderFrom(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"registry.terraform.io/hashicorp/aws", "aws"},
		{"registry.terraform.io/hashicorp/google", "google"},
		{"aws", "aws"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := providerFrom(tt.in); got != tt.want {
				t.Errorf("providerFrom(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func equalTags(got, want map[string]string) bool {
	if (got == nil) != (want == nil) || len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if gotValue, present := got[key]; !present || gotValue != value {
			return false
		}
	}
	return true
}
