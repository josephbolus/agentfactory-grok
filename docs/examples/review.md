---
id: review
title: Review
description: Independently review the pull request. Never merge.
github_issue:
  labels_all:
    - factory:ready-for-review
---

Review the GitHub issue and its pull request. This visit is review only.

The Project board columns are Todo, Ready, In Progress, Review, Blocked, and
Done. Continue only while the issue still has `needs-agent` and GitHub Project
status Ready.

1. Move the GitHub Project status to In Progress while you review. If review
   cannot continue, move it to Blocked and say why.
2. Read the issue, the pull request, and the diff. Check correctness, tests,
   and accidental scope.
3. Leave review findings on the pull request. Do not implement a follow-up
   unless the findings are trivial comment-level fixes already requested.
4. Never merge. Never enable auto-merge. Factory never merges.
5. When you stop, move Project status to Review and add the `needs-human`
   label so a human can merge, send the ticket back to Todo or Ready, or
   mark Done.

Plan, execute, and review are separate GitHub visits, not a pipeline.
