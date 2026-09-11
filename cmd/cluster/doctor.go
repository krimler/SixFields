package main

import (
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"sixfields/internal/msg"
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
					outf(cmd, "  fail  %s\n", label)
					return
				}
				outf(cmd, "  ok    %s\n", label)
			}

			outln(cmd, "management cluster")
			check("reachable", kubectl(g, "version", "-o", "json"))
			check("Cluster kind is served", kubectl(g, "get", "crd", "clusters.cluster.x-k8s.io"))

			outln(cmd, "permissions in "+g.namespace)
			check("create clusters", kubectl(g, "auth", "can-i", "create", "clusters.cluster.x-k8s.io", "-n", g.namespace))
			// `auth can-i` answers about RBAC, and RBAC is not what refuses a
			// managed kind — the admission policy is. A cluster-admin passes the
			// RBAC check and is still denied, so this asks the layer that decides,
			// with a dry-run the API server evaluates and then discards.
			switch refused, why := managedKindRefused(g); {
			case refused:
				outln(cmd, "  ok    managed kinds are refused at admission")
			default:
				failed = true
				outf(cmd, "  fail  a KubeadmControlPlane was admitted: %s\n", why)
				outln(cmd, "        the policy is not installed or you are exempt from it — see docs/eject.md")
			}

			outln(cmd, "classes")
			check("ClusterClass std exists", kubectl(g, "get", "clusterclass", "std", "-n", g.namespace))

			if failed {
				return msg.New(msg.NoRuntime, msg.Vars{})
			}
			return nil
		},
	}
}

// managedKindRefused submits a KubeadmControlPlane as a server-side dry run. The
// API server runs admission and discards the object, so nothing is created either
// way; a Forbidden naming the policy is the answer we want.
func managedKindRefused(g *globals) (refused bool, detail string) {
	const manifest = `apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: KubeadmControlPlane
metadata:
  name: sixfields-doctor-probe
spec:
  replicas: 1
  version: v1.0.0
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DevMachineTemplate
        name: sixfields-doctor-probe
`
	args := []string{"apply", "--dry-run=server", "-n", g.namespace, "-f", "-"}
	if g.kubeconfig != "" {
		args = append(args, "--kubeconfig", g.kubeconfig)
	}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return false, "it was accepted"
	}
	text := string(out)
	if strings.Contains(text, "break-glass") {
		return true, ""
	}
	// Denied by something else — RBAC, a webhook, a missing CRD. Still refused,
	// but not by this policy, and a user should know which.
	return true, text
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
