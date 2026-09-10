// Command cluster is the stream: it applies a Cluster, then shows four phases,
// an estimate, and — when nothing is moving — the one object to look at.
package main

import (
	"errors"
	"fmt"
	"os"

	"capi-distro/internal/msg"
)

// version is set by the linker in `make build`.
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		var q quiet
		if errors.As(err, &q) {
			os.Exit(q.code)
		}
		var e *msg.Error
		if msg.As(err, &e) {
			fmt.Fprintln(os.Stderr, e.Summary)
			fmt.Fprintln(os.Stderr, "next: "+e.Action)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(msg.ExitCodeOf(err))
	}
}
