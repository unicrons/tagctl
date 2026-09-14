package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResourceRef_RebuildsTheSameIdentity(t *testing.T) {
	resources := []Resource{
		{ID: "i-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-1", Type: "aws_instance", Provider: "aws", Account: "123456789012", Region: "us-east-1", Name: "web", Tags: map[string]string{"owner": "team@example.com"}},
		{ID: "web", Type: "k8s_deployment", Provider: "kubernetes", Account: "cluster", Region: "default"},
		{ID: "bucket", Type: "aws_s3_bucket"},
	}

	for _, resource := range resources {
		ref := resource.Ref()
		if ref.Identity != resource.Identity() {
			t.Errorf("Ref().Identity = %q, want %q", ref.Identity, resource.Identity())
		}
		rebuilt := ref.Resource()
		if got := rebuilt.Identity(); got != resource.Identity() {
			t.Errorf("Ref().Resource().Identity() = %q, want %q", got, resource.Identity())
		}
		if rebuilt.Type != resource.Type || rebuilt.ID != resource.ID {
			t.Errorf("rebuilt type/id = %q/%q, want %q/%q", rebuilt.Type, rebuilt.ID, resource.Type, resource.ID)
		}
	}
}

func TestScanResult_JSONCarriesResourcesOnlyWhenSet(t *testing.T) {
	empty, err := json.Marshal(NewScanResult())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(empty), `"resources"`) {
		t.Errorf("scan JSON without inventory = %s, want no resources key", empty)
	}

	resource := Resource{ID: "i-1", Type: "aws_instance", Provider: "aws", Account: "123456789012", Region: "us-east-1", Tags: map[string]string{"owner": "team@example.com"}}
	withInventory, err := json.Marshal(&ScanResult{Resources: []ResourceRef{resource.Ref()}})
	if err != nil {
		t.Fatal(err)
	}
	want := `"resources":[{"identity":"aws/123456789012/us-east-1/i-1","id":"i-1","type":"aws_instance","provider":"aws","account":"123456789012","region":"us-east-1"}]`
	if !strings.Contains(string(withInventory), want) {
		t.Errorf("scan JSON = %s, missing %s", withInventory, want)
	}
}
