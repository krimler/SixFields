package main

import (
	"github.com/spf13/cobra"
)

// globals are the flags every command that shows status accepts. They live in one
// struct so `up`, `status` and `why` cannot drift apart.
type globals struct {
	namespace  string
	kubeconfig string
	noTTY      bool
	jsonOut    bool
	verbose    bool
	noColor    bool
	replay     string
	speed      float64
	stallAfter string
	timeout    string
}

func newRootCmd() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:   "cluster",
		Short: "Create and watch Cluster API clusters without reading the condition tree",
		Long: "cluster applies one Cluster and shows four phases while it comes up.\n" +
			"When a phase stops moving it names the one object that is blocking, and it\n" +
			"always prints the kubectl command behind what it is showing.",
		Example: "  cluster up dev-1 -f cluster.yaml\n" +
			"  cluster status dev-1\n" +
			"  cluster why dev-1\n" +
			"  cluster render dev-1 | kubectl apply --dry-run=server -f -",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&g.namespace, "namespace", "n", "default", "namespace of the Cluster")
	root.PersistentFlags().StringVar(&g.kubeconfig, "kubeconfig", "", "path to the management cluster kubeconfig")
	root.PersistentFlags().BoolVar(&g.verbose, "verbose", false, "show full condition text and the ranking")
	root.PersistentFlags().BoolVar(&g.noColor, "no-color", false, "never emit colour")

	root.AddCommand(
		newUpCmd(g),
		newStatusCmd(g),
		newWhyCmd(g),
		newRenderCmd(g),
		newPlanCmd(g),
		newNewCmd(g),
		newExplainCmd(),
		newDocsCmd(),
		newKubeconfigCmd(g),
		newDoctorCmd(g),
		newFixtureCmd(g),
		newSkillsCmd(),
		newVersionCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print the version of this binary",
		Example: "  cluster version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println(version)
			return nil
		},
	}
}
