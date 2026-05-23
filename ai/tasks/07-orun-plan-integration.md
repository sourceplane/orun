# PR 7: orun plan — Artifact Integration

**Phase:** 7 (from implementation plan)
**Size:** Small — ~2 files changed

## Goal

Add `--artifact github` and `--github-output` flags to `orun plan`, enabling plan shard upload from CI.

## Files to change

### `cmd/orun/command_plan.go`
- Add `--artifact` flag (values: "", "github")
- Add `--github-output` flag (bool)
- After plan compilation inside GitHub Actions:
  - Derive exec ID: `gh-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>-<plan-short-sha>`
  - Write plan shard via `runbundle.WritePlanShard`
  - Upload plan shard via `github.Upload`
  - If `--github-output`, write `matrix`, `plan_id`, `exec_id` to `$GITHUB_OUTPUT`

### `cmd/orun/command_plan_test.go`
- Tests for flag parsing
- Tests for `--github-output` output format

## Dependencies
- PR 1 (runbundle schema)
- PR 2 (shard writer)
- PR 4 (store interface)
- PR 5 (github download)
- PR 6 (github upload)