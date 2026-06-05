# Risks & Open Questions

> Live register for the edge worker. The competence boundary (C-1…C-4) is the
> dominant risk surface — most entries here trace to it.

## Decisions (settled, with defaults)

| # | Question | Default decision | Rationale |
|---|----------|------------------|-----------|
| D-1 | Worker authority | **Advisory only; never the source of truth** | The full resolve (`orun`) is authoritative; the worker is a fast estimate gated by it (design §1). |
| D-2 | Bias on uncertainty | **Over-report (C-1, MUST)** | A false-missing component ships a broken thing; a false-extra wastes CI. The §2 superset assertion enforces it. |
| D-3 | No index for the event base | **Escalate (`needsFullResolve`), never fabricate** | Accuracy degrades safely toward over-report; the authority reconciles (design §4). |
| D-4 | `structural` detection | **Any `component.yaml` content edit is structural — no manifest parse** | A `dependsOn` edit adds an edge with no new path; parsing the manifest would be resolver semantics the worker must not own (C-2/C-4). |
| D-5 | Closure index / fingerprints | **Deferred (AW3/AW4); build only on measured need** | Tier-0 path-diff + graph walk is sufficient and cheap at component-graph scale; precision provisions are justified by measurement, not anticipated. |
| D-6 | Correctness definition | **Match `orun catalog affected` on the shared fixtures** | One semantics, two consumers; conformance is a direct equality and the drift gate. |
| D-7 | Language | **TypeScript (Cloudflare Worker)** | The one cross-language seam; drift is contained by C-4 + shared fixtures (design §7). |

## Open questions

| # | Question | Options | Needed by |
|---|----------|---------|-----------|
| Q-1 | How does the KV pointer get moved when `orun` publishes a catalog ref? | (a) `orun catalog refresh --sync` pushes the index + mirrors the ref to KV; (b) a webhook on the object-store push; (c) a periodic reconciler | AW0 (pointer source) |
| Q-2 | Who runs the reconciling full resolve on `needsFullResolve`? | (a) the integration's CI (`orun catalog refresh`); (b) a future cloud full-resolve worker (out of scope) | AW5 (gate wiring) |
| Q-3 | Per-branch index coverage | (a) `orun` resolves + publishes branch catalogs so the worker has tight indexes; (b) worker always falls back to `main` (wider over-report) | AW2 (base selection accuracy) |
| Q-4 | Changed-file source for large/force-push events | (a) compare API; (b) escalate when the payload set is truncated | AW2 |
| Q-5 | Multi-tenant isolation (many repos/orgs) | per-repo KV namespace + R2 prefix; auth on the webhook | AW5 |

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Worker under-reports (false negative) → broken thing ships | Low | **Critical** | C-1 over-report MUST; the §2 superset assertion fails on any under-report; structural/global always escalate. |
| Naive structural detection misses a dep-edge edit | Med | **High** | D-4: any `component.yaml` edit is structural; CB-2 conformance fixture. |
| Integration treats a low-confidence answer as authoritative | Med | **High** | C-3: `needsFullResolve` MUST gate irreversible steps; ESC-2 test; documented as the binding integration contract. |
| Index/worker semantics drift (Go vs TS) | Med | High | C-4 (no resolver semantics in the worker) + shared conformance fixtures as a build gate (DR-1) + `schemaVersion` reject (DR-2). |
| Base staleness silently degrades accuracy | Med | Med | `coversBase` → `needsFullResolve` when the event base isn't covered; over-report bias keeps it safe (BS-2). |
| Stale KV pointer serves an old index | Low | Med | Pointer is the only mutable state; index objects immutable; on a missed move the worst case is an older (wider/over-reporting) index → safe, plus `coversBase` escalation. |
| Premature precision engineering (AW3/AW4) | Med | Low | Deferred behind measurement (D-5); not built without a recorded profile. |
| Webhook truncation classified as a partial change | Med | High | Truncated/unavailable changed-file set → escalate, never classify a partial set (C-1; RT-4). |

## Explicitly deferred

- The cloud full-resolve worker (only the `needsFullResolve` **signal** is in
  scope; running the authority in the cloud is separate).
- `impact/closure.json` and `impact/fingerprints/` (AW3/AW4) until measured
  necessary — and each requires a producer-side change proposed back to
  `specs/orun-catalog-state/`.
- GitHub App installation/auth plumbing beyond what the webhook contract needs.
