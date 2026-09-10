package main

import (
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"capi-distro/internal/msg"
)

func newDoctorCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that this machine can create a cluster, and say what is missing",
		Long: "doctor checks the things that stop a `cluster up` before it starts: the\n" +
			"management cluster is reachable, you may create a Cluster in this namespace,\n" +
			"and the class you are about to name exists.\n\n" +
			"For the development environment itself — tools, architecture, container\n" +
			"runtime, memory profile — run `make doctor`.",
		Example: "  cluster doctor\n  cluster doctor -n team-a",
		RunE: func(cmd *cobra.Command, _ []string) error {
			failed := false
			check := func(label string, err error) {
				if err != nil {
					failed = true
					cmd.Printf("  fail  %s\n", label)
					return
				}
				cmd.Printf("  ok    %s\n", label)
			}

			cmd.Println("management cluster")
			check("reachable", kubectl(g, "version", "-o", "json"))
			check("Cluster kind is served", kubectl(g, "get", "crd", "clusters.cluster.x-k8s.io"))

			cmd.Println("permissions in " + g.namespace)
			check("create clusters", kubectl(g, "auth", "can-i", "create", "clusters.cluster.x-k8s.io", "-n", g.namespace))
			// Being able to write a managed kind means the policy is missing or you
			// are exempt from it. Either way it is worth knowing before you rely on
			// errors arriving at admission time.
			if err := kubectl(g, "auth", "can-i", "create", "kubeadmcontrolplanes.controlplane.cluster.x-k8s.io", "-n", g.namespace); err == nil {
				cmd.Println("  warn  you can create a KubeadmControlPlane directly: the admission policy is not covering you")
			} else {
				cmd.Println("  ok    managed kinds are refused")
			}

			cmd.Println("classes")
			check("ClusterClass std exists", kubectl(g, "get", "clusterclass", "std", "-n", g.namespace))

			if failed {
				return msg.New(msg.NoRuntime, msg.Vars{})
			}
			return nil
		},
	}
}

func kubectl(g *globals, args ...string) error {
	if g.kubeconfig != "" {
		args = append(args, "--kubeconfig", g.kubeconfig)
	}
	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	// `kubectl auth can-i` exits 0 and prints "no" when the answer is no.
	if strings.TrimSpace(string(out)) == "no" {
		return errNo
	}
	return nil
}

var errNo = errNoType{}

type errNoType struct{}

func (errNoType) Error() string { return "no" }
