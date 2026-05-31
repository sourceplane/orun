# Test Plan

Phase 2 ships with three test tiers, mirroring Phase 1's structure:

1. **Unit tests** colocated with each package under test (`*_test.go`).
2. **Property-based tests** using `pgregory.net/rapid` (already pinned at
   `v1.1.0` by the TUI cockpit spec).
3. **End-to-end CLI tests** under `cmd/orun/catalog_e2e_test.go`.

`make test-state-redesign` is extended (in C0) into
`make test-orun-state` covering both spec packs; the existing target
remains as an alias for backward compatibility.

---

## 1. Coverage targets

| Package | Statement coverage | Why |
|---------|--------------------|-----|
| `internal/sourcectx` | ≥ 90 % | Source key generation + dirty hash must be airtight. |
| `internal/catalogmodel` | ≥ 90 % | Pure data; sanitizers + roundtrip must be total. |
| `internal/catalogresolve` | ≥ 90 % | Resolver determinism + validation matrix. |
| `internal/catalogstore` | ≥ 90 % | Persistence contract + reader fallback. |
| `internal/catalogdiff` | ≥ 85 % | Output stability across structures. |
| `internal/catalogsync` | ≥ 80 % | Small surface; payload roundtrip + NoopSyncer. |

A coverage drop in any of these packages fails CI. Phase 1 coverage gates
remain in place unchanged.

---

## 2. Property tests

| ID | Subject | Property |
|----|---------|----------|
| T-IDK-1 | `catalogHash` | Same `(source, manifests, graphs, resolverVersion)` ⇒ identical `catalogHash` across 1 000 random orderings of inputs. |
| T-IDK-2 | `manifestHash` | Changing only `resolution.inheritedFrom` does NOT change `manifestHash`; changing any resolved value does. |
| T-IDK-3 | `SourceSnapshotKey` | Stable across 1 000 random orderings of dirty-input file lists. |
| T-IDK-4 | `dirtyHash` | Adding a non-catalog-relevant file to the workspace does NOT change `dirtyHash`. |
| T-IDK-5 | `SanitizeBranch` / `SanitizeComponentKey` / `SanitizeEventKind` | Total: any input string returns a valid filesystem segment matching the documented regex. |
| T-RES-1 | Resolver determinism | Two consecutive `Resolve(ctx, opts)` calls on the same fixture produce byte-identical `ResolvedCatalog`. |
| T-RES-2 | Provenance completeness | Every value with a non-empty `inheritedFrom` or `inferredFrom` entry has a corresponding source path that exists in the fixture. |
| T-STORE-1 | `CompareAndSwap` | 100 concurrent goroutines updating one global component index produce a final state equal to the serial-merged result. |
| T-STORE-2 | `CreateIfAbsent` idempotence | 100 concurrent calls writing byte-identical `source.json` produce one file with no errors. |
| T-STORE-3 | Index rebuild | `Resolver.RebuildIndexes(ctx)` on an arbitrary tree produces byte-identical files to a fresh resolver run. |
| T-COMPAT-1 | Phase 1 schema additivity | Every fixture under `internal/state/testdata/` decodes cleanly into the Phase 2 model; re-encoding loses no fields. |
| T-COMPAT-2 | ExecID preservation | `gh-{run_id}-{attempt}-{sha}` round-trips through the new layout unchanged. |
| T-COMPAT-3 | Compatibility alias | With `stateCompatibilityWrites = true`, `.orun/revisions/<revKey>/plan.json` is byte-identical to the canonical path. |

---

## 3. Atomicity suite (catalogstore)

Inherits the Phase 1 statestore atomicity suite plus catalog-specific cases:

```text
N := 100

// Two concurrent catalog refreshes producing the same catalog.
for i := 0; i < N; i++ {
    go writer.WriteCatalogSnapshot(ctx, src, cat, manifests, graphs, idx)
}
wait
assert: catalog.json exists exactly once with the expected body
assert: zero ErrConflict from CreateIfAbsent
assert: refs/catalogs/current.json points at cat-<expected>

// Two concurrent ComponentExecutionIndex updates on the same key.
for i := 0; i < N; i++ {
    go writer.AppendExecutionIndexEntry(ctx, key, entry{i})
}
wait
assert: index has N entries, no duplicates, no losses

// Concurrent AppendComponentEvent (sequence allocator).
for i := 0; i < N; i++ {
    go writer.AppendComponentEvent(ctx, ev{kind: "execution.completed"})
}
wait
assert: events directory has N files with sequential <seq> prefixes,
        no gaps, no duplicates
```

---

## 4. End-to-end walk

`cmd/orun/catalog_e2e_test.go` (added in C5; extended through C9):

```text
1.  Spin up isolated workspace via internal/testfx/statefs.
2.  Place intent.yaml + two component.yaml fixtures.
3.  Run `orun catalog refresh`.
4.  Assert sources/<srcKey>/source.json exists.
5.  Assert sources/<srcKey>/catalogs/<catKey>/catalog.json exists.
6.  Assert components/<name>/manifest.json for each component.
7.  Assert refs/catalogs/{current,latest}.json point at <catKey>.
8.  Run `orun catalog list --json` → shape match.
9.  Run `orun catalog describe api-edge --json` → manifest match.
10. Run `orun plan --intent intent.yaml --output plan.json`.
11. Assert revision lives under sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/.
12. Assert .orun/revisions/<revKey>/plan.json compatibility alias exists.
13. Run `orun run --plan plan.json --dry-run --runner github-actions`.
14. Assert execution under .../revisions/<revKey>/executions/<execKey>/.
15. Assert .orun/executions/<execKey>/ mirror exists (Phase 1 bridge).
16. Run `orun catalog history api-edge` → run-001 visible.
17. Mutate a component.yaml; run `orun catalog refresh`.
18. Assert new (srcKey, catKey) pair; old one preserved.
19. Run `orun catalog diff --base <oldCatKey> --head <newCatKey>` → expected
    diff.
20. Run `orun catalog validate --strict` → exits 0 on the canonical fixture.
21. Run `orun catalog refresh --sync` → exits 0 with warning.
```

Every step asserts both default text output (against a golden file) and the
shape of `--json` output where applicable.

---

## 5. Compatibility tests

Run as a sub-target of `make test-state-redesign` (legacy alias retained):

```text
- orun plan still works on a workspace with no component.yaml files
  (writes a revision with empty source/catalog keys; no resolver crash).
- orun run <legacy revKey> still works after migration with
  stateCompatibilityWrites=true.
- orun status falls back to legacy `.orun/executions/` when the global index
  is absent.
- orun describe revision latest works with a Phase 1-shaped tree.
- orun catalog list / describe / refs all return the typed
  ErrCatalogNotFound when no catalog has been refreshed.
- Phase 1 fixtures decode/encode with no field loss (T-COMPAT-1).
```

---

## 6. Help-text fixtures

Every `orun catalog *` subcommand has a `--help` golden file:

```text
cmd/orun/testdata/help/catalog/
  refresh.txt
  list.txt
  describe.txt
  tree.txt
  diff.txt
  history.txt
  refs.txt
  validate.txt
  root.txt          (orun catalog with no subcommand)
```

A failing diff fails CI. Updating fixtures is intentional — part of the PR.

---

## 7. Benchmarks

Benchmarks live under their respective packages and run nightly on CI; they
do not gate PRs but a > 2× regression vs main triggers an issue.

| Benchmark | Subject | Budget |
|-----------|---------|--------|
| `BenchmarkSourceSnapshotResolve` | `internal/sourcectx` clean tree | ≤ 30 ms |
| `BenchmarkCatalogResolve` | `internal/catalogresolve` 50-component repo | ≤ 250 ms |
| `BenchmarkCatalogWrite` | `internal/catalogstore` 50 components | ≤ 200 ms |
| `BenchmarkResolveCatalogCurrent` | `internal/catalogstore` resolver fast path with refs intact | ≤ 5 ms |
| `BenchmarkResolveCatalogFallback` | resolver fallback walk on a 1 000-revision tree | ≤ 50 ms |
| `BenchmarkOrunCatalogList` | end-to-end CLI on 50-component fixture | ≤ 400 ms |

---

## 8. CI integration

- `make test-orun-state` is the new umbrella target; runs Phase 1 + Phase 2
  packages with `-race` and the documented coverage gates. The legacy
  `make test-state-redesign` target becomes an alias to keep existing
  agent prompts working.
- A small `make test-orun-catalog` target exists for fast iteration on
  Phase 2 packages only.
- Coverage gates are checked via the same parser as Phase 1
  (`scripts/check-coverage.sh`) extended with the new package list.

---

## 9. Fixtures

Catalog fixtures live under `internal/catalogresolve/testdata/repos/`:

| Fixture | Shape | Used by |
|---------|-------|---------|
| `tiny/` | 1 component, no infer | unit tests |
| `cloudflare-stack/` | 4 components across 2 systems | E2E walk, diff golden |
| `multi-language/` | mixed TS / Go / Helm / Terraform | inference tests |
| `dirty/` | clean tree + scripted dirty mutation | dirty-hash tests |
| `pr-preview/` | branch + PR shape | preview-vs-main tests |
| `legacy-phase1/` | snapshot of `.orun/` from Phase 1 | T-COMPAT-* |

Every fixture has a `README.md` explaining its purpose and the tests that
reference it.
