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
	fmt.Fprintf(cmd.OutOrStdout(), format, args...)
}

func outln(cmd *cobra.Command, args ...any) {
	fmt.Fprintln(cmd.OutOrStdout(), args...)
}

func out(cmd *cobra.Command, s string) {
	fmt.Fprint(cmd.OutOrStdout(), s)
}
