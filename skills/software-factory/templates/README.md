# Epic: <reponame>-<slug> (<CODE>)

**<The thesis. One paragraph, in bold: what is wrong or missing today, what this
epic makes true, and the one design idea that makes it possible.>**

<One or two plain paragraphs expanding the thesis: who this is for, what they
can do when it ships that they cannot do now.>

## Status

| Field | Value |
|-------|-------|
| Status | Draft |
| Cluster | **<CODE>** (<CODE>0–<CODE>n) |
| Owner(s) | `apps/<worker>` (the resource) · `packages/contracts` + `packages/sdk` (the wire) · `apps/web-console-next` (the surface) |
| Builds on | `<baseline> baseline-vN` — <which bounded contexts it extends> |
| Changes | <one sentence: what is added, what is untouched> |
| Decisions locked | (1) <decision>. (2) <decision>. (3) <decision>. |
| Gate | <CODE>1 is invisible (the resource). <CODE>2 is the first user-visible change. |
| Shipped as | |

## Read order

1. `design.md` — the resource, the routes, the surfaces, what is out of scope
2. `implementation-plan.md` — the milestones and what "done" means for each
3. `risks-and-open-questions.md` — what could go wrong and what was decided
4. `IMPLEMENTATION-STATUS.md` — what actually shipped (kept distinct from intent)

## Milestones at a glance

| Milestone | What it lands | Done when |
|---|---|---|
| <CODE>0 — the spec | this doc set | merged and pushed with `orun spec push` |
| <CODE>1 — <name> | <what> | <observable fact> |
| <CODE>2 — <name> | <what> | <observable fact> |
