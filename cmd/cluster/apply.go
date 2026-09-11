package main

import (
	"bytes"
	"errors"
	"os/exec"

	"github.com/spf13/cobra"

	"sixfields/internal/msg"
)

// apply hands the object to kubectl rather than reimplementing server-side apply.
// The escape hatch runs the same command a user would, so a failure here is a
// failure they can reproduce by hand, and the admission message reaches them
// unchanged.
func apply(cmd *cobra.Command, g *globals, manifest []byte) error {
	if g.replay != "" {
		return nil
	}
	args := []string{"apply", "-f", "-", "-n", g.namespace}
	if g.kubeconfig != "" {
		args = append(args, "--kubeconfig", g.kubeconfig)
	}
	kubectl := exec.CommandContext(cmd.Context(), "kubectl", args...)
	kubectl.Stdin = bytes.NewReader(manifest)
	kubectl.Stdout = cmd.OutOrStdout()
	kubectl.Stderr = cmd.ErrOrStderr()
	if err := kubectl.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// kubectl has already printed the admission message, which is the one
			// the policy wrote. Repeating it here would only make it longer.
			return &msg.Error{Code: msg.FieldManaged, Summary: "the management cluster rejected the Cluster.",
				Action: "docs/eject.md", Cause: err}
		}
		return msg.Wrap(msg.NoRuntime, msg.Vars{}, err)
	}
	return nil
}
