# PR 6: GitHub Store — Upload via Embedded Helper

**Phase:** 6 (from implementation plan)
**Size:** Small — ~4 files

## Goal

Upload shards from within GitHub Actions using embedded `@actions/artifact` Node.js helper.

## Files to create

### `internal/artifactstore/github/upload.go`
- `Upload(ctx, shard)` — Go entry point
- Detects GitHub Actions env
- Extracts embedded helper to temp dir
- Invokes `node upload.mjs <shardDir> <name> [retentionDays]`
- Parses JSON result
- Guardrails: one artifact per invocation, graceful duplicate handling

### `internal/artifactstore/github/helper/upload.mjs`
- ESM module using `@actions/artifact` (`UploadArtifactClient`)
- Takes shardDir, artifactName, retentionDays
- Outputs JSON: `{ id, name, size }`

### `internal/artifactstore/github/helper/package.json`
- Type: module
- Dependency: `@actions/artifact` ^2.2.0

### `internal/artifactstore/github/upload_test.go`
- Mock exec invocation, result parsing
- Env detection tests

## Dependencies
- PR 1 (runbundle types)
- PR 4 (store interface)
- PR 5 (client)