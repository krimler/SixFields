package main

import (
	"bytes"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"sixfields/internal/msg"
)

// checkClass refuses a Cluster whose ClusterClass is not installed, before the
// Cluster is applied.
//
// This is the one mistake no other layer catches. The API server accepts a
// Cluster naming a class that does not exist: it returns a warning, not an
// error, stores the object, and then creates nothing. The admission policy
// cannot help, because a ValidatingAdmissionPolicy sees only the object being
// written and cannot read a second object to find out whether the class is
// there. So the client looks.
//
// A management cluster that cannot be reached is not an error here. `up` is
// about to apply, and apply will produce the better message.
func checkClass(cmd *cobra.Command, g *globals, class string) error {
	if class == "" || g.replay != "" {
		return nil
	}
	installed, ok := installedClasses(cmd, g)
	if !ok {
		return nil
	}
	for _, name := range installed {
		if name == class {
			return nil
		}
	}
	return classNotInstalled(class, g.namespace, installed)
}

func installedClasses(cmd *cobra.Command, g *globals) ([]string, bool) {
	args := []string{"get", "clusterclass", "-n", g.namespace, "-o", "name"}
	if g.kubeconfig != "" {
		args = append(args, "--kubeconfig", g.kubeconfig)
	}
	var out bytes.Buffer
	kubectl := exec.CommandContext(cmd.Context(), "kubectl", args...)
	kubectl.Stdout = &out
	if err := kubectl.Run(); err != nil {
		return nil, false
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if _, name, found := strings.Cut(strings.TrimSpace(line), "/"); found {
			names = append(names, name)
		}
	}
	return names, len(names) > 0
}

// classNotInstalled is the message, kept apart from the lookup so the wording is
// a unit test rather than something only a live cluster can check.
func classNotInstalled(class, namespace string, installed []string) *msg.Error {
	e := msg.New(msg.ClassNotFound, msg.Vars{Class: class, Namespace: namespace})
	e.Action = strings.ReplaceAll(e.Action, "{{.Namespace}}", namespace)
	// Three edits apart is a different name, not a misspelling of this one.
	if suggestion := nearest(class, installed, 3); suggestion != "" {
		e.Action = "did you mean " + suggestion + "? Otherwise: " + e.Action
	}
	return e
}
