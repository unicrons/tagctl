package demo

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/unicrons/tagctl/test/testutil"
)

// assertGolden compares v, as the CLI prints it with -o json, with a fixture.
func assertGolden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(testutil.FixturePath(name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Errorf("demo data drifted from %s:\n%s", name, got)
	}
}

func TestScan_MatchesGolden(t *testing.T) {
	result := Scan()

	if age := time.Since(result.ScannedAt); age < 0 || age > time.Minute {
		t.Errorf("ScannedAt = %s, want the current time", result.ScannedAt)
	}
	result.ScannedAt = time.Time{}
	assertGolden(t, "demo-scan.golden.json", result)
}
