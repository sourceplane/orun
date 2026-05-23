# PR 3: Synthesize + Hydrate

**Phase:** 3 (from implementation plan)
**Size:** Medium — ~5 files

## Goal

Merge plan + job shards into synthesized execution state and hydrate it into `.orun/executions/` layout compatible with existing `orun status` / `orun logs`.

## Files to create

### `internal/runbundle/synthesize.go`
- `Synthesize(plan, jobs)` — merges into `SynthesizedExecution`
- Handles partial state: missing shards → `status: "partial"`
- Computes job counts (total, completed, failed, cancelled, skipped, pending)

### `internal/runbundle/hydrate.go`
- `Hydrate(ctx, exec, opts, orunDir)` — reconstructs `.orun/executions/{exec-id}/`
- Uses existing `internal/state.ExecMetadata` and `internal/state.ExecState` for compatibility
- Writes: `metadata.json`, `github.json` (source), `plan.json`, `state.json`, `shards.json`, `logs/<jobID>/<stepID>.log`

### `internal/runbundle/hydrate_test.go`, `synthesize_test.go`
- Write shards → synthesize → hydrate → verify `.orun/` layout
- Partial hydration: missing job shards
- Verify existing `orun status` can read hydrated state

### `internal/runbundle/synthesize_test.go`
- Complete, failed, cancelled, partial scenarios

## Dependencies
- PR 1 (schema)
- PR 2 (writer/reader)