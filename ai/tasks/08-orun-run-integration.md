# PR 8: orun run — Artifact Integration

**Phase:** 8 (from implementation plan)
**Size:** Medium — ~2 files changed

## Goal

Add `--artifact github` flag to `orun run`, fix fresh-runner plan issue via `--from-ci github`, and implement defer/finally job shard upload.

## Files to change

### `cmd/orun/command_run.go`
- Add `--artifact` flag
- Support `--from-ci github` on `orun run` for fresh-runner plan recompilation
- Support `--exec-id` flag for explicit exec ID
- Implement defer/finally upload of job shard:
  - `defer` runs after job completes (even on failure)
  - Writes job shard via `runbundle.WriteJobShard`
  - Uploads via `github.Upload`
  - Preserves original exit code
  - Warns on upload failure but does not change job conclusion

### `cmd/orun/command_run_test.go`
- Tests for new flag parsing
- Tests for defer/finally semantics

## Dependencies
- PR 1 (runbundle schema)
- PR 2 (shard writer)
- PR 4 (store interface)
- PR 5 (github download)
- PR 6 (github upload)