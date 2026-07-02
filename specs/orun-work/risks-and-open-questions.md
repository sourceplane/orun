# Risks & Open Questions

> The living register for `orun-work` v2. Decisions are WP-n (locked in
> `README.md`/`design.md` §8); sharpness items P-n (`design.md` §11, expanded
> here); deferred items L-n; gaps G-n. The v1 registers are frozen in
> `specs/archive/orun-work-v1/`.

## Decision ledger (summary — full table in `design.md` §8)

WP-1 three planes by truth source · WP-2 two append-only logs, reads are
folds · WP-3 lifecycle is a derived query (pins render beside truth) ·
WP-4 two nouns, views for the rest · WP-5 work kinds in the shipped catalog
graph · WP-6 one mutator surface; ingesters are the only fact writers ·
WP-7 workspace-scoped tasks · WP-8 membership principals, no work identity ·
WP-9 log-native sync behind the verdict seam · WP-10 agents can't lie
(no lifecycle write surface, no pins).

## Open questions

**P-1 (fold performance).** Derive-at-read must stay cheap on a hot
workspace with years of log.
- Mitigations designed in: folds are per-subject and incremental (cursor
  caches, droppable by construction); list endpoints may keep a droppable
  lifecycle materialization written only by the fold.
- Resolution: WP1 records fold p95 over the imported dogfood corpus +
  backfilled history; budget locked before the console GA.

**P-2 (observation source contracts).** Three ingesters (webhooks, run
stream, overlay) each need a versioned fact contract; silent shape drift
would skew folds invisibly.
- Locked: `sourceVersion` on every observation; unknown shape ⇒ loud ingest
  failure + inbox item, never a best-effort parse.

**P-3 (gate name mapping).** `contract.gates` names ↔ orun run/check
identity, fixed in WP3 against real run data.
- The failure the fixture must encode: GitHub says green, orun has no
  record ⇒ the gate renders *unknown* and Done never derives. Sloppy
  mapping either lies or never fires; unknown-rendered-honestly is the only
  acceptable degradation.

**P-4 (import round-trip).** Spec docs must survive import → seal → pull
byte-identical (docRef content addressing makes this structural; golden
fixtures over this repo's own tree prove it).

**P-5 (pin semantics under races).** Pin placed while observations are in
flight: fold order is log order; a pin at seq n renders beside whatever
observed truth folds to at any later seq, and expires the moment observed ≥
pinned rung. Property-tested in WP1.

**P-6 (claim ambiguity).** Component-overlap claiming can match multiple
open tasks.
- Locked: ambiguous claims never link; they surface as inbox suggestions.
  Key-parse claims (`ORN-142` in branch/title) are always unambiguous and
  win.

**P-7 (backfill honesty).** WP0's history backfill derives lifecycle from
*reconstructed* observations (git/GitHub history), which may be incomplete
(e.g., no run records for old merges).
- Position: honest degradation — old tasks without gate records fold to
  Done only via explicit human pin or a `gates: []` contract; the demo
  narrative prefers "In Review (gates unknown)" over a flattering lie.

**P-8 (multi-repo `affects`).** A workspace spans repos; contracts may
reference components from multiple catalogs.
- v1 of v2: `affects` keys resolve against the workspace's catalogs as
  shipped by the catalog plane; `SpecSnapshot.catalog` pins the resolving
  snapshot. Cross-catalog closure for blast radius is deferred (L-3).

## Risk register

| Risk | Severity | Mitigation |
|---|---|---|
| Observation feeds lag the console (rungs stay dark) | High (product) | WP0 backfill makes history light up day one; WP2/WP3 feeds are the two bridge milestones, not afterthoughts; unknown renders as unknown |
| Fold cost surprises at scale | Medium | P-1 budget gate before GA; incremental per-subject folds; droppable materializations |
| Teams expect authored statuses ("just let me set Done") | Medium (adoption) | Pins are exactly that — public, attributed, auto-expiring; the UI makes the pin cheaper than the lie |
| resources-runtime slips and Released ships dark | Medium | WP3 fixture feed decouples the fold from the feed's arrival; Released lights up when the feed lands, no rework |
| Ingester duplication under webhook redelivery | Low | `dedupeKey` idempotency (invariant 4), replay fixtures in WP2 |

## Deferred register

- **L-1 (Agents section).** Dispatch-on-assign, fleet view — rails in
  WP0/WP4/WP5; its own epic.
- **L-2 (initiative/cycle entities + analytics).** Saved views first; the
  observation log already carries everything velocity/burnup need.
- **L-3 (cross-catalog blast radius).** P-8's closure across catalogs.
- **L-4 (external tracker adapters).** Linear/Jira import, after the native
  path proves out.
- **L-5 (cockpit work pane).** TUI over the same fold query API.
- **L-6 (CRDT doc editing).** Content-addressed docs + events until proven
  need.

## Gaps

- **G-1 (notification delivery).** Inbox items (drift, contract proposals,
  review suggestions) need channels; the logs are the source,
  `notifications-worker` is the delivery owner — contract to be drawn in
  WP2.
- **G-2 (reviewer suggestion depth).** Owner attribution in blast radius is
  gated on the `teams-ownership` owner→team resolver (draft); until it
  lands, blast radius names components without team routing.
