# Cassettes

Recorded model responses, replayed so CI is deterministic and free. A cassette is
named after the hash of everything the model was given (`explain.Key`), so an
identical stall replays the same answer and a different one has no cassette rather
than a wrong one.

Record with:

```sh
CLUSTER_AI_BACKEND=local CLUSTER_AI_RECORD=1 make test-llm
```

Cassettes committed here are recorded from the pinned local model in
`versions.env`, never from a paid API. `make test` never reads them and never
calls a model: it uses the `noop` backend, which exercises every AI code path
without one.

None are committed yet — `CLUSTER_AI_MODEL` is unset, so no model has been pinned
on this machine. `make doctor-ai` recommends one.
