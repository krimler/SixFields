# cluster-bench

After this page you can run the task benchmark, read its report, and say what any
number in it means.

## Run it

```sh
make profile P=bench
PROFILE=bench make bench
```

That scores the `noop` and `cassette` backends over every stall fixture in the
repo. Neither opens a socket: no model, no network, no key, no cost. The whole
run takes about two seconds.

`make bench` refuses to start outside the bench profile. The profile exists
because a model-backed run needs the machine to itself (PLAN.md D1): the container
VM at 4 GB, one model resident, nothing else.

Add backends with `BACKENDS`:

```sh
BACKENDS=noop,cassette,local PROFILE=bench make bench
```

| backend | what answers | cost |
|---|---|---|
| `noop` | The analyzer's own output, echoed back. This is the no-AI baseline. | free, offline |
| `cassette` | The responses in `testdata/cassettes/`, recorded from the pinned local model. | free, offline |
| `local` | The model at `CLUSTER_AI_URL`, named by `CLUSTER_AI_MODEL` (both pinned in `versions.env`). | free, one machine |
| `anthropic` | Anthropic's API, keyed by `ANTHROPIC_API_KEY`, model from `ANTHROPIC_MODEL`. | billed per run |

Selecting `local` or `anthropic` runs `make doctor-ai` first, which does the
memory math and measures tokens/s before anything is scored. The two default
backends skip it.

A new stall fixture has no cassette, so the `cassette` backend reports a
framework error on that task until `CLUSTER_AI_RECORD=1 make test-llm` records
one from the pinned local model.

## What a task is

One task is one stalled cluster and one question: which object is blocking, and
what class of stall is this?

The task list is every scenario directory under `testdata/fixtures` whose last
snapshot folds to a stalled phase. A new stall fixture becomes a task with no
list to edit; a scenario that stops stalling stops being a task. Each run prints
the list it scored, with the blocking object, the stall class, and whether the
fixture was recorded from a live cluster or hand-built.

The backend is handed exactly what `cluster why --explain` sends a model: the
ranked stall, the four-phase table, the runbook for the class, and the object
names in the envelope. It answers in the schema `docs/schema/explain.v1.json`
describes, a stall class code, three lines, one next command.

## How scoring works

The truth is computed, never written down. `why.Rank` over the same fixture names
one object and one `msg.Code`, and that is the answer key. An answer is correct
when both match:

- the `code` field equals the code the analyzer ranked;
- one of the three lines contains the object's name, as `Kind/name`, as
  `Kind name`, or on its own.

The three lines are what the user reads, so they are what is scored. A name that
appears only inside the suggested command does not count.

Nothing grades a transcript and nothing asks a model to judge another model.

Grounding is recorded next to correctness. `explain.Explanation.Grounded` checks
that every object reference and every number in the answer appeared in what the
backend was given; the `UNGROUNDED` column counts the answers that failed it. An
answer can be correct and ungrounded, right about the blocking object, wrong
about a number beside it, and the column is there to show it.

## Framework errors and reasoning errors

Every failure carries one of two labels. This is the k8s-bench split, and it is
the most useful column in the report: as a single failure count these two say
nothing, and they need opposite fixes.

| label | what happened | examples |
|---|---|---|
| `framework` | The backend produced no usable answer. | the call errored, the 60-second timeout expired, the output was not the schema, the `code` field is in no registry entry |
| `reasoning` | The backend answered, validly, and was wrong. | it named a stall class the analyzer did not rank, it named no object or a different one |

A schema breach is a framework error and a wrong stall class is a reasoning
error, so the shape of the answer and the content of the answer are counted
apart. The report's `FAILURES` block prints the backend, the task, the label, and
what went wrong, one line each.

## What the run fails on

The benchmark reports model scores; it does not gate on them. Two things fail it:

1. **The `noop` baseline missed a task.** The baseline is the analyzer's own
   answer read back, so anything short of a clean sweep is the harness
   disagreeing with itself.
2. **A backend answered nothing.** A framework error on every task means the
   backend was absent, and a 0% score for a model that was never running is a lie.

## Time and cost

On the 24 GB reference machine:

| backends | wall clock | cost |
|---|---|---|
| `noop,cassette` (the default) | under a second a task, about two seconds in total | none |
| `local` | 13 s a task at the median, so a couple of minutes for the list | none, one machine at ~24 tok/s |
| `anthropic` | one API call a task | billed; `AI_CREDIT_CAP_USD` in `versions.env` is the cap it spends against, and it is 0 until a human raises it |

## The rule

An AI feature that does not beat the no-AI baseline is not shipped.

On this task the baseline is also the ceiling. `why` decides and the model
explains, so a backend can match the analyzer or corrupt it, and the pass rate
measures fidelity: does routing the finding through a model keep the object and
the class intact? The `MEDIAN` and `P95` columns are what a model costs for
whatever it adds, and the four rungs below `--explain` stay free.
