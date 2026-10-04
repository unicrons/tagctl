package cli

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
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
	s.animate = true
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
	if !strings.HasSuffix(out[:idx], "[INFO]  ") || !strings.HasPrefix(out[:idx], clearLine) {
		t.Errorf("log line not written on a cleared line: %q", out)
	}
	if !strings.Contains(out[idx:], "working") {
		t.Errorf("spinner not redrawn after log line: %q", out)
	}
}

// lockedBuffer is a writer the spinner and the logger can share across goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSpinner_LoggingWhileItRunsNeitherRacesNorDeadlocks(t *testing.T) {
	const loggers, lines = 8, 100

	out := &lockedBuffer{}
	log.SetOutput(out)
	log.SetLevel(log.LevelInfo)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.LevelError)
	})

	finished := make(chan struct{})
	go func() {
		defer close(finished)

		// Several start/stop cycles, as a scan runs one spinner per phase.
		var wg sync.WaitGroup
		for g := range loggers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for n := range lines {
					log.Info("logger %d line %d end", g, n)
				}
			}()
		}
		for cycle := range 20 {
			s := &Spinner{out: out, animate: true, message: "working", frames: []string{"-", "+"}, interval: time.Millisecond}
			s.Start()
			s.Update(fmt.Sprintf("cycle %d", cycle))
			time.Sleep(2 * time.Millisecond)
			s.Success("done")
		}
		wg.Wait()
	}()

	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("logging while a spinner runs deadlocked")
	}

	got := out.String()
	for g := range loggers {
		for n := range lines {
			if want := fmt.Sprintf("[INFO]  logger %d line %d end\n", g, n); !strings.Contains(got, want) {
				t.Fatalf("log line %q was lost or torn by the spinner", want)
			}
		}
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
	s.animate = true
	s.interval = time.Hour
	s.Start()
	s.Success("done")
	buf.Reset()

	log.Info("after")
	if strings.Contains(buf.String(), "working") {
		t.Errorf("spinner still redrawn after Stop: %q", buf.String())
	}
}

func TestProgressBar_RendersWithoutWorkOrOutOfRange(t *testing.T) {
	tests := []struct {
		name           string
		total, current int
		want           string
	}{
		{name: "nothing to do", total: 0, current: 0, want: "0% "},
		{name: "advanced with nothing to do", total: 0, current: 3, want: "0% "},
		{name: "negative total", total: -1, current: 1, want: "0% "},
		{name: "negative progress", total: 4, current: -2, want: "0% "},
		{name: "half way", total: 4, current: 2, want: "50% "},
		{name: "past the end", total: 4, current: 9, want: "100% "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			bar := &ProgressBar{out: &buf, total: tt.total, width: 10}
			bar.SetCurrent(tt.current)

			out := stripANSI(buf.String())
			if !strings.Contains(out, "] "+tt.want) {
				t.Errorf("rendered %q, want it to show %q", out, tt.want)
			}
			if got := strings.Count(out, "█") + strings.Count(out, "░"); got != 10 {
				t.Errorf("bar is %d cells wide, want 10: %q", got, out)
			}
		})
	}
}

// stripANSI removes SGR colour sequences.
func stripANSI(s string) string {
	return regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(s, "")
}
