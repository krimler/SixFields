package main

import (
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"capi-distro/internal/snapshot"
)

func newRenderCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render NAME",
		Short: "Print every CAPI object behind a cluster",
		Long: "render is the escape hatch. It prints the objects the class produced, so you\n" +
			"can read them, diff them, or take them and leave.\n\n" +
			"The output re-applies with no diff: `cluster render NAME |\n" +
			"kubectl apply --dry-run=server -f -`.",
		Example: "  cluster render dev-1\n" +
			"  cluster render dev-1 | kubectl apply --dry-run=server -f -",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVar(&g.replay, "replay", "", "render from a recorded fixture directory")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		g.speed = 0
		_, src, err := setup(cmd, g, args, false)
		if err != nil {
			return err
		}
		defer src.Close()
		env, err := lastSnapshot(cmd.Context(), src)
		if err != nil {
			return err
		}
		for _, o := range env.Objects {
			b, err := yaml.Marshal(stripServerFields(o))
			if err != nil {
				return err
			}
			cmd.Println("---")
			cmd.Print(string(b))
		}
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
