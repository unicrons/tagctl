package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

// ocsfVersion is the OCSF schema version the events conform to.
const ocsfVersion = "1.4.0"

// Compliance Finding (class 2003) identifiers. type_uid is class_uid * 100 +
// activity_id, as the schema prescribes.
const (
	ocsfCategoryUID    = 2
	ocsfClassUID       = 2003
	ocsfActivityCreate = 1
	ocsfTypeUIDCreate  = ocsfClassUID*100 + ocsfActivityCreate
)

// compliance.status_id values.
const (
	ocsfCompliancePass = 1
	ocsfComplianceFail = 3
)

// severity_id values.
const (
	ocsfSeverityInformational = 1
	ocsfSeverityLow           = 2
	ocsfSeverityMedium        = 3
)

// Enumerations from the schema that the events reference.
const (
	ocsfStatusNew          = 1  // status_id
	ocsfAnalyticRule       = 1  // analytic.type_id
	ocsfObservableResource = 10 // observable.type_id "Resource UID"
	ocsfAccountAWS         = 10 // account.type_id "AWS Account"
)

// OCSFComplianceFinding is one OCSF Compliance Finding event (class_uid 2003).
type OCSFComplianceFinding struct {
	ActivityID   int    `json:"activity_id"`
	ActivityName string `json:"activity_name"`
	CategoryUID  int    `json:"category_uid"`
	CategoryName string `json:"category_name"`
	ClassUID     int    `json:"class_uid"`
	ClassName    string `json:"class_name"`
	TypeUID      int    `json:"type_uid"`
	TypeName     string `json:"type_name"`

	Time   int64  `json:"time"`
	TimeDT string `json:"time_dt"`

	SeverityID int    `json:"severity_id"`
	Severity   string `json:"severity"`
	StatusID   int    `json:"status_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`

	Cloud       OCSFCloud        `json:"cloud"`
	Compliance  OCSFCompliance   `json:"compliance"`
	FindingInfo OCSFFindingInfo  `json:"finding_info"`
	Metadata    OCSFMetadata     `json:"metadata"`
	Observables []OCSFObservable `json:"observables"`
	Osint       []struct{}       `json:"osint"`
	Remediation *OCSFRemediation `json:"remediation,omitempty"`
	Resources   []OCSFResource   `json:"resources"`
	Unmapped    OCSFUnmapped     `json:"unmapped"`
}

// OCSFCloud locates the finding in a cloud provider.
type OCSFCloud struct {
	Provider string       `json:"provider"`
	Region   string       `json:"region,omitempty"`
	Account  *OCSFAccount `json:"account,omitempty"`
}

// OCSFAccount identifies the account the resource belongs to.
type OCSFAccount struct {
	UID    string `json:"uid"`
	TypeID int    `json:"type_id,omitempty"`
	Type   string `json:"type,omitempty"`
}

// OCSFCompliance carries the outcome of the tag policy check.
type OCSFCompliance struct {
	Standards     []string `json:"standards"`
	Control       string   `json:"control"`
	Requirements  []string `json:"requirements,omitempty"`
	StatusID      int      `json:"status_id"`
	Status        string   `json:"status"`
	StatusDetails []string `json:"status_details"`
}

// OCSFFindingInfo identifies the finding and the rule that produced it.
type OCSFFindingInfo struct {
	UID         string       `json:"uid"`
	Title       string       `json:"title"`
	Desc        string       `json:"desc"`
	CreatedTime int64        `json:"created_time"`
	Types       []string     `json:"types"`
	Analytic    OCSFAnalytic `json:"analytic"`
}

// OCSFAnalytic names the policy rule as an OCSF analytic of type Rule.
type OCSFAnalytic struct {
	TypeID int    `json:"type_id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	UID    string `json:"uid"`
}

// OCSFMetadata describes the producing tool and schema version.
type OCSFMetadata struct {
	Version  string      `json:"version"`
	Product  OCSFProduct `json:"product"`
	Profiles []string    `json:"profiles"`
}

// OCSFProduct identifies tagctl as the product that reported the event.
type OCSFProduct struct {
	Name       string `json:"name"`
	VendorName string `json:"vendor_name"`
	Version    string `json:"version,omitempty"`
	URLString  string `json:"url_string"`
}

// OCSFObservable surfaces the resource identity for correlation.
type OCSFObservable struct {
	Name   string `json:"name"`
	TypeID int    `json:"type_id"`
	Type   string `json:"type"`
	Value  string `json:"value"`
}

// OCSFRemediation tells the consumer how to fix a failed finding.
type OCSFRemediation struct {
	Desc       string   `json:"desc"`
	References []string `json:"references,omitempty"`
}

// OCSFResource is the resource the finding is about.
type OCSFResource struct {
	UID    string         `json:"uid"`
	Name   string         `json:"name,omitempty"`
	Type   string         `json:"type,omitempty"`
	Region string         `json:"region,omitempty"`
	Tags   []OCSFKeyValue `json:"tags,omitempty"`
}

// OCSFKeyValue is an OCSF key:value object.
type OCSFKeyValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// OCSFUnmapped keeps the tagctl-specific fields that have no OCSF attribute.
type OCSFUnmapped struct {
	Reason   string `json:"reason"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// OCSFOptions configures the OCSF output.
type OCSFOptions struct {
	// Version is the tagctl version recorded in metadata.product.
	Version string

	// Lines writes one compact event per line (NDJSON) instead of an array.
	Lines bool
}

// OCSFLinesPath reports whether a report path asks for NDJSON by its extension.
func OCSFLinesPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ndjson", ".jsonl":
		return true
	}
	return false
}

// WriteOCSF renders every finding of a scan as OCSF Compliance Finding events
// (class_uid 2003, schema 1.4.0): an indented array, or one per line with
// opts.Lines.
//
// Passing findings are emitted too, with compliance.status Pass: a compliance
// consumer needs the denominator, and a resource that stops appearing is
// indistinguishable from one that was deleted. finding_info.uid is the same
// resource#tag identity SARIF uses as its fingerprint, so both outputs point
// at the same finding.
func WriteOCSF(w io.Writer, scan *types.ScanResult, opts OCSFOptions) error {
	findings := scan.Findings
	if len(findings) == 0 && len(scan.Violations) > 0 {
		findings = types.ViolationsToFindings(scan.Violations)
	}

	events := make([]OCSFComplianceFinding, 0, len(findings))
	for i := range findings {
		events = append(events, ocsfEvent(&findings[i], scan.ScannedAt, opts))
	}

	encoder := json.NewEncoder(w)
	if opts.Lines {
		for i := range events {
			if err := encoder.Encode(&events[i]); err != nil {
				return fmt.Errorf("failed to encode OCSF finding: %w", err)
			}
		}
		return nil
	}

	encoder.SetIndent("", "  ")
	if err := encoder.Encode(events); err != nil {
		return fmt.Errorf("failed to encode OCSF findings: %w", err)
	}
	return nil
}

func ocsfEvent(f *types.Finding, scannedAt time.Time, opts OCSFOptions) OCSFComplianceFinding {
	resource := f.Resource
	uid := resource.Identity()
	failed := f.Status == types.StatusFailed
	severityID, severity := ocsfSeverity(f)

	event := OCSFComplianceFinding{
		ActivityID:   ocsfActivityCreate,
		ActivityName: "Create",
		CategoryUID:  ocsfCategoryUID,
		CategoryName: "Findings",
		ClassUID:     ocsfClassUID,
		ClassName:    "Compliance Finding",
		TypeUID:      ocsfTypeUIDCreate,
		TypeName:     "Compliance Finding: Create",
		Time:         scannedAt.UnixMilli(),
		TimeDT:       scannedAt.UTC().Format(time.RFC3339),
		SeverityID:   severityID,
		Severity:     severity,
		StatusID:     ocsfStatusNew,
		Status:       "New",
		Message:      describeResource(resource) + ": " + findingMessage(*f),
		Cloud:        ocsfCloud(resource),
		Compliance:   ocsfCompliance(f),
		FindingInfo: OCSFFindingInfo{
			UID:         uid + "#" + f.Tag,
			Title:       findingMessage(*f),
			Desc:        describeResource(resource) + ": " + findingMessage(*f),
			CreatedTime: scannedAt.UnixMilli(),
			Types:       []string{"Tag Compliance"},
			Analytic: OCSFAnalytic{
				TypeID: ocsfAnalyticRule,
				Type:   "Rule",
				Name:   ruleID(*f),
				UID:    ruleID(*f),
			},
		},
		Metadata: OCSFMetadata{
			Version: ocsfVersion,
			Product: OCSFProduct{
				Name:       toolName,
				VendorName: toolName,
				Version:    opts.Version,
				URLString:  "https://github.com/unicrons/tagctl",
			},
			Profiles: []string{"cloud", "datetime"},
		},
		Observables: []OCSFObservable{{
			Name:   "resources[0].uid",
			TypeID: ocsfObservableResource,
			Type:   "Resource UID",
			Value:  uid,
		}},
		Osint: []struct{}{},
		Resources: []OCSFResource{{
			UID:    uid,
			Name:   resource.DisplayName(),
			Type:   resource.Type,
			Region: resource.Region,
			Tags:   ocsfTags(resource.Tags),
		}},
		Unmapped: OCSFUnmapped{
			Reason:   string(f.Reason),
			Expected: f.Expected,
			Actual:   f.Actual,
		},
	}

	if failed {
		event.Remediation = &OCSFRemediation{
			Desc:       ocsfRemediation(f),
			References: []string{"https://github.com/unicrons/tagctl#readme"},
		}
	}

	return event
}

// ocsfSeverity mirrors sarifLevel: a missing required tag is Medium because
// the resource cannot be attributed at all, a wrong value is Low because the
// resource is at least identifiable, and a pass is Informational.
func ocsfSeverity(f *types.Finding) (int, string) {
	switch {
	case f.Status != types.StatusFailed:
		return ocsfSeverityInformational, "Informational"
	case f.Reason == types.ReasonMissing:
		return ocsfSeverityMedium, "Medium"
	default:
		return ocsfSeverityLow, "Low"
	}
}

func ocsfCompliance(f *types.Finding) OCSFCompliance {
	c := OCSFCompliance{
		Standards:     []string{"tagctl tag policy"},
		Control:       f.Tag,
		StatusID:      ocsfCompliancePass,
		Status:        "Pass",
		StatusDetails: []string{findingMessage(*f)},
	}
	if f.Expected != "" {
		c.Requirements = []string{f.Expected}
	}
	if f.Status == types.StatusFailed {
		c.StatusID = ocsfComplianceFail
		c.Status = "Fail"
	}
	return c
}

// ocsfCloud maps the provider to the capitalised names OCSF consumers expect.
// The account is omitted when unknown (a Terraform plan has none yet) because
// the account object requires a name or uid.
func ocsfCloud(r types.Resource) OCSFCloud {
	cloud := OCSFCloud{Provider: r.Provider, Region: r.Region}

	switch r.Provider {
	case "aws":
		cloud.Provider = "AWS"
	case "kubernetes":
		cloud.Provider = "Kubernetes"
	}

	if r.Account != "" {
		cloud.Account = &OCSFAccount{UID: r.Account}
		if r.Provider == "aws" {
			cloud.Account.TypeID = ocsfAccountAWS
			cloud.Account.Type = "AWS Account"
		}
	}
	return cloud
}

func ocsfTags(tags map[string]string) []OCSFKeyValue {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	kv := make([]OCSFKeyValue, 0, len(keys))
	for _, k := range keys {
		kv = append(kv, OCSFKeyValue{Name: k, Value: tags[k]})
	}
	return kv
}

func ocsfRemediation(f *types.Finding) string {
	if f.Reason == types.ReasonForbidden {
		return fmt.Sprintf("Remove tag '%s' from %s. Run 'tagctl plan' to propose the removal and 'tagctl apply' to perform it.",
			f.Tag, describeResource(f.Resource))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Set tag '%s' on %s", f.Tag, describeResource(f.Resource))
	if f.Expected != "" {
		fmt.Fprintf(&b, " to a value satisfying: %s", f.Expected)
	}
	b.WriteString(". Run 'tagctl plan' to propose the value and 'tagctl apply' to write it.")
	return b.String()
}
