# Spec: orun-work-v4 (the planning hierarchy — orun half)

**Work gains a shape: Initiative → Design → Epic → Milestone → Task. v2
(`specs/orun-work/`) made delivery lifecycle a derived query nobody can lie
into; v3 (orun-cloud `specs/epics/orun-work-v3/`) made intent fast to author;
v4 makes intent itself governable — designs are first-class artifacts humans
and agents produce against sealed context, epics carry an authored,
human-only, content-addressed approval that seals the brief an agent
implements from, milestones promote the repo's own `implementation-plan.md`
ladder convention (WP0→WP5, PM0→PM5, WH0→WH6) into a primitive, and tasks
stay the regenerable v2 atom. One rule governs the whole evolution: split by
truth source. Review, approval, and adoption are decisions — authored,
attributed coordination events, exactly as v2 treats Canceled. Progress,
drift, and health are facts — folds nobody can edit.**

> **The authoritative epic lives in orun-cloud**
> (`specs/epics/orun-work-v4/`: README, design, implementation plan, risks —
> cluster **WH**, milestones WH0→WH6). This folder is the orun half: what
> this repo owns and must hold true. v4 is purely additive over the shipped
> v2 surfaces in this repo — the fold, the vocabularies' existing members,
> `orun work import/list`, sealing, `orun spec pull`, and the work MCP all
> keep working byte-for-byte.

## Status

| Field | Value |
|-------|-------|
| Status | **In progress — WH0–WH6 orun legs shipped** (#489 oracle · #490 EpicSnapshot + `orun epic pull` · #491 MCP surface 9→15 · WH6 import v4 + fold budget ~1.4 ms on the real corpus); the live dogfood import remains |
| Builds on | `specs/orun-work/` (v2, shipped WP0–WP5): `internal/worklens` (the conformance oracle), the two-log model, the contract, sealing + `orun spec pull`, `orun work import`, `orun mcp serve` |
| Coordinates with | orun-cloud `specs/epics/orun-work-v4/` (authoritative; schema/API/console), `specs/orun-agents/` (AG8 design runs land as Design drafts; AG9 dispatch consumes the sealed brief + preconditions) |
| apiVersion | `orun.io/v1` (adds `Design`, milestone sub-items, `EpicSnapshot`; `Spec` gains the surface name **Epic** — wire kind unchanged) |
| Decisions locked | Mirrors the cloud epic's V4-A…V4-F; restated below only where this repo enforces them |
| Milestone prefix | **WH** (this repo's legs land inside WH0/WH4/WH5/WH6) |

## What this repo owns

1. **The oracle grows (WH0), the fold does not.** `internal/worklens` gains:
   the `design` item kind and `<epic>#<key>` milestone subjects; the 8 new
   coordination kinds (`milestone_edited`, `milestone_set`,
   `review_requested`, `review_submitted`, `approved`, `approval_revoked`,
   `design_adopted`, `superseded`) with write-time validation — closed set,
   mandatory actor, **`approved`/`approval_revoked`/adoption human-only**
   (the agent-pin guard, extended); the **intent-ladder fold**
   (Draft / In Review / Approved / ApprovedDrifted / Adopted / Superseded —
   coordination events only); `ladderHash` (canonical digest of the
   milestone set — keys, titles, goals, doneWhen, order); and the **rollup
   folds** (milestone progress, epic execution, initiative health with
   named evidence, pin-beside-health with auto-expiry). The v2 delivery
   fold, claim join, and observation vocabulary are untouched; the v2
   conformance fixtures pass byte-identical, and new fixtures cover the
   intent ladder, drift, adoption, and health — replayed by the cloud TS
   fold (the established oracle pattern).
2. **Sealing: `EpicSnapshot` ⊇ `SpecSnapshot` (WH4).** Canonical JSON, same
   framing, one canonicalizer: the spec envelope + resolved doc, the
   milestone ladder + `ladderHash`, informative task envelopes (context,
   not approved scope — task churn never drifts approval), the adopted
   design revision when minted from one, the approval record, and the
   catalog/log cursors. Refs `refs/work/epics/<slug>/latest` beside the
   existing spec refs. **`orun epic pull <slug>[@sha256:…]`** is the strict
   superset of `orun spec pull` (which remains and prints a pointer);
   pin verification, read-only views, `--push` — unchanged mechanics.
3. **The MCP grows, the guardrails extend (WH5).** Reads:
   `initiative_get`, `design_get`, `milestone_get`, `epic_brief` (the
   sealed EpicSnapshot; `spec_get` remains). Writes: `design_propose`
   (create/revise a Draft design + structured proposal, applied AND flagged
   for human review — the `contract_propose` discipline), `task_create`
   gains `milestone`, `task_regenerate` (batch cancel+create under one
   milestone, one verdict, every contract flagged). **No status, pin,
   approve, or adopt tool exists** — the forbidden-tool sweep extends,
   asserted by test; the cloud mutators reject agent approval server-side
   regardless (defense in depth, both layers, as with pins).
4. **Import learns the hierarchy it already reads (WH6).**
   `orun work import`: epic folders → epics; `implementation-plan.md`
   `## <KEY> — <Title>` headings → **milestones** (Goal/Done-when/Deps →
   milestone contract); checklist items under a heading → tasks in that
   milestone, with one task per milestone materialized where none exist
   (v2's mapping preserved 1:1 under the new level); roadmap/epic-index
   clusters → initiatives. Idempotent; key-preserving migration for
   task-per-milestone corpora; `--dry-run` plans; golden fixtures over this
   repo's real `specs/` tree (P-4 round-trip discipline). **No lifecycle
   and no approvals are imported** — import writes intent, never decisions.
5. **CLI read surface.** `orun work list --tree` renders the hierarchy with
   both chips per epic (intent state @revision · execution counts);
   evidence-bearing output as today.

## Invariants this repo enforces (beyond v2's, which all stand)

1. The delivery fold and observation vocabulary are frozen in this epic;
   any diff touching `fold.go`'s lifecycle logic or the 6 observation kinds
   is a rejected PR (V4-1).
2. `approved` events validate actor `user` in the model — an agent approval
   is unrepresentable at write time here, before the cloud even answers.
3. Approval is never rendered without its revision; `ApprovedDrifted`
   carries both digests (V4-2/V4-3).
4. Seal determinism extends to `EpicSnapshot` and `ladderHash`: identical
   inputs ⇒ identical ObjectID, property-tested.
5. Import writes intent only; a fixture asserts no `review_*`, `approved`,
   or `design_adopted` event is ever emitted `via: import`.

## Read order

1. orun-cloud `specs/epics/orun-work-v4/README.md` — the five nouns, the two
   ladders, the honest-gesture table, invariants, milestones.
2. orun-cloud `specs/epics/orun-work-v4/design.md` — model deltas, schema,
   approval semantics, API, console IA, agent binding, import mapping.
3. This file — the orun-half ownership and its enforcement points.
4. `specs/orun-work/` (v2) — the substrate this never breaks.
