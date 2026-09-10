package gen_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"capi-distro/internal/gen"
	"capi-distro/internal/golden"
	"capi-distro/internal/msg"
)

func minimal() gen.Spec {
	return gen.Spec{Name: "dev-1", Version: "v1.34.11", Pools: []gen.Pool{{Name: "default", Replicas: 2}}}
}

func TestGen_SixFieldsProduceAWholeCluster(t *testing.T) {
	out, err := minimal().YAML()
	require.NoError(t, err)
	golden.Text(t, "../../testdata/golden/gen/minimal.yaml", string(out))
}

func TestGen_HAHostedProducesTheVariables(t *testing.T) {
	spec := minimal()
	spec.Size, spec.Placement = "ha", "hosted"
	out, err := spec.YAML()
	require.NoError(t, err)
	golden.Text(t, "../../testdata/golden/gen/ha-hosted.yaml", string(out))
}

// A pool is a pool whichever kind the class puts behind it. The generator input is
// identical; only the overlay-supplied backend differs.
func TestGen_PoolMapsToWhicheverKindTheOverlayUses(t *testing.T) {
	spec := minimal()
	spec.Backend = gen.MachinePools
	out, err := spec.YAML()
	require.NoError(t, err)
	require.Contains(t, string(out), "machinePools:")
	require.NotContains(t, string(out), "machineDeployments:")

	spec.Backend = gen.MachineDeployments
	out, err = spec.YAML()
	require.NoError(t, err)
	require.Contains(t, string(out), "machineDeployments:")
}

func TestGen_DefaultsAreTheOnesPlanNames(t *testing.T) {
	got := gen.Spec{Name: "dev-1", Version: "v1.34.11", Pools: []gen.Pool{{Name: "default"}}}.Defaults()
	require.Equal(t, "default", got.Namespace)
	require.Equal(t, "std", got.Class)
	require.Equal(t, "dev", got.Size)
	require.Equal(t, "self", got.Placement)
	require.Equal(t, gen.MachineDeployments, got.Backend)
	require.Equal(t, "default", got.Pools[0].Class)
}

// Every field a user may write round-trips; the spec that comes back out is the
// one that went in.
func TestGen_AllowedFieldsRoundTrip(t *testing.T) {
	in := minimal()
	in.Size, in.Placement = "ha", "hosted"
	in.Labels = map[string]string{"team": "platform"}
	out, err := in.YAML()
	require.NoError(t, err)

	back, errs := gen.FromYAML(out)
	require.Empty(t, errs, "%v", errs)
	require.Equal(t, in.Defaults(), back)
}

// One negative case per denied field. The policy denies these at admission; the
// generator denies them before submit, with the same text.
func TestGen_DeniedFieldsAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name  string
		yaml  string
		field string
	}{
		{"clusterNetwork", "spec:\n  clusterNetwork:\n    pods:\n      cidrBlocks: [10.0.0.0/16]\n", "spec.clusterNetwork"},
		{"controlPlaneRef", "spec:\n  controlPlaneRef:\n    kind: KubeadmControlPlane\n    name: x\n", "spec.controlPlaneRef"},
		{"infrastructureRef", "spec:\n  infrastructureRef:\n    kind: DevCluster\n    name: x\n", "spec.infrastructureRef"},
		{"paused", "spec:\n  paused: true\n", "spec.paused"},
		{"topology.controlPlane", "spec:\n  topology:\n    controlPlane:\n      replicas: 5\n", "spec.topology.controlPlane"},
		{"topology.workers.other", "spec:\n  topology:\n    workers:\n      somethingElse: []\n", "spec.topology.workers.somethingElse"},
		{"pool.machineHealthCheck", "spec:\n  topology:\n    workers:\n      machineDeployments:\n      - name: default\n        class: default\n        replicas: 1\n        machineHealthCheck: {}\n", "spec.topology.workers.machineDeployments[default].machineHealthCheck"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := "apiVersion: cluster.x-k8s.io/v1beta2\nkind: Cluster\nmetadata:\n  name: dev-1\n" + tc.yaml
			_, errs := gen.FromYAML([]byte(doc))
			require.NotEmpty(t, errs)
			require.True(t, mentions(errs, tc.field), "no error named %s: %v", tc.field, errs)
			for _, e := range errs {
				require.Equal(t, msg.ExitRejected, e.ExitCode())
			}
		})
	}
}

// An unknown variable is denied by name, so the user can see which one.
func TestGen_UnknownVariableIsDeniedByName(t *testing.T) {
	doc := `apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: dev-1
spec:
  topology:
    class: std
    version: v1.34.11
    variables:
    - name: gpuPool
      value: "2"
`
	_, errs := gen.FromYAML([]byte(doc))
	require.NotEmpty(t, errs)
	require.True(t, mentions(errs, "gpuPool"), "%v", errs)
}

// A positive case per allowed field: none of them is rejected.
func TestGen_AllowedFieldsAreAccepted(t *testing.T) {
	doc := `apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: dev-1
  namespace: default
  labels:
    team: platform
  annotations:
    note: "first cluster"
spec:
  topology:
    class: std
    version: v1.34.11
    variables:
    - name: size
      value: ha
    - name: placement
      value: hosted
    workers:
      machineDeployments:
      - name: default
        class: default
        replicas: 3
`
	spec, errs := gen.FromYAML([]byte(doc))
	require.Empty(t, errs, "%v", errs)
	require.Equal(t, "ha", spec.Size)
	require.Equal(t, "hosted", spec.Placement)
	require.Equal(t, int64(3), spec.Pools[0].Replicas)
	require.Equal(t, "platform", spec.Labels["team"])
}

// Every rejection ends with something to do, and every rejection exits 3.
func TestUX_EveryRejectionHasANextAction(t *testing.T) {
	_, errs := gen.FromYAML([]byte("apiVersion: cluster.x-k8s.io/v1beta2\nkind: Cluster\nspec:\n  paused: true\n"))
	require.NotEmpty(t, errs)
	for _, e := range errs {
		require.NotEmpty(t, e.Action)
		require.Contains(t, e.Error(), "Next: ")
		require.Contains(t, strings.ToLower(e.Summary), "break-glass")
	}
}

func mentions(errs []*msg.Error, needle string) bool {
	for _, e := range errs {
		if strings.Contains(e.Summary, needle) {
			return true
		}
	}
	return false
}
