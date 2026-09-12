package main

import (
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"sixfields/internal/snapshot"
	"sixfields/internal/watch"
)

func newRenderCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render NAME",
		Short: "Print every CAPI object behind a cluster",
		Long: "render is the escape hatch. It prints the objects the class produced, plus\n" +
			"the ClusterClass and the templates they came from, so the file stands on its\n" +
			"own: read it, diff it, or take it and leave.\n\n" +
			"Check it against what is running with `cluster render NAME > f.yaml &&\n" +
			"kubectl diff -f f.yaml`, which exits 0 when they match. Use diff and not\n" +
			"`apply --dry-run=server`: most of these are kinds the policy manages, so a\n" +
			"dry-run apply is denied for them and tells you nothing about the file.\n\n" +
			"It does not print the cluster's Secrets. It says on stderr which ones it\n" +
			"skipped and how to fetch them.",
		Example: "  cluster render dev-1\n" +
			"  cluster render dev-1 > dev-1.yaml && kubectl diff -f dev-1.yaml",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVar(&g.replay, "replay", "", "render from a recorded fixture directory")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		g.speed = 0
		_, src, err := setup(cmd, g, args, false)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		env, err := lastSnapshot(cmd.Context(), src)
		if err != nil {
			return err
		}
		for _, o := range env.Objects {
			b, err := yaml.Marshal(stripServerFields(o))
			if err != nil {
				return err
			}
			outln(cmd, "---")
			out(cmd, string(b))
		}
		notePrivateObjects(cmd, g, env)
		return nil
	}
	return cmd
}

// stripServerFields removes what the API server owns, so the output is what you
// would apply rather than what you would read back. Without this the dry-run diff
// is noise about resourceVersion.
func stripServerFields(o snapshot.Object) snapshot.Object {
	out := snapshot.Object{}
	for k, v := range o {
		if k == "status" {
			continue
		}
		out[k] = v
	}
	if meta, ok := o.Map("metadata"); ok {
		clean := map[string]any{}
		for k, v := range meta {
			switch k {
			case "resourceVersion", "uid", "generation", "creationTimestamp",
				"managedFields", "selfLink", "deletionTimestamp":
			default:
				clean[k] = v
			}
		}
		out["metadata"] = clean
	}
	return out
}

// notePrivateObjects names what render deliberately left out. The cluster's
// Secrets hold its certificate authority, its service-account key and one
// bootstrap token per machine; this tool does not read them, and writing them to
// a file the user is about to keep is not something it should do quietly. The
// silence was the defect, not the omission: three operators reading a rendered
// file all had to work out for themselves that the Secrets were missing.
//
// stderr, so `cluster render > file` is still a file of objects.
func notePrivateObjects(cmd *cobra.Command, g *globals, env snapshot.Envelope) {
	cluster, ok := env.Cluster()
	if !ok {
		return
	}
	cmd.PrintErrf("not included: the Secrets for %s (its certificate authority, etcd,"+
		" service-account and proxy keys, its kubeconfig, and one bootstrap-data"+
		" Secret per machine). They stay in the management cluster.\n", cluster.Name())
	cmd.PrintErrf("  to take them too: kubectl get secret -n %s -l %s=%s -o yaml\n",
		g.namespace, watch.ClusterNameLabel, cluster.Name())

	// A ClusterResourceSet names the objects it applies, and those live in the
	// core group, which this tool does not read. The set itself is in the file,
	// so the reference is not dangling; what it points at still has to be
	// fetched, and naming it is the difference between a gap and a surprise.
	for _, o := range env.Objects {
		if o.Kind() != "ClusterResourceSet" {
			continue
		}
		resources, _ := o.Slice("spec", "resources")
		for _, item := range resources {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			r := snapshot.Object(entry)
			cmd.PrintErrf("not included: %s/%s, applied by ClusterResourceSet %s."+
				" To take it too: kubectl get %s %s -n %s -o yaml\n",
				r.String("kind"), r.String("name"), o.Name(),
				strings.ToLower(r.String("kind")), r.String("name"), g.namespace)
		}
	}
}
