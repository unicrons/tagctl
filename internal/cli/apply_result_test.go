package cli

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

// captureStdout returns what fn printed to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureStream(t, &os.Stdout, fn)
}

// captureStderr returns what fn printed to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	return captureStream(t, &os.Stderr, fn)
}

func captureStream(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := *stream
	*stream = w

	// Reading while fn runs keeps a large output from filling the pipe.
	out := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		out <- string(data)
	}()

	func() {
		defer func() {
			*stream = original
			_ = w.Close()
		}()
		fn()
	}()

	printed := <-out
	_ = r.Close()
	return printed
}

func TestPrintApplyResult_SuccessReportsCountsWithoutFailureSection(t *testing.T) {
	result := &engine.ApplyResult{TotalChanges: 3, SuccessCount: 3, Duration: 1234567 * time.Microsecond}

	out := captureStdout(t, func() { printApplyResult(result) })

	for _, want := range []string{
		"Applied successfully: 3 changes\n",
		"Errors: 0\n",
		"Duration: 1.235s\n",
		"Run 'tagctl scan' to verify compliance.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Failed changes:") {
		t.Errorf("output lists failed changes for a clean apply:\n%s", out)
	}
}

func TestPrintApplyResult_ListsEveryFailedChange(t *testing.T) {
	result := &engine.ApplyResult{
		TotalChanges: 3,
		SuccessCount: 1,
		ErrorCount:   2,
		Errors: []engine.ApplyError{
			{
				Change: types.TagChange{Resource: types.Resource{Type: "aws_instance", ID: "i-1"}, Tag: "owner"},
				Error:  "AccessDenied",
			},
			{
				Change: types.TagChange{Resource: types.Resource{Type: "aws_s3_bucket", ID: "logs"}, Tag: "environment"},
				Error:  "throttled",
			},
		},
	}

	out := captureStdout(t, func() { printApplyResult(result) })

	for _, want := range []string{
		"Applied successfully: 1 changes\n",
		"Errors: 2\n",
		"Failed changes:\n",
		"  • aws_instance.i-1: owner - AccessDenied\n",
		"  • aws_s3_bucket.logs: environment - throttled\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
}
