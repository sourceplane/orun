# Current Roadmap Position

## Active Spec
`specs/orun-component-catalog/` (Phase 2, local-only) — content-addressed
SourceSnapshot/CatalogSnapshot model wrapping the Phase 1 trigger /
revision / execution lineage. **Local-only** for the entire phase: no
HTTP, no SaaS, no DB schema. `internal/catalogsync` ships only `Syncer`
interface + `NoopSyncer` (C9).

## Active Milestone
**C5 — Catalog CLI surface (`orun catalog *`).** C5 PR-1 CLOSED 2026-05-31
via PR #176 (`96e3bbd`): `orun catalog refresh` + `orun catalog refs` +
shared CLI foundation + two new pure seams. C5 PR-2 (`list`/`describe`/
`tree`/`history`/`validate`, `diff` stubbed for C8) is the next implementer
milestone; it reuses the shared RefSelector parser, §11 envelope, and both
seams shipped in PR-1.

## Just Completed — Tasks 0036 + 0037 (C5 PR-1 — PR #176 MERGED, single-pass closure)
- **Status:** ✅ Implementer + Verifier in one session (full-ship-cycle
  directive). PR #176 squash-merged into `main` at `96e3bbd` on
  2026-05-31T15:17:22Z; branch deleted. Reports:
  - Implementer: `ai/reports/task-0036-implementer.md`
  - Verifier: `ai/reports/task-0037-verifier.md`
- **What shipped (`cmd/orun`):**
  - `catalog.go` — `orun catalog` root + subcommand index; the stable §11
    JSON envelope writer (`apiVersion`/`kind`/`data` + always-present
    `warnings`); the single shared `parseCatalogSelector()` →
    `catalogstore.RefSelector` bridge (`current`|`main`|`latest`|
    `branches/<name>`|`prs/<n>`|`cat-<key>` pin), malformed → exit 1.
  - `catalog_refresh.go` — the only write-path command: resolve
    (`sourcectx`) → build (`catalogresolve.BuildCatalog`) → assemble
    (`catalogstore.AssembleBundle`) → commit (`WriteSourceSnapshot` →
    `WriteCatalogSnapshot` → `WriteGlobalIndexes` → `WriteRefs`). Idempotent
    reuse (`created=false`/`reused=true`, exit 0); dirty-worktree banner +
    local-only snapshot; authoritative on clean main/protected, preview
    otherwise; §2 exit codes (0/1/2/3); `--sync` no-op.
  - `catalog_refs.go` — ref enumeration via `catalogstore.ListRefs`; empty
    store → empty list exit 0; `--json` → `CatalogRefsResult`.
  - `main.go` — exit-code plumbing: unwrap `interface{ ExitCode() int }`
    via `errors.As`.
- **Two new pure seams in `internal/catalogstore`** (architecture rule held
  — catalogstore depends on catalogresolve, NEVER reverse; both seams
  no-raw-FS): `bundle.go` `AssembleBundle` (pure, deterministic, no I/O,
  golden-tested) and `listrefs.go` `ListRefs` (source/catalog ref-tree join
  via statestore, name-sorted).
- **Verifier adjudication:** PASS, no fixes required. `internal/catalogstore`
  coverage held at **90.2 %** (floor). `cmd/orun` package coverage 24.1 %
  (its long-standing baseline, no enforced floor); new seams + refresh→refs
  E2E ARE covered. The one mid-implementation correction (componentKey
  needs single-segment repo → `shortRepoName`) was caught pre-PR.
- **Static guards held:** `go build ./...`, `go vet`, `go test -race
  -count=1` (cmd/orun + catalogstore + catalogresolve + sourcectx),
  `make verify-generated`, no-raw-FS grep clean. Manual smoke verified all
  seven scenarios (created/reused/refs/feature-preview/dirty/invalid-
  selector/sync).
- **CI:** PR CI green on HEAD `88e104d` (run 26716370631 — test, Orun Plan,
  Harness dry-run guard); merged at `96e3bbd`.
- **Carry-forward to C5 PR-2:** `CatalogLocalIndexes` emits component-exec
  indexes only (empty `executions[]`); owner/system/domain/type axes
  deferred (data-model §9 under-specifies them; history events are C7).

## Just Completed — Task 0035 (C4 PR-3 verifier — PR #175 MERGED, C4 CLOSED)
- **Status:** ✅ Verifier PASS with path-(a) attached coverage fix. PR
  #175 squash-merged into `main` at `42eace5f` on 2026-05-31T12:51:14Z;
  branch deleted. **Milestone C4 CLOSED.** Reports:
  - Implementer: `ai/reports/task-0034-implementer.md`
  - Verifier: `ai/reports/task-0035-verifier.md`
- **What shipped:** the entire read side of `internal/catalogstore` —
  `resolver.go` with all 5 Resolver methods (`ResolveCurrentSource`,
  `ResolveSource`, `ResolveCatalog`, `ResolveComponent`,
  `ResolveComponentLatest`) implementing the `catalog-store.md` §4
  refs-first → walk-fallback → typed-sentinel ladder (`current → latest
  → main`; `ErrComponentNotFound` vs `ErrCatalogNotFound`; intact
  `errors.Is` chains; ctx cancellation; non-`NotFound` read errors
  surfaced not swallowed), plus `rebuild.go` `RebuildIndexes` (added to
  the `Resolver` interface) reconstructing local + global indexes from
  authoritative manifests deterministically, with a T-STORE-3
  scrub-then-rebuild byte-identity test across all three index kinds.
  Zero live `ErrNotImplemented` Resolver surfaces.
- **Coverage adjudication:** implementer landed `internal/catalogstore`
  at **88.9 %** (below the 90 % floor; the package has **no CI coverage
  gate**, so green CI did not enforce it). The gap was concentrated in
  the NEW `rebuild.go` bodies — reachable walk/merge/write logic, not
  validated-input defensive returns → path (a). Verifier attached two
  test files (`rebuild_coverage_test.go` 321 L +
  `resolver_coverage_test.go` 193 L, 514 insertions, `package
  catalogstore_test`, `writeRaw` helper) on commit `cb563be`, lifting
  to **90.3 %** (progression 88.9 → 89.5 → 89.8 → 90.2 → 90.3). Healthy
  margin above the floor to dodge the documented executionstate "flap
  problem". **No production code changed.**
- **Branches left uncovered (documented):** residual 81.8 % arms in
  `rebuildSourceGlobalIndex` / `rebuildCatalogGlobalIndex` /
  `writeComponentGlobalIndexPlain` are genuinely-unreachable defensive
  returns (path-builder errors on validated keys; `PrettyEncode` on
  guaranteed-serializable structs). Carried at zero rather than adding
  white-box hooks.
- **Adjudications:** `RefSelector.Snapshot` accept-and-document (spec
  `catalog-store.md:45-46` reserves it for C8; the implementer's
  `readSourceByKey` wiring is a permitted permissive superset).
  Implementer's 3 open questions resolved inline, no proposals filed:
  CGI test-helper duplication = harness hygiene; rebuild stale-index
  scrub → C8; corrupt-manifest silent skip = acceptable + now covered.
- **Static guards held:** `go vet ./...`, `go build ./...`, `go test
  -race -count=1 ./...`, `make verify-generated`, no raw FS imports in
  `internal/catalogstore/` (grep empty). Adjacent floors held.
- **CI:** PR-branch CI on `cb563be` (`test`/state-redesign-tests, Orun
  Plan, Harness dry-run guard, conformance) all PASS; post-merge main CI
  on `42eace5` (CI, state-redesign-tests, conformance) all PASS.
- **Carry-forward R-008:** executionstate zero-margin floor flapped once
  (89.6 % vs 90.0 %) on the PR #175 CI `test` job — recorded in
  `open-risks.md`; not fixed in-PR (PR-boundary). Did not recur on the
  verifier's runs.

## Current Task — Task 0036 (C5 PR-1 implementer — SCOPED, awaiting implementer)
- **Status:** scoped 2026-05-31. Prompt: `ai/tasks/task-0036.md`.
- **Milestone:** C5 — Catalog CLI surface.
- **Scope (one PR):** `cmd/orun/catalog.go` root + dispatch, plus the
  first two subcommands that form one coherent write→read slice —
  `orun catalog refresh` (resolve→build→persist the
  `(SourceSnapshot, CatalogSnapshot)` pair via the live
  `sourcectx` → `catalogresolve.BuildCatalog` → `catalogstore.Write*`
  path; idempotent reuse; dirty banner; `--source`/`--catalog-source`/
  `--catalog-snapshot`/`--strict`/`--no-infer`/`--json`/`--sync`; §2
  exit codes) and `orun catalog refresh refs` → `orun catalog refs`
  (enumerate all refs + `authoritative`; `--json`). Shared plumbing:
  `RefSelector` parser, the §11 JSON envelope writer (`kind` ∈
  `{CatalogRefreshResult, CatalogRefsResult}`), and help fixtures.
- **TWO NEW SEAMS are the crux** (decide home + justify in report):
  1. **Persistence-bundle assembly** — `CatalogView` →
     `CatalogGraphs` / `CatalogLocalIndexes` / `GlobalIndexUpdate` /
     `RefUpdate`. No helper exists today; recommended home
     `internal/catalogresolve` (owns the resolved shapes). Must be a
     pure, unit-tested helper, not untested cobra `RunE` glue.
  2. **Ref enumeration** — the `Resolver` interface resolves
     per-selector but cannot LIST. Add a typed `ListRefs`-style reader
     over `statestore` directory reads. **No raw `os`/`io/ioutil`/
     `path/filepath` imports in `internal/catalogstore/`** (existing
     no-raw-FS lint must stay green); mirror §4 path conventions.
  Both seams are reused by later C5 commands (`list` reuses enumeration),
  so their shapes are the leverage of this task.
- **Non-goals:** no `list`/`describe`/`tree`/`history`/`validate` (later
  C5 PRs), no `diff` (C8), no `plan`/`run` integration (C6/C7), no
  `AppendComponentEvent` (history is C7), no networking (`--sync` only
  prints `remote sync not configured`).
- **Coverage floors:** 90 % on `catalogstore` / `catalogresolve` /
  `sourcectx`; any new `internal/` package declares + meets ≥ 90 %.
  Phase-1 floors held byte-for-byte.
- **On implementer-pass:** scope Task 0037 = C5 PR-1 verifier (verify PR,
  adjudicate coverage, merge). On merge, the C5 PR-2 surface
  (`list`/`describe` + later `tree`/`history`/`validate`) unlocks.

## Just Completed — Task 0033 (C4 PR-2 verifier — PR #174 MERGED)
- **Status:** ✅ Verifier PASS with path-(a) attached coverage fix. PR
  #174 squash-merged into `main` at `73c6e8e1` on 2026-05-31T11:30:39Z.
  Reports:
  - Implementer: `ai/reports/task-0032-implementer.md`
  - Verifier: `ai/reports/task-0033-verifier.md`
- **Coverage adjudication outcome:** Path (c) → path (a) verifier-
  attached fix. Implementer landed at 85.3 % (HARD FAIL vs 90 % floor);
  verifier added 28 focused tests across two files —
  `internal/catalogstore/writer_test.go` (extended `spyStore` with
  `readErr` / `casErr` / `createNStdE` / `writeErr` one-shot injection
  maps) and new `internal/catalogstore/verifier_coverage_test.go` —
  lifting coverage to **90.1 %** (484/537 statements, +4.8 pp). Cleared
  the floor; landed inside the 90–91 % "PASS with note" band.
- **Branches covered by attached tests:** WriteRefs scope arms
  (catalog-only nil-source closure, Authoritative D.3, Branch D.4, PR
  D.5, branch sanitised-to-empty rejection); WriteRefs / WriteGlobalIndexes
  / AppendComponentEvent error paths (CreateIfAbsent non-Exists,
  post-Exists Read, CAS non-Conflict, decode failures, Write failures);
  allocateEventSeq corrupt envelope (next=0, unparseable JSON);
  validation rejection paths (invalid component names, branch
  sanitised to empty, malformed snapshot/catalog keys);
  WriteCatalogSnapshot manifest path validation, manifest non-Exists
  / post-Exists Read errors.
- **Branches LEFT uncovered (documented in verifier report §"Branches
  Left Unexercised"):** `paths.go` first-line CatalogDir error
  returns inside path builders (unreachable through public Store API
  thanks to upstream `preflightCatalogInputs`); `events.go:98`
  `componentKeyTail` no-slash return (gated by `ValidateComponentKey`);
  `writer.go:23,27` source path/encode error (gated by
  `ValidateSourceKey`, `PrettyEncode` cannot fail on validated input);
  `refs.go:54-129` closure pathFn / encode error returns (closures
  receive validated keys); `refs.go:176-181` re-Read-after-conflict
  error (symmetric to covered post-Exists Read; would require
  Read-after-N-calls hook on spy); `events.go:153-159` seq.lock
  encode error (fixed-shape struct); `writer.go:239-244`
  writeLocalIndexes PrettyEncode error (caller-supplied JSON-marshalable
  shapes). All are defensive returns guarded by upstream validation
  that runs first; carrying them at zero coverage rather than
  introducing white-box hooks keeps the test suite using only the
  public `Store` interface.
- **Static guards held:** `go vet`, `go build`, `go test -race
  -count=1 ./...`, no raw FS imports in `internal/catalogstore/`
  (verified `grep -RE '^\s*"(os|io/ioutil|path/filepath)"'` empty).
- **Spec drift call-out:** retry-budget 8 → 16 deviation noted in
  implementer's inline comments; verifier did NOT escalate to a spec
  proposal. Acceptable: spec wording is advisory, implementer
  documented justification, behaviour is harmless under
  single-writer Phase 2. Re-evaluate at C9 / Phase 3 when remote
  drivers and concurrency widen.
- **Adjacent floors held:** statestore 95.7 %, revision 90.3 %,
  executionstate 90.0 %, catalogmodel 91.1 %, sourcectx 91.1 %,
  catalogresolve 90.9 % — all byte-for-byte vs PR-1 close.
- **Post-merge main CI:** `Orun Plan` (run 26711390108), `CI` (run
  26711390118), `state-redesign-tests` (run 26711390118) all PASS.

## Current Task — Task 0035 (C4 PR-3 verifier — PR #175)
- **Status:** scoped 2026-05-31, awaiting verifier.
- **Prompt:** `ai/tasks/task-0035-verifier.md`
- **Verifying:** PR **#175** (Task 0034 implementer, C4 PR-3 read-side
  Resolver + RebuildIndexes), branch
  `task-0034-catalogstore-c4-pr3-resolver`. OPEN, all CI green,
  mergeStateStatus CLEAN. Implementer report:
  `ai/reports/task-0034-implementer.md`.
- **Implementer outcome (Task 0034):** all five Resolver methods +
  `RebuildIndexes` shipped; T-STORE-3 scrub-then-rebuild byte-identity
  test in place; zero `ErrNotImplemented` stubs claimed remaining.
  CI green / CLEAN. The `test` job flapped red once on an
  `executionstate` coverage measurement (89.6 % vs 90.0 % floor — a
  Phase-1 package PR-3 does not touch); rerun went green.
- **Central adjudication this verifier owns:** `internal/catalogstore`
  coverage landed at **88.9 %** — BELOW the 90 % floor / 91 % target.
  `internal/catalogstore` has **no CI coverage gate**, so green CI did
  NOT enforce the floor. Low funcs are the new `rebuild.go` bodies
  (rebuildSource/CatalogGlobalIndex, writeComponentGlobalIndexPlain,
  listAllSources, collectAllCatalogs — 72–79 %). Verifier re-measures
  and adjudicates per the three-branch policy (mirror Task 0033 PR-2):
  prefer path-(a) attach-tests-and-lift to ≥ 90 % (the gap is reachable
  rebuild logic, not defensive guards); PASS-with-note only if the
  residual is genuinely unreachable; FAIL → Task 0035.1 implementer-fix.
- **Also verifies:** §4 reader fallback ladder code path (refs→walk→typed
  sentinel; `ErrComponentNotFound` vs `ErrCatalogNotFound`; `errors.Is`
  chain; ctx cancellation); T-STORE-3 byte-identity across all three
  index kinds; no-raw-FS guard; implementer's 3 open questions.
- **On PASS + merge:** Milestone **C4 CLOSES**; **C5 (`orun catalog *`
  CLI)** becomes the active milestone.

## Next Task After 0035
- **Task 0036 (C5 PR-1 implementer)** — catalog CLI surface per
  `cli-surface.md`: `orun catalog refresh|list|describe|refs` (suggested
  first of two C5 PRs; `tree|history|validate` the second, `diff`
  stubbed for C8). Scoped only after PR #175 merges and C4 closes.
- **Carry-forward R-008:** `internal/executionstate` sits at exactly
  90.0 % (zero margin) and flapped to 89.6 % on one CI run. Latent
  CI-stability risk. If it reds again, scope a micro-task to add 2–4
  buffer tests OR widen the Makefile gate tolerance. Do NOT fold into
  any catalog PR.

## Just Completed — Task 0032 (C4 PR-2 implementer — PR #174 MERGED via Task 0033)
- **Status:** ✅ Implementer pass complete; merged via Task 0033
  verifier-attached coverage fix. See "Just Completed — Task 0033"
  block above for the merge details and coverage adjudication.

## Next Task After Task 0033
- **Task 0034 — C4 PR-3 implementer (resolver + fallback chain).**
  Final C4 PR. Surface: `resolver.go` implementing all 5 Resolver
  methods (`ResolveCurrentSource`, `ResolveSource`, `ResolveCatalog`,
  `ResolveComponent`, `ResolveComponentLatest`) per
  `catalog-store.md` §4, `current → latest → main` fallback chain,
  `RebuildIndexes` (rebuild local + global indexes from authoritative
  manifests). Closes Milestone C4. Unlocks **C5 (CLI surface)** —
  the new `orun catalog *` subcommands per `cli-surface.md`.
- Coverage gate stays at 90 % floor on `internal/catalogstore`;
  target re-asserted at ≥ 91 %.

## Just Completed — Task 0028 + 0029 (C3 — `CatalogSnapshot` + graph builder + `catalogHash`)
- **Status:** ✅ Verified PASS (Task 0029) and merged via PR #172 (squash
  commit `75082ca`) on 2026-05-31. Reports:
  - Implementer: `ai/reports/task-0028-implementer.md`
  - Verifier: `ai/reports/task-0029-verifier.md`
- **Outcome on `main`:** `internal/catalogresolve` gained the post-
  resolution layer wiring `resolution-pipeline.md` §1 stages 11
  (graph build) → 12 (`catalogHash`) → 13 (snapshot assemble + key
  derive + Source-block back-fill). New exported entry point:
  `BuildCatalog(ctx, opts, ResolverInputs) (*CatalogView,
  []ValidationIssue, error)`. `CatalogView` carries the existing
  `*ResolvedCatalog` plus `*CatalogSnapshot` and `[]*CatalogGraph`.
  Five `CatalogGraph` siblings (`dependencies`, `systems`, `apis`,
  `resources`, `owners`) emitted in the §9 fixed order with sorted
  nodes-by-`key` and edges-by-`(from, to, type, optional)`.
  `ResolverInputs` is fully caller-supplied; missing fields produce a
  typed `ErrResolverInputsIncomplete` (extractable via
  `IsResolverInputsIncomplete(err)`). Three new files
  (`graph.go`, `catalog_hash.go`, `catalog_snapshot.go`) plus their
  test siblings; convention from C2 PR-1 honoured (no edits to
  existing source files).
- **Properties proven:**
  1. T-IDK-1 — 1000 random orderings of the manifest input bundle ⇒
     identical `catalogHash` (rapid).
  2. `metadata.owner` edit changes both `manifestHash` AND `catalogHash`
     (deterministic backstop).
  3. `resolution.inheritedFrom` (provenance-only) edit does NOT change
     `manifestHash` AND does NOT change `catalogHash` (T-IDK-2 propagated
     through to the C3 layer).
  4. Two consecutive `BuildCatalog` calls produce byte-identical
     canonical-encoded `(*CatalogSnapshot, []*CatalogGraph)` after
     clearing the per-call ULID `CatalogSnapshotID`.
  5. `summary.*` counts equal sorted-distinct enumeration (components /
     systems / apis / resources / owners / domains).
  6. `catalogSnapshotKey` matches `^cat-[a-f0-9]{6,16}$` (width 8 default;
     collision policy `-x<n>` left to C4 writer).
  7. `manifestHash` invariant held: Source block fully excluded from the
     hashed payload (`hash.go`), so the post-stage-13 back-fill of
     `Source.{SourceSnapshotKey, CatalogSnapshotKey, HeadRevision,
     TreeHash, WorkingTree}` is safe by construction.
- **Coverage floors on main:** `internal/catalogresolve` **90.9 %**
  (90.2 → 90.9, +0.7 pp); Phase 2 floors held byte-for-byte
  (catalogmodel 91.1 %, sourcectx 91.1 %, Sanitize* 100 %); Phase 1
  floors held (statestore 95.7 %, revision 90.3 %, executionstate
  90.0 %).
- **Local gates on main:** `go build`, `go vet`, `go test ./... -race`,
  `make test-state-redesign`, `make verify-generated` all green.
- **Risk note:** the C3 layer trusts the caller to compute
  `Authoritative` / `Preview` correctly (no zero-value sentinel for
  booleans). The C4 writer is the next guardrail (`authoritative=true`
  must imply `workingTree=clean` per data-model §2).

## Repo Checkpoint

| Attribute | Value |
|---|---|
| Branch (local checkout) | `main` |
| `main` tip | `96e3bbd` — Tasks 0036/0037 / C5 PR-1 (PR #176) merged 2026-05-31T15:17:22Z |
| Open PRs | (none) |
| Repo health | 🟢 Green — `internal/catalogstore` at 90.2 % (floor held); C5 PR-1 CLI surface live |
| Last verified | 2026-05-31 (Task 0037 verifier PASS, PR #176 merged, C5 PR-1 CLOSED) |
| Active phase | Phase 2 (orun-component-catalog) |
| Active milestone | C5 (Catalog CLI surface) — PR-1 ✅ CLOSED; PR-2 next |
| Tasks completed | 0001–0005, 0007–0016, 0018–0037 (35 total) |
| Current task | (none — between cycles) |
| Next task | Task 0038 (C5 PR-2 implementer: `orun catalog list\|describe\|tree\|history\|validate`) |

## Milestone Sequence (C0 → C9)
| C  | Status | Goal |
|----|--------|------|
| C0 | ✅ done (PR #168 / `7f3f2bf`) | Spec foundation + pure data models |
| C1 | ✅ done (PR #169 / `b50d799`) | `internal/sourcectx` resolver |
| C2 | ✅ done (Tasks 0025 + 0026 / PRs #170 + #171 / `723be32` + `74b88e0`) | `internal/catalogresolve` — discovery + manifest resolver |
| C3 | ✅ done (Task 0028 / PR #172 / `75082ca`) | `CatalogSnapshot` + graph builder + `catalogHash` |
| C4 | ✅ done (Tasks 0030+0032+0034 / PRs #173+#174+#175 / `c2d7b9d`+`73c6e8e`+`42eace5`) | `internal/catalogstore` Writer + Resolver atomic writes |
| C5 | ◐ PR-1 done (Tasks 0036+0037 / PR #176 / `96e3bbd`) | Catalog CLI surface — PR-2 (`list`/`describe`/`tree`/`history`/`validate`) next |
| C6 |       | `orun plan` integration |
| C7 |       | `orun run` integration + history events |
| C8 |       | `internal/catalogdiff` + validate + rebuild |
| C9 |       | `internal/catalogsync` seam (`Syncer` + `NoopSyncer` ONLY — no HTTP, no auth) |

Phase 1 invariants preserved: do not rename Phase 1 fields, do not
lower coverage floors (`internal/statestore` 95.7 %, `internal/revision`
90.3 %, `internal/executionstate` 90.0 %), do not remove preserved
Phase 1 CLI workflows. Phase 2 floors held: `internal/catalogmodel`
91.1 %, `internal/sourcectx` 91.1 %, Sanitize* 100 %.

## Just Completed — Task 0024 (C1 resolver)
- **Status:** ✅ Verified PASS and merged via PR #169 (squash commit
  `b50d799`) on 2026-05-31. Reports:
  - Implementer: `ai/reports/task-0024-implementer.md`
  - Verifier: `ai/reports/task-0024-verifier.md`
- **Outcome on `main`:** `internal/sourcectx` resolver shipped with
  `ResolveSourceSnapshot(ctx, opts)`, Git/Clock/Filesystem adapters,
  `WorkspaceState` populated with `headRevision`, `treeHash`,
  `dirtyHash`, `catalogInputHash`. T-IDK-3 (key stability across
  random orderings) and T-IDK-4 (non-catalog files don't change
  `dirtyHash`) ship as property tests.
- **Verifier-attached fix:** added
  `internal/catalogmodel/coverage_test.go::TestCanonicalEncodeStringEscapeBranches`
  to deterministically pin the C0 catalogmodel coverage floor — rapid
  generators were probabilistically missing `\b` / `\f` escape branches
  in `writeQuotedString`, dropping coverage from 90.2 % to 87.9 % on
  some seeds. Post-fix: catalogmodel 91.1 % × 19 / 90.6 % × 1 across
  20 runs.
- **Local gates on main:** `go build`, `go vet`, `go test ./... -race`,
  `make test-state-redesign`, `make verify-generated`,
  `kiox -- orun validate --intent intent.yaml` all green.

## Just Completed — Task 0025 (C2 PR-1 discover/load/inherit)
- **Status:** ✅ Verified PASS and merged via PR #170 (squash commit
  `723be32`) on 2026-05-31T07:06:29Z. Reports:
  - Implementer: `ai/reports/task-0025-implementer.md`
  - Verifier: `ai/reports/task-0025-verifier.md`
  - Spec proposal: `ai/proposals/task-0025-spec-update.md`
- **Outcome on `main`:** `internal/catalogresolve` online with
  `DiscoverAndLoad(ctx, Options) (DiscoveryResult, error)`. Walks the
  workspace (default excludes: `.git .orun build dist node_modules
  vendor`, intent excludes appended), loads + Draft-7 schema-validates
  authored `component.yaml` / `component.yml` manifests (mixed-extension
  in same dir is a typed error), applies intent `catalog.defaults`
  inheritance (scalar-zero / per-key-map / wholesale-list rules), and
  emits a deterministic sorted `[]AuthoredManifest` with RFC 6901
  provenance pointers. Mini-T-RES-1 asserted in `resolve_test.go`.
- **Coverage floors on main:** `internal/catalogresolve` **90.0 %** (exact
  gate, deterministic across 3 local + CI runs); `internal/catalogmodel`
  91.1 %, `internal/sourcectx` 91.1 %, Sanitize* 100 %; Phase 1 floors
  byte-for-byte (statestore 95.7 %, revision 90.3 %, executionstate 90.0 %).
- **Verifier-accepted call-outs:**
  1. Additive `internal/catalogmodel/schema_embed.go` (18 lines,
     `//go:embed`-only) ACCEPTED — `//go:embed` cannot escape its
     package, vendoring is forbidden by spec. **Convention adopted
     (load-bearing for Phase 2):** *"One additive file per cross-package
     contract surface in `internal/catalogmodel/`. No edits to existing
     source files. Each additive file is `//go:embed`-only or a small
     read-only typed view — no logic."*
  2. `catalogresolve` 90.0 % no-headroom ACCEPTED WITH RISK NOTE —
     deterministic (no rapid-driven variance), CI matches local
     byte-for-byte; Task 0026 PR-2 will naturally raise the floor.
- **Spec proposal:** `ai/proposals/task-0025-spec-update.md` tightens
  the C2 PR-Boundary wording to *"No edits to **existing source files
  in** `internal/catalogmodel/` or `internal/sourcectx/`. Additive
  sibling files (embed-only exports, small read-only typed views)
  needed by dependent packages are permitted; one additive file per
  cross-package contract surface, no logic."* Fold into Task 0026
  prompt at scope time.
- **Local gates on main:** `go build`, `go vet`, `go test ./... -race`,
  `make test-state-redesign` ×3, `make verify-generated`, `kiox -- orun
  validate --intent intent.yaml`, `go test -count=10 -race
  ./internal/catalogresolve/...` all green.

## Just Completed — Task 0026 (C2 PR-2 infer + deps + validate + manifestHash)
- **Status:** ✅ Verified PASS (Task 0027) and merged via PR #171 (squash
  commit `74b88e0`) on 2026-05-31T08:36:04Z. Reports:
  - Implementer: `ai/reports/task-0026-implementer.md`
  - Verifier: `ai/reports/task-0027-verifier.md`
- **Outcome on `main`:** top-level
  `Resolve(ctx, opts) (*ResolvedCatalog, []ValidationIssue, error)` covering
  resolution-pipeline stages 4 (infer), 5/6 (validate), 7 (assemble),
  8 (deps), 9 (validate post-deps), 10 (`manifestHash`). New files in
  `internal/catalogresolve/`: `assemble.go`, `clock.go`, `dependencies.go`,
  `errors.go`, `hash.go`, `infer.go`, `resolve_full.go`, `validate.go`,
  `resolve_full_test.go`, `testdata/resolve_e2e/`, `testdata/resolve_cycle/`.
  Additive edits to `intent.go` (intentInference pointer-mirror) and
  `types.go` (+`ResolvedCatalog`; +`Options.{Strict,Repo,Namespace,Clock}`).
  No edits outside `internal/catalogresolve/`.
- **Coverage floors on main:** `internal/catalogresolve` **90.2%**
  (gate ≥ 90%, +0.2pp headroom over PR-1's exact 90.0%); Phase 2 floors
  held byte-for-byte (catalogmodel 91.1%, sourcectx 91.1%, Sanitize* 100%);
  Phase 1 floors held (statestore 95.7%, revision 90.3%, executionstate 90.0%).
- **Properties proven:**
  1. T-RES-1 — `Resolve` × 2 on a fixture produces byte-identical
     canonical encodings AND identical per-component `manifestHash` values.
  2. T-RES-2 — every `inheritedFrom` / `inferredFrom` pointer references a
     real authored / inferred origin in the fixture.
  3. `manifestHash` is provenance-invariant: flipping
     `resolution.inheritedFrom` does NOT change the hash; spec edits DO.
     Computed via `catalogmodel.CanonicalEncode` over
     `{identity, metadata, spec, runtime}`.
  4. `ErrDependencyMissing` carries both endpoints (`From`, `To`).
  5. `deploy-after` cycles error always; `calls` cycles warn (default) or
     error (strict).
  6. Inference is `recover()`-safe — failures emit warn-severity
     `ErrInferenceFailed` and skip rather than panic.
- **Determinism stress on main:** `go test -count=10 -race
  ./internal/catalogresolve/...` zero failures.
- **Local gates on main:** `go build`, `go vet`, `go test ./... -race`,
  `make test-state-redesign`, `make verify-generated`, `kiox -- orun
  validate / plan --changed / run --dry-run` all green.

## Current Task (none — between cycles)
- C2 closed; C3 awaiting orchestrator scope as Task 0028.

## Next Task After 0027 — Task 0028 (C3 implementer)
- **Milestone:** C3 — `CatalogSnapshot` and graph builder (single PR
  per `implementation-plan.md` §C3).
- **Adds:** `internal/catalogresolve/graph.go` building `dependencies`,
  `systems`, `apis`, `resources`, `owners` graphs;
  `internal/catalogresolve/resolver.go` (or extension of `resolve_full.go`)
  surfacing `ResolvedCatalog` with `CatalogGraph`, `summary.*` counts
  from sorted collections, and `catalogHash` per `identity-and-keys.md`
  §9 (inputs: `catalogInputHash` + sorted `(componentKey, manifestHash)`
  pairs + canonical `CatalogGraph` + `resolver.resolverVersion`).
- **"Done when":** T-IDK-1 (same source + inputs ⇒ same `catalogHash`);
  `metadata.owner` edit changes `catalogHash`; `resolution.inheritedFrom`
  edit does NOT change `manifestHash` (already proven by Task 0026 —
  verifier confirms this still holds); graph files byte-stable across
  runs.
- **Spec sources:** `implementation-plan.md` §C3, `resolution-pipeline.md`
  §1 + §7, `identity-and-keys.md` §9 + §10, `data-model.md` §3 + §6 + §7.

## Repo Checkpoint (historical — superseded by Phase 2 C3 close above)

| Attribute | Value |
|---|---|
| `main` tip after C2 close | `74b88e0` — Task 0026 / C2 PR-2 (PR #171) on 2026-05-31T08:36:04Z |
| Tasks completed at C2 close | 0001–0005, 0007–0016, 0018–0027 (25 total) |

---

# Past Phase — orun-state-redesign (Phase 1, COMPLETE)

Phase 1 (`specs/orun-state-redesign/`, M0–M6) closed via PR #165
(`ad3656e`) on 2026-05-30 with release-notes PR #166 (`b4178dd`)
on 2026-05-31. Coverage floors on `main` at phase close:
`internal/statestore` 95.7 %, `internal/revision` 90.3 %,
`internal/executionstate` 90.0 %.

| M  | PR    | Main commit |
|----|-------|-------------|
| M0 | #152  | `4ea1980`   |
| M1 | #153  | `db342dd`   |
| M2 | #156  | `cd8b3e8`   |
| M3 | #158  | `bfc2ae6`   |
| M4 | #159 / #160 | `ed48633` / `d51e828` |
| M5 | #161–#164 | `7a9c494` … `17ef788` |
| M6 | #165  | `ad3656e`   |

Phase 1 carry-forward (candidates for follow-on within Phase 2 scope,
NOT yet wired): MirrorModeHardlink debug-fold decision,
RunnerHooks.AfterStateUpdate async-mirror evaluation, `--persist-revision`
flag wiring, Option B trigger-name resolver branch
(`ai/proposals/task-0019-spec-update.md`), `--prune-legacy`. None of
these block Phase 2.

## Known Spec Drift / Open Questions (Phase 1 carry-forward)
- **`MirrorMode` trinary surface** (Task 0015 adjudicated, accepted with Risk
  Note). Reconsider when Phase 2 remote-driver wiring picks the right name.
- **`MirrorModeHardlink` is currently a test/drift-detection mode.** If no
  production caller emerges in Phase 2, fold into a debug flag.
- **Event-sequence retry budget of 32** is acceptable for single-writer
  Phase 1; re-evaluate when remote drivers come online (Phase 3).
- **Manifest required for `UpdateLatestExecutionSummary`** (Task 0013
  carry-forward). Pin normatively if any Phase 2 path needs to skip the
  manifest step.
- **`internal/executionstate` coverage at 90.0 % exact floor.** Carry-
  forward risk: small refactors deleting covered branches could trip the
  gate. Phase 2 work should bump headroom.
- **`RunnerHooks.AfterStateUpdate` fires bridge mirror synchronously on
  the runner goroutine** (Task 0018 carry-forward). Phase 2 may want to
  decide if buffered channel + dedicated goroutine is needed.
- **Task 0020 carry-forward: unknown-hash placeholder body** + `hashToRev`
  dual-keying depends on `state.PlanChecksumShort` continuing to emit
  bare-hex.
- **Task 0019 carry-forward: Option B trigger-name resolver branch**
  still open; fold into a Phase 2 milestone if/when E2E exercises it.
- **Persistent local environment quirk (NOT a regression):**
  `kiox -- orun plan --changed --intent examples/intent.yaml` fails on
  composition-cache resolution on this developer machine. Reproduced on
  every state-redesign verifier pass since Task 0014. CI is authoritative.
