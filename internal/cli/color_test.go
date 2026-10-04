package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

const escape = "\x1b"

func TestPaletteFor_NoColourOffATerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	if got := paletteFor(&bytes.Buffer{}); got != (palette{}) {
		t.Errorf("paletteFor(buffer) = %+v, want no codes", got)
	}
}

func TestSpinner_OffATerminalPrintsPlainLines(t *testing.T) {
	var buf bytes.Buffer
	s := &Spinner{out: &buf, message: "working", frames: []string{"-"}, interval: time.Hour}

	s.Start()
	s.Start()
	s.Update("still working")
	s.Suspend(func() { buf.WriteString("log line\n") })
	s.Success("done")
	s.Stop()

	if got, want := buf.String(), "working\nlog line\n✓ done\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}

	buf.Reset()
	s.Start()
	s.Fail("broken")
	if got, want := buf.String(), "still working\n✗ broken\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestSpinner_OnATerminalHonoursThePalette(t *testing.T) {
	tests := []struct {
		name   string
		colors palette
		want   string
	}{
		{name: "colour", colors: ansiPalette, want: clearLine + "\033[36m- working\033[0m" + clearLine + "\033[32m✓\033[0m done\n"},
		{name: "no colour", colors: palette{}, want: clearLine + "- working" + clearLine + "✓ done\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := &Spinner{out: &buf, colors: tt.colors, animate: true, message: "working", frames: []string{"-"}, interval: time.Hour}

			s.Start()
			// The first frame is drawn by the spinner goroutine.
			deadline := time.Now().Add(5 * time.Second)
			for {
				s.mu.Lock()
				drawn := buf.Len() > 0
				s.mu.Unlock()
				if drawn || time.Now().After(deadline) {
					break
				}
				time.Sleep(time.Millisecond)
			}
			s.Success("done")

			if got := buf.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintFindingsTable_NoColourOffATerminal(t *testing.T) {
	var buf bytes.Buffer
	printFindingsTable(&buf, []types.Finding{
		{Resource: types.Resource{ID: "i-1", Type: "aws_instance"}, Tag: "owner", Status: types.StatusFailed},
		{Resource: types.Resource{ID: "i-2", Type: "aws_instance"}, Tag: "owner", Status: types.StatusPass, Actual: "a@b.c"},
	})

	if strings.Contains(buf.String(), escape) {
		t.Errorf("table written to a buffer carries ANSI codes: %q", buf.String())
	}
}

func TestExecute_RedirectedOutputHasNoEscapeCodes(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: owner\n")
	scan := writeScan(t, dir, "scan.json", failedScan(0))
	account := writeFixture(t, dir, "account.yaml",
		"clouds:\n  aws:\n    - profile: default\n      regions: [us-east-1]\npolicy:\n  required:\n    - name: owner\n")

	tests := []struct {
		name string
		args []string
	}{
		{name: "scan", args: []string{"-c", policy, "scan", "--mock", "--verbose"}},
		{name: "scan demo mode", args: []string{"-c", policy, "scan"}},
		{name: "plan", args: []string{"-c", policy, "plan"}},
		{name: "diff", args: []string{"-c", policy, "diff", scan, scan}},
		{name: "validate", args: []string{"-c", account, "validate"}},
		{name: "init", args: []string{"init", "--name", filepath.Join(dir, "new.yaml")}},
		{name: "version", args: []string{"version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, tt.args...)
			if run.err != nil {
				t.Fatalf("tagctl %v error = %v", tt.args, run.err)
			}
			if run.stdout+run.stderr == "" {
				t.Fatal("command printed nothing")
			}
			if strings.Contains(run.stdout, escape) {
				t.Errorf("stdout carries ANSI codes:\n%q", run.stdout)
			}
			if strings.Contains(run.stderr, escape) {
				t.Errorf("stderr carries ANSI codes:\n%q", run.stderr)
			}
		})
	}
}
