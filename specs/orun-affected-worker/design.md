# Design

> The competence boundary (the centerpiece), the fast-path/slow-path
> architecture, the per-event flow, base/index selection, reconciliation,
> fat-index/thin-worker, and drift control. The worker is a consumer; its entire
> correctness is defined by matching `orun catalog affected` on the shared
> conformance fixtures and by honoring the C-1…C-4 contract.

## 1. Problem & shape

A GitHub integration wants, per commit/PR, the affected component set — fast,
always-on, at org scale. The authoritative computation (a full Go resolve) is
too slow per-event. So:

```
PRODUCER / AUTHORITY (orun, specs/orun-catalog-state)        CONSUMER / ESTIMATOR (this worker)
──────────────────────────────────────────────             ──────────────────────────────────
full resolve on boundaries (plan, refresh, merge)            per GitHub event:
  → CatalogSnapshot + impact/ownership.json                    fetch index@base (R2, edge-cached)
  → pushed to remote object storage (objremote closure)        changed files → classify → closure
                                                               → {affected, confidence}  (ms)
        ▲                                                              │
        └──────────── reconciliation: full resolve gates ◄────────────┘
                      irreversible actions; overrules a wrong estimate
```

This is **speculative serving with authoritative reconciliation** (a CDN edge
cache with origin revalidation; an incremental checker with periodic full
checks). The worker can be wrong; it can never be *permanently* wrong, because
the authority always exists.

## 2. The competence boundary (RFC 2119 — the centerpiece)

The worker's safety is **entirely** these four rules. They are restated from
`orun-catalog-state/design.md` §5.2 because the worker is the highest-stakes
consumer (it can feed a CI skip).

- **C-1 (over-report).** On any classification uncertainty the worker **MUST**
  over-approximate `affected`, never under-approximate. A false *extra* component
  wastes CI; a false *missing* component ships a broken thing. The published
  index marks `structural`/`global` explicitly, so the worker escalates rather
  than guesses.
- **C-2 (structural escalation).** A `structural` change (any `component.yaml`
  add/remove/**edit**, a new/removed component) **MUST** set `confidence: "low"`
  and `needsFullResolve: true`. The precomputed graph cannot be trusted across a
  structural change — a `dependsOn` edit adds an edge with no new file path, so
  the worker treats *any* `component.yaml` content change as structural (it does
  **not** parse the manifest to decide — that would be resolver semantics it must
  not own; C-4).
- **C-3 (gate before consequence).** The worker's answer is **advisory**. A
  consumer feeding an irreversible action **MUST** let the authoritative full
  resolve **gate** that action when `confidence == "low"`. "Re-verifiable" means
  the authority runs *before* the irreversible step — not "we notice later." The
  worker emits the escalation signal (§5); it does not itself perform irreversible
  actions.
- **C-4 (no semantic re-implementation).** The worker **MUST NOT** re-derive
  ownership, blast-radius, or component identity from raw repository files. It
  consumes the published classification (`ownership.json`) and the published
  edges (`graph/dependencies.json`). This bounds drift between the Go resolver and
  the TS worker (§7).

**The single most important property:** the worker must *know when it is outside
its competence and say so*. A worker that returns a confident wrong answer on a
structural change is worse than no worker. C-2 + C-3 are that knowledge.

## 3. Per-event flow

```ts
async function onGitHubEvent(evt): Promise<AffectedResult> {
  const { base, head, changedFiles } = extractChange(evt)   // from the webhook payload — no checkout
  const index = await selectIndex(evt.repo, base)            // §4 — which catalog's index, edge-cached
  if (!index) return needsFullResolve("no index for base")   // C-1: unknown → escalate

  let global = false, structural = false
  const direct = new Set<ComponentKey>()
  for (const path of changedFiles) {
    switch (classify(path, index.ownership)) {               // shared contract algorithm
      case "global":     global = true; break
      case "structural": structural = true; break
      case "component":  direct.add(ownerOf(path, index.ownership)); break
      case "ignore":     break
    }
  }

  let changed = global ? allComponents(index) : direct
  const affected = expandReverseClosure(changed, index.edges) // graph walk over published edges

  return {
    affected: [...affected].sort(),
    directlyChanged: [...changed].sort(),
    confidence: structural ? "low" : "high",
    needsFullResolve: structural || !index.coversBase,        // §4 base coverage
    impactIndexId: index.id,
    catalogId: index.catalogId,
  }
}
```

`classify` and `expandReverseClosure` are **byte-for-byte the same logic** as
`orun catalog affected` (CS7), validated by the shared conformance fixtures
(`orun-catalog-state/test-plan.md` §3). The worker adds only the runtime (webhook
parse, R2/KV fetch, edge cache).

## 4. Base & index selection (a sharpness point — S-staleness)

The index is built at a **catalog snapshot** (e.g. main's tip at the last
resolve). A GitHub event's base may not have an index:

- **Exact match:** the event's base SHA has a published catalog/index → use it;
  `coversBase = true`.
- **Nearest ancestor:** no index for the exact base → use the nearest catalog the
  worker has (typically `catalogs/main`), and **diff against that catalog's
  source**, not the event's base. This widens the changed-file set (everything
  since that catalog's base) → **over-reports** (acceptable per C-1) and sets
  `coversBase = false` → `needsFullResolve = true` (advisory).
- **Per-branch indexes:** if `orun` resolved a branch (`catalogs/branches/<b>`),
  the worker prefers it for events on that branch. Maintaining per-branch indexes
  is a producer concern; the worker just selects the best available.

**Decision (D-3):** the worker never fabricates an index. No index → escalate
(`needsFullResolve`), never guess. Accuracy degrades **safely** (toward
over-report) as the event's base drifts from the nearest resolved catalog.

## 5. Reconciliation (the slow path)

The worker emits a **signal**, not an action:

- `needsFullResolve: true` (low confidence, or no base coverage) is the request
  for the authority. *Who* runs the full resolve is the integration's choice: a
  CI job (`orun catalog refresh`/`orun plan`), or a future cloud full-resolve
  worker (out of scope here — only the signal is in scope).
- The integration **MUST** gate irreversible steps on the authority when the
  signal is set (C-3). On `confidence: "high"` + `coversBase`, the estimate may
  be used directly (the common case — code edits inside existing components with
  stable structure).
- After a full resolve, `orun` publishes a fresh index; the worker's next event
  on that base gets `coversBase = true`. Reconciliation is thus **eventually
  exact** without the worker holding any authoritative state.

## 6. Fast path / cache strategy

- The index is **immutable, content-addressed**. The worker fetches it from R2 by
  id and caches it at the edge (Cloudflare Cache API / KV) keyed by the index id.
  An unchanged catalog → same id → **zero origin fetches** after the first.
- KV holds the small mutable pointer: `repo+scope → current impactIndexId`
  (moved by `orun`'s ref publish, mirrored to KV). Only this pointer is mutable;
  everything it points at is immutable.
- A cold event is one R2 fetch (a few KB) + in-memory map lookups + a graph walk
  over a few-thousand-node graph (sub-millisecond). No checkout, no clone.

## 7. Drift control (fat index, thin worker)

Two implementations of one semantics (Go resolver + producer; TS worker) **will**
drift unless contained:

- **Push intelligence into the artifact.** The classification rules (`globalPaths`,
  `globalBlocks`, `structuralFilenames`, `ignoreDirs`) are **data** in
  `ownership.json`, not logic in the worker (C-4). The worker interprets the
  index; it does not encode resolver knowledge.
- **Shared conformance fixtures.** The worker is tested against the **same**
  fixtures as `orun catalog affected` and **MUST** produce identical
  `affected`/`confidence` (`test-plan.md`). A divergence is a build failure.
- **`schemaVersion` gate.** The worker rejects an `ownership.json` whose
  `schemaVersion` it doesn't understand (escalates to `needsFullResolve`) rather
  than mis-reading it.

## 8. Deferred provisions (proposed here, justified before building)

These are the cloud-only index pieces deferred by `orun-catalog-state` D-4. The
worker spec is where they get justified — **only when measured necessary**:

- **AW3 — materialized reverse-closure (`impact/closure.json`).** Precompute the
  transitive reverse-dependency closure per component so the worker does an O(1)
  lookup instead of a graph walk. Justify only if the per-event graph walk is a
  measured latency problem (unlikely at a few-thousand nodes).
- **AW4 — per-component input fingerprints (`impact/fingerprints/`).** Lets the
  worker do *content-aware* diffing (a comment-only edit in a component dir → not
  affected; rename robustness), reducing C-1 false positives. Justify only if the
  false-positive rate (over-reporting) is a measured CI-cost problem. Requires the
  worker to fetch file contents and re-hash — heavier; this is the Tier-1
  precision upgrade over Tier-0 path-diff.

Both stay **deferred** until the Tier-0 path-diff worker is in production and its
costs are measured (provision discipline).

## 9. Invariants

1. **Never under-report.** For any event, the worker's `affected` is a **superset**
   of the truly-changed set a full resolve would compute (C-1; asserted in
   `test-plan.md`).
2. **Conformance.** The worker matches `orun catalog affected` on every shared
   fixture.
3. **No authoritative state.** The worker holds only immutable index objects + a
   mutable pointer mirrored from `orun`'s refs; it is reconstructible from the
   object store at any time.
4. **Honest escalation.** `structural`/`global`/no-base-coverage always sets
   `needsFullResolve`; the worker never hides uncertainty.
5. **Thin.** The worker contains no resolver semantics — only index interpretation
   (C-4).
