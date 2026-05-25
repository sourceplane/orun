# Task 11: Fix Hydrate `orunDir` Path in `orun github pull`

**Priority:** Critical
**Phase:** Bug fix (post-Phase 10)
**Size:** Tiny — 2 lines changed + test

## Bug Description

In `cmd/orun/command_github.go:291-293`, `orun github pull` passes `storeDir()` (the intent root, e.g. `/path/to/repo`) to `runbundle.Hydrate()` as the `orunDir` parameter. But `Hydrate()` expects the `.orun/` directory path — the same pattern used in `Hydrate`'s own default (`orunDir = ".orun"`) and in all hydrate tests (`orunDir = filepath.Join(t.TempDir(), ".orun")`).

This causes three problems:
1. Execution files are written to `{intentRoot}/executions/...` instead of `{intentRoot}/.orun/executions/...`
2. The state store is created with `filepath.Dir(intentRoot)` → metadata written to wrong parent directory
3. `orun status` and `orun logs` cannot find hydrated executions

## Acceptance Criteria

1. **Root cause confirmed:** `storeDir()` returns the intent root, not the `.orun/` path
2. **Fix applied:** `orunDir` is resolved as `filepath.Join(storeDir(), ".orun")` when no explicit `--orun-dir` is provided
3. **No regression:** All existing tests pass
4. **Validation:** A unit test verifies that `Hydrate` is called with the correct `.orun/` base path

## File to Change

### `cmd/orun/command_github.go`

**Current (lines 291-293):**
```go
orunDir := githubPullOrunDir
if orunDir == "." {
    orunDir = storeDir()
}
```

**Required:**
```go
orunDir := githubPullOrunDir
if orunDir == "." {
    orunDir = filepath.Join(storeDir(), state.OrunDir)
}
```

The `state.OrunDir` constant is the canonical name for the `.orun` directory and is already defined in the `internal/state` package. This avoids hardcoding the `".orun"` string and stays consistent with how `state.NewStore` builds its base path (`filepath.Join(workDir, OrunDir)`).

If `state.OrunDir` is not exported, use `filepath.Join(storeDir(), ".orun")` with a comment referencing the convention.

## Validation

### Automated
1. `go build ./cmd/orun` — must compile without error
2. `go test ./cmd/orun/...` — all existing tests must pass
3. `go test ./internal/runbundle/... ./internal/artifactstore/...` — all downstream tests pass

### Unit test (to add)
Add a test to `cmd/orun/command_github_test.go` that:
1. Sets up a clean CLI command invocation for `orun github pull` with default flags
2. Verifies the resolved `orunDir` path ends with `.orun`
3. Verifies the path exists under the expected base directory

### Manual
1. Run `orun github pull --latest` with a valid GitHub token and repository with orun artifacts
2. Verify hydrated files land in `.orun/executions/{exec-id}/` (not `executions/` at repo root)
3. Verify `orun status` can read the hydrated execution
4. Run `orun github pull --orun-dir /tmp/test-orun` and verify files land in `/tmp/test-orun/executions/`

## Dependencies
- None (self-contained fix in one file)

## Out of Scope
- Wiring `ORUN_ARTIFACT_BACKEND` / `ORUN_ARTIFACT_UPLOAD` env vars (tracked separately)
- Adding unit tests for `runGithubPull` / `runGithubRuns` / `runGithubLogs` (tracked separately)