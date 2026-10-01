// Package log provides a simple leveled logger for tagctl.
//
// Everything is written to stderr so stdout stays reserved for command
// output and can be piped safely.
package log

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/term"
)

// Level represents a log level.
type Level int

const (
	// LevelError only shows error messages.
	LevelError Level = iota
	// LevelInfo shows info and error messages.
	LevelInfo
	// LevelDebug shows debug, info, and error messages.
	LevelDebug
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorCyan    = "\033[36m"
	colorMagenta = "\033[35m"
)

// isTerminal reports whether w is a terminal; tests replace it.
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// IsTerminal reports whether w is a terminal.
func IsTerminal(w io.Writer) bool {
	return isTerminal(w)
}

// UseColor reports whether ANSI colour may be written to w: only to a
// terminal, and never when NO_COLOR is set to a non-empty value.
func UseColor(w io.Writer) bool {
	return os.Getenv("NO_COLOR") == "" && isTerminal(w)
}

// LineHolder is a terminal element that owns the current line, such as a
// spinner. Suspend clears the line, runs fn, then redraws the element.
type LineHolder interface {
	Suspend(fn func())
}

var (
	mu           sync.Mutex
	currentLevel           = LevelError
	out          io.Writer = os.Stderr
	holder       LineHolder
)

// SetLevel sets the global log level.
func SetLevel(level Level) {
	mu.Lock()
	defer mu.Unlock()
	currentLevel = level
}

// SetLevelFromString sets the log level from a string (error, info, debug).
// An unknown level is an error and leaves the current level untouched.
func SetLevelFromString(level string) error {
	switch strings.ToLower(level) {
	case "error":
		SetLevel(LevelError)
	case "info":
		SetLevel(LevelInfo)
	case "debug":
		SetLevel(LevelDebug)
	default:
		return fmt.Errorf("invalid log level %q (valid: error, info, debug)", level)
	}
	return nil
}

// GetLevel returns the current log level.
func GetLevel() Level {
	mu.Lock()
	defer mu.Unlock()
	return currentLevel
}

// SetOutput redirects log output. Defaults to stderr.
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	out = w
}

// SetLineHolder registers the element that owns the terminal line, or nil to
// release it. While one is set, every log line is written through its Suspend.
func SetLineHolder(h LineHolder) {
	mu.Lock()
	defer mu.Unlock()
	holder = h
}

// Error logs an error message (always shown).
func Error(format string, args ...interface{}) {
	write(LevelError, colorRed, "[ERROR]", format, args...)
}

// Info logs an info message (shown at INFO and DEBUG levels).
func Info(format string, args ...interface{}) {
	write(LevelInfo, colorCyan, "[INFO] ", format, args...)
}

// Debug logs a debug message (shown only at DEBUG level).
func Debug(format string, args ...interface{}) {
	write(LevelDebug, colorMagenta, "[DEBUG]", format, args...)
}

func write(level Level, color, tag, format string, args ...interface{}) {
	// Lock order: log.mu, then the holder's own lock inside Suspend. A holder must never log while holding its lock.
	mu.Lock()
	defer mu.Unlock()

	if currentLevel < level {
		return
	}

	if UseColor(out) {
		tag = color + tag + colorReset
	}
	line := fmt.Sprintf("%s %s\n", tag, printable(fmt.Sprintf(format, args...)))
	emit := func() { fmt.Fprint(out, line) }

	if holder != nil {
		holder.Suspend(emit)
		return
	}
	emit()
}

// printable replaces control characters, so a logged cloud value or error
// cannot drive the terminal through escape sequences. Line breaks and tabs stay.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return '?'
		}
		return r
	}, s)
}
