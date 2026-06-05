# Spec: orun-affected-worker

> ⚠️ **STATUS: UNDER REVIEW — NOT A CURRENT REQUIREMENT.** This spec is a
> forward-looking sketch, held for review. **Do not implement it**, and do **not**
> let it drive the orun-side design: `specs/orun-catalog-state/` makes no
> structural accommodation for this worker beyond noting that the artifacts it
> would consume are content-addressed and already remotable. When this spec is
> reviewed and approved, any orun-side support will be specified at that time.
> The orun change-detection engine (`internal/affected`) is the authority and the
> conformance oracle; a worker, if built, mirrors it.

**A Cloudflare Worker that, per GitHub event, turns a changed-file set into an
*affected component set* in milliseconds — approximate, advisory, and re-verified
by an authoritative full resolve.** It is a **pure consumer** of the impact
artifacts that `orun` publishes (`specs/orun-catalog-state/`). It computes; it is
never the source of truth.

This is the **slow-path/fast-path** split made concrete: `orun` (the producer)
full-resolves for correctness on meaningful boundaries; this worker (the
consumer) serves a fast estimate on every commit. When they disagree, the full
resolve wins.

## Status

| Field | Value |
|-------|-------|
| Status | **UNDER REVIEW — not a current requirement; do not implement** |
| Gated on | A separate review/approval, **then** `specs/orun-catalog-state/` CS3 (artifacts published) + CS7 (`orun catalog affected` conformance oracle) |
| Runtime | Cloudflare Worker (TypeScript); state in R2 (objects) + KV (ref/index pointers) |
| Producer/authority | `orun` — `orun catalog affected` is the **conformance oracle** this worker must match |
| Language | TypeScript (the rest of the model is Go; this is the one cross-language seam — see drift risk) |
| Target | a separate service/repo; this spec is the contract |

## The one-paragraph thesis

For every GitHub push/PR event, an integration wants one fast answer: *which
components are affected?* Running the full Go resolver per commit at org scale is
too slow and too heavy. Instead, the worker fetches the **impact index** that
`orun` already published for the relevant catalog (an immutable, content-
addressed `ownership.json` plus the component dependency edges), maps the event's
changed files to components, expands by reverse dependency closure, and returns
`{affected, confidence}` in milliseconds — served from the edge cache when the
catalog is unchanged. The worker is deliberately **dumb**: it does map lookups
and a graph walk, never resolver semantics. It is also deliberately **honest**:
when an event changes the dependency *structure* (a `component.yaml` edit, a new
component, a global `intent.yaml` change), it lowers confidence and defers to a
full resolve rather than guessing. The full resolve **gates** any irreversible
action; the worker's answer is advisory.

## Why a worker (and not just `orun catalog affected`)

`orun catalog affected` is the same computation, but it runs locally/in-CI and
loads the index from the local store. The worker is the **always-on, edge-cached,
per-event** form: it serves a webhook, holds the index in R2/KV, and answers
without a checkout or a Go process. They share the **exact** classification and
closure logic by contract (the conformance fixture in
`orun-catalog-state/test-plan.md` §3) so the two never diverge in result.

## Read order

1. **`design.md`** — the competence-boundary contract (C-1…C-4, the
   centerpiece), the fast-path/slow-path architecture, the per-event flow, base
   selection, reconciliation, the fat-index/thin-worker discipline, and the
   drift-control strategy.
2. **`data-model.md`** — the index the worker consumes, the webhook→changed-files
   extraction, and the `AffectedResult` response contract.
3. **`implementation-plan.md`** — milestones **AW0 → AW5**.
4. **`test-plan.md`** — conformance against the `orun` oracle, the over-report
   (no-under-report) assertion, base-staleness cases, and edge-cache behavior.
5. **`risks-and-open-questions.md`** — decisions, open questions, risks.

## Phase boundaries

| In scope | Out of scope |
|----------|--------------|
| The Cloudflare Worker; consuming `impact/ownership.json` + `graph/dependencies.json` by content id from R2; the webhook→changed-files extraction; classification + reverse closure (shared contract); the `AffectedResult` response + confidence; edge-cache strategy; base/index selection; the reconciliation **signal** | Publishing the index (that's `orun` / `orun-catalog-state`); running full resolves in the cloud (a separate "cloud full-resolve" job — only the *signal* to trigger it is here); the materialized reverse-closure + fingerprint index (deferred provision — AW3/AW4 propose them once justified); GitHub App auth/installation plumbing beyond what the contract needs; multi-tenant isolation hardening |

## Document conventions

- TypeScript for interfaces; JSON for wire/stored schemas. Object IDs are
  `"<algo>:<hex>"`, matching `orun-object-model`.
- "MUST / SHOULD / MAY" carry RFC 2119 weight in the competence-boundary section.
- The worker **MUST** match `orun catalog affected` output on the shared
  conformance fixtures — that is the binding correctness definition.

## Out-of-band references

- Producer spec: `specs/orun-catalog-state/` (the impact index, the contract,
  the conformance oracle).
- Object model: `specs/orun-object-model/` (content addressing, `objremote`
  closure that moves the index to remote storage).
