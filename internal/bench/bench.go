//go:build bench

// Package bench is cluster-bench: a list of diagnosis tasks taken from the
// recorded fixtures, run against each selected explain backend and scored
// against the fixture's own truth.
//
// A task hands a backend the last snapshot of a stalled cluster and asks the
// question a waiting user asks: which object is blocking, and what class of stall
// is this? The answer is right when it names the object and the stall class that
// internal/why ranked over the same fixture. Nothing here grades a transcript and
// nothing here needs a judge, because the truth is already computable.
//
// Behind the bench build tag: a run with a model backend calls a model. `make
// test` never compiles this package; `make bench` runs it in the bench profile
// (PLAN.md D1).
package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"sixfields/internal/explain"
	"sixfields/internal/fixture"
	"sixfields/internal/fold"
	"sixfields/internal/msg"
	"sixfields/internal/snapshot"
	"sixfields/internal/why"
)

const (
	// fixtures is the path a reader of the report would type; fixturesDir is the
	// same directory from this package.
	fixtures     = "testdata/fixtures"
	fixturesDir  = "../../" + fixtures
	cassettesDir = "../../testdata/cassettes"
)

// task is one diagnosis. Its truth is not written down anywhere: it is what
// why.Rank computes over the fixture, so a task can never disagree with the
// analyzer the product ships.
type task struct {
	name    string
	request explain.Request
	object  snapshot.Ref
	code    msg.Code
	// recorded is false for the hand-built scenarios. The mix matters when reading
	// a score: a synthetic stall is cleaner than anything a real cluster produces.
	recorded bool
}

// loadTasks is every scenario under testdata/fixtures whose last snapshot is
// stalled. Deriving the list from the fixtures keeps it honest: a scenario that
// stops stalling stops being a task, and a new stall fixture becomes a task
// without a second list to keep in step.
func loadTasks() ([]task, error) {
	scenarios, err := fixture.Scenarios(fixturesDir)
	if err != nil {
		return nil, err
	}
	var out []task
	for _, scenario := range scenarios {
		envelopes, err := fixture.Load(filepath.Join(fixturesDir, scenario))
		if err != nil {
			return nil, err
		}
		if len(envelopes) == 0 {
			continue
		}
		env := envelopes[len(envelopes)-1]
		now := fixture.NowFor(env)

		res := fold.Fold(env, fold.Options{Now: now})
		if _, stalled := res.Stalled(); !stalled {
			continue
		}
		stall, ok := why.Rank(env, res, why.Options{Now: now})
		if !ok {
			return nil, fmt.Errorf("%s: a phase is stalled and the ranker named no object", scenario)
		}
		runbook, _ := msg.Runbook(stall.Code)
		out = append(out, task{
			name: scenario,
			request: explain.Request{
				Code: stall.Code, Stall: stall, Phases: res.Phases,
				Runbook: runbook, Names: objectNames(env),
			},
			object:   stall.Object,
			code:     stall.Code,
			recorded: !env.Meta.Synthetic,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no stalled scenario under %s", fixturesDir)
	}
	return out, nil
}

func objectNames(env snapshot.Envelope) []string {
	out := make([]string, 0, 2*len(env.Objects))
	for _, o := range env.Objects {
		out = append(out, o.Kind()+"/"+o.Name(), o.Name())
	}
	return out
}

// result is one backend's answer to one task.
type result struct {
	task     string
	outcome  outcome
	detail   string
	answered bool
	grounded bool
	took     time.Duration
}

// run puts one backend through every task, one call at a time. The bench profile
// gives the machine one resident model and 4 GB of VM, so two concurrent model
// calls would measure the swap.
func run(backend explain.Explainer, tasks []task) []result {
	out := make([]result, 0, len(tasks))
	for _, t := range tasks {
		ctx, cancel := context.WithTimeout(context.Background(), explain.Timeout)
		start := time.Now()
		answer, err := backend.Explain(ctx, t.request)
		took := time.Since(start)
		cancel()

		r := result{task: t.name, took: took, answered: err == nil}
		r.outcome, r.detail = score(t, answer, err)
		if r.answered {
			r.grounded = answer.Grounded(t.request) == nil
		}
		out = append(out, r)
	}
	return out
}

// backendFor builds one of the four explain backends by name, wrapped exactly as
// `cluster why --explain` wraps it: the interface a user goes through is the
// interface the benchmark scores. The one wrapper is AnalyserCommand, which
// replaces the model's suggested command with the analyzer's own.
func backendFor(name string) (explain.Explainer, error) {
	inner, err := chooseBackend(name)
	if err != nil {
		return nil, err
	}
	return explain.AnalyserCommand(inner), nil
}

func chooseBackend(name string) (explain.Explainer, error) {
	switch name {
	case "noop":
		return explain.Noop{}, nil
	case "cassette":
		return explain.Cassette{Dir: cassettesDir}, nil
	case "local":
		url, model := os.Getenv("CLUSTER_AI_URL"), os.Getenv("CLUSTER_AI_MODEL")
		if url == "" || model == "" {
			return nil, fmt.Errorf("the local backend needs CLUSTER_AI_URL and CLUSTER_AI_MODEL; both are pinned in versions.env, which `make bench` exports")
		}
		return explain.Local{URL: url, Model: model}, nil
	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			return nil, fmt.Errorf("the anthropic backend needs ANTHROPIC_API_KEY, and a run bills against AI_CREDIT_CAP_USD in versions.env")
		}
		// CLUSTER_AI_MODEL names the pinned local model, so the paid backend reads
		// its own variable. Empty falls back to explain.DefaultModel.
		return explain.Anthropic{Model: os.Getenv("ANTHROPIC_MODEL")}, nil
	}
	return nil, fmt.Errorf("unknown backend %q: choose from noop, cassette, local, anthropic", name)
}
