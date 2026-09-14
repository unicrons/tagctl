package report

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

// JUnitTestSuites is the root of a JUnit XML report.
type JUnitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite groups the checks for one tag.
type JUnitTestSuite struct {
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Timestamp string          `xml:"timestamp,attr"`
	Cases     []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase is one resource checked against one tag.
type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
}

// JUnitFailure describes why a test case failed.
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// WriteJUnit renders a scan as a JUnit XML report.
//
// Every finding becomes a test case, passing ones included, so a CI system
// shows the true ratio rather than only the failures. Cases are grouped into
// one suite per tag, which is how the report is most useful to read: it makes
// it obvious that, say, every failure is the owner tag.
func WriteJUnit(w io.Writer, scan *types.ScanResult) error {
	byTag := groupFindingsByTag(scan)

	tags := make([]string, 0, len(byTag))
	for tag := range byTag {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	timestamp := scan.ScannedAt.UTC().Format(time.RFC3339)

	report := JUnitTestSuites{
		Name:   toolName,
		Time:   "0",
		Suites: make([]JUnitTestSuite, 0, len(tags)),
	}

	for _, tag := range tags {
		findings := byTag[tag]
		suite := JUnitTestSuite{
			Name:      "tag: " + tag,
			Timestamp: timestamp,
			Cases:     make([]JUnitTestCase, 0, len(findings)),
		}

		for _, finding := range findings {
			testCase := JUnitTestCase{
				Name:      fmt.Sprintf("%s %s", finding.Resource.Type, finding.Resource.ID),
				ClassName: junitClassName(finding),
				Time:      "0",
			}

			if finding.Status == types.StatusFailed {
				testCase.Failure = &JUnitFailure{
					Message: findingMessage(finding),
					Type:    string(finding.Reason),
					Content: junitFailureDetail(finding),
				}
				suite.Failures++
				report.Failures++
			}

			suite.Tests++
			report.Tests++
			suite.Cases = append(suite.Cases, testCase)
		}

		report.Suites = append(report.Suites, suite)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}

	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}

	_, err := io.WriteString(w, "\n")
	return err
}

// groupFindingsByTag buckets every finding, passing and failing, by tag.
func groupFindingsByTag(scan *types.ScanResult) map[string][]types.Finding {
	findings := scan.Findings
	if len(findings) == 0 && len(scan.Violations) > 0 {
		findings = types.ViolationsToFindings(scan.Violations)
	}

	byTag := make(map[string][]types.Finding)
	for _, finding := range findings {
		byTag[finding.Tag] = append(byTag[finding.Tag], finding)
	}

	// Keep the order of cases stable across runs.
	for tag := range byTag {
		cases := byTag[tag]
		sort.Slice(cases, func(i, j int) bool {
			if cases[i].Resource.Account != cases[j].Resource.Account {
				return cases[i].Resource.Account < cases[j].Resource.Account
			}
			return cases[i].Resource.ID < cases[j].Resource.ID
		})
	}

	return byTag
}

// junitClassName places a case in the provider, account and region it came
// from, which is how CI reports group results into a browsable tree. Unknown
// parts are left out: a Terraform plan has no account or region yet.
func junitClassName(finding types.Finding) string {
	resource := finding.Resource
	parts := make([]string, 0, 3)
	for _, part := range []string{resource.Provider, resource.Account, resource.Region} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return toolName
	}
	return strings.Join(parts, ".")
}

// junitFailureDetail is the body of a failure, with the context needed to act
// on it without opening the scan file.
func junitFailureDetail(finding types.Finding) string {
	resource := finding.Resource

	detail := fmt.Sprintf("resource: %s\ntype: %s\naccount: %s\nregion: %s\ntag: %s\nreason: %s",
		resource.ID, resource.Type, resource.Account, resource.Region, finding.Tag, finding.Reason)

	if finding.Expected != "" {
		detail += "\nexpected: " + finding.Expected
	}
	if finding.Actual != "" {
		detail += "\nactual: " + finding.Actual
	}
	if resource.ARN != "" {
		detail += "\narn: " + resource.ARN
	}

	return detail
}
