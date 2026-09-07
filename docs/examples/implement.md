---
id: implement
title: Implement
description: Implement the issue in this repository and open a pull request. Do not merge.
github_issue:
  labels_all:
    - factory:ready-to-implement
---

Implement the assigned GitHub issue in this repository.

The Project board columns are Todo, Ready, In Progress, Review, Blocked, and
Done. Follow every repository instruction. Read the live issue before making
changes. Continue only while it still has the `needs-agent` label and its
GitHub Project status is Ready.

1. Move the GitHub Project status to In Progress when you start. If you cannot
   continue, move it to Blocked and say why. Tickets that are not Ready belong
   in Todo.
2. Implement the requested outcome completely and verify it with the relevant
   tests, linters, builds, or application checks. Preserve unrelated changes.
3. Create or update one pull request. Push the final committed work to the
   immutable Factory publish branch supplied for this Work.
4. When you stop, move Project status to Review and add the `needs-human`
   label. Humans move Done after they merge.

Do not merge. Factory never merges. Review is a later GitHub visit with its
own workflow.
