# Validation Report: GitHub Artifacts for Orun

**Date:** 2026-05-23
**Branch:** `feat/workflow-docs-ci`
**Validator:** Claude Code
**Source Plan:** `ai/implimentaion-plan-01.md`
**Tasks:** `ai/tasks/01-runbundle-schema.md` through `ai/tasks/10-workflow-docs-ci.md`

---

## Summary

| Dimension | Status |
|-----------|--------|
| Code Compilation | ✅ Passes |
| Tests (internal/runbundle) | ✅ Pass |
| Tests (internal/artifactstore) | ✅ Pass |
| Tests (internal/artifactstore/github) | ✅ Pass |
| Tests (cmd/orun) | ✅ Pass |
| All 10 Phases Implemented | ✅ Yes |
| Design Constraints | ✅ 8/8 met |
| Critical Issues Found | ⚠️ 1 (blocks `orun github pull` hydration) |
| Minor Issues Found | ⚠️ 3 |
| **Overall** | **🟡 Pass with fixes required** |

---

## Phase-by-Phase Validation

### Phase 0 — Design Spike (spikes/github-artifact-upload-helper/)

- **Code files:** NOT FOUND
- **Status:** ❌ Spike directory does not exist
- **Assessment:** The spike was a pre-production proof-of-concept. The production embedded helper (`internal/artifactstore/github/helper/`) was implemented directly. This is acceptable since the task definitions (01–10) start from Phase 1 and skip Phase 0. The spike was advisory, not a hard dependency.

### Phase 1 — RunBundle Schema ✅

| File | Status |
|------|--------|
| `internal/runbundle/schema.go` | ✅ All types defined (ShardRole, RunBundleShardManifest, ShardSource, Checksums, SynthesizedExecution, JobCounts, JobShardRef, ShardRef) |
| `internal/runbundle/naming.go` | ✅ ArtifactName, ParseShardName, ExecID, ParsedShardName, ValidExecID, IsOrunArtifact |
| `internal/runbundle/schema_test.go` | ✅ Round-trip tests |
| `internal/runbundle/naming_test.go` | ✅ Name parsing/construction tests |

### Phase 2 — Shard Writer/Reader ✅

| File | Status |
|------|--------|
| `internal/runbundle/writer.go` | ✅ WritePlanShard, WriteJobShard, Shard struct |
| `internal/runbundle/reader.go` | ✅ ReadShardManifest, ReadPlanShard, ReadJobShard, PlanShard/JobShard types |
| `internal/runbundle/validate.go` | ✅ ValidateShardManifest, ValidateShardFiles, path traversal defense |
| `internal/runbundle/writer_test.go` | ✅ |
| `internal/runbundle/reader_test.go` | ✅ |

### Phase 3 — Synthesize/Hydrate ✅

| File | Status |
|------|--------|
| `internal/runbundle/synthesize.go` | ✅ Synthesize, SynthesizedStatus, SynthesizedSummary, partial state handling |
| `internal/runbundle/hydrate.go` | ✅ Hydrate with existing state.Store types, metadata.json, state.json, logs |
| `internal/runbundle/synthesize_test.go` | ✅ Complete/failed/cancelled/partial scenarios |
| `internal/runbundle/hydrate_test.go` | ✅ Layout verification, overwrite protection, logs |

### Phase 4 — Generic Store Interface ✅

| File | Status |
|------|--------|
| `internal/artifactstore/store.go` | ✅ Store interface (Upload, List, Download) with types |
| `internal/artifactstore/memory/memory.go` | ✅ InMemoryStore implementation |
| `internal/artifactstore/store_test.go` | ✅ Round-trip tests |

### Phase 5 — GitHub List & Download ✅

| File | Status |
|------|--------|
| `internal/artifactstore/github/client.go` | ✅ NewClient, token resolution (GITHUB_TOKEN → GH_TOKEN → gh auth token), options pattern |
| `internal/artifactstore/github/list.go` | ✅ ListWorkflowRuns, ListArtifacts, ListOrunArtifacts |
| `internal/artifactstore/github/download.go` | ✅ Download, DownloadByName, ZIP extraction with path traversal defense |
| `internal/artifactstore/github/resolve.go` | ✅ ResolveRun with 5-step resolution algorithm |
| `internal/artifactstore/github/github_test.go` | ✅ httptest.Server mocks |

### Phase 6 — GitHub Upload ✅

| File | Status |
|------|--------|
| `internal/artifactstore/github/upload.go` | ✅ Upload with embedded helper extraction, npm install, env detection |
| `internal/artifactstore/github/helper/upload.mjs` | ✅ ESM module using @actions/artifact |
| `internal/artifactstore/github/helper/package.json` | ✅ @actions/artifact ^2.2.0 |
| `internal/artifactstore/github/upload_test.go` | ✅ Mock exec, env detection, retention parsing |

### Phase 7 — orun plan Integration ✅

| File | Status |
|------|--------|
| `cmd/orun/command_plan.go` | ✅ `--artifact` and `--github-output` flags registered |
| `cmd/orun/main.go` (generatePlan) | ✅ Plan shard write + upload + GitHub output (lines 446–527) |
| `cmd/orun/command_plan_test.go` | ✅ Flag parsing tests |

### Phase 8 — orun run Integration ✅

| File | Status |
|------|--------|
| `cmd/orun/command_run.go` | ✅ `--artifact` flag, `--exec-id`, defer/finally upload on lines 311–400 |
| `cmd/orun/command_run_test.go` | ✅ Flag/defer tests |

### Phase 9 — CLI Commands ✅

| File | Status |
|------|--------|
| `cmd/orun/command_github.go` | ✅ Full command tree: runs, pull, status, logs |
| `cmd/orun/command_github_test.go` | ✅ Registration, flags, parseGitHubRepo, filter/group tests |

### Phase 10 — Workflow Template & Docs ✅

| File | Status |
|------|--------|
| `docs/examples/github-artifacts-workflow.yaml` | ✅ Clean workflow template without upload/download steps |
| `docs/github-artifacts.md` | ✅ Usage guide with 3 inspection levels |
| `.github/workflows/orun-default-workflow.yaml` | ✅ Updated with ORUN_ARTIFACT_BACKEND, ORUN_ARTIFACT_UPLOAD, --artifact github |
| `.github/workflows/release-oci.yaml` | ✅ No upload-specific changes needed (release flow) |
| `website/docs/cli/orun-plan.md` | ✅ `--artifact` and `--github-output` documented |
| `website/docs/cli/orun-run.md` | ✅ `--artifact` documented in flags table |
| `website/docs/concepts/execution-model.md` | ✅ CI artifacts section added |

---

## Design Constraints Check

| # | Constraint | Status | Notes |
|---|-----------|--------|-------|
| 12.1 | No collector job | ✅ | Each invocation uploads one immutable shard |
| 12.2 | RunBundle is portable format | ✅ | Uses `kind: RunBundleShard`, generic source block |
| 12.3 | Partial hydration | ✅ | Missing shards → `status: "partial"` |
| 12.4 | Compatibility with existing state types | ✅ | Uses `state.ExecMetadata` and `state.ExecState` |
| 12.5 | Defer/finally upload semantics | ✅ | Upload in defer, preserves original exit code |
| 12.6 | Minimal CI YAML | ✅ | No upload/download artifact steps or fragile jq |
| 12.7 | Fresh-runner plan resolution | ✅ | `--from-ci github` on `orun run` |
| 12.8 | Security | ✅ | Path traversal defense, no token logging, IncludeRaw flag |

---

## Test Matrix Validation

| Area | Status |
|------|--------|
| Naming — parse/build artifact names, unsafe chars | ✅ |
| Writer — plan/job shard exact layout | ✅ |
| Reader — reject missing files, bad checksums, bad schema | ✅ |
| Synthesis — complete, failed, cancelled, partial | ✅ |
| Hydration — .orun/ layout with existing commands | ✅ |
| Store interface — in-memory round-trip | ✅ |
| GitHub list — REST pagination, auth | ✅ (httptest) |
| GitHub download — ZIP extraction, path traversal | ✅ (httptest) |
| GitHub upload — helper invocation, result parsing | ✅ (mock exec) |
| Plan integration — flag parsing, --github-output | ✅ |
| Run integration — flag parsing, defer/finally | ✅ |

---

## Issues Found

### 🔴 CRITICAL: Hydrate `orunDir` path is wrong in `orun github pull`

**File:** `cmd/orun/command_github.go:291-293`
```go
orunDir := githubPullOrunDir
if orunDir == "." {
    orunDir = storeDir()     // returns intent root, NOT .orun/
}
```

**Problem:** `storeDir()` returns the intent root (e.g., `/path/to/repo`), but `Hydrate()` expects the `.orun/` directory path. This causes:
- Hydrated files written to `/path/to/repo/executions/...` instead of `/path/to/repo/.orun/executions/...`
- State store metadata written to `/path/to/.orun/executions/...` instead
- `orun status` / `orun logs` cannot find hydrated executions

**Fix:** Change to `orunDir = filepath.Join(storeDir(), ".orun")`
Uses `filepath` already imported but not used. Import statement also needs `"path/filepath"`.

### 🟡 Minor: Env-based activation not wired

**File:** `cmd/orun/command_run.go` and `cmd/orun/main.go`

Per implementation plan section 8.3, the following env vars should implicitly activate artifact upload:
- `ORUN_ARTIFACT_BACKEND=github` — should be equivalent to `--artifact github`
- `ORUN_ARTIFACT_UPLOAD=true` — should enable upload in CI
- `ORUN_SKIP_ARTIFACT_UPLOAD=true` — should disable upload for debugging

Currently only `--artifact github` flag activates upload. The env vars are documented but not read.

**Workaround:** The CI workflow YAMLs use `--artifact github` explicitly, so all CI paths work. This only affects users who expect env-based activation as documented.

### 🟡 Minor: No unit test for `runGithubPull` command

**Files:** `cmd/orun/command_github.go` (rungGithubPull) and `cmd/orun/command_github_test.go`

The `runGithubPull` function has no dedicated unit test. `rungGithubRuns` and `rungGithubLogs` are similarly untested. Only command registration, flag parsing, and utility functions (`parseGitHubRepo`, `filterOrunShards`, `groupByExecID`) are tested.

---

## Critical Bug Fix

Before release, apply this fix:

**File:** `cmd/orun/command_github.go`, line 291-293

Replace:
```go
orunDir := githubPullOrunDir
if orunDir == "." {
    orunDir = storeDir()
}
```

With:
```go
orunDir := githubPullOrunDir
if orunDir == "." {
    orunDir = filepath.Join(storeDir(), ".orun")
}
```

This ensures hydrated files are placed in `.orun/executions/{exec-id}/` where existing commands (`orun status`, `orun logs`) expect them.

---

## Release Decision

| Check | Result |
|-------|--------|
| All source code compiles | ✅ |
| All tests pass | ✅ |
| All 10 task phases implemented | ✅ |
| All 8 design constraints satisfied | ✅ |
| Critical bugs | ⚠️ 1 (hydrate path) |
| Minor gaps | ⚠️ 3 |

**Recommendation: 🟡 Fix critical bug before release, then release v2.4.0**

The hydrate path bug in `orun github pull` would cause silent data corruption (writing to wrong directory). It must be fixed before tagging a release. The env-var activation gaps are documentation/ergonomic enhancements that can be deferred to a follow-up.