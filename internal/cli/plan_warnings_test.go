package cli

import (
	"bytes"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestPrintPlanWarnings(t *testing.T) {
	var buf bytes.Buffer
	printPlanWarnings(&buf, &types.Plan{Warnings: []string{"rules.inherit could not be applied"}})
	if got, want := buf.String(), "Warning: rules.inherit could not be applied\n"; got != want {
		t.Errorf("printPlanWarnings() wrote %q, want %q", got, want)
	}

	buf.Reset()
	printPlanWarnings(&buf, &types.Plan{})
	if buf.Len() != 0 {
		t.Errorf("printPlanWarnings() wrote %q for a plan without warnings", buf.String())
	}
}
