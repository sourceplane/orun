# Task 0141 — Implementer Report

## Summary

- Added `DownloadManifestOnly()` helper in `internal/artifactstore/github/manifest.go` that downloads a shard ZIP to a temp directory, reads only `manifest.json`, and cleans up — the Level 2 manifest-only path.
- Wired `printManifestDetails()` in `runGithubRuns()` behind the `--details` flag. When set, each Orun shard's manifest is downloaded and displayed with role, exec-id, status, job, component, and environment fields.
- Level 1 (`orun github runs` without `--details`) remains completely download-free — no behavior change.
- Individual manifest download/read failures produce a stderr warning and skip that shard; one bad shard does not abort or hide other runs.
- Output is sorted by shard name for deterministic display.

## Files Changed

### GitHub Artifact Store
- `internal/artifactstore/github/manifest.go` — new `DownloadManifestOnly()` helper and `ManifestDetail` type
- `internal/artifactstore/github/manifest_test.go` — 4 tests: success, plan shard, download error, invalid manifest

### GitHub CLI
- `cmd/orun/command_github.go` — `printManifestDetails()`, `manifestStatus()`, `sortShardsByName()` added; `--details` gate in `runGithubRuns()`
- `cmd/orun/command_github_test.go` — 4 tests: `TestGithubRunsDetailsFlag`, `TestManifestStatus`, `TestSortShardsByName`, `TestGithubRunsLevel1NoDownload`

### Specs/Docs
- `.kiro/specs/github-artifacts/requirements.md` — Requirement 11 status rows updated to DONE
- `.kiro/specs/github-artifacts/design.md` — Level 2 status rows and gap section updated to DONE

## Checks Run

| Command | Result |
|---------|--------|
| `go test ./cmd/orun/ -run 'TestGithubRuns\|TestGithubCommandRunsHelp' -v` | PASS |
| `go test ./internal/artifactstore/github/... -v` | PASS |
| `go test ./internal/runbundle/... -v` | PASS |
| `go test ./cmd/orun/... -v` | PASS |
| `go build ./cmd/orun/` | PASS |
| Orun validate (`intent.yaml`) | N/A — no root intent.yaml in repo |

## Assumptions

- Manifest-only download still downloads the full shard ZIP (GitHub API does not support partial artifact download). The temp directory is cleaned up immediately after reading `manifest.json`.
- Plan shards without an explicit `status` field report "plan" as their effective status.
- The `sortShardsByName` insertion sort is sufficient for the small number of shards per run (typically <20).

## Spec Proposals

None — implementation matches the Phase 9 design's Level 2 specification exactly.

## Remaining Gaps

- Requirement 10: Partial hydration display verification in `orun status` — not in scope.
- Requirement 17: `.github/workflows/orun.yaml` root workflow template — not in scope.
- Requirement 20: CLI integration test coverage for partial hydration — not in scope.
- Requirement 21: E2E workflow coverage — not in scope.
- TUI cockpit (Phase 1 from `.kiro/specs/orun-tui-cockpit/tasks.md`) — not in scope.

## Next Task Dependencies

- Task 0141 unblocks a verifier task for PR #141.
- After verification, the likely next implementer task is partial hydration display verification / CLI integration coverage for Requirements 10 and 20, or workflow template/E2E coverage for Requirements 17 and 21.

## PR Number

#141
