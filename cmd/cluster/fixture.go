package main

import (
	"github.com/spf13/cobra"

	"capi-distro/internal/fixture"
	"capi-distro/internal/msg"
	"capi-distro/internal/snapshot"
)

// The fixture subcommands are for people working on this tool, not for people
// running clusters, so they are hidden from help.
func newFixtureCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "fixture",
		Short:  "Record, generate and inspect status fixtures",
		Hidden: true,
	}
	cmd.AddCommand(newFixtureRecordCmd(g), newFixtureSynthCmd(), newFixtureListCmd())
	return cmd
}

func newFixtureRecordCmd(g *globals) *cobra.Command {
	var name, out string
	var points []int
	cmd := &cobra.Command{
		Use:     "record",
		Short:   "Record a real run into testdata/fixtures/<name>/",
		Example: "  cluster fixture record --name std-docker-happy --cluster dev-1",
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := newLiveSource(g, name, false)
			if err != nil {
				return err
			}
			defer func() { _ = src.Close() }()
			snapshots, err := src.Snapshots(cmd.Context())
			if err != nil {
				return err
			}
			tl := fixture.Timeline{Name: name, Note: "recorded from a live management cluster"}
			for env := range snapshots {
				env.Meta.Scenario = name
				env.Meta.Synthetic = false
				tl.Envelopes = append(tl.Envelopes, env)
				cmd.Printf("recorded t+%ds (%d objects)\n", env.Meta.TPlusS, len(env.Objects))
				if len(tl.Envelopes) >= len(points) && len(points) > 0 {
					break
				}
			}
			return fixture.Write(out, tl)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "scenario name")
	cmd.Flags().StringVar(&out, "out", "testdata/fixtures", "fixtures directory")
	cmd.Flags().IntSliceVar(&points, "at", nil, "seconds after start to capture; empty records every change")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newFixtureSynthCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "synth",
		Short: "Regenerate the synthetic fixtures",
		Long: "Synthetic fixtures stand in for scenarios that have not been recorded yet.\n" +
			"Every envelope they write is marked _meta.synthetic with a note saying what it\n" +
			"stands in for; recording the same scenario replaces the directory and no\n" +
			"assertion changes.",
		Example: "  cluster fixture synth --out testdata/fixtures",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, tl := range fixture.All() {
				if err := fixture.Write(out, tl); err != nil {
					return err
				}
				cmd.Printf("%-22s %d envelopes\n", tl.Name, len(tl.Envelopes))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "testdata/fixtures", "fixtures directory")
	return cmd
}

func newFixtureListCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List recorded and synthetic fixtures with their provenance",
		Example: "  cluster fixture list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			scenarios, err := fixture.Scenarios(dir)
			if err != nil {
				return msg.Wrap(msg.ClusterNotFound, msg.Vars{Object: dir, Namespace: ""}, err)
			}
			for _, name := range scenarios {
				envelopes, err := fixture.Load(dir + "/" + name)
				if err != nil || len(envelopes) == 0 {
					continue
				}
				cmd.Printf("%-22s %2d envelopes  %s\n", name, len(envelopes), provenance(envelopes[0]))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "testdata/fixtures", "fixtures directory")
	return cmd
}

func provenance(env snapshot.Envelope) string {
	if env.Meta.Synthetic {
		return "synthetic"
	}
	if env.Meta.RecordedAt != "" {
		return "recorded " + env.Meta.RecordedAt
	}
	return "recorded"
}
