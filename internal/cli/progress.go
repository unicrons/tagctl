package cli

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/unicrons/tagctl/internal/log"
)

// clearLine erases the current terminal line and returns the cursor to column 0.
// It is only written to a terminal.
const clearLine = "\r\033[2K"

// Spinner provides a terminal spinner for indicating progress.
//
// It draws on stderr and registers itself with the log package while
// running, so log lines printed meanwhile land on their own line instead
// of colliding with the animation. When stderr is not a terminal it does not
// animate: the message is printed once, then the outcome.
type Spinner struct {
	out      io.Writer
	colors   palette
	animate  bool
	message  string
	frames   []string
	interval time.Duration
	done     chan bool
	mu       sync.Mutex
	running  bool
	frame    int
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(message string) *Spinner {
	return &Spinner{
		out:      os.Stderr,
		colors:   paletteFor(os.Stderr),
		animate:  log.IsTerminal(os.Stderr),
		message:  message,
		frames:   []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		interval: 80 * time.Millisecond,
	}
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	if !s.animate {
		fmt.Fprintln(s.out, s.message)
		s.mu.Unlock()
		return
	}
	s.done = make(chan bool)
	done := s.done
	s.mu.Unlock()

	log.SetLineHolder(s)

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		s.tick()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				s.tick()
			}
		}
	}()
}

// tick draws the current frame and advances to the next one.
func (s *Spinner) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.draw()
	s.frame = (s.frame + 1) % len(s.frames)
}

// draw renders the current frame. Caller holds s.mu.
func (s *Spinner) draw() {
	fmt.Fprintf(s.out, "%s%s%s %s%s", clearLine, s.colors.cyan, s.frames[s.frame], s.message, s.colors.reset)
}

// Suspend clears the spinner line, runs fn, and redraws the spinner.
func (s *Spinner) Suspend(fn func()) {
	// Lock order: called with log.mu held, so s.mu is always taken second; never log or call log.SetLineHolder under s.mu.
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.animate {
		fn()
		return
	}
	fmt.Fprint(s.out, clearLine)
	fn()
	if s.running {
		s.draw()
	}
}

// Update changes the spinner message.
func (s *Spinner) Update(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = message
}

// Stop halts the spinner and clears the line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	if !s.animate {
		s.mu.Unlock()
		return
	}
	close(s.done)
	s.mu.Unlock()

	log.SetLineHolder(nil)
	fmt.Fprint(s.out, clearLine)
}

// Success stops the spinner and shows a success message.
func (s *Spinner) Success(message string) {
	s.Stop()
	fmt.Fprintf(s.out, "%s✓%s %s\n", s.colors.green, s.colors.reset, message)
}

// Fail stops the spinner and shows a failure message.
func (s *Spinner) Fail(message string) {
	s.Stop()
	fmt.Fprintf(s.out, "%s✗%s %s\n", s.colors.red, s.colors.reset, message)
}
