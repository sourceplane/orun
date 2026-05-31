# Task 0029 — Verifier Report (C3: snapshot + graph builder + catalogHash)

## Result: PASS

PR #172 lands C3 — `BuildCatalog`, `CatalogView`, `ResolverInputs`,
`ErrResolverInputsIncomplete`, and the five `CatalogGraph` siblings —
inside the bounded surface scoped by Task 0028. All acceptance criteria
are met: spec-faithful hash ordering, deterministic byte-identical
re-encoding, owner-edit propagation, provenance-only stability, summary
counts via sorted-distinct enumeration, valid `cat-…` snapshot key,
typed `ErrResolverInputsIncomplete` returned and `errors.As`-extractable
through `IsResolverInputsIncomplete`, and source-key back-fill that
happens AFTER `manifestHash` is computed (Source block is wholly
excluded from `manifestHash` per `internal/catalogresolve/hash.go`).
Phase 1 floors held byte-for-byte; Phase 2 floors held; catalogresolve
coverage rose 90.2 → 90.9 (+0.7 pp), confirming the implementer's claim.
PR CI: 3/3 required SUCCESS, branch CLEAN/MERGEABLE.

## Checks

| # | Step | Command | Result |
|---|------|---------|--------|
| 1 | Branch checkout + sync | `git fetch && git checkout task-0028-catalog-c3-snapshot-graph && git pull --ff-only` | up-to-date at `ffb5ee9` (HEAD on PR), `101b5d4` is the post-implementer report-only commit on `main` |
| 2 | PR metadata | `gh pr view 172 --json …` | `state=OPEN`, `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`, +1273 / -0 |
| 3 | PR scope (file list) | `gh pr view 172 --json files` | 7 files: `ai/reports/task-0028-implementer.md` + 3 new sources + 3 new test files in `internal/catalogresolve/`. **No edits to existing source files**. Bounded as specified. |
| 4 | Required CI checks | statusCheckRollup | `Orun Plan` SUCCESS · `Harness dry-run guard` SUCCESS · `state-redesign-tests / test` SUCCESS. Skipped rows are matrix placeholders (no work scheduled by the changed-plan). |
| 5 | `go build ./...` | exit 0 | clean |
| 6 | `go vet ./...` | exit 0 | clean |
| 7 | `go test ./... -race -count=1` | exit 0 | all packages green |
| 8 | `make verify-generated` | exit 0 | "✅ generated artifacts up-to-date" |
| 9 | `make test-state-redesign` | exit 0 | all gates pass: statestore 95.7, revision 90.3, executionstate 90.0, catalogmodel 91.1, sourcectx 91.1, catalogresolve 90.9, catalogmodel Sanitize* 100.0 |
| 10 | catalogresolve coverage | `go test -coverprofile=cover.out ./internal/catalogresolve/... && go tool cover -func` | **90.9 %** (claim confirmed) |
| 11 | CI log inspection | `gh run view 26708921479 --log` | `make test-state-redesign` actually executed; `internal/catalogresolve` test package ran; coverage gates printed; the run is not a no-op. |
| 12 | Code-path read: `graph.go` | n/a | five graphs built in fixed order [dependencies, systems, apis, resources, owners]; nodes sorted by `key`; edges sorted by `(from, to, type, optional)` per the verifier task prompt. `stampCatalogSnapshotKey` is idempotent. |
| 13 | Code-path read: `catalog_hash.go` | n/a | hashed payload = `(catalogInputHash, sorted (componentKey, manifestHash) pairs, graphs in fixed order with `catalogSnapshotKey` cleared to break the self-reference, resolverVersion)`. `Source` block isn't present — `catalog_hash` consumes manifest hashes only. Output `sha256:<hex>` via `catalogmodel.CanonicalEncode`. Matches identity-and-keys §9. |
| 14 | Code-path read: `catalog_snapshot.go` | n/a | `validateResolverInputs` enforces every required field; HeadRevision/TreeHash exempted only for `local-nogit`; assembleSnapshot copies inputs verbatim and never invents `authoritative`/`preview`/`sourceSnapshotKey`/`catalogInputHash`/`headRevision`/`treeHash`/`workingTree`. Source-key back-fill happens AFTER `manifestHash` was finalised at C2 stage 10 and after `catalogHash` is computed; `hash.go` `manifestHash` excludes the entire `Source` block from its hashed payload, so back-fill is safe. |
| 15 | T-IDK-1 deterministic hash | `TestCatalogHash_Deterministic_T_IDK_1` | uses `pgregory.net/rapid`, 1000 random orderings → identical hash after the documented `sortByComponentKey` precondition. Passes. |
| 16 | Owner-edit acceptance | `TestCatalogHash_OwnerEditChanges` | deterministic test; mutates `metadata.owner`, recomputes both `manifestHash` and `catalogHash`, asserts both change. Passes. |
| 17 | Provenance-only stability | `TestCatalogHash_ProvenanceOnlyEdit_Stable` | mutates only `Resolution.InheritedFrom`; asserts both `manifestHash` and `catalogHash` are unchanged. Passes. |
| 18 | Two-call determinism | `TestBuildCatalog_Deterministic` | runs `BuildCatalog` twice on the same fixture; clears the per-call ULID `CatalogSnapshotID` and asserts canonical-encoded snapshot + every graph are byte-identical. Passes. |
| 19 | Summary sorted-distinct | `TestSummaryCounts_FromSortedDistinct` | components/systems/apis/resources/owners/domains all asserted against fixture. Passes. |
| 20 | `catalogSnapshotKey` shape | `TestBuildCatalog_E2E_HappyPath` | regex `^cat-[a-f0-9]{6,16}$` AND `catalogmodel.ValidateCatalogSnapshotKey` both checked. Passes. |
| 21 | `ErrResolverInputsIncomplete` | `TestBuildCatalog_MissingInputs` | empty `ResolverInputs` → `IsResolverInputsIncomplete(err) == true` (uses `errors.As` under the hood — type-extraction works through `fmt.Errorf("%w", …)` wrappers). Passes. |
| 22 | `ResolverVersion` and `CatalogInputHash` flow into hash | `TestCatalogHash_ResolverVersionBump`, `TestCatalogHash_InputHashChange` | both inputs assert hash sensitivity. Passes. |

## CI Log Review

CI run for the test job (`state-redesign-tests / test`, run id `26708921479`)
inspected via `gh run view --log`. Confirmed:
- `make test-state-redesign` executed as a single shell step.
- All Phase 1 + Phase 2 packages compiled and tested under `-race`.
- `internal/catalogresolve` test binary ran (1.186s) and the coverage
  gate printed `measured: 90.9%`.
- `Orun Plan` (changed-plan policy) and `Harness dry-run guard` ran on
  the same head SHA — both green. No `FAILURE`/`ERROR` rows.

## Coverage Numbers

Local re-run on PR head `ffb5ee9`:

| Package | Floor | Measured | Δ vs floor |
|---------|-------|----------|------------|
| `internal/statestore`     | ≥ 95.0 | **95.6 %** (full pkg path; `make` reports 95.7 %) | held |
| `internal/revision`       | ≥ 90.0 | **90.3 %** | held |
| `internal/executionstate` | ≥ 90.0 | **90.0 %** | held |
| `internal/catalogmodel`   | ≥ 90.0 | **91.1 %** | held |
| `internal/sourcectx`      | ≥ 90.0 | **91.1 %** | held |
| `internal/catalogresolve` | ≥ 90.2 | **90.9 %** | +0.7 pp (claim confirmed) |
| `Sanitize*` 100 % gate    | == 100 | **100.0 %** | held |

(`statestore` 95.6 vs 95.7 is the well-known cmd/orun-vs-pkg rounding
seam in `make test-state-redesign`; the gate uses `... | awk` on the
combined `./internal/statestore/...` figure and reports 95.7 %, ≥ 95
floor. Not a regression.)

## Issues

None. No verifier fixes required.

## Risk Notes

1. **Minor spec drift, low risk** — `data-model.md` §4 / `resolution-
   pipeline.md` §7 describe node/edge ordering as `(kind, key, type)`,
   while the verifier prompt (and the implementer) sort nodes by `key`
   alone and edges by `(from, to, type, optional)`. Functionally
   deterministic in both readings; within each graph, nodes sharing a
   `key` always share a `kind`, so the orderings agree. Worth a future
   spec wording cleanup but not a C3 blocker. No proposal raised this
   cycle to keep scope tight.
2. The C3 layer trusts the caller to compute `Authoritative`/`Preview`
   correctly. `validateResolverInputs` cannot police booleans (no zero-
   value sentinel). Documented in code; the C4 writer is the next
   guardrail (writer must reject `Authoritative=true` paired with
   `WorkingTree="dirty"`).
3. `CatalogSnapshotID` is freshly minted per `BuildCatalog` call (ULID
   via `catalogmodel.NewCatalogSnapshotID`). Two consecutive calls
   intentionally produce different IDs even though the rest of the
   snapshot is byte-identical; the determinism test correctly clears
   the ID before encoding. C4 writers must be aware of this — the ID
   is identity, not content.

## Spec Proposals

None required for C3. The minor wording mismatch noted above can be
folded into a C4-or-later editorial pass.

## Recommended Next Move

PASS-merged. Next orchestrator cycle should scope **C4 implementer**:
the catalog writer (`internal/catalogwrite` or equivalent) — disk layout
under `sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/…`,
collision-policy `-x<n>` suffix on `catalogSnapshotKey`, and the
`writeCatalogSnapshot` / `writeCatalogGraph` / `writeComponentManifest`
trio per data-model §2 / §3 / §4. C3's `BuildCatalog` is the pure
upstream — C4 takes its `CatalogView` and persists.

## PR Number

**#172** — https://github.com/sourceplane/orun/pull/172
