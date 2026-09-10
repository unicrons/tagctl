package report

import "github.com/unicrons/tagctl/internal/types"

// failedFindings returns only the FAILED findings of a scan, reading the
// deprecated Violations field when the scan was written by an older version.
func failedFindings(scan *types.ScanResult) []types.Finding {
	failures := make([]types.Finding, 0, len(scan.Findings))

	for _, finding := range scan.Findings {
		if finding.Status == types.StatusFailed {
			failures = append(failures, finding)
		}
	}

	if len(failures) == 0 && len(scan.Violations) > 0 {
		failures = types.ViolationsToFindings(scan.Violations)
	}

	return failures
}

// findingMessage describes a finding in one line.
func findingMessage(finding types.Finding) string {
	return finding.Message()
}

// toolName is how the reports identify tagctl to the consuming tool.
const toolName = "tagctl"
