Yes. The current plan is directionally strong, but I would change the implementation strategy in a few important ways.

The biggest issue is that the plan jumps too quickly into a **native Go reimplementation of GitHub’s artifact upload protocol**. That is the riskiest part. GitHub’s REST API documents artifact list/get/download/delete operations, but not a normal public upload endpoint. Upload is officially exposed through `actions/upload-artifact` / `@actions/artifact`, and `@actions/artifact` is the programmatic package powering artifact upload/download. ([GitHub Docs][1]) ([GitHub][2])

So I would not make native Twirp upload the first implementation. I would make it a later optimization behind an experimental flag.

The current implementation plan also correctly captures the core product goal: Orun itself uploads immutable plan/job shards, CI has no collector/index job, and local `orun github pull` performs lazy fan-in into `.orun/executions/`. 

## Key improvements to the current plan

### 1. Do not start with native Go Twirp upload

Replace this:

```text
internal/githubartifacts/upload.go
  native Go implementation of GitHub Actions artifact Twirp protocol
```

With this:

```text
internal/artifactupload/
  uploader.go          # generic upload interface
  github_js.go         # stable v1 path using @actions/artifact helper
  github_native.go     # experimental future native Go implementation
```

For v1, Orun should invoke a tiny embedded Node helper that uses `@actions/artifact`.

That still satisfies the product rule:

```text
No actions/upload-artifact step in workflow YAML.
Orun uploads artifacts by itself.
```

But it avoids depending on undocumented GitHub internal wire behavior. GitHub’s artifact v4 model gives important guarantees Orun needs: artifacts are immediately available through UI/API, immutable after upload, and cannot be modified by later jobs. It also has a per-job artifact limit of 10, so Orun must keep each CI job to one job-shard artifact. ([GitHub][2])

### 2. Fix the matrix runner plan problem

The current workflow has this bug:

```yaml
orun run latest --job "${{ matrix.job.id }}" --gha
```

A matrix job runs on a fresh machine. It will not have `.orun/plans/latest.json` from the planner job.

There are three possible models:

| Model                                | Description                                                                         | Recommendation         |
| ------------------------------------ | ----------------------------------------------------------------------------------- | ---------------------- |
| Recompile plan in every matrix job   | `orun run --from-ci github --job ...` compiles deterministically, then runs one job | Best v1                |
| Orun downloads plan shard before run | `orun run latest` auto-fetches plan artifact by `ORUN_EXEC_ID`                      | Good v1.5              |
| Workflow downloads plan artifact     | Requires `actions/download-artifact` step                                           | Reject for this design |

Best v1 contract:

```bash
orun run \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH" \
  --exec-id "$ORUN_EXEC_ID" \
  --job "${{ matrix.job.id }}" \
  --artifact github \
  --gha
```

This keeps the workflow free of artifact glue and uses Orun’s deterministic compiler model.

### 3. Split “artifact schema” from “GitHub transport”

The current plan mixes these concerns:

```text
internal/artifacts
internal/githubartifacts
```

That is close, but I would make the boundary stricter:

```text
internal/runbundle/        # Orun-owned evidence format
internal/artifactstore/    # generic list/push/pull abstraction
internal/artifactstore/github/
cmd/orun/github/
```

Why: GitHub artifacts are only the first storage backend. The same shard format should later work with R2, S3, local directories, or Orun backend.

### 4. Keep shard format Orun-native, not GitHub-native

Use a generic name like **Run Bundle Shard**, not `GitHubArtifactShard`, inside manifest schemas.

Better:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "RunBundleShard",
  "schemaVersion": "1.0.0",
  "role": "job",
  "execId": "gh-26185145757-1-a1b2c3d4",
  "planId": "a1b2c3d4",
  "jobUid": "7f6a9c21d4e8b012",
  "jobId": "cloudflare-hyperdrive@stage-preview.validate",
  "status": "failed",
  "source": {
    "type": "github-actions",
    "repository": "sourceplane/multi-tenant-saas",
    "runId": "26185145757",
    "runAttempt": "1",
    "sha": "abc123"
  }
}
```

This makes the bundle portable.

### 5. Add a manifest-only fast path

`orun github runs` should not download full logs.

There should be two levels:

```text
Level 1: artifact names only
  Fast, approximate status.

Level 2: manifest-only download
  Exact status, no logs.

Level 3: full shard download
  Hydrates plan, state, logs.
```

Commands:

```bash
orun github runs
orun github runs --details
orun github pull --latest-failed
```

### 6. Make partial hydration a first-class state

Missing shards are normal in cancelled CI runs.

Hydration should support:

```json
{
  "status": "partial",
  "reason": "missing_job_shards",
  "expectedJobs": 18,
  "foundJobShards": 14
}
```

Then:

```bash
orun status
```

Can show:

```text
EXECUTION gh-26185145757-1-a1b2c3d4  ◐ partial  14/18 shards
```

This is better than failing hydration entirely.

---

# Better Implementation Plan

## Phase 0 — Design spike and compatibility proof

Before coding the full feature, prove these three things:

1. Orun can upload one artifact from inside GitHub Actions without using an `actions/upload-artifact` workflow step.
2. The uploaded artifact is visible through the GitHub artifact REST API before the workflow finishes.
3. Local Orun can list and download that artifact using `GITHUB_TOKEN`, `gh auth token`, or a PAT.

Deliverables:

```text
spikes/github-artifact-upload-helper/
  upload.mjs
  README.md
```

Decision:

```text
v1 uploader = embedded @actions/artifact helper
native Go uploader = future/experimental
```

Reason: `@actions/artifact` is the supported programmatic library, while the REST API is for list/get/download/delete, not upload. ([GitHub][2]) ([GitHub Docs][1])

---

## Phase 1 — Define Orun Run Bundle schema

Create:

```text
internal/runbundle/
  schema.go
  naming.go
  checksums.go
```

Types:

```go
type ShardRole string

const (
    ShardRolePlan ShardRole = "plan"
    ShardRoleJob  ShardRole = "job"
)

type RunBundleShardManifest struct {
    APIVersion    string            `json:"apiVersion"`
    Kind          string            `json:"kind"`
    SchemaVersion string            `json:"schemaVersion"`
    Role          ShardRole         `json:"role"`
    ExecID        string            `json:"execId"`
    PlanID        string            `json:"planId"`
    JobUID        string            `json:"jobUid,omitempty"`
    JobID         string            `json:"jobId,omitempty"`
    Component     string            `json:"component,omitempty"`
    Environment   string            `json:"environment,omitempty"`
    Composition   string            `json:"composition,omitempty"`
    Profile       string            `json:"profile,omitempty"`
    Status        string            `json:"status,omitempty"`
    StartedAt     string            `json:"startedAt,omitempty"`
    FinishedAt    string            `json:"finishedAt,omitempty"`
    Source        ShardSource       `json:"source,omitempty"`
    Files         map[string]string `json:"files"`
}

type ShardSource struct {
    Type       string `json:"type"` // github-actions, local, r2, s3
    Repository string `json:"repository,omitempty"`
    RunID      string `json:"runId,omitempty"`
    RunAttempt string `json:"runAttempt,omitempty"`
    Workflow   string `json:"workflow,omitempty"`
    SHA        string `json:"sha,omitempty"`
    Ref        string `json:"ref,omitempty"`
    EventName  string `json:"eventName,omitempty"`
}
```

Artifact name:

```text
orun.v1.<exec-id>.<role>.<suffix>.<status>
```

Examples:

```text
orun.v1.gh-26185145757-1-a1b2c3d4.plan.a1b2c3d4.created
orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.failed
```

Important: keep `exec-id` constrained to safe characters:

```text
[a-zA-Z0-9_-]+
```

---

## Phase 2 — Build shard writer/reader

Create:

```text
internal/runbundle/
  writer.go
  reader.go
  validate.go
```

Plan shard:

```text
manifest.json
plan.json
matrix.json
trigger.json
git.json
checksums.json
```

Job shard:

```text
manifest.json
job.json
state.json
steps.jsonl
events.jsonl
summary.md
logs/
  <step-id>.log
```

Writer API:

```go
func WritePlanShard(ctx context.Context, opts WritePlanShardOptions) (*Shard, error)

func WriteJobShard(ctx context.Context, opts WriteJobShardOptions) (*Shard, error)
```

Reader API:

```go
func ReadShardManifest(dir string) (*RunBundleShardManifest, error)

func ReadPlanShard(dir string) (*PlanShard, error)

func ReadJobShard(dir string) (*JobShard, error)
```

Validation:

```text
- schemaVersion supported
- manifest files exist
- checksums match
- job shard planId matches plan shard
- jobId exists in plan
- no path traversal in extracted files
```

---

## Phase 3 — Define generic artifact store interface

Create:

```text
internal/artifactstore/
  store.go
  memory.go
  local.go
```

Interface:

```go
type Store interface {
    Upload(ctx context.Context, shard *runbundle.Shard) (*UploadResult, error)
    List(ctx context.Context, opts ListOptions) ([]RemoteShard, error)
    Download(ctx context.Context, shard RemoteShard, destDir string) (*DownloadedShard, error)
}

type RemoteShard struct {
    Name      string
    ID        string
    SizeBytes int64
    CreatedAt time.Time
    ExpiresAt time.Time
    Parsed    *runbundle.ParsedShardName
    Source    map[string]string
}
```

This lets GitHub be one implementation, not the architecture.

---

## Phase 4 — GitHub artifact store: list/download first

Create:

```text
internal/artifactstore/github/
  client.go
  list.go
  download.go
  resolve.go
```

Implement:

```go
func ListWorkflowRuns(ctx context.Context, opts ListRunOptions) ([]WorkflowRun, error)

func ListRunArtifacts(ctx context.Context, runID int64) ([]artifactstore.RemoteShard, error)

func DownloadArtifact(ctx context.Context, artifactID int64, destDir string) (*DownloadedShard, error)
```

Token resolution:

```text
1. GITHUB_TOKEN
2. GH_TOKEN
3. gh auth token
4. explicit --token
```

Permissions:

```text
Private repo local pull needs Actions: read.
Public repo can list public resources without auth.
```

GitHub’s REST docs say fine-grained tokens need repository “Actions” read permission for artifact listing. ([GitHub Docs][1])

---

## Phase 5 — GitHub artifact upload via embedded helper

Create:

```text
internal/artifactstore/github/
  upload.go
  helper/
    upload.mjs
    package.json
```

Upload implementation:

```go
func Upload(ctx context.Context, shard *runbundle.Shard) (*artifactstore.UploadResult, error)
```

Behavior:

```text
- only works inside GitHub Actions
- requires GITHUB_ACTIONS=true
- invokes embedded Node helper
- helper imports @actions/artifact
- uploads staged shard directory as one artifact
- returns artifact id, size, digest if available
```

Runtime options:

```bash
ORUN_ARTIFACT_BACKEND=github
ORUN_ARTIFACT_UPLOAD=true
ORUN_ARTIFACT_RETENTION_DAYS=14
ORUN_ARTIFACT_UPLOAD_MODE=js       # default
ORUN_ARTIFACT_UPLOAD_MODE=native   # future experimental
```

Guardrails:

```text
- one artifact per Orun invocation
- never upload to the same artifact name twice
- fail gracefully if job already uploaded artifact
- warn if GHES unsupported for v4 artifact APIs
```

The per-job limit matters: `@actions/artifact` v2 / upload-artifact v4 documents a limit of 10 artifacts for an individual job, so Orun should produce at most one job shard per matrix job. ([GitHub][2])

---

## Phase 6 — Integrate with `orun plan`

Add:

```bash
orun plan --artifact github
```

Behavior in GitHub Actions:

```text
1. Compile plan.
2. Derive exec ID:
   gh-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>-<plan-short-sha>
3. Write plan shard.
4. Upload plan shard.
5. Print outputs in GitHub-friendly form.
```

Add a cleaner output command to avoid fragile `jq` usage:

```bash
orun plan --from-ci github --artifact github --github-output
```

This writes directly to `$GITHUB_OUTPUT`:

```text
matrix=<json>
plan_id=<short>
exec_id=<exec-id>
```

So the workflow becomes simpler.

---

## Phase 7 — Integrate with `orun run`

Add:

```bash
orun run --artifact github
```

Behavior:

```text
1. Resolve or compile plan.
2. Run selected job.
3. Always write job shard in a post-run hook.
4. Always attempt upload, even on failure.
5. Return original job exit code after upload attempt.
```

Critical rule:

```text
Job shard upload must run under defer/finally semantics.
```

Pseudo-flow:

```go
exitCode := 1
jobState := runbundle.JobState{}

defer func() {
    shard := WriteJobShard(jobState)
    uploadErr := artifactStore.Upload(ctx, shard)

    if uploadErr != nil {
        ui.Warn("failed to upload Orun job artifact: %v", uploadErr)
    }

    os.Exit(exitCode)
}()

exitCode = runner.RunJob(...)
```

Also fix the fresh-runner plan issue:

```bash
orun run \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH" \
  --exec-id "$ORUN_EXEC_ID" \
  --job "$ORUN_JOB_ID" \
  --artifact github \
  --gha
```

This recompiles the same plan in the matrix job instead of expecting `.orun/plans/latest.json` to exist.

---

## Phase 8 — Local fan-in and hydration

Create:

```text
internal/runbundle/
  synthesize.go
  hydrate.go
```

Synthesize:

```go
func Synthesize(plan *PlanShard, jobs []*JobShard) (*SynthesizedExecution, error)
```

Hydrate:

```go
func Hydrate(ctx context.Context, opts HydrateOptions) (*HydrateResult, error)
```

Hydrated layout:

```text
.orun/
  plans/
    <plan-id>.json
  executions/
    latest -> <exec-id>
    <exec-id>/
      metadata.json
      github.json
      plan.json
      state.json
      shards.json
      logs/
        <job-id>/
          <step-id>.log
```

Important compatibility task:

```text
Use existing internal/state structs wherever possible.
Do not invent a parallel state.json shape if existing orun status/log readers expect another structure.
```

---

## Phase 9 — CLI: `orun github`

Create:

```text
cmd/orun/github/
  root.go
  runs.go
  pull.go
  status.go
  logs.go
```

Commands:

```bash
orun github runs
orun github runs --failed
orun github runs --details

orun github pull --latest
orun github pull --latest-failed
orun github pull --run-id 26185145757
orun github pull --exec-id gh-26185145757-1-a1b2c3d4

orun github status --latest-failed
orun github logs --latest-failed --failed
```

`orun github runs` should be fast:

```text
- list workflow runs
- list artifacts per run
- parse artifact names
- do not download logs
```

`orun github pull` should hydrate:

```text
- resolve run
- list Orun shards
- download selected execId shards
- synthesize execution state
- hydrate .orun/
- print normal Orun status summary
```

---

## Phase 10 — Minimal workflow template

New workflow:

```yaml
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

No upload step. No download step. No collector job.

---

## Phase 11 — Tests

Test matrix:

| Area            | Test                                                      |
| --------------- | --------------------------------------------------------- |
| Naming          | Parse/build artifact names, reject unsafe names           |
| Writer          | Plan/job shards produce exact layout                      |
| Reader          | Reject missing files, bad checksums, bad schema           |
| Synthesis       | Complete, failed, cancelled, partial runs                 |
| Hydration       | Hydrated `.orun/` works with existing `status` and `logs` |
| GitHub list     | Mock REST pagination and filters                          |
| GitHub download | Mock ZIP download and path traversal defense              |
| Upload helper   | Mock helper invocation and result parsing                 |
| CI E2E          | Real GitHub Actions workflow uploads plan + job shards    |
| Local E2E       | `orun github pull --latest-failed && orun logs --failed`  |

---

## Final recommended implementation order

```text
1. Prove upload helper inside GitHub Actions.
2. Define portable RunBundleShard schema.
3. Implement writer/reader/checksum.
4. Implement synthesis/hydration against existing state layout.
5. Implement GitHub list/download.
6. Add `orun github pull`.
7. Add `orun plan --artifact github`.
8. Add `orun run --artifact github`.
9. Add `orun github runs/status/logs`.
10. Only then consider native Go artifact upload.
```

## Final design statement

The best model is:

```text
Orun owns the evidence format.
GitHub Artifacts is only the first evidence store.
Each Orun invocation uploads one immutable shard.
Local Orun performs lazy fan-in and hydrates normal .orun state.
No collector job.
No workflow-level upload/download glue.
No native GitHub internal protocol dependency in v1.
```

That gives you a CNCF-grade design: portable state, minimal CI YAML, deterministic execution evidence, and a clean path from GitHub artifacts to future R2/S3/Orun Cloud storage.

[1]: https://docs.github.com/v3/actions/artifacts "REST API endpoints for GitHub Actions artifacts - GitHub Docs"
[2]: https://github.com/actions/toolkit/blob/main/packages/artifact/README.md "toolkit/packages/artifact/README.md at main · actions/toolkit · GitHub"
