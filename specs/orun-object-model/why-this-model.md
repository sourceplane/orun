# Why This Model

> Audience: the Orun team and anyone reviewing the rewrite. This is the
> argument, not the spec. If you only read one document, read this one.

## The problem we actually have

Orun has accreted three state layouts in three phases:

1. **Phase 1** gave us `trigger → revision → execution` under a flat
   `revisions/<key>/…` tree with refs and indexes. Good bones.
2. **Phase 2** wrapped it with `source → catalog → component` — but to stay
   "additive and reversible," it **copies bytes**: the same plan/trigger/
   revision/manifest are written once under the global layout and again under
   `sources/<src>/catalogs/<cat>/revisions/<rev>/`. Two homes for the same
   data.
3. The **runner** never moved. It still writes the original `internal/state`
   `state.json` and a bridge *mirrors* two files into the new tree. So a third
   representation of execution state exists, and `internal/state` is still the
   source of truth for anything in-flight.

The result: **the same fact can live in three places, the layout is the model,
and the model is the layout.** Every new capability (catalog, SaaS, TUI) either
forks a code path or retrofits an abstraction. Disk grows linearly in
`#revisions × catalog_size` because shared catalogs are copied, not referenced.
And we cannot delete the legacy module because too much still depends on its
types and its on-disk shape.

We have, in other words, the exact problem git was invented to solve: **we are
storing content by location instead of by identity.**

## The insight

> Good programmers worry about data structures. Get the object graph right and
> the code — CLI, runner, TUI, SaaS — falls out as projections of it.

There are only two kinds of state in Orun:

- **Content.** A source tree, a resolved catalog, a component manifest, a
  compiled plan, a finished execution. These are *values*. Two byte-identical
  values are the same thing. They never change after they are written.
- **Events / pointers.** "A trigger fired at 10:04 and produced revision X."
  "`main` currently points at catalog Y." "Execution Z is running." These are
  *names and happenings*. They change; they accumulate.

Git made one decision and everything else followed: **store content by the hash
of its bytes; store names as pointers to hashes.** A blob is content. A tree is
content (a list of name→hash). A commit is content (a snapshot pointer + parents
+ message). A branch is a *ref* — a mutable name pointing at a commit hash.
Identical files across a thousand commits are stored once. History is cheap.
Integrity is automatic (the hash *is* the content). Sync is "send the objects
you don't have."

Orun's lineage maps onto this perfectly, and we already half-built it without
admitting it: our keys are *already* content-derived (`src-…-t<treeHash>`,
`cat-…` from `catalogHash`, `rev-…-p<planHash>`). We compute the hashes and then
throw away the dedup by copying bytes into named directories. **We pay for
content addressing and don't collect the dividend.**

## What the new model is

```
content (immutable, hashed, dedup'd, GC'd):  source ◄ catalog ◄ revision
pointers (mutable, CAS'd, GC roots):          refs/sources, refs/catalogs, refs/…
events (append-only, point at content):       triggers, executions
derived (rebuildable caches):                  indexes, component history, working view
```

- **A catalog is stored once.** A thousand revisions off `main` reference the
  same catalog tree by hash. Editing application code that isn't
  catalog-relevant doesn't even change the catalog hash (our dirty-hash is
  already scoped to catalog-relevant files).
- **A revision is stored once.** Two triggers — a manual run and a PR check —
  that compile the *same plan* against the *same catalog* share one revision
  object. The two triggers are two small event records pointing at it. The plan
  is one blob, not two copies.
- **An execution is an event that becomes content.** While it runs, it is a
  mutable working tree (job/step status changing). When it terminates, it is
  *sealed* into immutable objects — exactly git's working-tree → commit. This
  is how the runner finally writes the native `job → attempt → step` levels and
  how `internal/state` and the bridge **die with no workaround**.

## What it buys us

| Benefit | Mechanism |
|---|---|
| **Disk shrinks, often dramatically** | Dedup (one catalog, one plan, one log object shared everywhere) + zstd + reachability GC. Replaces linear copy growth. |
| **Legacy deletes cleanly** | Working-tree/seal gives the runner a native home for live + finished state. No bridge, no dual-write, no `internal/state`. |
| **Integrity for free** | An object's name is the hash of its content. `orun fsck` is a hash check. Corruption is detectable, not silent. |
| **Provenance is total and cheap** | Every execution transitively names its revision, catalog, and source by hash. "What git state produced this run?" is one pointer chase. |
| **SaaS sync is trivial and correct** | Content addressing ⇒ the same object has the same name everywhere. Remote sync = "push objects the remote lacks, move the remote ref." Org-wide dedup is automatic. This is git push / Nix substitution, not a bespoke replication protocol. |
| **TUI and SaaS share one model** | Both read refs + objects (locally, or via remote substitution) and start runs by creating the same trigger/execution objects. No second data model. |
| **One code path** | The current best-effort/fallback/dual-write fork collapses into a single tolerant-strict walk. |

## What it costs (and how we pay it)

We are not selling magic. The honest costs:

- **Inspectability.** Raw content-addressed objects are opaque; you cannot
  `cat .orun/objects/sha256/9f/86…` and read JSON (it's zstd-compressed, and
  named by hash). **Payment:** a materialized **working view** (`.orun/current/`)
  for the hot set — current source+catalog and active/recent executions — plus
  porcelain (`orun cat`, `orun show`, `orun log`, `orun ls-tree`). Git is 10%
  object store and 90% porcelain; we budget for the porcelain. This is why we
  chose the *hybrid* model, not pure CAS.
- **We are building a small piece of git.** **Payment:** we deliberately build
  the *simple* piece — loose objects + zstd + reachability GC. We **do not**
  hand-roll packfiles or delta chains in v1; zstd + dedup captures most of the
  win at a fraction of the complexity. Packing is a later, optional milestone
  gated on real profiling data.
- **Live-execution liveness needs care.** A running execution is mutable state
  that must survive a crash. **Payment:** the working tree is the mutable
  surface; the *seal* is the atomic publish point (write all objects, then move
  one ref — partial objects are inert GC fodder, exactly git's model).
- **A real migration.** **Payment:** a one-shot, additive `orun migrate` that
  ingests the legacy `.orun/` into objects and never deletes the old tree until
  the user opts in.

## Why now, and why a rewrite rather than a fourth layer

A fourth additive layer would add a fourth copy of the truth. The whole reason
disk grows and legacy can't die is *additive layering*. The model only pays off
when it is **the** model — when the runner writes it natively, when reads
resolve through it, and when the old shapes are gone. That requires a rewrite,
staged behind a flag so it stays reviewable and reversible until it's proven on
real executions.

The bet: a few weeks of disciplined data-structure work removes three
overlapping state systems, shrinks disk, and turns "build the SaaS sync" from a
project into a push/pull of content-addressed objects. That is the trade git
made, and Nix made, and it is the right trade here.
