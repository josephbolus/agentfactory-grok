# Goal: label-driven software factory

This tree is a Makefile-driven, label-and-Project-Ready software factory.
`ARCHITECTURE.md` is the source of truth for current behavior.

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

Plan, execute, and review are **separate GitHub visits**, not a pipeline.
Sequence lives on the ticket. Default workflows under `docs/examples` are
triage, implement, and review. Each declares `labels_all`. Review never
merges.

Saved Tasks and Procedures remain the operator door: create, schedule, and
`factory run PROCEDURE` still admit Work. Each admitted Work still carries
exactly one frozen workflow snapshot. Workers advertise Pi, Codex, and
Claude Code.

## Keep

| Feature | Why |
|---|---|
| GitHub issue intake (`needs-agent` **and** Project Ready) | Factory door |
| `.factory/workflows/**/*.md` + `labels_all` | Repo-owned policy |
| Frozen workflow snapshot (id, path, SHA, digest, body) | Replay and audit |
| One visit = one Work; leave-label rearm | No double-start |
| Worker live re-check of `needs-agent` and Ready | Stale poll must not start work |
| Saved Tasks / Procedures, schedule, `factory run PROCEDURE` | Operator door |
| `factory-server` + `factory-worker` + SQLite | Durable local control plane |
| Loopback HTTP API, CLI reads API only | Process boundary |
| Worker worktrees, leases, heartbeats, retries | Execution kernel |
| Cleanup fail-closed; retain dirty/failed trees | Safety |
| `needs-human` / never merge | Shipping boundary |
| Pi + Codex + Claude Code | Real agents |
| Worker ↛ controlplane import boundary | `make boundary` |
| Makefile quality gates | No Justfile |

## Drop

| Feature | Why |
|---|---|
| Cloud Run fake + `cloud-run-*` synthetic workers | Speculative, not used |
| Pipelines as SDLC (1–20 stages sharing a worktree) | One Work is one frozen workflow |
| `agent_update` semantic status protocol | Outcome is process exit |
| Factory merge / auto-merge | Humans merge |

## Quality gates

```sh
make format-check
make vet
make boundary
make staticcheck
make test-go
make ui-check
```

Makefile is the only driver. There is no Justfile.
