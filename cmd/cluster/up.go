package main

import (
	"os"

	"github.com/spf13/cobra"

	"sixfields/internal/gen"
	"sixfields/internal/msg"
)

func newUpCmd(g *globals) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "up NAME",
		Short: "Apply a Cluster and wait, showing four phases while it comes up",
		Long: "up applies the file, then blocks until the cluster is ready. It is safe to\n" +
			"Ctrl-C: `cluster status NAME` picks the same view back up.\n\n" +
			"Exit codes: 0 ready, 2 still running or stalled when the wait ended,\n" +
			"3 rejected at admission, 4 environment.",
		Example: "  cluster up dev-1 -f cluster.yaml\n" +
			"  cluster up dev-1 -f cluster.yaml --no-tty --timeout 20m",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "the Cluster to apply")
	cmd.Flags().BoolVar(&g.noTTY, "no-tty", false, "one line per state change, for logs and CI")
	cmd.Flags().BoolVar(&g.jsonOut, "json", false, "emit the versioned status document")
	cmd.Flags().StringVar(&g.timeout, "timeout", "", "give up waiting after this long")
	cmd.Flags().StringVar(&g.stallAfter, "stall-after", "3m", "how long without a change before a phase is called stalled")
	cmd.Flags().StringVar(&g.replay, "replay", "", "replay a recorded fixture directory in place of a cluster")
	cmd.Flags().Float64Var(&g.speed, "speed", 1, "replay speed multiplier")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		// Validating before applying is the point: an error the policy would raise
		// at admission is raised here, against the file the user just edited.
		if file != "" {
			b, err := os.ReadFile(file)
			if err != nil {
				return msg.Wrap(msg.ClusterNotFound, msg.Vars{Object: file, Namespace: g.namespace}, err)
			}
			spec, errs := gen.FromYAML(b)
			if len(errs) > 0 {
				for _, e := range errs {
					cmd.PrintErrln(e.Summary)
					cmd.PrintErrln("  next: " + e.Action)
				}
				return exitWith(errs[0])
			}
			// The class name is the one field admission cannot check, because a
			// policy sees only the object being written. Unchecked, a typo is
			// accepted with a warning and the cluster then creates nothing.
			if err := checkClass(cmd, g, gen.ClassFor(spec.Class, spec.Placement)); err != nil {
				return err
			}
			if err := apply(cmd, g, b); err != nil {
				return err
			}
		}

		opt, src, err := setup(cmd, g, args, true)
		if err != nil {
			return err
		}
		return run(cmd.Context(), src, opt)
	}
	return cmd
}
