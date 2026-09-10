package msg

import "errors"

// Exit codes. The contract is documented in docs/user-guide.md and asserted per
// scenario by the golden tests.
const (
	ExitReady    = 0
	ExitStalled  = 2
	ExitRejected = 3
	ExitEnv      = 4
)

// Error is the only error type this tool shows a user. It carries a code, the
// rendered one-line summary, and a next action.
//
// There is no way to build one without a next action: New is the only constructor
// and it takes the code, whose registry entry supplies the action. That is what
// "the compiler is the lint" means in D2.4 — a message with nothing to do next
// cannot be constructed, so no test has to look for one.
type Error struct {
	Code    Code
	Summary string
	Action  string
	// Cause is the underlying error, kept for --verbose and for errors.Is.
	Cause error
}

func (e *Error) Error() string { return e.Summary + " Next: " + e.Action }
func (e *Error) Unwrap() error { return e.Cause }

// ExitCode maps an error to the documented exit code.
func (e *Error) ExitCode() int {
	entry, ok := Get(e.Code)
	if !ok {
		return ExitEnv
	}
	switch entry.Class {
	case Denial:
		return ExitRejected
	case Stall:
		return ExitStalled
	default:
		return ExitEnv
	}
}

func New(code Code, v Vars) *Error {
	entry, ok := Get(code)
	if !ok {
		panic("msg: unknown code " + string(code))
	}
	return &Error{Code: code, Summary: Render(code, v), Action: entry.NextAction}
}

// Wrap attaches an underlying error for --verbose; the user-facing text is
// unchanged.
func Wrap(code Code, v Vars, cause error) *Error {
	e := New(code, v)
	e.Cause = cause
	return e
}

// ExitCodeOf returns the exit code for any error: 4 for anything that did not come
// from this package, since an unexpected error is an environment failure.
func ExitCodeOf(err error) int {
	if err == nil {
		return ExitReady
	}
	var e *Error
	if As(err, &e) {
		return e.ExitCode()
	}
	return ExitEnv
}

// As is errors.As, kept here so callers of this package need only one import.
func As(err error, target **Error) bool { return errors.As(err, target) }
