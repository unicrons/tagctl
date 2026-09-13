package cli

import "errors"

const (
	exitOK         = 0
	exitGateFailed = 1
	exitError      = 2
)

// gateError marks an error as a failed policy gate.
type gateError struct {
	err error
}

func (e *gateError) Error() string { return e.err.Error() }

func (e *gateError) Unwrap() error { return e.err }

// gateFailed marks err as a failed policy gate; any unmarked error exits 2.
func gateFailed(err error) error {
	return &gateError{err: err}
}

// ExitCode maps the error Execute returned to the process exit code: 0 on
// success, 1 when a policy gate failed, 2 for any other error.
func ExitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var gate *gateError
	if errors.As(err, &gate) {
		return exitGateFailed
	}
	return exitError
}
