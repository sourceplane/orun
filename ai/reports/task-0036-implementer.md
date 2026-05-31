# Milestone C5 PR-1 — cmd/orun catalog: refresh + refs + shared CLI foundation

Closes the C5 PR-1 surface in `specs/orun-component-catalog/cli-surface.md`:
the `orun catalog` command root, the `refresh` write-path command, the
`refs` enumeration command, the shared `RefSelector` parser, the §11 JSON
envelope, and the two new pure seams every later catalog command depends on.

Refs: `ai/tasks/task-0036.md`
Branch: `feat/c5-pr1-catalog-cli`
PR: #176
Spec: `specs/orun-component-catalog/cli-surface.md` §1 (refresh), §2 (exit
codes), §8 (refs), §11 (envelope), §12 (root index)
Builds on: PR #175 (task 0034/0035, C4 PR-3 — read+write store complete)

## Surface delivered (PR-1)

### `cmd/orun/catalog.go` — root + shared plumbing

  - `registerCatalogCommand(rootCmd)` — the `orun catalog` parent with a
    subcommand index help page (§12), registered from `commands_root.go`.
  - `writeCatalogEnvelope(kind, data, warnings)` — the stable §11 JSON
    envelope writer: `apiVersion` / `kind` / `data` + an always-present
    (never-nil) `warnings` array. `json.NewEncoder(os.Stdout)` with
    `SetIndent("", "  ")`. Reused by every `--json` subcommand.
  - `parseCatalogSelector()` — the single shared bridge from
    `--catalog-source` / `--source` / `--catalog-snapshot` to a
    `catalogstore.RefSelector`. Selector grammar (`current` | `main` |
    `latest` | `branches/<name>` | `prs/<n>` | explicit `cat-<key>` pin)
    lives in exactly one tested place; a malformed selector is surfaced as
    an exit-1 validation error.
  - Pure input-builder glue: `repoForInputs` / `shortRepoName` (single-
    segment repo for componentKey construction), `workingTreeLabel`,
    `computeCatalogAuthoritative` (clean branch-main/protected → true),
    `sourceSnapshotFromState`, `resolverInputsFromState`,
    `buildCatalogInputHash`.

### `cmd/orun/catalog_refresh.go` — `orun catalog refresh` (write path)

Drives the live pipeline end-to-end:

  1. resolve workspace VCS state via `sourcectx.ResolveSourceSnapshot`
  2. `catalogresolve.BuildCatalog(Options, ResolverInputs)` → `CatalogView`
  3. `catalogstore.AssembleBundle(BundleInputs)` → `CatalogBundle` (pure)
  4. commit in order: `WriteSourceSnapshot` → `WriteCatalogSnapshot` →
     `WriteGlobalIndexes` → `WriteRefs`

  - **Idempotent reuse**: a byte-identical re-write reports
    `created=false` / `reused=true`, prints `↺ Catalog up to date`, exits 0
    (does not error on idempotent re-run).
  - **Dirty-worktree banner**: always printed when the workspace is dirty;
    the snapshot is local-only (preview).
  - **Authoritative vs preview**: clean `branch-main` / `branch-protected`
    → authoritative; everything else → preview.
  - Flags: `--source`/`--catalog-source`, `--catalog-snapshot`,
    `--strict`/`--catalog-strict`, `--no-infer`, `--json`, `--sync`
    (Phase-2 no-op: prints `remote sync not configured`, exit 0).
  - Exit codes per §2: 0 created/reused, 1 validation, 2 schema-invalid
    manifest, 3 io/store error.

### `cmd/orun/catalog_refs.go` — `orun catalog refs` (enumeration)

  - Lists every ref (`current` / `main` / `latest` + any
    `branches/<name>`, `prs/<n>`) with the resolved `sourceSnapshotKey`,
    `catalogSnapshotKey`, `sourceScope`, and `authoritative` flag.
  - Pure read — no resolution, no writes; delegates the source/catalog
    ref-tree join to the tested `catalogstore.ListRefs` seam.
  - Empty store → empty list, exit 0 (text form prints a friendly hint).
  - `--json` emits the `CatalogRefsResult` envelope.

### `cmd/orun/main.go` — exit-code plumbing

`main()` now unwraps an `interface{ ExitCode() int }` from the returned
error (via `errors.As`) and calls `os.Exit(code)`, so the §2 exit-code
contract propagates from `exitErr(code, ...)` through cobra's `RunE`.

## Two new pure seams in `internal/catalogstore`

Architecture rule preserved — **`catalogstore` depends on `catalogresolve`,
never the reverse** (doc.go). Both seams avoid raw FS (no `os` / `filepath`
imports; all paths via `pathJoin`, all ref reads via `statestore`).

### `bundle.go` — `AssembleBundle(BundleInputs) (CatalogBundle, error)`

The single pure helper turning a resolved `CatalogView` (Source +
`*CatalogSnapshot` + `[]*ComponentManifest` + `[]*CatalogGraph`) into the
four writer-input bundles (`CatalogGraphs`, `CatalogLocalIndexes`,
`GlobalIndexUpdate`, `RefUpdate`). Performs **no I/O** — byte-identical for
identical inputs (golden-testable). Lives in `catalogstore` because it must
return `catalogstore` types and `catalogresolve` may not import
`catalogstore`.

### `listrefs.go` — `ListRefs(ctx, statestore) ([]RefListing, error)`

Joins the `refs/sources/*` and `refs/catalogs/*` trees by ref name via
`statestore.List`/`Read`, returns a name-sorted `[]RefListing`. Empty store
→ empty slice, nil error.

## Tests

### `cmd/orun/catalog_test.go` (new)

  - `TestParseCatalogSelector_Forms` — every selector shape
    (empty→current, current, main, latest, branches/<name>, prs/<n>,
    cat-<key> pin) maps to the right `RefSelector`.
  - `TestParseCatalogSelector_MalformedExit1` — `branches/` → exit code 1.
  - `TestWriteCatalogEnvelope_Shape` — envelope is valid JSON with the
    right apiVersion/kind/data and a non-nil `warnings` array.
  - `TestComputeCatalogAuthoritative` — clean main/protected → true; dirty
    main, feature, pr, local-dirty → false.
  - `TestShortRepoName` / `TestWorkingTreeLabel` — pure helper mapping.
  - `TestCatalogRefresh_E2E_CreatedThenReused` — seeds a real git workspace
    (origin remote + main branch + one valid component), asserts
    `created=true` first run then `reused=true` (same `cat-` key) on the
    second; authoritative; 1 component.
  - `TestCatalogRefs_E2E_AfterRefresh` — refresh then refs: current/main/
    latest present, non-empty keys, authoritative.
  - `TestCatalogRefs_E2E_EmptyStore` — fresh store → empty refs list.
  - `TestCatalogRefresh_SyncNoop` — `--sync` prints the documented line.

### `internal/catalogstore/seams_test.go` (new)

  - `TestParseRefSelector_Forms` / `_Malformed` — selector grammar at the
    seam level.
  - `TestAssembleBundle_RequiresSnapshot` / `_RequiresUpdatedAt` /
    `_HappyPath` / `_Deterministic` / `_BranchAndPRScopeCarried` — purity,
    determinism (byte-identical on re-run), and scope-label carry.
  - `TestListRefs_Empty` / `_JoinsSourceAndCatalog` / `_SortedByName`.

## Validation

  - `go build ./...` — exit 0
  - `go vet ./cmd/orun ./internal/catalogstore ./internal/catalogresolve
    ./internal/sourcectx` — clean
  - `go test -race -count=1` on all four packages — PASS
  - `make verify-generated` — generated artifacts up-to-date
  - No raw FS in `internal/catalogstore` (grep for `os.`/`filepath.` in
    non-test files → only `paths.go`)
  - `internal/catalogstore` coverage 90.2% (floor held)
  - Manual smoke against a seeded git workspace: created → reused → refs →
    feature-branch preview (branch ref written) → dirty banner +
    local-only → invalid selector exit 1 → sync no-op exit 0

## No secrets

  - [x] No credentials introduced
  - [x] No log lines emit user/token/key material
  - [x] Test fixtures use literal `svc-a` / `team/x` / synthetic git repo

## Open questions for verifier

  1. **Selector flags on `refresh`**: refresh always resolves the *current*
     workspace, so `--source`/`--catalog-snapshot` are not used to pick a
     snapshot there — but a malformed value still fails fast (exit 1) rather
     than being silently ignored. Verifier may confirm this matches §1/§2
     intent (the flags exist on refresh per the task's flag list).

  2. **CatalogLocalIndexes scope**: `buildLocalIndexes` emits one
     ComponentExecutionIndex per manifest with an empty `executions[]`
     (history events are a C7 concern). The owner/system/domain/type
     catalog-local index axes are left empty because data-model.md §9 only
     fully specifies §9.2's component index. Flagged in `bundle.go` and
     `ai/proposals/task-0036-spec-update.md` (if filed).

  3. **`refs` output ordering**: sorted by ref name ascending. The
     canonical `current`/`latest`/`main` and `branches/*`/`prs/*` therefore
     interleave alphabetically rather than grouping canonical-first.
     Verifier may confirm §8 does not mandate a grouped order.

## Next steps for verifier

  1. Spec read: `cli-surface.md` §1, §2, §8, §11, §12.
  2. Code read: `cmd/orun/catalog*.go` + `internal/catalogstore/{bundle,
     listrefs,selector}.go` and the new tests.
  3. Smoke: `go test ./cmd/orun/... ./internal/catalogstore/... -race
     -count=1` should pass clean.
  4. Coverage: re-measure `internal/catalogstore` (floor 90.2%) plus
     `cmd/orun` for the new files; path-(a) attach tests if a floor slips.
  5. Confirm the two seams' homes preserve the no-raw-FS and
     catalogstore→catalogresolve dependency invariants.
  6. Confirm PR #176 closes C5 PR-1; C5 PR-2 (list/describe/tree/history/
     validate) reuses the shared parser + envelope + both seams.

## References

  - `specs/orun-component-catalog/cli-surface.md` — §1/§2/§8/§11/§12
  - `specs/orun-component-catalog/data-model.md` §9 — index shapes
  - `internal/catalogstore/store.go` — Writer/Resolver contract reused
  - `internal/catalogresolve/catalog_snapshot.go` — BuildCatalog reused
  - `internal/sourcectx/resolve.go` — ResolveSourceSnapshot reused
  - PR #175 — predecessor C4 PR-3

Closes Task 0036.
