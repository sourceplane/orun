# Implementation Plan: GitHub Artifacts for Orun (v2)

**Date:** 2026-05-23
**Source:** `chatgpt-github-artifacts-for-orun.md`
**Module:** `github.com/sourceplane/orun`

---

## 1. Summary

Add GitHub Actions artifact support to Orun so that CI execution evidence (plans, job results, logs) is automatically uploaded as immutable GitHub artifacts by Orun itself — no `actions/upload-artifact` steps in workflow YAML. The local CLI can then list, pull, and reconstruct these shards into the native `.orun/executions/` layout for offline inspection with existing `orun status`, `orun logs`, and `orun describe` commands.

**Key design decisions:**
- No collector/index job in CI. Each Orun invocation uploads one immutable shard.
- Local CLI performs lazy fan-in when pulling.
- Artifact naming scheme: `orun.v1.<exec-id>.<role>.<suffix>.<status>`
- **v1 upload via embedded `@actions/artifact` Node.js helper.** Native Go Twirp upload is a future experimental optimization, not the first implementation. GitHub's public REST API documents artifact list/get/download/delete, not upload. `@actions/artifact` is the supported programmatic upload path.
- Two shard types: **plan** and **job**.
- Portable **RunBundleShard** schema — GitHub is the first storage backend, not the schema owner.
- Three levels of remote inspection: artifact names only, manifest-only download, full shard download.
- **Partial hydration** as a first-class state for cancelled/incomplete CI runs.

---

## 2. Package Map

The plan introduces four new package trees:

```
internal/runbundle/                  # Orun-owned evidence format (schema, writer, reader, synthesize, hydrate)
internal/artifactstore/              # Generic list/push/pull abstraction
internal/artifactstore/github/       # GitHub implementation (list, download, upload via embedded helper)
cmd/orun/github/                     # CLI commands: runs, pull, status, logs
spikes/github-artifact-upload-helper/ # Design spike/proof before Phase 1
```

**Why this structure:** GitHub artifacts are only the first storage backend. The same shard format and `Store` interface should later support R2, S3, local directories, or Orun Cloud. Separating `runbundle` (the format Orun owns) from `artifactstore` (the transport abstraction) from `github` (the first adapter) keeps that boundary clean.

---

## 3. Schema: `internal/runbundle/schema.go`

### 3.1 Manifest (portable, not GitHub-specific)

```go
package runbundle

type ShardRole string

const (
    ShardRolePlan ShardRole = "plan"
    ShardRoleJob  ShardRole = "job"
)

// RunBundleShardManifest — embedded in every shard artifact.
// Portable across storage backends (GitHub, R2, S3, Orun Cloud).
type RunBundleShardManifest struct {
    APIVersion    string            `json:"apiVersion"`    // "orun.io/v1alpha1"
    Kind          string            `json:"kind"`           // "RunBundleShard"
    SchemaVersion string            `json:"schemaVersion"`  // "1.0.0"
    Role          ShardRole         `json:"role"`
    ExecID        string            `json:"execId"`
    PlanID        string            `json:"planId"`
    JobUID        string            `json:"jobUid,omitempty"`
    JobID         string            `json:"jobId,omitempty"`
    Component     string            `json:"component,omitempty"`
    Environment   string            `json:"environment,omitempty"`
    Composition   string            `json:"composition,omitempty"`
    Profile       string            `json:"profile,omitempty"`
    Status        string            `json:"status,omitempty"`   // job terminal status
    StartedAt     string            `json:"startedAt,omitempty"`
    FinishedAt    string            `json:"finishedAt,omitempty"`
    Source        ShardSource       `json:"source,omitempty"`   // provenance, not GitHub-specific
    Files         map[string]string `json:"files"`              // logical name → relative path
}

// ShardSource describes where this shard originated.
type ShardSource struct {
    Type       string `json:"type"`       // "github-actions", "local", "r2", "s3"
    Repository string `json:"repository,omitempty"`
    RunID      string `json:"runId,omitempty"`
    RunAttempt string `json:"runAttempt,omitempty"`
    Workflow   string `json:"workflow,omitempty"`
    SHA        string `json:"sha,omitempty"`
    Ref        string `json:"ref,omitempty"`
    EventName  string `json:"eventName,omitempty"`
}

// Checksums — checksums.json inside plan shard
type Checksums struct {
    Algorithm string            `json:"algorithm"` // "sha256"
    Files     map[string]string `json:"files"`      // relative path → hex digest
}
```

### 3.2 Synthesized state (after local fan-in)

```go
// SynthesizedExecution is what hydrate produces for local .orun/ state.
type SynthesizedExecution struct {
    ExecID       string                `json:"execId"`
    PlanID       string                `json:"planId"`
    Status       string                `json:"status"`  // "completed" | "failed" | "partial" | "cancelled"
    Partial      bool                  `json:"partial,omitempty"`
    PartialReason string               `json:"partialReason,omitempty"`
    Counts       JobCounts             `json:"counts"`
    Jobs         map[string]JobShardRef `json:"jobs"`
    PlanShard    ShardRef              `json:"planShard"`
    Source       ShardSource           `json:"source,omitempty"`
    CreatedAt    string                `json:"createdAt"`
}

type JobCounts struct {
    Total     int `json:"total"`
    Completed int `json:"completed"`
    Failed    int `json:"failed"`
    Cancelled int `json:"cancelled"`
    Skipped   int `json:"skipped"`
    Pending   int `json:"pending"`
}

type JobShardRef struct {
    JobUid     string `json:"jobUid"`
    JobID      string `json:"jobId"`
    Status     string `json:"status"`
    ShardName  string `json:"shardName"`
    StartedAt  string `json:"startedAt,omitempty"`
    FinishedAt string `json:"finishedAt,omitempty"`
}

type ShardRef struct {
    Name   string `json:"name"`
    Role   string `json:"role"`
    PlanID string `json:"planId,omitempty"`
}
```

### 3.3 Artifact naming

```go
// ArtifactName format: orun.v1.<exec-id>.<role>.<suffix>.<status>
// exec-id must be constrained to [a-zA-Z0-9_-]+
//
// Examples:
//   orun.v1.gh-26185145757-1-a1b2c3d4.plan.a1b2c3d4.created
//   orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.failed
func ArtifactName(execID string, role ShardRole, suffix, status string) string

type ParsedShardName struct {
    ExecID string
    Role   ShardRole
    Suffix string
    Status string
}

func ParseShardName(name string) (*ParsedShardName, bool)

// ExecID constructs the execution ID for GitHub Actions runs.
// Format: gh-{run_id}-{attempt}-{plan_short_sha}
func ExecID(runID, runAttempt, planShortSHA string) string
```

---

## 4. Shard Layout: `internal/runbundle/writer.go` / `reader.go`

### 4.1 Plan shard

```
{shardDir}/
  manifest.json
  plan.json
  matrix.json
  trigger.json
  git.json
  checksums.json
```

```go
func WritePlanShard(ctx context.Context, opts WritePlanShardOptions) (*Shard, error)

type WritePlanShardOptions struct {
    ExecID     string
    Plan       *model.Plan
    Source     ShardSource
    OutputDir  string  // staging directory for the shard
}
```

### 4.2 Job shard

```
{shardDir}/
  manifest.json
  job.json
  state.json
  steps.jsonl
  events.jsonl
  summary.md
  logs/
    <step-id>.log
```

```go
func WriteJobShard(ctx context.Context, opts WriteJobShardOptions) (*Shard, error)

type WriteJobShardOptions struct {
    ExecID    string
    PlanID    string
    JobUID    string
    JobID     string
    Component string
    Env       string
    Profile   string
    Status    string
    Source    ShardSource
    State     *model.JobState
    LogsDir   string
    OutputDir string
}
```

### 4.3 Reader

```go
func ReadShardManifest(dir string) (*RunBundleShardManifest, error)
func ReadPlanShard(dir string) (*PlanShard, error)
func ReadJobShard(dir string) (*JobShard, error)
```

Validation rules:
- `schemaVersion` must be supported.
- All files listed in `manifest.json.files` must exist on disk.
- Checksums must match (when `checksums.json` is present).
- Job shard `planId` must match plan shard.
- No path traversal in extracted files.

### 4.4 Synthesize and Hydrate

```go
// Synthesize takes one plan shard + all job shards and produces a SynthesizedExecution.
// Supports partial state: missing shards produce status="partial".
func Synthesize(plan *PlanShard, jobs []*JobShard) (*SynthesizedExecution, error)

type HydrateOptions struct {
    ExecID     string
    Source     ShardSource
    Overwrite  bool
    IncludeRaw bool  // include unredacted logs
}

// Hydrate reconstructs .orun/executions/{exec-id}/ from synthesized state.
// Uses existing internal/state structs for compatibility with orun status/logs.
func Hydrate(ctx context.Context, exec *SynthesizedExecution, opts HydrateOptions, orunDir string) (*HydrateResult, error)
```

Hydrated local layout:

```
.orun/
  plans/
    {planID}.json
  executions/
    latest -> {exec-id}
    {exec-id}/
      metadata.json     # ExecMetadata (compatible with existing state types)
      github.json       # ShardSource
      plan.json         # copy of plan
      state.json        # SynthesizedExecution
      shards.json       # list of shard names + digests for provenance
      logs/
        {jobID}/
          {stepID}.log
```

**Critical rule:** Use existing `internal/state.ExecMetadata` and `internal/state.ExecState` structs wherever possible. Do not invent a parallel state.json shape if existing `orun status` / `orun logs` readers expect another structure.

Partial hydration output:

```json
{
  "status": "partial",
  "partialReason": "missing_job_shards",
  "counts": { "total": 18, "completed": 12, "failed": 1, "cancelled": 0, "skipped": 3, "pending": 2 }
}
```

Then `orun status` displays:

```
EXECUTION gh-26185145757-1-a1b2c3d4  ◐ partial  13/18 shards
```

---

## 5. Generic Store Interface: `internal/artifactstore/store.go`

```go
package artifactstore

type Store interface {
    Upload(ctx context.Context, shard *runbundle.Shard) (*UploadResult, error)
    List(ctx context.Context, opts ListOptions) ([]RemoteShard, error)
    Download(ctx context.Context, shard RemoteShard, destDir string) (*DownloadedShard, error)
}

type RemoteShard struct {
    Name       string
    ID         string
    SizeBytes  int64
    CreatedAt  time.Time
    ExpiresAt  time.Time
    Parsed     *runbundle.ParsedShardName
    SourceMeta map[string]string   // backend-specific metadata
}

type ListOptions struct {
    RunID    int64
    ExecID   string
    Prefix   string  // filter by name prefix
}

type UploadResult struct {
    ID       string
    Name     string
    Size     int64
    Digest   string  // backend-reported digest if available
}

type DownloadedShard struct {
    Name     string
    Dir      string  // extracted shard directory
    Shard    *runbundle.RunBundleShardManifest
}
```

This keeps GitHub as one implementation of `Store`, not the architecture itself. Adding R2 or S3 later means implementing the same three-method interface.

---

## 6. GitHub Store Implementation: `internal/artifactstore/github/`

### 6.1 Client

```go
func NewClient(ctx context.Context, repo string) (*Client, error)

type Client struct {
    repo    string  // "owner/repo"
    token   string
    baseURL string
    http    *http.Client
}
```

Token resolution order:
1. `GITHUB_TOKEN` env var
2. `GH_TOKEN` env var
3. `gh auth token` via subprocess
4. Explicit `--token` flag

### 6.2 List

```go
func (c *Client) ListWorkflowRuns(ctx context.Context, opts ListRunOptions) ([]WorkflowRun, error)
func (c *Client) ListArtifacts(ctx context.Context, runID int64) ([]artifactstore.RemoteShard, error)
func (c *Client) ListOrunArtifacts(ctx context.Context, runID int64) ([]artifactstore.RemoteShard, error)
```

Uses:
- `GET /repos/{owner}/{repo}/actions/runs`
- `GET /repos/{owner}/{repo}/actions/runs/{run_id}/artifacts`

### 6.3 Download

```go
func (c *Client) Download(ctx context.Context, shard artifactstore.RemoteShard, destDir string) (*artifactstore.DownloadedShard, error)
func (c *Client) DownloadByName(ctx context.Context, runID int64, name, destDir string) (*artifactstore.DownloadedShard, error)
```

Uses:
- `GET /repos/{owner}/{repo}/actions/artifacts/{artifact_id}/{zip}`

Includes path traversal defense when extracting ZIP.

### 6.4 Resolve

```go
type ResolveOpts struct {
    RunID    int64
    ExecID   string
    SHA      string
    Branch   string
    Failed   bool
    Workflow string
}

func ResolveRun(ctx context.Context, c *Client, opts ResolveOpts) (*WorkflowRun, error)
```

Resolution algorithm:
1. `--run-id`: fetch run directly by ID.
2. `--exec-id`: parse `gh-{run_id}-{attempt}-{plan_sha}`, fetch run + attempt.
3. `--sha`: list runs for SHA, pick latest.
4. `--failed`: list runs with `conclusion=failure`, pick latest.
5. Default: latest run for current branch.

### 6.5 Upload (v1: embedded `@actions/artifact` helper)

```go
func (c *Client) Upload(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error)
```

**v1 implementation:** Invoke an embedded Node.js helper that uses the `@actions/artifact` package. This is the supported programmatic upload path. The native Go Twirp reimplementation is deferred to a future experimental phase.

Structure:

```
internal/artifactstore/github/
  upload.go           # Go entry point: detects env, extracts + invokes helper
  helper/
    upload.mjs        # ESM module using @actions/artifact
    package.json      # declares @actions/artifact dependency
```

`upload.go`:
```go
//go:embed helper/*
var helperFS embed.FS

func (c *Client) Upload(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error) {
    if !IsGitHubActions() {
        return nil, fmt.Errorf("github upload only supported inside GitHub Actions")
    }

    name := runbundle.ArtifactName(shard.ExecID, shard.Role, shard.Suffix, shard.Status)

    // Extract helper to temp directory on first use
    helperDir, err := ensureHelperExtracted(ctx)

    // Run: node upload.mjs <shardDir> <artifactName> [retentionDays]
    cmd := exec.CommandContext(ctx, "node", "upload.mjs",
        shard.Dir, name, strconv.Itoa(retentionDays()))
    cmd.Dir = helperDir
    cmd.Env = append(os.Environ(),
        "ACTIONS_RUNTIME_TOKEN="+os.Getenv("ACTIONS_RUNTIME_TOKEN"),
        "ACTIONS_RESULTS_URL="+os.Getenv("ACTIONS_RESULTS_URL"),
    )

    output, err := cmd.CombinedOutput()

    // Parse JSON output: { "id": "...", "name": "...", "size": 123 }
    var result UploadResult
    json.Unmarshal(output, &result)
    return &result, nil
}
```

`upload.mjs`:
```javascript
import { UploadArtifactClient } from '@actions/artifact';

async function main() {
    const [shardDir, artifactName, retentionDays] = process.argv.slice(2);
    const client = new UploadArtifactClient();
    const result = await client.uploadArtifact(artifactName, shardDir, {
        retentionDays: parseInt(retentionDays, 10) || 14,
    });
    console.log(JSON.stringify({
        id: result.id,
        name: artifactName,
        size: result.size,
    }));
}

main().catch(e => {
    console.error(e.message);
    process.exit(1);
});
```

`package.json`:
```json
{
    "type": "module",
    "dependencies": {
        "@actions/artifact": "^2.2.0"
    }
}
```

**Why `@actions/artifact` and not native Twirp:** GitHub's REST API documents artifact list/get/download/delete, not upload. The `@actions/artifact` package is the official programmatic upload path used by `actions/upload-artifact`. Reimplementing the Twirp wire protocol means depending on undocumented internal behavior. The helper approach respects GitHub's supported surface while keeping the workflow YAML free of upload steps.

**Per-job artifact limit:** `@actions/artifact` v2 has a limit of 10 artifacts per job. Orun produces at most **one job shard per matrix cell**, so this is not a constraint in practice.

**Runtime options:**
```
ORUN_ARTIFACT_BACKEND=github        # select the GitHub store
ORUN_ARTIFACT_UPLOAD=true           # enable upload
ORUN_ARTIFACT_RETENTION_DAYS=14     # override retention
ORUN_SKIP_ARTIFACT_UPLOAD=true      # disable upload for debugging
ORUN_ARTIFACT_UPLOAD_MODE=js        # v1 default (embedded helper)
ORUN_ARTIFACT_UPLOAD_MODE=native    # future experimental
```

### 6.6 Future: Native Go Twirp Upload

This is explicitly NOT v1. It should be implemented only after:
1. The full feature set works with the embedded helper.
2. The `@actions/artifact` wire format has been formally documented or a maintained Go client exists.
3. Behind `ORUN_ARTIFACT_UPLOAD_MODE=native` as an experimental opt-in.

When implemented, it would live in:
```
internal/artifactstore/github/
  upload_native.go     // build tag: experimental
  upload_common.go     // shared helpers (tar+gzip, chunking, env detection)
```

---

## 7. CLI Commands: `cmd/orun/github/`

```
cmd/orun/github/
  root.go        # "orun github" parent command
  runs.go        # "orun github runs" — list workflow runs
  pull.go        # "orun github pull" — download + hydrate
  status.go      # "orun github status" — quick remote status
  logs.go        # "orun github logs" — remote logs
```

### 7.1 `orun github runs`

```
orun github runs [flags]

Flags:
  --workflow string    workflow filename filter (default "orun.yaml")
  --branch string      branch filter
  --sha string         commit SHA filter
  --failed             show only failed runs
  --limit int          max runs to show (default 10)
  --details            download manifests for accurate status (slower)
```

**Three levels of detail:**

| Level | What happens | Speed |
|-------|-------------|-------|
| Level 1 (default) | List workflow runs + artifact names only. Parse exec-id, role, status from artifact names. Approximate. | Fast |
| Level 2 (`--details`) | Download all plan shard manifests (no logs). Exact status. | Medium |
| Level 3 (`orun github pull`) | Full shard download + hydrate. | Slowest |

Example output:

```
EXECUTION                         STATUS    JOBS       SHA       AGE
gh-26185145757-1-a1b2c3d4         failed    17✓ 1✗     abc123    12m
gh-26184210001-1-d9e8f7a6         passed    18✓        def456    1h
```

### 7.2 `orun github pull`

```
orun github pull [flags]

Flags:
  --run-id int          explicit GitHub run ID
  --exec-id string      explicit execution ID (gh-<run>-<attempt>-<sha>)
  --sha string          pull latest for this SHA
  --branch string       pull latest for this branch (default: current)
  --latest              pull latest run (default)
  --failed              pull latest failed run
  --include-raw         include unredacted logs (trusted only)
  --orun-dir string     target .orun directory (default: ./.orun)
```

Algorithm:
1. Resolve repo from git remote.
2. Resolve workflow run via `ResolveRun`.
3. List Orun artifacts for that run.
4. Group artifacts by execId.
5. Download plan shard.
6. Download all job shards.
7. Validate checksums, handle missing shards gracefully.
8. Call `runbundle.Synthesize` → `runbundle.Hydrate`.
9. Print compact status summary (reuse existing status renderer).

### 7.3 `orun github status`

Lightweight: download only manifests (no logs), synthesize state, render with existing renderers.

### 7.4 `orun github logs`

Download specific job artifact shard(s), extract log files, render.

```
orun github logs --latest-failed --failed
orun github logs --exec-id gh-... --job cloudflare-hyperdrive@stage-preview.plan
```

---

## 8. Integration into Existing Commands

### 8.1 `orun plan` — add `--artifact` flag and `--github-output`

```bash
orun plan \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH" \
  --artifact github \
  --github-output
```

`--github-output` writes directly to `$GITHUB_OUTPUT`, removing the need for fragile `jq` in CI workflow YAML:

```
matrix=<json>
plan_id=<short>
exec_id=<exec-id>
```

Behavior when `--artifact github` is set inside GitHub Actions:
1. Compile plan.
2. Derive exec ID: `gh-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>-<plan-short-sha>`.
3. Write plan shard via `runbundle.WritePlanShard`.
4. Upload plan shard via `artifactstore/github.Upload`.
5. If `--github-output`, write to `$GITHUB_OUTPUT`.

### 8.2 `orun run` — add `--artifact` flag, fix fresh-runner plan issue

**Critical fix:** A matrix job runs on a fresh machine. It will not have `.orun/plans/latest.json` from the planner job. There are three models:

| Model | Description | Recommendation |
|-------|-------------|---------------|
| Recompile plan in every matrix job | `orun run --from-ci github --job ...` compiles deterministically, then runs one job | **v1** |
| Orun downloads plan shard before run | `orun run` auto-fetches plan artifact by `ORUN_EXEC_ID` | v1.5 |
| Workflow downloads plan artifact | Requires `actions/download-artifact` step | Reject |

**v1 contract:**

```bash
orun run \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH" \
  --exec-id "$ORUN_EXEC_ID" \
  --job "$ORUN_JOB_ID" \
  --artifact github \
  --gha
```

**Upload with defer/finally semantics:**

```go
func runJob(cmd *cobra.Command, args []string) {
    exitCode := 1
    var jobShardDir string

    defer func() {
        if artifactBackend == "github" && artifactUpload && IsGitHubActions() {
            shard, err := runbundle.WriteJobShard(ctx, writeOpts)
            if err != nil {
                ui.Warn("failed to write job shard: %v", err)
                os.Exit(exitCode)
            }
            _, err = store.Upload(ctx, shard)
            if err != nil {
                ui.Warn("failed to upload job artifact: %v", err)
            }
        }
        os.Exit(exitCode)
    }()

    exitCode = runner.Run(ctx, runOpts)
}
```

Key rule: **Job shard upload must always run, even on failure.** The upload happens in `defer`, after the runner produces the exit code. The original exit code is preserved — a failed upload warns but does not change the job conclusion.

### 8.3 Env-based activation

```go
ORUN_ARTIFACT_BACKEND=github    # implicit --artifact github
ORUN_ARTIFACT_UPLOAD=true       # enable upload
ORUN_EXEC_ID=gh-...             # set by planner outputs
ORUN_JOB_ID=...                 # set by workflow matrix
```

---

## 9. GitHub Actions Workflow Template

```yaml
# .github/workflows/orun.yaml
name: orun

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read
  actions: read

jobs:
  plan:
    runs-on: ubuntu-latest
    outputs:
      matrix: ${{ steps.plan.outputs.matrix }}
      exec_id: ${{ steps.plan.outputs.exec_id }}
    steps:
      - uses: actions/checkout@v4

      - name: Plan
        id: plan
        env:
          ORUN_ARTIFACT_BACKEND: github
          ORUN_ARTIFACT_UPLOAD: "true"
        run: |
          orun plan \
            --from-ci github \
            --event-file "$GITHUB_EVENT_PATH" \
            --artifact github \
            --github-output

  run:
    needs: plan
    strategy:
      fail-fast: false
      matrix:
        job: ${{ fromJson(needs.plan.outputs.matrix) }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run
        env:
          ORUN_EXEC_ID: ${{ needs.plan.outputs.exec_id }}
          ORUN_JOB_ID: ${{ matrix.job.id }}
          ORUN_ARTIFACT_BACKEND: github
          ORUN_ARTIFACT_UPLOAD: "true"
        run: |
          orun run \
            --from-ci github \
            --event-file "$GITHUB_EVENT_PATH" \
            --exec-id "$ORUN_EXEC_ID" \
            --job "$ORUN_JOB_ID" \
            --artifact github \
            --gha
```

**No upload step. No download step. No collector job.** Orun handles every artifact operation internally.

---

## 10. Implementation Phases

### Phase 0 — Design Spike

**Goal:** Prove three things before writing production code.

1. Orun can upload one artifact from inside GitHub Actions without an `actions/upload-artifact` workflow step.
2. The uploaded artifact is visible through the GitHub artifact REST API before the workflow finishes.
3. Local Orun can list and download that artifact using `GITHUB_TOKEN` or `gh auth token`.

**Deliverable:**
```
spikes/github-artifact-upload-helper/
  upload.mjs
  package.json
  README.md
```

**Exit criteria:**
- A minimal Node.js script using `@actions/artifact` successfully uploads a test directory as a GitHub artifact when run inside a GitHub Actions workflow.
- The artifact is listable and downloadable via the REST API before the workflow finishes.
- The download ZIP contents match the uploaded directory.

### Phase 1 — RunBundle Schema

**Files:** `internal/runbundle/schema.go`, `internal/runbundle/naming.go`, `internal/runbundle/checksums.go`

**Deliverable:** Go types and naming functions with tests.

- Define `RunBundleShardManifest`, `ShardSource`, `Checksums`, `SynthesizedExecution`.
- Implement `ArtifactName`, `ParseShardName`, `ExecID`.
- Unit tests: marshal/unmarshal round-trip, name construction and parsing, unsafe character rejection.

### Phase 2 — Shard Writer/Reader

**Files:** `internal/runbundle/writer.go`, `internal/runbundle/reader.go`, `internal/runbundle/validate.go`

**Deliverable:** Write and read plan/job shards with validation.

- `WritePlanShard`, `WriteJobShard` — produce exact directory layouts.
- `ReadShardManifest`, `ReadPlanShard`, `ReadJobShard` — read and validate.
- Validation: schema version, file existence, checksum matching, path traversal defense.
- Unit tests: produce shards, verify layout, reject invalid inputs.

### Phase 3 — Synthesize and Hydrate

**Files:** `internal/runbundle/synthesize.go`, `internal/runbundle/hydrate.go`

**Deliverable:** Local fan-in of shards into `.orun/executions/`.

- `Synthesize`: merge plan + job shards into `SynthesizedExecution`.
- `Hydrate`: reconstruct `.orun/executions/{exec-id}/` layout.
- **Use existing `internal/state` structs** for `metadata.json` and `state.json` compatibility.
- Partial hydration: missing shards produce `status: "partial"`.
- Unit tests: write shards → synthesize → hydrate → verify `.orun/` layout works with existing `orun status` and `orun logs`.

### Phase 4 — Generic Store Interface

**Files:** `internal/artifactstore/store.go`

**Deliverable:** Store interface with no-op in-memory implementation.

- Define `Store` interface with `Upload`, `List`, `Download`.
- In-memory implementation for unit testing.

### Phase 5 — GitHub Store: List and Download

**Files:** `internal/artifactstore/github/client.go`, `list.go`, `download.go`, `resolve.go`

**Deliverable:** Authenticated GitHub API client for listing and downloading artifacts.

- Token resolution: `GITHUB_TOKEN`, `GH_TOKEN`, `gh auth token`.
- `ListWorkflowRuns`, `ListArtifacts`, `ListOrunArtifacts`.
- `Download`, `DownloadByName` with path traversal defense.
- `ResolveRun` with the resolution algorithm.
- Unit tests with `httptest.Server` mock; integration test against public repo artifacts.
- Reuse patterns from existing `internal/gha/fetch.go`.

### Phase 6 — GitHub Store: Upload via Embedded Helper

**Files:** `internal/artifactstore/github/upload.go`, `internal/artifactstore/github/helper/upload.mjs`, `helper/package.json`

**Deliverable:** Orun can upload shards from within GitHub Actions.

- Embed `upload.mjs` and `package.json` via `embed.FS`.
- Extract helper to temp directory, install dependencies (or pre-bundle).
- Invoke via `exec.CommandContext`.
- Parse JSON result for ID, name, size.
- Guardrails: one artifact per invocation, never upload same name twice, fail gracefully on duplicate.

### Phase 7 — `orun plan` Integration

**Files:** `cmd/orun/plan.go`

**Deliverable:** `orun plan --artifact github` with `--github-output`.

- Add `--artifact` flag.
- Add `--github-output` flag that writes `$GITHUB_OUTPUT`.
- After plan compilation inside GitHub Actions: write plan shard + upload.

### Phase 8 — `orun run` Integration

**Files:** `cmd/orun/run.go`

**Deliverable:** `orun run --artifact github` with `--from-ci github` recompile and defer/finally upload.

- Add `--artifact` flag.
- Fix fresh-runner plan issue by supporting `--from-ci github` on `orun run`.
- Implement defer/finally upload of job shard.
- Support `--exec-id` flag for explicit exec ID.

### Phase 9 — CLI Commands

**Files:** `cmd/orun/github/root.go`, `runs.go`, `pull.go`, `status.go`, `logs.go`

**Deliverable:** All `orun github` subcommands.

- Register `github` subcommand on root.
- Wire resolve → list → download → synthesize → hydrate pipeline.
- Three-level detail for `runs`.
- Render output using existing `internal/render/` and `internal/ui/`.

### Phase 10 — Workflow Template and Docs

**Files:** `docs/examples/github-artifacts-workflow.yaml`, `docs/github-artifacts.md`

**Deliverable:** Documented workflow template and usage guide.

### Phase 11 (Future) — Native Go Twirp Upload

**Files:** `internal/artifactstore/github/upload_native.go`

**Deliverable:** Experimental native upload behind `ORUN_ARTIFACT_UPLOAD_MODE=native`.

Only after Phase 0–10 are complete and stable. Not part of v1.

---

## 11. Test Matrix

| Area | Test | Approach |
|------|------|----------|
| Naming | Parse/build artifact names, reject unsafe chars | Unit |
| Writer | Plan/job shards produce exact layout | Unit |
| Reader | Reject missing files, bad checksums, bad schema | Unit |
| Synthesis | Complete, failed, cancelled, partial runs | Unit |
| Hydration | Hydrated `.orun/` works with `status` and `logs` | Unit + integration |
| Store interface | In-memory implementation round-trip | Unit |
| GitHub list | REST pagination, filters, auth | Unit (httptest) |
| GitHub download | ZIP extraction, path traversal defense | Unit (httptest) |
| GitHub upload | Helper invocation, result parsing | Unit (mock exec) |
| Plan integration | Flag parsing, `--github-output` output format | Unit |
| Run integration | Flag parsing, defer/finally semantics | Unit |
| CI E2E | Real GitHub Actions: plan + matrix run upload shards | E2E (CI) |
| Local E2E | `orun github pull --failed && orun status` | E2E (manual + CI) |

---

## 12. Key Design Constraints

### 12.1 No collector job

Each Orun invocation uploads one immutable shard. The index is synthesized locally on pull. This avoids race conditions, parallel-write conflicts, and extra CI jobs.

### 12.2 RunBundle is the portable format

The manifest uses `kind: RunBundleShard` (not `GitHubArtifactShard`) and a generic `source` block. This lets the same shard format work with R2, S3, or Orun Cloud later.

### 12.3 Partial hydration

Missing shards produce `status: "partial"` in synthesized state. Hydration never fails due to missing job shards (missing plan shard still fails).

### 12.4 Compatibility with existing state types

The hydrated `.orun/executions/{exec-id}/` must produce `metadata.json` and `state.json` that existing `internal/state` readers already understand. Do not invent a parallel state shape.

### 12.5 defer/finally upload semantics

Job shard upload always runs, even when the job itself fails. The original exit code is preserved. A failed upload warns but does not change job conclusion.

### 12.6 Minimal CI YAML

The workflow must have:
- No `actions/upload-artifact` step.
- No `actions/download-artifact` step.
- No collector job.
- No fragile `jq` piping (use `--github-output` instead).

### 12.7 Fresh-runner plan resolution

Matrix jobs never assume `.orun/plans/latest.json` exists. They either:
- Recompile the plan deterministically (`--from-ci github`), or
- Auto-download the plan shard by `ORUN_EXEC_ID` (v1.5).

### 12.8 Security

- Default `orun github pull` hydrates only redacted logs. Add `--include-raw` for trusted users.
- Never log or persist the GitHub token.
- Do not include resolved env values in artifacts — only key names and source levels.
- Path traversal defense in ZIP extraction.
- Private repo local pull requires `Actions: read` fine-grained token permission.

---

## 13. Risk Assessment

| Risk | Impact | Likelihood | Mitigation |
|------|--------|-----------|------------|
| `@actions/artifact` Node.js dependency adds runtime requirement (Node.js) in CI images | Medium | Medium | Node.js is already present in `ubuntu-latest` GitHub runners. Pin `@actions/artifact` version. Pre-bundle `node_modules` in the embedded helper. |
| GitHub artifact upload rate limits / per-job artifact limits (10/job) | Medium | Low | One shard per matrix job is well under the limit. |
| Hydrated layout diverges from local execution layout | High | Low | Reuse `internal/state` types; verify against current `state.go` and `store.go`. |
| Matrix job plan resolution: `--from-ci github` must be deterministic | High | Low | Orun's planner already produces deterministic plans from the same inputs. Verify the compiler path works without prior `.orun/` state. |
| `gh auth token` unavailable or enterprise GitHub host | Medium | Low | Support `GITHUB_TOKEN` and `GH_TOKEN` env vars; document GHES limitations for v4 artifacts. |
| Partial download: large runs may have many job shards | Low | Low | Parallel downloads; show progress; handle partial state gracefully. |

---

## 14. Future Considerations (not in v1)

- **`orun pull --from s3/r2`** — same `Store` interface, different backend.
- **Native Go Twirp upload** — behind `ORUN_ARTIFACT_UPLOAD_MODE=native`.
- **Streaming logs from live CI runs** — requires `orun-backend` as a real-time backend; artifacts are post-hoc only.
- **Auto-fetch plan shard** in `orun run` (v1.5) — `orun run` downloads plan artifact by `ORUN_EXEC_ID` instead of recompiling.
- **Long-term artifact retention** — periodic cleanup of old local hydrated executions via `orun gc`.
- **Artifact reaping from CLI** — `orun github prune` to delete old artifacts via the GitHub API.
- **Backend adapter interface in `orun pull`** — `orun pull --from github-artifacts` vs `orun pull --from orun-backend`.