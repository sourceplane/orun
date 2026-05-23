# PR 1: RunBundle Schema + Naming

**Phase:** 1 (from implementation plan)
**Size:** Small — ~4 files, purely additive

## Goal

Define the portable `RunBundleShardManifest` schema, naming functions, and checksum types. This is the data contract that all downstream phases build on.

## Files to create

### `internal/runbundle/schema.go`
- Package `runbundle`
- Types:
  - `ShardRole` string type with constants `ShardRolePlan` and `ShardRoleJob`
  - `RunBundleShardManifest` struct with all fields from the plan
  - `ShardSource` struct
  - `Checksums` struct
  - `SynthesizedExecution` struct
  - `JobCounts`, `JobShardRef`, `ShardRef` structs
- JSON tags for all fields

### `internal/runbundle/naming.go`
- `ArtifactName(execID, role, suffix, status)` — constructs `orun.v1.<exec-id>.<role>.<suffix>.<status>`
- `ParseShardName(name)` — parses artifact name back into components
- `ExecID(runID, runAttempt, planShortSHA)` — constructs `gh-{run_id}-{attempt}-{plan_short_sha}`
- `ParsedShardName` struct

### `internal/runbundle/schema_test.go`
- Marshal/unmarshal round-trip for all types
- JSON tag verification

### `internal/runbundle/naming_test.go`
- Artifact name construction and parsing
- Unsafe character rejection in exec IDs
- Edge cases: empty strings, long names

## Out of scope
- No writer/reader
- No GitHub API calls
- No CLI changes
- No CI changes