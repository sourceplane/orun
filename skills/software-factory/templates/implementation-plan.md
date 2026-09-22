# <reponame>-<slug> — implementation plan

Milestones land in order. Each is one or more tasks, each task one pull
request, each pull request landed with `orun pr land`. A milestone is marked
✅ here when its "done when" list is true, and recorded in
`IMPLEMENTATION-STATUS.md`.

## <CODE>0 — the spec

This doc set, merged to `main` and attached to the epic with `orun spec push`.

**Done when**
- the five documents are on `main`
- `orun spec list --epic <reponame>-<slug>` shows them

## <CODE>1 — <name>

<What this milestone changes, in a paragraph. Which worker, which package,
which migration.>

**Done when**
- <observable fact: a route answers, a migration applied on stage and prod, a test lane green>
- <observable fact>

## <CODE>2 — <name>

<…>

**Done when**
- <…>

## Sequencing note

<Why this order: what <CODE>2 needs from <CODE>1, what can run in parallel,
what is gated on a provider or a decision.>
