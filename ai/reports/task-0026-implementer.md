# Task 0026 Implementer Report — C2 PR-2 (`internal/catalogresolve` infer + deps + validate + manifestHash)

- **Agent:** Implementer
- **Task:** 0026
- **Milestone:** C2 (Phase 2 — `specs/orun-component-catalog/`) — second / closing PR
- **Branch:** `task-0026-catalogresolve-c2-pr2`
- **PR Number:** #171

## Summary

- Closes Milestone C2 by lifting `internal/catalogresolve` from
  discover/load/inherit (Task 0025) to the full `Resolve(ctx, opts)` top-level
  entry covering resolution-pipeline.md stages 4 (infer), 5/6 (validate),
  7 (assemble), 8 (deps), 9 (validate post-deps), 10 (manifestHash).
- Adds `infer.go` (gated by `intent.catalog.inference.*`; never panics —
  failures emit a `warn`-severity `ErrInferenceFailed` issue and skip),
  `dependencies.go` (component / API / resource ref resolution with
  cross-repo handling, `deploy-after` cycle = error always, `calls` cycle =
  warn / error in strict), `validate.go` (typed issue collection,
  strict-mode promotion of warns → errors), `assemble.go` (sorted
  `[]ComponentManifest` build with provenance carried through),
  `hash.go` (canonical-encode driven `manifestHash` per
  `identity-and-keys.md` §10), and `errors.go` (typed `ErrDuplicateComponent`,
  `ErrDependencyMissing`, `ErrCycle`, `ErrInferenceFailed`).
- Implements T-RES-1 (byte-identical `Resolve` × 2 on a fixture),
  T-RES-2 (every `inheritedFrom` / `inferredFrom` pointer references a
  real authored or inferred origin), and the `manifestHash` stability
  invariant (provenance-only changes do NOT change `manifestHash`; spec
  edits DO change it) as deterministic table-driven tests in
  `resolve_full_test.go`.
- Follows the Task 0025 verifier-accepted convention: no edits to existing
  source files in `internal/catalogmodel/` or `internal/sourcectx/`. The
  only mutations to existing source files are within
  `internal/catalogresolve/` itself (intent struct gains `Inference`
  block; types.go gains `ResolvedCatalog` + `Options.{Strict,Repo,Namespace,Clock}`).
- Local quality gates green: `go build`, `go vet`, `go test -race -count=3
  ./internal/catalogresolve/...`, `go test -race -count=1 ./...`,
  `make test-state-redesign`, `make verify-generated`. Coverage on
  `internal/catalogresolve` measured **90.2 %** (gate ≥ 90 %).

## Files Changed

### `internal/catalogresolve/` — production

- `assemble.go` (new, 128 LOC) — sorted `[]ComponentManifest` build
  honouring `Identity.ComponentKey` ordering with provenance carried from
  authored / inherited / inferred origins.
- `clock.go` (new, 32 LOC) — `Clock` seam (`time.Now` injection point);
  resolver itself does not call `time.Now`, reserved for inference layers
  that want to stamp scanned-at fields.
- `dependencies.go` (new, 224 LOC) — stage-8 cross-component / API /
  resource ref resolution. Cross-repo dependency keys are matched only
  when the namespace+repo+name triple resolves inside the discovered set;
  otherwise → `ErrDependencyMissing` carrying `{From, To}`. Cycle
  detection: `deploy-after` = error always; `calls` = warn (default) or
  error (strict).
- `errors.go` (new, 163 LOC) — typed `ErrDuplicateComponent`,
  `ErrDependencyMissing`, `ErrCycle`, `ErrInferenceFailed` with
  `errors.Is`/`errors.As`-friendly `Unwrap()` chains.
- `hash.go` (new, 39 LOC) — `manifestHash(*ComponentManifest)` per
  `identity-and-keys.md` §10. Built on `catalogmodel.CanonicalEncode` so
  the hash sees only spec content (provenance fields excluded by the
  encoder mask).
- `infer.go` (new, 341 LOC) — stage-4 inference layer behind
  `intent.catalog.inference.*` toggles. Each detector
  (`packageJson`, `dockerfile`, `terraform`, `helm`, `readme`) is wrapped
  in `recover()`-safe execution: a panic or error inside any single
  detector emits a `warn`-severity `ErrInferenceFailed` issue and skips,
  never aborts the run.
- `resolve_full.go` (new, 147 LOC) — top-level `Resolve(ctx, Options) →
  (*ResolvedCatalog, []ValidationIssue, error)`. Orchestrates discover →
  load → inherit → infer → assemble → deps → validate → manifestHash in
  the order documented in `resolution-pipeline.md` §1. Strict mode flips
  warn → error and aborts on the first non-fatal issue.
- `validate.go` (new, 173 LOC) — typed issue collection
  (`ValidationIssue{Severity, Code, Message, File, Pointer}`), sorting
  (severity desc → code → file → pointer), `hasError`/`firstError`
  helpers, strict-mode promotion.
- `intent.go` — `+14 LOC` — adds `intentInference` mirror struct under
  `intentCatalogBlock.Inference` (pointer fields so absent ≠ false).
- `types.go` — `+63 LOC` — adds `ResolvedCatalog` carrier and extends
  `Options` with `Strict`, `Repo`, `Namespace`, `Clock`. Doc-comment
  updated to clarify Options now drives both `DiscoverAndLoad` and
  `Resolve`.

### `internal/catalogresolve/` — tests

- `resolve_full_test.go` (new, 467 LOC) — covers:
  - `TestResolve_E2E_HappyPath` — full pipeline fixture under
    `testdata/resolve_e2e/`; asserts every manifest has
    `Source.ManifestHash` with `sha256:` prefix.
  - `TestResolve_Determinism` — T-RES-1: two consecutive `Resolve`
    calls produce byte-identical canonical encodings AND identical
    `manifestHash` values per component.
  - `TestResolve_ProvenanceDoesNotChangeManifestHash` — flips
    `resolution.inheritedFrom` pointers and asserts `manifestHash` is
    unchanged.
  - `TestResolve_ManifestHashChangesOnSpecEdit` — edits a spec field and
    asserts `manifestHash` changes.
  - `TestResolve_StrictPromotesWarnings` — strict mode flips warn → error
    and aborts.
  - `TestResolve_DeployAfterCycleAlwaysError` — cycle on
    `deploy-after` edges errors regardless of strict.
  - `TestResolve_OwnerAndLifecycleWarnings_NoIntent` — default policy
    warnings without intent overrides.
  - `TestResolve_DuplicateComponentKey` — emits
    `ErrDuplicateComponent` with both file paths.
  - `TestResolve_NameInvalid_StrictPromotes` — schema-shape warn becomes
    error in strict.
  - `TestResolveInferenceConfig_DefaultsAndOverrides` — pointer-field
    defaulting (absent → on when `enabled=true`).
  - `TestResolveDepKey_Variants` — ref forms (`name`, `repo/name`,
    `namespace/repo/name`) all resolve to the same component when the
    workspace namespace+repo match.
  - Errors-package shape coverage — `errors.Is`/`errors.As` against
    `&ErrDependencyMissing{}` and `&ErrCycle{}`.

### Fixtures

- `internal/catalogresolve/testdata/resolve_e2e/` — happy-path workspace
  with intent, multiple components, inheritance + inference + deps.
- `internal/catalogresolve/testdata/resolve_cycle/` — workspace with
  intentional `deploy-after` cycle to exercise error path.

## Checks Run

- `go build ./...` — exit 0.
- `go vet ./...` — exit 0.
- `go test -race -count=3 ./internal/catalogresolve/...` — PASS, ~1.6s
  per run, no flake.
- `go test -race -count=1 ./...` — PASS across the whole module.
- `go test -coverprofile=/tmp/c2pr2.out ./internal/catalogresolve/...` →
  **90.2 %** (gate ≥ 90 %).
- `make test-state-redesign` — PASS; coverage gates:
  `internal/catalogresolve` 90.2 %, `internal/catalogmodel` 91.1 %,
  `internal/sourcectx` 91.1 %, Sanitize* 100 %, Phase 1 floors held
  byte-for-byte (statestore 95.7, revision 90.3, executionstate 90.0).
- `make verify-generated` — PASS.

## Assumptions (durable)

1. **`Options.Repo` defaults to `filepath.Base(WorkspaceRoot)`.** Cross-repo
   dependency keys (`<namespace>/<otherRepo>/<name>`) are permitted in
   `spec.dependsOn` but resolve against the discovered set only when the
   triple matches a workspace component. Otherwise →
   `ErrDependencyMissing`, never silent-skip.
2. **Inference detector safety.** Every inference detector runs under
   `recover()`. A panic OR returned error → `warn`-severity
   `ErrInferenceFailed` issue (file = inferred origin, pointer = "")
   and the detector is skipped; the rest of the pipeline continues.
   This keeps the resolver crash-free against malformed
   `package.json` / `Dockerfile` / `terraform` files.
3. **Cycle policy.** `deploy-after` cycles always error (consistent with
   `resolution-pipeline.md` §6). `calls` cycles default to warn and are
   promoted to error in strict mode.
4. **`manifestHash` input.** Computed via
   `catalogmodel.CanonicalEncode(cm)` after stage 9. The encoder masks
   provenance fields so flipping
   `cm.Resolution.InheritedFrom` / `InferredFrom` does NOT change the
   hash. Spec field edits DO. Verified by paired tests.
5. **`Clock` seam.** Reserved for stage-4 inference layers that want to
   stamp scanned-at fields. The resolver itself does not call
   `time.Now`. Nil → `defaultClock`, never panics.

## Spec Proposals

None. The Task 0025 verifier-accepted convention (one additive sibling
file per cross-package contract surface in `internal/catalogmodel/` or
`internal/sourcectx/`, no edits to existing source files) was honoured
without amendment — Task 0026 introduced no new additive files in those
two packages; all new surface area lives inside
`internal/catalogresolve/`.

## Remaining Gaps

- **Coverage at 90.2 %** — gate is ≥ 90 %, comfortable but not deep.
  `validate.go::validate` sits at 88 % (one strict-mode promotion branch
  not exercised). Acceptable for C2 close; C3 will naturally lift the
  floor as graph-builder tests exercise more paths.
- **Inference detector breadth.** Spec lists `packageJson`, `dockerfile`,
  `terraform`, `helm`, `readme`. All five are scaffolded and gated; the
  `helm` and `readme` detectors are minimal-shape best-effort and will
  be extended once C5 CLI surfaces real-world signals.

## Next Task Dependencies

- **C3 unblocks immediately** on Task 0026 merge — `CatalogSnapshot` +
  graph builder + `catalogHash` consume `ResolvedCatalog` verbatim
  (`Manifests` slice + `Issues` slice + `IntentPath` / `Namespace` /
  `Repo` carriers). No further computation required from
  `internal/catalogresolve`.
- **C4 (`internal/catalogstore`)** is one milestone away and will write
  the `ResolvedCatalog` payload through `internal/statestore`.

## PR Number

#171.
