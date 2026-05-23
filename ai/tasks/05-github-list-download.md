# PR 5: GitHub Store — List & Download

**Phase:** 5 (from implementation plan)
**Size:** Medium — ~7 files

## Goal

Authenticated GitHub API client for listing workflow runs, listing Orun artifacts, downloading artifacts with path traversal defense, and resolving runs by various criteria.

## Files to create

### `internal/artifactstore/github/client.go`
- `NewClient(ctx, repo)` — token resolution: GITHUB_TOKEN → GH_TOKEN → `gh auth token`
- `Client` struct with http client, base URL, repo

### `internal/artifactstore/github/list.go`
- `ListWorkflowRuns(ctx, opts)` — GET `/repos/{owner}/{repo}/actions/runs`
- `ListArtifacts(ctx, runID)` — GET `/repos/{owner}/{repo}/actions/runs/{run_id}/artifacts`
- `ListOrunArtifacts(ctx, runID)` — filters to `orun.v1.` prefix

### `internal/artifactstore/github/download.go`
- `Download(ctx, shard, destDir)` — GET `/repos/{owner}/{repo}/actions/artifacts/{artifact_id}/{zip}`
- `DownloadByName(ctx, runID, name, destDir)` — find by name then download
- Path traversal defense when extracting ZIP

### `internal/artifactstore/github/resolve.go`
- `ResolveRun(ctx, opts)` — resolution algorithm:
  - `--run-id`: fetch by ID
  - `--exec-id`: parse `gh-{run_id}...`
  - `--sha`: list runs for SHA
  - `--failed`: latest failure
  - Default: latest for current branch

### Test files
- `client_test.go`, `list_test.go`, `download_test.go` — httptest.Server mocks
- `resolve_test.go` — resolution algorithm tests

## Dependencies
- PR 1 (runbundle types)
- PR 4 (store interface)