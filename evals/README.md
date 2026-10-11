# PRD-generation evaluation assets

This directory holds versioned, reviewable inputs for the offline developer
evaluation harness. It is not part of the installed `noise-tasks` command and
the harness contains no provider or network client.

From the repository root, use:

```sh
go run ./cmd/noise-tasks-eval validate-corpus evals/corpus/v1/corpus.json
go run ./cmd/noise-tasks-eval validate-runs \
  --corpus evals/corpus/v1/corpus.json runs.json
go run ./cmd/noise-tasks-eval report \
  --corpus evals/corpus/v1/corpus.json runs.json > report.json
```

`source.json` is the review-friendly corpus source. The generated
`corpus.json` must remain byte-for-byte equal to `compile-corpus` output; the
Go test suite enforces that invariant.

Live calls are performed by a separate, explicitly configured runner. It must
produce the closed run-bundle schema consumed here and record the resolved
provider model. Provider credentials and authentication output must never be
written into these assets or a run bundle.
