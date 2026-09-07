# Agent Factory architecture

> **Status:** Current implementation
>
> **Verification basis:** implementation and tests in this repository

## Executive summary

Agent Factory is a local-first, label-driven software factory. A GitHub issue
admits Work only when the `needs-agent` label is set **and** its GitHub Project
status is **Ready**. `labels_all` on a repository workflow selects one frozen
`.factory/workflows` markdown file. One matching visit is one Work. Leaving the
matching label set rearms; a later matching visit is new Work. Plan, execute,
and review are separate GitHub visits, not a pipeline. After the agent, Project
status moves to In Progress then Review and the issue is labeled `needs-human`.
Factory never merges.

Saved Tasks and Procedures remain the operator door: create, schedule, and
`factory run PROCEDURE` still admit Work. Each admitted Work still carries
exactly one frozen workflow snapshot (id, path, SHA, digest, body), not an
ordered stage list. Workers advertise Pi, Codex, and Claude Code. Remote VM
Workers use the existing TLS listener; that contract is frozen.

Cloud Run, `fake_cloud`, `cloud-run-*` synthetic workers, Pipelines-as-SDLC,
and the `agent_update` protocol are removed. They are not current behavior.

The implementation has four main parts:

- `factory` is the operator CLI. Long-running commands replace themselves with
  the compatible server or Worker executable. Finite commands read the loopback
  HTTP API and never open SQLite or Worker directories.
- `factory-server` owns durable state, scheduling, routing, GitHub intake, the
  HTTP API, and the embedded browser UI.
- `factory-worker` owns runtime health, repository caches, worktrees, a single
  agent process per Work, live re-check of `needs-agent` and Project Ready
  before the agent starts, and cleanup or retention.
- SQLite stores Tasks, Runs, Session-backed Work, executions, Attempts, events,
  Workers, repositories, workflow catalogs, and GitHub issue visits.

The operator API is loopback-only. Workers make outbound polling requests to
the server. Remote VM Workers use a separate TLS listener and per-Worker bearer
credential. No server connection into a Worker host is required.

### System architecture

```text
Operator browser or `factory` CLI
      |
      | loopback HTTP and JSON
      v
factory-server
  |-- GitHub intake (needs-agent AND Project Ready)
  |-- Task scheduler and Procedure fleet admission
  |-- routing and lease state machine
  |-- embedded React UI
  `-- SQLite
      ^
      | register, claim, heartbeat, events, complete
      | local HTTP or separate authenticated TLS
      |
factory-worker
  |-- stable identity and N slots
  |-- runtime capability probes (pi, codex, claude-code)
  |-- live re-check of needs-agent and Project Ready
  |-- bounded repository cache
  |-- isolated worktrees and manifests
  `-- one Pi, Codex, or Claude Code process per Work
```

### Dependency hierarchy

```text
cmd/factory         -> internal/factorycli   -> internal/protocol
cmd/factory-server  -> internal/controlplane -> internal/protocol
                    -> web
cmd/factory-worker  -> internal/worker       -> internal/protocol
```

Entry points depend on their runtime package. Both long-running runtime
packages share only protocol types. Worker code must never import
`internal/controlplane`. Makefile is the only driver; there is no Justfile.

## Kernel

```text
GitHub issue + needs-agent + Project Ready
    -> poll / admit once per visit
    -> labels_all matches one repository workflow
    -> freeze markdown + SHA + digest + body
    -> one sandboxed agent Work
    -> worker re-checks needs-agent and Ready before the agent starts
    -> Project In Progress then Review, label needs-human
    -> human merges. Factory never does.
```

Plan, execute, and review are **separate label visits**. Sequence lives on the
ticket. Default repository workflows (triage, implement, review) live under
`docs/examples` and declare `labels_all`. Review never merges.

### Admission

A managed repository may opt into GitHub issue intake. The poller lists open
issues with `needs-agent`. An issue becomes Work only when:

1. the `needs-agent` label is still present, and
2. GitHub Project status is `Ready`.

Missing either condition does not admit Work. `labels_all` selects one workflow
from the repository catalog (case-insensitive, every declared label present).
Zero matches fall back to `implement`. Ambiguous matches block and record a
diagnostic. Admission freezes workflow id, path, commit SHA, digest, and the
markdown body onto the Run Task snapshot and the Work snapshot.

The visit key is per issue and matching workflow. A second poll while the
matching set stays present does not create another Work. Removing `needs-agent`
or leaving Ready closes the visit. Returning with both present creates new Work
(leave-label rearm).

### Worker re-check

Before the agent process starts, the Worker re-reads the live GitHub issue.
Work proceeds only while `needs-agent` is set and Project status is Ready.
Stale claims fail closed without starting the runtime.

After the agent, Factory does not merge. The issue is labeled `needs-human`.
Project status moves to In Progress while the agent runs, then Review when the
attempt finishes. Workflows instruct those column moves; humans merge.

### One workflow per Work

Work stores one `RepositoryWorkflowSnapshot`:

- `id`
- `path`
- `commit_sha`
- `digest`
- `instructions` (the markdown body)

There is no ordered pipeline stage list on the Work hot path. GitHub intake
freezes the matched repository workflow. Task and Procedure admission freeze
the saved prompt as a single workflow snapshot (or the selected repository
workflow when one is named). The Worker runs one agent process for that
snapshot.

### Tasks and Procedures

A Task is the stored form of a saved Procedure: name, input prompt, runtime
(`pi`, `codex`, or `claude-code`), timeout, concurrency, managed repositories,
optional cron schedule, and generation. `factory run PROCEDURE` admits one
Work per selected repository. Scheduled admission polls every ten seconds.
Each resulting Work still has exactly one workflow snapshot.

`factory build` admits GitHub issue URLs or opaque references as Work using
the same one-workflow freeze.

### Execution and Attempt

An Execution is the durable assignment of one Session to one Worker and runtime.
An Attempt is one leased try of that Execution. An Attempt begins in
`preparing`, moves to `running` after the Worker reports its supervisor
identity, then ends as `succeeded`, `failed`, `cancelled`, or `lost`. Outcome
is process exit. There is no `agent_update` semantic status protocol.

### Worker and repository

A Worker has one durable ID, display name, labels, capacity, health, runtime
capabilities, source access, repository advertisements, and retained-worktree
inventory. One Worker can advertise several runtimes (`pi`, `codex`,
`claude-code`) and run 1 to 100 Attempts, with ten slots by default.

There are no synthetic `cloud-run-*` Workers and no fake Cloud Run dispatcher.

The control plane owns a catalog of managed GitHub repositories. Eligible
Workers clone them on demand with `gh`, keep at most 100 cache entries, fetch
before an Attempt, and resolve the current base branch and commit.

Remote VM Workers require TLS, one-time enrollment bound to a stable Worker ID,
and a stored bearer credential. That TLS path is frozen and is not expanded.

## Architectural invariants

1. SQLite and the control plane are the authority for Run and Attempt state.
2. A GitHub issue admits Work only for `needs-agent` **and** Project Ready.
3. `labels_all` selects one frozen workflow; ambiguous matches block; zero
   matches use `implement`.
4. One visit is one Work. Leaving the matching label set rearms.
5. A Work stores one workflow snapshot, not a stage list.
6. The Worker re-checks `needs-agent` and Project Ready before starting the
   agent. Stale work does not run.
7. After the agent: Project In Progress then Review, label `needs-human`.
   Factory never merges.
8. A claim is assigned only to its selected, healthy, online Worker with a
   ready runtime, free capacity, and repository availability.
9. A random lease token owns one active Attempt. The server stores its digest,
   not the token.
10. Every runtime starts in a Worker-owned worktree. Cleanup fails closed.
11. Dirty, failed, cancelled, lost, unpublished, or uncertain worktrees are
    retained. Clean unchanged or proved-published work may be removed.
12. Plain HTTP accepts loopback clients only. Remote Workers require TLS.
13. Worker code must never import `internal/controlplane`.
14. Operator builds embed committed `web/dist` assets and do not require Node
    at runtime.
15. Makefile is the only driver. Quality gates are `make format-check vet
    boundary staticcheck test-go` (and `make ui-check` when UI source changes).

## Components

### Operator CLI

`cmd/factory` delegates parsing and finite HTTP work to `internal/factorycli`.
The `build`, `run`, `procedures`, `status`, `show`, and `workers` commands use
typed protocol resources. They do not import SQLite or Worker packages.

`factory run PROCEDURE` selects explicit enabled managed repositories or all
enabled repositories. `factory build` admits issue URLs. There is no
`factory update` agent-status command and no pipeline-stage flags.

### Control plane

`cmd/factory-server` loads optional bootstrap TOML, opens SQLite, applies
embedded migrations, sweeps expired leases, starts the Task scheduler and
GitHub intake, serves the local API and UI, and optionally starts the remote
Worker TLS listener. Shutdown stops schedulers first and gives HTTP servers
ten seconds.

### Worker

`cmd/factory-worker` loads worker TOML, probes git and advertised runtimes,
registers, claims Work, prepares an isolated worktree, re-checks GitHub
admission, runs one agent process, streams events, and completes the Attempt.

### Operator UI

The embedded React UI lists Work, Tasks/Procedures, Workers, and repositories.
It uses Lucide icons and a 32px square schematic grid. It does not author
Pipelines as SDLC.

## Quality gates

- `make format-check`, `make vet`, `make boundary`, `make staticcheck`, and
  `make test-go` are the Go gates. They do not require `just`.
- `make ui-check` runs UI lint, typecheck, and component tests when UI source
  is shipped.
- `make build` produces `factory`, `factory-server`, and `factory-worker`
  from committed embedded UI assets without Node.js.
