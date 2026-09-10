package terraform

import (
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

// valueProd is the environment value the fixtures use.
const valueProd = "prod"

// parsePlan parses the plan fixture and indexes the result by address.
func parsePlan(t *testing.T, opts Options) map[string]map[string]string {
	t.Helper()

	resources, err := Parse(strings.NewReader(planJSON), opts)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	byAddress := make(map[string]map[string]string, len(resources))
	for _, resource := range resources {
		byAddress[resource.ID] = resource.Tags
	}
	return byAddress
}

func TestParse_Plan(t *testing.T) {
	resources, err := Parse(strings.NewReader(planJSON), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

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
	resources, err := Parse(strings.NewReader(stateJSON), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

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
	resources, err := Parse(strings.NewReader(stateJSON), Options{ChangedOnly: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("got %d resources, want 0: a state file records no changes", len(resources))
	}
}

func TestParse_Errors(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		if _, err := Parse(strings.NewReader("{not json"), Options{}); err == nil {
			t.Error("Parse() on malformed JSON returned nil error")
		}
	})

	t.Run("neither planned_values nor values", func(t *testing.T) {
		_, err := Parse(strings.NewReader(`{"format_version": "1.2"}`), Options{})
		if err == nil {
			t.Fatal("Parse() on a document with no module tree returned nil error")
		}
		if !strings.Contains(err.Error(), "terraform show -json") {
			t.Errorf("error should say how to produce the right input, got: %v", err)
		}
	})
}

func TestParse_EmptyPlan(t *testing.T) {
	empty := `{"format_version":"1.2","planned_values":{"root_module":{}}}`

	resources, err := Parse(strings.NewReader(empty), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("got %d resources from an empty plan, want 0", len(resources))
	}
}

// Values unknown until apply come back as null; the key is still declared.
func TestExtractTags_UnknownValues(t *testing.T) {
	values := map[string]any{
		"tags": map[string]any{
			"environment": "prod",
			"instance-id": nil,
			"port":        8080.0,
		},
	}

	tags, ok := extractTags(values)
	if !ok {
		t.Fatal("extractTags() reported no tag attribute")
	}
	if tags["environment"] != valueProd {
		t.Errorf("environment = %q, want prod", tags["environment"])
	}
	if _, present := tags["instance-id"]; !present {
		t.Error("a tag whose value is unknown at plan time was dropped")
	}
	if tags["instance-id"] != "" {
		t.Errorf("unknown value = %q, want empty", tags["instance-id"])
	}
	if tags["port"] != "8080" {
		t.Errorf("non-string value = %q, want 8080", tags["port"])
	}
}

func TestExtractTags_Labels(t *testing.T) {
	values := map[string]any{"labels": map[string]any{"env": "prod"}}

	tags, ok := extractTags(values)
	if !ok {
		t.Fatal("labels were not recognised as a tag attribute")
	}
	if tags["env"] != valueProd {
		t.Errorf("env = %q, want prod", tags["env"])
	}
}

func TestExtractTags_NoTagAttribute(t *testing.T) {
	if _, ok := extractTags(map[string]any{"policy": "{}"}); ok {
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
