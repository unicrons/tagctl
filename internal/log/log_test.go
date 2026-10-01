package log

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capture(t *testing.T, level Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevLevel := GetLevel()
	SetOutput(&buf)
	SetLevel(level)
	t.Cleanup(func() {
		SetOutput(os.Stderr)
		SetLevel(prevLevel)
		SetLineHolder(nil)
	})
	return &buf
}

func TestLevelFiltering(t *testing.T) {
	cases := []struct {
		level          Level
		wantInfo, want bool
	}{
		{LevelError, false, false},
		{LevelInfo, true, false},
		{LevelDebug, true, true},
	}
	for _, tc := range cases {
		buf := capture(t, tc.level)
		Error("e")
		Info("i")
		Debug("d")
		out := buf.String()
		if !strings.Contains(out, "[ERROR] e\n") {
			t.Errorf("level %d: error line missing", tc.level)
		}
		if got := strings.Contains(out, "[INFO]"); got != tc.wantInfo {
			t.Errorf("level %d: info shown=%v, want %v", tc.level, got, tc.wantInfo)
		}
		if got := strings.Contains(out, "[DEBUG]"); got != tc.want {
			t.Errorf("level %d: debug shown=%v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestSetLevelFromString(t *testing.T) {
	capture(t, LevelError)
	for in, want := range map[string]Level{"error": LevelError, "INFO": LevelInfo, "Debug": LevelDebug} {
		if err := SetLevelFromString(in); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if GetLevel() != want {
			t.Errorf("%q: got %d, want %d", in, GetLevel(), want)
		}
	}
}

func TestSetLevelFromString_InvalidKeepsTheLevel(t *testing.T) {
	capture(t, LevelDebug)

	err := SetLevelFromString("loud")
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
	for _, want := range []string{`"loud"`, "error, info, debug"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
	if GetLevel() != LevelDebug {
		t.Errorf("level = %d after an invalid value, want it unchanged", GetLevel())
	}
}

func TestWrite_NeutralisesControlCharacters(t *testing.T) {
	buf := capture(t, LevelError)

	Error("skipped %s:\n\tsecond line", "bucket\x1b[2J\x07\r")

	want := "[ERROR] skipped bucket?[2J??:\n\tsecond line\n"
	if got := buf.String(); !strings.HasSuffix(got, want) {
		t.Errorf("got %q, want it to end with %q", got, want)
	}
}

func stubTerminal(t *testing.T, terminal bool) {
	t.Helper()
	original := isTerminal
	isTerminal = func(io.Writer) bool { return terminal }
	t.Cleanup(func() { isTerminal = original })
}

func TestUseColor(t *testing.T) {
	tests := []struct {
		name     string
		noColor  string
		terminal bool
		want     bool
	}{
		{name: "terminal", terminal: true, want: true},
		{name: "not a terminal", terminal: false, want: false},
		{name: "terminal with NO_COLOR=1", noColor: "1", terminal: true, want: false},
		{name: "terminal with NO_COLOR=0", noColor: "0", terminal: true, want: false},
		{name: "terminal with NO_COLOR=false", noColor: "false", terminal: true, want: false},
		{name: "not a terminal with NO_COLOR=1", noColor: "1", terminal: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			stubTerminal(t, tt.terminal)

			if got := UseColor(os.Stderr); got != tt.want {
				t.Errorf("UseColor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUseColor_FilesAndBuffersAreNotTerminals(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	if UseColor(file) || IsTerminal(file) {
		t.Error("a regular file was treated as a terminal")
	}
	if UseColor(&bytes.Buffer{}) || IsTerminal(&bytes.Buffer{}) {
		t.Error("a buffer was treated as a terminal")
	}
}

func TestWrite_ColoursTheTagOnlyWhenColourIsOn(t *testing.T) {
	tests := []struct {
		name, noColor string
		terminal      bool
		want          string
	}{
		{name: "terminal", terminal: true, want: "\033[31m[ERROR]\033[0m e\n"},
		{name: "terminal with NO_COLOR", noColor: "1", terminal: true, want: "[ERROR] e\n"},
		{name: "redirected", terminal: false, want: "[ERROR] e\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			stubTerminal(t, tt.terminal)
			buf := capture(t, LevelError)

			Error("e")

			if got := buf.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

type recordingHolder struct {
	buf   *bytes.Buffer
	calls int
}

func (h *recordingHolder) Suspend(fn func()) {
	h.calls++
	h.buf.WriteString("<clear>")
	fn()
	h.buf.WriteString("<redraw>")
}

func TestLineHolderWrapsEachLine(t *testing.T) {
	buf := capture(t, LevelInfo)
	h := &recordingHolder{buf: buf}
	SetLineHolder(h)

	Info("hello")
	Debug("hidden")

	if h.calls != 1 {
		t.Fatalf("Suspend called %d times, want 1 (filtered lines must not suspend)", h.calls)
	}
	want := "<clear>[INFO]  hello\n<redraw>"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	SetLineHolder(nil)
	Info("after")
	if h.calls != 1 {
		t.Error("Suspend called after holder was released")
	}
}
