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

func TestPrintable_StripsControlCharacters(t *testing.T) {
	if got := printable("ok\x1b[2Jbad\r\n"); got != "ok?[2Jbad??" {
		t.Errorf("printable() = %q", got)
	}
}
