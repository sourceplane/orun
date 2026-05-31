# Risks and Open Questions

Live register for Phase 2. Risks have a likelihood, an impact, a mitigation,
and an owner. Open questions have a default decision and the trigger that
would force a re-decision. New risks discovered during implementation are
appended here rather than carried in PR descriptions.

---

## Risks

### CR1 — Resolver non-determinism breaks `catalogHash` stability

- **Likelihood:** Medium
- **Impact:** High
- **Mitigation:** All maps and arrays sorted at stage 11 / 13 of the
  resolver pipeline; canonical JSON encoding (sorted keys, no whitespace)
  for hashed inputs; T-RES-1 / T-IDK-1 property tests; `clock.Time` and IO
  shim seams for inference.
- **Owner:** C2 + C3 implementer.

### CR2 — Phase 1 callers depend on `.orun/revisions/<revKey>` paths

- **Likelihood:** High (any external script / CI integration assuming the
  Phase 1 layout)
- **Impact:** Medium
- **Mitigation:** `stateCompatibilityWrites = true` for the entire Phase 2
  lifetime; alias under `.orun/revisions/<revKey>/plan.json` is byte-identical
  to the canonical body (T-COMPAT-3); sunset reserved for Phase 3.
- **Owner:** C6 implementer + every PR touching `cmd/orun/plan.go` or
  `internal/revision`.

### CR3 — Dirty-hash churn on every keystroke

- **Likelihood:** Medium (developers running `orun catalog refresh` in tight
  loops)
- **Impact:** Medium (snapshot bloat, slow refresh)
- **Mitigation:** Dirty-hash inputs restricted to catalog-relevant files
  per `identity-and-keys.md` §7; T-IDK-4 property test asserts non-catalog
  files don't churn the hash; resolver caches `ResolvedCatalog` keyed by
  `catalogInputHash` for the process lifetime.
- **Owner:** C1 implementer.

### CR4 — Two `orun catalog refresh` runs racing the `current` ref

- **Likelihood:** Low (manual operation; unlikely in CI with serial steps)
- **Impact:** Low
- **Mitigation:** `CompareAndSwap` from Phase 1 `StateStore`; loser retries
  with the latest body; both writers' bodies converge because the catalog
  body is content-addressed. T-STORE-1 property test exercises 100-way
  concurrency.
- **Owner:** C4 implementer.

### CR5 — Catalog migration mis-attaches Phase 1 revisions

- **Likelihood:** Medium (legacy revisions may lack enough metadata to infer
  a source)
- **Impact:** Medium
- **Mitigation:** Bucket under `sources/src-migrated-<sha>/catalogs/cat-orphan-<sha>/`
  with a `migration.json` explaining the attribution; `--dry-run` is the
  documented onboarding step; migration is opt-in only.
- **Owner:** C6 + C7 implementer (when migration extension lands).

### CR6 — Inference layer panics on malformed package files

- **Likelihood:** Medium (real-world repos contain broken JSON, pinned
  pre-release tools, weird shapes)
- **Impact:** Low (with mitigation)
- **Mitigation:** Inference is wrapped in a `defer`/recover pattern only at
  the package boundary; per-file failures emit `ErrInferenceFailed` and
  continue; never abort the resolver in default mode. `--strict` upgrades to
  abort.
- **Owner:** C2 implementer.

### CR7 — `Syncer` interface drift before Phase 3

- **Likelihood:** Low
- **Impact:** Medium (would require a coordinated revv across packages)
- **Mitigation:** `internal/catalogsync` ships small and locked; CI lint
  forbids `net/http`, `internal/runner`, `cmd/orun` imports; `SyncPayload`
  is built from `internal/catalogmodel` types only. Any change to the
  interface goes through a proposal under `/ai/proposals/`.
- **Owner:** C9 implementer + Orchestrator.

### CR8 — Component-execution-index unbounded growth

- **Likelihood:** Medium (long-lived `main` catalogs accumulate executions)
- **Impact:** Low (read latency)
- **Mitigation:** Index keeps the latest 200 executions per component;
  older entries live only in `history/components/<name>/events/`; reader
  paginates via the events stream when needed.
- **Owner:** C7 implementer.

### CR9 — JSON Schema generation drift between code and committed file

- **Likelihood:** Low
- **Impact:** Low (test breakage, not data corruption)
- **Mitigation:** `go generate ./internal/catalogmodel` is wired into a CI
  `make verify-generated` target; PRs that touch the schema must commit the
  regenerated artifact.
- **Owner:** C0 implementer.

---

## Open questions

### CQ1 — Should `orun status` auto-refresh the catalog if missing?

- **Default decision:** No. Only `plan`, `run`, and explicit
  `catalog refresh` ever write catalog state. `orun status` is read-only.
- **Re-decision trigger:** ≥ 3 user reports asking why `orun status`
  shows nothing on a fresh repo.

### CQ2 — Should dirty-workspace snapshots include `README.md` content hash?

- **Default decision:** Yes, but only when `intent.yaml`
  `catalog.inference.readme = true`. Off by default in inference toggles.
- **Re-decision trigger:** README-driven inference proves churny in real
  use; revisit by gating on a separate `catalog.dirtyHash.readme` flag.

### CQ3 — Should `metadata.owner` be required by default?

- **Default decision:** Warn locally, error in `--strict`. `intent.yaml`
  `catalog.validation.requireOwner = true` upgrades to error per repo.
- **Re-decision trigger:** Adoption survey: > 80 % of users set the flag;
  default flips.

### CQ4 — Should component dependency cycles fail catalog refresh?

- **Default decision:** Warn unless the edge type is `deploy-after`, in
  which case error. `--strict` errors on any cycle.
- **Re-decision trigger:** Cycles found in `calls` graphs cause concrete
  bugs in downstream tooling (TUI, SaaS rendering).

### CQ5 — Should catalog refs be global or repo-scoped under `.orun/`?

- **Default decision:** Global under `.orun/refs/` for the local repo.
  Future remote driver adds the `orgs/<org>/projects/<project>/repos/<repo>/`
  prefix per `sync-model.md` §3. The local layout remains single-repo.
- **Re-decision trigger:** Multi-workspace `.orun/` use cases land (e.g.
  monorepos with multiple Orun roots).

### CQ6 — Should `orun plan --no-catalog-refresh` allow a stale catalog?

- **Default decision:** Yes, with a one-line warning unless `--quiet`. The
  plan records `metadata.catalog.skipped = true` so downstream tooling can
  see it.
- **Re-decision trigger:** Stale-catalog plans cause production incidents
  attributed to manifest staleness.

### CQ7 — Should SaaS accept branch snapshots from any branch?

- **Default decision:** Yes as preview; canonical update only from branches
  listed in `intent.yaml` `catalog.sourceOfTruth.canonicalBranches`. Phase 3
  spec will encode tenancy + branch filtering on the server.
- **Re-decision trigger:** Phase 3 design.

### CQ8 — Should the resolver call `git` directly or use `go-git`?

- **Default decision:** Implementer's choice during C1; document the
  trade-off in the PR. Either is spec-compatible. Constraint: the resolver
  must still work in environments where `git` is not on `PATH` (e.g.
  minimal Docker runners) — if shelling out, the implementer must verify
  this with a CI fixture.
- **Re-decision trigger:** First production failure tied to the chosen
  Git layer.

### CQ9 — Should `internal/catalogstore.Resolver` cache reads in-process?

- **Default decision:** No. `internal/catalogresolve` already caches
  `ResolvedCatalog` for the process lifetime; double-caching at the
  storage layer adds invalidation surface for no real win in the local
  driver. Phase 3 may add a remote-driver cache.
- **Re-decision trigger:** Profiling shows storage reads are the hot path
  in catalog CLI commands.

### CQ10 — Should component identity ever include environment?

- **Default decision:** No. Environment is a binding under
  `spec.environments`. Component identity is `<namespace>/<repo>/<name>`.
  This is an invariant per `design.md` §8.
- **Re-decision trigger:** Treat any proposal asking for environment-scoped
  identity as a Phase 3 design issue, not a Phase 2 spec change.
