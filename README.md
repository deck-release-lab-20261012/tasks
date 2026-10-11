# noise tasks

A standalone task-backlog CLI: PRD in, dependency-ordered tasks out,
tracked in one SQLite database shared by every checkout of a repository.

**Status: experimental.** Daily-driven by its author; interfaces may change
between releases. See [DEVELOPMENT.md](DEVELOPMENT.md) for the development
model (curated release mirror, PRs disabled, issues open).

Part of the [Noisefloor](https://noisefloor.sh) suite — infrastructure for
running fleets of third-party coding agents against real repositories. Each
tool works standalone; none requires the others.

## What it does

You write a product requirements document; `noise tasks generate` turns it
into a dependency-ordered backlog through a configured LLM provider, and
the CLI tracks that backlog — statuses, priorities, dependencies, subtasks,
and milestones — as ordinary data you can list, query, and change from a
terminal or a script. The canonical commands are `noise tasks <command>`
and the standalone `noise-tasks <command>`; both have the same behavior.

Task state lives in one SQLite database derived from the repository's Git
common directory, so the primary checkout and every linked worktree resolve
the same backlog regardless of branch; no file inside a worktree is ever
task state. Concurrent workers are fenced, not trusted: tasks are claimed
under expiring leases with fencing tokens, every mutation of an existing
task is compare-and-swap on its revision, and claims, runs, plan
applications, and worktree operations commit immutable receipts into the
same database. A content-free change feed, versioned backups with a one-way
cutover from the retired JSON layouts, atomic orchestration plans, and
task-bound Git worktrees complete the engine. Machine consumers use the
`--json` surfaces and the versioned `context --json` contract rather than
reproducing discovery.

What it deliberately does not do: no daemon, no TUI, no network listener,
no telemetry, no multi-repository sync or server, and no AI rewriting of
existing tasks. It does not choose models, supervise agent processes, judge
output quality, or merge branches — deciding what to do with a prepared
task belongs to a trusted orchestrator (Noisefloor Deck is one such
consumer); `noise-tasks` commits, fences, records, and proves. The only
networked command is `generate`.

## Install

From source. Requires the Go version declared in `go.mod` or newer. The build
is pure Go, including the SQLite driver, so no C toolchain or system
libraries are needed:

```sh
git clone https://github.com/noisefloor-oss/tasks.git
cd tasks
git checkout vX.Y.Z           # the release tag
make build VERSION=X.Y.Z      # the same version, without the leading v
```

The build produces the standalone engine `build/bin/noise-tasks`, the
`noise` umbrella host and the transition-only `noisetask` shim in
`build/bin/`, and the plugin payloads under `build/libexec/`. The
standalone binary covers everything in the quick start; copy it onto your
`PATH` and verify:

```sh
install -m 0755 build/bin/noise-tasks ~/.local/bin/
noise-tasks version           # prints: noise-tasks X.Y.Z
```

A plain `make build` without `VERSION` embeds the development version
`dev`. `make package` assembles the full system installation — umbrella
host, managed plugin root, and the setuid worktree broker used by sandboxed
orchestration — and requires root by design; see
[docs/PACKAGING.md](docs/PACKAGING.md).

Release artifacts are published on GitHub Releases with SHA-256 checksums.
Verify downloads against the checksums file attached to the release.

## Quick start

Start inside a Git repository (outside Git the current directory becomes
the project boundary and the database lives at `.tasks/tasks.sqlite`).
Onboarding records one generation provider in `.tasks/preferences.json` —
an adapter and a model, never credentials — and creates the database:

```sh
git init greeter && cd greeter
export ANTHROPIC_API_KEY=...            # for the anthropic-api adapter
noise-tasks setup --adapter anthropic-api --model claude-sonnet-4-5 --yes
```

The compiled-in adapters are `anthropic-api` (in-process HTTPS; reads
`ANTHROPIC_API_KEY` at invocation time) and the `claude-cli` and `omp`
harness adapters, which run the fixed `claude` and `omp` executables from
your `PATH`. Rerun `setup` to change the choice. `noise-tasks init` is the
task-only variant: it creates the database with an empty `main` tag and no
generation preferences.

Write a small PRD at the conventional path and generate a first backlog
(`generate` is the only networked command; without `--yes` it previews the
drafts and asks before appending):

```sh
mkdir -p .tasks/docs
cat > .tasks/docs/PRD.md <<'EOF'
# Greeter CLI PRD

A tiny command-line greeter.

## Requirements
- A `greet` command that prints "Hello, <name>!" for a required --name flag.
- A friendly default of "world" when --name is omitted.
- Unit tests for the greeting formatting.

## Non-goals
- No configuration file, no internationalization, no daemon.
EOF

noise-tasks generate --yes .tasks/docs/PRD.md
```

```text
3 task(s) drafted from .tasks/docs/PRD.md for tag "main":

   1. Create the project skeleton  [high]  deps: -
   2. Implement the greet command  [medium]  deps: 1
   3. Add unit tests  [medium]  deps: 2
appended 3 task(s) to tag "main":
```

Then work the backlog:

```sh
noise-tasks list
noise-tasks next        # highest-priority pending task whose dependencies are done
noise-tasks show 1
```

Claims fence concurrent workers. Acquiring one returns a fencing token;
mutating a claimed task requires its owner and token, and every live
mutation passes the expected `--revision` (compare-and-swap: a stale
revision exits 3 — re-read with `show --json` and reconcile):

```sh
noise-tasks claim --owner you --request-id demo.claim.1 1
# token=1 expiresAt=2026-10-03T08:54:08Z revision=1

noise-tasks set-status --owner you --token 1 --revision 1 1 in-progress
noise-tasks set-status --owner you --token 1 --revision 2 1 done
noise-tasks next        # the dependent task is now ready
```

`noise-tasks release --owner you --token 1 1` drops a claim; setting status
`done` clears it. `noise-tasks --help` lists every command, and
`noise-tasks <command> -help` shows its flags.
[docs/SPEC.md](docs/SPEC.md) is the behavioral contract;
[docs/WORKTREES.md](docs/WORKTREES.md) covers task-bound worktree
discipline and [docs/MIGRATION.md](docs/MIGRATION.md) the one-way cutover
from the retired JSON store.

## Compatibility

The tool is experimental; only the commitments below are stable. Each is
defined normatively in [docs/SPEC.md](docs/SPEC.md):

- **Context schema (SPEC §2.2).** `noise-tasks context --json` emits one
  JSON object with every field present, currently `schemaVersion: 5`.
  Consumers treat an unknown schema version, a missing field, or an extra
  field as an unsupported contract and fail closed.
- **Exit codes (SPEC §6).** 0 success; 1 operational failure (including an
  uninitialized project or a corrupt or newer store); 2 usage error; 3
  typed conflict — a stale revision, a claim or fencing conflict, or a
  request-id replay mismatch. `verify` keeps its own 0/1/2 contract
  (1 = findings). The `noise` host adds 126 (unsafe plugin or exec failure)
  and 127 (plugin not installed).
- **Store schema (SPEC §8.2).** The binary owns the SQLite schema, recorded
  in the database's `PRAGMA user_version` (currently 5). An older store
  upgrades transactionally at its next mutation; a store newer than the
  binary refuses to open, fail-closed. The JSON task model of SPEC §1 is
  the stable exchange format used by the non-authoritative `--file`
  fixture surface, by backups, and by migration.
- **Managed compatibility window (SPEC §7).** The `noisetask` shim, the
  `parse-prd` alias for `generate`, and the legacy provider-selector
  environment variables remain during the window; their removal is
  evidence-gated and will be announced as a breaking release, never tied to
  elapsed releases.

Everything else — flags, output shapes, file layouts — may change between
releases without a deprecation cycle.

## Uninstall / data retention

Remove the installed binaries (`noise-tasks`; for a full package install
also `noise`, `noisetask`, and the libexec tree — see
[docs/PACKAGING.md](docs/PACKAGING.md)). The only data the tool ever
writes, per project, is:

- inside Git, `<git-common-dir>/noise-tasks/` — `tasks.sqlite`, the task
  authority, plus its lock and WAL sidecars (typically
  `.git/noise-tasks/` under the primary checkout);
- outside Git, `<project>/.tasks/tasks.sqlite`;
- at the project boundary, `.tasks/preferences.json` (adapter and model; no
  credentials) and anything you added under `.tasks/docs/`;
- task-bound workspaces under `<project>/.worktrees/<tag>-<id>` and their
  retained branches `tasks/<tag>/<id>`;
- any `*.ntbackup` bundles you created with `backup create`.

Delete those and nothing of the tool remains. There is no telemetry; the
only data that ever leaves the machine is PRD text sent to your configured
provider by `generate`.

## Security

The engine reads and writes only its own task state. The preferences file
is a closed schema: it can name a compiled-in adapter and a model, never a
path, executable, argument, environment value, or credential. CLI adapters
invoke fixed basenames resolved from the invoking user's `PATH`, refuse
executables contained by the project boundary, run without a shell, and
never copy child stderr or authentication output into diagnostics or
project files. Claim, run, and saga tokens are fencing values, not
authentication, and appear only in acquisition output. The umbrella host
and the worktree broker add an installation trust boundary defined in SPEC
§5 and §9.5.

Report vulnerabilities privately via GitHub Security Advisories; see
[SECURITY.md](SECURITY.md).

## License

MIT © 2026 Doug Doan. Built at [Noisefloor](https://noisefloor.sh). See
[LICENSE](LICENSE).
