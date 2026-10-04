package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

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

// ProgressBar provides a terminal progress bar.
type ProgressBar struct {
	out       io.Writer
	total     int
	current   int
	message   string
	width     int
	startTime time.Time
	mu        sync.Mutex
}

// NewProgressBar creates a new progress bar.
func NewProgressBar(total int, message string) *ProgressBar {
	width := 40
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 60 {
		width = w - 40 // Leave room for message and percentage
		if width > 60 {
			width = 60
		}
	}

	return &ProgressBar{
		out:       os.Stderr,
		total:     total,
		message:   message,
		width:     width,
		startTime: time.Now(),
	}
}

// Increment advances the progress bar by one.
func (p *ProgressBar) Increment() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current++
	p.render()
}

// IncrementBy advances the progress bar by n.
func (p *ProgressBar) IncrementBy(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current += n
	p.render()
}

// SetCurrent sets the current progress value.
func (p *ProgressBar) SetCurrent(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = n
	p.render()
}

// SetMessage updates the progress bar message.
func (p *ProgressBar) SetMessage(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.message = message
	p.render()
}

// percent is the completed share, 0 to 100. A bar with nothing to do is at 0.
func (p *ProgressBar) percent() float64 {
	if p.total <= 0 {
		return 0
	}
	return min(max(float64(p.current)/float64(p.total)*100, 0), 100)
}

// render draws the progress bar to the terminal.
func (p *ProgressBar) render() {
	percent := p.percent()

	filled := int(float64(p.width) * percent / 100)
	empty := p.width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	c := paletteFor(p.out)

	fmt.Fprintf(p.out, "\r%s%s%s %s[%s]%s %s%.0f%%%s (%d/%d)",
		c.cyan, p.message, c.reset,
		c.dim, bar, c.reset,
		c.bold, percent, c.reset,
		p.current, p.total)
}

// Finish completes the progress bar with a newline.
func (p *ProgressBar) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = p.total
	p.render()
	fmt.Fprintln(p.out)
}

// Clear removes the progress bar from the terminal.
func (p *ProgressBar) Clear() {
	fmt.Fprintf(p.out, "\r%s\r", strings.Repeat(" ", 100))
}

// StatusLine provides a simple status line that can be updated.
type StatusLine struct {
	mu      sync.Mutex
	lastLen int
}

// NewStatusLine creates a new status line.
func NewStatusLine() *StatusLine {
	return &StatusLine{}
}

// Update displays a new status message, clearing the previous one.
func (s *StatusLine) Update(format string, args ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	message := fmt.Sprintf(format, args...)

	// Clear previous line
	if s.lastLen > 0 {
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", s.lastLen))
	}

	c := paletteFor(os.Stderr)
	fmt.Fprintf(os.Stderr, "\r%s%s%s", c.cyan, message, c.reset)
	s.lastLen = len(message) + 10 // Account for color codes
}

// Done clears the status line and prints a final message.
func (s *StatusLine) Done(format string, args ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear previous line
	if s.lastLen > 0 {
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", s.lastLen))
	}

	message := fmt.Sprintf(format, args...)
	c := paletteFor(os.Stderr)
	fmt.Fprintf(os.Stderr, "\r%s✓%s %s\n", c.green, c.reset, message)
	s.lastLen = 0
}

// Clear removes the status line.
func (s *StatusLine) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastLen > 0 {
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", s.lastLen))
	}
	s.lastLen = 0
}
