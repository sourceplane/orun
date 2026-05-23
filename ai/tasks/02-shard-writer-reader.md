# PR 2: Shard Writer + Reader + Validation

**Phase:** 2 (from implementation plan)
**Size:** Small — ~5 files

## Goal

Write and read plan/job shards to/from disk with full validation (schema version, file existence, checksums, path traversal defense).

## Files to create

### `internal/runbundle/writer.go`
- `WritePlanShard(ctx, opts)` — writes plan shard directory:
  - `manifest.json`, `plan.json`, `matrix.json`, `trigger.json`, `git.json`, `checksums.json`
- `WriteJobShard(ctx, opts)` — writes job shard directory:
  - `manifest.json`, `job.json`, `state.json`, `steps.jsonl`, `events.jsonl`, `summary.md`, `logs/<step-id>.log`
- `WritePlanShardOptions`, `WriteJobShardOptions` structs

### `internal/runbundle/reader.go`
- `ReadShardManifest(dir)` — reads and validates manifest.json
- `ReadPlanShard(dir)` — reads full plan shard
- `ReadJobShard(dir)` — reads full job shard

### `internal/runbundle/validate.go`
- Validation: schema version, file existence, checksum matching, path traversal defense

### `internal/runbundle/writer_test.go`, `reader_test.go`
- Produce shards, verify exact directory layout
- Reject missing files, bad checksums, bad schema version

## Dependencies
- PR 1 (schema types)