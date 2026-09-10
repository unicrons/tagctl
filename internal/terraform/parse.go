// Package terraform reads the JSON that terraform show emits, so tag policy
// can be checked against infrastructure before it is created.
package terraform

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/unicrons/tagctl/internal/types"
)

// document is the shape shared by the plan and state JSON from terraform show.
//
// A plan puts the post-apply picture under planned_values; a state file puts
// the current picture under values. Both wrap the same module tree, so one
// type reads either.
type document struct {
	FormatVersion    string           `json:"format_version"`
	TerraformVersion string           `json:"terraform_version"`
	PlannedValues    *valuesWrapper   `json:"planned_values"`
	Values           *valuesWrapper   `json:"values"`
	ResourceChanges  []resourceChange `json:"resource_changes"`
}

type valuesWrapper struct {
	RootModule module `json:"root_module"`
}

// module is one level of the configuration tree.
type module struct {
	Address      string         `json:"address"`
	Resources    []planResource `json:"resources"`
	ChildModules []module       `json:"child_modules"`
}

// planResource is a resource as terraform reports it.
type planResource struct {
	Address      string         `json:"address"`
	Mode         string         `json:"mode"`
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	ProviderName string         `json:"provider_name"`
	Values       map[string]any `json:"values"`
}

// resourceChange is one entry of the plan's resource_changes list.
type resourceChange struct {
	Address string `json:"address"`
	Mode    string `json:"mode"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Change  struct {
		Actions []string `json:"actions"`
	} `json:"change"`
}

// Options controls which resources are returned.
type Options struct {
	// ChangedOnly limits the result to resources the plan creates or updates,
	// which is what a pull request check usually wants. Without it, every
	// resource in the plan is evaluated.
	ChangedOnly bool
}

// managedMode is the terraform mode for resources it manages. Data sources are
// read-only lookups and cannot be tagged.
const managedMode = "managed"

// Parse reads terraform plan or state JSON and returns the taggable resources
// it describes.
//
// Only managed resources are returned, and only those whose type actually
// carries a tag attribute: flagging an IAM policy for having no tags would be
// noise. Tags are read from tags_all when terraform provides it, because that
// is the set that includes the provider's default_tags — checking tags alone
// would report a violation on resources the provider tags for you.
func Parse(r io.Reader, opts Options) ([]types.Resource, error) {
	var doc document

	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to parse terraform JSON: %w", err)
	}

	root := doc.PlannedValues
	if root == nil {
		root = doc.Values
	}
	if root == nil {
		return nil, fmt.Errorf("no planned_values or values found: run 'terraform show -json' on a plan or state file")
	}

	var changing map[string]bool
	if opts.ChangedOnly {
		changing = changingAddresses(doc.ResourceChanges)
	}

	var resources []types.Resource
	collectModule(root.RootModule, changing, &resources)

	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })

	return resources, nil
}

// changingAddresses indexes the resources a plan creates or updates. A
// resource being destroyed does not need tags, and a no-op is already live.
func changingAddresses(changes []resourceChange) map[string]bool {
	changing := make(map[string]bool, len(changes))

	for _, change := range changes {
		for _, action := range change.Change.Actions {
			if action == "create" || action == "update" {
				changing[change.Address] = true
				break
			}
		}
	}

	return changing
}

// collectModule walks a module and its children, appending taggable resources.
func collectModule(m module, changing map[string]bool, out *[]types.Resource) {
	for _, resource := range m.Resources {
		if resource.Mode != managedMode {
			continue
		}
		if changing != nil && !changing[resource.Address] {
			continue
		}

		tags, ok := extractTags(resource.Values)
		if !ok {
			continue
		}

		*out = append(*out, types.Resource{
			ID:       resource.Address,
			Name:     resource.Name,
			Type:     resource.Type,
			Provider: providerFrom(resource.ProviderName),
			Tags:     tags,
		})
	}

	for _, child := range m.ChildModules {
		collectModule(child, changing, out)
	}
}

// tagAttributes are the attribute names providers use for tags, in the order
// they should be preferred. tags_all wins because it includes the provider's
// default_tags; labels is what the Google and Kubernetes providers use.
var tagAttributes = []string{"tags_all", "tags", "labels"}

// extractTags pulls the tag map out of a resource's values. The second return
// reports whether the resource carries a tag attribute at all, which is how
// resource types that cannot be tagged are skipped.
func extractTags(values map[string]any) (map[string]string, bool) {
	for _, attribute := range tagAttributes {
		raw, present := values[attribute]
		if !present {
			continue
		}

		// An unset tag block is null in the JSON. The resource still supports
		// tags, it simply has none, which is exactly what should be reported.
		if raw == nil {
			return map[string]string{}, true
		}

		asMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		tags := make(map[string]string, len(asMap))
		for key, value := range asMap {
			// Values not yet known at plan time come back as nil; treat them
			// as present but empty rather than dropping the key.
			if value == nil {
				tags[key] = ""
				continue
			}
			if str, isString := value.(string); isString {
				tags[key] = str
			} else {
				tags[key] = fmt.Sprint(value)
			}
		}

		return tags, true
	}

	return nil, false
}

// providerFrom reduces a terraform provider name to tagctl's short form:
// registry.terraform.io/hashicorp/aws becomes aws.
func providerFrom(name string) string {
	if name == "" {
		return ""
	}

	short := name
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			short = name[i+1:]
			break
		}
	}

	return short
}
