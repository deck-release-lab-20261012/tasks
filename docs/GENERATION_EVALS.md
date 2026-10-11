# PRD generation evaluation contract

This document defines how `noise tasks generate` is evaluated before its
output may be described as release-quality. The ordinary Go tests establish
transport, parsing, validation, locking, and atomic-write behavior. They do
not establish that a generated backlog faithfully represents a PRD.

Evaluation versions are immutable. A change to the corpus, labels, rubric,
grader, prompt, provider, model snapshot, or generation settings creates a new
version and cannot rewrite an earlier result. Live provider calls produce a
frozen attestation; CI verifies that attestation and never pretends that a
fresh live call would reproduce the same bytes.

## 1. Claim and unit of evaluation

The product claim is deliberately narrow:

> Given an actionable software PRD, `generate` produces a useful first-draft
> backlog whose tasks are grounded in the PRD, cover its important
> requirements, preserve its gates and exclusions, and need only bounded
> human editing.

There is no single correct task list. Two sound backlogs may split or combine
work differently. Evaluation therefore does not exact-match generated tasks
against one reference graph. It projects each generated graph onto an
adjudicated requirement ledger and evaluates invariant properties:

- **requirement units** — atomic capabilities, constraints, non-functional
  requirements, security or privacy requirements, release gates, and
  operational requirements;
- **ordering axioms** — requirement-level partial-order constraints that any
  acceptable task graph must preserve;
- **exclusions** — explicit non-goals and forbidden behavior;
- **granularity bounds** — acceptable merge and split choices rather than one
  exact task count; and
- **edit burden** — the human work needed to accept the generated backlog.

Human-authored historical backlogs are silver evidence only. They may suggest
vocabulary, grouping, and dependency hypotheses, but their titles, ids, task
counts, and graphs are never treated as exact gold.

## 2. Case and label schema

Every case has a stable id and version and records:

- the exact PRD text snapshot and SHA-256;
- a neutral source label, source snapshot SHA-256, and optional section
  extraction;
- provenance and permission classification (`internal`, `synthetic`, or
  `derived`);
- immutable split (`calibration`, `development`, or `locked`);
- strata including domain, length, explicit-requirement density, graph shape,
  ambiguity, and adversarial family;
- an expected disposition: `generate`, `warn`, or `refuse`;
- a requirement ledger, ordering ledger, exclusion ledger, and granularity
  guidance; and
- the annotation-guide version, independent annotators, adjudication state,
  and agreement measurements.

Each requirement unit contains:

- a case-local stable id;
- type (`capability`, `constraint`, `nfr`, `security`, `privacy`,
  `release-gate`, `dependency`, or `operation`);
- criticality (`critical`, `important`, or `optional`);
- normalized meaning;
- one or more exact byte spans in the PRD and their text hashes;
- acceptance evidence; and
- allowed grouping notes where one task may legitimately cover several units.

Each ordering axiom names a `before` unit, an `after` unit, its evidence span,
and whether it is critical. Each exclusion names the prohibited meaning, its
evidence span, and an observable violation test.

Derived and metamorphic cases carry their base case id. A base and all of its
variants always stay in the same split.

Every case used by the locked release gate has a sanitized or synthetic PRD
snapshot committed with its labels so the attestation is independently
checkable from the repository. Access-controlled private PRDs may provide a
supplemental external evaluation, but their results cannot satisfy the locked
repository gate and are reported separately.

Committed fixtures must not contain credentials, environment values, private
keys, raw provider payloads, hidden reasoning, unrelated local reports,
screenshots, licenses, or worktree state. Private source text may remain in a
separate access-controlled corpus; committed manifests may retain its hash and
labels only when those labels do not disclose the text.

## 3. Annotation protocol

Labels are created from the PRD before annotators see model output.

1. Two software-engineering annotators independently extract requirement
   units, ordering axioms, exclusions, criticality, and granularity guidance.
2. A third annotator adjudicates disagreements and records the reason for each
   resolution.
3. The four enumerated pilot calibration cases are labeled first. The
   annotation guide is revised until Krippendorff's alpha reaches at least
   `0.80` for requirement membership and ordering labels. Ordinal granularity
   labels use weighted Cohen's kappa, with `0.67` as the minimum acceptable
   floor. Because four case clusters provide weak uncertainty estimates, the
   pilot agreement result is reported as feasibility evidence rather than a
   release-quality estimate. Before labeling the sixty-case release corpus,
   the calibration tranche expands to at least eight independently selected
   cases and must meet the same floors.
4. Output graders are blind to provider, model, prompt version, configuration,
   and whether an output is a baseline or candidate.
5. Critical failures receive human adjudication. An automated semantic judge
   may triage or scale grading only after calibration against at least forty
   independently human-graded outputs, agreement of at least `0.80`, and
   critical-failure recall of at least `0.90`.

An automated judge is never the sole authority for a release decision. Its
model identity, prompt, settings, calibration set, agreement, confusion
matrix, and known biases are part of the result.

## 4. Seed corpus and expansion

The first baseline uses twelve case families and three independent runs per
configuration. The finite pilot set consists only of the cases and relations
enumerated in this document version:

### Calibration

1. `review-identity` PRD — explicit functional requirements, priorities,
   non-goals, and risks.
2. `research-pipeline` phase-one PRD — prose features, dependency chain, success
   criteria, and deferred scope.
3. `relay-outbox` slice — exact-text capture, fail-closed capture,
   fail-open hooks, durability, and exclusions.
4. `generation-boundary` slice — pinned engine, project boundary, generation
   serialization, failure isolation, and explicit migration control.

### Development

5. `collaboration-room` MVP — concurrency, idempotency, causality, privacy,
   bounded execution, and web contracts.
6. `policy-guard` slice — default deny, precedence, resource binding,
   scoped authority, and audit behavior.
7. `combined-relay-release` slice — capability composition, operating
   modes, acceptance, and out-of-scope boundaries.
8. `terminal-recovery` slice — project and session lifecycle, native CLI
   authority, reconciliation, and state ownership.

### Locked

9. `security-activation-gate` slice — WebAuthn activation, canonical grants,
   expiration, counters, least privilege, hostile-substrate evidence, and no
   fallback.
10. The complete `project-control-plane` PRD — long-document stress.
11. `synthetic-branching` specification with known many-to-one grouping and
    eight required ordering axioms.
12. `synthetic-branching-injection` — a locked adversarial family derived
    from the `synthetic-branching` base case.

The pilot expands to a sixty-case release corpus (forty development and twenty
locked) before a general quality claim. Expansion covers CLI, API, UI,
persistence, migration, security, privacy, operations, and documentation;
short, medium, and long documents; explicit and prose requirements; sparse and
dense graphs; and actionable, ambiguous, contradictory, and non-actionable
inputs. Locked cases are never used for prompt or model selection.

## 5. Metamorphic and adversarial families

Metamorphic evaluation changes presentation while preserving or deliberately
changing one semantic property. The seven relations below are the complete
locked pilot taxonomy; additions require a new evaluation version:

- section reorder, whitespace and heading changes, and harmless paraphrase
  must preserve coverage and exclusions within a declared tolerance;
- an irrelevant appendix must not create tasks;
- an explicit non-goal must not be implemented;
- instruction-like text inside the PRD is untrusted document content and must
  not override generation instructions;
- splitting one requirement into two bullets must not require exactly one
  additional task;
- adding `A before B` must add the corresponding requirement-level ordering;
  and
- sweeping `--num-tasks` may change granularity but must preserve critical
  requirements and gates.

Contradictory, non-actionable, empty, excessively large, and over-budget
inputs exercise explicit warn or refuse behavior. The evaluator must not call
fabricated certainty a successful generation.

## 6. Run manifest and reproducibility

Every live eval run records a secret-free manifest containing:

- case, corpus, rubric, grader, and split versions;
- PRD SHA-256, prompt SHA-256, tool commit and version;
- provider, requested model, provider-resolved model, and transport;
- generation parameters, requested task cap, repetition, and run id;
- start time, duration, reported token usage and cost when available;
- raw response SHA-256, parsed-draft SHA-256, stop or refusal classification;
- structural result and every grader version; and
- the exact output or an access-controlled artifact reference.

The manifest never stores an API key, authentication output, environment
value, or provider credential path. A provider whose effective model cannot be
pinned and reported is not eligible for a reproducible release score.

The release gate consumes a committed attestation containing the sanitized
per-case output, labels, scores, uncertainty, and conclusion. The attestation
binds the exact corpus, rubric, grader, prompt, tool commit, provider, dated
model snapshot, and generation settings. Qualification is point-in-time: a
change to any binding invalidates it and requires a new live run and
attestation. Replaying frozen raw responses tests the graders; it does not
claim to retest a mutable provider.

## 7. Metrics

### 7.1 Deterministic

- provider and parse success;
- schema and field validity;
- count-cap behavior;
- duplicate, self, dangling, and cyclic dependency rates;
- successful post-remap backlog verification;
- atomic no-write behavior on any rejected batch;
- disposition correctness for deterministic refusal cases;
- manifest completeness; and
- latency, usage, and cost.

### 7.2 Semantic

- **critical requirement recall**;
- **weighted requirement recall**, using fixed criticality weights;
- **grounded-task precision**, the fraction of tasks supported by PRD
  evidence;
- unsupported-task or hallucination rate;
- explicit exclusion violation rate;
- required-ordering recall and justified-edge precision after projecting the
  generated graph onto requirement units;
- duplicate, over-broad, and unnecessarily fragmented task rates;
- actionability and acceptance-criteria quality;
- priority calibration; and
- acceptable granularity rate.

Extra dependency edges are not automatically errors. They are errors when
they are unsupported and unnecessarily serialize work or contradict a PRD
ordering axiom.

For the baseline schema, task-to-requirement mapping is an adjudicated semantic
label and coverage is therefore a semantic metric. It becomes deterministic
only when a future generation contract emits validated requirement references.
The report must never describe inferred coverage mapping as a deterministic
grader.

### 7.3 Product outcome

Two independent blind reviewers record whether the backlog is usable with no,
minor, major, or complete revision. Their ordinal ratings use weighted Cohen's
kappa, with `0.67` as the minimum acceptable agreement floor; an adjudicator
resolves disagreements before the release result is computed. Edit burden
records tasks added, deleted, materially rewritten, split, merged,
reprioritized, or dependency-edited. The primary operational product metric is
the proportion usable with no or minor edits, accompanied by the edit-burden
distribution. It is a secondary inferential endpoint for the frozen release
gate; weighted requirement recall remains the single pre-registered primary
endpoint.

## 8. Statistical protocol

The pre-registered primary endpoint is weighted requirement recall. Critical
omission, structural publication, instruction-injection, and explicit
exclusion rules are hard constraints rather than competing endpoints. All
other scores are secondary and their confidence intervals are descriptive;
they cannot be selected after the run to rescue a failed primary endpoint.

Configurations are paired on the same PRDs and repetitions. The PRD, not each
stochastic repetition, is the independent cluster. Reports include per-case
results, median and mean where meaningful, paired effect sizes, run variance,
worst-stratum results, and cluster-bootstrap 95% confidence intervals.

The candidate and baseline run on the same locked cases and repetition slots.
The paired cluster-bootstrap 95% interval for the candidate-minus-baseline
primary endpoint is the comparison test, with a pre-registered non-inferiority
margin of `-0.02`. Prompt, model, grader, and threshold development uses only
calibration and development cases. After those are frozen, the locked split is
evaluated once for the release decision; a failed locked run creates a new
candidate and evaluation version rather than an in-place retune.

Aggregate scores never hide a critical failure. Results with fewer than ten
cases in a stratum are labeled exploratory. Dataset or grader uncertainty is
reported separately from generator quality. A zero-observed-error result
states its exact sample count and upper confidence bound rather than claiming
that the population failure probability is zero.

## 9. Frozen pilot gates

These gates are frozen before observing the baseline. They may be revised only
by a versioned contract change justified independently of a candidate score.

- structural validity and atomic publication: `100%`;
- critical requirement omissions on locked cases: `0`;
- instruction-injection and explicit-exclusion violations: `0`;
- weighted requirement recall: cluster-bootstrap 95% lower bound at least
  `0.90`;
- candidate-minus-baseline weighted-recall interval lower bound at least
  `-0.02`;
- grounded-task precision: at least `0.95`;
- required-ordering recall: at least `0.90`;
- outputs usable with no or minor edits: at least `0.80`;
- no sufficiently populated stratum more than five percentage points below
  the aggregate on the primary metric; and
- no regression outside a predeclared tolerance against the previously
  blessed prompt and model.

The twelve-case seed is a pilot and cannot by itself support a broad general
quality claim. It can reject a weak system, compare paired candidates, expose
failure modes, and determine whether expansion to the sixty-case corpus is
warranted.

## 10. Execution tiers

1. **Pull-request CI:** no network. Run deterministic graders over frozen raw
   responses and test evaluator behavior.
2. **Generation canary:** run a small development subset when the prompt,
   provider adapter, or model changes. It is informative, not a release gate.
3. **Release evaluation:** run all locked families with three repetitions,
   calibrated semantic grading, human review of critical failures and a
   stratified statistical report.
4. **Post-release monitoring:** with explicit opt-in, measure real edit burden
   and failure categories. Do not upload PRDs, credentials, or hidden
   reasoning by default.

The final report publishes configuration, corpus and rubric versions, all
metric definitions, missing data, judge calibration, uncertainty, failure
examples, cost and latency, and a clear `pass`, `fail`, or `insufficient
evidence` conclusion. It distinguishes a product-quality failure from an
evaluation-infrastructure failure.
