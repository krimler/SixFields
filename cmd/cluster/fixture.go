package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"capi-distro/internal/fixture"
	"capi-distro/internal/fold"
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
	var name, clusterName, out string
	var every time.Duration
	var maxPoints int
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record a real run into testdata/fixtures/<name>/",
		Long: "Watches a cluster and writes an envelope every --every until it is ready or\n" +
			"--max-points is reached. Timestamps are relative to the Cluster's creation, so a\n" +
			"recording replays at the speed it happened.",
		Example: "  cluster fixture record --name std-docker-happy --cluster dev-1\n" +
			"  cluster fixture record --name scale-up --cluster dev-1 --every 15s --max-points 20",
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := newLiveSource(g, clusterName, false)
			if err != nil {
				return err
			}
			defer func() { _ = src.Close() }()

			snapshots, err := src.Snapshots(cmd.Context())
			if err != nil {
				return err
			}

			tl := fixture.Timeline{
				Name: name,
				Note: fmt.Sprintf("recorded from cluster %s on %s", clusterName, time.Now().UTC().Format(time.RFC3339)),
			}
			var start time.Time
			var lastWrite time.Time

			for env := range snapshots {
				cluster, ok := env.Cluster()
				if !ok {
					continue
				}
				if start.IsZero() {
					start = cluster.CreationTimestamp()
				}
				now := time.Now()
				if !lastWrite.IsZero() && now.Sub(lastWrite) < every {
					continue
				}
				lastWrite = now

				env.Meta.Scenario = name
				env.Meta.Synthetic = false
				env.Meta.CAPI = capiVersion(env)
				env.Meta.RecordedAt = now.UTC().Format(time.RFC3339)
				env.Meta.TPlusS = int(now.Sub(start).Round(time.Second).Seconds())
				tl.Envelopes = append(tl.Envelopes, env)
				outf(cmd, "t+%ds  %d objects\n", env.Meta.TPlusS, len(env.Objects))

				res := fold.Fold(env, fold.Options{Now: now})
				if res.Ready || len(tl.Envelopes) >= maxPoints {
					break
				}
			}
			if len(tl.Envelopes) == 0 {
				return msg.New(msg.ClusterNotFound, msg.Vars{Object: clusterName, Namespace: g.namespace})
			}
			if err := fixture.Write(out, tl); err != nil {
				return err
			}
			outf(cmd, "wrote %d envelopes to %s/%s\n", len(tl.Envelopes), out, name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "scenario name; becomes the directory")
	cmd.Flags().StringVar(&clusterName, "cluster", "", "the cluster to record")
	cmd.Flags().StringVar(&out, "out", "testdata/fixtures", "fixtures directory")
	cmd.Flags().DurationVar(&every, "every", 30*time.Second, "minimum interval between envelopes")
	cmd.Flags().IntVar(&maxPoints, "max-points", 30, "stop after this many envelopes")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("cluster")
	return cmd
}

// capiVersion records which release a fixture was taken from, so a fold that
// changes with a CAPI bump can be traced to the recording that predates it.
func capiVersion(env snapshot.Envelope) string {
	for _, o := range env.Objects {
		if o.Kind() == "Cluster" {
			return o.APIVersion()
		}
	}
	return ""
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
				// A recording outranks the stand-in that was written for it. This
				// is the whole relationship between the two: synth fills a gap and
				// steps aside as soon as a real run fills it.
				if fixture.IsRecorded(filepath.Join(out, tl.Name)) {
					outf(cmd, "%-22s skipped, already recorded\n", tl.Name)
					continue
				}
				if err := fixture.Write(out, tl); err != nil {
					return err
				}
				outf(cmd, "%-22s %d envelopes\n", tl.Name, len(tl.Envelopes))
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
				outf(cmd, "%-22s %2d envelopes  %s\n", name, len(envelopes), provenance(envelopes[0]))
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
