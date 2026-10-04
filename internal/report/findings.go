package report

import "github.com/unicrons/tagctl/internal/types"

// findingMessage describes a finding in one line.
func findingMessage(finding types.Finding) string {
	return finding.Message()
}

// toolName is how the reports identify tagctl to the consuming tool.
const toolName = "tagctl"
