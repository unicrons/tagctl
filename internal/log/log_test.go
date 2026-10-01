package log

import (
	"bytes"
	"os"
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
		if !strings.Contains(out, "[ERROR]\033[0m e\n") {
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
	want := "<clear>\033[36m[INFO] \033[0m hello\n<redraw>"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	SetLineHolder(nil)
	Info("after")
	if h.calls != 1 {
		t.Error("Suspend called after holder was released")
	}
}
