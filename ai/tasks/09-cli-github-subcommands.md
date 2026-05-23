# PR 9: CLI — GitHub Subcommands

**Phase:** 9 (from implementation plan)
**Size:** Medium — ~7 files

## Goal

Add `orun github` command tree with runs, pull, status, logs subcommands for remote inspection of CI execution artifacts.

## Files to create

### `cmd/orun/github/root.go`
- `orun github` parent command
- Registered on root command

### `cmd/orun/github/runs.go`
- `orun github runs` — list workflow runs with three levels of detail
- Level 1: artifact names only (fast)
- Level 2: `--details` downloads manifests (exact)
- Level 3: `orun github pull` (full download)

### `cmd/orun/github/pull.go`
- `orun github pull` — download + synthesize + hydrate
- Resolution → list → download → validate → synthesize → hydrate pipeline
- Flags: `--run-id`, `--exec-id`, `--sha`, `--branch`, `--latest`, `--failed`, `--include-raw`, `--orun-dir`

### `cmd/orun/github/status.go`
- `orun github status` — lightweight remote status via manifest-only download

### `cmd/orun/github/logs.go`
- `orun github logs` — download specific job artifact shard logs

### `cmd/orun/github/runs_test.go`, `pull_test.go`
- Tests for output rendering
- Tests for resolution pipeline

## Dependencies
- PR 1 (runbundle types)
- PR 3 (synthesize/hydrate)
- PR 4 (store interface)
- PR 5 (github list/download/resolve)
- PR 7 (plan integration — for shared flags/patterns)