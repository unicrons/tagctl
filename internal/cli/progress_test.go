package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/log"
)

func TestSpinnerSuspendKeepsLogLineIntact(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.LevelInfo)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.LevelError)
	})

	s := NewSpinner("working")
	s.out = &buf
	s.interval = time.Hour // keep the ticker out of the way
	s.Start()
	log.Info("hello")
	s.Stop()

	out := buf.String()
	idx := strings.Index(out, "hello")
	if idx < 0 {
		t.Fatalf("log line missing from %q", out)
	}
	// The log line must start on a cleared line and be followed by a redraw.
	if !strings.HasSuffix(out[:idx], "[INFO] \033[0m ") || !strings.HasPrefix(out[:idx], clearLine) {
		t.Errorf("log line not written on a cleared line: %q", out)
	}
	if !strings.Contains(out[idx:], "working") {
		t.Errorf("spinner not redrawn after log line: %q", out)
	}
}

func TestSpinnerStopReleasesLogLine(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.LevelInfo)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.LevelError)
	})

	s := NewSpinner("working")
	s.out = &buf
	s.interval = time.Hour
	s.Start()
	s.Success("done")
	buf.Reset()

	log.Info("after")
	if strings.Contains(buf.String(), "working") {
		t.Errorf("spinner still redrawn after Stop: %q", buf.String())
	}
}
