package cli

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/unicrons/tagctl/internal/types"
)

func TestPrintFindingsTable_AlignsAndTruncates(t *testing.T) {
	long := strings.Repeat("a", 200)
	findings := []types.Finding{
		{Resource: types.Resource{ID: long, Type: "aws_s3_bucket"}, Tag: "owner", Status: types.StatusFailed, Actual: long},
		{Resource: types.Resource{ID: "i-1", Type: "aws_ec2_instance"}, Tag: "environment", Status: types.StatusPass, Actual: "prod"},
	}
	var buf bytes.Buffer
	printFindingsTable(&buf, findings)

	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	lines := strings.Split(strings.TrimRight(ansi.ReplaceAllString(buf.String(), ""), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4:\n%s", len(lines), buf.String())
	}
	header, _, _ := strings.Cut(lines[0], "TYPE")
	typeColumn := utf8.RuneCountInString(header)
	for _, line := range lines[2:] {
		prefix, _, _ := strings.Cut(line, "aws_")
		if got := utf8.RuneCountInString(prefix); got != typeColumn {
			t.Errorf("TYPE starts at column %d, header at %d: %q", got, typeColumn, line)
		}
		if strings.Contains(line, strings.Repeat("a", maxResourceWidth)) {
			t.Errorf("long cell not truncated: %q", line)
		}
	}
}
