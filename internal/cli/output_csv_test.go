package cli

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestWriteFindingsCSV_QuotesAndNeutralisesFormulas(t *testing.T) {
	result := &types.ScanResult{Findings: []types.Finding{{
		Resource: types.Resource{ID: "i-1", Name: `web, "prod"`, Type: "aws_instance", Provider: "aws"},
		Tag:      "owner", Status: types.StatusFailed, Reason: types.ReasonInvalidValue,
		Actual: "=HYPERLINK(\"http://evil\")", Expected: "^.+@.+$",
	}}}

	var buf bytes.Buffer
	if err := writeFindingsCSV(&buf, result); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if len(rows) != 2 || len(rows[1]) != len(csvHeader) {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1][1] != `web, "prod"` {
		t.Errorf("name = %q, want it quoted and intact", rows[1][1])
	}
	if !strings.HasPrefix(rows[1][10], "'=") {
		t.Errorf("actual = %q, want the formula neutralised", rows[1][10])
	}
}

func TestCSVSafe(t *testing.T) {
	tests := []struct {
		name, cell string
		quoted     bool
	}{
		{name: "empty", cell: ""},
		{name: "plain value", cell: "platform@company.com"},
		{name: "trigger in the middle", cell: "a=b+c-d"},
		{name: "leading space only", cell: " prod"},
		{name: "equals", cell: "=1+1", quoted: true},
		{name: "plus", cell: "+1", quoted: true},
		{name: "minus", cell: "-1", quoted: true},
		{name: "at", cell: "@SUM(A1)", quoted: true},
		{name: "space before equals", cell: " =1+1", quoted: true},
		{name: "spaces before at", cell: "   @SUM(A1)", quoted: true},
		{name: "tab before equals", cell: "\t=1+1", quoted: true},
		{name: "carriage return before plus", cell: "\r+1", quoted: true},
		{name: "newline before minus", cell: "\n-1", quoted: true},
		{name: "mixed whitespace before equals", cell: " \t\r\n =HYPERLINK(A0)", quoted: true},
		{name: "no-break space before equals", cell: " =1+1", quoted: true},
		{name: "control character before equals", cell: "\x00=1+1", quoted: true},
		{name: "leading tab", cell: "\tprod", quoted: true},
		{name: "leading carriage return", cell: "\rprod", quoted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.cell
			if tt.quoted {
				want = "'" + tt.cell
			}
			if got := csvSafe(tt.cell); got != want {
				t.Errorf("csvSafe(%q) = %q, want %q", tt.cell, got, want)
			}
		})
	}
}

func TestPrintable_StripsControlCharacters(t *testing.T) {
	if got := printable("ok\x1b[2Jbad\r\n"); got != "ok?[2Jbad??" {
		t.Errorf("printable() = %q", got)
	}
}
