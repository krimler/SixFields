# What the AI features send, and where

After this page you can say exactly which bytes leave your machine for each backend, turn
every model call off with one environment variable, and know why `make test` can never
reach a model.

## Turn it off

```
export CLUSTER_AI=off
```

`CLUSTER_AI` takes three values:

| value | effect |
|---|---|
| `off` | No model is called by any code path. `cluster why --explain` prints the runbook and stops. |
| `explain` | The default. Only `cluster why --explain` may call a model. |
| `all` | Every AI path may call a model, including `cluster new --from` and build-time generation. |

No model is called unless a command asks for one. `cluster up`, `cluster status`,
`cluster why` and `cluster render` never call a model at any setting; there is no
background call, no warm-up, and no telemetry.

## The four backends

`local` is the default. With it, nothing leaves the machine.

| backend | where the model runs | what leaves the machine | used by |
|---|---|---|---|
| `noop` | Nowhere. It echoes the analyzer's output back. | Nothing. No socket is opened. | Unit tests of every AI code path |
| `cassette` | Nowhere. It replays a response recorded under `testdata/`. | Nothing. No socket is opened. | CI and `make test-llm` |
| `local` | An OpenAI-compatible HTTP server on this machine, at `CLUSTER_AI_URL` (default `http://127.0.0.1:1234/v1`) | Nothing. The payload crosses a loopback socket to a process you started. | `cluster why --explain`, `make bench`, build-time generation |
| `anthropic` | Anthropic's API, `https://api.anthropic.com/v1/messages`, authenticated with `ANTHROPIC_API_KEY` | The redacted, anonymized payload described below | Opt-in only, when the key is set and `AI_CREDIT_CAP_USD` allows it |

One caveat on `local`: `CLUSTER_AI_URL` can point at a model on another machine on your
network. That is supported — it is how you get a heavier model without giving up the dev
loop — and it means the payload leaves this machine for that host. Nothing else changes:
the same redaction runs, and `--anonymize` still defaults off. Check `CLUSTER_AI_URL`
before assuming `local` means loopback.

## What is in the payload

`cluster why --explain` sends four things, and nothing else:

- the ranked stall candidates `internal/why` produced: kind, name, namespace, condition
  type, reason, message, and elapsed time;
- the folded phase table: four phase names, states, details, and durations;
- the error code being explained;
- the runbook text for that code, which ships in the binary.

It does not send the snapshot envelope, object `spec`s, provider credentials, your
kubeconfig, the workload cluster's kubeconfig, container logs, or anything the CLI did not
already print to your terminal.

The answer is required to be JSON matching `docs/schema/explain.v1.json`: three
plain-language lines, one suggested command, and the code. Output that does not validate
is discarded and the runbook stands alone. The runbook prints first and the model's lines
stream under it, with a 60-second hard timeout.

## Redaction runs on every backend

Redaction happens inside the process, before the payload reaches any backend — including
`noop`, `cassette` and `local`. A recorded cassette therefore cannot contain a secret.

Always removed:

- kubeconfig contents: `client-certificate-data`, `client-key-data`, `token`, and any
  embedded `kind: Config` document;
- bearer tokens and kubeadm bootstrap tokens, wherever they appear in a condition message;
- Secret payloads: every value under `data` and `stringData`;
- the contents of any field whose key contains `password`, `token`, `key` or `secret`.

Removed only when you ask:

- IP addresses and CIDRs, with `--redact-ips`. They are off by default because a stall
  message about an unreachable endpoint is unreadable without them.

Redaction replaces a value with a marker; it never silently drops the field, so an answer
grounded on a redacted value is still traceable.

## --anonymize

`--anonymize` replaces object names and namespaces with stable keys before the payload
leaves the process — `dev-1-control-plane-7fk2x` becomes `Machine/m1`, `default` becomes
`ns/n1` — and substitutes the real names back into the answer you see. The mapping never
leaves the process. Round-trip identity is asserted against every fixture in the test
suite.

| backend | default |
|---|---|
| `anthropic` | on |
| `local`, `cassette`, `noop` | off |

`--anonymize` and `--no-anonymize` override the default in either direction.

It does not replace condition types, reasons, replica counts, durations, kinds, versions,
or the error code. Those are what the grounding test checks the answer against, and
anonymizing them would make the answer unverifiable.

## The cost cap

Two keys in `versions.env` bound paid use:

```
AI_DAILY_TOKEN_CAP=200000
AI_CREDIT_CAP_USD=0
```

`AI_DAILY_TOKEN_CAP` counts input plus output tokens per UTC day against the `anthropic`
backend. When the day's count reaches it, that backend behaves as `CLUSTER_AI=off` until
the next day: `--explain` prints the runbook and says the cap is reached.

`AI_CREDIT_CAP_USD` is zero in this repo, so paid API use is off until someone raises it
deliberately. `local`, `cassette` and `noop` are not capped; they cost nothing and send
nothing.

## Tests never call a model

The split is enforced by two targets:

```
make test        # go test ./... — no LLM, no network, under 30s
make test-llm    # go test -tags llm ./internal/explain/... — cassettes and live evals
```

`make test` is the gate. It builds no model client and opens no socket; the `llm` build
tag keeps every model-touching test out of the default package set. `make test-llm` is
never run by the default gate, and its cassettes are recorded from the pinned local model
so CI is deterministic and free.

## Where the settings live

| setting | where | what it does |
|---|---|---|
| `CLUSTER_AI` | environment | `off` / `explain` / `all` |
| `CLUSTER_AI_URL` | `versions.env`, overridable | The OpenAI-compatible endpoint the `local` backend calls |
| `CLUSTER_AI_MODEL`, `CLUSTER_AI_QUANT`, `CLUSTER_AI_SHA256`, `CLUSTER_AI_RUNTIME` | `versions.env` | The pinned local model, filled in by `make doctor-ai` |
| `ANTHROPIC_API_KEY` | environment | Selects the `anthropic` backend. Without it, that backend is unavailable |
| `AI_DAILY_TOKEN_CAP`, `AI_CREDIT_CAP_USD` | `versions.env` | The paid-use caps above |
| `--anonymize` / `--no-anonymize`, `--redact-ips` | flags on `cluster why` | Per-invocation overrides |

`make doctor-ai` reports which backend is active, which model it resolved, and its
tokens/s, without sending anything but a fixed benchmark prompt.
