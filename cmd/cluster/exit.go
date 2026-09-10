package main

import "capi-distro/internal/msg"

// quiet carries an exit code for a command that has already said everything it
// has to say. Without it the stall block would be printed twice: once by the
// renderer and once by the top-level error handler.
type quiet struct{ code int }

func (q quiet) Error() string { return "" }

// exitWith returns a quiet error for the exit code that matches this error's
// class, keeping the documented contract (0 ready, 2 stalled, 3 rejected,
// 4 environment) without repeating the message.
func exitWith(err error) error {
	if err == nil {
		return nil
	}
	return quiet{code: msg.ExitCodeOf(err)}
}
