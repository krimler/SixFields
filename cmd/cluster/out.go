package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cobra's cmd.Print family writes to stderr, not stdout — it exists for usage and
// error text. Anything a user redirects into a file must go through these
// instead, or `cluster render > objects.yaml` and `cluster new > cluster.yaml`
// write empty files, which is exactly what they did until this file existed.
func outf(cmd *cobra.Command, format string, args ...any) {
	// A failed write to stdout is not something a user can act on, and returning
	// it would put an error path on every line of output.
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), format, args...)
}

func outln(cmd *cobra.Command, args ...any) {
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), args...)
}

func out(cmd *cobra.Command, s string) {
	_, _ = fmt.Fprint(cmd.OutOrStdout(), s)
}
