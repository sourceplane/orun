# Requirements Document

## Introduction

Orun produces immutable GitHub Actions artifact shards from CI execution — plan evidence, job results, and step logs — without requiring `actions/upload-artifact` steps in workflow YAML. Each Orun invocation uploads exactly one shard. Local `orun github pull` performs lazy fan-in, synthesizes execution state, and hydrates the standard `.orun/executions/` layout so that `orun status` and `orun logs` work unchanged.

This document derives formal requirements from the approved design. Requirements are organized by the same phases as the design document. Each section includes a **Current Implementation Status** subsection with verification commands so the team can confirm current state before starting any remaining work.

---

## Glossary

- **RunBundle**: The portable shard format owned by Orun, backend-agnostic.
- **Shard**: A single artifact ZIP containing a `manifest.json`, checksums, and role-specific files (plan or job).
- **ExecID**: The execution identifier in the format `gh-{GITHUB_RUN_ID}-{GITHUB_RUN_ATTEMPT}-{plan_short_sha}`.
- **PlanShard**: A shard with `role: "plan"` containing `plan.json`, `trigger.json`, `git.json`.
- **JobShard**: A shard with `role: "job"` containing `job.json`, `state.json`, `steps.jsonl`, and `logs/`.
- **ArtifactStore**: The generic backend-agnostic interface for upload, list, and download operations.
- **GitHub_Backend**: The GitHub Actions implementation of ArtifactStore.
- **Synthesize**: The operation that merges one PlanShard and N JobShards into a `SynthesizedExecution`.
- **Hydrate**: The operation that writes a synthesized execution into the `.orun/executions/{exec-id}/` layout.
- **Level_1**: Artifact listing using only artifact names (no downloads).
- **Level_2**: Artifact listing with manifest download for exact status.
- **Level_3**: Full shard download and hydration via `orun github pull`.
- **Partial_Execution**: A synthesized execution where fewer job shards exist than plan jobs.
- **ACTIONS_RUNTIME_TOKEN**: The GitHub Actions runtime token required for artifact upload.
- **Upload_Helper**: The embedded Node.js helper (`helper/upload.mjs`) that uses `@actions/artifact` for upload.

---

## Requirements

### Requirement 1: RunBundle Shard Schema

**User Story:** As a CI engineer, I want a portable, backend-agnostic shard format, so that execution evidence can be stored in any artifact backend without format changes.

#### Acceptance Criteria

1. THE RunBundle_Schema SHALL define a `RunBundleShardManifest` struct with fields: `APIVersion`, `Kind`, `SchemaVersion`, `Role`, `ExecID`, `PlanID`, `JobUID`, `JobID`, `Component`, `Environment`, `Status`, `StartedAt`, `FinishedAt`, `Source`, and `Files`.
2. THE RunBundle_Schema SHALL define a `ShardSource` struct with fields: `Type`, `Repository`, `RunID`, `RunAttempt`, `Workflow`, `SHA`, `Ref`, and `EventName`.
3. THE RunBundle_Schema SHALL define `ShardRole` as an enumerated type with values `"plan"` and `"job"`.
4. WHEN a `RunBundleShardManifest` is serialized to JSON and deserialized, THE RunBundle_Schema SHALL produce a value equivalent to the original.
5. THE RunBundle_Schema SHALL define `SynthesizedExecution`, `JobCounts`, and `JobShardRef` types for synthesis output.

#### Current Implementation Status

**Files:** `internal/runbundle/schema.go`, `internal/runbundle/schema_test.go`

| Item | Status |
|------|--------|
| `RunBundleShardManifest` struct | ✅ DONE |
| `ShardSource` struct | ✅ DONE |
| `ShardRole` type with "plan"/"job" values | ✅ DONE |
| `SynthesizedExecution`, `JobCounts`, `JobShardRef` | ✅ DONE |

**Verify:**
```bash
go test ./internal/runbundle/ -run TestSchema -v
go build ./internal/runbundle/
```

---

### Requirement 2: Artifact Naming Convention

**User Story:** As a CI engineer, I want artifact names to encode execution metadata, so that shard identity and status can be determined from the name alone without downloading the artifact.

#### Acceptance Criteria

1. THE Naming_System SHALL produce artifact names in the format `orun.v1.{exec-id}.{role}.{suffix}.{status}`.
2. THE Naming_System SHALL restrict all name components to characters matching `[a-zA-Z0-9_-]+`.
3. WHEN `ArtifactName(execID, role, suffix, status)` is called with valid inputs, THE Naming_System SHALL return a string matching the naming convention.
4. WHEN `ParseShardName(name)` is called on a name produced by `ArtifactName`, THE Naming_System SHALL return the original `execID`, `role`, `suffix`, and `status` components.
5. WHEN `ParseShardName(name)` is called on a string that does not match the naming convention, THE Naming_System SHALL return an error.
6. THE Naming_System SHALL derive ExecID as `gh-{GITHUB_RUN_ID}-{GITHUB_RUN_ATTEMPT}-{plan_short_sha}` via `ExecID()`.
7. WHEN `ValidExecID(id)` is called with a string containing characters outside `[a-zA-Z0-9_-]`, THE Naming_System SHALL return false.

#### Current Implementation Status

**Files:** `internal/runbundle/naming.go`, `internal/runbundle/naming_test.go`

| Item | Status |
|------|--------|
| `ArtifactName()` | ✅ DONE |
| `ParseShardName()` | ✅ DONE |
| `ExecID()` | ✅ DONE |
| `ValidExecID()`, `IsOrunArtifact()` | ✅ DONE |

**Verify:**
```bash
go test ./internal/runbundle/ -run TestNaming -v
```

---

### Requirement 3: Shard Writer

**User Story:** As a CI runner, I want to write plan and job shards to disk, so that execution evidence can be packaged and uploaded as a single artifact.

#### Acceptance Criteria

1. WHEN `WritePlanShard(ctx, opts)` is called with a valid plan and source, THE Shard_Writer SHALL produce a directory containing `manifest.json`, `checksums.json`, `plan.json`, `trigger.json`, and `git.json`.
2. WHEN `WriteJobShard(ctx, opts)` is called with a valid job state, THE Shard_Writer SHALL produce a directory containing `manifest.json`, `checksums.json`, `job.json`, `state.json`, `steps.jsonl`, and a `logs/` subdirectory.
3. WHEN `WriteJobShard` is called and the job has step log files, THE Shard_Writer SHALL copy each step log into `logs/{step-id}.log` and record the path in `manifest.Files` under the key `log:{step-id}`.
4. WHEN `WritePlanShard` or `WriteJobShard` completes, THE Shard_Writer SHALL compute SHA-256 checksums over all tracked files and write them to `checksums.json`.
5. WHEN a shard directory produced by `WritePlanShard` is passed to `ReadPlanShard`, THE Shard_Writer SHALL produce a shard that passes `ValidateShardFiles` without modification.
6. WHEN a shard directory produced by `WriteJobShard` is passed to `ReadJobShard`, THE Shard_Writer SHALL produce a shard that passes `ValidateShardFiles` without modification.

#### Current Implementation Status

**Files:** `internal/runbundle/writer.go`, `internal/runbundle/writer_test.go`

| Item | Status |
|------|--------|
| `WritePlanShard()` | ✅ DONE |
| `WriteJobShard()` with log copy | ✅ DONE |
| Checksum computation | ✅ DONE |

**Verify:**
```bash
go test ./internal/runbundle/ -run TestWriter -v
```

---

### Requirement 4: Shard Reader and Validation

**User Story:** As a developer pulling CI artifacts, I want shard reading to validate integrity automatically, so that corrupted or tampered shards are rejected before use.

#### Acceptance Criteria

1. WHEN `ReadShardManifest(dir)` is called on a valid shard directory, THE Shard_Reader SHALL return a `RunBundleShardManifest` with all fields populated.
2. WHEN `ReadShardManifest(dir)` is called, THE Shard_Reader SHALL call `ValidateShardManifest` and `ValidateShardFiles` before returning.
3. WHEN `ValidateShardFiles` is called and any file listed in `manifest.Files` does not exist on disk, THE Shard_Reader SHALL return an error naming the missing file.
4. WHEN `ValidateShardFiles` is called and any file's SHA-256 digest does not match `checksums.json`, THE Shard_Reader SHALL return an error naming the file and both digests.
5. WHEN `ValidateShardFiles` is called and any file path in the manifest resolves outside the shard directory, THE Shard_Reader SHALL return an error and reject the shard entirely.
6. WHEN `ValidateShardManifest` is called with a `SchemaVersion` not in `supportedSchemaVersions`, THE Shard_Reader SHALL return an error.

#### Current Implementation Status

**Files:** `internal/runbundle/reader.go`, `internal/runbundle/validate.go`, `internal/runbundle/reader_test.go`

| Item | Status |
|------|--------|
| `ReadShardManifest()` with validation | ✅ DONE |
| `ReadPlanShard()`, `ReadJobShard()` | ✅ DONE |
| Checksum verification | ✅ DONE |
| Path traversal defense | ✅ DONE |
| Schema version check | ✅ DONE |

**Verify:**
```bash
go test ./internal/runbundle/ -run TestReader -v
go test ./internal/runbundle/ -run TestValidate -v
```

---

### Requirement 5: Generic ArtifactStore Interface

**User Story:** As a platform engineer, I want a backend-agnostic artifact store interface, so that the same shard format works with GitHub, R2, S3, and future backends without code changes.

#### Acceptance Criteria

1. THE ArtifactStore SHALL define a `Store` interface with methods: `Upload(ctx, shard) (*UploadResult, error)`, `List(ctx, opts) ([]RemoteShard, error)`, and `Download(ctx, shard, destDir) (*DownloadedShard, error)`.
2. THE ArtifactStore SHALL define a `RemoteShard` type with fields: `Name`, `ID`, `SizeBytes`, `CreatedAt`, `ExpiresAt`, `Parsed`, and `SourceMeta`.
3. THE ArtifactStore SHALL define an `UploadResult` type with fields: `ID`, `Name`, `Size`, and `Digest`.
4. THE ArtifactStore SHALL define a `DownloadedShard` type with fields: `Name`, `Dir`, and `Shard`.
5. THE GitHub_Backend SHALL implement the `Store` interface at compile time.

#### Current Implementation Status

**Files:** `internal/artifactstore/store.go`, `internal/artifactstore/store_test.go`

| Item | Status |
|------|--------|
| `Store` interface | ✅ DONE |
| `RemoteShard`, `UploadResult`, `DownloadedShard` | ✅ DONE |
| `ListOptions` | ✅ DONE |
| `memory.go` / `local.go` test implementations | ⚠️ NEEDS WORK — not present; tests use inline mocks |

**Verify:**
```bash
go build ./internal/artifactstore/...
```

---

### Requirement 6: GitHub Artifact Store — List and Download

**User Story:** As a developer, I want to list and download Orun artifact shards from GitHub Actions, so that I can inspect CI execution results locally.

#### Acceptance Criteria

1. WHEN `NewClient(ctx, repo)` is called, THE GitHub_Backend SHALL resolve the GitHub token by checking `GITHUB_TOKEN`, then `GH_TOKEN`, then `gh auth token`, then an explicit `WithToken` option, in that order.
2. IF no token is found via any resolution method, THEN THE GitHub_Backend SHALL return an error naming all three sources tried.
3. WHEN `ListWorkflowRuns(ctx, opts)` is called, THE GitHub_Backend SHALL return workflow runs filtered by the provided branch, SHA, and status options.
4. WHEN `ListArtifacts(ctx, runID)` is called, THE GitHub_Backend SHALL call `ParseShardName` on each artifact name and populate the `Parsed` field of each `RemoteShard`.
5. WHEN `ListOrunArtifacts(ctx, runID)` is called, THE GitHub_Backend SHALL return only artifacts where `Parsed != nil`.
6. WHEN `Download(ctx, shard, destDir)` is called, THE GitHub_Backend SHALL download the artifact ZIP, extract it to `destDir`, and call `ReadShardManifest` on the extracted directory.
7. WHEN `Download` extracts a ZIP and any entry's resolved path falls outside `destDir`, THE GitHub_Backend SHALL return an error immediately without writing any further files.
8. WHEN an HTTP request returns status 429 or 5xx, THE GitHub_Backend SHALL retry with exponential backoff and jitter, up to 3 retries with a 500ms initial delay and 10s maximum delay.
9. WHERE a GitHub Enterprise Server base URL is configured via `WithBaseURL`, THE GitHub_Backend SHALL use that URL for all API calls.

#### Current Implementation Status

**Files:** `internal/artifactstore/github/client.go`, `internal/artifactstore/github/list.go`, `internal/artifactstore/github/download.go`, `internal/artifactstore/github/resolve.go`, `internal/artifactstore/github/retry.go`

| Item | Status |
|------|--------|
| `NewClient()` with token resolution chain | ✅ DONE |
| `ListWorkflowRuns()` with filters | ✅ DONE |
| `ListArtifacts()` / `ListOrunArtifacts()` | ✅ DONE |
| `Download()` with ZIP extraction + path traversal defense | ✅ DONE |
| `ResolveRun()` with all resolution modes | ✅ DONE |
| `retryDo()` with exponential backoff + jitter | ✅ DONE |
| GHES support via `WithBaseURL` | ✅ DONE |

**Verify:**
```bash
go test ./internal/artifactstore/github/ -run TestList -v
go test ./internal/artifactstore/github/ -run TestDownload -v
go test ./internal/artifactstore/github/ -run TestRetry -v
```

---

### Requirement 7: GitHub Artifact Upload via Embedded Helper

**User Story:** As a CI runner, I want to upload shard artifacts using the official GitHub Actions artifact API, so that uploads are reliable and compatible with GitHub's artifact retention policies.

#### Acceptance Criteria

1. WHEN `UploadShard(ctx, shard)` is called and `GITHUB_ACTIONS=true` and `ACTIONS_RUNTIME_TOKEN` is set, THE GitHub_Backend SHALL invoke the embedded Node.js helper to upload the shard directory as a named artifact.
2. WHEN `UploadShard` is called and `GITHUB_ACTIONS` is not `"true"`, THE GitHub_Backend SHALL return a descriptive error explaining that upload requires a GitHub Actions environment.
3. WHEN the embedded helper is first used, THE GitHub_Backend SHALL extract `helper/upload.mjs` and `helper/package.json` to a temp directory and run `npm install` before invoking the helper.
4. WHEN `UploadShard` completes successfully, THE GitHub_Backend SHALL call `VerifyArtifactExists` to confirm the artifact appears in the artifact list before returning.
5. WHEN `UploadShard` is called twice with the same shard (same exec-id, role, suffix, and status), THE GitHub_Backend SHALL produce the same artifact name for both calls.
6. THE GitHub_Backend SHALL read artifact retention days from the `ORUN_ARTIFACT_RETENTION_DAYS` environment variable, defaulting to 14 days if not set.
7. WHEN the upload helper exits with a non-zero status, THE GitHub_Backend SHALL retry the upload according to the retry configuration before returning an error.

#### Current Implementation Status

**Files:** `internal/artifactstore/github/upload.go`, `internal/artifactstore/github/helper/upload.mjs`, `internal/artifactstore/github/helper/package.json`

| Item | Status |
|------|--------|
| `Upload()` via embedded Node.js helper | ✅ DONE |
| `UploadWithRetry()` with retry logic | ✅ DONE |
| `UploadShard()` orchestrator with verification | ✅ DONE |
| `VerifyArtifactExists()` with polling | ✅ DONE |
| `ensureHelperExtracted()` with npm install | ✅ DONE |
| `helper/upload.mjs` using `@actions/artifact` | ✅ DONE |
| Retention days from env var | ✅ DONE |

**Verify:**
```bash
go test ./internal/artifactstore/github/ -run TestUpload -v
# Real upload requires GitHub Actions environment
```

---

### Requirement 8: `orun plan` GitHub Artifact Integration

**User Story:** As a CI engineer, I want `orun plan` to upload a plan shard automatically, so that downstream matrix jobs can reference the plan without re-running plan generation.

#### Acceptance Criteria

1. THE Plan_Command SHALL accept an `--artifact` flag that selects the artifact backend (e.g., `github`).
2. THE Plan_Command SHALL accept a `--github-output` flag that writes `matrix`, `plan_id`, and `exec_id` to the file path in `$GITHUB_OUTPUT`.
3. WHEN `--artifact github` is set and `GITHUB_ACTIONS=true`, THE Plan_Command SHALL derive the exec-id as `gh-{GITHUB_RUN_ID}-{GITHUB_RUN_ATTEMPT}-{plan_short_sha}`.
4. WHEN `--artifact github` is set and `GITHUB_ACTIONS=true`, THE Plan_Command SHALL call `WritePlanShard()` followed by `UploadShard()` after plan generation.
5. WHEN the plan shard upload fails, THE Plan_Command SHALL print a warning to stderr and continue without aborting the plan command.
6. WHEN `--github-output` is set and `$GITHUB_OUTPUT` is a writable file path, THE Plan_Command SHALL write `matrix`, `plan_id`, and `exec_id` in GitHub Actions multiline output format.

#### Current Implementation Status

**Files:** `cmd/orun/command_plan.go`, `cmd/orun/main.go`

| Item | Status |
|------|--------|
| `--artifact` flag registered | ✅ DONE |
| `--github-output` flag registered | ✅ DONE |
| Exec-id derivation from GitHub env vars | ✅ DONE |
| `WritePlanShard()` + `UploadShard()` in `generatePlan()` | ✅ DONE |
| `$GITHUB_OUTPUT` write with matrix + plan_id + exec_id | ✅ DONE |
| Upload failure is warn-only | ✅ DONE |

**Verify:**
```bash
go test ./cmd/orun/ -run TestPlan -v
go run ./cmd/orun plan --help | grep -E 'artifact|github-output'
```

---

### Requirement 9: `orun run` GitHub Artifact Integration

**User Story:** As a CI matrix job, I want `orun run` to upload a job shard after execution completes, so that job results and logs are available for local inspection via `orun github pull`.

#### Acceptance Criteria

1. THE Run_Command SHALL accept an `--artifact` flag that selects the artifact backend (e.g., `github`).
2. WHEN `--artifact github` is set and `GITHUB_ACTIONS=true` and the run is not a dry run, THE Run_Command SHALL call `uploadJobShardsAfterRun()` after `r.Run(plan)` returns, regardless of whether the run succeeded or failed.
3. WHEN `uploadJobShardsAfterRun()` is called, THE Run_Command SHALL skip jobs with status `""`, `"running"`, or `"pending"`.
4. WHEN a job shard upload fails, THE Run_Command SHALL print a warning to stderr and continue uploading remaining shards without changing the original exit code.
5. WHEN `uploadJobShardsAfterRun()` completes, THE Run_Command SHALL remove the temporary shard directory for each job regardless of upload success or failure.
6. THE Run_Command SHALL accept an `--exec-id` flag so that matrix jobs can receive the exec-id from the plan job's `$GITHUB_OUTPUT`.

#### Current Implementation Status

**Files:** `cmd/orun/command_run.go`

| Item | Status |
|------|--------|
| `--artifact` flag registered | ✅ DONE |
| `uploadJobShardsAfterRun()` called after `r.Run()` | ✅ DONE |
| Defer semantics (upload after success or failure) | ✅ DONE |
| Upload failure is warn-only | ✅ DONE |
| `WriteJobShard()` with log copy | ✅ DONE |
| Temp dir cleanup after upload | ✅ DONE |
| `--exec-id` flag for matrix job exec-id propagation | ✅ DONE |

**Verify:**
```bash
go test ./cmd/orun/ -run TestRun -v
go run ./cmd/orun run --help | grep artifact
```

---

### Requirement 10: Synthesis and Hydration

**User Story:** As a developer, I want `orun github pull` to synthesize a complete execution view from downloaded shards, so that `orun status` and `orun logs` work on remotely-executed runs without modification.

#### Acceptance Criteria

1. WHEN `Synthesize(plan, jobs)` is called, THE Synthesizer SHALL set `Counts.Total` equal to `len(plan.Plan.Jobs)` for any non-nil plan with a non-nil `Plan` field.
2. WHEN `Synthesize(plan, jobs)` is called and `len(jobs) < len(plan.Plan.Jobs)`, THE Synthesizer SHALL return a `SynthesizedExecution` with `Status == "partial"` and `Partial == true`.
3. WHEN `Synthesize(plan, jobs)` is called and all jobs have matching shards and none failed, THE Synthesizer SHALL return `Status == "completed"`.
4. WHEN `Synthesize(plan, jobs)` is called and all jobs have matching shards and at least one failed, THE Synthesizer SHALL return `Status == "failed"`.
5. WHEN `Synthesize(plan, jobs)` is called and a plan job has no matching job shard, THE Synthesizer SHALL assign that job `status: "pending"` and increment `Counts.Pending`.
6. WHEN `Hydrate(ctx, planShard, jobShards, opts, orunDir)` is called, THE Hydrator SHALL write `metadata.json`, `state.json`, `plan.json`, `github.json`, `shards.json`, and a `logs/` directory under `.orun/executions/{exec-id}/`.
7. WHEN `Hydrate` completes, THE Hydrator SHALL update the `executions/latest` symlink to point to the hydrated exec-id.
8. WHEN `state.Store.LoadState(execID)` and `state.Store.LoadMetadata(execID)` are called on a hydrated execution directory, THE Hydrator SHALL produce files that both functions read without error.
9. WHEN `orun status` is run after `orun github pull` on a partial execution, THE Status_Command SHALL display the partial status using the `metadata.json` `Status` field.

#### Current Implementation Status

**Files:** `internal/runbundle/synthesize.go`, `internal/runbundle/hydrate.go`, `internal/runbundle/synthesize_test.go`, `internal/runbundle/hydrate_test.go`

| Item | Status |
|------|--------|
| `Synthesize()` with plan/job matching | ✅ DONE |
| Partial status when shards missing | ✅ DONE |
| `SynthesizedSummary()` / `SynthesizedStatus()` | ✅ DONE |
| `Hydrate()` writing `state.ExecMetadata` + `state.ExecState` | ✅ DONE |
| Log copy from job shards | ✅ DONE |
| `latest` symlink update | ✅ DONE |
| `orun status` displays partial state correctly | ⚠️ NEEDS VERIFICATION — `metadata.json` has `Status: "partial"` but display format needs manual check |

**Verify:**
```bash
go test ./internal/runbundle/ -run TestSynthesize -v
go test ./internal/runbundle/ -run TestHydrate -v
# After orun github pull, verify:
cat .orun/executions/latest/metadata.json | jq .status
go run ./cmd/orun status
```

---

### Requirement 11: `orun github runs` — Level 1 and Level 2 Listing

**User Story:** As a developer, I want to list workflow runs and their Orun shard status at varying levels of detail, so that I can quickly assess CI execution state without downloading full artifacts.

#### Acceptance Criteria

1. WHEN `orun github runs` is invoked, THE GitHub_CLI SHALL list workflow runs showing run ID, short SHA, branch, status, age, and Orun shard count for each run.
2. WHEN `orun github runs` is invoked and no Orun artifacts exist for a run, THE GitHub_CLI SHALL display `[no orun artifacts]` for that run.
3. THE GitHub_CLI SHALL accept `--workflow`, `--branch`, `--sha`, `--failed`, and `--limit` flags to filter the run list.
4. WHEN `orun github runs --details` is invoked, THE GitHub_CLI SHALL download the `manifest.json` file from each Orun shard and display the exact status from `manifest.Status` rather than the name-encoded status.
5. WHEN `orun github runs --details` downloads a shard ZIP to read the manifest, THE GitHub_CLI SHALL discard the full ZIP after reading `manifest.json` without writing other shard files to disk.
6. WHEN `orun github runs --details` fails to download a manifest for a specific shard, THE GitHub_CLI SHALL print a warning for that shard and continue displaying other shards.

#### Current Implementation Status

**Files:** `cmd/orun/command_github.go`, `cmd/orun/command_github_test.go`

| Item | Status |
|------|--------|
| `orun github runs` — Level 1 listing | ✅ DONE |
| `--workflow`, `--branch`, `--sha`, `--failed`, `--limit` flags | ✅ DONE |
| `orun github runs --details` — Level 2 manifest download | ✅ DONE — `DownloadManifestOnly()` helper + `printManifestDetails()` wired in Task 0141 |

**Verify:**
```bash
go run ./cmd/orun github runs --help
go run ./cmd/orun github runs --help | grep details
# Confirm --details flag exists but is not yet wired:
go test ./cmd/orun/ -run TestGithubRuns -v
```

---

### Requirement 12: `orun github pull` — Full Fan-In

**User Story:** As a developer, I want `orun github pull` to download all shards for a run and hydrate them locally, so that I can use `orun status` and `orun logs` on remotely-executed runs.

#### Acceptance Criteria

1. WHEN `orun github pull` is invoked, THE GitHub_CLI SHALL resolve the target workflow run using the provided `--run-id`, `--exec-id`, `--sha`, `--branch`, `--latest`, or `--failed` flag.
2. WHEN `orun github pull` resolves a run, THE GitHub_CLI SHALL list all Orun artifacts for that run, download each shard to a temp directory, synthesize the execution, and hydrate it into the local `.orun/` directory.
3. WHEN a shard download fails during `orun github pull`, THE GitHub_CLI SHALL print a warning for that shard and continue with remaining shards.
4. IF the plan shard is missing from the run, THEN THE GitHub_CLI SHALL return an error because synthesis requires a plan shard.
5. WHEN `orun github pull` completes, THE GitHub_CLI SHALL print a summary line showing the exec-id and synthesized status (e.g., `✓ hydrated gh-... completed 18/18`).
6. WHEN `--include-raw` is not set, THE GitHub_CLI SHALL hydrate with redacted logs by default.

#### Current Implementation Status

**Files:** `cmd/orun/command_github.go`

| Item | Status |
|------|--------|
| `orun github pull` — full fan-in + hydration | ✅ DONE |
| `--run-id`, `--exec-id`, `--sha`, `--branch`, `--latest`, `--failed` flags | ✅ DONE |
| Warning on shard download failure | ✅ DONE |
| Error when plan shard missing | ✅ DONE |
| `--include-raw` flag | ✅ DONE |

**Verify:**
```bash
go run ./cmd/orun github pull --help
go test ./cmd/orun/ -run TestGithubPull -v
```

---

### Requirement 13: `orun github status` — Lightweight Remote Status

**User Story:** As a developer, I want a quick remote status check that doesn't download full artifacts, so that I can see shard counts and execution grouping without the cost of a full pull.

#### Acceptance Criteria

1. WHEN `orun github status` is invoked, THE GitHub_CLI SHALL list Orun artifacts for the resolved run and display per-execution-group summaries showing exec-id, plan shard count, and job shard count.
2. WHEN `orun github status` is invoked, THE GitHub_CLI SHALL NOT download any shard ZIP files.

#### Current Implementation Status

**Files:** `cmd/orun/command_github.go`

| Item | Status |
|------|--------|
| `orun github status` — lightweight shard count | ✅ DONE |
| No ZIP downloads | ✅ DONE |

**Verify:**
```bash
go run ./cmd/orun github status --help
go test ./cmd/orun/ -run TestGithubStatus -v
```

---

### Requirement 14: `orun github logs` — Log Content Display

**User Story:** As a developer, I want `orun github logs` to print actual step log content from downloaded shards, so that I can read CI job output without running `orun github pull` first.

#### Acceptance Criteria

1. WHEN `orun github logs` is invoked, THE GitHub_CLI SHALL download the shard ZIP for each matching artifact and print the content of each `log:*` file to stdout.
2. WHEN printing log content, THE GitHub_CLI SHALL prefix each log section with a header in the format `=== {shard-name} / {step-id} ===`.
3. WHEN `orun github logs` is invoked and a log file cannot be read from the extracted shard directory, THE GitHub_CLI SHALL print a warning to stderr and continue with remaining log files.
4. WHEN `--job` is provided, THE GitHub_CLI SHALL filter to only artifacts whose names contain the job filter string.
5. WHEN `orun github logs` is invoked and no artifacts match the job filter, THE GitHub_CLI SHALL return an error.

#### Current Implementation Status

**Files:** `cmd/orun/command_github.go`

| Item | Status |
|------|--------|
| `orun github logs` — download shards | ✅ DONE |
| Print actual log file content | ⚠️ NEEDS WORK — currently prints `manifest.Files` keys (file names), not content |
| `=== {shard} / {step-id} ===` header format | ⚠️ NEEDS WORK — header exists for shard name but not per-step |
| `--job` filter | ✅ DONE |
| Warning on unreadable log file | ⚠️ NEEDS WORK — not implemented (no file read attempted) |

**Verify:**
```bash
go run ./cmd/orun github logs --help
# Confirm current behavior (prints file names, not content):
go test ./cmd/orun/ -run TestGithubLogs -v
# After fix, verify content is printed:
# orun github logs --latest 2>&1 | head -40
```

---

### Requirement 15: Run Resolution

**User Story:** As a developer, I want flexible run resolution so that I can target a specific workflow run by multiple identifiers, so that I don't need to look up run IDs manually.

#### Acceptance Criteria

1. WHEN `ResolveRun` is called with an explicit `RunID`, THE Run_Resolver SHALL use that run ID without querying the API for other runs.
2. WHEN `ResolveRun` is called with an `ExecID`, THE Run_Resolver SHALL parse the run ID from the exec-id format `gh-{run_id}-{attempt}-{sha}` and use it directly.
3. WHEN `ResolveRun` is called with a `SHA`, THE Run_Resolver SHALL return the most recent run for that commit SHA.
4. WHEN `ResolveRun` is called with `Failed: true`, THE Run_Resolver SHALL return the most recent run with a failure conclusion.
5. WHEN `ResolveRun` is called with no options, THE Run_Resolver SHALL return the most recent run.

#### Current Implementation Status

**Files:** `internal/artifactstore/github/resolve.go`

| Item | Status |
|------|--------|
| `ResolveRun()` with RunID priority | ✅ DONE |
| ExecID parsing | ✅ DONE |
| SHA resolution | ✅ DONE |
| Failed flag | ✅ DONE |
| Latest fallback | ✅ DONE |

**Verify:**
```bash
go test ./internal/artifactstore/github/ -run TestResolve -v
```

---

### Requirement 16: Repository Auto-Detection

**User Story:** As a developer, I want `orun github` commands to detect the GitHub repository automatically, so that I don't need to pass `--repo` on every invocation.

#### Acceptance Criteria

1. WHEN `GITHUB_REPOSITORY` is set in the environment, THE GitHub_CLI SHALL use its value as the repository identifier.
2. WHEN `GITHUB_REPOSITORY` is not set, THE GitHub_CLI SHALL attempt to parse the repository from the `git remote get-url origin` output.
3. WHEN the git remote URL is in `git@github.com:owner/repo.git` format, THE GitHub_CLI SHALL extract `owner/repo` as the repository identifier.
4. WHEN the git remote URL is in `https://github.com/owner/repo.git` format, THE GitHub_CLI SHALL extract `owner/repo` as the repository identifier.
5. IF the repository cannot be determined from either source, THEN THE GitHub_CLI SHALL return an error instructing the user to set `GITHUB_REPOSITORY`.

#### Current Implementation Status

**Files:** `cmd/orun/command_github.go`

| Item | Status |
|------|--------|
| `GITHUB_REPOSITORY` env var | ✅ DONE |
| `git remote get-url origin` fallback | ✅ DONE |
| SSH and HTTPS URL parsing | ✅ DONE |
| Error when repo not found | ✅ DONE |

**Verify:**
```bash
go test ./cmd/orun/ -run TestParseGitHubRepo -v
```

---

### Requirement 17: Workflow Template

**User Story:** As a new user, I want a ready-to-use GitHub Actions workflow template, so that I can enable Orun artifact uploads in my repository with minimal configuration.

#### Acceptance Criteria

1. THE Workflow_Template SHALL exist at `docs/examples/github-artifacts-workflow.yaml` and be valid YAML.
2. THE Workflow_Template SHALL define a `plan` job that runs `orun plan --artifact github --github-output` and exposes `matrix` and `exec_id` as job outputs.
3. THE Workflow_Template SHALL define a `run` job with `strategy.matrix` driven by the plan job's `matrix` output and `fail-fast: false`.
4. THE Workflow_Template SHALL set `permissions: actions: write` to allow artifact upload.
5. THE Workflow_Template SHALL pass `ORUN_EXEC_ID` and `ORUN_JOB_ID` as environment variables to the `run` job step.
6. WHERE a user wants a starter template in their own repository, THE Workflow_Template SHALL be copyable from `docs/examples/github-artifacts-workflow.yaml` without modification for standard use cases.
7. THE Orun_Repository SHALL contain a `.github/workflows/orun.yaml` file that demonstrates the artifact workflow for the orun project itself.

#### Current Implementation Status

| Item | Status |
|------|--------|
| `docs/examples/github-artifacts-workflow.yaml` | ✅ DONE |
| Template has `plan` job with `--artifact github --github-output` | ✅ DONE |
| Template has `run` job with matrix strategy | ✅ DONE |
| Template has `permissions: actions: write` | ✅ DONE |
| `.github/workflows/orun.yaml` in repo root | ⚠️ NEEDS WORK — does not exist |

**Verify:**
```bash
ls docs/examples/github-artifacts-workflow.yaml
cat docs/examples/github-artifacts-workflow.yaml | python3 -c "import sys,yaml; yaml.safe_load(sys.stdin)" && echo "valid YAML"
ls .github/workflows/orun.yaml 2>/dev/null || echo "MISSING"
```

---

### Requirement 18: Unit Tests — RunBundle Package

**User Story:** As a developer, I want comprehensive unit tests for the RunBundle package, so that regressions in shard writing, reading, synthesis, and hydration are caught before merging.

#### Acceptance Criteria

1. THE RunBundle_Tests SHALL include tests for `ArtifactName` / `ParseShardName` round-trip with valid inputs.
2. THE RunBundle_Tests SHALL include tests for `WritePlanShard` / `ReadPlanShard` round-trip.
3. THE RunBundle_Tests SHALL include tests for `WriteJobShard` / `ReadJobShard` round-trip including log file content.
4. THE RunBundle_Tests SHALL include a test for `Synthesize` where `len(jobs) < len(plan.Jobs)` and verify `Status == "partial"` and `Counts.Pending > 0`.
5. THE RunBundle_Tests SHALL include a test for `Hydrate` that calls `state.Store.LoadState` and `state.Store.LoadMetadata` on the hydrated directory and verifies no error is returned.
6. THE RunBundle_Tests SHALL include a test for `ValidateShardFiles` that modifies a file after writing and verifies a checksum error is returned.
7. THE RunBundle_Tests SHALL include a test for path traversal rejection in `ValidateShardFiles`.

#### Current Implementation Status

**Files:** `internal/runbundle/*_test.go`

| Item | Status |
|------|--------|
| Naming round-trip tests | ✅ DONE |
| Writer/reader round-trip tests | ✅ DONE |
| Synthesize tests | ✅ DONE |
| Hydrate tests | ✅ DONE |
| `Synthesize` partial case test (len(jobs) < len(plan.Jobs)) | ⚠️ NEEDS VERIFICATION — check synthesize_test.go for partial coverage |
| `Hydrate` + `LoadState` compatibility test | ⚠️ NEEDS WORK — not confirmed present |
| Checksum tamper test | ⚠️ NEEDS VERIFICATION |

**Verify:**
```bash
go test ./internal/runbundle/... -v -cover
go test ./internal/runbundle/ -run TestSynthesize -v
go test ./internal/runbundle/ -run TestHydrate -v
```

---

### Requirement 19: Unit Tests — GitHub Artifact Store

**User Story:** As a developer, I want unit tests for the GitHub artifact store, so that list, download, upload, and retry logic are verified without requiring a live GitHub connection.

#### Acceptance Criteria

1. THE GitHub_Store_Tests SHALL include tests for `ListOrunArtifacts` that verify only artifacts matching the `orun.v1.*` naming scheme are returned.
2. THE GitHub_Store_Tests SHALL include tests for `Download` that verify path traversal entries in a ZIP are rejected.
3. THE GitHub_Store_Tests SHALL include tests for `retryDo` that verify 429 and 5xx responses trigger retries and 4xx responses (except 429) do not.
4. THE GitHub_Store_Tests SHALL include tests for `UploadShard` that verify the function returns an error when `GITHUB_ACTIONS` is not `"true"`.
5. THE GitHub_Store_Tests SHALL include tests for `NewClient` token resolution order.

#### Current Implementation Status

**Files:** `internal/artifactstore/github/github_test.go`, `internal/artifactstore/github/upload_test.go`, `internal/artifactstore/github/retry_test.go`

| Item | Status |
|------|--------|
| List filtering tests | ✅ DONE |
| Download path traversal test | ✅ DONE |
| Retry logic tests | ✅ DONE |
| Upload outside GitHub Actions test | ✅ DONE |
| Token resolution order test | ⚠️ NEEDS VERIFICATION |

**Verify:**
```bash
go test ./internal/artifactstore/github/... -v -cover
```

---

### Requirement 20: Integration Tests — CLI Commands

**User Story:** As a developer, I want integration tests for the `orun github` CLI commands, so that the full pull → status → logs pipeline is verified with fixture data.

#### Acceptance Criteria

1. THE CLI_Integration_Tests SHALL include a test for `orun github logs` that mocks a shard download returning a shard with log files and verifies stdout contains the log file content (not just file names).
2. THE CLI_Integration_Tests SHALL include a test for `orun github pull` that uses fixture shard data, calls `Hydrate`, and verifies `orun status` reads the hydrated execution without error.
3. THE CLI_Integration_Tests SHALL include a test for partial hydration display that hydrates a partial execution (fewer job shards than plan jobs) and verifies `orun status` output reflects the partial state.
4. THE CLI_Integration_Tests SHALL include a test for `orun github runs --details` that mocks manifest downloads and verifies exact status values are displayed.

#### Current Implementation Status

**Files:** `cmd/orun/command_github_test.go`

| Item | Status |
|------|--------|
| `orun github logs` content test | ⚠️ NEEDS WORK — not present |
| `orun github pull` → `orun status` integration test | ⚠️ NEEDS WORK — not present |
| Partial hydration display test | ⚠️ NEEDS WORK — not present |
| `orun github runs --details` test | ⚠️ NEEDS WORK — not present (feature not implemented) |

**Verify:**
```bash
go test ./cmd/orun/ -run TestGithub -v
# After adding tests:
go test ./cmd/orun/ -run TestGithubLogs_PrintsContent -v
go test ./cmd/orun/ -run TestGithubPull_StatusCompatibility -v
go test ./cmd/orun/ -run TestGithubStatus_Partial -v
```

---

### Requirement 21: E2E CI Workflow Test

**User Story:** As a platform engineer, I want an end-to-end CI test that exercises the full plan → run → pull cycle, so that regressions in the artifact upload/download pipeline are caught in CI before release.

#### Acceptance Criteria

1. THE E2E_Test SHALL be a GitHub Actions workflow that runs `orun plan --artifact github --github-output`, followed by matrix `orun run --artifact github` jobs, followed by `orun github pull --latest` and `orun status`.
2. WHEN the E2E_Test workflow completes, THE E2E_Test SHALL verify that at least one plan shard and at least one job shard appear in the artifact list for the run.
3. WHEN the E2E_Test workflow completes, THE E2E_Test SHALL verify that `orun status` exits with code 0 after `orun github pull`.
4. THE E2E_Test workflow SHALL use the `examples/github-artifact-demo/` directory as the test subject.

#### Current Implementation Status

| Item | Status |
|------|--------|
| E2E CI workflow test | ⚠️ NEEDS WORK — not present |
| `examples/github-artifact-demo/` directory | ✅ DONE — exists as test subject |

**Verify:**
```bash
ls examples/github-artifact-demo/
ls .github/workflows/ | grep -i artifact
# After adding E2E workflow:
# Trigger via GitHub Actions and inspect artifact list
```

---

## Gap Summary

The following requirements have acceptance criteria that are not yet fully implemented:

| Requirement | Gap | Priority |
|-------------|-----|----------|
| Req 11 — `orun github runs --details` | ✅ Level 2 manifest download implemented (Task 0141) | — |
| Req 14 — `orun github logs` | Prints file names instead of log content | High |
| Req 10 — Partial hydration display | `orun status` partial display needs verification | Medium |
| Req 17 — Workflow template | `.github/workflows/orun.yaml` missing from repo root | Medium |
| Req 20 — CLI integration tests | Logs content, pull→status, partial display, --details tests missing | Medium |
| Req 21 — E2E CI workflow test | No E2E test workflow exists | Medium |
| Req 5 — ArtifactStore test implementations | `memory.go`/`local.go` Store implementations missing | Low |
