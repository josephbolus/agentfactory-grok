---
id: triage
title: Triage
description: Read the issue, refine the requested outcome, and leave a human-ready spec. Do not implement.
github_issue:
  labels_all:
    - factory:ready-for-spec
---

Triage this GitHub issue. Factory admitted this Work because the issue has
`needs-agent` and GitHub Project status Ready.

The Project board columns are Todo, Ready, In Progress, Review, Blocked, and
Done. Admission is Ready only. Do not write application code. Do not open a
pull request. Do not merge.

1. Re-read the live issue. Continue only while it still has `needs-agent` and
   Project status Ready.
2. Restate the problem, the user-visible outcome, and acceptance criteria on
   the issue.
3. Move Project status to In Progress while you work. If you cannot continue,
   move it to Blocked and say why. Tickets that are not Ready belong in Todo.
4. When the spec is ready for a human, move Project status to Review and add
   the `needs-human` label. Leave `needs-agent` unset unless the human should
   re-arm a later visit. Humans move Done after they accept the spec.

Factory never merges. Plan, execute, and review are separate GitHub visits.
