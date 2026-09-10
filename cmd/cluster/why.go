package main

import (
	"github.com/spf13/cobra"

	"capi-distro/internal/fold"
	"capi-distro/internal/msg"
	"capi-distro/internal/render"
	"capi-distro/internal/why"
)

func newWhyCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "why NAME",
		Short: "Name the one object that is blocking, and the command to read it",
		Long: "why prints the stall line and nothing else: which object, what is wrong, how\n" +
			"long it has been that way, and the kubectl command behind it.",
		Example: "  cluster why dev-1\n" +
			"  cluster why dev-1 --explain-ranking\n" +
			"  cluster why --replay testdata/fixtures/inmem-stall-etcd",
		Args: cobra.MaximumNArgs(1),
	}
	var explainRanking bool
	ai := aiOptions{}
	cmd.Flags().BoolVar(&explainRanking, "explain-ranking", false, "show why each candidate lost")
	cmd.Flags().BoolVar(&ai.enabled, "explain", false, "print the runbook, then have a model explain the analyzer's finding")
	cmd.Flags().StringVar(&ai.anonymize, "anonymize", "", "on or off; defaults to on for a remote model and off for a local one")
	cmd.Flags().BoolVar(&g.jsonOut, "json", false, "emit the stall as JSON")
	cmd.Flags().StringVar(&g.replay, "replay", "", "replay a recorded fixture directory instead of a cluster")
	cmd.Flags().StringVar(&g.stallAfter, "stall-after", "3m", "how long without a change before a phase is called stalled")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		g.speed = 0 // a replay for `why` jumps to the end; there is nothing to watch
		opt, src, err := setup(cmd, g, args, false)
		if err != nil {
			return err
		}
		defer src.Close()

		env, err := lastSnapshot(cmd.Context(), src)
		if err != nil {
			return err
		}
		now := opt.now()
		res := fold.Fold(env, fold.Options{Now: now, StallAfter: opt.stallAfter})
		stall, ok := why.Rank(env, res, why.Options{Now: now})
		if !ok {
			cmd.Printf("%s is not waiting on anything.\n", res.Cluster)
			return nil
		}

		view := render.View{Result: res, Stall: &stall, Verbose: g.verbose || explainRanking,
			Estimates: estimates(res, opt), Elapsed: res.Elapsed}
		if g.jsonOut {
			out, _ := (&render.JSON{}).Render(view, 0)
			cmd.Print(out)
		} else {
			printStall(cmd, view)
			explainStall(cmd.Context(), cmd, stall, view, env, ai)
		}
		if _, stalled := res.Stalled(); stalled {
			return exitWith(msg.New(stall.Code, msg.Vars{Object: stall.Object.String(), Since: ""}))
		}
		return nil
	}
	return cmd
}

// printStall writes the stall block on its own, which is all `why` is.
func printStall(cmd *cobra.Command, v render.View) {
	theme := render.Theme{Color: useColor(&globals{}, cmd.OutOrStdout())}
	for _, line := range render.StallLines(v, theme) {
		cmd.Println(line)
	}
}
