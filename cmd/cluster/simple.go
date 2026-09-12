package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"sixfields/internal/gen"
	"sixfields/internal/msg"
	"sixfields/internal/snapshot"
)

// lastSnapshot drains a source and returns the final envelope, which is what the
// one-shot commands work from.
func lastSnapshot(ctx context.Context, src source) (snapshot.Envelope, error) {
	snapshots, err := src.Snapshots(ctx)
	if err != nil {
		return snapshot.Envelope{}, err
	}
	var last snapshot.Envelope
	found := false
	for env := range snapshots {
		last, found = env, true
	}
	if !found {
		return snapshot.Envelope{}, msg.New(msg.ClusterNotFound, msg.Vars{Object: "", Namespace: "the namespace"})
	}
	return last, nil
}

func newExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain [CODE]",
		Short: "Print the long form of an error code",
		Long: "Every stall class and every denial has a stable code. The long form says what\n" +
			"happened, why, and what to do next. It ships in the binary: no key, no network.",
		Example: "  cluster explain CAPI-CP-003\n  cluster explain",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				for _, code := range msg.Codes() {
					entry, _ := msg.Get(code)
					outf(cmd, "%-18s %s\n", code, entry.Title)
				}
				return nil
			}
			code := msg.Code(strings.ToUpper(args[0]))
			text, ok := msg.Longform(code)
			if !ok {
				return unknownCode(code)
			}
			out(cmd, text)
			return nil
		},
	}
}

func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs CODE",
		Short: "Print the runbook for a stall class",
		Long: "The runbook is the rung above the long form: what you are seeing, which object\n" +
			"holds the truth, what to check in order, and the common causes.",
		Example: "  cluster docs CAPI-CP-003",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := msg.Code(strings.ToUpper(args[0]))
			text, ok := msg.Runbook(code)
			if !ok {
				return unknownCode(code)
			}
			out(cmd, text)
			return nil
		},
	}
}

// unknownCode is where `did you mean` earns its place: a mistyped code is the most
// likely way to get here.
func unknownCode(code msg.Code) error {
	suggestion := nearest(string(code), codeStrings(), 4)
	action := "cluster explain"
	if suggestion != "" {
		action = fmt.Sprintf("did you mean %s? Otherwise: cluster explain", suggestion)
	}
	return &msg.Error{
		Code:    msg.ClusterNotFound,
		Summary: fmt.Sprintf("%s is not an error code.", code),
		Action:  action,
	}
}

func codeStrings() []string {
	out := make([]string, 0, len(msg.Codes()))
	for _, c := range msg.Codes() {
		out = append(out, string(c))
	}
	return out
}

func newPlanCmd(g *globals) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "plan -f FILE",
		Short: "Run the admission rules locally, before submitting anything",
		Long: "plan answers the question `up` would answer a second later, without touching\n" +
			"the cluster. The messages are the ones admission would produce, character for\n" +
			"character.",
		Example: "  cluster plan -f cluster.yaml",
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "the Cluster to check")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
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
		outf(cmd, "%s in %s: class %s, %s, %s control plane (%d nodes)\n",
			spec.Name, spec.Namespace, spec.Class, spec.Version, spec.Placement,
			gen.ControlPlaneReplicas(spec.Size))
		for _, p := range spec.Pools {
			outf(cmd, "  pool %s: %d nodes\n", p.Name, p.Replicas)
		}
		// plan reads the file and nothing else, so it cannot know whether the class
		// exists. Saying so is the difference between "this is fine" and "the part
		// I can check is fine": a Cluster naming a class that is not installed
		// passes every field rule and then creates nothing.
		outln(cmd, "no field is rejected; `cluster up` would apply this unchanged.")
		outf(cmd, "class %s is not checked here; `cluster up` looks it up before applying.\n", spec.Class)
		return nil
	}
	return cmd
}

func newNewCmd(g *globals) *cobra.Command {
	spec := gen.Spec{}
	var pools []string
	cmd := &cobra.Command{
		Use:   "new NAME",
		Short: "Write a Cluster from the six fields",
		Long: "new prints YAML; it never applies. Pipe it to a file, read it, then\n" +
			"`cluster up NAME -f` it.",
		Example: "  cluster new dev-1 --version v1.34.11 --pool default=2\n" +
			"  cluster new ha-1 --version v1.34.11 --size ha --placement hosted --pool default=3",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec.Name = args[0]
			spec.Namespace = g.namespace
			for _, p := range pools {
				name, count, ok := strings.Cut(p, "=")
				if !ok {
					return &msg.Error{Code: msg.FieldManaged,
						Summary: fmt.Sprintf("--pool %q is not name=replicas.", p),
						Action:  "cluster new NAME --pool default=2"}
				}
				replicas := int64(0)
				if _, err := fmt.Sscanf(count, "%d", &replicas); err != nil {
					return &msg.Error{Code: msg.FieldManaged,
						Summary: fmt.Sprintf("--pool %q has a non-numeric replica count.", p),
						Action:  "cluster new NAME --pool default=2"}
				}
				spec.Pools = append(spec.Pools, gen.Pool{Name: name, Replicas: replicas})
			}
			if errs := spec.Validate(); len(errs) > 0 {
				for _, e := range errs {
					cmd.PrintErrln(e.Summary)
					cmd.PrintErrln("  next: " + e.Action)
				}
				return exitWith(errs[0])
			}
			manifest, err := spec.YAML()
			if err != nil {
				return err
			}
			out(cmd, string(manifest))
			return nil
		},
	}
	cmd.Flags().StringVar(&spec.Version, "version", "", "Kubernetes version, e.g. v1.34.11")
	cmd.Flags().StringVar(&spec.Size, "size", "dev", "dev or ha")
	cmd.Flags().StringVar(&spec.Placement, "placement", "self", "self or hosted")
	cmd.Flags().StringVar(&spec.Class, "class", gen.DefaultClass, "ClusterClass name")
	cmd.Flags().StringArrayVar(&pools, "pool", nil, "a worker pool as name=replicas, repeatable")
	return cmd
}

// nearest is a small-edit suggestion: cheap, and it catches the typo that
// actually happens (a transposed character or a missing one). The budget is the
// caller's, because an error code and a class name tolerate different amounts of
// wrongness: 'gpu' is three edits from 'std' and is not a misspelling of it.
func nearest(input string, candidates []string, budget int) string {
	best, bestDistance := "", budget
	for _, c := range candidates {
		if d := distance(strings.ToLower(input), strings.ToLower(c)); d < bestDistance {
			best, bestDistance = c, d
		}
	}
	return best
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(prev[j]+1, min(current[j-1]+1, prev[j-1]+cost))
		}
		copy(prev, current)
	}
	return prev[len(b)]
}
