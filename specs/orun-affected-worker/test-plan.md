# Test Plan

> Correctness for the worker is defined by **matching `orun catalog affected`**
> on shared fixtures and by **never under-reporting**. Plus base-staleness,
> escalation, and edge-cache behavior.

## 1. Conformance (the binding correctness definition)

The worker imports the **same** fixture corpus as
`orun-catalog-state/test-plan.md` §3 (CB-1…CB-9) and **MUST** produce identical
`{affected, directlyChanged, confidence, needsFullResolve}` for each. The fixture
table is the contract; a single divergence is a build failure.

| Case | Change | Expected (must match `orun`) |
|------|--------|------------------------------|
| CB-1 | edit `apps/web/src/x.ts` | `web` direct, high |
| CB-2 | edit `apps/web/component.yaml` (add `dependsOn`) | low, `needsFullResolve` (structural; no manifest parse) |
| CB-3 | add `apps/new/component.yaml` | low, `needsFullResolve` (structural) |
| CB-4 | `intent.yaml` `catalog.defaults` edit | **all**, global |
| CB-5 | `intent.yaml` `environments:` edit | **none**, ignore |
| CB-6 | `apps/web/README.md` typo | `web` (over-report, acceptable) |
| CB-7 | `libs/shared` edit, `web`/`api` depend on it | `shared`,`web`,`api` (closure) |
| CB-8 | `docs/x.md` (no owner) | none, ignore |
| CB-9 | malformed `intent.yaml` | escalate to global (over-report) |

## 2. The no-under-report assertion (C-1 / invariant 1)

For every case **and** a randomized property corpus: the worker's `affected` is a
**superset** of the truly-affected set computed by a full resolve over the head
tree. False positives (extra components) pass; **any** false negative fails. This
is the asymmetry that keeps a wrong estimate from shipping a broken thing.

## 3. Base / index selection (design §4)

- **BS-1 exact base:** event base SHA has a published index → `coversBase: true`;
  `needsFullResolve` only if structural.
- **BS-2 nearest ancestor:** no index for base → use `catalogs/main`; changed-set
  widens to "since main's catalog base"; `coversBase: false` →
  `needsFullResolve: true`; `affected` is still a superset (BS-2 runs the §2
  assertion).
- **BS-3 no index at all:** escalate (`needsFullResolve`), never fabricate.
- **BS-4 per-branch index present:** event on `branch/x` with
  `catalogs/branches/x` → prefer it; tighter (correct) result.

## 4. Escalation & gate (C-2/C-3)

- **ESC-1:** every `structural`/`global-uncertain`/`truncated`/`unknown-schema`
  path sets `needsFullResolve: true`.
- **ESC-2:** a sample integration test: a structural-change event with
  `needsFullResolve` is **gated** — the irreversible step (a simulated CI skip)
  does not proceed until an authoritative full resolve is supplied. Verifies the
  worker is advisory, not deciding.

## 5. Runtime / cache (design §6)

- **RT-1 edge cache:** two events against the same (unchanged) catalog → one R2
  fetch, the second served from edge cache (asserted: zero origin round-trips).
- **RT-2 immutability:** a moved KV pointer (new catalog) → new index id → fresh
  fetch; the old index object is still fetchable by id (immutable).
- **RT-3 latency budget:** a cold event (one R2 fetch + classify + closure over a
  synthetic 2,000-node graph) completes within the worker CPU budget; a warm
  event is cache + compute only.
- **RT-4 webhook parse:** push (with `commits[]`), PR (files API, paginated),
  force-push/truncated (escalate) all parse to the correct `ChangeSet`.

## 6. Drift gate (design §7)

- **DR-1:** the conformance corpus (§1) runs in the worker's CI on every change;
  it is the **same** fixture file the `orun` repo uses (vendored or fetched), so a
  resolver/classification change in `orun` that isn't mirrored fails the worker
  build.
- **DR-2 schemaVersion:** an index with a `schemaVersion` the worker doesn't
  support → `needsFullResolve`, never a parse attempt.
