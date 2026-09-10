// Package assembly_test checks the rendered ClusterClass, not the kustomize
// inputs: the rendered file is what a cluster sees and what a reviewer reads.
package assembly_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const repoRoot = ".."

var overlays = []string{"docker", "inmemory"}

// render runs the same script `make render` runs, so a test can never pass
// against a rendering nobody else produces.
func render(t *testing.T, overlay string) []map[string]any {
	t.Helper()
	cmd := exec.Command("bash", "hack/render.sh")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "hack/render.sh:\n%s", out)

	b, err := os.ReadFile(filepath.Join(repoRoot, "bin", "render", overlay+".yaml"))
	require.NoError(t, err)
	return documents(t, b)
}

func documents(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, doc := range strings.Split(string(b), "\n---\n") {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var obj map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(doc), &obj), doc)
		out = append(out, obj)
	}
	return out
}

func find(t *testing.T, docs []map[string]any, kind string) map[string]any {
	t.Helper()
	for _, d := range docs {
		if d["kind"] == kind {
			return d
		}
	}
	t.Fatalf("no %s in the rendered overlay", kind)
	return nil
}

// The rendered overlay is what is checked in. A change to the class shows up as a
// diff in a file a person can read, not as a passing test.
func TestAssembly_RenderMatchesGolden(t *testing.T) {
	for _, overlay := range overlays {
		t.Run(overlay, func(t *testing.T) {
			rendered, err := os.ReadFile(filepath.Join(repoRoot, "bin", "render", overlay+".yaml"))
			if err != nil {
				render(t, overlay)
				rendered, err = os.ReadFile(filepath.Join(repoRoot, "bin", "render", overlay+".yaml"))
				require.NoError(t, err)
			}
			want, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "golden", "class-"+overlay+".yaml"))
			require.NoError(t, err, "run: make render && cp bin/render/%s.yaml testdata/golden/class-%s.yaml", overlay, overlay)
			require.Equal(t, string(want), string(rendered),
				"the rendered class has changed; review the diff, then copy bin/render/%s.yaml over the golden", overlay)
		})
	}
}

// The user-facing variables are exactly the ones PLAN.md names. A variable that
// creeps in here is a seventh field the user has to learn.
func TestAssembly_ExposesOnlyTheDocumentedVariables(t *testing.T) {
	for _, overlay := range overlays {
		t.Run(overlay, func(t *testing.T) {
			class := find(t, render(t, overlay), "ClusterClass")
			spec := class["spec"].(map[string]any)
			var names []string
			for _, v := range spec["variables"].([]any) {
				names = append(names, v.(map[string]any)["name"].(string))
			}
			require.ElementsMatch(t, []string{"size", "placement"}, names,
				"size and placement are the whole variable surface")
		})
	}
}

// Every variable's schema rejects an invalid value and accepts its default. The
// class is the first line of defence and the policy is the second; both must
// actually say no.
func TestAssembly_VariableSchemasRejectAndDefault(t *testing.T) {
	class := find(t, render(t, "docker"), "ClusterClass")
	variables := class["spec"].(map[string]any)["variables"].([]any)

	want := map[string]struct {
		enum       []string
		defaultsTo string
	}{
		"size":      {enum: []string{"dev", "ha"}, defaultsTo: "dev"},
		"placement": {enum: []string{"self", "hosted"}, defaultsTo: "self"},
	}

	for _, item := range variables {
		v := item.(map[string]any)
		name := v["name"].(string)
		expect, ok := want[name]
		require.True(t, ok, "undocumented variable %q", name)

		schema := v["schema"].(map[string]any)["openAPIV3Schema"].(map[string]any)
		require.Equal(t, "string", schema["type"], "%s", name)

		// Every variable has a default. CAPI's mutating webhook writes defaults back
		// onto the Cluster, so a variable without one would force a user to set it,
		// and a variable a user must set is a seventh field.
		require.Equal(t, expect.defaultsTo, schema["default"], "%s", name)
		if len(expect.enum) > 0 {
			var got []string
			for _, e := range schema["enum"].([]any) {
				got = append(got, e.(string))
			}
			require.ElementsMatch(t, expect.enum, got,
				"%s must reject anything outside its enum", name)
		}
	}
}

// The two placements differ in as little as possible. PLAN.md Phase 5 asks for
// exactly one class reference; until the hosted class exists, this pins that the
// docker and inmemory overlays differ only in the machine backend.
func TestAssembly_OverlaysDifferOnlyInTheBackend(t *testing.T) {
	docker := find(t, render(t, "docker"), "ClusterClass")
	inmemory := find(t, render(t, "inmemory"), "ClusterClass")
	require.Equal(t, docker["metadata"], inmemory["metadata"],
		"both overlays are the same class; a cluster written for one applies to the other")

	dockerDocs, inmemoryDocs := render(t, "docker"), render(t, "inmemory")
	require.Equal(t, len(dockerDocs), len(inmemoryDocs))

	dockerTemplate := find(t, dockerDocs, "DevMachineTemplate")
	inmemoryTemplate := find(t, inmemoryDocs, "DevMachineTemplate")
	require.Contains(t, backendKeys(t, dockerTemplate), "docker")
	require.Contains(t, backendKeys(t, inmemoryTemplate), "inMemory")
}

func backendKeys(t *testing.T, template map[string]any) []string {
	t.Helper()
	spec := template["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	backend, ok := spec["backend"].(map[string]any)
	require.True(t, ok, "a Dev* template must select a backend")
	var keys []string
	for k := range backend {
		keys = append(keys, k)
	}
	return keys
}

// The deprecated Docker* kinds are scheduled for removal. Naming one anywhere in
// the assembly is a bug that would only show up on a CAPI bump.
func TestAssembly_UsesNoDeprecatedKinds(t *testing.T) {
	for _, overlay := range overlays {
		t.Run(overlay, func(t *testing.T) {
			for _, doc := range render(t, overlay) {
				kind := doc["kind"].(string)
				require.False(t, strings.HasPrefix(kind, "Docker"),
					"%s is deprecated; use the Dev* kinds with spec.backend", kind)
			}
		})
	}
}

// The node image is written into the machine templates, not exposed as a class
// variable: CAPI's mutating webhook defaults every class variable onto the
// Cluster, so a nodeImage variable would appear in an ordinary user's
// spec.topology.variables and the admission policy would reject their write.
func TestAssembly_NodeImageIsInTheTemplateAndComesFromVersionsEnv(t *testing.T) {
	want := pinned(t, "WORKLOAD_NODE_IMAGE")
	require.NotEmpty(t, want)

	docs := render(t, "docker")
	found := 0
	for _, doc := range docs {
		if doc["kind"] != "DevMachineTemplate" {
			continue
		}
		spec := doc["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
		docker, ok := spec["backend"].(map[string]any)["docker"].(map[string]any)
		require.True(t, ok, "%v has no docker backend", doc["metadata"])
		require.Equal(t, want, docker["customImage"],
			"the image drifted from versions.env; run: make render")
		found++
	}
	require.Equal(t, 2, found, "both the control-plane and the worker template carry the image")

	class := find(t, docs, "ClusterClass")
	for _, item := range class["spec"].(map[string]any)["variables"].([]any) {
		require.NotEqual(t, "nodeImage", item.(map[string]any)["name"],
			"nodeImage must not be a class variable; see the comment in the docker overlay")
	}
}

func pinned(t *testing.T, key string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "versions.env"))
	require.NoError(t, err)
	for _, line := range strings.Split(string(b), "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok && name == key {
			return value
		}
	}
	return ""
}

// Installing the assembly writes kinds the policy manages, so every rendered
// object carries one half of the break-glass; hack/dev-up.sh supplies the other
// by impersonating the group. Without this the second `make dev-up` on a machine
// that already has the policy is denied — which is how it was found.
func TestAssembly_RenderedObjectsCarryTheBreakGlassLabel(t *testing.T) {
	for _, overlay := range overlays {
		t.Run(overlay, func(t *testing.T) {
			docs := render(t, overlay)
			require.NotEmpty(t, docs)
			for _, doc := range docs {
				metadata := doc["metadata"].(map[string]any)
				labels, ok := metadata["labels"].(map[string]any)
				require.True(t, ok, "%v has no labels", metadata["name"])
				require.Equal(t, "true", labels["capi-distro.io/break-glass"],
					"%v cannot be installed while the policy is enforcing", metadata["name"])
			}
		})
	}
}
