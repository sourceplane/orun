# Test Plan

> Coverage targets, the **selection-parity gate** (new engine == existing
> `--changed`), the change-detection fixtures (the engine's correctness corpus),
> freshness/refresh property tests, the catalog parity guard, and determinism.

## 1. Coverage targets

| Package | Target | Notes |
|---------|--------|-------|
| `internal/objcatalog` | ≥90% | read view; tolerant of missing `impact/` |
| `internal/affected` | ≥90% | the engine; both change sources |
| `internal/nodes` (impact types + `Path`) | maintain ≥90% | additive |
| `internal/objplan` (ownership + fingerprints) | ≥90% | deterministic |
| `internal/tui/services` (gate + map + changed view) | data-seam coverage | drive methods directly |
| `cmd/orun` (`catalog affected`, migrated `--changed`) | command + parity tests | |

## 2. The selection-parity gate (CS5/CS8 — the migration safety net)

The single most important test: the migrated `plan`/`run --changed` produces the
**identical job selection** as the pre-migration path.

- **PG-1.** A golden corpus of `(workspace, ChangeOptions)` cases; for each,
  assert the new engine's `Affected` selection == the selection the existing
  `collectChangedComponents` + `expand.DependencyResolver` path produced (captured
  as goldens before migration). Run both paths in CI until parity is green; the
  old path is removed only after.
- **PG-2.** Parity holds across: `--files`, `--uncommitted`, `--untracked`,
  `--base/--head` (with merge-base), `--intent-impact all|watch|none`,
  inline-component intent changes, and nested component dirs.
- **PG-3 (`--explain`).** The migrated `--explain` output is byte-equivalent
  (intent diff mode, changed sections, watch matches) to the existing printer.

## 3. Change-detection fixtures (the engine's correctness corpus)

Table-driven: `(workspace, base, head/changed-files) → expected
{DirectlyChanged, Dependents, Affected, IntentMode, Confidence, NeedsFullResolve}`.
Run against **both** change sources (git + fingerprint) — they must agree on the
component-level result. Encodes CD-1…CD-4.

| Case | Change | Expected | Contract |
|------|--------|----------|----------|
| CB-1 | edit `apps/web/src/x.ts` | `web` changed, high | `component` |
| CB-2 | edit `apps/web/component.yaml` (add `dependsOn`) | `web` changed, **low**, `needsFullResolve` | CD-2/S-3: dep-edge edit is structural |
| CB-3 | add `apps/new/component.yaml` | low, `needsFullResolve` | structural (discovery changed) |
| CB-4 | `intent.yaml` `catalog.defaults` edit | **all** | global |
| CB-5 | `intent.yaml` non-catalog block edit (`environments:`) | **none** | ignore |
| CB-5b | `intent.yaml` global change, `--intent-impact watch`, component watches the section | only the watching components | CD-3 watch preserved |
| CB-5c | same, `--intent-impact all` / `none` | all / none | CD-3 |
| CB-6 | `apps/web/README.md` typo (desc unchanged) | git source: `web`; fingerprint source: **none** (early cutoff) | CD-1 over-report tolerated; fingerprints tighten |
| CB-7 | `libs/shared` edit, `web`/`api` depend on it | `shared`,`web`,`api` | reverse closure |
| CB-8 | `docs/x.md` (no owner) | none | ignore |
| CB-9 | malformed `intent.yaml` | escalate to global | CD-1 over-report on parse failure |

**Never-under-select assertion (S-4).** For every case, `Affected` is a
**superset** of the truly-changed set a full resolve would compute. False
positives pass; **any** false negative fails.

## 4. Freshness / refresh property tests

- **P-1 Freshness soundness.** `SourceID(cur) == catalog.SourceID` **iff** inputs
  unchanged (resolve → fast path; touch a file → stale; revert → fast).
- **P-2 Determinism.** Two resolves of one source → byte-identical
  `ownership.json` **and** `fingerprints/` (≥100 random component orderings).
- **P-3 Read purity.** A `RefreshCatalog` write failure does not fail
  `LoadWorkspace` rendering.
- **P-4 CAS convergence.** Write-through racing a concurrent `catalogs/current`
  move converges; both objects persist.
- **P-5 Universal-hook debounce (D-8).** Two commands within `refreshTTL` on a
  stale tree → exactly one resolve; best-effort (a resolve error doesn't fail the
  command); time-bounded (deadline → abandoned, command succeeds).
- **P-6 Cockpit interval (D-9).** An external `catalogs/current` move reflects in
  the in-memory snapshot within one `refreshInterval`; resolve runs as a `tea.Cmd`;
  `ctrl+r` forces a tick; `ORUN_NO_AUTO_REFRESH=1` disables the hook.
- **P-7 Fingerprint determinism (S-11).** A clean→dirty→clean edit cycle returns
  the component `subtree` hash to its original value; committed-file hashes equal
  their git blob shas.

## 5. Catalog parity guard (CS8)

- **CG-1.** `[]ComponentSummary` from the graph path equals the intent path for a
  multi-component workspace, field for field
  (`name/type/domain/path/envs/profile/dependsOn`) — the guard that catches a
  dropped `Path`.
- **CG-2.** CG-1 over a fixture with nested dirs, inference files, a dependency
  edge, and a multi-env subscription.

## 6. E2E walk

`orun plan` (writes catalog + `impact/{ownership,fingerprints}`) → `orun catalog
affected --files <set>` (each CB case) → `orun plan --changed` / `run --changed`
(parity) → drive `LoadWorkspace` (fast then stale path) + the changed overlay →
`orun catalog refresh` (both materializations, golden keys stable). Assert the
`impact/` artifact ids are stable across an unchanged re-plan (determinism).
