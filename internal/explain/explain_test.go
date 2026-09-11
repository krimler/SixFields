package explain_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sixfields/internal/explain"
	"sixfields/internal/fixture"
	"sixfields/internal/fold"
	"sixfields/internal/msg"
	"sixfields/internal/snapshot"
	"sixfields/internal/why"
)

const fixturesDir = "../../testdata/fixtures"

var stallScenarios = []string{
	"stall-bad-version", "stall-cp-killed", "stall-bad-variable",
	"inmem-stall-etcd", "inmem-stall-node", "hosted-stall-pod", "two-stalls",
}

func requestFor(t *testing.T, scenario string) explain.Request {
	t.Helper()
	envelopes, err := fixture.Load(filepath.Join(fixturesDir, scenario))
	require.NoError(t, err)
	env := envelopes[len(envelopes)-1]
	now := fixture.NowFor(env)

	res := fold.Fold(env, fold.Options{Now: now})
	stall, ok := why.Rank(env, res, why.Options{Now: now})
	require.True(t, ok, scenario)

	runbook, _ := msg.Runbook(stall.Code)
	return explain.Request{
		Code: stall.Code, Stall: stall, Phases: res.Phases,
		Runbook: runbook, Names: objectNames(env),
	}
}

func objectNames(env snapshot.Envelope) []string {
	out := make([]string, 0, len(env.Objects))
	for _, o := range env.Objects {
		out = append(out, o.Kind()+"/"+o.Name(), o.Name())
	}
	return out
}

// The noop backend exists so every AI code path is exercised without a model.
// It must produce something that passes both gates, or the gates are untested.
func TestExplain_NoopPassesTheSameGatesAsAModel(t *testing.T) {
	for _, scenario := range stallScenarios {
		t.Run(scenario, func(t *testing.T) {
			req := requestFor(t, scenario)
			got, err := explain.Noop{}.Explain(context.Background(), req)
			require.NoError(t, err)
			require.NoError(t, got.Validate(req.Code))
			require.NoError(t, got.Grounded(req))
		})
	}
}

// The grounding check is the deterministic half of the AI story: an explanation
// that names an object the analyzer never saw fails the build, not the user.
func TestExplain_GroundingRejectsAnInventedName(t *testing.T) {
	req := requestFor(t, "inmem-stall-etcd")
	invented := explain.Explanation{
		Code: string(req.Code),
		Lines: []string{
			"Machine/totally-made-up is not coming up.",
			"Its disk is full.",
			"The control plane finishes when it starts.",
		},
		NextCommand: "kubectl get machines",
	}
	require.NoError(t, invented.Validate(req.Code))
	err := invented.Grounded(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "totally-made-up")
}

func TestExplain_GroundingRejectsAnInventedNumber(t *testing.T) {
	req := requestFor(t, "inmem-stall-etcd")
	wrong := explain.Explanation{
		Code: string(req.Code),
		Lines: []string{
			"The control plane is waiting.",
			"It has been 47m since anything changed.",
			"It finishes when etcd starts.",
		},
		NextCommand: "cluster status inmem-1",
	}
	err := wrong.Grounded(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "47m")
}

// The schema is a contract with whatever produced the output, model or not.
func TestExplain_ValidateEnforcesTheSchema(t *testing.T) {
	good := explain.Explanation{
		Code:        "CAPI-CP-003",
		Lines:       []string{"one", "two", "three"},
		NextCommand: "kubectl get devmachine x -o yaml",
	}
	require.NoError(t, good.Validate("CAPI-CP-003"))

	for name, mutate := range map[string]func(*explain.Explanation){
		"wrong code":       func(e *explain.Explanation) { e.Code = "CAPI-CP-001" },
		"two lines":        func(e *explain.Explanation) { e.Lines = e.Lines[:2] },
		"four lines":       func(e *explain.Explanation) { e.Lines = append(e.Lines, "four") },
		"empty line":       func(e *explain.Explanation) { e.Lines[1] = "" },
		"newline in line":  func(e *explain.Explanation) { e.Lines[0] = "a\nb" },
		"over budget":      func(e *explain.Explanation) { e.Lines[0] = strings.Repeat("x", 161) },
		"no next command":  func(e *explain.Explanation) { e.NextCommand = "" },
		"prose as command": func(e *explain.Explanation) { e.NextCommand = "you should look at the machine" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := good
			bad.Lines = append([]string{}, good.Lines...)
			mutate(&bad)
			require.Error(t, bad.Validate("CAPI-CP-003"))
		})
	}
}

// Anonymisation must be exactly reversible on every fixture, or a user reads keys
// instead of their own object names.
func TestExplain_AnonymizeRoundTripsOnEveryFixture(t *testing.T) {
	for _, tl := range fixture.All() {
		t.Run(tl.Name, func(t *testing.T) {
			env := tl.Envelopes[len(tl.Envelopes)-1]
			anon := explain.NewAnonymizer()
			anon.Learn(objectNames(env)...)

			for _, o := range env.Objects {
				original := o.Kind() + "/" + o.Name() + " in " + o.Namespace()
				hidden := anon.Hide(original)
				require.NotContains(t, hidden, o.Name(), "the real name survived anonymisation")
				require.Equal(t, original, anon.Reveal(hidden))
			}
		})
	}
}

func TestExplain_AnonymizeHidesNamesFromTheRequest(t *testing.T) {
	req := requestFor(t, "inmem-stall-etcd")
	anon := explain.NewAnonymizer()
	anon.Learn(req.Names...)

	hidden := anon.HideRequest(req)
	prompt := explain.Prompt(hidden)
	require.NotContains(t, prompt, "inmem-1-cp-abcde")
	require.Contains(t, prompt, "obj-")

	// And the answer comes back in the user's own vocabulary.
	answer := explain.Explanation{
		Code:        string(req.Code),
		Lines:       []string{anon.Hide("DevMachine/inmem-1-cp-abcde is stuck."), "b", "c"},
		NextCommand: "cluster status " + anon.Hide("inmem-1"),
	}
	revealed := anon.RevealExplanation(answer)
	require.Contains(t, revealed.Lines[0], "DevMachine/inmem-1-cp-abcde")
	require.Equal(t, "cluster status inmem-1", revealed.NextCommand)
}

// The prompt carries the analyzer's finding and the runbook, and nothing a user
// typed. There is no free-text path into a model in this tool.
func TestExplain_PromptCarriesOnlyAnalyzerOutput(t *testing.T) {
	req := requestFor(t, "stall-bad-version")
	prompt := explain.Prompt(req)
	require.Contains(t, prompt, string(req.Code))
	require.Contains(t, prompt, req.Stall.Object.String())
	require.Contains(t, prompt, "Runbook for this stall class")
	require.Contains(t, prompt, "Every object name and every number you write must")
}

// A cassette answers without a network. This is what CI runs.
func TestExplain_CassetteReplaysWithoutAModel(t *testing.T) {
	dir := t.TempDir()
	req := requestFor(t, "inmem-stall-etcd")

	recorder := explain.Cassette{Dir: dir, Record: true, Inner: explain.Noop{}}
	recorded, err := recorder.Explain(context.Background(), req)
	require.NoError(t, err)

	replay := explain.Cassette{Dir: dir}
	got, err := replay.Explain(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, recorded, got)

	// A cassette that does not exist is an error with a fix in it, never a call.
	other := requestFor(t, "stall-bad-version")
	_, err = replay.Explain(context.Background(), other)
	require.Error(t, err)
	require.Contains(t, err.Error(), "make test-llm")
}

// The cache key is the stall, so an identical stall is answered once and two
// different stalls never share an answer.
func TestExplain_KeyIsStable(t *testing.T) {
	a := requestFor(t, "inmem-stall-etcd")
	b := requestFor(t, "inmem-stall-node")
	require.Equal(t, explain.Key(a), explain.Key(a))
	require.NotEqual(t, explain.Key(a), explain.Key(b))
}

func TestExplain_ModeParsing(t *testing.T) {
	for input, want := range map[string]explain.Mode{
		"": explain.ExplainOne, "explain": explain.ExplainOne,
		"off": explain.Off, "OFF": explain.Off, "all": explain.All,
	} {
		got, err := explain.ParseMode(input)
		require.NoError(t, err, input)
		require.Equal(t, want, got, input)
	}
	_, err := explain.ParseMode("sometimes")
	require.Error(t, err)
	require.Contains(t, err.Error(), "off, explain or all")
}

// --redact-ips is one-way on purpose: an address replaced by a placeholder cannot
// be revealed, so a model can never repeat one back.
func TestExplain_RedactIPsIsOneWay(t *testing.T) {
	anon := explain.NewAnonymizer()
	anon.RedactIPs = true
	anon.Learn("dev-1")

	const text = "dev-1 endpoint 10.96.0.1:6443 is unreachable"
	hidden := anon.Hide(text)
	require.NotContains(t, hidden, "10.96.0.1")
	require.NotContains(t, hidden, "dev-1")
	require.Contains(t, hidden, "<ip>")

	revealed := anon.Reveal(hidden)
	require.Contains(t, revealed, "dev-1", "names come back")
	require.NotContains(t, revealed, "10.96.0.1", "addresses do not")
}

// With redaction off — the default for the local backend — an address survives,
// because it is often the whole answer.
func TestExplain_AddressesSurviveWhenRedactionIsOff(t *testing.T) {
	anon := explain.NewAnonymizer()
	require.Contains(t, anon.Hide("endpoint 10.96.0.1:6443"), "10.96.0.1")
}

// The cassettes are recorded from the pinned local model (versions.env), and this
// replays them in `make test` — no network, no model, no key. Every one must pass
// the same two gates a live answer does, so a model that starts inventing object
// names fails the build the next time cassettes are re-recorded.
func TestExplain_RecordedAnswersAreValidAndGrounded(t *testing.T) {
	cassettes := explain.Cassette{Dir: "../../testdata/cassettes"}

	for _, scenario := range stallScenarios {
		t.Run(scenario, func(t *testing.T) {
			req := requestFor(t, scenario)
			answer, err := cassettes.Explain(context.Background(), req)
			require.NoError(t, err, "no cassette for %s; record one with: CLUSTER_AI_RECORD=1 make test-llm", scenario)

			require.NoError(t, answer.Validate(req.Code), "%+v", answer)
			require.NoError(t, answer.Grounded(req), "%+v", answer)

			// The model explains; it does not decide. The code it returns is the
			// one the analyzer chose.
			require.Equal(t, string(req.Code), answer.Code)
		})
	}
}
