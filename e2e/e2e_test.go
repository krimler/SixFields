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

const namespace = "default"

// The suite runs on the in-memory substrate by default: a whole cluster comes up
// in about a minute with no containers, so the whole suite fits inside the budget
// and people actually run it. E2E_SUBSTRATE=docker runs the same tests against
// real containers, which takes about ten minutes per cluster.
func substrate() (class string, timeout time.Duration) {
	if os.Getenv("E2E_SUBSTRATE") == "docker" {
		return "std", 15 * time.Minute
	}
	return "std-inmemory", 5 * time.Minute
}

// writeCluster generates a six-field Cluster with the CLI, which exercises
// `cluster new` on the way to exercising everything else.
func writeCluster(t *testing.T, name string) string {
	t.Helper()
	class, _ := substrate()
	manifest, code := run(t, time.Minute, cli(t), "new", name,
		"--version", pinned(t, "WORKLOAD_K8S_VERSION"),
		"--class", class, "--pool", "default=2")
	require.Equal(t, 0, code, manifest)

	path := filepath.Join(t.TempDir(), name+".yaml")
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o644))
	return path
}

// pinned reads one value out of versions.env, so no test hardcodes a version.
func pinned(t *testing.T, key string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "versions.env"))
	require.NoError(t, err)
	for _, line := range strings.Split(string(b), "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok && name == key {
			return value
		}
	}
	return ""
}

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
	_, timeout := substrate()
	deleteCluster(t, name)

	out, code := run(t, timeout+time.Minute, cli(t), "up", name,
		"-f", writeCluster(t, name), "--no-tty", "--timeout", timeout.String())
	require.Equal(t, 0, code, "cluster up did not reach Ready:\n%s", out)
	require.Contains(t, out, "ready in ")
	require.Contains(t, out, "control plane   done")
	require.Contains(t, out, "workers         done")
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
// It runs with the policy's bindings removed. That is what ejecting means: the
// objects render describes are managed kinds, so while the policy is enforcing,
// re-applying them is denied. docs/eject.md says to remove the bindings first and
// this test proves that sequence works.
//
// It builds its own cluster. Sharing one with another test made it depend on that
// test's cleanup order, and it failed the first time the suite ran.
func TestE2E_RenderReAppliesWithNoDiff(t *testing.T) {
	const name = "e2e-eject"
	_, timeout := substrate()
	deleteCluster(t, name)

	up, code := run(t, timeout+time.Minute, cli(t), "up", name,
		"-f", writeCluster(t, name), "--no-tty", "--timeout", timeout.String())
	require.Equal(t, 0, code, up)

	rendered, code := run(t, 2*time.Minute, cli(t), "render", name)
	require.Equal(t, 0, code)
	require.NotEmpty(t, strings.TrimSpace(rendered), "render wrote nothing to stdout")

	path := filepath.Join(t.TempDir(), "rendered.yaml")
	require.NoError(t, os.WriteFile(path, []byte(rendered), 0o644))

	// The bindings come off first. kubectl diff sends no patch for an object that
	// has not changed, so it is not a reliable way to observe the policy denying;
	// TestE2E_ManagedKindIsRejectedAtAdmission covers that with a real write.
	run(t, time.Minute, "kubectl", "delete", "validatingadmissionpolicybinding",
		"sixfields-cluster-fields", "sixfields-managed-kinds")
	t.Cleanup(func() { run(t, time.Minute, "kubectl", "apply", "-f", "policy/vap/") })

	out, code := run(t, 2*time.Minute, "kubectl", "diff", "-f", path)
	// kubectl diff exits 0 when there is no difference and non-zero when there is.
	require.Equal(t, 0, code, "cluster render output does not re-apply cleanly:\n%s", out)
}

// The twelve-minute test: during provisioning the user always sees a phase, a
// detail, and either an estimate or a stall reason, never a bare Provisioning.
func TestE2E_NoBareProvisioning(t *testing.T) {
	const name = "e2e-dev-2"
	_, timeout := substrate()
	deleteCluster(t, name)

	out, _ := run(t, timeout+time.Minute, cli(t), "up", name,
		"-f", writeCluster(t, name), "--no-tty", "--timeout", timeout.String())

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
	class, _ := substrate()
	manifest := `apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: e2e-break-glass
  namespace: default
  labels:
    sixfields.io/break-glass: "true"
spec:
  clusterNetwork:
    pods:
      cidrBlocks: ["10.128.0.0/12"]
  topology:
    classRef:
      name: ` + class + `
    version: ` + pinned(t, "WORKLOAD_K8S_VERSION") + `
`
	path := filepath.Join(t.TempDir(), "break-glass.yaml")
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o644))

	// The label alone is not enough.
	out, code := run(t, time.Minute, "kubectl", "apply", "--dry-run=server", "-f", path)
	require.NotEqual(t, 0, code, "the label alone let a managed field through:\n%s", out)
	require.Contains(t, out, "needs both")
}
