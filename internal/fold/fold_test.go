package fold_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sixfields/internal/fixture"
	"sixfields/internal/fold"
	"sixfields/internal/golden"
	"sixfields/internal/snapshot"
)

const fixturesDir = "../../testdata/fixtures"
const goldenDir = "../../testdata/golden/fold"

// Every envelope of every scenario folds to a checked-in result. Re-recording a
// fixture changes these files and nothing else, which is what makes recorded
// fixtures usable as the source of truth.
func TestFold_EveryFixtureMatchesItsGolden(t *testing.T) {
	scenarios, err := fixture.Scenarios(fixturesDir)
	require.NoError(t, err)
	require.NotEmpty(t, scenarios)

	for _, name := range scenarios {
		t.Run(name, func(t *testing.T) {
			envelopes, err := fixture.Load(filepath.Join(fixturesDir, name))
			require.NoError(t, err)
			require.NotEmpty(t, envelopes, "scenario has no envelopes")
			for _, env := range envelopes {
				res := fold.Fold(env, optionsFor(env))
				golden.JSON(t, filepath.Join(goldenDir, name, fixture.Filename(env)), res)
			}
		})
	}
}

func optionsFor(env snapshot.Envelope) fold.Options {
	return fold.Options{Now: fixture.NowFor(env)}
}

func lastEnvelope(t *testing.T, scenario string) snapshot.Envelope {
	t.Helper()
	envelopes, err := fixture.Load(filepath.Join(fixturesDir, scenario))
	require.NoError(t, err)
	require.NotEmpty(t, envelopes)
	return envelopes[len(envelopes)-1]
}

func TestFold_HappyPathEndsReady(t *testing.T) {
	for _, scenario := range []string{"std-docker-happy", "inmem-happy", "hosted-docker-happy"} {
		t.Run(scenario, func(t *testing.T) {
			env := lastEnvelope(t, scenario)
			res := fold.Fold(env, optionsFor(env))
			require.True(t, res.Ready, "phases: %+v", res.Phases)
			for _, p := range res.Phases {
				require.Equal(t, fold.Done, p.State, "%s", p.Name)
			}
		})
	}
}

func TestFold_FourPhasesInOrder(t *testing.T) {
	env := lastEnvelope(t, "std-docker-happy")
	res := fold.Fold(env, optionsFor(env))
	require.Len(t, res.Phases, 4)
	for i, name := range fold.Order {
		require.Equal(t, name, res.Phases[i].Name)
	}
}

// A phase is stalled when nothing contributing to it has changed for longer than
// stallAfter. Every induced-stall fixture must reach that state, and no happy-path
// fixture may.
func TestFold_StallFixturesStall(t *testing.T) {
	stalls := map[string]fold.PhaseName{
		"stall-bad-version":  fold.ControlPlane,
		"stall-cp-killed":    fold.ControlPlane,
		"inmem-stall-vm":     fold.ControlPlane,
		"hosted-stall-pod":   fold.ControlPlane,
		"stall-bad-variable": fold.Infrastructure,
	}
	for scenario, want := range stalls {
		t.Run(scenario, func(t *testing.T) {
			env := lastEnvelope(t, scenario)
			res := fold.Fold(env, optionsFor(env))
			stalled, ok := res.Stalled()
			require.True(t, ok, "no phase stalled: %+v", res.Phases)
			require.Equal(t, want, stalled.Name)
		})
	}
}

func TestFold_HappyPathNeverStalls(t *testing.T) {
	for _, scenario := range []string{"std-docker-happy", "inmem-happy", "hosted-docker-happy", "scale-up"} {
		t.Run(scenario, func(t *testing.T) {
			envelopes, err := fixture.Load(filepath.Join(fixturesDir, scenario))
			require.NoError(t, err)
			for _, env := range envelopes {
				res := fold.Fold(env, optionsFor(env))
				_, stalled := res.Stalled()
				require.False(t, stalled, "t+%ds stalled on a happy path", env.Meta.TPlusS)
			}
		})
	}
}

// Stall detection latency: the stall line must appear within stallAfter + 10s of
// the last transition (D2.8).
func TestUX_StallDetectionLatency(t *testing.T) {
	env := lastEnvelope(t, "inmem-stall-vm")
	res := fold.Fold(env, fold.Options{Now: fixture.NowFor(env).Add(time.Hour), StallAfter: time.Minute})
	stalled, ok := res.Stalled()
	require.True(t, ok)

	const stallAfter = time.Minute
	last := stalled.LastTransition
	require.False(t, last.IsZero())

	// Just before the deadline: not yet stalled. Just after: stalled.
	before := fold.Fold(env, fold.Options{Now: last.Add(stallAfter - time.Second), StallAfter: stallAfter})
	_, early := before.Stalled()
	require.False(t, early, "stalled before stallAfter elapsed")

	after := fold.Fold(env, fold.Options{Now: last.Add(stallAfter + 10*time.Second), StallAfter: stallAfter})
	_, late := after.Stalled()
	require.True(t, late, "not stalled by stallAfter + 10s")
}

// The hosted control plane reports readiness, not node counts (PLAN.md Phase 4).
func TestFold_HostedControlPlaneReportsReadinessNotNodes(t *testing.T) {
	env := lastEnvelope(t, "hosted-docker-happy")
	res := fold.Fold(env, optionsFor(env))
	cp, ok := res.Phase(fold.ControlPlane)
	require.True(t, ok)
	require.Contains(t, cp.Detail, "hosted")
	require.NotContains(t, cp.Detail, "nodes")

	self := lastEnvelope(t, "std-docker-happy")
	selfRes := fold.Fold(self, optionsFor(self))
	selfCP, _ := selfRes.Phase(fold.ControlPlane)
	require.Contains(t, selfCP.Detail, "nodes")
}

// Scaling a pool takes the workers phase from done back to running, and the phase
// detail must say so rather than silently staying done.
func TestFold_ScaleUpReopensTheWorkersPhase(t *testing.T) {
	envelopes, err := fixture.Load(filepath.Join(fixturesDir, "scale-up"))
	require.NoError(t, err)
	require.Len(t, envelopes, 3)

	states := make([]fold.State, 0, 3)
	for _, env := range envelopes {
		res := fold.Fold(env, optionsFor(env))
		p, ok := res.Phase(fold.Workers)
		require.True(t, ok)
		states = append(states, p.State)
	}
	require.Equal(t, []fold.State{fold.Done, fold.Running, fold.Done}, states)
}

// A cluster with no workers says so instead of showing an empty row.
func TestFold_NoWorkersSaysSo(t *testing.T) {
	env := snapshot.Envelope{Objects: []snapshot.Object{{
		"apiVersion": "cluster.x-k8s.io/v1beta2",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": "empty", "namespace": "default"},
		"spec":       map[string]any{"topology": map[string]any{"classRef": map[string]any{"name": "std"}}},
		"status":     map[string]any{},
	}}}
	res := fold.Fold(env, fold.Options{Now: fixture.T0})
	p, ok := res.Phase(fold.Workers)
	require.True(t, ok)
	require.Equal(t, fold.Done, p.State)
	require.Equal(t, "no workers", p.Detail)
}

// CLAUDE.md rule 2: never invent API details. Every condition type name the fold
// package uses must exist in the generated snapshot of the pinned release. This is
// the test that makes the rule enforceable rather than aspirational.
func TestFold_OnlyUsesSnapshotConditions(t *testing.T) {
	known := conditionValues(t)
	require.NotEmpty(t, known)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "conditions.go", nil, 0)
	require.NoError(t, err)

	checked := 0
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		require.True(t, known[value],
			"%s = %q is not a condition type in docs/api-snapshot.json; run make api-snapshot or fix the name",
			vs.Names[0].Name, value)
		checked++
		return true
	})
	require.NotZero(t, checked, "no condition constants were checked")
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

// CLAUDE.md rule 4: fixtures are recorded, not written. A synthetic one is the
// exception and must declare itself and say what it stands in for, so a reader
// can tell at a glance which assertions rest on a real run.
func TestFixtures_SyntheticAreDeclared(t *testing.T) {
	scenarios, err := fixture.Scenarios(fixturesDir)
	require.NoError(t, err)

	recorded := 0
	for _, name := range scenarios {
		envelopes, err := fixture.Load(filepath.Join(fixturesDir, name))
		require.NoError(t, err)
		require.NotEmpty(t, envelopes, name)

		for _, env := range envelopes {
			require.NotEmpty(t, env.Meta.Note, "%s/%s has no note saying where it came from",
				name, fixture.Filename(env))
			if env.Meta.Synthetic {
				// Two honest kinds of synthetic: one that stands in for a run
				// nobody has recorded yet, and one built by hand to isolate a rule
				// no real run would produce on demand.
				require.Regexp(t, `^(stands in for|hand-built|hand-made)`, env.Meta.Note,
					"%s is synthetic but its note does not say which kind it is", name)
				continue
			}
			require.NotEmpty(t, env.Meta.RecordedAt,
				"%s is not marked synthetic, so it must say when it was recorded", name)
		}
		if !envelopes[0].Meta.Synthetic {
			recorded++
		}
	}
	require.NotZero(t, recorded, "no fixture has been recorded from a real run")
}
