# noise tasks v2.6 specification

The contract the implementation is built against and reviewed against.
Spec-driven and from-scratch: this document defines behavior. The file shape
is described here in full; no external implementation is part of the contract.

## 1. Data model

The task backlog is grouped by **tag** (a named, independent task list).
The §8 SQLite database is the only authority for this state; this section
defines the logical model, and its JSON rendering below is the exchange
format used by the non-authoritative `--file` fixture surface and by
migration and backup compatibility. Top level is an object mapping tag name →
tag block:

```json
{
  "<tag>": {
    "tasks": [ <task>, ... ],
    "metadata": {
      "version": "<string>",
      "lastModified": "<RFC3339>",
      "taskCount": <int>,
      "completedCount": <int>,
      "tags": ["<tag>"]
    }
  }
}
```

Task:

```json
{
  "id": "42",
  "title": "...",
  "description": "...",
  "details": "...",
  "status": "pending",
  "priority": "high",
  "dependencies": ["4", "7"],
  "subtasks": [],
  "updatedAt": "<RFC3339>"
}
```

Rules:

- Top-level `id` and dependency ids are **strings** of positive integers,
  unique within a tag. On the fixture surface a new id is `max(id)+1`. A live
  store seeds `nextId` once to `max(id)+1` (or the greater valid imported
  counter during migration), then increments that durable counter on
  allocation and never decreases, recomputes, or reuses it after removal.
- `status` ∈ `pending | in-progress | review | done | deferred | cancelled |
  blocked`.
- `priority` ∈ `high | medium | low`. A task whose `priority` is absent
  is `medium` everywhere — ranking, filtering, and output — and the file
  is never rewritten to add it.
- Subtasks carry `id` (a positive integer, unique per parent), `title`,
  `description`, `details`, `status`, `dependencies`, `parentTaskId`. A
  subtask is addressed as `<parent-id>.<subtask-id>` (for example `32.1`);
  both components use the same no-leading-zero positive-integer grammar as a
  top-level id. In a subtask's `dependencies`, a bare positive integer means a
  sibling id under the same parent; a qualified `<parent>.<child>` string means
  that exact subtask in the same tag. Bare integers may use existing integer or
  string encodings; qualified ids are strings. Zero, leading-zero strings,
  self-dependencies, duplicates after normalization, missing targets, and
  cycles are `verify` findings. Existing encodings are preserved on write.
  v2.5 preserves subtasks byte-faithfully and only mutates the selected
  subtask's `status`; its other fields, the parent task's fields (including
  `updatedAt`), and sibling subtasks remain
  byte-identical. Normal per-write metadata maintenance still applies.
- A task may carry `milestone` — a string naming one entry of the tag's
  optional `milestones` registry — and a tag block may carry that registry
  array. Both are ordinary state under this section's rules (unknown fields
  preserved verbatim); grammar, uniqueness, and semantics are §12.1's.
- Unknown fields on any object are **preserved verbatim** on write
  (round-trip safety) — this is what makes adopting an existing backlog
  safe.
- `completedCount` is maintained on every write (count of `done` tasks);
  `taskCount` is set on file creation and never trusted afterward.
- The default tag for new files is **`main`**. A file whose only tag is the
  legacy `master` is read and written as-is; nothing ever renames a tag
  implicitly.

## 2. Project context and file discovery

Task state has exactly one authority: the §8.1 SQLite database derived
from the repository family's Git common directory. Discovery never
searches a worktree for task state, and no worktree file is ever
selected, read, or written as a backlog. An explicit `--file <path>` is
a non-authoritative fixture surface per §8.1: it operates on the named
standalone JSON document only and is never selected by discovery.
`generate --file` still resolves the invocation's project boundary only
for the fixed preferences location in §2.3; the explicit file does not
influence that boundary. For implicit operation, `noise-tasks` resolves
one starting directory: a nonempty `NOISE_PROJECT_DIR`, which must name
an absolute existing directory, or canonical CWD when the variable is
unset or empty. The standalone executable performs this resolution
itself. Umbrella dispatch supplies the same canonical CWD through
`NOISE_PROJECT_DIR` as specified in §5.4. Given the same CWD and
inherited environment, both invocation forms therefore select the same
starting directory and the same §8.1 authority.

The repository boundary is the nearest directory at or above the start
that contains `.git` as either a directory or a regular file; the file
form covers Git worktrees and submodules. Outside Git, the start itself
is the boundary. `noise-tasks` owns this algorithm; the umbrella host
does not inspect `.git` or duplicate store discovery.

The retired JSON layouts — `.tasks/tasks/tasks.json`,
`.noisetask/tasks.json`, and `.taskmaster/tasks/tasks.json` — are inert
projections or leftovers. They are never selected, parsed, compared, restored,
or written as task state. The one exception is the exact derived legacy
`<common-dir>/noise-tasks/live.json`: only `backup create --legacy` and
`migrate plan|apply` may read it through the bounded compatibility surface in
§8.5–§8.7. No normal command, including `setup`, `init`, `verify`, or
`live feed`, accepts it. Mere existence checks select no authority.

`noise-tasks setup` is the canonical project-onboarding command. At the
repository boundary, or at the start outside Git, it creates the §8.1
database directly when none exists and stores the explicit generation
choice in `.tasks/preferences.json` as specified in §2.3. `noise-tasks
init` remains the noninteractive, task-only bootstrap: it creates only
the database with an empty `main` tag. Both commands use one shared
store-creation implementation. `.tasks/docs/PRD.md` is the conventional
project PRD path, although `generate` accepts any explicit PRD path.

An implicit command that requires a task store and finds neither a database nor
a migration recovery journal exits 1 and writes this exact diagnostic to
stderr:

```text
noise-tasks: project is not initialized; run noise tasks setup
```

It never opens an onboarding prompt from an ordinary task command; this
keeps agents, scripts, and CI from hanging. A fresh clone of a migrated
repository is exactly this intentional no-database state. `setup`,
`init`, `context`, and `version` retain their own missing-state
behavior. An explicit `--file` reports an ordinary file error rather
than project onboarding guidance.

A valid §8.6 recovery journal with no published database takes precedence
over missing-state onboarding. Every command except `context`, `version`, and
the matching `migrate apply` exits 1 with:

```text
noise-tasks: migration recovery required; rerun noise tasks migrate apply with the original plan and backup
```

`setup` and `init` never suggest creating a new store over that state.

### 2.2 Read-only project context

`noise tasks context --json` is the engine-owned project-context contract
for consumers that must locate task state without reproducing §2 or §8.1.
The standalone form is `noise-tasks context --json`. `--json` is required;
`context` accepts no `--file`, `--tag`, or other flags. It performs no
write and never mutates task state. When no database exists it opens
nothing. When the §8.1 database exists, `context` opens it through the
engine's read-only open path under the shared maintenance lock to report
exactly two store facts — the recorded owner boundary and the store
schema version — and nothing else. A store that cannot be opened and
read (corrupt, not a database, carrying no released schema, newer than
this binary, or unopenable in a read-only directory that cannot host
the WAL sidecars) is an operational failure: `context` exits 1 with
empty stdout, exactly like a malformed recovery journal. A successfully
observed blocked state is a successful context query and exits 0; failure
to resolve the canonical starting directory or project boundary exits 1
with empty stdout.

The output is one JSON object with every field present:

```json
{
  "schemaVersion": 5,
  "start": "/absolute/canonical/start",
  "projectBoundary": "/absolute/canonical/project",
  "storePath": "/absolute/canonical/path/to/tasks.sqlite",
  "storeOwnerBoundary": "/absolute/canonical/owner/checkout",
  "storeSchemaVersion": 5,
  "engineSchemaVersion": 5,
  "docsPath": ".tasks/docs/PRD.md",
  "preferencesPath": ".tasks/preferences.json",
  "initialized": true,
  "writable": true,
  "blocked": false,
  "blockedReason": ""
}
```

`start` is the canonical §2 starting directory and `projectBoundary` is
its nearest Git boundary, or `start` outside Git. They are absolute so a
consumer can prove that its registered real project path is the same
boundary. `storePath` is the absolute canonical §8.1 database path — the
only context path allowed outside the project boundary, because the
database lives under the Git common directory by design.
`storeOwnerBoundary` is the empty string when `initialized` is false;
otherwise it is the store's recorded immutable owner boundary (§8.1). It
lets a consumer prove the store it resolved is owned by the family it
registered: a linked worktree of the same family resolves the same owner,
while a consumer registered at a different root can refuse the mismatch
instead of mutating another family's store. `storeSchemaVersion` is 0
when not initialized; otherwise it is the store's `PRAGMA user_version`
(§8.2), reported as observed — a store older than the binary upgrades
only at its next mutation, and a store newer than the binary never
reaches the output because the read-only open refuses it fail-closed.
`engineSchemaVersion` is the maximum released store schema this binary
owns (§8.2), constant for the binary; the pair lets a consumer detect
pin-versus-store version skew before acting on it. `docsPath` and
`preferencesPath` are slash-separated paths relative to
`projectBoundary` and never contain `.` or `..` components. There is no
`backlogPath`: no worktree file is task state, and a consumer that needs
task data uses the released binary's read commands or the documented
database schema, never a file in the worktree.

`initialized` reports that the §8.1 database exists as a regular,
non-symlink file. `writable` reports whether the current process can
publish in the database's containing directory, or at the nearest
existing ancestor before initialization. This is a read-only permission
check; it never creates a probe file. A blocked result is never
writable.

`blockedReason` is empty unless `blocked` is true, when it is exactly
`migration-recovery-required`: a valid §8.6 apply journal proves that cutover
began but SQLite publication did not complete. Any malformed or
state-inconsistent journal is an operational failure: `context` exits 1 with
empty stdout rather than classifying or choosing authority. Context does not
expose receipt content.

The object contains no tag or task data, database content, directory
inventory, receipt detail, environment value, credential, or provider
data. Consumers treat an unknown schema version, blocked reason, missing
field, or additional field as an unsupported contract and fail closed.
Schema version 5 replaces version 4 by adding `storeOwnerBoundary`,
`storeSchemaVersion`, and `engineSchemaVersion`, and by opening an
existing store read-only to report them. As before, the `layout`,
`stateRoot`, `backlogPath`, `migration`, and `live` members are gone.
Consumers pinned to version 4 fail closed, so consumer adoption rolls
out exactly like a §7 consumer-adoption gate — consumers accept
schema 5 before any family in their care runs an engine release that
reports it.

### 2.3 Project preferences and onboarding

Generation preferences are project-local, reviewable data at
`<projectBoundary>/.tasks/preferences.json`. Outside Git, `start` is the
boundary. The preference location does not move when an explicit `--file` is
used and is not inferred from that file: `--file` selects the fixture
document while the invocation's §2 project boundary selects generation
preferences. A nested repository discovered on the upward walk supplies
authoritative task state, but its repository uses the one boundary-level
preferences file.

The file is strict JSON:

```json
{
  "schemaVersion": 1,
  "generation": {
    "adapter": "omp",
    "model": "zai/glm-5.2"
  }
}
```

Every shown field is required. Unknown or duplicate keys, a non-integer or
unknown schema version, a trailing JSON value, malformed UTF-8, or a document
larger than 16 KiB is invalid. `adapter` is one compiled-in identifier from
§4.1; any other adapter value makes the preferences file invalid.
`model` is trimmed, nonempty UTF-8 of at most 256 bytes, contains no ASCII
control character, and does not begin with `-`. Neither value is interpreted
by a shell. The schema has no extension map: it cannot store an executable,
path, arguments,
environment, endpoint, credential, authentication output, or secret. Adding a
field or adapter requires a reviewed schema- or contract-compatible product
change rather than project-supplied code.

The preferences path must be a regular, non-symlink file. Existing permission
bits are not a schema-validity condition; setup publishes every new or replaced
file with mode 0644. Setup refuses a symlink or special file at the file path
and refuses a symlinked `.tasks` component rather than following it outside the
boundary. It publishes through a same-directory temp file, explicit mode, file
fsync, atomic rename, and directory fsync. The file is suitable for version
control and contains no secret; provider credentials remain in the provider's
own credential store or documented secret environment.

`noise tasks setup` and `noise-tasks setup` are project-scoped and accept no
positional argument, `--file`, or `--tag`. Their options are
`--adapter <id>`, `--model <id>`, `--yes`, and `--json`:

- `--adapter` and `--model` are a pair and are rejected when only one is
  present. They select one explicit desired configuration; setup never guesses
  a model or silently selects the only available adapter.
- With neither selector and no preferences file, a terminal invocation prompts
  the user to select an available adapter and enter a model, then previews the
  exact pair before confirmation. Prompts go to stderr. Without an interactive
  terminal, or under `--json`, missing selectors are a usage error that names
  the required flags and leaves stdout empty.
- Creating preferences or replacing them with a different explicit pair
  previews the change and requires terminal confirmation. A noninteractive or
  `--json` invocation must instead supply `--yes`. With neither selector and an
  existing valid preferences file, setup keeps that configuration and
  idempotently repairs any missing §8.1 database. Identical desired state
  is a successful no-op. `--yes` skips only the final confirmation; it never
  supplies a default.
- Setup checks only that the adapter is compiled in and, for a CLI adapter,
  whether its fixed executable is presently resolvable and acceptable under
  §4.1. Creating or changing preferences requires an available adapter.
  Keeping an existing valid preference or repairing its missing database may
  succeed with `adapterAvailable: false` and a human warning, so task-only
  recovery does not depend on a provider installation. Setup performs no model
  call, subscription probe, or authentication exchange and never captures
  provider authentication output. Successful output says that authentication
  is unverified.

Human success output names the database and preferences paths,
their `created`, `updated`, or `unchanged` actions, and the selected adapter and
model. `--json` emits exactly one object with every field present:

```json
{
  "schemaVersion": 2,
  "projectBoundary": "/absolute/canonical/project",
  "storePath": "/absolute/canonical/path/to/tasks.sqlite",
  "preferencesPath": ".tasks/preferences.json",
  "storeAction": "created",
  "preferencesAction": "created",
  "generation": {
    "adapter": "omp",
    "model": "zai/glm-5.2"
  },
  "adapterAvailable": true,
  "authenticationVerified": false
}
```

`storePath` is the absolute canonical §8.1 database path. The action values
are exactly `created`, `updated`, or `unchanged`;
`storeAction` is never `updated`. A refusal produces empty stdout. The
absolute boundary is returned only because the user requested setup output;
no preference or diagnostic records environment or credential values.
`adapterAvailable` reports only whether a compiled-in CLI adapter's fixed
executable is acceptable and currently resolvable; it does not make an unknown
adapter identifier valid. The in-process adapter is available by construction.

Setup supports these recoverable states:

| Observed state | Setup behavior |
| --- | --- |
| no database, no preferences | create the §8.1 database, then preferences |
| database, no preferences | preserve the database and create preferences |
| no database, valid preferences | preserve preferences and create the database |
| database, valid preferences | keep or explicitly replace preferences; never rewrite task state |
| malformed, newer, symlinked, or special preferences | fail closed without overwriting it |
| inert projection present | ignore it; it is never task state |
| legacy JSON live store present | fail without creating a database; use §8.5–§8.6 |
| recoverable or invalid §8.6 migration state | preserve §8.6 fail-closed behavior |

Database creation and preference publication are individually atomic but
cannot be one filesystem transaction. Setup creates the database first,
then preferences. A crash between them leaves the supported task-only state;
rerunning setup completes it. It never removes or truncates either to
simulate rollback.

Provider selection and any required user confirmation happen first.
Setup records the observed preference bytes or absence, then opens the
database (or creates it) in one immediate transaction, repeats store
discovery, and
re-reads the preferences path before writing. If another setup published the
identical desired state, the later process succeeds as an idempotent no-op. Any other
change since preview refuses as a concurrent update instead of applying
last-writer-wins. Two different concurrent onboarding choices cannot
silently replace one another.

### 2.4 Workspace doctor inventory

`noise-tasks doctor` is a read-only workspace inventory for operators:
it discovers the Git repository families under a root and reports, per
family, whether the §8.1 authority exists and whether any retired task
state layout still carries files. It exists to surface the two silent
failure classes the 2026-09-06 workspace audit found: families whose
only task state is a retired layout the binary rightly refuses to read,
and families whose recorded migration never produced a store.

`doctor [--root <dir>] [--json]` accepts no `--file` or `--tag`. `--root`
names the directory to walk; it defaults to the §2 starting directory
(`NOISE_PROJECT_DIR` or canonical CWD). The root needs no Git boundary
of its own and no initialized store anywhere: `doctor` works from any
directory and exits 0 with the report whether or not it finds residue;
operational failures follow §6 and exit 1. It never mutates anything,
never runs Git, and appends no event: it is a filesystem walk plus the
same read-only store facts `context` reports.

Discovery walks the root depth-first without following symlinks and
without descending into any `.git` directory. A directory whose contents
cannot be listed is simply not descended into — its own family still
reports when its facts resolve, since path traversal needs execute
permission, not list permission — and an entry whose own facts cannot
be read contributes nothing; only a root that cannot be walked at
all is an operational failure. A directory whose entry
set contains `.git` — directory or the linked-worktree file — is a
boundary; the family is resolved through the §8.1 common-directory
derivation, so a primary checkout and its linked worktrees are one
family reported once, preferring the primary checkout (the boundary
whose `.git` is a directory) over a linked worktree, with every path
canonicalized to absolute before comparison and reporting. Directories
that carry no `.git` are not families and are not reported: a non-Git
project has no discovery marker, and §2 covers it one project at a
time.

Per family the report carries: the boundary path; the resolved common
directory; the `storePath` derived directly from that common directory,
so a checkout whose `.git` file points at a separately located Git
directory still finds the family's real store; `initialized` (the store
exists as a regular, non-symlink file); when initialized, the same two
read-only store facts as §2.2 schema 5 — `storeOwnerBoundary` and
`storeSchemaVersion` — read through SQLite's immutable read-only mode,
which never takes a lock and never creates or modifies a file
(no maintenance lock, no WAL sidecars); an actively mutating store is
observed at its last checkpoint rather than failing, and a store that
cannot be served that way is reported as
`authorityError` text rather than failing the walk; `recoveryJournal` (a
store or where the store would publish); `legacyLiveJson` (a derived
`<common-dir>/noise-tasks/live.json` exists); and `legacyLayouts`, the
sorted subset of `.taskmaster/tasks/tasks.json`,
`.noisetask/tasks.json`, and `.tasks/tasks/tasks.json` that exist as
regular files at the boundary. The report carries no titles,
descriptions, tokens, environment values, or credentials, and it never
classifies dirtiness of legacy files: it names them with size and
modification time, and reconciling them with Git is the operator's
explicit decision.

`--json` prints one object with `schemaVersion`, `root`, and a
`families` array (always present, empty when none are found) ordered by
boundary path. Human output prints one line per family — the boundary,
`store` or `no store`, the store schema when initialized, and each
retired layout path — then one summary line naming the family count,
how many are initialized, how many have no store, and how many carry
retired layouts.

## 3. Writes

- Transactional: every task-state mutation runs in one immediate SQLite
  transaction per §8.2, covering authoritative read, validation, write, and
  event append. There is no temp-file/rename protocol for task state and no
  document-wide rewrite.
- Minimal: a mutation touches only the target task's row plus `updatedAt`
  and per-tag `completedCount` metadata. Untouched tasks are never rewritten.
- Serialized by the database: concurrent mutating processes serialize on the
  immediate transaction with the configured busy timeout; a mutation that
  cannot acquire the write lock within the timeout refuses rather than
  proceeding concurrently. Read-only commands use deferred transactions and
  observe one committed snapshot.
- Slow work is outside the critical section. `generate` may call a provider
  and produce a §9.2 plan, but never publishes task state. `plan validate`
  is read-only. `plan apply` revalidates the exact accepted digest against the
  current store and publishes its complete batch and receipt in one immediate
  transaction; IDs come from the tag's `nextId` counter in that transaction.
- The non-authoritative `--file` fixture surface writes its standalone JSON
  document atomically (same-directory temp file, mode, fsync, rename,
  directory fsync) with 2-space indent, keys in first-seen order, and a
  trailing newline, preserving unknown fields verbatim per §1.
- `setup` may publish the database and preferences as described in §2.3.
  Only `setup` and `generate` read
  preferences, so malformed or missing preferences never prevent other task
  work in an initialized project.

## 4. Tasks commands (v2.5)

The standalone executable is `noise-tasks`. Every command below is available
as either `noise-tasks <command> <args...>` or
`noise tasks <command> <args...>`. These forms have the same task behavior.
Backlog-scoped commands accept `--file`, `--tag` (default: sole tag, else
`main`), and `--json` (machine-readable output on stdout, nothing else on
stdout); `--file` selects only the non-authoritative fixture surface of
§8.1. Project-scoped `setup`, `context`, and `doctor` do not accept
`--file` or `--tag`; `setup` accepts optional `--json`, while
`context` requires it. `version` is context-free. §8 adds `backup`,
`migrate`, `claim`, `claim-next`, `release`, `live feed`, `receipt`, and
`hygiene`. §2.4 adds `doctor`.
§9 adds `plan`, `run`, and `worktree`. Authority and orchestration commands
never accept `--file`. They require the §8.1 database except context-free
`backup verify`, `plan validate --offline`, and the explicit legacy
backup/migration surface. Every command that requires the database,
when no database
exists, exits 1 with the exact §2 diagnostic
`noise-tasks: project is not initialized; run noise tasks setup`.

Every flag that supplies a fencing token integer — `--token`,
`--claim-token`, `--attempt-token`, and `--saga-token` — has a `-file`
twin with the same name plus `-file` that reads the same decimal
integer from a regular, non-symlink file of at most 64 bytes holding
one trimmed base-10 integer, so an acquired capability can be exercised
without appearing in argv, process listings, or session transcripts.
The integer flag and its file twin are mutually exclusive (a usage
error, exit 2, when both are supplied); supplying the twin satisfies
every requirement expressed on the integer flag. A missing,
non-regular, oversized, empty, or non-integer file is the same usage
error naming the twin. Acquisition output is unchanged: tokens are
returned only by acquisition or replay, and reading one into a private
file for use through the twin is the caller's choice. Token flags and
their twins follow the same `--json` and output-stability rules as the
commands below.
§12 adds the `milestone` command group, the `set-milestone` command, and
the `--milestone` flag on `add`, `list`, and `next`; they are
backlog-scoped and follow the same `--file`/`--tag`/`--json` and
output-stability rules as the commands below.

- `setup [--adapter id --model id] [--yes] [--json]` — perform the canonical,
  rerunnable project onboarding in §2.3. It creates missing canonical task
  state and records one explicit generation configuration without storing
  credentials.
- `init`
- `list [--status s] [--priority p] [--milestone m]` — table or JSON; includes a summary
  line of counts by status.
- `next [--milestone m]` — highest-priority task whose status is `pending` and whose
  dependencies are all `done`/`cancelled`; ties break by lower id. A
  dependency that does not resolve to a task in the tag counts as
  unsatisfied. §12.2: a task assigned to a closed milestone is not a
  candidate; `--milestone` restricts candidates to that milestone. On the
  §8.1 database a task with an unexpired claim or a nonterminal §9.3 run is
  not actionable: `next` never advertises work another owner holds or work
  that requires explicit run recovery, even after its claim expires (§8.3).
  A standalone `--file` document carries no claims or runs, so its candidates
  are unchanged.
- `show <id>` — accepts a top-level id or a hierarchical subtask id. Subtask
  output reports the qualified id and its normalized parent id.
- `add --title t [--description d] [--details x] [--priority p]
  [--dependencies 1,2] [--milestone m]` — prints the new id.
- `set-status <id> <status>` — accepts a top-level id or a hierarchical
  subtask id and refuses unknown ids and unknown statuses.
- `set-milestone <id> <name>` / `set-milestone <id> --clear` — assigns or
  clears a task's `milestone` (§12.2). Top-level ids only; a hierarchical
  subtask id is a usage error.
- `append <id> <text>` — appends a timestamped block to `details`
  (deterministic, no AI): `\n<info added YYYY-MM-DDTHH:MM:SSZ>\n<text>\n</info>`.
- `remove <id> [--force]` — accepts a top-level id or a hierarchical subtask
  id. Top-level removal refuses if any other task depends on `<id>` unless
  `--force`, which also strips the dangling references and says so. Removing a
  subtask deletes only that exact array element: the parent task's other
  fields and every sibling remain byte-identical. A subtask with a dependent
  under §1's normalized bare-sibling or qualified-id grammar refuses removal;
  `--force` is not accepted for a hierarchical id. Operators remove the exact
  dependents first. Normal per-write metadata maintenance still applies.
- `milestone add <name> | milestone list | milestone close <name> |
  milestone reopen <name> | milestone remove <name> [--force]` — the per-tag
  registry command group (§12.2).
- `verify [--against <git-ref>]` — structural integrity: unique ids, valid
  statuses, dependency references exist, no dependency cycles, counters
  consistent. With `--file` it checks the named fixture document and
  `--against <git-ref>` compares it against the file at that ref, proving no
  task present in the ref's file was removed and listing every field-level
  change; `--against` is valid only with `--file`, because authoritative
  task state no longer lives in Git history.
  Removed subtasks are removal findings named by their
  qualified ids, not merely an opaque change to the parent's `subtasks` field.
  Exit 0 clean, 1 findings, 2 error, except the §2
  implicit uninitialized-project refusal exits 1 with its exact diagnostic.
- `context --json` — report the versioned project boundary, §8.1 store path,
  initialization and writability, and validated migration/blocking state
  defined by §2.2. It is read-only and project-scoped.
- `version` — prints `noise-tasks <version>\n` without project or store
  discovery.
- `generate <prd.md> [--file path] [--tag tag] [--num-tasks n]
  [--output plan.json]` — drafts a
  versioned §9.2 plan through a provider interface. This is the only
  networked command. It never writes authoritative state, takes a claim,
  creates a run, or invokes `plan apply`. A configured project selects its
  adapter and model only through the validated §2.3 preferences snapshot.
  Users change that selection by rerunning `setup`; `generate` has no adapter,
  model, executable, or arbitrary-command override. Credentials remain
  adapter-managed. An existing preferences path that is malformed, unsupported,
  oversized, symlinked, special, or otherwise invalid makes generation refuse;
  it is never treated as missing. Preference-less projects retain the bounded
  compatibility behavior in §7 until its evidence gates permit removal.

The provider may deliberately return an empty JSON array when the PRD
has no actionable requirements — pure vision prose with no concrete
capabilities or gates, contradictory requirements that make a faithful
backlog impossible, or empty/gibberish input. This is a disposition
refusal, not a provider failure or a validation error: `generate` exits
1 with `noise-tasks: PRD has no actionable requirements; the provider
returned no drafts` and writes nothing. A partially actionable PRD
generates tasks for its actionable parts only; ambiguity alone never
refuses.

Generation resolves and validates the selected store and tag, then snapshots
the boundary-level generation configuration before reading the PRD or creating
a provider. PRD input is limited to 1 MiB. A larger input refuses before
provider construction. The provider result and preview use that immutable
selection; setup changes during the slow call do not change or discard the
draft. The result must pass the same structural parser as `plan validate`;
provider success never implies that the plan is acceptable against current
state.

Human output prints the adapter, requested model, provider-resolved model, plan
digest, and destination. An unavailable resolved identity is
printed as `unreported`; it is not guessed from an alias. `--json` emits one
versioned object and every generation-identity field is present:

```json
{
  "schemaVersion": 2,
  "generation": {
    "adapter": "omp",
    "requestedModel": "zai/glm-5.2",
    "resolvedModel": ""
  },
  "tag": "main",
  "authoritativeWritten": false,
  "planDigest": "sha256:<hex>",
  "plan": {}
}
```

`resolvedModel` is empty when the adapter does not report an independently
resolved identity. A nonempty resolved identity is subject to the same UTF-8,
length, whitespace, control-character, and leading-hyphen bounds as a requested
model before it is rendered. During the §7 compatibility path, `requestedModel` is
empty only when the historical Claude CLI selection used its ambient default.
Warnings go to stderr under `--json`; stdout
contains only the one object. A refusal leaves stdout empty.

Generated-backlog quality is evaluated under `docs/GENERATION_EVALS.md`.
Structural command tests do not by themselves establish semantic quality. A
provider/model configuration may be described as quality-qualified only for a
named, immutable evaluation version whose frozen locked attestation meets that
contract. This describes evidence and does not add a CLI state, flag, or
authorization mechanism. No generation flag makes generated planning
authoritative. Only a trusted orchestrator or operator that accepts its digest
and calls `plan apply` can publish it.

`generate` is canonical. `parse-prd` is an exact transition-only alias during
§7. `import` is not a command or alias.

- `plan validate <plan.json> [--offline] [--json]` / `plan apply <plan.json>
  --digest sha256:<hex> --request-id <id> [--json]` — the §9.2 atomic plan
  contract.
- `claim <id> --owner <o> --request-id <id> [--ttl s]` / `claim-next --owner
  <o> [--ttl s] [--milestone m] [--assignment <owner>] --request-id <id>` /
  `claim <id> --owner <o> --token <n> --renew [--ttl s]` / `release <id>
  --owner <o> --token <n> [--force]` — the §8.3 claim lifecycle.
- `run start|retry|renew|finish|cancel|show|list` — durable run and attempt
  leases and receipts (§9.3).
- `dispatch prepare|reconcile` — replayable orchestration preparation and
  recovery (§9.3B).
- `live feed [--since <seq>] [--json]` — replay the §8.4 change feed.
- `backup create --output <bundle> [--legacy]` / `backup verify <bundle>` /
  `backup restore <bundle> [--overwrite] --offline --request-id <id>` — the
  authority-neutral §8.5 protocol.
- `migrate plan [--json] <bundle>` / `migrate apply --digest
  sha256:<hex> --offline --request-id <id> [--json] <bundle>` — the one-way §8.6
  cutover. The plan is always the derived
  `<git-common-dir>/noise-tasks/migration-plan.json`; no caller-supplied plan
  or output path can become migration state. There is no quarantine or
  finalize command.
- `receipt show <kind> <id> [--json]` / `receipt verify <kind> <id> [--json]`
  — inspect durable proof records (§8.4).
- `worktree provision|remove|recover|list|prune` — the §9.4 saga.

### 4.0 Generated task text contract

Generated task `title`, `description`, and `details` are provider-neutral
plain text with one Markdown allowance each, chosen so every consumer
that renders task text — card views, terminal listings, exported
reports — can do so safely without a full Markdown implementation:

- **Title**: a single line of at most 200 bytes, no newlines, no
  Markdown syntax. It is an imperative summary, never markup.
- **Description**: one sentence of at most 500 bytes on a single line,
  no Markdown syntax.
- **Details**: at most 8 KiB of text that may use exactly four Markdown
  constructs — bullet lists (`- `), inline code spans (\`\`), bold
  (**), and section headings introduced by a literal `## ` at line
  start. No links, no images, no HTML, no fenced code blocks, no
  tables. Every line is at most 120 bytes before rendering.

`generate` and every §4.1 adapter prompt states this contract and
requests it. Machine enforcement is deliberately narrower than the
contract: the tail validator rejects a draft whose title or description
contains a line break or exceeds its byte bound (200/500), because
those violations corrupt every consumer's rendering. The
no-Markdown-sentence shape of title and description, the details size
bound, and the four allowed details constructs are provider guidance
carried in the prompt, not machine-checked: a draft that passes
validation may carry advisory prose outside the guidance, and consumers
must render title and description as plain text (the validator's
single-line guarantee is what makes that safe) and render details with
their own Markdown handling if they choose to render it at all.

### 4.1 Generation adapter boundary

The initial configured adapter allowlist is `anthropic-api`, `claude-cli`, and
`omp`:

- `anthropic-api` is an in-process HTTPS adapter. It takes authentication from
  `ANTHROPIC_API_KEY` at invocation time and never persists it.
- `claude-cli` executes the fixed basename `claude` directly as
  `claude -p --output-format text --model <model> --no-session-persistence
  --safe-mode --tools ""`. Each shown token is one argument, including the
  empty tools value. The configured model is one argument.
- `omp` executes the fixed basename `omp` directly as
  `omp -p --mode text --no-tools --no-session --no-skills --no-rules
  --no-extensions --model <model>`. Each shown token is one argument. This
  provides a generic route to models supported by the user's OMP installation
  without making a project command executable.

CLI adapters resolve their fixed basename from the invocation's user-controlled
`PATH`; that environment is an installation trust input, not project data.
Setup and generation reject a result contained by the project boundary,
require a canonical absolute regular executable with an executable mode, and
invoke it directly without a shell. The project cannot select the basename,
path, arguments, environment, working-directory escape, or another program.
Adding a CLI adapter requires a reviewed product change to this allowlist.

Every adapter receives the same bounded prompt contract and returns raw model
text plus requested and provider-resolved model identity when the provider
exposes it. Calls have explicit time and output bounds. The task engine owns
prompt construction, response parsing, draft validation, dependency-cycle
checks, preview, and plan serialization; an adapter cannot write the backlog.
Child stderr and authentication output are bounded and reduced to
sanitized error classes, never copied verbatim into diagnostics or project
files. A model that cannot be pinned and resolved may be used as an
unqualified draft aid, but cannot satisfy `docs/GENERATION_EVALS.md`.

One CLI call has a five-minute timeout, an 8 MiB stdout cap, a 64 KiB stderr
cap, and a ten-second post-cancellation wait bound. Buffers remain bounded while
surplus bytes are drained so a child cannot deadlock the parent. Crossing
either cap refuses the run. Nonzero exit, timeout, cancellation, output-limit,
and start failures are reported only as closed error classes (and a numeric
exit code when available); raw child stderr, authentication output, executable
output, and credentials are never copied into diagnostics. The configured
project boundary is the child working directory. The in-process API response
body uses the same 8 MiB cap.
Non-success API response bodies are not copied into diagnostics; only the HTTP
status class is reported.

The adapter and requested model are shown with the draft and in JSON
output. A run continues with that invocation-time snapshot even if setup
changes preferences concurrently. It never reselects based on a newly present
credential, executable, subscription, or ambient default.

## 5. Umbrella host

### 5.1 Scope and grammar

`noise` is a small host. It owns only host version output, host help, installed
plugin discovery, and noun dispatch. It does not implement task behavior,
parse plugin output, load project configuration, install or update plugins, or
search for executables.

The first non-host argument is a plugin noun. A noun must match
`^[a-z0-9]+(?:-[a-z0-9]+)*$` and must not start with `noise-`: one or more
lowercase ASCII alphanumeric components separated by single hyphens. A noun
maps to exactly one entry name, `noise-<noun>`. There is no other prefix,
suffix, extension, or nested path form. In particular, uppercase characters,
leading or trailing hyphens, repeated hyphens, slashes, backslashes, dot
segments, absolute paths, and an input such as `noise-tasks` as an alternate
spelling of `tasks` are rejected as invalid nouns before filesystem access.

`help`, `version`, and `plugins` are reserved host words, not plugin nouns. An
installer must reject entries for those nouns, and discovery never lists them.

The host-reserved forms are:

- `noise`, `noise help`, `noise --help`, and `noise -h`: print static host
  usage to stdout and exit 0. Host help does not execute a plugin or require a
  valid installation. Plugin help is `noise <noun> --help`.
- `noise version` and `noise --version`: print `noise <version>\n` to stdout
  and exit 0 without consulting the plugin root.
- `noise plugins`: print the discovered nouns, one per line, in ascending
  bytewise order, then exit 0.
- `noise <noun> <args...>`: dispatch the noun. `help`, `version`, and
  `plugins` are reserved and cannot be dispatched as plugin nouns.

Host-generated success output goes only to stdout and writes nothing to
stderr. A usage error, including an invalid noun or extra arguments to a
reserved form, writes a diagnostic and short usage to stderr and exits 2.
Help and version remain available on an otherwise unsupported platform.
Discovery and dispatch support Linux and macOS; on another platform they
write `noise: unsupported platform: <GOOS>\n` to stderr and exit 1 without
filesystem lookup.

### 5.2 Installation-selected plugin root

The `noise` and transition-only `noisetask` builds contain the same absolute
libexec root selected by the build or package. A direct build uses
`/usr/local/libexec/noise`. A platform package may select its managed prefix,
including `/usr/libexec/noise` or `/opt/homebrew/libexec/noise`. The selected
value is part of each executable; no runtime environment variable,
command-line flag, current directory, `PATH`, executable location, or project
file can replace or augment it.

The installer or setup owner establishes trust once, at the boundary that owns
the installation. It declares the principals allowed to modify the
installation and any runtime agent identities that must not be able to modify
it. Those declarations are build or packaging inputs, not host runtime
configuration. Setup then validates and publishes the complete root atomically:

1. The recorded root is absolute and lexically clean, outside
   workspace-writable trees, and on a supported local filesystem. Setup
   validates every directory and symlink on both the recorded and eventual
   canonical paths. Ownership, mode bits, groups, ACLs, sticky-directory
   semantics, and mount policy must ensure that only declared setup principals
   can write the published tree or replace a path component. No declared
   runtime agent identity may write or replace the root or a plugin.
2. A staged root contains only direct `noise-<noun>` entries for valid,
   non-reserved nouns. Each canonical target remains beneath the staged root,
   is a regular file with an executable mode, and cannot be replaced by a
   principal outside the declared setup authority. Symlink escapes, invalid
   names, non-regular targets, and undeclared writable state fail setup.
3. After validating and syncing the complete staged tree, setup publishes it
   in one package-manager transaction or same-filesystem atomic rename. An
   upgrade publishes a new immutable versioned tree and atomically replaces
   the recorded root link; it never repairs or mutates the live tree one file
   at a time. The published version remains intact while a dispatcher can have
   resolved it.
4. The package records the same root in `noise`, `noisetask`, and
   `noise-tasks` (the standalone build resolves the §9.5 broker from
   it), installs the corresponding `noise-tasks` there, and fails
   atomically if any value or
   destination disagrees. Changing setup principals, runtime identities, or
   the selected prefix requires setup to validate and republish the
   installation.

A setup failure exits nonzero, reports the rejected path and trust condition,
and leaves the prior published installation unchanged. The host never repairs
an installation or attempts to complete interrupted setup.

UID 0 is necessarily a setup authority: a process with effective UID 0 can
replace any ordinary Unix installation regardless of mode bits. Root
invocation is supported and consumes that trusted installation; v2.5 does not
claim to isolate the installation from a root-running agent. A system
deployment that needs that isolation runs agents as a distinct non-root
identity denied write access by setup.

A user-owned Homebrew prefix is also coherent when its owner is a declared
setup principal. `/opt/homebrew/libexec/noise` and a Homebrew-managed
`/usr/local/libexec/noise` may have user- or trusted-group-writable ancestors;
setup accepts them only when every writer is a declared setup principal.
Interactive invocation by that owner is trusted in the same sense as root
invocation. An autonomous agent running as the Homebrew owner cannot be
declared isolated from that installation; such a deployment must use a
separate agent identity or a system-managed prefix.

Normal discovery and dispatch consume this established invariant rather than
repeating setup. Bounded runtime resolution canonicalizes the recorded root
once, requires it to be a directory, forms only
`<canonical-root>/noise-<noun>`, canonicalizes that target, verifies strict
containment beneath the same canonical root, and requires a regular file with
an executable mode. It does not re-walk ancestor ownership, groups, ACLs, or
permissions. A concurrent change by setup authority is governed by the atomic
publication protocol; any other writer means the installation boundary was
not established and is outside the host's threat model.

A missing, unresolved, or non-directory root is an installation error:
`noise` writes a diagnostic naming the configured root to stderr and exits 1.
If a valid root has no entry for a valid noun, dispatch writes
`noise: plugin not installed: <noun>\n` to stderr and exits 127. If the named
entry exists but fails canonicalization, containment, type, or executable
checks, dispatch identifies it as an unsafe plugin on stderr and exits 126. It
never falls back to `PATH` or another directory.

Discovery reads only direct entries of the canonical root. It considers only
the single `noise-` prefix with a valid, non-reserved noun, applies the same
bounded runtime checks as dispatch, omits nonmatching and unsafe entries, and
sorts the nouns bytewise. A valid empty root produces no output and exits 0. A
root-level installation error fails discovery rather than reporting an empty
installation.

### 5.3 Plugin surface contract (v1)

Every `noise-<noun>` plugin presents the same minimal text surface, so an
operator can discover any plugin identically without knowing its domain. The
surface is part of the host contract; this section is normative for every
plugin, and the public `conform` package's `TestPlugin` probe asserts it
mechanically against a built binary.

- `noise-<noun>`, `noise-<noun> help`, `noise-<noun> --help`, and
  `noise-<noun> -h`: print top-level usage to stdout and exit 0. These forms
  work from any working directory and require no installation state, project
  root, configuration file, credential, or network access. Help is
  side-effect-free: it creates or modifies no file, takes no lock, and
  returns promptly. Success output goes to stdout only; stderr stays empty.
- Top-level usage has exactly this parseable shape: a first line
  `usage: noise-<noun> <command> [options]`; a blank line; a `commands:`
  line; one line per command as two spaces, the command name, two or more
  spaces, and a nonempty one-line description; a blank line; and a final
  footer line naming how to reach per-command flags. Every listed command
  dispatches, and every dispatched command is listed — no phantom or hidden
  commands. The two directions have different owners: a black-box probe can
  only observe that listed commands dispatch, so the plugin must generate the
  list from the same command table that dispatch consumes, and that
  single-source invariant is verified by the plugin's own tests and review.
  The `conform` probe asserts the listed-to-dispatched direction; it cannot
  and does not enumerate commands a binary accepts but does not list.
- Per-command help: `noise-<noun> <command> -help` or
  `noise-<noun> <command> --help` — at least one form is required, both are
  preferred — prints a usage line naming the command followed by the
  command's flags and their defaults, and exits 0. A usage line is a line
  whose first word is `usage` (any letter case) and that contains the
  command name; the `conform` probe parses for exactly that line, and flag
  and default content behind it is verified by the plugin's own tests. The
  usage stream is the plugin's choice.
- `noise-<noun> version` prints exactly one `noise-<noun> <version>` line
  to stdout — the plugin name, one space, and a SemVer 2.0 version (or the
  literal `dev` for a development build, §5.5) — and exits 0. Like the help
  forms it works from any working directory, is side-effect-free, and
  performs no project or backlog discovery. The `version` subcommand is the
  only contracted version form; a `--version` flag is not part of the
  contract.
- An unknown command writes a diagnostic naming the rejected command and a
  short usage that points at the help forms to stderr, writes nothing to
  stdout, and exits nonzero.

### 5.4 Process-transparent dispatch

For `noise tasks <args...>`, the only candidate is the validated absolute
`<libexec-root>/noise-tasks`. Immediately after validation the host calls
`syscall.Exec` with that absolute target. The plugin receives
`argv[0] == "noise-tasks"`; every argument after the noun is preserved in
order and byte-for-byte as `argv[1:]`. The host does not parse plugin flags.

The host passes the inherited environment unchanged except for ensuring
exactly one `NOISE_PROJECT_DIR` entry. If a nonempty value is inherited, the
host preserves it for `noise-tasks` to resolve under §2. If it is absent or
empty, the host replaces it with canonical CWD. The host does not inspect Git,
backlog paths, or plugin flags. This is the only environment difference between
umbrella and direct invocation; both give the plugin the same logical starting
directory for the same CWD and inherited context. All other environment
entries retain their values. File descriptors 0, 1, and 2 are inherited
without buffering or interception. Because `syscall.Exec` replaces the host
process, signals and the plugin's exit status are observed directly; the host
does not translate them. If `syscall.Exec` itself returns, the host reports the
execution error on stderr and exits 126.

`noise-tasks` is also a standalone executable and must not depend on being
started by `noise`. It alone resolves project and backlog context under §2.
Direct invocation otherwise has the same standard streams, environment,
signal, and exit behavior as any normal process.

### 5.5 Packaging surface

An installation contains `noise` and, during §7, the `noisetask` shim in its
normal executable directory. All packaged executables record the same
libexec root. It places
`noise-tasks` in that root and exposes the same release for direct invocation
in the normal executable directory using a package-managed copy, hard link, or
symlink. The host and shim never resolve the direct entry. A package whose
embedded roots or plugin destination disagree fails installation atomically.

The host and plugin report the same release version. `noise-tasks version`
prints `noise-tasks <version>\n`; umbrella invocation produces the same plugin
output. Development builds use the literal version `dev`. Version commands do
not perform project or backlog discovery.

## 6. Error and output stability

Plugin command usage errors exit 2. Operational failures exit 1 unless a
command specifies another status; `verify` retains its 0/1/2 contract. A
typed stale-write or claim conflict under §8 exits 3. Human
diagnostics go to stderr. Under `--json`, stdout is either the command's one
complete JSON value or empty, including on refusal; warnings never corrupt
stdout. Umbrella dispatch adds no output of its own before a successful exec.

## 7. Managed compatibility window

The canonical identities are `noise tasks`, `noise-tasks`, `generate`,
`.tasks`, and the closed preferences file. `NOISE_PROJECT_DIR` remains the
process-context variable from §2; generation selector variables are now
compatibility-only. The compatibility window deliberately decouples executable,
configuration, and state migration:

- `noisetask <args...>` writes this exact line followed by one newline to
  stderr once per process:

  ```text
  warning: noisetask is deprecated; use noise tasks (or noise-tasks); removal is gated by the published migration plan
  ```

  It then resolves and execs the installation's `noise-tasks` exactly as the
  host does. It contains no task logic, argument rewriting, PATH search, or
  alternate data model.
- `parse-prd` remains the exact `generate` alias, and the three
  `NOISETASK_*` names participate in the selector compatibility behavior
  defined below.
- An initialized project without the boundary-level
  `.tasks/preferences.json` remains task-capable. During the compatibility
  window only, `generate` also retains the previous provider selection contract
  for any such preference-less project:
  `NOISE_TASKS_PROVIDER=api|claude-cli` selects a provider,
  `NOISE_TASKS_MODEL` selects a model, and
  `NOISE_TASKS_CLAUDE_EXECUTABLE` selects the CLI executable; the corresponding
  `NOISETASK_*` names are aliases. Unequal nonempty values in an alias pair
  refuse before provider access without printing either value. Without a
  provider override, a present `ANTHROPIC_API_KEY` selects the API, otherwise
  an available Claude CLI is selected, otherwise generation refuses. Before
  use, a compatibility-selected executable passes §4.1's same canonical-path,
  regular-file, executable-mode, project-containment, and direct no-shell
  invocation checks; the override weakens none of them. Before
  any compatible generation attempt, stderr receives this one-line warning:

  ```text
  warning: generation preferences are not configured; run noise tasks setup; ambient provider selection is deprecated
  ```

  A valid preferences file is authoritative. If any provider, model, or CLI
  executable selector variable above is then nonempty, generation refuses and
  asks the user to remove it or rerun setup; it never mixes project preferences
  with ambient selectors. Authentication variables such as
  `ANTHROPIC_API_KEY` remain allowed because they contain no selection state.
- New onboarding, documentation examples, package entry points, and new
  integrations use only canonical identities and explicit preferences.

The retired projection layouts remain closed and inert. The exact derived JSON
live store is accepted only by v2.5's backup/migration compatibility surface;
normal commands never use it. New integrations and backups are SQLite-only,
and restore never recreates a retired format.

Compatibility removal is a separate reviewed change. It is forbidden until
all of these observable gates are satisfied:

1. The maintained workspace inventory reports no tracked invocation,
   configuration, build input, or documentation requirement for `noisetask`,
   `parse-prd`, or `NOISETASK_*` outside historical records.
2. Every supported consumer has adopted a release containing canonical entry
   points and has a tested rollback procedure.
3. Every supported consumer that invokes `generate` has a valid reviewed
   preferences file, has removed selector/executable environment overrides,
   and has successfully run its selected adapter in a disposable canary.
4. The removal PR includes a fresh inventory artifact and is announced as a
   breaking release. Compatibility is never removed merely because one release
   elapsed.

The change-management runbook and the current workspace inventory for these
gates are maintained working records, kept outside this specification.

## 8. Live authority (repository family, SQLite)

This section defines the single task-state authority for a repository
family: one SQLite database shared by the primary checkout, every linked
worktree, and every agent harness, so all of them resolve the same task
state regardless of branch. No file inside a Git worktree is task state.
The committed `.tasks/tasks/tasks.json` projection and JSON live store are
retired: this section is their replacement, and §8.6 defines the one-way
migration compatibility surface. Independent clones
remain independent repository families; synchronizing them is out of
scope and would require a server or replication protocol, which v2.5 does
not provide.

### 8.1 Store location and discovery

Live discovery extends §2; it does not duplicate it. After the §2
repository boundary is found, the Git common directory is resolved: when
`.git` at the boundary is a regular file, its `gitdir:` target is read;
the common directory is then that gitdir's `commondir` file contents when
present, else the gitdir itself. The live authority is the single
database `<common-dir>/noise-tasks/tasks.sqlite`, created with mode
0600. Because linked worktrees share one common directory, every checkout
of the repository family resolves the same database. The path is derived,
never configured: no command flag, environment variable, project file, or
workspace setting can redirect it.

Store metadata records one immutable `ownerBoundary`: inside Git, the canonical
primary working-tree root registered for the common directory when the family
is initialized, upgraded, or migrated; outside Git, the initialization
boundary. Absence or ambiguity of that Git root refuses creation. Linked
worktrees resolve the shared database through the Git common directory but do
not replace this owner. Sandboxed task worktrees remain beneath
`<ownerBoundary>/.worktrees`; Git's administrative registrations remain
beneath `<common-dir>/worktrees`. These paths have different purposes and are
never substituted for one another.

Outside Git, or when the boundary's common directory cannot be resolved,
the authority is `<boundary>/.tasks/tasks.sqlite` with the same mode and
derivation rules; it is never committed because it does not live under
version control in that case. Inside Git the database lives under the
common directory and therefore never appears in any worktree.

If a store was initialized outside Git at `<boundary>/.tasks/tasks.sqlite` and
that same boundary later becomes a Git working tree, discovery fails closed
instead of selecting the new common-directory path. `setup`, `init`, and every
normal command report
`noise-tasks: existing outside-Git task store cannot be rehomed automatically; back it up and restore it into this repository family`
(exit 1). The operator removes or disables the new Git metadata, backs up the
old authority while it is again the derived store, and restores that bundle
offline only into a clean repository family that does not contain the old
store. No command moves, merges, or creates a second authority implicitly.

After the recovery and rehoming checks above, the §2 missing-state refusal
(`noise-tasks: project is not initialized; run noise tasks setup`) fires
when no database resolves. A fresh clone of a migrated repository is
exactly this intentional no-database state: commands fail closed with
that diagnostic, and only `setup`, `init`, `context`, and `version`
proceed. `setup` and `init` bootstrap the SQLite authority directly —
`init` noninteractively creates the database with an empty `main` tag;
neither command ever creates a worktree task-state file. An explicit
`--file` is a non-authoritative fixture surface for tests and evaluation
only: it operates on the named standalone JSON document, is never
selected by discovery, never becomes an authority, and does not support
claims, worktree bindings, revisions, or the feed. Backlog-scoped
commands accept `--file` solely in that capacity.

### 8.2 Schema, records, and transactions

The database stores the §1 model relationally. Its minimum durable records
are: one immutable `familyId` and `ownerBoundary`; schema metadata; tags and `nextId`
counters; milestones; tasks, dependencies, subtasks, and byte-faithful unknown
fields; assignments; claims and one store-wide monotonic fencing counter
shared by claim tokens, attempt tokens, saga fences, and forced-cancellation
fences;
legacy bindings retained only for verified import/cleanup; events and immutable
receipts; applied plans and local-key mappings; runs,
attempts, leases, and worktree sagas. Foreign keys, uniqueness, and check
constraints enforce local invariants; `verify` checks cross-row and graph
invariants.

`setup`, `init`, and the first v2.5 upgrade of a released SQLite schema mint
`familyId` from a cryptographically secure random UUID; legacy backup/migration
use §8.5's deterministic derivation. That first upgrade also records the Git
primary working tree as owner. Once stored, no normal mutation, restore, or
later schema upgrade changes either value.

The binary owns the schema. The schema version is recorded in
`PRAGMA user_version`; v2.5 writes `user_version = 5`. Version 2 introduced
immutable family identity, outer-record preservation, and receipts. Version 3
adds the §9.2 `plans`, `planMappings`, and `assignments` records and their
indexes. Version 4 adds the §9.3 `runs` and `attempts` records and indexes.
Version 5 adds the §9.4 `worktree_sagas` records and indexes and converts
legacy bindings as defined there. A normal mutation transactionally traverses
every required step through v5 before applying its effect; no later step
changes family identity. Opening a database whose schema version is newer
than the binary's refuses fail-closed (exit 1) with
`noise-tasks: task store schema is newer than this binary; upgrade noise-tasks`.
Schema changes apply as transactional DDL migrations at open time of a normal
mutation, never while backup, restore, or migration is deciding source state.
The database runs in WAL mode with an explicit busy timeout. Normal processes
hold a shared lock on `<common-dir>/noise-tasks/maintenance.lock` from before
opening the database until after closing it; offline restore and migration
apply hold the exclusive lock. This coordinates supported binaries. The
operator must also stop older binaries and non-cooperating SQLite clients.

Read-only commands use deferred transactions. Every authoritative mutation
runs in one immediate transaction covering its current-state read, validation,
write, event, and receipt, then commits before reporting success. Provider
calls, Git commands, broker calls, filesystem copies, and fsyncs never run
inside that transaction. SQLite transactions, not document replacement, are
the normal-state atomicity mechanism.

Compare-and-swap on the live authority keeps its public contract; the fixture
surface has no revision API. Every live mutation of an existing task's §1
fields or status supplies `--revision <n>`; omitting
it is a usage error (exit 2) naming the flag. The mutation applies as a
conditional update matching `(tag, id, revision)`; zero rows affected is
a **typed stale-write conflict**:
exit 3, empty stdout, and stderr carrying the task, supplied revision,
current revision, and current status and claim (JSON under `--json`,
human-readable otherwise). The stale writer re-reads and reconciles; the
tool never retries implicitly. `add` takes its id from the tag row's
`nextId` inside the same immediate transaction, so id allocation is
collision-free across processes and worktrees without any document-wide
scan and the counter is never lowered after removal. `remove` requires
`--revision`; `remove --force` strips dangling
references transactionally, incrementing each affected task's revision
and appending its `update` event in the same transaction. Claim
operations and §9 run-lease or worktree-saga mutations neither require nor
increment task `revision`; they use their own conditional fencing tokens.
The optional task-status sub-effect of §9.3 `run finish` is the one exception
to the diagnostic shape above: its stale-revision check still precedes
fencing, but returns the redacted run conflict defined there and does not
disclose current revision, status, claim, owner, or expiry. The Trusted
Orchestrator re-reads the task explicitly before deciding whether to reconcile
or abandon the run.

A live subtask status mutation supplies and conditionally increments its
parent task's revision even though the parent task's §1 fields and `updatedAt`
remain byte-identical. Every other live field/status mutation increments the
selected top-level task revision. `show` of either a parent or subtask reports
the parent revision required for the next mutation.

`show` and `list --json` report current revisions so a caller obtains one
in the same step as reading state.

### 8.3 Claims

An acquisition succeeds only when the task has no unexpired claim and no
nonterminal run. Claim expiry never transfers or abandons run ownership: a
task with a `running` or `retryable` run remains unavailable to `claim` and
`claim-next` until the Trusted Orchestrator explicitly force-cancels that run
(directly or through force release). A later acquisition is then a new run
lineage. It
increments the store-wide fencing counter and stores `(owner, token,
acquiredAt, expiresAt)` in the same transaction. Expiry is judged against the
database process's UTC clock when an operation reads the row; there is no
reaper. Claim acquire, renew, release, and expiry never change task status.
The token is returned only by acquisition or an idempotent replay to
the same owner and request, and is omitted from list, show, feed, receipts,
diagnostics, and logs. Owner and assignment labels share one grammar with
§9.2 plan-assignment owners — a trimmed, nonempty UTF-8 string of at most
256 bytes containing no ASCII control character; a `--owner` or
`--assignment` value violating it is a usage error (exit 2) reported
before any store is opened or mutated.

Acquisition — in `claim` and `claim-next` alike — records one immutable
§8.4 `claim` receipt inside the same immediate transaction as the fenced
write and its `claim` event. The receipt carries the normalized operation
(`claim` or `claim-next`), tag, task id, owner, ttl, acquiredAt,
expiresAt, the task revision read inside that transaction, and a fresh
unique acquisition identity — never the fencing token — and its input
digest is the lowercase `sha256:<hex>` SHA-256 of the §8.6 canonical JSON
object containing exactly `assignment`, `milestone`, `operation`, `owner`,
`requestId`, `tag`, `task`, and `ttlSeconds`. All keys are always present:
`task` is the selected id for `claim` and the empty string for `claim-next`,
because claim-next selection is a result; absent milestone/assignment filters
are empty strings; TTL is the normalized integer after applying its default.
There is no additional digest domain prefix. The receipt's `resultDigest` is
likewise lowercase `sha256:<hex>` SHA-256 with no additional prefix over the
canonical JSON object containing exactly `acquiredAt`, `acquisitionId`,
`expiresAt`, `owner`, `revision`, and the selected `task`. The acquisition
identity is a random non-secret value
persisted with the live claim, preserved unchanged by renewal, and
recorded in the receipt; it binds replay to one exact acquisition without
revealing or making the sequential fencing token guessable, so a
same-owner same-second reacquisition after release or expiry is a
different acquisition. Replay re-reads the task inside its own
transaction and returns the live token only after verifying the receipt's
canonical body, stored and embedded digests, envelope identities, normalized
input digest, result digest, and linkage to the live acquisition. The recorded
acquisition — same task, owner, and acquisition identity — must still be the
task's unexpired claim; a malformed or altered receipt fails closed without a
token, and a released, expired, or superseded acquisition is a typed exit-3
conflict that discloses no token and no other holder's identity. Replay reports
the revision recorded with the acquisition even
if the task was mutated later, and renewal preserves owner, acquiredAt,
and the acquisition identity, so replaying a renewed acquisition returns
its current token and expiry without extending them. A claim-next that
selects no candidate is the successful empty result and records no
receipt: §8.4 replay is keyed by a committed effect, so a request id is
never reserved without one and the same id may later perform a real
acquisition. The acquisition's audit event is `kind=claim`,
`action=acquired`, and carries the receipt id and receipt digest and no
token (§8.4).

Fixed claim digest vectors (the first line in each pair is the exact UTF-8
input without a trailing newline):

```text
{"assignment":"","milestone":"","operation":"claim","owner":"agent-a","requestId":"req-1","tag":"main","task":"7","ttlSeconds":300}
sha256:52a1a004dbb2555aba4ff30d436e32227a0cc7455bfd0a5f691700bacf379dd7
{"assignment":"agent-a","milestone":"M1","operation":"claim-next","owner":"scheduler","requestId":"root.cn","tag":"main","task":"","ttlSeconds":900}
sha256:065e223fdef031d2a6d1a7173e6cf4d2cc8dfe3a116e30e4a6b0b3d8d4738a9e
{"acquiredAt":"2026-08-28T12:00:00Z","acquisitionId":"00112233445566778899aabbccddeeff","expiresAt":"2026-08-28T12:05:00Z","owner":"agent-a","revision":3,"task":"7"}
sha256:184ca831045fd0bd7aa6b77793498b6151454d11ebf435652f5b3c89541bc468
```

`claim-next --owner <o> [--ttl s] [--milestone m] [--assignment <owner>]
--request-id <id>` combines selection and acquisition in one immediate
transaction. Selection is
exactly `next`'s rule under §12.2 — `pending` status, every dependency
resolved in the tag and `done`/`cancelled`, milestone absent or open — plus
three claim-specific exclusions: a task with an unexpired claim, a task with
a nonterminal run even when its claim expired, and, when `--assignment
<owner>` is supplied, any task without a recorded
assignment to that owner. Assignments are the durable §9.2 plan records,
so the filter matches exactly the tasks the tag assigns to that owner; a
store or owner with no matching assignments leaves no candidates, and
the filter never falls back to unassigned work, because claim-next never
advertises work it cannot actually claim. Ordering is deterministic:
priority high > medium (absent) > low, ties by lower id. Every dependency and
filter is re-evaluated after the write lock is held, inside the transaction;
an unknown `--milestone` value is a usage error (exit 2) as in `next`, while
a milestone removed after that check simply leaves no candidates. Expiry is
judged inside the same transaction, so a lease that expired by the database
clock is claimable only when no nonterminal run remains, and one that did not
excludes its task. The acquisition
follows §8.3's fencing rules unchanged: the store-wide counter increments,
the task revision is neither required nor incremented (the reported revision
is read inside the transaction), and one `claim` event is appended for the
audit feed. On success the output is `claim`'s acquisition shape plus the
task id — the token appears only there. When no candidate remains the result
is `next`'s successful empty result: exit 0, `no actionable task`
human-readable, JSON `null`; claim-next surfaces no claim conflict, because
selection and the conditional acquisition write are one transaction, so two
concurrent callers cannot receive the same task. Acquisition requires
`--request-id`: replay with the same owner and normalized inputs returns the
original task and token while that acquisition remains recorded; reuse of the
request id with different normalized inputs is the §8.4 typed conflict, and a
stale acquisition replays as the typed no-token conflict above — never a
second acquisition and never another owner's token.

Bare claim renew and ordinary release condition on task, owner, and token;
both refuse while a nonterminal run exists. `run renew` is the sole operation
that jointly replaces a live claim and attempt lease. Force release
is an explicit operator override, records the displaced owner but never its
token, and fences any nonterminal run and its current attempt in the same
transaction. A task
field or status mutation on an unexpired claim requires `--owner` and
`--token`; when a live attempt exists it also requires `--run`, `--attempt`,
and `--attempt-token` from §9.3. All values participate in the mutation's
single conditional SQL write. Revision, structural, dependency, and proposed-
value validation errors precede fencing errors when the request is
independently invalid; validation is side-effect free. A stale token, owner mismatch, occupied claim, or
live-run mismatch is exit 3. No claim operation alters a task's durable status.
Removing a claimed task — or a subtask of a claimed parent — is such a
mutation and takes the same gate. A forced removal that strips dangling
references rewrites each dependent's §1 fields, and `milestone remove
--force` clears referencing tasks' milestone fields. The supplied gate can
authorize only the command's named target (or a subtask's parent); any
secondary task holding an unexpired claim refuses the whole transaction,
even if its owner and token were supplied. There is no operator override for
collateral field mutation; force release is the only override §8.3 defines.

The pre-v2.5 compound flag `claim --worktree` is removed by the schema-v5
cutover. It is a usage error with the §9.4 guidance and can neither create an
unrecorded effect nor assert false acquisition/worktree atomicity. Renewal and
release take no `--request-id`; they remain token CAS operations.

### 8.4 Change feed

The event table has a monotonic `seq` and retains the most recent 10,000 rows.
Every committed mutation appends either one event or one documented bulk
summary in its transaction; compaction happens there too. Events contain only
kind, tag/task/run identifiers, action, owner labels, count, revision or fence
numbers, receipt id/digest, and timestamp. They never contain titles, descriptions,
details, prompts, model text, terminal output, transcripts, paths, credentials,
tokens, environment values, or reasoning. `live feed [--since]` reports
`oldestRetainedSeq` and a typed truncation indicator. It is pull-only.

Event `kind` is exactly `task | status | milestone | claim | plan | run |
attempt | worktree | restore | migration | schema`; `action` is the command's
lowercase lifecycle verb (`created`, `updated`, `removed`, `acquired`,
`renewed`, `released`, `applied`, `started`, `retried`, `finished`,
`cancelled`, `planned`, `bound`, `recovering`, `needs-broker`,
`cleanup-required`, `restored`, `removing`, `failed`, `migrated`, or
`upgraded`). A kind/action combination not
defined by its command is invalid; bulk summaries use that command's action
and `count`.

Receipts are immutable canonical-JSON proof records retained independently of
feed compaction. A receipt contains `schemaVersion`, `kind`, `receiptId`,
`familyId`, request id when applicable, input digest, result digest, inventory,
event sequence range, and commit time. Operation-specific fields are defined
below. The input digest excludes claim, run, broker, and credential capability
values; a receipt never makes a reusable secret guessable. `receiptDigest` is
SHA-256 over the domain string
`noise-tasks-receipt-v1\0` and the canonical receipt excluding that field.
`receipt verify` recomputes the digest and checks referenced durable rows. This
proves what this store committed and detects alteration; it is not a signature
and does not prove who made the decision.

Receipt `kind` is exactly `task | milestone | claim | plan | run | worktree |
restore | migration | schema`. Every verb that accepts `--request-id` uses one
store-wide replay rule keyed by `(kind, requestId)`: identical normalized,
non-secret inputs return the original receipt and public response without
repeating effects; different inputs are exit 3. Acquisition replay may return
its token only to the original owner and only after the verification required
by §8.3; alteration fails closed and never falls back to reacquiring. Other
operation-specific receipt-verification rules are defined with those
operations. The receipt is written in the same transaction as the durable
effect, so no request-id verb implements a private replay table or a different
collision rule. The §9.4 saga is the one staged-effect exception: its
authoritative saga row reserves `(worktree, requestId)` before the non-SQLite
effect exists, and supplies collision and resume decisions until a terminal
receipt can be written. It is an audited domain record, not a private replay
table; terminal replay is receipt-based under the same common rule.

### 8.5 Durability, corruption, backup, and restore

The store path must be a regular, non-symlink file; a symlink, directory,
or special file at the path fails closed. Opening a database runs
SQLite's own validation; a corrupt, truncated, or otherwise unreadable
database fails closed with
`noise-tasks: task store is not a valid SQLite database: <path>` (exit 1)
and never attempts implicit repair that could discard committed
transactions. Permission and I/O failures fail closed with the failing
operation named. WAL sidecar files (`-wal`, `-shm`) are database
internals, never separate authorities, and are never copied without the
main file.

The versioned backup is one authority-neutral directory bundle ending in
`.ntbackup`. It contains exactly regular, non-symlink files `manifest.json`
and `store.sqlite`; the directory and files are mode 0700 and 0600. The strict,
bounded manifest records backup format, creating binary and UTC time,
`familyId`, source kind (`sqlite` or `legacy-json`), source and current schema
versions, inventory counts for every variable-cardinality durable record class
shown below, SHA-256 of the
SQLite payload, and a versioned, formatting-independent semantic SHA-256
covering every authoritative value. It contains no task content, token,
credential, path, or
environment value. The manifest is at most 64 KiB and rejects duplicate or
unknown keys.

Authority-neutral means the same bundle contract represents a current SQLite
source or the exact legacy JSON source and always restores SQLite. It does not
mean repository-family or owner-boundary neutral: the payload retains those
identities so restore cannot silently transplant worktree ownership. The
format is portable across supported binaries and storage media, but v2.5
restore targets the same family and owner boundary. Rehoming or cloning an
authority is a separate, explicit protocol outside this specification.

```json
{
  "schemaVersion": 1,
  "createdBy": "noise-tasks 2.5.0",
  "createdAt": "<RFC3339>",
  "familyId": "<uuid>",
  "sourceKind": "sqlite",
  "sourceSchema": "sqlite:<integer>",
  "currentSchemaVersion": 5,
  "inventory": {
    "tags": 1,
    "milestones": 1,
    "tasks": 2,
    "dependencies": 1,
    "subtasks": 0,
    "bindings": 0,
    "assignments": 2,
    "claims": 0,
    "events": 4,
    "receipts": 1,
    "plans": 1,
    "planMappings": 1,
    "runs": 0,
    "attempts": 0,
    "worktreeSagas": 0
  },
  "payloadDigest": "sha256:<hex>",
  "semanticDigestVersion": 1,
  "semanticDigest": "sha256:<hex>",
  "comparisonDigestVersion": 1,
  "comparisonDigest": "sha256:<hex>"
}
```

Every shown field and inventory counter is required. `sourceSchema` is
`sqlite:<user_version>` or the literal `legacy-json:1`.
Counts are nonnegative integers; the singleton family/owner and fence records
have no redundant count. Digests are lowercase SHA-256.

Semantic digest version 1 is SHA-256 over the bytes
`noise-tasks-semantic-v1\0` followed by these record classes in this exact
order: `store`, `tags`, `milestones`, `tasks`, `dependencies`, `subtasks`,
`bindings`, `assignments`, `claims`, `fence`, `events`, `receipts`, `plans`,
`planMappings`, `runs`, `attempts`, `worktreeSagas`. For each class, emit its
ASCII name and one NUL byte; then, for each record, an unsigned 64-bit
big-endian byte length and canonical JSON; terminate the class with eight zero
bytes. Canonical JSON sorts object keys by raw UTF-8 bytes, preserves array
order, uses minimal JSON escapes, preserves string bytes without Unicode
normalization, renders integers in minimal base 10, and admits only strings,
integers, booleans, null, arrays, and objects.

Within classes, order is respectively: the singleton family/owner record; tag
bytes; `(tag, registry index)`; `(tag, numeric task id)`; `(tag, numeric task
id, dependency index)`; `(tag, numeric parent id, subtask index)`; `(tag,
numeric task id)` for legacy bindings; `(tag, numeric task id, assignment key
bytes)`; `(tag, numeric task id)`; the singleton
counter; numeric event sequence; `(kind bytes, receipt id bytes)`; plan id
bytes; `(plan id bytes, local key bytes)`; numeric run id; `(numeric run id,
attempt number)`; and numeric saga id. Every logical field and unknown-field
extension is present in its record. Physical SQLite layout, `user_version`,
WAL state, journal files, and timestamps created only by backup are excluded.

Comparison digest version 1 uses domain
`noise-tasks-legacy-compare-v1\0`, the same framing and canonical JSON, and
only the shared classes `tags`, `milestones`, `tasks`, `dependencies`,
`subtasks`, `bindings`, `claims`, `fence`, and `events` in that order. It
excludes family/owner metadata. For a
legacy bundle, `assignments`, `receipts`, `plans`, `planMappings`, `runs`,
`attempts`, and `worktreeSagas` counts are zero. Migration compares source and
bundle with this digest; it does not compare their full semantic digests.

Neither algorithm nor class order changes under version 1. A binary that does
not implement both manifest digest versions fails closed; it never substitutes
its newest algorithm. This makes verification stable across releases that
still support version 1.

`backup create` snapshots the current authority through SQLite's backup API or
`VACUUM INTO`, never by copying a live main file. With `--legacy`, it is the
only command besides §8.6 allowed to parse the exact derived `live.json`; it
fully validates and imports that source into a temporary exact-v3 SQLite
compatibility payload without publishing it. The legacy manifest therefore
records `currentSchemaVersion: 3`; restore or migration upgrades only its
private stage to v5. A legacy bundle's family UUID is
derived deterministically from the first 128 bits of SHA-256 over
`noise-tasks-legacy-family-v1\0`, the canonical Git common-directory path, one
NUL, and the canonical owner-boundary path, with RFC 4122 version-5 and variant
bits set. Repeated backups of that family remain compatible even when task
content changes. Both paths run `integrity_check`,
`foreign_key_check`, inventory comparison, and semantic digest comparison,
fsync both files and the temporary bundle directory, atomically rename the
directory to its unused output path, then fsync the parent. The temporary
bundle is created in the output path's parent, so an output filesystem may
differ from the authority's; no cross-filesystem rename is attempted. A parent
that cannot provide same-filesystem atomic directory rename and directory
fsync is refused rather than copied as a fallback. Creation never overwrites
an artifact.

SQLite backup creation also accepts an identity-bearing released v2, v3, or
v4 store without mutating or upgrading the source: the payload and
`currentSchemaVersion` retain that source version. Verification admits only
the exact trusted v2, v3, v4, and v5 catalogs and evaluates record classes
absent from an older schema as empty under semantic digest version 1. Restore
verifies that original payload first, then upgrades only its private staged
copy through the normal transactional path to v5 before embedding the restore
receipt and atomically publishing the current schema. It never upgrades the
bundle or source authority in place.

`backup verify` is context-free and read-only. It rejects extra entries,
symlinks, special files, duplicate or unknown manifest keys, unsupported
format/schema, size-limit violations, digest or inventory mismatch,
`integrity_check` or `foreign_key_check` findings, and payload/manifest family
mismatch. Corruption is exit 1 with empty stdout under `--json`; verification
recomputes payload, semantic, and comparison digests and never repairs or
upgrades the bundle.

`backup restore` always produces the current `tasks.sqlite`; it never produces
JSON. Restore is operator-only and offline: `--offline` acknowledges that all
unsupported writers are stopped, and the command must obtain the exclusive
maintenance lock. It refuses while either `migration-apply.json` or the exact
legacy `live.json` exists; restore is never an alternate cutover path. It
re-runs `backup verify`, constructs and verifies a fresh
temporary database through SQLite's backup interface, and embeds an immutable
restore receipt before publication. An existing target refuses unless
`--overwrite`; even then its `familyId` and `ownerBoundary` must equal the
bundle's and its WAL must checkpoint cleanly. `--overwrite` never authorizes a
wrong-family or wrong-boundary restore. An absent target explicitly adopts the
bundle's family id only when the currently derived owner boundary equals the
payload's; output names that recovery fact.

Publication closes SQLite handles, removes only checkpointed stale sidecars,
fsyncs the temporary database, atomically renames it to the derived target,
and fsyncs the directory. A failure before rename leaves the old authority (or
absence) intact; a failure after rename leaves the restored database with its
receipt. Re-running the same request and bundle returns that receipt. A
different bundle or request never guesses whether overwrite was intended.

### 8.6 One-way migration from `live.json`

Migration has only `plan` and `apply`; there is no quarantine, finalize, or
rollback-to-JSON phase. `migrate plan` requires a successful §8.5 legacy
bundle. It re-reads the exact derived source, proves source and bundle
comparison digests and shared inventories equal, and atomically writes a strict bounded plan
containing format version, family id, source/backup/target schemas and digests,
inventory, derived source and target identities, and a canonical plan digest.
It changes no authority.

`migrate apply` requires the plan, its accepted `--digest`, the same verified
bundle, `--offline`, and a request id. Under the exclusive maintenance lock it
refuses an unrelated existing database, wrong family, changed legacy source,
wrong backup, unsupported schema, or any projection offered as input. It
builds the current SQLite target from the bundle, verifies it, and writes the
permanent migration receipt into that database. The receipt includes all plan
and verification digests, inventories, versions, request id, and publication
time; it never expires and is never deleted by a finalize step.

To avoid a dual-authority window, apply then writes and fsyncs a bounded
`migration-apply.json` recovery journal containing plan, request, family,
source-comparison, backup-payload, and target-schema digests. The journal has
no phase or target-receipt field; recovery derives progress from source and
target presence plus the target's embedded receipt. Apply atomically removes
the exact legacy `live.json`, fsyncs the directory, publishes `tasks.sqlite` by
atomic rename, and fsyncs again. The journal is coordination metadata, not task authority.
After durable publication it is removed and the directory fsynced. Inert
projection files are neither read nor moved.

Apply recognizes only these crash states:

1. Source present, target absent, and no journal is pre-retirement. Source
   verification may start a new apply.
2. Source present, target absent, and a matching journal means retirement did
   not become durable. Reverify source and bundle, then resume retirement.
3. Source absent, target absent, and a matching journal means retirement is
   durable but publication is not. This is the
   deliberate no-authority recovery state; reconstruct the target from the
   named verified bundle. A target receipt is neither required nor possible.
4. Source absent and a target with the matching embedded migration receipt is
   published. Verify the target receipt, remove the journal, fsync the
   directory, and return the receipt.
5. Target present with that receipt and no journal is complete; the shared
   `(migration, requestId)` replay rule returns it.

Every other combination—including simultaneous source and target, a target
without its receipt, or mismatched plan/request/bundle fields—fails closed
without deleting or choosing data. A valid state 2 or 3 produces the exact
§2 recovery diagnostic outside matching `migrate apply`. The verified bundle
is the rollback medium, but restore always yields SQLite; this protocol never
resurrects JSON.

### 8.7 Cross-version fencing

Normal v2.5 commands never consult legacy JSON or projections. A legacy-only
family gets
`noise-tasks: legacy live store found; create a verified backup and run noise tasks migrate plan`
(exit 1). Only the compatibility commands named in §8.5–§8.6 bypass that
refusal. The removed projection commands and JSON `live init`, `live import`,
`live materialize`, and `live reconcile` remain nonexistent.

Migration requires all pre-v2.5 writers stopped. Removing `live.json` before
publication mechanically fences binaries for which it was the authority; the
exclusive maintenance lock fences cooperating v2.5 processes. A newer SQLite
`user_version` and unknown plan, bundle, receipt, or context schema fail
closed. SQLite created by earlier releases is upgraded transactionally; a
valid permanent receipt from this protocol is preserved. Historic
`sqlite-migration.json` files, including quarantine/finalize receipts, are
inert: they are not v2.5 authority, journals, or receipts and cannot select or
replace a database.

The v2.5 schema upgrade preserves tasks, revisions, counters, milestones,
claims, fencing values, and retained events. A pre-v2.5 worktree binding is
recorded as `cleanup-required` with no run and may only be inspected, recovered,
or removed; it is never assigned to a leaf or silently attached to a new run.
Context schema 4 and generation output schema 2 deliberately make pinned older
consumers fail closed. Consumers adopt those read schemas and the
plan/run/worktree-saga commands before a family accepts its first v2.5
mutation. A later release raises context to schema 5 (§2.2) to expose the
store owner boundary and the store/engine schema pair; the same fence
applies — consumers accept schema 5 before any family in their care runs
an engine release that reports it. Once upgraded, the
new `user_version` prevents an older writer from opening the family. Removed
compound flags and `worktree create` are usage errors with guidance to create
a claim, run, and provisioning saga; there is no semantics-changing alias.
A matching recovery journal takes precedence over the legacy-found refusal and
every missing-state/onboarding diagnostic; only matching `migrate apply` may
advance it.

### 8.8 Quality bar additions

`go test ./...` additionally covers with real processes and linked worktrees:
visibility after commit; CAS and claim-next races with exactly one winner;
claim/run token fencing, expiry, replay, force release, and restart; corrupt,
truncated, future-schema, symlinked, wrong-family, wrong-owner-boundary, and
permission-broken stores and bundles; a consistent backup during writes;
restore overwrite and every fsync/crash boundary, cross-filesystem output,
live/journal restore refusal,
and versioned semantic/comparison digests with fixed record order; migration
source/backup mismatch and every allowed and invalid journal
boundary (legacy only, recoverable absence, or SQLite only, never both);
semantic preservation of unknown fields, counters, milestones, assignments,
claims, events, and receipts; feed compaction; and exact diagnostics. The suite
includes outside-Git-to-Git rehoming refusal, event vocabulary, every receipt
kind, and the shared `(kind, requestId)` collision/replay rule. The suite
also runs `-race`, multi-process, linked-worktree, and the §11 gates. A
10,000-task database must run `list`, `next`, `claim-next`, and one CAS
mutation within ordinary test timeouts; v2.5 specifies no latency SLA.

### 8.9 Lifecycle hygiene report

`noise-tasks hygiene` is a read-only, authority-scoped report of
lifecycle residue, so an orchestrator can apply force-cancel, release,
or requeue policy to observed state instead of accumulating it. It
accepts only `--json` or no flags; it never accepts `--file` or `--tag`.
It takes no lock beyond the shared maintenance lock of a normal read,
appends no event, and never mutates. Residue is not an error: the
command exits 0 with the report whether or not residue exists;
operational failures follow §6 and exit 1. All expiry comparisons use
the database process's UTC clock, judged inside one deferred
transaction, exactly like §8.3.

`--json` prints one object with every field present; the three arrays
are always present and empty when a class has no residue:

```json
{
  "schemaVersion": 1,
  "now": "<RFC3339 UTC as judged>",
  "staleRuns": [
    {"runId": 1, "tag": "main", "task": "590", "owner": "agent",
     "attempt": 1, "ttlSeconds": 3600, "attemptExpiresAt": "<RFC3339>"}
  ],
  "expiredClaims": [
    {"tag": "main", "task": "438", "owner": "agent", "expiredAt": "<RFC3339>"}
  ],
  "claimlessInProgress": [
    {"tag": "main", "task": "267", "updatedAt": "<RFC3339>"}
  ]
}
```

- `staleRuns` lists every run with status `running` whose current
  attempt is still `running` and whose attempt lease has expired. A
  `retryable` run is never listed: its attempt already terminalized and
  the orchestrator owns an explicit retry-or-cancel decision, not a
  lease question.
- `expiredClaims` lists every task row still carrying a claim whose
  `claim_expires_at` has passed.
- `claimlessInProgress` lists every `in-progress` task with no live
  claim (no claim row at all, or an expired one). One task may appear
  in both `expiredClaims` and `claimlessInProgress`; the classes answer
  different questions.
- The report never contains fencing, claim, or attempt tokens, and
  carries no titles, descriptions, or details.

Human output prints one line per finding grouped by class, a summary
line, or exactly `no lifecycle residue found` when every class is
empty.

## 9. Trusted orchestration, runs, and workspaces

### 9.1 Roles and trust boundary

The v2.5 thesis is: **the orchestrator decides; noise-tasks commits, fences,
records, and proves**. The roles are explicit:

- The **Operator** owns policy and irreversible authority. The operator
  selects the trusted orchestrator, accepts plan and migration digests,
  authorizes force/overwrite/offline actions, and controls credentials.
- The **Trusted Orchestrator** may read family-wide state, hold claim and run
  tokens, call providers, choose ordering and models, apply plans, start and
  finish runs, provision worktrees, and evaluate leaf results. It is a trusted
  principal, not an untrusted plugin inside noise-tasks.
- A **Leaf Agent** receives one bounded assignment and worktree plus the
  minimum source context needed to propose a result. It does not receive the
  database or Git common-directory path, provider credentials, claim/run
  tokens, maintenance or broker capabilities, other task details, or a global
  task command surface. Its output is a proposal until the orchestrator
  validates and commits it.

`noise-tasks` does not choose a model, infer delegation policy, spawn or
supervise a leaf process, judge semantic quality, merge a branch, or convert
agent output into authority without an explicit command. It may render the
bounded controller projection and leaf brief below, but the Trusted
Orchestrator owns provider execution and the host sandbox establishes principal
separation.
Names such as `owner` and `assignment` are audit labels and fences, not
authentication.

Owner labels are trimmed, nonempty UTF-8 of at most 256 bytes with no ASCII
control character and compare by exact bytes. Operation request ids use the
local-key grammar in §9.2. Neither value is shell-interpreted.

### 9.2 Versioned plans

A plan is strict UTF-8 JSON, at most 8 MiB, with no duplicate or unknown keys:

```json
{
  "schemaVersion": 1,
  "planId": "release-25",
  "tag": "main",
  "milestones": [
    { "key": "ms", "name": "v2.5", "status": "open" }
  ],
  "tasks": [
    {
      "key": "spec",
      "title": "Settle the protocol",
      "description": "",
      "details": "",
      "priority": "high",
      "milestone": { "key": "ms" },
      "dependencies": [{ "id": "41" }]
    }
  ],
  "assignments": [
    { "key": "primary", "task": { "key": "spec" }, "owner": "planner-a" }
  ]
}
```

All six top-level members are required; the three arrays may be empty. Every
shown nested field is required except task `milestone`, which may be absent.
The §1 grammar and bounds apply to task and milestone values.

Local keys and plan ids match `[A-Za-z0-9][A-Za-z0-9._-]{0,127}` and are
unique in their namespace. A milestone reference is exactly `{"key": k}`
for a milestone created by the plan or `{"name": n}` for an existing one. A
task reference is exactly `{"key": k}` for this plan or `{"id": id}` for an
existing task in the tag. Tasks are new, initially `pending`, and carry no id,
revision, claim, run, subtask, status, or timestamp. Each task has at most one
assignment, whose task reference must be a local key; an assignment is durable
scheduling intent, not a claim or access grant. Dependency references form one
DAG across existing and local tasks.

Canonical plan JSON sorts object keys by UTF-8 byte order, preserves array
order, uses minimal JSON string escaping, and permits only base-10 integers
without exponent or fraction. The plan digest is SHA-256 over
`noise-tasks-plan-v1\0` plus those bytes. `plan validate --offline` checks
shape, bounds, local references, and the local DAG without opening a store.
Online validation additionally resolves existing ids/milestones, checks the
combined DAG and duplicate names, and reports the digest and inventory; it
does not reserve ids or state.

`plan apply` requires the accepted digest and request id, then repeats all
online validation under one immediate transaction. It appends new milestones
in plan order, allocates task ids in task order, resolves dependencies and
milestones, records assignments and local-key mappings, appends one content-
free summary event, and commits one receipt containing the plan digest,
inventory, and every key-to-id mapping. Nothing is published on any error.
Replay of the same request id, plan id, and digest returns the original
receipt; reuse of either id with different normalized input is exit 3. Thus a
plan is atomic and idempotent, but never self-authorizing.

Validation output is one object with `schemaVersion`, `planId`, `tag`,
`planDigest`, `offline`, and `inventory`. `inventory` is the proposed batch,
not the current store: it contains `milestones`, `tasks`, `assignments`, and
`dependencies`, where dependencies counts every dependency edge declared by
the plan. Human output carries the same facts. Online validation requires the
named tag to exist; applying a plan never creates a tag implicitly.

The durable plan row stores the canonical plan JSON, accepted digest, request
id, receipt id, tag, and commit time. A plan id is store-wide unique. Local
task mappings are stored and returned in task-array order as
`taskMappings: [{"key": k, "id": id}]`; milestone keys are resolved to their
registry names but are not assigned synthetic ids. The plan receipt adds
`planId`, `planDigest`, and `taskMappings` to the common receipt fields. Its
`inputDigest` is SHA-256 over canonical JSON containing the request id, plan
id, and plan digest. Its `resultDigest` is SHA-256 over
`noise-tasks-plan-result-v1\0` plus canonical JSON containing the plan id,
plan digest, proposed-batch inventory, and task mappings. This batch result
digest deliberately excludes the receipt and feed sequence, avoiding a
self-referential store semantic digest while proving the complete published
mapping. The summary event is `kind=plan`, `action=applied`, carries the task
count, receipt id and receipt digest, and no plan content.

An apply retry first checks the durable `(plan, requestId)` receipt while
holding the immediate transaction. An identical request id, plan id, and plan
digest returns that original receipt and mapping even though revalidating the
now-published batch would find duplicates. Reusing the request id for another
plan or digest, or reusing the plan id under another request or digest, is a
typed replay conflict (exit 3). The original canonical plan bytes remain the
durable collision proof; digest equality never authorizes replacing them.

`generate` emits this schema and digest only. Milestones created by a plan are
the same §12 registry entries as manually created milestones; assignments do
not alter `next` and affect `claim-next` only when its explicit filter is used.

### 9.3 Runs and attempts

A run is the durable orchestration lifecycle for one top-level task. Run status
is `running | retryable | succeeded | cancelled`; attempt status is `running |
succeeded | failed | lost | cancelled`. `running` and `retryable` are the two
nonterminal run states. A task may have at most one nonterminal run, enforced
by a partial unique index; expiry never permits a second run. A positive
numeric `runId` is allocated by the database, is never reused, is not a fence,
and orders store-wide run history. Hierarchical task ids are a usage error.

Schema v4 stores these logical records. A run contains exactly `runId`, `tag`,
`task`, `owner`, `claimAcquisitionId`, the private captured `claimToken`,
`status`, `currentAttempt`, `startRequestId`, `terminalResultDigest`, nullable
`forceCancelFence`, `startedAt`, `updatedAt`, nullable `terminalAt`, and
byte-faithful `extra`. An attempt contains exactly `runId`, `attempt`, random
`acquisitionId`, private `attemptToken`, `status`, `ttlSeconds`,
`leaseGeneration`, `createdRequestId`, `startedAt`, `expiresAt`, `updatedAt`,
nullable `terminalAt`, `terminalResultDigest`, and byte-faithful `extra`.
Attempt numbers begin at 1 and increase without gaps within a run. The current
attempt is the greatest recorded number. An attempt acquisition id is a fresh
128-bit cryptographically random value rendered as 32 lowercase hexadecimal
digits. It is non-secret, never reused, and remains unchanged across renewal.
`leaseGeneration` begins at 1 and increments by one on each renewal.

Their semantic records always include every named field. Nullable fields render
as JSON null, never omission; absent `terminalResultDigest`, `terminalAt`, and
`forceCancelFence` are null. The physical `extra` object is decoded and
flattened into the record exactly like existing unknown-field classes; there is
no literal `extra` member, and a collision with a named field is corruption.
For example, these are canonical running records with one unknown field:

```json
{"claimAcquisitionId":"00112233445566778899aabbccddeeff","claimToken":41,"currentAttempt":1,"forceCancelFence":null,"owner":"agent-a","runId":1,"startRequestId":"req.start","startedAt":"2026-08-28T17:45:00Z","status":"running","tag":"main","task":"7","terminalAt":null,"terminalResultDigest":null,"updatedAt":"2026-08-28T17:45:00Z","x-note":true}
{"acquisitionId":"11223344556677889900aabbccddeeff","attempt":1,"attemptToken":42,"createdRequestId":"req.start","expiresAt":"2026-08-28T18:00:00Z","leaseGeneration":1,"runId":1,"startedAt":"2026-08-28T17:45:00Z","status":"running","terminalAt":null,"terminalResultDigest":null,"ttlSeconds":900,"updatedAt":"2026-08-28T17:45:00Z","x-note":true}
```

Foreign keys preserve run history. The run→task and attempt→run foreign keys
are restrictive and never cascade. The non-null composite
`(runId,currentAttempt)` run pointer references `(runId,attempt)` and is
`DEFERRABLE INITIALLY DEFERRED`, allowing both start rows to be created in one
transaction without weakening the committed catalog. Removing a top-level
task that has any run or any retained §9.4 worktree saga is a typed exit-3
conflict even under `remove --force`; cancellation, worktree removal, and
terminal saga status do not authorize deletion. The history precheck occurs
after revision, structural, dependency, and proposed-value validation and
before claim fencing or effects, so neither restrictive FK can leak a raw
SQLite error. Runs and sagas store the task identity for display and audit but
cannot outlive their task row.

The allowed current status pairs are exactly `running/running`,
`retryable/failed`, `retryable/lost`, `succeeded/succeeded`,
`cancelled/cancelled`, `cancelled/failed`, and `cancelled/lost`, written as
run/attempt. A non-current attempt is exactly `failed` or `lost`; succeeded and
cancelled attempts cannot gain a successor. A run has null `terminalAt` exactly
when it is running or retryable. An attempt has null `terminalAt` exactly when
it is running. `verify` checks those rules, the current/greatest-attempt
invariant, timestamp, digest and acquisition-id grammar, nonterminal
uniqueness, receipt linkage, foreign keys, and that the shared fence counter is
not below any captured claim token, attempt token, saga fence, or
forced-cancellation fence. A malformed, negative, missing, or overflowing
counter fails closed instead of being coerced.

The `runs` and `attempts` semantic-digest classes use the exact logical records
above, including private fencing values, ordered by numeric `runId` and then
`(runId, attempt)`. Semantic digest version 1 and its class order do not change;
v2 and v3 encode both classes as empty. The comparison digest remains version
1 and excludes both. Capability tokens are fencing values rather than an
authentication boundary; they remain absent from user surfaces, while the
trusted backup's semantic and payload digests cover them. Runs and attempts
remain schema-v4 records and are present unchanged in v5. New stores and
published legacy imports are v5; verified legacy compatibility bundles remain
v3 as §8.5 requires. Backup verification admits exact v2 through v5 catalogs,
and restore upgrades a verified private v2, v3, or v4 stage to v5 before
publication. A recovery journal naming a pre-v4 migration target is not
reinterpreted. V5 strictly parses journal schema 1 with target schema 3 only to
classify the recovery block in every valid journal crash state, including an
already-published v3 target. Context and normal commands report recovery
required; v5 `migrate apply` refuses with guidance to use the target-3 binary.
V5 never upgrades, rewrites, removes, or completes that journal or its target.
After the owning binary finishes recovery and removes the journal, ordinary
v3→v4→v5 upgrade may run.

During v1/v2/v3→v4 upgrade, every recorded claim lacking an `acquisitionId`
receives an id with the grammar above in the same upgrade transaction; its
owner, token, TTL, acquired/expiry timestamps, and other unknown fields remain
unchanged. A normal SQLite authority or SQLite-source restore stage uses a
fresh cryptographically random id. A legacy-source v3 bundle instead derives
the id as the first 128 bits of SHA-256 over
`noise-tasks-legacy-claim-acquisition-v1\0`, family id, NUL, tag, NUL, task id,
NUL, the decimal claim token, NUL, and acquiredAt. This deterministic legacy
exception lets migration crash recovery reconstruct the exact private v4
target. A present but malformed acquisition id is corruption and refuses the
complete upgrade.

Legacy source/bundle comparison is proved against the unaugmented exact-v3
payload. The private v4 intermediate adds only the deterministic claim
identities, and v5 then performs the deterministic legacy-binding conversion
in §9.4; the migration receipt binds the verified source and bundle digests
plus target schema version. Recovery reconstructs that deterministic target
and rechecks the exact v5 catalog, run/saga invariants, and embedded receipt
before publication.
Its target semantic digest is not required to equal the source comparison
digest: the schema-upgrade event and migration event/receipt are deliberately
target-local. This gives every published v5 run a valid claim lineage without
adding another journal format, forcing release/reacquisition, or altering a
source bundle in place.

All run TTLs use one database-process UTC clock reading. Omitted `--ttl` is 900
seconds; an explicit TTL is a base-10 integer from 1 through 86400 inclusive.
The normalized TTL is hashed. A renewal may shorten a lease. Effective attempt
liveness requires an unexpired matching claim, a `running` run, its current
`running` attempt, and an unexpired attempt lease; equality with the clock is
expired. Read-only commands report stored state and never persist expiry.

The mutation commands are:

```text
run start <task> --owner <o> --claim-token <c> --request-id <id> [--ttl s] [--tag <tag>]
run retry <runId> --owner <o> --claim-token <c> --attempt <n> --attempt-token <f> --request-id <id> [--ttl s]
run renew <runId> --owner <o> --claim-token <c> --attempt <n> --attempt-token <f> --request-id <id> [--ttl s]
run finish <runId> --owner <o> --claim-token <c> --attempt <n> --attempt-token <f> --outcome <succeeded|failed> --request-id <id> [--task-status <status> --revision <r>] [--result-digest sha256:<hex>]
run cancel <runId> --owner <o> --claim-token <c> --attempt <n> --attempt-token <f> --request-id <id>
run cancel <runId> --force --request-id <id>
run show <runId> [--json]
run list [--task <id>] [--status <status>] [--owner <o>] [--json]
```

`start` is the only run verb that accepts `--tag`, because it resolves a task
id before a globally unique run id exists. It uses the normal tag default from
§4 when omitted. `start` requires the task's effective claim and no nonterminal run. In one
immediate transaction it allocates a run id, allocates one shared-counter fence
for attempt 1, creates both rows, appends `kind=run action=started`, and commits
one run receipt. The operation's clock value is both rows' `startedAt` and
`updatedAt`; terminal timestamps and terminal result digests are null. It
returns the attempt capability only after commit.

`retry` requires the named run, claim, and current attempt. It accepts an
expired `running` attempt, the latest `lost` attempt with no successor, or a
`failed` attempt in a `retryable` run. In the first case it marks the predecessor
`lost`; then it allocates one shared-counter fence, creates attempt `n+1` with a
new acquisition id and generation 1, changes the run to `running`, appends
`kind=attempt action=retried`, and commits one receipt. These effects are one
transaction, so a failed retry cannot leave a newly lost attempt. A live,
non-current, succeeded, or cancelled attempt cannot be retried.
When retry observes expiry, it writes its clock value to the predecessor's
`updatedAt` and `terminalAt`; an already failed or lost predecessor remains
byte-identical. The successor uses that value for `startedAt` and `updatedAt`,
has null terminal fields, and never inherits a result digest. The run becomes
`running`, clears no historical attempt result, keeps its own result digest
null, and writes the same value to `updatedAt` while `terminalAt` remains null.

`renew` is the only operation that replaces a claim lease while its run is
nonterminal. It requires the current effectively live attempt, allocates two
successive shared-counter fences in the fixed order new claim token then new
attempt token, and computes both expiries from one clock reading and normalized
TTL. In one transaction it preserves the claim's owner, acquired time, and
acquisition id; rotates its token; replaces its durable TTL and expiry; rotates
the attempt token; replaces its `ttlSeconds` and expiry; increments
`leaseGeneration`; updates the run's captured claim token;
sets both the attempt and run `updatedAt` to the operation's clock value;
appends `kind=attempt action=renewed`; and commits one receipt. Bare claim renew
and ordinary release refuse while any nonterminal run exists. Force release
allocates one forced-cancellation fence and, before clearing the claim,
atomically cancels its nonterminal run and a current `running` attempt. A
current `failed` or `lost` attempt retains that evidence while the run becomes
`cancelled`; the run stores the force fence. The release event summarizes the
combined effect and no separate run receipt is created.

`finish` requires the current effectively live attempt. `succeeded` makes the
attempt and run `succeeded`; `failed` makes the attempt `failed` and the run
`retryable`. The optional `--task-status` and `--revision` must appear together.
That optional task sub-effect alone follows ordinary task CAS and increments
the task revision; run/attempt mutations do not. A failed outcome with task
status `done` is a usage error. Successful publication of `done` clears the
claim in the same transaction. The caller may store only a lowercase
`sha256:<64-hex>` terminal result digest, never result bytes. The transaction
appends only `kind=run action=finished`; that event summarizes any task-status
sub-effect, and one receipt covers the whole commit.

Finish writes the supplied result digest, or null when omitted, to the attempt.
On success it writes the same value to the run; on failure the retryable run's
result digest remains null. A later retry preserves the failed predecessor's
digest, and a later successful finish sets the run digest from that successful
attempt. Finish writes its clock value to the attempt's `updatedAt` and
`terminalAt` and the run's `updatedAt`; successful run `terminalAt` receives
that value, while a retryable run keeps null `terminalAt`. An optional task
update uses the same clock value under the ordinary task timestamp rules.

Ordinary `cancel` requires the live matching claim and current attempt token.
A `running` attempt must also be unexpired; a current `failed` or `lost`
attempt in a `retryable` run may be cancelled without pretending its lease is
live. The run becomes `cancelled`; a running attempt becomes `cancelled`, while
failed or lost evidence is preserved. Forced cancel is operator-only, accepts
no owner or capabilities, allocates one shared-counter fence, stores it on the
run, and applies the same status rule. A terminal run refuses a fresh cancel.
Both forms append `kind=run action=cancelled` and one run receipt.
Cancelling a running attempt leaves both result digests null and writes the
operation's clock value to that attempt's `updatedAt` and `terminalAt`.
Cancelling a failed or lost attempt does not rewrite any attempt field. In all
cases the run result digest remains null and the run receives that clock value
in `updatedAt` and `terminalAt`. Force release uses the same timestamp and
result rules for the run/attempt rows it fences.

Every task field or status mutation during an effectively live attempt must
supply `--run`, `--attempt`, and `--attempt-token` in addition to the claim
gate. Its conditional SQL statement matches all gates and the revision where
applicable. Structural, dependency, proposed-value, and supplied-revision
validation happens before fencing; no independently invalid request can use a
fence diagnostic as an oracle. A superseded attempt can neither publish state
nor renew itself.

All run mutations validate flag grammar, local combinations, owner/request-id
grammar, ids, TTL, status, and digest before opening the store. Every new
effect then uses one `BEGIN IMMEDIATE`, one clock reading, and conditional SQL.
Exit 0 is success or a valid replay. Exit 1 is an operational, missing-record,
unsupported-schema, or corruption failure. Exit 2 is local usage. Exit 3 is a
request collision, stale capability or revision, expired lease, existing
nonterminal run, invalid transition, superseded attempt, or stale capability
replay. Every failure has empty stdout. A conflict diagnostic names only the
operation and caller-supplied non-secret tag/task/run/attempt identifiers; it
never reports a holder, token, acquisition id, expiry, or current status.

#### Run receipts and replay

All six request-id effects — start, retry, renew, finish, ordinary cancel, and
force cancel — share the store-wide `(kind="run", requestId)` namespace. After
local validation, replay lookup occurs inside the immediate transaction before
current-state effect validation. A changed normalized input is exit 3. An
identical replay performs no write, allocation, lease extension, event, or
receipt creation. Before returning anything it verifies the strict canonical
receipt, its envelope and digests, family/request identity, exact event and
inventory linkage, and the referenced durable rows. Alteration is exit 1 and
never falls back to a new effect.

Input digest is lowercase `sha256:<hex>` with no domain prefix over §8.5
canonical JSON containing exactly these always-present keys:

```text
attempt, force, operation, outcome, owner, requestId, revision, runId, tag,
task, taskStatus, terminalResultDigest, ttlSeconds
```

`attempt`, `revision`, `runId`, and `ttlSeconds` use zero when absent; other
absent values are `""`; `force` is false except forced cancel. `attempt` is the
source/current attempt supplied by retry, renew, finish, and ordinary cancel.
Start uses `runId=0`, `attempt=0`, and its resolved tag/task. Non-start effects
use empty tag/task because run identity is authoritative. Force cancel uses
empty owner and attempt zero. Input omits claim and attempt tokens and every
other capability or fence.

Result digest is likewise unprefixed SHA-256 over canonical JSON containing
exactly these always-present keys:

```text
attempt, attemptAcquisitionId, attemptExpiresAt, attemptStatus,
claimAcquisitionId, claimExpiresAt, committedAt, leaseGeneration, operation,
owner, runId, runStatus, tag, task, taskRevision, taskStatus,
terminalResultDigest
```

Absent strings and numbers are `""` and zero. It never contains a token or
force fence. For force cancel, owner and acquisition identities are empty.
`taskStatus` and `taskRevision` are the task state observed after the effect,
whether or not finish requested a task update; the receipt's separate
`taskStatus` and `revision` fields remain the normalized caller inputs.

The values are deterministic for every operation:

- `operation` is normalized as above; `runId`, `tag`, and `task` identify the
  resolved run; `owner` is the supplied owner, or empty for force cancel.
- `attempt`, `attemptStatus`, `attemptAcquisitionId`, `attemptExpiresAt`, and
  `leaseGeneration` describe the resulting current attempt after start, retry,
  renew, finish, or ordinary cancel. Terminal attempts retain their stored
  expiry and generation. Force cancel reports the resulting current attempt
  number and status but uses empty acquisition/expiry and generation zero, so
  it does not disclose a displaced capability lineage.
- `claimAcquisitionId` and `claimExpiresAt` are the resulting live claim for
  start, retry, and renew. Finish and ordinary cancel record the matching claim
  immediately before any successful-`done` clearing; force cancel leaves both
  empty. Thus a terminal receipt remains independently linked even after the
  claim is cleared.
- `runStatus` and `attemptStatus` are post-effect. `taskStatus` and
  `taskRevision` are the post-effect task state. `terminalResultDigest` is the
  caller's normalized finish digest and is empty for every other operation;
  cancel therefore does not expose a preserved failed/lost attempt digest.

Every run receipt contains exactly `schemaVersion`, `kind`, `receiptId`,
`familyId`, `requestId`, `inputDigest`, `resultDigest`, `operation`, `tag`,
`task`, `runId`, `owner`, `sourceAttempt`, `attempt`, `ttlSeconds`, `outcome`,
`revision`, `taskStatus`, `taskRevision`, `terminalResultDigest`, `force`,
`claimAcquisitionId`, `attemptAcquisitionId`, `leaseGeneration`,
`claimExpiresAt`, `attemptExpiresAt`, `runStatus`, `attemptStatus`, `inventory`,
`resultTaskStatus`, `eventSeq`, `committedAt`, and `receiptDigest`; no key may
be omitted or added. `resultTaskStatus` is the result digest's post-effect
`taskStatus`; the unprefixed `taskStatus` is the caller's normalized optional
finish input. `taskRevision` is already result-only.
`sourceAttempt` is zero for start and force cancel and otherwise the caller's
attempt; `attempt` is the resulting/current attempt. Inventory is the complete
post-commit §8.5 inventory, including the pending event and receipt after feed
compaction. `eventSeq` is the common inclusive sequence-range object
`{"first": n, "last": n}`; run mutations emit exactly one event, so its two
integers are equal. The event contains tag/task/run, affected attempt, owner when
nonempty, receipt id/digest, and timestamp, but no acquisition id, lease,
result, status, token, or force fence. If the range is still inside the retained
feed, receipt verification requires the exact event. If its last sequence is below
`oldestRetainedSeq`, verified compaction is allowed; a missing retained event
is corruption. This avoids a digest cycle: predict event sequence and
post-effect inventory, compute the receipt and its digest, then append the
event carrying that digest and insert the receipt before commit.

The receipt id is `run-` plus the full lowercase hexadecimal SHA-256 of the
bytes `noise-tasks-run-receipt-v1\0`, the request id, one NUL byte, and the
ASCII input digest. `receiptDigest` uses §8.4's common domain over the canonical
receipt without that field.

Start, retry, and renew replays may return capabilities only when the receipt's
recorded run, resulting attempt acquisition id, and lease generation are still
the exact current `running` attempt; the matching claim acquisition is still
live; and both leases are unexpired. Otherwise replay is exit 3 without a
token. Finish and cancel replay return only their recorded public terminal
response and remain valid after liveness ends. Owner labels and request ids are
not authentication: capability recovery is available only to the Trusted
Orchestrator principal established by the host boundary in §9.1. Leaf agents
must not receive the database path, command surface, owner/request pair, or
receipt access. This receipt-based recovery is deliberate: a renew rotates the
old tokens, so requiring them again would make commit-before-output
unrecoverable. A later renewal or retry supersedes the recorded generation and
makes the older capability replay stale.

For any request-id operation, a crash before commit leaves no effect or
receipt. Commit-before-output is recovered by the rules above. Concurrent
identical calls serialize to one effect and one replay.

Fixed digest vectors follow. The first eight JSON/digest pairs are exact UTF-8
with no trailing newline and use unprefixed SHA-256 as specified above. The
final four lines are respectively the receipt-id byte input (with NUL shown as
`\0`), its receipt id, the canonical start receipt without `receiptDigest`, and
its §8.4 domain-prefixed receipt digest. These vectors are normative and use
§8.5 canonical key order.

```text
{"attempt":0,"force":false,"operation":"start","outcome":"","owner":"agent-a","requestId":"req.start","revision":0,"runId":0,"tag":"main","task":"7","taskStatus":"","terminalResultDigest":"","ttlSeconds":900}
sha256:fe37acb4ec67410fdf246deecda135cd1484d90b46399c6c4f441d098e5e3ed5
{"attempt":1,"force":false,"operation":"retry","outcome":"","owner":"agent-a","requestId":"req.retry","revision":0,"runId":1,"tag":"","task":"","taskStatus":"","terminalResultDigest":"","ttlSeconds":900}
sha256:7f7762b22dbecee5f1578355a6a0073837c1b047d2584e9a60b92801a3afdeaa
{"attempt":2,"force":false,"operation":"renew","outcome":"","owner":"agent-a","requestId":"req.renew","revision":0,"runId":1,"tag":"","task":"","taskStatus":"","terminalResultDigest":"","ttlSeconds":300}
sha256:8077bf4524f111a24b25623c71197e3c69c77114c9644a04aab1cb03dd6d54ec
{"attempt":2,"force":false,"operation":"finish","outcome":"succeeded","owner":"agent-a","requestId":"req.finish","revision":3,"runId":1,"tag":"","task":"","taskStatus":"done","terminalResultDigest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","ttlSeconds":0}
sha256:21d37a732659cdc08323b6e541a9d647ea230e0068390492488193cecc041a2b
{"attempt":2,"force":false,"operation":"finish","outcome":"failed","owner":"agent-a","requestId":"req.fail","revision":0,"runId":1,"tag":"","task":"","taskStatus":"","terminalResultDigest":"","ttlSeconds":0}
sha256:fddfb13fd392b56b65b3e9de32cdaf986a5cb4a1e074bf6c82a5df85b4246d49
{"attempt":2,"force":false,"operation":"cancel","outcome":"","owner":"agent-a","requestId":"req.cancel","revision":0,"runId":1,"tag":"","task":"","taskStatus":"","terminalResultDigest":"","ttlSeconds":0}
sha256:8e69b9f9c7c16b7ae72aee6f2457f9699f84bd5cf41deada5e73872d4fd92550
{"attempt":0,"force":true,"operation":"cancel","outcome":"","owner":"","requestId":"req.cancel.force","revision":0,"runId":1,"tag":"","task":"","taskStatus":"","terminalResultDigest":"","ttlSeconds":0}
sha256:367b0a09ccaa54544f7084c01bb704d282b8d6652a75eb654914439fe08198aa
{"attempt":1,"attemptAcquisitionId":"11223344556677889900aabbccddeeff","attemptExpiresAt":"2026-08-28T18:00:00Z","attemptStatus":"running","claimAcquisitionId":"00112233445566778899aabbccddeeff","claimExpiresAt":"2026-08-28T18:00:00Z","committedAt":"2026-08-28T17:45:00Z","leaseGeneration":1,"operation":"start","owner":"agent-a","runId":1,"runStatus":"running","tag":"main","task":"7","taskRevision":3,"taskStatus":"pending","terminalResultDigest":""}
sha256:bc41786d858cd06eb463a4d25a4f7a5f59c241790f59f524cca83ddd50374c98
noise-tasks-run-receipt-v1\0req.start\0sha256:fe37acb4ec67410fdf246deecda135cd1484d90b46399c6c4f441d098e5e3ed5
run-9a5a925849e5b65b1f0507cb457237bdacc62e44b85422f6b448cac819110dcc
{"attempt":1,"attemptAcquisitionId":"11223344556677889900aabbccddeeff","attemptExpiresAt":"2026-08-28T18:00:00Z","attemptStatus":"running","claimAcquisitionId":"00112233445566778899aabbccddeeff","claimExpiresAt":"2026-08-28T18:00:00Z","committedAt":"2026-08-28T17:45:00Z","eventSeq":{"first":11,"last":11},"familyId":"123e4567-e89b-42d3-a456-426614174000","force":false,"inputDigest":"sha256:fe37acb4ec67410fdf246deecda135cd1484d90b46399c6c4f441d098e5e3ed5","inventory":{"assignments":0,"attempts":1,"bindings":0,"claims":1,"dependencies":0,"events":1,"milestones":0,"planMappings":0,"plans":0,"receipts":1,"runs":1,"subtasks":0,"tags":1,"tasks":1,"worktreeSagas":0},"kind":"run","leaseGeneration":1,"operation":"start","outcome":"","owner":"agent-a","receiptId":"run-9a5a925849e5b65b1f0507cb457237bdacc62e44b85422f6b448cac819110dcc","requestId":"req.start","resultDigest":"sha256:bc41786d858cd06eb463a4d25a4f7a5f59c241790f59f524cca83ddd50374c98","resultTaskStatus":"pending","revision":0,"runId":1,"runStatus":"running","schemaVersion":1,"sourceAttempt":0,"tag":"main","task":"7","taskRevision":3,"taskStatus":"","terminalResultDigest":"","ttlSeconds":900}
sha256:26595634d1c83b5dce5a1af1ea51cce81543277229756ce752696a360b2aa676
```

#### Run output and reads

Start JSON is
`{"runId":1,"attempt":1,"leaseGeneration":1,"attemptToken":42,"expiresAt":"2026-08-28T18:00:00Z"}`;
retry uses the same shape with the successor attempt number. Human output is
the same fields in that order as `key=value` pairs. Renew JSON
is `{"runId":1,"attempt":1,"leaseGeneration":2,"claimToken":43,"claimExpiresAt":"2026-08-28T18:15:00Z","attemptToken":44,"attemptExpiresAt":"2026-08-28T18:15:00Z"}`;
human output uses that field order. These are the only token-bearing surfaces.

Finish returns `runId`, `runStatus`, `attempt`, `attemptStatus`, `task`, and
`terminalResultDigest` in that order. `task` is either null or an object with
`id`, `status`, and `revision`. Cancel returns `runId`, `runStatus`, `attempt`,
`attemptStatus`, and `forced` in that order. Human output is the corresponding
ordered `key=value` summary and uses `task=-` for null.

`run show` returns exactly `runId`, `tag`, `task`, `owner`, `status`,
`currentAttempt`, `startedAt`, `updatedAt`, `terminalAt`,
`terminalResultDigest`, `attempts`, and `receipts`. Each ordered attempt contains
exactly `attempt`, `acquisitionId`, `status`, `ttlSeconds`, `leaseGeneration`,
`startedAt`, `expiresAt`, `updatedAt`, `terminalAt`, and
`terminalResultDigest`. Each receipt reference contains exactly `operation`,
`requestId`, `receiptId`, `receiptDigest`, and `committedAt`; receipt bodies are
never returned. `run list` returns summaries containing exactly `runId`, `tag`,
`task`, `owner`, `status`, `currentAttempt`, and `updatedAt`, ordered by
ascending numeric run id. Filters combine by AND. The list omits acquisition
ids, expiries, results, and receipts. Empty list is exit 0. Both reads omit
every token, force fence, and captured claim acquisition id.
The Store API mirrors the commands as `StartRun`, `RetryRun`, `RenewRun`,
`FinishRun`, `CancelRun`, `GetRun`, and `ListRuns`; each mutation returns its
public result, whether it was replayed, and an error.

### 9.3A Confined leaf handoff

A machine-oriented orchestration surface may render a controller projection
from one §9.4 saga mutation result and, only when that saga is a safe bound
provision, a leaf brief.
Each projection is a read, creates no receipt or authority, and never launches
a provider or process. A leaf brief succeeds only while the task retains the
revision recorded by its claim acquisition, the claim and current attempt are
both live, and the run/attempt pair is exactly `running/running`. The selected
saga must name that run, tag, and top-level task and have
`operation=provision`, `status=bound`, and `binding_active=1`. No saga for that
tag/task may have live status `pending`, `needs-broker`, or
`cleanup-required`. Any mismatch refuses without a leaf brief; a caller must
reconcile durable state rather than hand an uncertain workspace to a leaf.
The projection's owner is read from the referenced run; no owner or attempt
lineage is stored on the saga.

The controller projection contains exactly `owner`, `claim`, `run`, and
`workspace`, in that order. `claim` contains exactly `token`, `expiresAt`, and
`revision`. `run` contains exactly `runId`, `attempt`, `leaseGeneration`,
`attemptToken`, and `expiresAt`. `workspace` contains exactly `sagaId`,
`sagaToken`, `status`, `path`, `ownerBoundary`, `branch`, `baseCommit`,
`recoveryGeneration`, `recoverRequestId`, and `brokerRequest`. Its path and
boundary are the absolute host identities from the saga. `recoveryGeneration`
is the persisted saga generation. `recoverRequestId` is the deterministic
next §9.4 recovery id for every nonterminal status and is null for a terminal
status, including `bound`. For `needs-broker` it equals the nested
`brokerRequest.recoverRequestId`. `brokerRequest` is null except when the stored
status is `needs-broker`, when it has the exact §9.4 shape. Confinement is
known only to a recovery attempt that observed the §9.5 signal (§9.4), so no
read-only projection derives the confined-compensation broker request. A
`needs-broker` or
other non-bound controller projection is recovery material only and is never
accompanied by a leaf brief. The projection consumes the saga mutation result;
it does not alter that command's exact shape or create another durable result.
Acquisition identities, the Git common-directory path, and the authenticated
fd-3 capability are not members.

A nonterminal saga at SQLite's maximum signed integer recovery generation has
no next id. Rendering its controller projection refuses fail-closed with
`noise-tasks: worktree recovery generation exhausted` (exit 1, empty stdout)
instead of returning a null or wrapped id. Terminal projection and exact raw
receipt replay do not derive another generation and remain available.

For a bound provision, the exact controller projection is, for example:

```json
{"owner":"agent-a","claim":{"token":41,"expiresAt":"2026-08-28T18:00:00Z","revision":3},"run":{"runId":1,"attempt":1,"leaseGeneration":1,"attemptToken":42,"expiresAt":"2026-08-28T18:00:00Z"},"workspace":{"sagaId":1,"sagaToken":43,"status":"bound","path":"/repo/.worktrees/main-7","ownerBoundary":"/repo","branch":"tasks/main/7","baseCommit":"0123456789abcdef0123456789abcdef01234567","recoveryGeneration":0,"recoverRequestId":null,"brokerRequest":null}}
```

The token-free leaf brief contains exactly `schemaVersion`, `tag`, `task`,
`run`, and `workspace`, in that order. `schemaVersion` is 1. `task` contains
exactly the selected top-level task's `id`, `title`, `description`, `details`,
`status`, `priority`, `milestone`, `dependencies`, `subtasks`, and `updatedAt`,
using the same values and empty-array rules as `show --json`, but omits its
revision and claim. `run` contains exactly `runId` and `attempt`. `workspace`
contains exactly `path`, `branch`, and `baseCommit`; its path is the literal
`.` because the host starts the leaf with the registered workspace as its
current directory. It never exposes an absolute host path or owner boundary.

Thus the leaf brief contains no owner, revision, token, lease, acquisition,
request or receipt id, saga id or fence, broker request or capability, family
id, registration root, common-directory path, provider credential, or other
task record or details. Dependency ids are permitted identifiers; they do not
expose the referenced task records. The task text and worktree are proposal
context only. The Trusted Orchestrator validates any leaf output and alone may
use the controller projection to renew, recover, finish, or otherwise commit an
effect.

The matching exact leaf shape is, for example:

```json
{"schemaVersion":1,"tag":"main","task":{"id":"7","title":"Implement layer","description":"","details":"","status":"pending","priority":"high","milestone":"v2.5","dependencies":[],"subtasks":[],"updatedAt":"2026-08-28T17:40:00Z"},"run":{"runId":1,"attempt":1},"workspace":{"path":".","branch":"tasks/main/7","baseCommit":"0123456789abcdef0123456789abcdef01234567"}}
```

### 9.3B Replayable dispatch preparation and reconciliation

`dispatch prepare` and `dispatch reconcile` are JSON-only machine surfaces for a
trusted orchestrator. They compose exactly one `claim-next`, one `run start`,
and one `worktree provision` or `worktree recover` lineage from one stable root
request id. They write no standalone dispatch table, cache, journal, or receipt
and therefore add no shadow authority state: replay and crash recovery are
derived only from the underlying claim receipt, run receipt, worktree saga, and
worktree receipts. Their purpose is to prepare or resume one bounded leaf
assignment and render the exact §9.3A controller projection and, when eligible,
leaf brief. The Trusted Orchestrator's Deck deployment decides how to use those
values; this composition adds no noise-tasks leaf launcher or sandbox.

`dispatch prepare --owner <o> --request-id <root> [--claim-ttl s]
[--run-ttl s] [--milestone m] [--assignment <owner>] --json` resolves the tag
exactly as `claim-next`, then composes §8.3 selection, §9.3 run start, and
§9.4 worktree provisioning for one top-level task. `--json` is required; human
output is not defined for this machine surface and omitting `--json` is a usage
error (exit 2).

`dispatch reconcile <run> --owner <o> --claim-token <c> --attempt <n>
--attempt-token <f> --saga <s> --saga-token <g>
--recover-request-id <id> --root-request-id <root> --json` resumes only the
worktree leg. All controller capability flags are required. Reconcile proves
that the derived claim, start, and provision identities name the supplied
currently live controller for the named provisioning saga's run. The current
attempt may be a retry of the attempt that planned it. For a nonterminal saga,
`<id>` must equal the current controller projection's advertised
`recoverRequestId`, and reconcile delegates exactly to `worktree recover <s>
--saga-token <g> --request-id <id>`. For a terminal saga that projection field
is null: reconcile accepts only the exact recovery request whose verified
receipt produced the terminal outcome, and delegates only to that receipt
replay. No new recovery id is accepted. Reconcile also inherits any
authenticated broker capability on fd 3 unchanged. If the committed records
do not prove that one lineage, reconcile is a typed conflict (exit 3), not a
fallback to `claim-next` or a search for a convenient saga.

This dispatch surface is deliberately narrower than raw §9.4 recovery.
Against a nonterminal saga it rejects a historical receipt id that is not the
currently advertised next id, even though raw `worktree recover` would replay
that receipt. Against a terminal saga it accepts only the receipt that
terminalized the saga, not an earlier historical recovery receipt.

The root request id uses the local-key grammar and is only a derivation seed. It
is not itself a receipt namespace. For each of the literal steps `claim`,
`start`, and `provision`, the exact request id is
`dp1.<step>.<hex64>`, where `<hex64>` is the lowercase hexadecimal SHA-256 of
`noise-tasks-dispatch-step-v1\0` plus canonical JSON
`{"rootRequestId":"<root>","step":"<step>"}`. These ids are stable across
retries, bounded well below the local-key limit, and unique to the root request
id plus step name. Recovery deliberately does not invent a fourth `dp1` id:
its exact `recover-<hex64>` request id is the saga-and-generation-derived value
advertised by §9.4. Reusing one root request id with different normalized
command inputs therefore reuses the same underlying primitive identities and
surfaces their typed replay conflicts.

For root request id `release-25`, the exact derived ids are:

```text
dp1.claim.a724c9d7d6f921ce1f587b374e557912ed15360a7c392218e628b6b706a8a67e
dp1.start.2373de635e80051c8615ed6bcefce2c7164fc8aa14dbcb33151956944c52c58b
dp1.provision.9daa26cc30b6cb1b65b4ddf70934472b93aec2e005b63e94350deacdafa7361c
```

`dispatch prepare` performs at most one attempt of each missing step in this
order:

1. `claim-next` with the derived `claim` request id and the supplied owner,
   normalized claim ttl, milestone filter, and assignment filter.
2. If that selection is the successful empty result, return exit 0 with JSON
   `null` exactly as `claim-next` does. No dispatch or step receipt is created,
   because `claim-next` records none when no acquisition committed. The root id
   and normalized prepare inputs therefore remain unreserved: the same root
   with a changed owner, claim ttl, run ttl, milestone, or assignment filter may
   later acquire work. Each primitive's normalized inputs become
   collision-bound only when that primitive commits its receipt.
3. `run start` with the derived `start` request id, the claimed top-level task,
   the same owner, the acquisition's live claim token, and the normalized run
   ttl. Replay uses the start receipt to prove the same run, then reads that
   run's current live controller rather than reusing historical tokens.
4. `worktree provision` with the derived `provision` request id, the run id,
   owner, current live claim token, current attempt number, and current live
   attempt token from authoritative run state.

On retry, `dispatch prepare` resumes from the first missing committed effect.
The derived claim and start receipts prove the same acquisition and run
lineage, but their returned tokens and attempt are historical after renewal or
retry. Prepare reads the authoritative run and uses its current live claim and
attempt capabilities; it never acquires a new task or invents a new run for
that root. The same run-owned saga therefore resumes without reprovisioning.
When the matching worktree saga is already `bound`, replay verifies its
terminal original receipt and applies every §9.3A
leaf-eligibility check before returning a brief. A bound saga with a stale task
revision, claim, run, attempt, or active-binding edge is exit 3 with empty
stdout, never a successful or blocked handoff. The command never invents a
second run, claim, or worktree lineage for the same root request id.

If the worktree leg cannot yet yield a safe leaf handoff, prepare or reconcile
returns exit 0 with a typed blocked controller result; it never emits a leaf
brief for a nonterminal workspace. The JSON contains exactly `kind`, `reason`,
`rootRequestId`, `task`, `stepRequestIds`, and `controller`, in that order.
`kind` is `dispatch-blocked`; `reason` is `needs-broker` or `needs-recovery`.
`needs-broker` means the controller projection's workspace has that exact §9.4
status and carries the exact non-secret `brokerRequest`. `needs-recovery` means
that workspace is otherwise nonterminal, including `cleanup-required`.
`stepRequestIds` contains exactly `claim`, `start`, and `provision`, in that
order, with the derived values above. `controller` is the exact §9.3A
projection without an added member or changed nested shape; its workspace
already carries `sagaId`, `sagaToken`, `status`, `recoveryGeneration`, the
deterministic next `recoverRequestId`, and nullable `brokerRequest`. This is
the actionable controller handoff. Operational failures and stale or mismatched capabilities
retain the primitive exit 1 or exit 3 result with empty stdout; they are not
relabeled as blocked.

Against a nonterminal saga, `dispatch reconcile` accepts only the advertised
recovery request id for its next generation, performs at most one §9.4 recovery
attempt, and lets that primitive commit and verify its own recovery receipt. A
nonterminal outcome returns the next blocked handoff, including the persisted
generation after that attempt — advanced when the attempt commits, unchanged
when §9.4's unproved provision cleanup attempt commits nothing. Against a
terminal saga, reconcile is
crash-before-output replay only: it accepts the exact terminalizing recovery
receipt's request id even though the current projection's `recoverRequestId` is
null; every new id is exit 3. A terminal replay also requires the saga's
original provision receipt before
dispatch returns a leaf brief. Reconcile never performs a new `claim-next` or
`run start`, switches tasks, accepts a caller filesystem identity, or writes a
dispatch receipt.

The successful JSON object returned by prepare or reconcile contains exactly
`rootRequestId`, `task`, `stepRequestIds`, `controller`, and `leaf`, in that
order. `task` is the selected top-level task id. `stepRequestIds` has the exact
three-member shape above. `controller` and `leaf` are the exact logical objects
defined by §9.3A: dispatch adds no member to either projection. Thus the
controller claim has exactly `token`, `expiresAt`, and `revision`; its workspace
has exactly `sagaId`, `sagaToken`, `status`, absolute host `path`,
`ownerBoundary`, `branch`, `baseCommit`, `recoveryGeneration`, null
`recoverRequestId`, and null `brokerRequest`. The leaf includes
`schemaVersion: 1`, the exact bounded task
shape, run identity, and workspace `path: "."`. A successful result is possible
only for an active bound provision satisfying every §9.3A eligibility rule.
The dispatch-only root and step request ids remain outside both projections.

One backlog task is one reviewable PR layer. When work must land as a stack, the
stack is represented as multiple dependent tasks in the normal DAG, one task per
review layer, rather than a hidden multi-PR payload inside one dispatch. A
successful dispatch therefore prepares exactly one selected backlog task for one
leaf brief.

### 9.4 Recoverable worktree saga

A worktree belongs to one run, not one attempt. Its sandbox working-tree root is
`<ownerBoundary>/.worktrees/<tag>-<task-id>` and its branch is
`tasks/<tag>/<task-id>`; its separate Git administrative registration is under
`<common-dir>/worktrees`. The binding records both identities and the immutable
owner boundary. Invocation from a linked worktree still provisions at that
family-owned root. Retrying an attempt in the same run reuses the workspace;
a later run never adopts it.

If the owner root disappeared or resolves differently, refuse with
`noise-tasks: worktree owner boundary is unavailable: <path>` (exit 1);
never rehome it. The base is `origin/HEAD` when it resolves to a commit, then
local `main`; otherwise refuse with
`noise-tasks: cannot resolve worktree base: origin/HEAD and main do not resolve to commits`
(exit 1). Before Git, a slash tag uses
`noise-tasks: tag <t> cannot name a worktree` (exit 2), a target not proven
ignored uses
`noise-tasks: .worktrees is not git-ignored; add it to .gitignore or .git/info/exclude`
(exit 2), an existing target uses `noise-tasks: worktree path exists: <path>`,
and an existing branch uses
`noise-tasks: branch tasks/<tag>/<id> already exists` (both exit 3). A done
task or mismatched live claim/run/attempt fence also refuses. Branches are
retained after removal, so reprovisioning the same task requires the operator
to remove or rename that retained branch explicitly.

#### Schema v5 and durable states

Schema v5 adds exactly this table and these explicit indexes (SQLite's
autoindexes remain engine-owned):

```sql
CREATE TABLE worktree_sagas (
  saga_id                 INTEGER PRIMARY KEY AUTOINCREMENT CHECK(saga_id > 0),
  run_id                  INTEGER,
  source_saga_id          INTEGER,
  tag                     TEXT NOT NULL,
  task_id                 TEXT NOT NULL,
  operation               TEXT NOT NULL CHECK(operation IN ('provision','remove')),
  status                  TEXT NOT NULL CHECK(status IN ('pending','needs-broker','bound','removed','failed','cleanup-required')),
  binding_active          INTEGER NOT NULL CHECK(binding_active IN (0,1)),
  saga_fence              INTEGER NOT NULL UNIQUE CHECK(typeof(saga_fence) = 'integer' AND saga_fence > 0),
  request_id              TEXT NOT NULL UNIQUE,
  recovery_generation     INTEGER NOT NULL DEFAULT 0 CHECK(typeof(recovery_generation) = 'integer' AND recovery_generation >= 0),
  owner_boundary          TEXT NOT NULL,
  registration_root       TEXT,
  target_path             TEXT NOT NULL,
  branch                  TEXT NOT NULL,
  base_commit             TEXT,
  force                   INTEGER NOT NULL CHECK(force IN (0,1)),
  created_at              TEXT NOT NULL CHECK(length(created_at) > 0),
  updated_at              TEXT NOT NULL CHECK(length(updated_at) > 0),
  extra                   TEXT,
  FOREIGN KEY (run_id) REFERENCES runs(run_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
  FOREIGN KEY (source_saga_id) REFERENCES worktree_sagas(saga_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
  FOREIGN KEY (tag, task_id) REFERENCES tasks(tag, id) ON UPDATE RESTRICT ON DELETE RESTRICT,
  CHECK((operation = 'provision' AND status IN ('pending','needs-broker','bound','failed','cleanup-required')) OR
        (operation = 'remove' AND status IN ('pending','needs-broker','removed','failed','cleanup-required'))),
  CHECK(operation = 'remove' OR force = 0),
  CHECK(base_commit IS NULL OR
        (length(base_commit) IN (40,64) AND base_commit NOT GLOB '*[^0-9a-f]*')),
  CHECK(binding_active = 0 OR (operation = 'provision' AND status = 'bound')),
  CHECK((operation = 'provision' AND run_id IS NOT NULL AND source_saga_id IS NULL AND
         registration_root IS NOT NULL AND base_commit IS NOT NULL) OR
        (operation = 'remove' AND
         (source_saga_id IS NOT NULL OR
          (run_id IS NULL AND registration_root IS NULL AND base_commit IS NULL))))
);
CREATE UNIQUE INDEX worktree_sagas_one_active_binding
  ON worktree_sagas(tag, task_id) WHERE binding_active = 1;
CREATE UNIQUE INDEX worktree_sagas_one_live_effect
  ON worktree_sagas(tag, task_id)
  WHERE status IN ('pending','needs-broker','cleanup-required');
CREATE UNIQUE INDEX worktree_sagas_one_removed_source
  ON worktree_sagas(source_saga_id) WHERE status = 'removed';
CREATE INDEX worktree_sagas_run_history ON worktree_sagas(run_id, saga_id);
CREATE INDEX worktree_sagas_status ON worktree_sagas(status, saga_id);
```

The local-key grammar governs `request_id`.
`recovery_generation` is the number of recovery attempts committed for this
saga; it is not reconstructed from receipts or events. A non-null
`base_commit` is the repository's exact object-id width, either 40 or 64
lowercase hex. `owner_boundary`, `registration_root`, and `target_path`
are absolute canonical paths, and `branch` is the exact derived branch.
`extra`, when present, is a strict object of byte-faithful unknown fields.

A provision references its owning run. A linked removal references its exact
provision source; its `run_id` is either that same run, for a manual or
run-terminal cleanup, or null for direct task-status cleanup. A runless removal
with no source is exact legacy cleanup. A successful linked removal sets the
source's `binding_active` to zero in the same transaction. New bindings exist
only as saga rows; task `wt_*` columns are legacy cleanup evidence.

The saga fence is the sole durable recovery authority. The referenced run,
not the saga, supplies the controller owner. Claim and attempt capabilities
authorize initial planning but are not copied into this table. The immutable
effect identity is the operation, run/source edge, tag/task, owner boundary,
registration root, target, branch, base, force, original request id, and saga
fence. No attempt retry, lease renewal, receipt, or event changes it.

The SQL checks enforce local grammar. `verify` additionally enforces:

- exact request grammar, object-id width, canonical stored paths, and derived
  path and branch;
- a matching provision for every linked removal, with identical tag, task,
  boundary, registration root, path, branch, and base;
- the run relationship above, one active binding and one live effect per task,
  and an inactive source if and only if it has its unique `removed` child;
- `binding_active=1` exactly for a bound provision with no removed child;
- a fencing counter not below any saga fence, and exact automatic request-id
  derivation;
- the original `(worktree, request_id)` receipt for every terminal row; and
- for stored recovery generation `g`, exactly one valid deterministic recovery
  receipt for every generation 1 through `g`, and none outside that range for
  the saga. Each receipt proves the immutable identity, its exact generation,
  and an outcome allowed by the state machine at that generation.

Receipts are checked directly by deterministic generation, not discovered or
ordered through events. Missing feed history is allowed after compaction.
Events are an audit projection and are never recovery authority; verification
does not replay an event chain to infer current state. A missing, duplicate, or
impossible historical receipt, a violated edge, or a colliding `extra` field is
corruption. Removing a task or tag retained by any
saga, including an inactive terminal saga, is a typed exit-3 worktree-history
conflict; force does not discard recovery evidence.

The v4→v5 upgrade performs no Git or filesystem access. In `(tag bytes,
numeric task id)` order it converts each non-null legacy `wt_path` binding to
one runless `remove/cleanup-required` row. Run and source are null,
`registration_root` and `base_commit` are null, `binding_active` and
`force` are zero, `recovery_generation` is zero, and the exact stored
boundary, path, branch, creation time, and task identity are copied. Its
request id uses the legacy automatic derivation below and it receives the next
saga fence. The legacy binding remains byte-identical until recovery removes
it; successful recovery clears its `wt_*` columns in the same finalization
transaction. A store with no legacy binding allocates no fence. Conversion adds
no per-row event or receipt; the single `schema/upgraded` event audits the
batch.

`worktreeSagas` is physical only in v5. Its semantic record contains exactly
`sagaId`, nullable `runId`, nullable `sourceSagaId`, `tag`, `task`,
`operation`, `status`, `bindingActive`, private `sagaFence`,
`requestId`, `recoveryGeneration`, `ownerBoundary`, nullable
`registrationRoot`, `path`, `branch`, nullable `baseCommit`, `force`,
`createdAt`, `updatedAt`, and every flattened `extra` field. No literal
`extra` key exists. Records are ordered by numeric `sagaId`. V2, v3, and v4
encode the class as empty; v5 inventory counts every row. Semantic-digest
version and class order remain 1, and the comparison digest still excludes
this class. The v4→v5 conversion changes the full semantic digest while leaving
the `bindings` class byte-identical. The resulting comparison digest always
changes because the batch appends its one `schema/upgraded` event. With legacy
bindings it additionally advances the comparison `fence` record exactly once
per binding, in the deterministic order above; every other comparison class
remains byte-identical. Exact backup catalogs admit v2 through v5; restore
upgrades only its verified private stage. Legacy compatibility bundles remain
exact v3.

#### Provision, remove, and recovery

`worktree provision <run> --owner <o> --claim-token <c> --attempt <n>
--attempt-token <f> --request-id <id>` and
`worktree remove <run> --owner <o> --claim-token <c> --attempt <n>
--attempt-token <f> --request-id <id> [--force]` validate local grammar before
opening the store. Provision/remove reject caller ids beginning `auto-` or
`recover-`. Before a write transaction, the trusted process derives the
immutable owner boundary, registration root, target, and branch; provision
resolves the base, and removal reads it from the active source. Read-only
Git/filesystem preflight runs before planning.

Each original operation has three phases:

1. One immediate transaction validates the task, live claim, run, current
   attempt, supplied capabilities, lease generation, and database
   preconditions exactly once. It allocates `saga_id` and `saga_fence`,
   inserts `pending` with the immutable effect identity, and appends a
   content-free `worktree/planned` or `worktree/removing` event. Removal
   snapshots the active source.
2. After commit, with no SQLite transaction open, it repeats collision and
   safety checks and performs the exact idempotent Git/filesystem effect.
   Provider, Git, broker, filesystem, cleanup, and fsync effects never run in a
   SQLite transaction. Exact registration confinement records
   `needs-broker`; a broker runs only during later recovery with §9.5 fd 3.
3. One immediate transaction conditionally finalizes the unchanged saga
   fence, state, effect identity, and observed result. A provision may become
   `bound` only while its task is not done and its referenced run still has the
   same tag/task identity and is `running` or `retryable`. It does not
   revalidate or copy a claim, current attempt, token, generation, or lease.
   Removal needs no live run.
   Success records `bound` or `removed`, updates binding activity, and writes
   the event and original receipt. If the provision run is terminal or another
   conditional changed, the process closes SQLite, compensates the exact
   created worktree, then records `failed` only when absence is proved or
   `cleanup-required` otherwise.

`failed` is terminal and means the requested effect and everything uniquely
attributable to it are proven absent. It has the original receipt and follows
the exit-1 empty-stdout contract in both output modes. `cleanup-required` is
nonterminal and means exact presence, absence, or ownership could not be
proved. A worktree provision, remove, or recover result — fresh or exact
receipt replay — with status `cleanup-required` is the exit-0 mutation
result under `--json`; on human invocation it is exit 1 with empty stdout
and exactly `noise-tasks: worktree cleanup is required` on stderr. It never
guesses, clears a binding, or deletes a branch. Automatic cleanup never
sets force.

The only state transitions are:

- provision: `pending → needs-broker|bound|failed|cleanup-required`;
- remove: `pending → needs-broker|removed|failed|cleanup-required`;
- provision recovery: `pending|needs-broker` to
  `needs-broker|bound|failed|cleanup-required`, while `cleanup-required`
  commits `failed` only: an attempt that cannot prove exact absence retains
  `cleanup-required` without a committed transition, receipt, event, or
  consumed generation;
- removal recovery: `pending|needs-broker|cleanup-required` to
  `needs-broker|removed|failed|cleanup-required`;
- run terminalization or direct task done: the affected nonterminal provision
  becomes `cleanup-required` without advancing recovery generation.

The `failed` removal transition is forbidden while its source run is terminal
or its task remains done: such cleanup may terminalize only as `removed`, or
remain `needs-broker`/`cleanup-required`. If the task is reopened and the
source run is nonterminal, the ordinary removal transitions apply.

Terminal operation, effect identity, original request id, saga fence, and
recovery generation never regress. The sole later mutation of a terminal
provision is `binding_active: 1 → 0` when its exact linked removal commits.

`worktree recover <saga> --saga-token <f> --request-id <id>` accepts no
owner, run, attempt, path, branch, base, or force override. It validates the
private saga fence, then checks the common `(kind="worktree", requestId)`
namespace before inspecting current saga status. If an exact recovery receipt
exists, recover validates its envelope, deterministic generation id, input and
result digests, saga/source identity, and allowed historical outcome, then
replays that historical public result without an effect. This receipt-first
path works even when the row is now at a later generation or terminal. A
receipt for different normalized input is the common typed exit-3 collision;
a malformed or inconsistent receipt is corruption and fails closed.

Only when no receipt exists must the saga be nonterminal. Before deriving or
comparing the next id, recover checks `recovery_generation`; at SQLite's
maximum signed integer, 9223372036854775807, it returns
`noise-tasks: worktree recovery generation exhausted` (exit 1, empty stdout).
It performs this check before any Git, broker, filesystem, or compensation
effect. Otherwise the request must be the deterministic id advertised for the
current generation. A terminal saga with a receipt-missing request is exit 3.
The command snapshots only recorded identities and closes its read transaction
before Git or broker work. A `needs-broker` recovery requires the exact §9.5
capability on fd 3. The effect and compensation are idempotent for those
immutable identities.

A provisioning recovery rechecks only the immutable task/run relationship,
task status, and run status. If the task is done or the referenced run is
terminal, it never creates, binds, or delegates creation to the broker. It
instead compensates any exact effect left by an earlier phase and commits
`failed` when absence is proved or `cleanup-required` otherwise. While the
task is not done and the run is `running` or `retryable`, recovery may bind
regardless of which attempt is current, except that `cleanup-required` is
cleanup-only and can never become bound. SQLite serialization makes this race
complete: either binding commits first and run/task terminalization schedules
its removal, or terminalization commits first and binding cannot commit.

A `cleanup-required` provision stays cleanup-only, but its recovery is not
broker-blind. When the compensating registration write itself produces the
§9.5 permission-class confinement signal, recovery may delegate that exact
compensation step through the §9.5 broker capability — the `remove` verb
over the same immutable identity, requested under the same advertised
deterministic `recover-` id; no new verb, request-id form, or command
exists for it. Such an attempt terminalizes `failed` only after exact
absence is proved. Otherwise the saga remains `cleanup-required` without
consuming a generation, writing a receipt, or appending an event, and the
command returns its current handoff — same status, generation, and
advertised id — so the identical request may be retried. When the attempt
observed the confinement signal, that result also carries the
deterministic non-secret broker mint bindings: the exact §9.4
`brokerRequest` shape with verb `remove`, the same immutable identity,
and the still-advertised `recoverRequestId`, letting a controller mint
the §9.5 capability and retry identically. Carrying them persists
nothing; no saga field, state, or command is added. A
`cleanup-required` provision never binds, whatever the task or run
liveness, and never returns to `needs-broker`: confinement discovered
during cleanup never commits that status.

If the snapshot generation is `g`, finalization is a CAS on the unchanged
saga fence, status, and `recovery_generation=g`. Every committed recovery
attempt, including one that remains `needs-broker` or `cleanup-required`,
sets the generation to `g+1` and writes one immutable recovery receipt; the
unproved `cleanup-required` provision attempt above is the sole exception,
because it performs no CAS and so commits nothing. A crash before that
commit leaves generation and advertised id unchanged, so the same id safely
repeats the idempotent effect. A concurrent winner makes the loser receipt
replay or a typed exit-3 conflict; no event-history count is consulted.

Broker work that does not reach a reconciled outcome commits nothing.
Inability to invoke fd 3 — no capability inherited, or one that is
unauthenticated, expired, or bound to another identity or verb — a broker
invocation that exceeds its bound and is refused as timed out, an explicit
broker refusal, and a broker that cannot be strictly resolved or executed
each leave the current nonterminal status, the stored recovery generation,
and the advertised deterministic recovery request id unchanged: no event,
receipt, or saga field is written, and the identical request may be
retried. Only a host outcome the process has successfully reconciled
against the snapshot identities reaches the finalization CAS and consumes
a generation; a definitive permission-class confinement signal from the
direct registration write is such an outcome and still commits
`needs-broker` normally. When broker success is uncertain — a lost,
partial, or otherwise unclassifiable broker result — the host fails
closed exactly as above and retries the identical idempotent broker
capability and request under the same advertised id; §9.5's verbs are
idempotent for identical bound inputs, so that retry is safe. Each such
failure keeps its existing surface contract: empty stdout, the bounded
§9.5 diagnostic on stderr, exit 1 as an operational failure or exit 3 as
a typed capability conflict.

When recovery reaches a terminal state, its transaction also writes the
original operation's receipt if absent. That receipt is found by
`(kind="worktree", original request_id)`, not a saga-row receipt field.
The recovery receipt is found by its own common request id. Any exact recovery
receipt can replay through raw recover; a receipt-missing request against a
terminal saga is exit 3. Recovery authorization is always the saga fence, even
after attempt retry or run terminalization.

The first direct registration attempt discovers confinement only after the
saga fence and capability bindings exist, so no broker capability can be
pre-minted for it. Exact permission-class confinement commits
`needs-broker` and returns the non-secret broker mint bindings under
`--json`. The controller mints a short-lived §9.5 capability, places it only
on fd 3, and calls recover with the advertised id. Human output retains
`noise-tasks: worktree registration is confined and no broker is available`
(exit 1, empty stdout). Broker absence, mint failure, or refusal leaves the
saga recoverable.
The stored `registration_root` is not ownership proof. Immediately before
every direct or broker effect, the trusted host rederives the exact Git common
directory for the stored family, proves its registration root and owner
boundary, and requires equality with the immutable row. A legacy row derives
that root at effect time because its stored value is null. A mismatch fails
closed without an effect or row rewrite.

#### Replay, receipts, events, and outputs

The saga row reserves its original `(kind="worktree", requestId)` before a
terminal receipt exists. Different normalized input is exit 3. An identical
original request may repeat phase 2 only while the row is still `pending` and
`recovery_generation=0`, the exact crash-before-initial-finalization state.
Once an initial outcome is `needs-broker` or `cleanup-required`, or any recovery
generation has committed, original replay only returns the current handoff and
performs no effect or mutation. It never allocates a second saga. Returning its
saga token requires a currently live controller for the same run, but the
current attempt may differ from the one that planned it. This authorizes
disclosure, not the effect. Runless automatic and legacy rows are recoverable
only through trusted prune.

Completed original replay verifies the common receipt, saga, source edge, and
permitted current state and performs no effect. Recovery requests use the
common receipt namespace directly. The row and recovery generation decide
resume; receipts prove committed requests, and events only audit them. There
is no recovery reservation, event-chain reconstruction, or hidden replay table.

Original input digest is lowercase `sha256:<hex>` over canonical JSON with
exactly `force`, `operation`, `owner`, `requestId`, `runId`, and
`sagaId`. Provision/remove use `sagaId: 0`. Automatic operations use empty
owner and their recorded run id or zero. Recovery input instead contains
exactly `force`, `operation: "recover"`, `recoveryGeneration`,
`requestId`, `runId: 0`, and `sagaId`; the generation is the one the
attempt commits. Result digest contains exactly `active`, `baseCommit`,
`branch`, `committedAt`, `force`, `operation`, `ownerBoundary`,
`path`, `recoveryGeneration`, `runId`, `sagaId`, `status`, `tag`,
and `task`. No digest contains a capability, registration root, or failure
text.

A completed worktree receipt contains the common §8.4 envelope and exactly
`operation`, `sagaId`, nullable `sourceSagaId`, `tag`, `task`,
nullable `runId`, `owner`, `status`, `active`, `ownerBoundary`,
`path`, `branch`, nullable `baseCommit`, `force`, and
`recoveryGeneration`. The owner is derived from the referenced run and is
empty for a runless row. An original receipt uses generation zero for an
effect completed without recovery, or the current generation when recovery
made it terminal. A recovery receipt uses the generation that attempt
committed. Receipt id remains `worktree-` plus lowercase SHA-256 of
`noise-tasks-worktree-receipt-v1\0`, request id, one NUL, and the ASCII
input digest.

Receipts preserve historical request results; the saga row is current
authority. A bound provision receipt retains `active=true` after its unique
removed child sets the source row inactive. Likewise, run or task
terminalization may change a provisioning row from `needs-broker` to
`cleanup-required` without advancing its recovery generation, so that
generation's receipt may retain `status=needs-broker`. Verify accepts only
these state-machine-authorized historical differences while requiring the
same immutable identity and generation. Exact receipt replay returns its
historical result; a fresh read returns the current saga handoff. No event is
needed to justify either change.

Terminal original events are `bound`, `removed`, or `failed` and carry
their original receipt reference. A committed recovery appends
`worktree/recovering` with its recovery receipt; when it terminalizes, the
original terminal event and receipt are written in the same transaction.
That terminal recovery transaction writes in this exact order: first the
`worktree/recovering` event and its recovery receipt, then the original
operation's terminal event and its original receipt. Each worktree
receipt's §8.4 event sequence range covers exactly its own one event —
the recovery receipt the `recovering` event, the original receipt the
terminal event — never a shared, empty, or crossed range. Both semantic
inventories — the §8.5 manifest inventory counters and the §8.5
semantic-digest record classes — are computed from the final transaction
state, so no inventory or digest observes the recovery event or receipt
without also observing the terminal event, the original receipt, and the
final saga row. Nonterminal original outcomes append `needs-broker` or
`cleanup-required` without a receipt. Events contain only the §8.4
content-free fields. Feed compaction or a missing historical recovery
event cannot change recoverability.

The provision/remove JSON result contains exactly, in order, `sagaId`,
`operation`, `status`, nullable `runId`, `ownerBoundary`, `path`,
`branch`, nullable `baseCommit`, `sagaToken`,
`recoveryGeneration`, nullable `recoverRequestId`, and nullable
`brokerRequest`. `recoverRequestId` is non-null only for a nonterminal
status and is normally required there; the exhausted nonterminal mutation
result defined below is the sole null exception. Recover JSON has the same
shape except `sagaToken`. Human output
renders the non-secret fields and the saga token only for manual
provision/remove; human recover never renders a token or broker request.

The status, output, and exit contract for fresh and replayed saga outcomes
is:

| Committed outcome | Fresh `--json` | Fresh human | Replay `--json` | Replay human |
| --- | --- | --- | --- | --- |
| `failed` (terminal) | exit 1, empty stdout | exit 1, empty stdout; diagnostic is stderr-only | exit 1, empty stdout; no effect | exit 1, empty stdout; no effect |
| `needs-broker` (nonterminal) | exit 0; mutation result with `brokerRequest` and `recoverRequestId`; `sagaToken` only in provision/remove results | exit 1, empty stdout; §9.5 confinement diagnostic on stderr | exit 0; historical result at its recorded generation; same token rule | exit 1, empty stdout; §9.5 confinement diagnostic on stderr |
| `cleanup-required` (nonterminal) | exit 0; mutation result; `brokerRequest` null except in a recover result whose attempt observed the §9.5 confinement signal; `recoverRequestId` non-null — null only at the exhausted generation | exit 1, empty stdout; `noise-tasks: worktree cleanup is required` on stderr | exit 0; historical result at its recorded generation; same token rule | exit 1, empty stdout; `noise-tasks: worktree cleanup is required` on stderr |
| `bound` or `removed` (terminal) | exit 0; mutation result; `sagaToken` only in manual provision/remove results | exit 0; non-secret fields; saga token only for manual provision/remove | exit 0; historical result; saga token only through token-bearing replay surfaces | exit 0; non-secret fields; token only for manual provision/remove |

Exact receipt replay preserves the historical result's mode-specific
output class: each replay cell applies the recorded outcome's contract to
the invoking mode, never swapping classes between modes. Recovery output
never exposes `sagaToken`: recover JSON omits the field, human recover
renders neither a token nor a broker request, and exact recover receipt
replay keeps both rules in both modes. The `failed` row binds the original
operation and the recovery that terminalizes it. The `cleanup-required`
row also covers the unproved provision cleanup attempt above, which
returns the unchanged handoff and writes nothing; its `--json` form
carries the confined-compensation `brokerRequest` when that attempt
observed confinement. A receipt-missing request against a terminal saga,
and a request whose normalized input differs, remain exit 3 under §8.4.

`brokerRequest` is non-null only for `needs-broker` and in the recover result
of a `cleanup-required` provision attempt that observed the §9.5 confinement
signal. It contains exactly, in order, `familyId`, `verb`,
`registrationRoot`, `ownerBoundary`, `targetPath`, `branch`, nullable
`baseCommit`, `force`, and `recoverRequestId`. The verb is `create` for
provision, `remove` for linked removal and for that confined
compensation, and `prune-registration` for runless legacy cleanup. The
registration root is the exact effect-time proof above. The broker request is
non-secret mint input and omits uid, gid, expiry, authentication, and every
claim/run capability.

For current stored generation `g`, the advertised recovery request commits
generation `g+1` and is `recover-` plus the 64 lowercase hex digits of
SHA-256 over `noise-tasks-worktree-recover-v1\0`, family id, one NUL,
decimal saga id, one NUL, and decimal `g+1`. For family
`123e4567-e89b-42d3-a456-426614174000`, saga 1, and next generation 1, it is
`recover-b9b9942729a6879f2266171a38aee8bd1180e34262584040608bc5464eff150c`.

The formula is evaluated only for `g < 9223372036854775807`. A recovery from
the preceding generation may commit that maximum. If its outcome is still
nonterminal, its exact result has null `recoverRequestId` and null
`brokerRequest`; no wrapped or saturated id is advertised. Thereafter a new
raw recovery, controller/dispatch projection, or prune broker handoff refuses
with the exhaustion diagnostic before any effect. Receipt-first raw replay of
an already committed request remains valid because it derives no next id.

The only public saga-token surfaces are manual provision/remove JSON, their
verified token-bearing replay, the controller projection, and trusted
`prune --json`. List, human prune, recover, receipts, feed, diagnostics,
logs, run/task reads, and the leaf brief omit it. Token-bearing replay requires
the same run owner and a current live controller lease for that run, not the
planning attempt. Prune may expose a stored fence only in its explicit
controller-only JSON handoff.

The provision input vector remains:

```text
{"force":false,"operation":"provision","owner":"agent-a","requestId":"wt.provision","runId":1,"sagaId":0}
sha256:b53d9c2f78031fe22f0a068a5801ce58d605c1ab6d51b2db6719483e1dc08ab9
```

The recovery input vector commits generation 1:

```text
{"force":false,"operation":"recover","recoveryGeneration":1,"requestId":"req.recover","runId":0,"sagaId":1}
sha256:56a1cc42bcad2f72b2b4b74dd7de7260cbdfe242cd5cf5b29f60f0ca96ad5afb
```

#### Automatic cleanup, reads, and cutover

Run finish that records `succeeded`, run cancel, force release that terminalizes
a run, and direct top-level `set-status ... done` establish cleanup authority
in their own transaction. When an active bound source has no live linked
removal, the parent inserts one unforced `remove/pending` saga against it. A
run-caused row references the source run; a direct task-done row is runless.
When the source already has a live linked removal, the parent adopts that exact
saga instead of inserting a conflicting row. Adoption changes no immutable
identity, request id, run/source edge, fence, or force bit. In particular,
automatic cleanup never turns a manual force bit on or off.

If the only live effect is the run's nonterminal provision, the parent changes
it to `cleanup-required` instead of inventing a removal source. Recovery then
compensates any exact partial effect and can only record `failed` or retain
`cleanup-required`; it never binds for a terminal run or done task. If binding
won the serialization race first, the parent sees the active source and
inserts or adopts its removal. Thus a late provision cannot escape cleanup,
while an attempt retry within the still-live run retains the same workspace.

Scheduling, coalescing, and the parent mutation share one transaction; the Git
effect starts only after commit. Neither a new nor adopted cleanup saga stores
attempt lineage, a cause revision, or a second authority record. The parent
event audits the decision without adding a second event to its receipt range,
and no new worktree receipt exists until an effect attempt commits.

While the source run is terminal or the task remains done, a new or adopted
linked removal may terminalize only as `removed`; a nonterminal attempt may
remain `needs-broker` or `cleanup-required`, but never becomes terminal
`failed`. Proven absence is successful removal and deactivates the source. If
the task is later reopened and the source run is nonterminal, ordinary manual
removal semantics may apply. This state-dependent rule does not rewrite the
adopted saga's identity or force bit.

For a newly inserted cleanup, automatic request id is `auto-` plus lowercase
SHA-256 over
`noise-tasks-worktree-auto-v1\0`, family id, one NUL, decimal source saga id,
one NUL, and the cause identity. Cause identity is `run:<run-id>` for
run-terminal cleanup and the literal `task-done` for direct done. The source
saga makes the latter unique without persisting task revision. The legacy
conversion instead hashes the same domain, family id, one NUL, the literal
`legacy`, one NUL, tag, task, canonical stored path, and branch separated by
NULs. A generated-id collision with a nonidentical saga or receipt is
corruption. Caller commands cannot use the reserved prefix.

Recovery of a newly inserted or adopted cleanup saga needs no extant claim,
attempt, lease, or parent task revision while the terminal/done cleanup
condition holds. The parent transaction established or coalesced its cleanup
authority; later work is fenced solely by its existing saga token. It may
proceed through trusted prune after run or task state changes. A cleanup result
never rolls back the already committed parent mutation. Post-commit failure may
warn but does not change the parent's successful result.

`worktree list [--json]` returns exactly `entries` and `unknown`. Entries
are ordered by saga id and contain exactly `sagaId`, nullable `runId`,
`tag`, `task`, `operation`, `status`, `active`, `ownerBoundary`,
`path`, `branch`, nullable `baseCommit`, `force`, and `updatedAt`.
Unknown filesystem entries contain exactly `path` and are bytewise sorted.
List omits request ids, registration roots, generations, fences, and
capabilities.

`worktree prune --json` is a Trusted-Orchestrator surface and returns exactly
`results`, `brokerRequests`, and `unknown`. Results are ordered by saga id
and contain `sagaId` and committed `status`. Each broker request contains
`sagaId`, `sagaToken`, and the exact broker request above, using the
currently advertised generation. Prune snapshots eligible rows, closes its
read transaction, and invokes recovery only for exact saga or legacy evidence.
Shape alone is never ownership proof. Human prune never prints a saga token or
mint binding. The leaf cannot invoke list, prune, recover, or store access.

The v5 cutover removes `worktree create`, `claim --worktree`, and
`--keep-worktree` from claim, release, and set-status. Each is a usage error
with guidance to acquire a claim, start a run, and use
`worktree provision`. Branch retention and explicit operator
`worktree remove --force` replace keep flags.

### 9.5 Confined broker boundary

The sandbox must deny the leaf identity access to the Git common directory,
the SQLite directory, provider credential sources, the trusted plugin root,
and inherited claim/run or maintenance capabilities. The leaf receives a
working-tree view without a usable `.git` escape when the host cannot safely
expose one. A deployment in which the leaf can act as the orchestrator's OS
principal does not satisfy this boundary.

When `<common-dir>/worktrees` is confined, the trusted process may delegate
only the registration step to the installed `noise-worktree-broker`. The
broker is resolved from the immutable §5.2 root, accepts exactly
`create|remove|prune-registration`, and requires an operation-specific,
authenticated host capability on inherited fd 3. It refuses direct invocation
of those verbs without that fd-3 capability, and any authentication, expiry,
or binding mismatch. The capability binds
family id, saga fence, verb, common directory registration root,
owner boundary, exact sandbox target and branch, the saga-recorded immutable
base commit (null only for exact legacy cleanup), force bit, invoking uid/gid,
and expiry;
it contains no
claim/run token or credential. The leaf never inherits fd 3.

Confinement is detected by the registration write itself, not by a pre-flight
probe: a permission-class failure of the direct registration write into
`<common-dir>/worktrees` is the confinement signal. Except during a
`cleanup-required` provision's compensating registration write — governed by
§9.4's confined-compensation rule, which never records `needs-broker` — the
process records
`needs-broker`; only a later trusted `recover` bearing the exact fd-3
capability delegates that registration step through the broker resolved by the
bounded §5.2 runtime resolution. That recover never repeats the completed
direct attempt. A broker that cannot be strictly resolved, or that cannot be
executed, counts as no available broker. The broker child runs with a fixed
argv and a stripped, fixed environment, and only bounded, sanitized output is
consumed as the refusal reason; a broker invocation that exceeds its bound is
refused as timed out.

The host pins the direct Git write's diagnostic locale and classifies
confinement only when the permission diagnostic names the exact canonical
`<common-dir>/worktrees` root (or its exact boundary-relative spelling); a
sandbox target or an unrelated ancestor named `worktrees` is not sufficient.
Before removal, the host resolves and validates the target's exact direct
registration directory, atomically stages the already dirty-checked sandbox
target within its owner boundary, and attempts an atomic same-directory rename
of that registration. That rename is the registration write and either
succeeds completely or supplies the confinement signal without emptying the
registration first. Broker unavailability or refusal restores the staged
target; registration success precedes deletion of the staged target. A failed
create compare-deletes its newly created branch only when the branch still
names the planned base and neither the exact target nor its registration
exists. The durable saga remains authoritative for any state that cannot be
proven safe for this exact compensation.

The broker validates those bindings with no-follow directory operations,
enters only the host-provided writable namespace, drops permanently to the
invoking uid/gid before Git or target effects, uses a fixed argv and stripped
environment, and returns only bounded success or a sanitized refusal. It does
not open SQLite, choose a task, mint a path, hold state, listen, or run as a
daemon. Permission failure without a valid broker capability is
`noise-tasks: worktree registration is confined and no broker is available`;
a refusal is `noise-tasks: worktree broker refused: <bounded sanitized
reason>`. On a human invocation each is exit 1 with empty stdout. Under
`--json`, an exact recoverable confinement outcome instead follows §9.4's
exit-0 `needs-broker` saga mutation result; any diagnostic remains stderr-only.
Both leave the saga recoverable. A confinement signal raised by a
`cleanup-required` provision's compensating registration write follows §9.4's
cleanup-required contract instead — never `needs-broker`, and its human
diagnostic is the cleanup-required one.

The broker is stateless and SQLite-blind, so it does not claim to distinguish
an identical retry from a replay. Its three verbs are idempotent for identical
bound inputs. The worktree saga and the shared `(worktree, requestId)` receipt
rule decide whether to avoid, repeat, finalize, or compensate that effect;
changed inputs fail capability binding.

### 9.6 Quality bar additions

Per §11, tests cover strict plan parsing and canonical digests; offline and
online validation; local/existing DAGs; atomic id/milestone/assignment mapping;
plan and request replay; claim-next races; dispatch prepare/reconcile replay,
successful-empty pass-through and unreserved root reuse, blocked
needs-broker/needs-recovery results, terminal crash-before-output receipt
replay, and no-shadow-state resume from underlying receipts and sagas; every
run/attempt transition,
expiry, retry, sole joint renewal, force cancellation, stale fence, and parent
revision for subtask mutation; atomic optional status publication;
saga crashes before and after every database, Git, broker, and compensation
boundary; dirty worktrees, family owner-boundary persistence, deterministic
base/path refusals, naming, and branch
retention; recovery idempotency; no transaction open during child execution;
content-free feed/receipt linkage; exact v4→v5 legacy conversion and semantic,
comparison, and inventory digests; every state transition and generated-id
collision; 40- and 64-hex object ids; persisted recovery-generation CAS and
one deterministic receipt for every generation without event-chain
reconstruction; receipt-first historical raw replay versus narrower dispatch
reconcile; maximum-generation refusal before derivation or external effect;
automatic cleanup coalescing onto an existing live removal with immutable
force and identity; automatic run-terminal and task-done scheduling without
copied attempt or task-revision
lineage; original-versus-recovery common receipt lookup;
retained-saga task-removal conflict ordering; crash-before-output replay;
saga planning on one attempt followed by retry in the same run; both
serialization orders of provision finalization versus run terminalization;
saga-mutation JSON versus human `needs-broker` output; confined-handoff
eligibility, exact projection/brief shapes and task-revision drift; and token,
acquisition, host-path, saga, broker, receipt, and family-state redaction from
the leaf brief. Privileged opt-in package tests prove fd-3 authentication,
expiry and binding mismatch refusal including force mismatch, identical broker retry
idempotency, privilege drop, fixed broker argv/environment, fd closure, denial
of broker invocation without a capability, and exact confined diagnostics.
The Trusted Orchestrator's deployment conformance suite, not this package,
proves that its leaf principal and sandbox cannot reach the store, credentials,
broker capability, controller command surface, or hidden host paths.

## 10. Non-goals (v2.5)

No daemon, no TUI, no tag CRUD beyond reading what exists, no AI-backed
`update`/`expand`, no telemetry, no secret-bearing configuration, no
project-defined command or provider plugin, and no host-managed plugin
installation. Task state and the closed project preferences file are the only
project configuration surfaces. The §8 database adds no daemon, watcher,
network listener, or push subscription to this list.

## 11. Quality bar

- `go test ./...` covers: round-trip fidelity of the §1 model through the
  database and the `--file` fixture surface, including a real 100-task
  fixture, every command's happy path and refusal paths, cycle
  detection, transaction atomicity (a killed process never commits a
  partial mutation),
  concurrent writers, atomic plan batches, and the legacy `master`
  tag carried through §8.6 migration.
- Setup tests cover first onboarding, task-only repair, valid preference reuse
  and replacement, noninteractive refusal, PTY prompt/preview/confirmation,
  `--yes` semantics, closed-schema and size rejection, symlink/special-file
  refusal, crash after database creation, identical and conflicting
  concurrent setups, §8.6 migration-state refusal, and exact JSON and
  missing-project diagnostics. They assert availability for in-process and CLI
  adapters, the unavailable-but-keepable repair path, and setup-time
  project-containment refusal. Existing task commands are tested with missing,
  malformed, and newer preferences to prove they do not read generation
  configuration.
- Generation integration tests cover each compiled adapter with a fake or
  hermetic endpoint/executable, fixed argument vectors, no-shell execution,
  leading-hyphen model rejection, invalid-preferences refusal without ambient
  fallback, project-contained executable refusal, bounded stdout/stderr and
  timeouts, invocation-time preference snapshots during concurrent setup, selector
  compatibility warnings and conflicts, requested/resolved model reporting,
  provider failure isolation, dependency-cycle rejection, and all-or-nothing
  backlog publication.
- Host integration tests use real child processes to cover fixed-root build
  selection, every setup and runtime refusal, noun grammar, deterministic
  discovery, project context inside and outside Git, byte-identical arguments
  and environment, standard streams, signals, exit statuses, help, version,
  unsupported platforms, direct plugin use, the compatibility shim and alias,
  environment conflicts, and generation through the router.
- Unprivileged real-process tests may act as their own setup authority: create
  and sync a complete staged root in an exclusive temporary directory, publish
  it by atomic rename, and embed that absolute root in a test host at
  build time. A sticky system temporary parent is permitted because unrelated
  users cannot replace the owned child. No untrusted runtime identity is
  declared for that test installation, so the test process may be both setup
  owner and invoker. Setup-refusal tests separately declare the test identity
  as an untrusted runtime agent or create an invalid path. Those binaries are
  test artifacts and are never packaged. This is the only exception to the
  production location rule; production roots remain fixed outside workspaces
  and have no runtime override.
- On a clean checkout with no network, the merge gate is exactly
  `go build ./... && go vet ./... && go test ./...`. The release gate also
  runs `go test -race ./...`, the real multi-process and linked-worktree
  suites, backup/restore and migration fault injection, and the opt-in
  privileged broker suite on a supported isolated runner.

## 12. Milestones

This section defines the backlog's grouping primitive: a per-tag milestone
registry and a task's optional `milestone` field naming one registry entry.
Milestones organize work inside one tag; they do not change id assignment,
dependency semantics, or subtask handling. The registry and the field are
ordinary §1 state: unknown fields on registry entries are preserved
verbatim on write, and a binary that predates this section round-trips both
the registry key and the task field untouched.

### 12.1 Data model

A tag block may carry one `milestones` key: an array of entries whose
array position is the registry order (the tool never re-sorts it):

```json
"milestones": [
  { "name": "auth refactor", "status": "open" },
  { "name": "v1.3 release", "status": "closed" }
]
```

- `name` is trimmed, nonempty, valid UTF-8 of at most 256 bytes, contains
  no ASCII control character, and does not begin with `-`. Names are
  unique within a tag by exact string identity; nothing lowercases,
  normalizes, or case-folds them.
- `status` is exactly `open` or `closed`. Closing is a statement about
  scope, not an invariant: `close` never refuses because tasks remain,
  and its report names the count of assigned tasks not yet
  `done`/`cancelled`.
- A task's `milestone`, when present, names a registry entry in the same
  tag. Absent means unassigned. Subtasks do not carry the field; §1's
  byte-faithful subtask handling is unchanged.
- Commands that assign the field refuse a name that is not in the
  registry. A stored `milestone` value naming no registry entry is a
  `verify` finding (§12.4), never a parse error: the file stays readable
  and writable.
- A `milestones` value that is not an array, an entry that is not an
  object or lacks `name`, or an entry `status` outside `open | closed`
  is a `verify` finding as well; commands that mutate the registry
  refuse such a file rather than repairing it.

### 12.2 Commands

`milestone` is a backlog-scoped command group; every subcommand accepts
`--file`, `--tag`, and `--json` under §4 and follows §6's output rules:

- `milestone add <name>` appends one open entry and prints the name. An
  invalid name is a usage error (exit 2); a duplicate name is a typed
  conflict (exit 3, `noise-tasks: milestone <name> already exists in tag
  <tag>`).
- `milestone list` prints, in registry order, each entry's name, status,
  and task rollup (assigned total and `done` count). Under `--json` one
  object: `{ "tag": <tag>, "milestones": [ { "name", "status", "order",
  "total", "done" } ] }` with `order` the zero-based array position.
- `milestone close <name>` and `milestone reopen <name>` flip the entry's
  `status` in place (splicing only that value; every other byte of the
  registry stays identical) and report `remaining`, the count of assigned
  tasks whose status is not `done`/`cancelled`. An unknown name is a
  usage error (exit 2). Flipping to the status the entry already has is a
  successful no-op.
- `milestone remove <name> [--force]` refuses, as a typed conflict (exit
  3) naming the referencing task ids, while any task in the tag carries
  the milestone, unless `--force`, which also removes the field from
  those tasks and bumps each `updatedAt` — the §4 `remove --force`
  ref-stripping semantics applied to the milestone field. Removing an
  unknown name is a usage error (exit 2).

Task commands gain:

- `add --milestone <name>` sets the field on creation; an unknown name
  refuses before any write (exit 2).
- `set-milestone <id> <name>` sets the field to a known registry name;
  `set-milestone <id> --clear` removes it. Supplying both a name and
  `--clear`, or neither, is a usage error. Only a top-level id is
  accepted; a hierarchical subtask id is a usage error (exit 2). Under a
  live authority the command is a §1 field mutation in full: it requires
  `--revision`, increments the task revision, and appends one `update`
  event. Setting a task's milestone to the value it already carries is a
  successful no-op that still consumes the revision check but changes
  nothing.
- `list --milestone <name>` filters to tasks assigned to that name; an
  unknown name is a usage error (exit 2).
- `next` considers only tasks whose `milestone` is absent or names an
  open entry; `next --milestone <name>` further restricts candidates to
  that milestone, which must be a known name (exit 2 otherwise) and,
  like the unflagged form, still yields nothing once the entry is closed.
- `show <id>` reports `milestone: <name>` after the priority line when
  set. The `list --json` and `show --json` task object carries
  `milestone`, always present, empty when unassigned.

### 12.3 Live authority

- The per-tag registry lives on the tag's database row as `milestones`,
  holding the §1 registry value byte-faithfully. Registry mutations are
  §8.2 immediate-transaction mutations. They touch no task
  revision and are not CAS-fenced: the registry carries no revision, and
  the transaction serializes its writers.
- Each accepted registry mutation (`add`, `close`, `reopen`, `remove`)
  appends one content-free event: kind `milestone`, `count` 1, the tag,
  and a timestamp — never a name, per §8.4. A `remove --force` that
  clears task fields appends, in the same transaction, one `update`
  event per cleared task carrying its new revision.
- `set-milestone` follows §8.2 exactly: expected `--revision`, revision
  increment, `update` event, and the typed stale-write conflict (exit 3)
  on mismatch.
- §8.6 migration copies each tag's registry from the legacy store into
  the database wholesale; the registry has no per-entry merge.
- §9.2 plan apply appends its new registry entries and task assignments in
  the same transaction as the plan receipt; a conflicting name rejects the
  whole plan.
- The registry is part of the initial §8.2 schema; this section adds no
  later schema change, and §8.7's `user_version` fencing is the only
  mixed-version mechanism.

### 12.4 Quality bar additions

Per §11, `go test ./...` additionally covers: registry `add`, `list`,
`close`, `reopen`, and `remove` happy paths and every refusal (duplicate
name, invalid grammar, unknown name, remove with and without `--force`);
`set-milestone`/`--clear` touching only the target task, its `updatedAt`,
and per-write metadata;
`next`'s closed-milestone exclusion and the `--milestone` filter on `next`
and `list`; unknown-name usage refusals; `verify` findings for duplicate
names, invalid entry shape or status, and unresolvable task references;
registry preservation through §8.6 migration; content-free `milestone`
events;
`set-milestone` CAS including the stale-revision exit-3 conflict; and
`remove --force` clearing records with revision increments and `update`
events.
