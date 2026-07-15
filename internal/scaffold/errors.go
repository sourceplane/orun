package scaffold

import "fmt"

// ExitError carries a process exit code out of the scaffold engine so the CLI
// can map failures to the design §10 exit-code matrix without the command layer
// re-classifying them: 1 = input-validation / gate failure, 6 = unknown
// blueprint/composition. The CLI's central mapper honors any error implementing
// ExitCode() int.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }
func (e *ExitError) ExitCode() int { return e.Code }

// gateErr wraps an output-gate failure (fail closed) with exit code 1.
func gateErr(format string, args ...any) error {
	return &ExitError{Code: 1, Err: fmt.Errorf(format, args...)}
}

// notFoundErr wraps an unknown-blueprint/composition failure with exit code 6.
func notFoundErr(format string, args ...any) error {
	return &ExitError{Code: 6, Err: fmt.Errorf(format, args...)}
}
