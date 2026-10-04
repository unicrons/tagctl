// Package report renders scan results in the machine-readable formats CI
// systems consume: SARIF for code scanning, JUnit for test reporting.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/unicrons/tagctl/internal/types"
)

// sarifVersion is the SARIF specification this output conforms to.
const sarifVersion = "2.1.0"

// sarifSchema is the schema URL GitHub validates uploads against.
const sarifSchema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

// SARIFLog is the root of a SARIF document.
type SARIFLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []SARIFRun `json:"runs"`
}

// SARIFRun is a single tool invocation.
type SARIFRun struct {
	Tool              SARIFTool               `json:"tool"`
	AutomationDetails *SARIFAutomationDetails `json:"automationDetails,omitempty"`
	Invocations       []SARIFInvocation       `json:"invocations"`
	Results           []SARIFResult           `json:"results"`
}

// SARIFAutomationDetails names the analysis a run belongs to. Code scanning
// uses the id as the category, so runs of different commands do not close
// each other's alerts.
type SARIFAutomationDetails struct {
	ID string `json:"id"`
}

// SARIFInvocation records whether the run covered everything it was asked to.
type SARIFInvocation struct {
	ExecutionSuccessful        bool                `json:"executionSuccessful"`
	ToolExecutionNotifications []SARIFNotification `json:"toolExecutionNotifications,omitempty"`
}

// SARIFNotification is a problem the tool hit while running.
type SARIFNotification struct {
	Level   string    `json:"level"`
	Message SARIFText `json:"message"`
}

// SARIFTool identifies the tool that produced the run.
type SARIFTool struct {
	Driver SARIFDriver `json:"driver"`
}

// SARIFDriver describes the analysis tool and the rules it can report.
type SARIFDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version,omitempty"`
	Rules          []SARIFRule `json:"rules"`
}

// SARIFRule is one check the tool can report, one per required tag.
type SARIFRule struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	ShortDescription SARIFText         `json:"shortDescription"`
	FullDescription  SARIFText         `json:"fullDescription"`
	DefaultConfig    SARIFRuleConfig   `json:"defaultConfiguration"`
	Properties       map[string]any    `json:"properties,omitempty"`
	Help             *SARIFMultiFormat `json:"help,omitempty"`
}

// SARIFRuleConfig carries a rule's default severity.
type SARIFRuleConfig struct {
	Level string `json:"level"`
}

// SARIFText is a plain-text message.
type SARIFText struct {
	Text string `json:"text"`
}

// SARIFMultiFormat is a message with an optional markdown rendering.
type SARIFMultiFormat struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown,omitempty"`
}

// SARIFResult is a single finding.
type SARIFResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             SARIFText         `json:"message"`
	Locations           []SARIFLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	Properties          map[string]string `json:"properties,omitempty"`
}

// SARIFLocation is where a finding applies.
type SARIFLocation struct {
	PhysicalLocation SARIFPhysicalLoc  `json:"physicalLocation"`
	LogicalLocations []SARIFLogicalLoc `json:"logicalLocations,omitempty"`
}

// SARIFPhysicalLoc points at a file. Cloud resources have no source file, so
// this points at the policy that the resource was judged against, which is
// what a reviewer would open.
type SARIFPhysicalLoc struct {
	ArtifactLocation SARIFArtifactLoc  `json:"artifactLocation"`
	Region           SARIFRegionMarker `json:"region"`
}

// SARIFArtifactLoc names the file a result is anchored to.
type SARIFArtifactLoc struct {
	URI string `json:"uri"`
}

// SARIFRegionMarker anchors a result to a line. SARIF requires one, and every
// result is anchored to the first line of the policy file.
type SARIFRegionMarker struct {
	StartLine int `json:"startLine"`
}

// SARIFLogicalLoc names the cloud resource a finding is about. This is where
// the resource identity actually lives, since there is no source location.
type SARIFLogicalLoc struct {
	Name               string `json:"name"`
	FullyQualifiedName string `json:"fullyQualifiedName"`
	Kind               string `json:"kind"`
}

// SARIFOptions controls how a SARIF document is rendered.
type SARIFOptions struct {
	// PolicyFile is the path results are anchored to. Defaults to tagctl.yaml.
	PolicyFile string

	// Command is the tagctl command that produced the run, recorded as
	// automationDetails.id tagctl/<command>/. Empty omits it.
	Command string

	// Version is the tagctl version recorded in the document.
	Version string
}

// WriteSARIF renders a scan's failed findings as a SARIF document.
//
// Only failures are emitted: a SARIF result means "something to look at", and
// code scanning would otherwise show a passing resource as an alert. One rule
// is declared per required tag, so GitHub groups alerts by the tag that failed
// rather than lumping every violation together.
func WriteSARIF(w io.Writer, scan *types.ScanResult, opts SARIFOptions) error {
	policyFile := opts.PolicyFile
	if policyFile == "" {
		policyFile = "tagctl.yaml"
	}

	failures := scan.FailedFindings()

	log := SARIFLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []SARIFRun{
			{
				Tool: SARIFTool{
					Driver: SARIFDriver{
						Name:           toolName,
						InformationURI: "https://github.com/unicrons/tagctl",
						Version:        opts.Version,
						Rules:          rulesFor(failures),
					},
				},
				AutomationDetails: automationDetails(opts.Command),
				Invocations:       []SARIFInvocation{invocationOf(scan)},
				Results:           resultsFor(failures, policyFile),
			},
		},
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(log)
}

func automationDetails(command string) *SARIFAutomationDetails {
	if command == "" {
		return nil
	}
	return &SARIFAutomationDetails{ID: toolName + "/" + command + "/"}
}

// invocationOf marks a partial scan unsuccessful, so code scanning does not
// treat the alerts missing from it as fixed.
func invocationOf(scan *types.ScanResult) SARIFInvocation {
	invocation := SARIFInvocation{ExecutionSuccessful: !scan.Partial}
	for _, scanErr := range scan.Errors {
		invocation.ToolExecutionNotifications = append(invocation.ToolExecutionNotifications, SARIFNotification{
			Level:   "error",
			Message: SARIFText{Text: scanErr},
		})
	}
	return invocation
}

// ruleID is the stable SARIF rule identifier for a tag.
func ruleID(tag string) string {
	return "tagctl/missing-or-invalid-tag/" + tag
}

// rulesFor declares one rule per tag that has at least one failure.
func rulesFor(failures []types.Finding) []SARIFRule {
	seen := make(map[string]bool, len(failures))
	tags := make([]string, 0, len(failures))

	for _, finding := range failures {
		if seen[finding.Tag] {
			continue
		}
		seen[finding.Tag] = true
		tags = append(tags, finding.Tag)
	}

	sort.Strings(tags)

	rules := make([]SARIFRule, 0, len(tags))
	for _, tag := range tags {
		rules = append(rules, SARIFRule{
			ID:               ruleID(tag),
			Name:             "MissingOrInvalidTag",
			ShortDescription: SARIFText{Text: fmt.Sprintf("Tag '%s' is missing or invalid", tag)},
			FullDescription: SARIFText{
				Text: fmt.Sprintf("The tag policy requires '%s' on this resource. A resource without it cannot be attributed to an owner, an environment or a cost centre.", tag),
			},
			DefaultConfig: SARIFRuleConfig{Level: "warning"},
			Help: &SARIFMultiFormat{
				Text:     fmt.Sprintf("Add the '%s' tag, or run 'tagctl plan' and 'tagctl apply' to set it from your rules.", tag),
				Markdown: fmt.Sprintf("Add the `%s` tag, or run `tagctl plan` followed by `tagctl apply` to set it from your rules.", tag),
			},
			Properties: map[string]any{"tags": []string{"tagging", "governance", "finops"}},
		})
	}

	return rules
}

// resultsFor renders one SARIF result per failed finding.
func resultsFor(failures []types.Finding, policyFile string) []SARIFResult {
	results := make([]SARIFResult, 0, len(failures))

	for _, finding := range failures {
		resource := finding.Resource
		qualified := resource.Identity()

		results = append(results, SARIFResult{
			RuleID:  ruleID(finding.Tag),
			Level:   sarifLevel(finding.Reason),
			Message: SARIFText{Text: fmt.Sprintf("%s: %s", describeResource(resource), findingMessage(finding))},
			Locations: []SARIFLocation{
				{
					PhysicalLocation: SARIFPhysicalLoc{
						ArtifactLocation: SARIFArtifactLoc{URI: policyFile},
						Region:           SARIFRegionMarker{StartLine: 1},
					},
					LogicalLocations: []SARIFLogicalLoc{
						{
							Name:               resource.ID,
							FullyQualifiedName: qualified,
							Kind:               "resource",
						},
					},
				},
			},
			// Keeps an alert attached to the same resource and tag across runs,
			// so GitHub does not reopen it every scan.
			PartialFingerprints: map[string]string{
				"tagctl/resourceTag": qualified + "#" + finding.Tag,
			},
			Properties: map[string]string{
				"resourceId":   resource.ID,
				"resourceType": resource.Type,
				"account":      resource.Account,
				"region":       resource.Region,
				"provider":     resource.Provider,
				"tag":          finding.Tag,
				"reason":       string(finding.Reason),
			},
		})
	}

	return results
}

// describeResource names a resource without repeating its type. A Terraform
// address already starts with the type, so "aws_s3_bucket aws_s3_bucket.logs"
// would be noise.
func describeResource(resource types.Resource) string {
	if resource.Type == "" || strings.HasPrefix(resource.ID, resource.Type) {
		return resource.ID
	}
	return resource.Type + " " + resource.ID
}

// sarifLevel maps a finding reason to a SARIF severity. A missing required tag
// is an error because the resource cannot be attributed at all; a wrong value
// is a warning because the resource is at least identifiable.
func sarifLevel(reason types.ViolationReason) string {
	if reason == types.ReasonMissing {
		return "error"
	}
	return "warning"
}
