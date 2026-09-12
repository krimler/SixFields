package tests_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sixfields/internal/gen"
	"sixfields/internal/msg"
	"sixfields/policy/tests"
)

const vapDir = "../vap"

const (
	fieldsPolicy = "sixfields-cluster-fields"
	kindsPolicy  = "sixfields-managed-kinds"
)

// Identities. The controllers are the ones that must be exempt for the happy path
// to work at all; the humans are the ones the policy exists to stop.
const (
	human           = "kubernetes-admin"
	capiManager     = "system:serviceaccount:capi-system:capi-manager"
	capdManager     = "system:serviceaccount:capd-system:capd-manager"
	kubeadmCP       = "system:serviceaccount:capi-kubeadm-control-plane-system:capi-kubeadm-control-plane-manager"
	kubeadmBoot     = "system:serviceaccount:capi-kubeadm-bootstrap-system:capi-kubeadm-bootstrap-manager"
	k0sBootstrap    = "system:serviceaccount:k0smotron:k0smotron-controller-manager-bootstrap"
	k0sControlPlane = "system:serviceaccount:k0smotron:k0smotron-controller-manager-control-plane"
	k0sInfra        = "system:serviceaccount:k0smotron:k0smotron-controller-manager-infrastructure"
)

func policies(t *testing.T) map[string]*tests.Policy {
	t.Helper()
	loaded, err := tests.LoadDir(vapDir)
	require.NoError(t, err)
	require.NotEmpty(t, loaded)
	out := map[string]*tests.Policy{}
	for _, p := range loaded {
		out[p.Name] = p
	}
	require.Contains(t, out, fieldsPolicy)
	require.Contains(t, out, kindsPolicy)
	return out
}

// cluster is the six-field Cluster every positive case starts from.
func cluster(mutate ...func(map[string]any)) map[string]any {
	obj := map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": "dev-1", "namespace": "default"},
		"spec": map[string]any{
			"topology": map[string]any{
				"classRef": map[string]any{"name": "std"},
				"version":  "v1.34.11",
				"workers": map[string]any{
					"machineDeployments": []any{
						map[string]any{"name": "default", "class": "default", "replicas": int64(2)},
					},
				},
			},
		},
	}
	for _, m := range mutate {
		m(obj)
	}
	return obj
}

func topology(obj map[string]any) map[string]any {
	return obj["spec"].(map[string]any)["topology"].(map[string]any)
}

func withVariable(name string, value any) func(map[string]any) {
	return func(obj map[string]any) {
		t := topology(obj)
		vars, _ := t["variables"].([]any)
		t["variables"] = append(vars, map[string]any{"name": name, "value": value})
	}
}

func withLabel(key, value string) func(map[string]any) {
	return func(obj map[string]any) {
		metadata := obj["metadata"].(map[string]any)
		labels, _ := metadata["labels"].(map[string]any)
		if labels == nil {
			labels = map[string]any{}
			metadata["labels"] = labels
		}
		labels[key] = value
	}
}

func decide(t *testing.T, policy string, req tests.Request) tests.Decision {
	t.Helper()
	if req.Username == "" {
		req.Username = human
	}
	if req.Kind == "" {
		req.Kind, _ = req.Object["kind"].(string)
	}
	if req.Resource == "" {
		req.Resource = strings.ToLower(req.Kind) + "s"
	}
	got, err := policies(t)[policy].Evaluate(req)
	require.NoError(t, err)
	return got
}

// --- positive: every field a user may write ---

func TestPolicy_AllowsTheSixFieldCluster(t *testing.T) {
	got := decide(t, fieldsPolicy, tests.Request{Object: cluster()})
	require.True(t, got.Matched, "the policy must apply to an ordinary user")
	require.True(t, got.Allowed, got.Message)
}

func TestPolicy_AllowsEveryPermittedField(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"labels": withLabel("team", "platform"),
		"annotations": func(o map[string]any) {
			o["metadata"].(map[string]any)["annotations"] = map[string]any{"note": "first"}
		},
		"size dev":  withVariable("size", "dev"),
		"placement": withVariable("placement", "hosted"),
		"classRef.namespace": func(o map[string]any) {
			topology(o)["classRef"].(map[string]any)["namespace"] = "default"
		},
		"pool replicas": func(o map[string]any) {
			pools := topology(o)["workers"].(map[string]any)["machineDeployments"].([]any)
			pools[0].(map[string]any)["replicas"] = int64(5)
		},
		"a second pool": func(o map[string]any) {
			workers := topology(o)["workers"].(map[string]any)
			workers["machineDeployments"] = append(workers["machineDeployments"].([]any),
				map[string]any{"name": "gpu", "class": "default", "replicas": int64(1)})
		},
		"machinePools instead": func(o map[string]any) {
			topology(o)["workers"] = map[string]any{"machinePools": []any{
				map[string]any{"name": "default", "class": "default", "replicas": int64(2)}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := decide(t, fieldsPolicy, tests.Request{Object: cluster(mutate)})
			require.True(t, got.Allowed, got.Message)
		})
	}
}

// size is the user's word for how many control-plane nodes. The generator expands
// it, and the policy accepts the expansion only when the two agree, the same
// mapping internal/gen uses, so the CLI and the policy cannot drift.
func TestPolicy_ControlPlaneReplicasMustAgreeWithSize(t *testing.T) {
	for _, tc := range []struct {
		size     string
		replicas int64
		allowed  bool
	}{
		{"dev", 1, true},
		{"ha", 3, true},
		{"dev", 3, false},
		{"ha", 1, false},
		{"dev", 5, false},
	} {
		name := tc.size + "/" + itoa(tc.replicas)
		t.Run(name, func(t *testing.T) {
			obj := cluster(withVariable("size", tc.size), func(o map[string]any) {
				topology(o)["controlPlane"] = map[string]any{"replicas": tc.replicas}
			})
			got := decide(t, fieldsPolicy, tests.Request{Object: obj})
			require.Equal(t, tc.allowed, got.Allowed, got.Message)
			if tc.allowed {
				require.Equal(t, gen.ControlPlaneReplicas(tc.size), tc.replicas,
					"the policy and internal/gen must agree on what a size means")
			}
		})
	}
}

// --- negative: one case per denied path ---

// deniedPaths is the coverage list. Adding a denial to the policy without adding
// a case here fails TestPolicy_EveryDeniedPathHasACase.
var deniedPaths = map[string]func(map[string]any){
	"spec.clusterNetwork": func(o map[string]any) {
		o["spec"].(map[string]any)["clusterNetwork"] = map[string]any{"pods": map[string]any{"cidrBlocks": []any{"10.0.0.0/16"}}}
	},
	"spec.controlPlaneRef": func(o map[string]any) {
		o["spec"].(map[string]any)["controlPlaneRef"] = map[string]any{"kind": "KubeadmControlPlane", "name": "x"}
	},
	"spec.infrastructureRef": func(o map[string]any) {
		o["spec"].(map[string]any)["infrastructureRef"] = map[string]any{"kind": "DevCluster", "name": "x"}
	},
	"spec.controlPlaneEndpoint": func(o map[string]any) {
		o["spec"].(map[string]any)["controlPlaneEndpoint"] = map[string]any{"host": "1.2.3.4", "port": int64(6443)}
	},
	"spec.paused": func(o map[string]any) {
		o["spec"].(map[string]any)["paused"] = true
	},
	// Not a managed key but a malformed value. The commonest cause is a file meant
	// to be run through envsubst that was applied directly, and the API server
	// accepts it: nothing in Cluster API checks the shape of this string.
	"spec.topology.version": func(o map[string]any) {
		topology(o)["version"] = "1.34.11"
	},
	"spec.availabilityGates": func(o map[string]any) {
		o["spec"].(map[string]any)["availabilityGates"] = []any{map[string]any{"conditionType": "Ready"}}
	},
	"spec.topology.controlPlane.metadata": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"metadata": map[string]any{"labels": map[string]any{"a": "b"}}}
	},
	"spec.topology.controlPlane.healthCheck": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"healthCheck": map[string]any{"enabled": true}}
	},
	"spec.topology.controlPlane.deletion": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"deletion": map[string]any{"nodeDrainTimeoutSeconds": int64(30)}}
	},
	"spec.topology.controlPlane.readinessGates": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"readinessGates": []any{map[string]any{"conditionType": "Ready"}}}
	},
	"spec.topology.controlPlane.variables": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"variables": map[string]any{"overrides": []any{}}}
	},
	"spec.topology.controlPlane.taints": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"taints": []any{map[string]any{"key": "a", "effect": "NoSchedule"}}}
	},
	"spec.topology.controlPlane.rollout": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"rollout": map[string]any{"strategy": map[string]any{}}}
	},
	"spec.topology.workers.machineDeployments[].failureDomain": func(o map[string]any) {
		pools := topology(o)["workers"].(map[string]any)["machineDeployments"].([]any)
		pools[0].(map[string]any)["failureDomain"] = "zone-a"
	},
	"spec.topology.workers.machineDeployments[].rollout": func(o map[string]any) {
		pools := topology(o)["workers"].(map[string]any)["machineDeployments"].([]any)
		pools[0].(map[string]any)["rollout"] = map[string]any{"strategy": map[string]any{}}
	},
	"spec.topology.workers.machinePools[].minReadySeconds": func(o map[string]any) {
		topology(o)["workers"] = map[string]any{"machinePools": []any{
			map[string]any{"name": "default", "class": "default", "replicas": int64(1), "minReadySeconds": int64(5)}}}
	},
	"spec.topology.variables[unknown]": withVariable("gpuPool", "2"),
	"spec.topology.controlPlane.replicas": func(o map[string]any) {
		topology(o)["controlPlane"] = map[string]any{"replicas": int64(5)}
	},
}

func TestPolicy_EveryDeniedPathIsDenied(t *testing.T) {
	for path, mutate := range deniedPaths {
		t.Run(path, func(t *testing.T) {
			got := decide(t, fieldsPolicy, tests.Request{Object: cluster(mutate)})
			require.False(t, got.Allowed, "%s was allowed", path)
			require.Equal(t, "Forbidden", got.Reason)
		})
	}
}

// Every denial names the offending path, names the class, and says break-glass,
// the same three things internal/msg promises (D2.3).
func TestUX_AdmissionMessageContract(t *testing.T) {
	for path, mutate := range deniedPaths {
		if path == "spec.topology.version" {
			// A managed field names the class that owns it. A malformed value is a
			// different denial: the field is the user's, the value is wrong, and
			// no class owns it. TestPolicy_MalformedVersionIsDenied covers its text.
			continue
		}
		t.Run(path, func(t *testing.T) {
			got := decide(t, fieldsPolicy, tests.Request{Object: cluster(mutate)})
			require.False(t, got.Allowed)
			require.Contains(t, got.Message, "break-glass")
			require.Contains(t, got.Message, "ClusterClass 'std'")
			require.LessOrEqual(t, strings.Count(got.Message, ". "), 1, "at most two sentences: %q", got.Message)
			require.NotContains(t, got.Message, "\n")
		})
	}
}

// The admission message and `cluster plan` must be the same text. This is what
// makes moving an error before submit honest rather than approximate.
func TestUX_PlanMatchesAdmission(t *testing.T) {
	for _, tc := range []struct {
		path   string
		mutate func(map[string]any)
		want   string
	}{
		{"spec.clusterNetwork", deniedPaths["spec.clusterNetwork"],
			msg.Render(msg.FieldManaged, msg.Vars{Field: "spec.clusterNetwork", Class: "std"})},
		{"spec.paused", deniedPaths["spec.paused"],
			msg.Render(msg.FieldManaged, msg.Vars{Field: "spec.paused", Class: "std"})},
		{"unknown variable", deniedPaths["spec.topology.variables[unknown]"],
			msg.Render(msg.VariableUnknown, msg.Vars{Variable: "gpuPool", Class: "std"})},
		{"controlPlane.replicas", deniedPaths["spec.topology.controlPlane.replicas"],
			msg.Render(msg.FieldManaged, msg.Vars{Field: "spec.topology.controlPlane.replicas", Class: "std"})},
		{"malformed version", deniedPaths["spec.topology.version"],
			msg.Render(msg.VersionMalformed, msg.Vars{Field: "spec.topology.version", Version: "1.34.11"})},
	} {
		t.Run(tc.path, func(t *testing.T) {
			got := decide(t, fieldsPolicy, tests.Request{Object: cluster(tc.mutate)})
			require.False(t, got.Allowed)
			require.Equal(t, tc.want, got.Message,
				"the policy and internal/msg have drifted; they must be byte-for-byte identical")
		})
	}
}

// --- exemptions ---

func TestPolicy_ControllersAreExempt(t *testing.T) {
	for _, sa := range []string{capiManager, capdManager, kubeadmCP, kubeadmBoot, k0sBootstrap, k0sControlPlane, k0sInfra} {
		t.Run(shortSA(sa), func(t *testing.T) {
			obj := cluster(deniedPaths["spec.infrastructureRef"])
			got := decide(t, fieldsPolicy, tests.Request{Object: obj, Username: sa})
			require.True(t, got.Allowed, "%s must be able to write what it owns: %s", sa, got.Message)
		})
	}
}

// A human with cluster-admin is exactly who this policy exists to constrain.
// Exempting system:masters would make it a no-op for the dev-loop kubeconfig.
func TestPolicy_ClusterAdminIsNotExempt(t *testing.T) {
	for _, groups := range [][]string{
		{"system:masters", "system:authenticated"},
		{"system:serviceaccounts", "system:authenticated"},
	} {
		t.Run(groups[0], func(t *testing.T) {
			obj := cluster(deniedPaths["spec.clusterNetwork"])
			got := decide(t, fieldsPolicy, tests.Request{Object: obj, Groups: groups})
			require.False(t, got.Allowed, "%v must not be exempt", groups)
		})
	}
}

// --- break-glass: both halves, or nothing ---

func TestPolicy_BreakGlassNeedsBothHalves(t *testing.T) {
	deny := deniedPaths["spec.clusterNetwork"]
	label := withLabel("sixfields.io/break-glass", "true")
	group := []string{"sixfields:break-glass", "system:authenticated"}

	t.Run("label only", func(t *testing.T) {
		got := decide(t, fieldsPolicy, tests.Request{Object: cluster(deny, label)})
		require.False(t, got.Allowed)
		require.Contains(t, got.Message, "needs both")
		require.Contains(t, got.Message, "sixfields:break-glass")
	})
	t.Run("group only", func(t *testing.T) {
		got := decide(t, fieldsPolicy, tests.Request{Object: cluster(deny), Groups: group})
		require.False(t, got.Allowed)
		require.Contains(t, got.Message, "needs both")
	})
	t.Run("both", func(t *testing.T) {
		got := decide(t, fieldsPolicy, tests.Request{Object: cluster(deny, label), Groups: group})
		require.True(t, got.Allowed, got.Message)
	})
}

// Every use of break-glass is visible in the audit log, and a request that used
// none records nothing.
func TestPolicy_BreakGlassIsAudited(t *testing.T) {
	deny := deniedPaths["spec.clusterNetwork"]
	label := withLabel("sixfields.io/break-glass", "true")
	group := []string{"sixfields:break-glass", "system:authenticated"}

	granted := decide(t, fieldsPolicy, tests.Request{Object: cluster(deny, label), Groups: group, Username: "carol"})
	require.Contains(t, granted.Audit["break-glass"], "granted")
	require.Contains(t, granted.Audit["break-glass"], "user=carol")

	partial := decide(t, fieldsPolicy, tests.Request{Object: cluster(deny, label), Username: "dave"})
	require.Contains(t, partial.Audit["break-glass"], "incomplete")

	clean := decide(t, fieldsPolicy, tests.Request{Object: cluster()})
	require.Empty(t, clean.Audit["break-glass"], "an ordinary write records no break-glass annotation")
}

// --- managed kinds ---

func managed(kind string, mutate ...func(map[string]any)) map[string]any {
	group := "controlplane.cluster.x-k8s.io"
	switch kind {
	case "MachineDeployment", "MachineSet", "Machine", "MachinePool":
		group = "cluster.x-k8s.io"
	case "DockerCluster", "DevCluster", "DevMachineTemplate", "DockerMachineTemplate":
		group = "infrastructure.cluster.x-k8s.io"
	case "KubeadmConfig", "KubeadmConfigTemplate":
		group = "bootstrap.cluster.x-k8s.io"
	}
	obj := map[string]any{
		"apiVersion": group + "/v1beta2",
		"kind":       kind,
		"metadata":   map[string]any{"name": "dev-1-x", "namespace": "default"},
		"spec":       map[string]any{},
	}
	for _, m := range mutate {
		m(obj)
	}
	return obj
}

func TestPolicy_ManagedKindsAreDeniedToPeople(t *testing.T) {
	for _, kind := range gen.ManagedKinds {
		t.Run(kind, func(t *testing.T) {
			got := decide(t, kindsPolicy, tests.Request{Object: managed(kind)})
			require.False(t, got.Allowed, "%s was allowed", kind)
			require.Contains(t, got.Message, kind)
			require.Contains(t, got.Message, "break-glass")
		})
	}
}

func TestPolicy_ManagedKindsAreAllowedToTheirControllers(t *testing.T) {
	for _, tc := range []struct{ kind, sa string }{
		{"MachineDeployment", capiManager},
		{"Machine", capiManager},
		{"KubeadmControlPlane", kubeadmCP},
		{"KubeadmConfig", kubeadmBoot},
		{"DevMachineTemplate", capdManager},
		{"K0sControlPlane", k0sControlPlane},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			got := decide(t, kindsPolicy, tests.Request{Object: managed(tc.kind), Username: tc.sa})
			require.True(t, got.Allowed, "%s must be able to write %s: %s", tc.sa, tc.kind, got.Message)
		})
	}
}

// The kinds a user still owns. A ClusterClass is what an operator installs, and
// a Cluster is the one kind the whole design asks a user to write; denying
// either would deny the assembly itself.
//
// DevMachine used to be on this list, on the reasoning that denying it would
// break the class. It does not: a DevMachine is created by the MachineSet
// controller and reconciled by the CAPD one, and both service accounts are
// exempt. Leaving it writable meant `kubectl patch devmachine` succeeded for an
// ordinary user, which is the one thing the policy exists to stop.
func TestPolicy_UnmanagedKindsAreUntouched(t *testing.T) {
	for _, kind := range []string{"ClusterClass", "Cluster"} {
		t.Run(kind, func(t *testing.T) {
			got := decide(t, kindsPolicy, tests.Request{Object: managed(kind)})
			require.True(t, got.Allowed, "%s is not a managed kind", kind)
		})
	}
}

// The ownership label is the second check, not a way for a user to exempt
// themselves, metadata.labels is inside the allowed surface.
func TestPolicy_OwnedLabelDoesNotExemptAPerson(t *testing.T) {
	obj := managed("MachineDeployment", func(o map[string]any) {
		o["metadata"].(map[string]any)["labels"] = map[string]any{"topology.cluster.x-k8s.io/owned": ""}
	})
	got := decide(t, kindsPolicy, tests.Request{Object: obj})
	require.False(t, got.Allowed, "a person with the owned label must still be denied")
}

func itoa(n int64) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return strings.TrimSpace(strings.Repeat(" ", 0)) + string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func shortSA(sa string) string {
	parts := strings.Split(sa, ":")
	return parts[len(parts)-1]
}

// A managed object carries the cluster's name, never its class, so the policy
// cannot know which of the assembly's classes produced it. Naming one told a
// std-inmemory user their object belonged to std, which a UX probe caught.
func TestUX_KindDenialNamesNoParticularClass(t *testing.T) {
	got := decide(t, kindsPolicy, tests.Request{Object: managed("MachineDeployment")})
	require.False(t, got.Allowed)
	require.Contains(t, got.Message, "the ClusterClass that created it")
	require.NotContains(t, got.Message, "'std'")
	require.Equal(t, msg.Render(msg.KindManaged, msg.Vars{Kind: "MachineDeployment"}), got.Message,
		"the policy and internal/msg have drifted")
}

// The generator refuses managed kinds before submit and the policy refuses them
// at admission. Two lists that must agree, so this is the test that says so.
func TestPolicy_ManagedKindListMatchesTheGenerator(t *testing.T) {
	inPolicy := kindsInPolicy(t)
	for _, kind := range gen.ManagedKinds {
		require.Contains(t, inPolicy, kind, "gen.ManagedKinds has %s and the policy does not", kind)
	}
	for _, kind := range inPolicy {
		require.Contains(t, gen.ManagedKinds, kind, "the policy has %s and gen.ManagedKinds does not", kind)
	}
}

// A version the provider cannot publish is refused at admission. The control
// study found this one accepted by the API server and caught by `cluster plan`,
// which a user reaching for kubectl never runs.
func TestPolicy_MalformedVersionIsDenied(t *testing.T) {
	// The empty string is not here: spec.topology.version is +required with
	// MinLength=1 (api@v1.14.2 core/v1beta2/cluster_types.go:563), so the API
	// server's own schema refuses it and the CEL harness does not model schemas.
	for _, version := range []string{"1.34.11", "v1.34", "${KUBERNETES_VERSION}", "latest"} {
		t.Run(version, func(t *testing.T) {
			got := decide(t, fieldsPolicy, tests.Request{Object: cluster(func(o map[string]any) {
				topology(o)["version"] = version
			})})
			require.False(t, got.Allowed, "version %q was allowed", version)
			require.Contains(t, got.Message, "not a Kubernetes version")
		})
	}
}

// The versions the assembly actually uses must keep working. A rule this close to
// the six fields is one typo away from refusing every cluster.
func TestPolicy_WellFormedVersionIsAllowed(t *testing.T) {
	for _, version := range []string{"v1.34.11", "v1.30.0", "v1.34.11+k0s.0", "v1.34.11-rc.1"} {
		t.Run(version, func(t *testing.T) {
			got := decide(t, fieldsPolicy, tests.Request{Object: cluster(func(o map[string]any) {
				topology(o)["version"] = version
			})})
			require.True(t, got.Allowed, "version %q was refused: %s", version, got.Message)
		})
	}
}

// An existing Cluster whose stored version is malformed must stay editable. The
// rule fires on a change to the field, not on its presence, or a cluster created
// before the rule existed could never be updated again, including to fix it.
func TestPolicy_AMalformedVersionAlreadyStoredDoesNotLockTheCluster(t *testing.T) {
	stored := cluster(func(o map[string]any) { topology(o)["version"] = "1.34.11" })
	edited := cluster(func(o map[string]any) {
		topology(o)["version"] = "1.34.11"
		topology(o)["workers"] = map[string]any{"machineDeployments": []any{
			map[string]any{"class": "default", "name": "default", "replicas": int64(3)},
		}}
	})
	got := decide(t, fieldsPolicy, tests.Request{Object: edited, OldObject: stored})
	require.True(t, got.Allowed, "an unrelated edit was refused: %s", got.Message)
}
