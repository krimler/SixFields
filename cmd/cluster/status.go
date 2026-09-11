package main

import (
	"time"

	"github.com/spf13/cobra"

	"sixfields/internal/eta"
	"sixfields/internal/fold"
)

func newStatusCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status NAME",
		Short: "Show the four phases of a cluster without changing anything",
		Long: "status is `up` without the apply and without the wait: it prints the current\n" +
			"phases once and exits. --watch keeps it open.",
		Example: "  cluster status dev-1\n" +
			"  cluster status dev-1 --json | jq .status.phases\n" +
			"  cluster status --replay testdata/fixtures/inmem-stall-etcd --speed 20",
		Args: cobra.MaximumNArgs(1),
	}
	var watch bool
	cmd.Flags().BoolVar(&watch, "watch", false, "keep watching until the cluster is ready")
	cmd.Flags().BoolVar(&g.noTTY, "no-tty", false, "one line per state change, for logs and CI")
	cmd.Flags().BoolVar(&g.jsonOut, "json", false, "emit the versioned status document")
	cmd.Flags().StringVar(&g.replay, "replay", "", "replay a recorded fixture directory instead of a cluster")
	cmd.Flags().Float64Var(&g.speed, "speed", 1, "replay speed multiplier")
	cmd.Flags().StringVar(&g.stallAfter, "stall-after", "3m", "how long without a change before a phase is called stalled")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		opt, src, err := setup(cmd, g, args, watch)
		if err != nil {
			return err
		}
		return run(cmd.Context(), src, opt)
	}
	return cmd
}

// setup builds the source and options shared by status, up and why.
func setup(cmd *cobra.Command, g *globals, args []string, blocking bool) (streamOptions, source, error) {
	stallAfter, err := parseDuration(g.stallAfter, fold.DefaultStallAfter)
	if err != nil {
		return streamOptions{}, nil, err
	}
	timeout, err := parseDuration(g.timeout, 0)
	if err != nil {
		return streamOptions{}, nil, err
	}

	clock := time.Now()
	opt := streamOptions{
		out:        cmd.OutOrStdout(),
		globals:    g,
		stallAfter: stallAfter,
		timeout:    timeout,
		blocking:   blocking,
		history:    eta.NewFileStore(""),
		provider:   "docker",
		now:        func() time.Time { return clock },
	}

	if g.replay != "" {
		return opt, newReplaySource(g.replay, g.speed, &clock), nil
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	src, err := newLiveSource(g, name, !blocking)
	if err != nil {
		return streamOptions{}, nil, err
	}
	opt.now = time.Now
	return opt, src, nil
}
