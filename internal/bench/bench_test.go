//go:build bench

package bench

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sixfields/internal/explain"
	"sixfields/internal/snapshot"
)

// The default needs no model and no network, so `make bench` is free and offline
// until someone asks for a model.
var backends = flag.String("backends", "noop,cassette",
	"comma-separated backends to score: noop, cassette, local, anthropic")

func TestBench(t *testing.T) {
	tasks, err := loadTasks()
	require.NoError(t, err)

	var runs []backendRun
	for _, name := range strings.Split(*backends, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		backend, err := backendFor(name)
		require.NoError(t, err)
		runs = append(runs, backendRun{name: name, results: run(backend, tasks)})
	}
	require.NotEmpty(t, runs, "--backends selected nothing")

	// The report is what `make bench` produces, so it goes to stdout whole.
	// t.Log would indent every line of it under the test name.
	fmt.Print(render(tasks, runs))

	// Two pre-registered failures, and no others. A benchmark that goes red on a
	// model's score is a benchmark nobody can quote a score from.
	for _, r := range runs {
		s := summarize(r)
		if s.backend == "noop" {
			// The baseline is the analyzer's own answer read back. Anything short of
			// a clean sweep is this harness disagreeing with itself.
			require.Equal(t, s.tasks, s.passed, "the no-AI baseline missed a task it wrote itself")
		}
		require.NotEqual(t, s.tasks, s.framework,
			"%s answered none of the %d tasks; it was not measured, it was absent", s.backend, s.tasks)
	}
}

func firstTask(t *testing.T) task {
	t.Helper()
	tasks, err := loadTasks()
	require.NoError(t, err)
	return tasks[0]
}

// The two labels are the report's most useful column, so each one has a test that
// puts an answer in front of the scorer and reads the label back.

func TestBench_NamingTheWrongStallClassIsAReasoningError(t *testing.T) {
	task := firstTask(t)
	answer, err := explain.Noop{}.Explain(context.Background(), task.request)
	require.NoError(t, err)

	answer.Code = "CAPI-ADDON-001"
	require.NotEqual(t, string(task.code), answer.Code)

	got, detail := score(task, answer, nil)
	require.Equal(t, reasoningError, got)
	require.Contains(t, detail, string(task.code))
}

func TestBench_NamingNoObjectIsAReasoningError(t *testing.T) {
	task := firstTask(t)
	answer := explain.Explanation{
		Code: string(task.code),
		Lines: []string{
			"Something in the control plane is blocking progress.",
			"It has not reported a reason.",
			"The phase finishes when it does.",
		},
		NextCommand: "cluster status",
	}
	got, detail := score(task, answer, nil)
	require.Equal(t, reasoningError, got)
	require.Contains(t, detail, task.object.String())
}

func TestBench_OutputThatBreaksTheSchemaIsAFrameworkError(t *testing.T) {
	task := firstTask(t)
	for name, answer := range map[string]explain.Explanation{
		"two lines": {
			Code:        string(task.code),
			Lines:       []string{"one", "two"},
			NextCommand: "cluster status",
		},
		"a code no registry entry has": {
			Code:        "CAPI-MADE-UP",
			Lines:       []string{"one", "two", "three"},
			NextCommand: "cluster status",
		},
		"prose where a command belongs": {
			Code:        string(task.code),
			Lines:       []string{"one", "two", "three"},
			NextCommand: "have a look at the machine",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, detail := score(task, answer, nil)
			require.Equal(t, frameworkError, got)
			require.NotEmpty(t, detail)
		})
	}
}

func TestBench_ABackendThatNeverAnswersIsAFrameworkError(t *testing.T) {
	task := firstTask(t)

	got, detail := score(task, explain.Explanation{}, fmt.Errorf("local model at %s: %w", "http://127.0.0.1:1234/v1", context.DeadlineExceeded))
	require.Equal(t, frameworkError, got)
	require.Contains(t, detail, explain.Timeout.String())

	got, detail = score(task, explain.Explanation{}, fmt.Errorf("connection refused"))
	require.Equal(t, frameworkError, got)
	require.Contains(t, detail, "connection refused")
}

// The pinned local model writes object references both ways. Marking one of them
// wrong would report a reasoning error where the model reasoned correctly.
func TestBench_AnObjectNamedWithoutASlashStillCounts(t *testing.T) {
	ref := snapshot.Ref{Kind: "DevMachine", Name: "dev-3-zqx65-zlwhb"}
	for _, line := range []string{
		"DevMachine/dev-3-zqx65-zlwhb is blocking control plane progress.",
		"DevMachine dev-3-zqx65-zlwhb is blocking control plane progress.",
		"The machine (dev-3-zqx65-zlwhb) is blocking control plane progress.",
	} {
		require.True(t, namesObject(explain.Explanation{Lines: []string{line}}, ref), line)
	}
	require.False(t, namesObject(explain.Explanation{
		Lines: []string{"DevMachine/dev-3 is blocking control plane progress."},
	}, ref), "a prefix of the name is a different object")
}
