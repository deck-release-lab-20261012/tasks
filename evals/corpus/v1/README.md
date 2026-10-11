# Pilot corpus v1

This directory contains the twelve-case exploratory corpus defined by
`docs/GENERATION_EVALS.md`.

- `source.json` is the review-friendly authoring form. Evidence is exact PRD
  text, not manually calculated offsets.
- `corpus.json` is the resolved, immutable input to the evaluator. It records
  byte spans and SHA-256 hashes for every PRD and evidence span.
- `calibration-glm-pass-2.json` and
  `calibration-granularity-glm-pass-3.json` retain independent calibration
  passes that were blind to reference labels and generated outputs.

Regenerate and validate with:

```sh
go run ./cmd/noise-tasks-eval compile-corpus \
  evals/corpus/v1/source.json > /tmp/noise-tasks-corpus.json
cmp /tmp/noise-tasks-corpus.json evals/corpus/v1/corpus.json
go run ./cmd/noise-tasks-eval validate-corpus \
  evals/corpus/v1/corpus.json
```

The source PRDs are sanitized rewrites or purpose-built synthetic inputs.
Hashes of internal source documents provide provenance; their private text is
not committed here. The labels are cross-model, software-engineering
annotations, not human-expert gold. They are sufficient for an exploratory
pilot and are not sufficient for the sixty-case general quality claim.

## Annotation reconciliation

The first blind pass found all named functional and release-gate units but
mixed ordering clauses into the requirement list and projected compound
ordering inconsistently. The guide was revised before a second calibration
pass:

- `R` and `G` clauses are requirement units;
- `O` clauses are ordering labels rather than extra requirements;
- every independently necessary compound antecedent becomes an edge; and
- an endpoint that cannot be projected onto a requirement unit is recorded as
  unprojectable rather than turned into an invented edge.

After the revision, the two label sets agreed on calibration requirement
membership and projected ordering (`Krippendorff's alpha = 1.00` for each).
Granularity bounds remained subjective. A third blind pass used a tighter
grouping guide; adjudication selected the overlap of the independent bounds.
The pilot records weighted kappa `0.70`, above the frozen `0.67` feasibility
floor but too small and too model-mediated to support a release-quality
annotation claim.

No annotator saw generator output, provider identity, prompt version, or the
baseline/candidate designation while creating these labels.
