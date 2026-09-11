# Paper

`sixfields.tex` is a six-page experience report on this project, written for
arXiv. It compiles with any TeX distribution that has `IEEEtran`:

```sh
pdflatex sixfields.tex && pdflatex sixfields.tex
```

Two passes, because the second resolves the table and figure references.

## Uploading to arXiv

arXiv wants the source, not the PDF. Upload `sixfields.tex` on its own; there are
no figures or `.bib` files, and the bibliography is inline in a `thebibliography`
environment, so one file is the whole submission.

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
