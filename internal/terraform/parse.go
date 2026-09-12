// Package terraform reads the JSON that terraform show emits, so tag policy
// can be checked against infrastructure before it is created.
package terraform

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

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
	Deposed string `json:"deposed"`
	Change  struct {
		Actions []string `json:"actions"`
		// AfterUnknown marks with true the values only known after apply,
		// which terraform leaves out of planned_values.
		AfterUnknown any `json:"after_unknown"`
	} `json:"change"`
}

// Options controls which resources are returned.
type Options struct {
	// ChangedOnly limits the result to resources the plan creates or updates,
	// which is what a pull request check usually wants. Without it, every
	// resource in the plan is evaluated.
	ChangedOnly bool
}

// Result is what Parse found in a terraform document.
type Result struct {
	// Resources are the taggable resources, sorted by address.
	Resources []types.Resource
	// Unreadable lists the addresses of resources whose tags are only known
	// after apply; they are not in Resources.
	Unreadable []string
}

// managedMode is the terraform mode for resources it manages. Data sources are
// read-only lookups and cannot be tagged.
const managedMode = "managed"

// supportedFormatMajor is the format_version major this parser reads.
const supportedFormatMajor = "1"

// maxModuleDepth bounds the recursion over child_modules.
const maxModuleDepth = 100

// Parse reads terraform plan or state JSON and returns the taggable resources
// it describes.
//
// Only managed resources are returned, and only those whose type actually
// carries a tag attribute: flagging an IAM policy for having no tags would be
// noise. Tags are read from tags_all when terraform provides it, because that
// is the set that includes the provider's default_tags — checking tags alone
// would report a violation on resources the provider tags for you.
func Parse(r io.Reader, opts Options) (Result, error) {
	var doc document

	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&doc); err != nil {
		return Result{}, fmt.Errorf("failed to parse terraform JSON: %w", err)
	}

	if err := checkFormatVersion(doc.FormatVersion); err != nil {
		return Result{}, err
	}

	root := doc.PlannedValues
	if root == nil {
		root = doc.Values
	}
	if root == nil {
		return Result{}, fmt.Errorf("no planned_values or values found: run 'terraform show -json' on a plan or state file")
	}

	c := collector{unknown: unknownValues(doc.ResourceChanges)}
	if opts.ChangedOnly {
		c.changing = changingAddresses(doc.ResourceChanges)
	}
	if err := c.collect(root.RootModule, 0); err != nil {
		return Result{}, err
	}

	sort.Slice(c.result.Resources, func(i, j int) bool { return c.result.Resources[i].ID < c.result.Resources[j].ID })
	sort.Strings(c.result.Unreadable)

	return c.result, nil
}

// checkFormatVersion rejects documents in a format this parser cannot read.
func checkFormatVersion(version string) error {
	if version == "" {
		return fmt.Errorf("no format_version found: run 'terraform show -json' on a plan or state file")
	}
	if major, _, _ := strings.Cut(version, "."); major != supportedFormatMajor {
		return fmt.Errorf("unsupported terraform JSON format_version %q: tagctl reads major version %s", version, supportedFormatMajor)
	}
	return nil
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

// unknownValues indexes each resource's after_unknown object by address.
// Deposed objects share the address of the live instance, so they are left out.
func unknownValues(changes []resourceChange) map[string]map[string]any {
	unknown := make(map[string]map[string]any, len(changes))

	for _, change := range changes {
		if change.Deposed != "" {
			continue
		}
		if afterUnknown, ok := change.Change.AfterUnknown.(map[string]any); ok {
			unknown[change.Address] = afterUnknown
		}
	}

	return unknown
}

// collector walks the module tree and gathers the taggable resources.
type collector struct {
	changing map[string]bool
	unknown  map[string]map[string]any
	result   Result
}

func (c *collector) collect(m module, depth int) error {
	if depth > maxModuleDepth {
		return fmt.Errorf("terraform JSON nests modules more than %d levels deep", maxModuleDepth)
	}

	for _, resource := range m.Resources {
		if resource.Mode != managedMode {
			continue
		}
		if c.changing != nil && !c.changing[resource.Address] {
			continue
		}

		tags, unknownKeys, read := extractTags(resource.Values, c.unknown[resource.Address])
		switch read {
		case noTagAttribute:
			continue
		case tagsUnknown:
			c.result.Unreadable = append(c.result.Unreadable, resource.Address)
			continue
		}

		c.result.Resources = append(c.result.Resources, types.Resource{
			ID:          resource.Address,
			Name:        resource.Name,
			Type:        resource.Type,
			Provider:    providerFrom(resource.ProviderName),
			Tags:        tags,
			UnknownTags: unknownKeys,
		})
	}

	for _, child := range m.ChildModules {
		if err := c.collect(child, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// tagAttributes are the attribute names providers use for tags, in the order
// they should be preferred. tags_all wins because it includes the provider's
// default_tags; labels is what the Google and Kubernetes providers use.
var tagAttributes = []string{"tags_all", "tags", "labels"}

// tagRead is what extractTags could learn about a resource's tags.
type tagRead int

const (
	noTagAttribute tagRead = iota
	tagsRead
	tagsUnknown
)

// extractTags reads the first tag attribute holding a known map, falling through null and
// unknown ones; when none holds a map, the last one decides: null is untagged, unknown unreadable.
func extractTags(values, unknown map[string]any) (map[string]string, []string, tagRead) {
	read := noTagAttribute

	for _, attribute := range tagAttributes {
		if unknown[attribute] == true {
			read = tagsUnknown
			continue
		}

		raw, present := values[attribute]
		if !present {
			continue
		}
		if raw == nil {
			read = tagsRead
			continue
		}
		if asMap, ok := raw.(map[string]any); ok {
			tags, unknownKeys := tagValues(asMap, unknown[attribute])
			return tags, unknownKeys, tagsRead
		}
	}

	if read == tagsRead {
		return map[string]string{}, nil, tagsRead
	}
	return nil, nil, read
}

// tagValues converts a tag map to strings. Keys whose value is only known
// after apply are marked in unknownKeys: they are kept empty and returned sorted.
func tagValues(known map[string]any, unknownKeys any) (map[string]string, []string) {
	tags := make(map[string]string, len(known))

	for key, value := range known {
		switch v := value.(type) {
		case nil:
			tags[key] = ""
		case string:
			tags[key] = v
		default:
			tags[key] = fmt.Sprint(v)
		}
	}

	keys, ok := unknownKeys.(map[string]any)
	if !ok {
		return tags, nil
	}

	unknown := make([]string, 0, len(keys))
	for key, isUnknown := range keys {
		if isUnknown == true {
			tags[key] = ""
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return tags, nil
	}
	sort.Strings(unknown)

	return tags, unknown
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
