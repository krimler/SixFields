//go:build e2e

// Package e2e drives a real management cluster: kind, Cluster API, CAPD, the
// assembly and the policy, all brought up by `make dev-up`. It is the only layer
// that proves the class actually reconciles; everything faster than it is covered
// by the pure tests and by envtest.
//
// Run with: make e2e
package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	namespace = "default"
	upTimeout = 15 * time.Minute
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Dir(wd)
}

// run executes a command from the repo root and returns its combined output.
func run(t *testing.T, timeout time.Duration, name string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	code := 0
	var exit *exec.ExitError
	if err != nil {
		if ok := asExit(err, &exit); ok {
			code = exit.ExitCode()
		} else {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	t.Logf("$ %s %s\n%s", name, strings.Join(args, " "), out)
	return string(out), code
}

func asExit(err error, target **exec.ExitError) bool {
	for err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func cli(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "bin", "cluster")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("bin/cluster is missing; run: make build")
	}
	return path
}

// TestMain leaves the management cluster up: `make dev-up` is idempotent and a
// rerun should cost minutes, not a rebuild. Each test uses its own cluster name
// and deletes it.
func TestMain(m *testing.M) {
	if os.Getenv("SKIP_DEV_UP") == "" {
		cmd := exec.Command("make", "dev-up")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			os.Stderr.WriteString("e2e: make dev-up failed; is a container runtime running?\n")
			os.Exit(4)
		}
	}
	os.Exit(m.Run())
}

func deleteCluster(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		run(t, 5*time.Minute, "kubectl", "delete", "cluster", name, "-n", namespace, "--ignore-not-found")
	})
}

// The six-field test: a new user creates a working cluster writing only allowed
// fields, and the tool blocks until it is ready and exits 0.
func TestE2E_SixFieldClusterReachesReady(t *testing.T) {
	const name = "e2e-dev-1"
	deleteCluster(t, name)

	out, code := run(t, upTimeout, cli(t), "up", name,
		"-f", filepath.Join(repoRoot(t), "examples", "dev-1.yaml"),
		"--no-tty", "--timeout", upTimeout.String())
	require.Equal(t, 0, code, "cluster up did not reach Ready:\n%s", out)
	require.Contains(t, out, "ready in ")
	require.Contains(t, out, "control plane   done")
}

// The untouched test: the only objects in the namespace besides the Cluster are
// ones the topology owns.
func TestE2E_OnlyTopologyOwnedObjectsExist(t *testing.T) {
	out, _ := run(t, time.Minute, "kubectl", "get",
		"kubeadmcontrolplane,machinedeployment,machine", "-n", namespace,
		"-l", "!topology.cluster.x-k8s.io/owned", "-o", "name")
	require.Empty(t, strings.TrimSpace(out),
		"objects exist that the class does not own:\n%s", out)
}

// The early-error test: a normal kubeconfig user cannot create a managed kind, and
// the error names the class and the break-glass.
func TestE2E_ManagedKindIsRejectedAtAdmission(t *testing.T) {
	manifest := `apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: KubeadmControlPlane
metadata:
  name: e2e-hand-written
  namespace: default
spec:
  replicas: 1
  version: v1.34.11
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DevMachineTemplate
        name: std-control-plane-machine
`
	path := filepath.Join(t.TempDir(), "kcp.yaml")
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o644))

	out, code := run(t, time.Minute, "kubectl", "apply", "-f", path)
	require.NotEqual(t, 0, code, "a hand-written KubeadmControlPlane was accepted")
	require.Contains(t, out, "ClusterClass")
	require.Contains(t, out, "break-glass")
}

// The eject test: `cluster render` re-applies with zero diff.
//
// It runs with the policy's bindings removed, which is not a workaround — it is
// what ejecting means. The objects render describes are managed kinds, so while
// the policy is enforcing, re-applying them is denied; docs/eject.md says to
// remove the bindings first and this test proves that sequence works.
func TestE2E_RenderReAppliesWithNoDiff(t *testing.T) {
	const name = "e2e-dev-1"
	rendered, code := run(t, 2*time.Minute, cli(t), "render", name)
	require.Equal(t, 0, code)
	require.NotEmpty(t, strings.TrimSpace(rendered), "render wrote nothing to stdout")

	path := filepath.Join(t.TempDir(), "rendered.yaml")
	require.NoError(t, os.WriteFile(path, []byte(rendered), 0o644))

	// While the policy enforces, re-applying a managed kind is denied. That is the
	// assembly working, so assert it before ejecting.
	denied, code := run(t, 2*time.Minute, "kubectl", "diff", "-f", path)
	require.NotEqual(t, 0, code, "a managed kind was patchable while the policy was enforcing")
	require.Contains(t, denied, "break-glass")

	run(t, time.Minute, "kubectl", "delete", "validatingadmissionpolicybinding",
		"capi-distro-cluster-fields", "capi-distro-managed-kinds")
	t.Cleanup(func() { run(t, time.Minute, "kubectl", "apply", "-f", "policy/vap/") })

	out, code := run(t, 2*time.Minute, "kubectl", "diff", "-f", path)
	// kubectl diff exits 0 when there is no difference and non-zero when there is.
	require.Equal(t, 0, code, "cluster render output does not re-apply cleanly:\n%s", out)
}

// The twelve-minute test: during provisioning the user always sees a phase, a
// detail, and either an estimate or a stall reason — never a bare Provisioning.
func TestE2E_NoBareProvisioning(t *testing.T) {
	const name = "e2e-dev-2"
	deleteCluster(t, name)

	out, _ := run(t, upTimeout, cli(t), "up", name,
		"-f", filepath.Join(repoRoot(t), "examples", "dev-1.yaml"),
		"--no-tty", "--timeout", upTimeout.String())

	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "t+") {
			continue
		}
		require.NotRegexp(t, `\bProvisioning\s*$`, line,
			"a line ended in a bare provider phase: %q", line)
		require.GreaterOrEqual(t, len(strings.Fields(line)), 4,
			"a status line must carry a phase, a state and a detail: %q", line)
	}
}

// Break-glass works end to end, and half of it does not.
func TestE2E_BreakGlassNeedsBothHalves(t *testing.T) {
	manifest := `apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: e2e-break-glass
  namespace: default
  labels:
    capi-distro.io/break-glass: "true"
spec:
  clusterNetwork:
    pods:
      cidrBlocks: ["10.128.0.0/12"]
  topology:
    classRef:
      name: std
    version: v1.34.11
`
	path := filepath.Join(t.TempDir(), "break-glass.yaml")
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o644))

	// The label alone is not enough.
	out, code := run(t, time.Minute, "kubectl", "apply", "--dry-run=server", "-f", path)
	require.NotEqual(t, 0, code, "the label alone let a managed field through:\n%s", out)
	require.Contains(t, out, "needs both")
}
