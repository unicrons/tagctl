package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestConfirmApply(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "yes", input: "y\n", want: true},
		{name: "yes in capitals with spaces", input: " YES \n", want: true},
		{name: "no", input: "n\n", want: false},
		{name: "empty answer", input: "\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := confirmApply(context.Background(), strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("confirmApply() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("confirmApply(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestConfirmApply_CancelledAtPromptReturnsWithoutInput(t *testing.T) {
	stdin, keyboard := io.Pipe()
	t.Cleanup(func() { _ = keyboard.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	confirmed, err := confirmApply(ctx, stdin)

	if confirmed || !errors.Is(err, context.Canceled) {
		t.Fatalf("confirmApply() = %v, %v; want false and a context.Canceled error", confirmed, err)
	}
	if code := ExitCode(err); code != exitError {
		t.Errorf("ExitCode(%v) = %d, want %d", err, code, exitError)
	}
}
