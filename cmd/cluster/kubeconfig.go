package main

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"capi-distro/internal/msg"
)

func newKubeconfigCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubeconfig NAME",
		Short: "Print a workload cluster kubeconfig that works from this host",
		Long: "CAPD writes a kubeconfig pointing at the load balancer container's address\n" +
			"inside the container network. On Docker Desktop that address is unreachable\n" +
			"from the host, so this rewrites the server to the published port. Nothing to\n" +
			"remember, nothing in a README footnote.",
		Example: "  cluster kubeconfig dev-1 > dev-1.kubeconfig\n" +
			"  KUBECONFIG=dev-1.kubeconfig kubectl get nodes",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			raw, err := readSecret(g, name+"-kubeconfig")
			if err != nil {
				return err
			}
			port, err := publishedPort(name)
			if err != nil {
				cmd.PrintErrln("kubeconfig: could not find the published port; leaving the server address alone")
				cmd.Print(raw)
				return nil
			}
			cmd.Print(RewriteServer(raw, port))
			return nil
		},
	}
	return cmd
}

// serverLine matches the one field that has to change. Rewriting text rather than
// parsing the whole kubeconfig keeps every other field, including ones this tool
// has never heard of.
var serverLine = regexp.MustCompile(`(?m)^(\s*server:\s*)https://[^\s]+$`)

// RewriteServer points the kubeconfig at the host's published port. Exported so
// TestKubeconfig_RewritesForDockerDesktop can cover it without a cluster.
func RewriteServer(kubeconfig, hostPort string) string {
	return serverLine.ReplaceAllString(kubeconfig, "${1}https://127.0.0.1:"+hostPort)
}

func readSecret(g *globals, name string) (string, error) {
	args := []string{"get", "secret", name, "-n", g.namespace, "-o", "jsonpath={.data.value}"}
	if g.kubeconfig != "" {
		args = append(args, "--kubeconfig", g.kubeconfig)
	}
	out, err := exec.Command("kubectl", args...).Output()
	if err != nil {
		return "", msg.Wrap(msg.ClusterNotFound, msg.Vars{Object: name, Namespace: g.namespace}, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", msg.Wrap(msg.ClusterNotFound, msg.Vars{Object: name, Namespace: g.namespace}, err)
	}
	return string(decoded), nil
}

// publishedPort asks the container runtime which host port the cluster's load
// balancer is published on. CAPD names that container <cluster>-lb.
func publishedPort(cluster string) (string, error) {
	out, err := exec.Command("docker", "port", cluster+"-lb", "6443/tcp").Output()
	if err != nil {
		return "", err
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	_, port, ok := strings.Cut(first, ":")
	if !ok {
		return "", fmt.Errorf("unexpected docker port output %q", first)
	}
	return port, nil
}
