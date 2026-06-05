# Implementation Plan

> **Under review — do not implement until this spec is approved.** Then gated on
> `orun-catalog-state` CS3 (artifacts published) + CS7 (the `orun catalog
> affected` conformance oracle). The worker is a separate
> service; these milestones are its build order. AW0–AW2 are the Tier-0 worker;
> AW3–AW4 are the deferred precision provisions (build only when measured
> necessary); AW5 is hardening.

```
AW0 Index fetch + cache ──► AW1 classify + closure (shared contract) ──► AW2 webhook + response
                                        │
                                        └──► AW5 hardening (escalation, observability, limits)
   (deferred, measure first)  AW3 closure index     AW4 fingerprint index (content-aware)
```

---

## AW0 — Index fetch + edge cache
**Goal:** load an immutable index by id from R2, cache at the edge, gate on
`schemaVersion`.
- KV pointer read (`affected:<repo>:<scope>`); R2 fetch of `ownership.json` +
  `graph/dependencies.json` + `catalog.json` by id; Cache API keyed by index id.
- Reject unknown `schemaVersion` → escalation stub.

**Deps:** orun CS3. **PR scope:** 1 PR. **Done when:** a published index loads
into `LoadedIndex`; a second fetch hits the edge cache (no R2 round-trip);
unknown `schemaVersion` returns `needsFullResolve`. **Design:** `design.md` §6,
`data-model.md` §1–2.

## AW1 — Classification + reverse closure (the shared contract)
**Goal:** the byte-for-byte port of `orun catalog affected`'s `classify` +
`expandReverseClosure`.
- Implement `classify(path, ownership)` and `expandReverseClosure(changed,
  edges)`; cycle-safe; deterministic sorted output.
- Wire the **shared conformance fixtures** (`orun-catalog-state/test-plan.md`
  §3) as the worker's test corpus.

**Deps:** AW0, orun CS7. **PR scope:** 1 PR. **Done when:** the worker matches
`orun catalog affected` on **every** shared fixture (CB-1…CB-9); the
no-under-report (superset) assertion passes. **Design:** `design.md` §2–§3,
`test-plan.md` §1–2.

## AW2 — Webhook + `AffectedResult`
**Goal:** the per-event HTTP path.
- `extractChange` for push + pull_request (changed-files from payload/PR files
  API); base/index selection (`coversBase`); assemble `AffectedResult`.
- Escalate on truncated/unavailable changed-file sets.

**Deps:** AW1. **PR scope:** 1–2 PRs (extraction; base selection + response).
**Done when:** a sample push and PR event return a correct `AffectedResult`; a
truncated diff escalates; a no-op push returns `affected: []`. **Design:**
`design.md` §3–§4, `data-model.md` §3–§4.

## AW3 — Materialized reverse-closure index  [DEFERRED — measure first]
**Goal:** O(1) closure lookup instead of a graph walk.
- **Only if** the AW2 graph walk is a measured latency problem. Requires `orun`
  to publish `impact/closure.json` (a producer change, proposed back to
  `orun-catalog-state`).

**Deps:** AW2 + measurement. **Done when:** justified by a profile; otherwise
**not built**. **Design:** `design.md` §8.

## AW4 — Per-component fingerprint index (content-aware)  [DEFERRED — measure first]
**Goal:** cut C-1 false positives (comment-only edits, renames).
- **Only if** over-reporting is a measured CI-cost problem. Requires `orun` to
  publish `impact/fingerprints/` and the worker to fetch + re-hash changed file
  contents (Tier-1). Producer change proposed back to `orun-catalog-state`.

**Deps:** AW2 + measurement. **Done when:** justified by measured false-positive
cost; otherwise **not built**. **Design:** `design.md` §8.

## AW5 — Hardening
**Goal:** production posture.
- Escalation signal plumbing (how `needsFullResolve` reaches the integration's
  gate, C-3); observability (confidence/escalation rates, cache hit rate, base-
  coverage rate); request limits; per-repo isolation.

**Deps:** AW2. **PR scope:** 1–2 PRs. **Done when:** escalation rate + cache hit
rate are observable; a structural-change event is gated correctly end-to-end in a
sample integration. **Design:** `design.md` §5, `risks-and-open-questions.md`.

---

## Cross-cutting
- The worker **MUST NOT** add resolver semantics (C-4); any temptation to parse
  manifests/intent is a signal to push that data into the producer's index.
- Every PR runs the shared conformance fixtures; a divergence from
  `orun catalog affected` is a build failure (drift gate).
- No deferred provision (AW3/AW4) is built without a recorded measurement
  justifying it.
