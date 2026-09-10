package tests_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// CLAUDE.md rule 5: every denied field has a negative test. This is what makes
// that checkable rather than aspirational — the list comes out of the policy, so
// adding a denial without adding a case here fails the build.
func TestPolicy_EveryDeniedPathHasACase(t *testing.T) {
	covered := map[string]bool{}
	for path := range deniedPaths {
		covered[path] = true
	}

	for _, want := range deniedPathsFromPolicy(t) {
		require.True(t, covered[want],
			"%s is denied by policy/vap/cluster-fields.yaml but has no case in deniedPaths", want)
	}
}

// Every case in the table must correspond to something the policy actually
// denies, or the table is testing a rule that is no longer there.
func TestPolicy_NoCaseTestsAVanishedRule(t *testing.T) {
	declared := map[string]bool{}
	for _, path := range deniedPathsFromPolicy(t) {
		declared[path] = true
	}
	// These two are denied by a rule rather than by a key list: an unknown
	// variable name, and a control-plane replica count that disagrees with size.
	declared["spec.topology.variables[unknown]"] = true
	declared["spec.topology.controlPlane.replicas"] = true

	for path := range deniedPaths {
		require.True(t, declared[path],
			"deniedPaths has a case for %s, which the policy no longer denies", path)
	}
}

// deniedPathsFromPolicy reads the key lists the policy enumerates. They exist in
// the YAML so the denial message is deterministic; here they double as the
// coverage list.
func deniedPathsFromPolicy(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(vapDir + "/cluster-fields.yaml")
	require.NoError(t, err)
	text := string(b)

	var out []string
	for _, list := range []struct{ variable, prefix string }{
		{"managedSpecKeys", "spec."},
		{"managedControlPlaneKeys", "spec.topology.controlPlane."},
	} {
		for _, key := range keyList(t, text, list.variable) {
			out = append(out, list.prefix+key)
		}
	}
	// A worker entry may carry exactly the allowed keys; anything else is denied,
	// and the table covers one field per pool kind.
	out = append(out,
		"spec.topology.workers.machineDeployments[].failureDomain",
		"spec.topology.workers.machineDeployments[].rollout",
		"spec.topology.workers.machinePools[].minReadySeconds",
	)
	require.NotEmpty(t, out)
	return out
}

var keyListPattern = regexp.MustCompile(`'([a-zA-Z][a-zA-Z0-9]*)'`)

func keyList(t *testing.T, text, variable string) []string {
	t.Helper()
	marker := "- name: " + variable + "\n"
	start := strings.Index(text, marker)
	require.NotEqual(t, -1, start, "the policy has no variable %q", variable)

	rest := text[start+len(marker):]
	if next := strings.Index(rest, "\n    - name: "); next > 0 {
		rest = rest[:next]
	}
	var out []string
	for _, match := range keyListPattern.FindAllStringSubmatch(rest, -1) {
		out = append(out, match[1])
	}
	require.NotEmpty(t, out, "no keys found in variable %q", variable)
	return out
}
