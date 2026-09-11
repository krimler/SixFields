# Paper

`sixfields.tex` is a six-page experience report on this project, written for
arXiv. It compiles with any TeX distribution that has `IEEEtran`:

```sh
pdflatex sixfields.tex && pdflatex sixfields.tex
```

Two passes, because the second resolves the table and figure references.

## Uploading to arXiv

`arxiv-sixfields.zip` is the submission. It holds `sixfields.tex` and nothing
else: the figures are listings, the bibliography is inline, so one file is the
whole paper. It was verified by unpacking it into an empty directory and
compiling there.

Rebuild it after any edit:

```sh
make paper
```

Suggested categories: `cs.SE` as primary, `cs.DC` as cross-list.

## Keeping it honest

Every number in the evaluation came from a command run against this repository or
a live cluster. If the artefact changes, the numbers in Section V go stale.
The ones most likely to move:

| Claim in the paper | Where it came from |
|---|---|
| 7,137 lines non-test Go, 4,018 test | `find . -name '*.go' \| xargs wc -l` |
| 348 assertions, 140 test functions | `go test ./... -v \| grep -c -- '--- PASS'` |
| 2s, 16s, 187s test layers | `make test`, `make test-envtest`, `make e2e` |
| 84 policy cases | `go test ./policy/... -v \| grep -c -- '--- PASS'` |
| 659 API constants | `docs/api-snapshot.json` |
| 6 recorded, 5 synthetic fixtures | `testdata/fixtures/*/` `_meta.synthetic` |
| provisioning times | single runs, recorded in the session that produced them |

Figure 2, the mid-run display, is reproduced from
`testdata/golden/render/tty/std-docker-happy-midrun-80.txt`, which a test
regenerates. A layout change shows up as a diff in that golden, so the figure and
the tool cannot drift apart silently.
