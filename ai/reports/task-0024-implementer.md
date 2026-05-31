# Implementer Report — task-0024 (Phase 2 Milestone C1)

- **Branch:** `impl/task-0024-c1-sourcectx-resolver`
- **PR:** #169
- **Status:** Ready for verification

## Scope

Implements **Milestone C1** of
`specs/orun-component-catalog/implementation-plan.md`: the
`internal/sourcectx` resolver that turns a workspace path into a
fully-populated `WorkspaceState` (source scope, revision, dirty hash,
source-snapshot key) — the input every later catalog package consumes.

C0 (data model + key/sanitize/hash skeleton) is unchanged. C1 builds
on top of the pre-existing `hash.go` / `keys.go` / `model.go` from C0
and wires in:

- `Git`, `Clock`, `Filesystem` adapter interfaces,
- a default shell-out `git` adapter,
- a default `os`-rooted filesystem with prune list,
- a deterministic `ResolveSourceSnapshot` orchestrator,
- a typed `--from-ci` no-match error envelope,
- a CI event injection path.

## Deliverables

### New files in `internal/sourcectx/`

| File | Purpose |
|---|---|
| `adapters.go` | `Git`/`Clock`/`Filesystem` interfaces, `ResolveOptions`, `CIEventInjection`, `InferenceToggles`, `WithDefaults` wiring |
| `clock.go` | `systemClock`, `DefaultClock()`, `FixedClock` test helper |
| `fs.go` | `osFilesystem`, `DefaultFilesystem(root)` — prunes `.git`/`node_modules`/`vendor` at root |
| `git_exec.go` | Shell-out `git` adapter (HasRepo/HeadRevision/TreeHash/Branch/Ref/Tag/RemoteURL/DiffTreePaths) |
| `dirty.go` | `ErrCIEventNoMatch` sentinel + typed `*CIEventNoMatchError`, dirty-enumeration helpers, `CatalogRelevant` filter |
| `resolve.go` | `ResolveSourceSnapshot`, `populateFromGit`, `populateDirty`, `applyCIInjection`, `repoFromRemote` |
| `resolver_test.go` | Scope matrix + T-IDK-3 determinism + T-IDK-4 + CI no-match + default-git tempdir |
| `coverage_test.go` | FS Walk/Stat/ReadFile coverage, clock defaults, error rendering, missing-WorkspacePath, dirty race-skip, clean-tree, only-non-relevant-dirty, HasRepo error |
| `testhelpers_test.go` | `execCommand`, `writeFileWithDir` |

### Resolver public surface

```go
// Top-level entry.
func ResolveSourceSnapshot(ctx context.Context, opts ResolveOptions) (WorkspaceState, error)

// Adapters (all interfaces; nil → defaults inside ResolveSourceSnapshot).
type Git interface {
    HasRepo(ctx context.Context, root string) (bool, error)
    HeadRevision(ctx context.Context, root string) (string, error)
    TreeHash(ctx context.Context, root string) (string, error)
    Branch(ctx context.Context, root string) (string, error)
    Ref(ctx context.Context, root string) (string, error)
    Tag(ctx context.Context, root string) (string, error)
    RemoteURL(ctx context.Context, root string) (string, error)
    DiffTreePaths(ctx context.Context, root, since string) ([]string, error)
}
type Clock interface { Now() time.Time }
type Filesystem interface {
    Walk(root string, fn func(rel string, d fs.DirEntry) error) error
    Stat(path string) (fs.FileInfo, error)
    ReadFile(path string) ([]byte, error)
}

// Defaults.
func DefaultClock() Clock
func DefaultFilesystem(root string) Filesystem
func DefaultGit() Git

// CI event no-match envelope (mirrors triggerctx.ErrNoMatchingBinding).
var ErrCIEventNoMatch = errors.New("sourcectx: CI event did not match workspace state")
type CIEventNoMatchError struct {
    Provider, Event, Action, Reason string
}
func (e *CIEventNoMatchError) Error() string
func (e *CIEventNoMatchError) Unwrap() error  // → ErrCIEventNoMatch
```

## Git adapter trade-off — shell-out vs go-git

Chose **shell-out to system `git` via `os/exec`**. Rationale:

1. **Precedent in repo.** `internal/git/changes.go` already shells out;
   matching that convention keeps mental model uniform across packages.
2. **Zero new module deps.** `go-git` brings in a sizeable transitive
   graph (`go-billy`, `gcfg`, `crypto/openpgp`, etc.) and ~6–8 MB of
   binary growth. C1 is leaf-level infrastructure — the cost would
   propagate everywhere downstream.
3. **CI always has `git`.** GitHub Actions runners ship git ≥ 2.40;
   our existing matrix already depends on it. No portability gain
   from go-git in our deployment surface.
4. **Test budget held.** `test-plan.md` §7 budgets ≤ 30 ms per resolver
   call; shell-out measured ≈ 8–14 ms on macOS for a small tempdir
   repo, well under budget.
5. **Swap stays cheap.** The `Git` interface is intentionally narrow
   (8 methods, all read-only, all return strings). Swapping in go-git
   later is a single new file and a constructor change — no caller
   rework.

If/when we hit a CI environment without git, or want
sandbox-without-fork semantics, the swap is one file.

## Property test results (T-IDK-3, T-IDK-4)

- **T-IDK-3 (ordering stability).** Vary `[]DirtyFile` ordering 1 000
  times for a fixed workspace; assert `DirtyHash` and the resolved
  `SourceSnapshotKey` are byte-identical across all orderings. **PASS.**
- **T-IDK-4 (non-catalog-relevant insulation).** Add `notes.txt`,
  `vendor/foo.go`, `node_modules/x/y.js` to the dirty enumeration
  input; assert `DirtyHash` is unchanged versus the catalog-relevant
  baseline. **PASS** (filtered via `CatalogRelevant` before hashing).

## Local gates (all green)

```
go build ./...                                  → ok
go vet ./...                                    → clean
go test ./... -race -count=1                    → all pkgs PASS
make test-state-redesign                        → all gates PASS
   statestore        95.7%  (≥ 95)
   revision          90.3%  (≥ 90)
   executionstate    90.0%  (≥ 90)
   catalogmodel      90.2%  (≥ 90)
   sourcectx         91.1%  (≥ 90)
   catalogmodel/Sanitize*  100.0%  (== 100)
make verify-generated                            → up-to-date
go list -deps ./internal/sourcectx/... | grep internal | exclude self+catalogmodel
                                                → leaf-clean
```

## Constraints honoured

- Phase 1 floors held byte-for-byte (statestore 95.7 %, revision 90.3 %,
  executionstate 90.0 %).
- C0 floors held (catalogmodel 90.2 %, Sanitize* 100 %).
- `internal/sourcectx` package gate raised to 91.1 % (gate ≥ 90).
- Leaf-clean: `internal/sourcectx` imports only `internal/catalogmodel`
  + stdlib. No cross-Phase-1 imports introduced.
- Read-only adapters; resolver does not mutate the workspace.
- No secrets in fixtures, tests, or this report. CI-event fixtures
  use placeholder strings.
- `internal/catalogmodel` untouched.
- `specs/orun-component-catalog/` untouched.

— Implementer, 2026-05-31
