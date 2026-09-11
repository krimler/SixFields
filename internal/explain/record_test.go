//go:build llm

package explain_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"capi-distro/internal/explain"
)

// TestLLM_RecordCassettes runs the pinned local model over every stall fixture and
// writes a cassette for each. It is behind the llm tag: `make test` never calls a
// model, and CI replays what this recorded.
//
//	CLUSTER_AI_RECORD=1 make test-llm
func TestLLM_RecordCassettes(t *testing.T) {
	if os.Getenv("CLUSTER_AI_RECORD") == "" {
		t.Skip("set CLUSTER_AI_RECORD=1 to re-record cassettes from the pinned local model")
	}
	url, model := os.Getenv("CLUSTER_AI_URL"), os.Getenv("CLUSTER_AI_MODEL")
	require.NotEmpty(t, url, "CLUSTER_AI_URL must point at the local runtime")
	require.NotEmpty(t, model, "CLUSTER_AI_MODEL must name the pinned model")

	recorder := explain.Cassette{
		Dir:    "../../testdata/cassettes",
		Record: true,
		Inner:  explain.Local{URL: url, Model: model},
	}

	for _, scenario := range stallScenarios {
		t.Run(scenario, func(t *testing.T) {
			req := requestFor(t, scenario)
			answer, err := recorder.Explain(context.Background(), req)
			require.NoError(t, err)

			// A recorded answer must pass the same two gates a live one does, or
			// the cassette is a record of a failure.
			require.NoError(t, answer.Validate(req.Code), "%+v", answer)
			require.NoError(t, answer.Grounded(req), "%+v", answer)
			t.Logf("%s -> %s", scenario, answer.Lines[0])
		})
	}
}
