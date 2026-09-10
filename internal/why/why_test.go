package why_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"capi-distro/internal/fixture"
	"capi-distro/internal/fold"
	"capi-distro/internal/golden"
	"capi-distro/internal/msg"
	"capi-distro/internal/snapshot"
	"capi-distro/internal/why"
)

const fixturesDir = "../../testdata/fixtures"
const goldenDir = "../../testdata/golden/why"

func lastEnvelope(t *testing.T, scenario string) snapshot.Envelope {
	t.Helper()
	envelopes, err := fixture.Load(filepath.Join(fixturesDir, scenario))
	require.NoError(t, err)
	require.NotEmpty(t, envelopes)
	return envelopes[len(envelopes)-1]
}

func rank(t *testing.T, scenario string) why.Stall {
	t.Helper()
	env := lastEnvelope(t, scenario)
	now := fixture.T0.Add(time.Duration(env.Meta.TPlusS) * time.Second)
	res := fold.Fold(env, fold.Options{Now: now})
	stall, ok := why.Rank(env, res, why.Options{Now: now})
	require.True(t, ok, "nothing ranked for %s", scenario)
	return stall
}

// One golden per stall scenario. The golden is the whole ranking, so a change to
// the ranking rules shows up as a reviewable diff rather than a passing test.
func TestWhy_EveryStallFixtureMatchesItsGolden(t *testing.T) {
	for _, scenario := range []string{
		"stall-bad-version", "stall-cp-killed", "stall-bad-variable",
		"inmem-stall-etcd", "inmem-stall-node", "hosted-stall-pod", "two-stalls",
	} {
		t.Run(scenario, func(t *testing.T) {
			golden.JSON(t, filepath.Join(goldenDir, scenario+".json"), rank(t, scenario))
		})
	}
}

// The intended object ranks first in every stall fixture. This is the assertion
// that would survive a rewrite of the ranker.
func TestUX_StallLineNamesTheRightObject(t *testing.T) {
	for _, tc := range []struct {
		scenario string
		object   string
		code     msg.Code
	}{
		{"stall-bad-version", "DevMachine/dev-1-cp-abcde", msg.VersionUnavailable},
		{"stall-cp-killed", "DevMachine/dev-1-cp-abcde", msg.ControlPlaneMachine},
		{"stall-bad-variable", "Cluster/dev-1", msg.TopologyFailed},
		{"inmem-stall-etcd", "DevMachine/inmem-1-cp-abcde", msg.EtcdNotHealthy},
		// A node that never became ready is the same problem, and the same runbook,
		// whether it is a control-plane node or a worker.
		{"inmem-stall-node", "DevMachine/inmem-1-cp-abcde", msg.NodeNotJoining},
		{"hosted-stall-pod", "K0smotronControlPlane/hosted-1-cp", msg.ControlPlaneNotInit},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			stall := rank(t, tc.scenario)
			require.Equal(t, tc.object, stall.Object.String())
			require.Equal(t, tc.code, stall.Code)
		})
	}
}

// Two objects failing at once, at different depths and different ages. The more
// specific one wins, and the loser records why it lost.
func TestUX_RankingPrefersTheMoreSpecificObject(t *testing.T) {
	stall := rank(t, "two-stalls")
	require.Equal(t, "DevMachine/dev-1-cp-abcde", stall.Object.String())

	var clusterCandidate *why.Candidate
	for i := range stall.Candidates {
		if stall.Candidates[i].Object.Kind == "Cluster" {
			clusterCandidate = &stall.Candidates[i]
		}
	}
	require.NotNil(t, clusterCandidate, "the Cluster must still be a candidate")
	require.Equal(t, "less specific object", clusterCandidate.LostTo)
}

// The stall line contract: one line, at most 120 characters, naming kind/name and
// an elapsed time, with no raw CAPI condition type in it (D2.2, D2.5).
func TestUX_StallLineContract(t *testing.T) {
	for _, scenario := range []string{
		"stall-bad-version", "stall-cp-killed", "stall-bad-variable",
		"inmem-stall-etcd", "inmem-stall-node", "hosted-stall-pod", "two-stalls",
	} {
		t.Run(scenario, func(t *testing.T) {
			stall := rank(t, scenario)
			line := stall.Line()
			require.LessOrEqual(t, len(line), 120, "%q", line)
			require.NotContains(t, line, "\n")
			require.Contains(t, line, stall.Object.String())
			require.NotEmpty(t, stall.Raw)
			require.True(t, strings.HasPrefix(stall.Raw, "kubectl get "), "%q", stall.Raw)
			require.NotContains(t, line, stall.ConditionType,
				"the raw condition type belongs on the raw: line, not in the message")
		})
	}
}

// Long upstream messages are truncated at a word boundary and the full text is
// kept for --verbose.
func TestWhy_LongMessagesAreTruncatedAndKept(t *testing.T) {
	env := lastEnvelope(t, "inmem-stall-etcd")
	for _, o := range env.Objects {
		if o.Kind() != "DevMachine" {
			continue
		}
		conds, _ := o.Slice("status", "conditions")
		for _, c := range conds {
			m := c.(map[string]any)
			if m["type"] == "EtcdProvisioned" {
				m["message"] = strings.Repeat("etcd is still starting up and has not reported healthy ", 4)
			}
		}
	}
	now := fixture.T0.Add(time.Duration(env.Meta.TPlusS) * time.Second)
	res := fold.Fold(env, fold.Options{Now: now})
	stall, ok := why.Rank(env, res, why.Options{Now: now})
	require.True(t, ok)
	require.LessOrEqual(t, len(stall.Message), why.MessageBudget)
	require.True(t, strings.HasSuffix(stall.Message, "…"))
	require.NotEmpty(t, stall.FullMessage)
	require.Greater(t, len(stall.FullMessage), len(stall.Message))
}

// A healthy cluster has nothing to rank.
func TestWhy_ReadyClusterRanksNothing(t *testing.T) {
	env := lastEnvelope(t, "std-docker-happy")
	now := fixture.T0.Add(time.Duration(env.Meta.TPlusS) * time.Second)
	res := fold.Fold(env, fold.Options{Now: now})
	_, ok := why.Rank(env, res, why.Options{Now: now})
	require.False(t, ok)
}

// Every code the ranker can emit has a runbook and a long form. A stall class with
// nowhere to send the user is a bug.
func TestUX_EveryStallCodeHasARunbook(t *testing.T) {
	for _, scenario := range []string{
		"stall-bad-version", "stall-cp-killed", "stall-bad-variable",
		"inmem-stall-etcd", "inmem-stall-node", "hosted-stall-pod", "two-stalls",
	} {
		stall := rank(t, scenario)
		_, ok := msg.Longform(stall.Code)
		require.True(t, ok, "%s emits %s which has no long form", scenario, stall.Code)
		require.FileExists(t, filepath.Join("..", "..", "docs", "runbooks", string(stall.Code)+".md"),
			"%s emits %s which has no runbook", scenario, stall.Code)
	}
}

// CLAUDE.md rule 2, for the ranker: every condition type the classifier keys off
// must exist in the pinned release. The names live in two packages, so both are
// checked the same way.
func TestWhy_OnlyUsesSnapshotConditions(t *testing.T) {
	known := conditionValues(t)
	require.NotEmpty(t, known)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../why/why.go", nil, 0)
	require.NoError(t, err)

	checked := 0
	ast.Inspect(f, func(n ast.Node) bool {
		entry, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		lit, ok := entry.Key.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		// Only the condition maps hold condition names; everything else in this
		// file keyed by a string is a kind, which the snapshot also knows.
		if !known[value] && !isKind(value) {
			t.Errorf("%q is neither a condition type nor a kind in docs/api-snapshot.json", value)
		}
		checked++
		return true
	})
	require.NotZero(t, checked)
}

func isKind(value string) bool {
	for _, kind := range []string{
		"Machine", "DevMachine", "DockerMachine", "KubeadmConfig", "K0sWorkerConfig",
		"MachineSet", "MachineDeployment", "MachinePool", "DevMachinePool",
		"KubeadmControlPlane", "K0sControlPlane", "K0smotronControlPlane",
		"DevCluster", "DockerCluster", "ClusterResourceSetBinding", "Cluster",
	} {
		if value == kind {
			return true
		}
	}
	return false
}

func conditionValues(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile("../../docs/api-snapshot.json")
	require.NoError(t, err)
	var snap struct {
		Entries []struct {
			Class string `json:"class"`
			Value string `json:"value"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(b, &snap))
	out := map[string]bool{}
	for _, e := range snap.Entries {
		if e.Class == "condition" {
			out[e.Value] = true
		}
	}
	return out
}

// A condition that reports an activity — Paused, Deleting, ScalingUp — is False
// whenever nothing is happening, which is most of the time. Ranking that as a
// failure is how the first live run named "Paused=False" as the blocking
// condition on a machine whose node had genuinely not come up.
func TestUX_ActivityConditionsAreNotFailures(t *testing.T) {
	env := lastEnvelope(t, "inmem-stall-etcd")
	for _, o := range env.Objects {
		if o.Kind() != "DevMachine" {
			continue
		}
		conditions, _ := o.Slice("status", "conditions")
		for _, name := range []string{"Paused", "Deleting", "ScalingUp", "Updating"} {
			conditions = append(conditions, map[string]any{
				"type": name, "status": "False", "reason": "Not" + name,
				// More recent than the real fault, so it would win on recency.
				"lastTransitionTime": fixture.T0.Add(time.Hour).Format(time.RFC3339),
			})
		}
		o["status"].(map[string]any)["conditions"] = conditions
	}

	now := fixture.T0.Add(time.Duration(env.Meta.TPlusS) * time.Second)
	res := fold.Fold(env, fold.Options{Now: now})
	stall, ok := why.Rank(env, res, why.Options{Now: now})
	require.True(t, ok)
	require.Equal(t, "EtcdProvisioned", stall.ConditionType,
		"an activity condition outranked the real fault")
	for _, c := range stall.Candidates {
		require.NotContains(t, []string{"Paused", "Deleting", "ScalingUp", "Updating"}, c.Condition.Type,
			"%s should not be a candidate at all", c.Condition.Type)
	}
}

// Ready and Available aggregate other conditions: "Ready=False reason=NotReady"
// restates what a specific condition already said. The line a user reads must
// name the specific one.
func TestUX_SummaryConditionsRankBelowSpecificOnes(t *testing.T) {
	env := lastEnvelope(t, "inmem-stall-node")
	for _, o := range env.Objects {
		if o.Kind() != "DevMachine" {
			continue
		}
		conditions, _ := o.Slice("status", "conditions")
		conditions = append(conditions, map[string]any{
			"type": "Ready", "status": "False", "reason": "NotReady",
			"lastTransitionTime": fixture.T0.Add(time.Hour).Format(time.RFC3339),
		})
		o["status"].(map[string]any)["conditions"] = conditions
	}

	now := fixture.T0.Add(time.Duration(env.Meta.TPlusS) * time.Second)
	res := fold.Fold(env, fold.Options{Now: now})
	stall, ok := why.Rank(env, res, why.Options{Now: now})
	require.True(t, ok)
	require.NotEqual(t, "Ready", stall.ConditionType,
		"a summary condition won despite a specific one being available")

	// There is a Ready on more than one object; the one that matters is the one on
	// the object that won, which lost on the condition rather than on the object.
	var summary *why.Candidate
	for i := range stall.Candidates {
		c := &stall.Candidates[i]
		if c.Condition.Type == "Ready" && c.Object == stall.Object {
			summary = c
		}
	}
	require.NotNil(t, summary)
	require.Equal(t, "summary condition", summary.LostTo)
}
