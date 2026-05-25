# ChatGPT Conversation: GitHub Artifacts for Orun

**Source:** https://chatgpt.com/share/6a11610c-8b6c-83e8-9998-7dabb6fda759

---

## User

What is the best strategy to push the run details to github artifacts in a stuctured way so that we can easily retrieve the logs locally using orun to check the status. Expand on this idea. Think like a advaced cncf echosystem tool developer

## Assistant

I'll ground this in Orun's current component/plan model first, then propose an artifact layout and local retrieval UX that can scale from plain GitHub Actions to a future remote backend.

The key design point: GitHub artifacts should be treated as immutable snapshots, not as a mutable remote state database. That pushes the design toward "many small job artifacts + one final index artifact," rather than one shared artifact every matrix worker edits.

**Recommendation**

Use **GitHub Actions artifacts as an immutable execution evidence store**, not as the live Orun state backend.

The best model is:

1. **Plan artifact**: one immutable plan bundle produced by the planner job.
2. **Job result artifacts**: one artifact per Orun job/matrix cell.
3. **Final run index artifact**: one fan-in artifact that references the plan, all job results, status summary, log locations, digests, and GitHub metadata.
4. **Local hydrate command**: `orun github pull` downloads those artifacts and reconstructs the normal local `.orun/executions/{exec-id}/` layout so existing `orun status`, `orun logs`, `orun describe`, and `orun get runs` work unchanged.

This matches Orun's current model well because the plan is already the immutable planning boundary, and execution records are already structured as `metadata.json`, `state.json`, and per-step logs under `.orun/executions/{exec-id}/`. GitHub artifacts are also designed for files produced by workflow runs, including logs and build/test outputs, and are intended for post-run inspection rather than dependency caching.

**Core design principle**

Do **not** try to maintain one mutable artifact called `orun-run-state`.

That will fight GitHub's artifact model. GitHub's own docs show that artifacts are considered immutable in v4, so when a later job changes data, it should upload a differently named artifact instead of mutating the old one.

So the Orun model should be append-only:

```
planner job
  uploads:
    orun-plan-<exec-id>

matrix job A
  uploads:
    orun-job-<exec-id>-<job-uid-a>

matrix job B
  uploads:
    orun-job-<exec-id>-<job-uid-b>

collector job
  downloads all job artifacts
  builds final index
  uploads:
    orun-run-<exec-id>-index
```

This gives you deterministic, race-free, parallel-safe CI evidence.

**Artifact naming**

Use names that are unique per GitHub run attempt and stable for lookup:

```
orun-plan-gh-<github_run_id>-<attempt>-<plan_short_sha>
orun-job-gh-<github_run_id>-<attempt>-<plan_short_sha>-<job_uid>
orun-run-gh-<github_run_id>-<attempt>-<plan_short_sha>-index
```

Where:
```
exec_id = gh-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-${plan_short_sha}
job_uid = sha256(job_id)[0:16]
```

Do not put raw job IDs directly in artifact names. Orun job IDs may contain `/`, `@`, `.`, `:`, environment names, or long component names. Use a short stable hash and keep the human-readable mapping inside JSON.

**Recommended artifact layout**

Each artifact should unpack into an Orun-native layout, not a random CI-specific layout.

**Plan artifact**
```
orun-plan-<exec-id>/
  manifest.json
  plan.json
  jobs.json
  matrix.json
  trigger.json
  git.json
```

Example `manifest.json`:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "RunArtifactManifest",
  "schemaVersion": "1.0.0",
  "execId": "gh-26185145757-1-a1b2c3d4",
  "planId": "a1b2c3d4",
  "artifactRole": "plan",
  "repo": "sourceplane/multi-tenant-saas",
  "workflow": "orun",
  "github": {
    "runId": "26185145757",
    "runAttempt": "1",
    "sha": "abc123",
    "ref": "refs/pull/27/merge",
    "eventName": "pull_request"
  },
  "files": {
    "plan": "plan.json",
    "jobs": "jobs.json",
    "matrix": "matrix.json",
    "trigger": "trigger.json",
    "git": "git.json"
  }
}
```

**Per-job artifact**
```
orun-job-<exec-id>-<job_uid>/
  job.json
  state.json
  steps.jsonl
  events.jsonl
  summary.md
  logs/
    <step-id>.log
```

Example `job.json`:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "JobRunResult",
  "execId": "gh-26185145757-1-a1b2c3d4",
  "planId": "a1b2c3d4",
  "jobUid": "7f6a9c21d4e8b012",
  "jobId": "cloudflare-hyperdrive@stage-preview.plan",
  "component": "cloudflare-hyperdrive",
  "environment": "stage-preview",
  "composition": "terraform",
  "profile": "plan-only",
  "github": {
    "runId": "26185145757",
    "runAttempt": "1",
    "jobName": "cloudflare-hyperdrive / stage-preview / plan",
    "matrixIndex": 4
  },
  "status": "failed",
  "conclusion": "failure",
  "startedAt": "2026-05-23T05:40:00Z",
  "finishedAt": "2026-05-23T05:42:31Z",
  "durationMs": 151000
}
```

Example `state.json`:

```json
{
  "jobId": "cloudflare-hyperdrive@stage-preview.plan",
  "status": "failed",
  "steps": [
    {
      "id": "terraform-init",
      "name": "Terraform Init",
      "status": "failed",
      "exitCode": 1,
      "startedAt": "2026-05-23T05:41:10Z",
      "finishedAt": "2026-05-23T05:41:18Z",
      "log": "logs/terraform-init.log"
    }
  ],
  "error": {
    "step": "terraform-init",
    "message": "command exited with code 1"
  }
}
```

**Final index artifact**
```
orun-run-<exec-id>-index/
  index.json
  state.json
  failures.json
  components.json
  jobs.json
  artifacts.json
  summary.md
```

Example `index.json`:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "RunIndex",
  "schemaVersion": "1.0.0",
  "execId": "gh-26185145757-1-a1b2c3d4",
  "planId": "a1b2c3d4",
  "repo": "sourceplane/multi-tenant-saas",
  "status": "failed",
  "counts": {
    "total": 18,
    "completed": 14,
    "failed": 1,
    "cancelled": 0,
    "skipped": 3,
    "pending": 0
  },
  "artifacts": {
    "plan": {
      "name": "orun-plan-gh-26185145757-1-a1b2c3d4",
      "digest": "sha256:..."
    },
    "jobs": [
      {
        "jobUid": "7f6a9c21d4e8b012",
        "jobId": "cloudflare-hyperdrive@stage-preview.plan",
        "name": "orun-job-gh-26185145757-1-a1b2c3d4-7f6a9c21d4e8b012",
        "status": "failed",
        "digest": "sha256:..."
      }
    ]
  },
  "createdAt": "2026-05-23T05:44:00Z"
}
```

GitHub's upload/download artifact flow provides a digest output and validates the downloaded digest, so Orun should capture those digests in `artifacts.json` or `index.json` for integrity checking.

**Local retrieval UX**

Add a GitHub artifact source to Orun:

```bash
orun github runs
orun github pull --run-id 26185145757
orun github pull --exec-id gh-26185145757-1-a1b2c3d4
orun status --exec-id gh-26185145757-1-a1b2c3d4
orun logs --exec-id gh-26185145757-1-a1b2c3d4 --job cloudflare-hyperdrive@stage-preview.plan
```

The important part: `orun github pull` should hydrate artifacts into the same local execution record format:

```
.orun/
  plans/
    a1b2c3d4.json
  executions/
    latest -> gh-26185145757-1-a1b2c3d4
    gh-26185145757-1-a1b2c3d4/
      metadata.json
      state.json
      index.json
      github.json
      plan.json
      logs/
        cloudflare-hyperdrive@stage-preview.plan/
          terraform-init.log
```

Then all existing commands continue to work:

```bash
orun status
orun status --detailed
orun logs --failed
orun describe run latest
orun describe job cloudflare-hyperdrive@stage-preview.plan
```

That is the CNCF-grade move: make GitHub artifacts just another state source adapter, not a separate CLI experience.

**Retrieval algorithm**

`orun github pull` should do this:

1. Resolve repo from git remote.
2. Resolve target workflow run:
   - explicit `--run-id`
   - explicit `--exec-id`
   - latest run for current branch/SHA
   - latest failed run for current branch/SHA
3. List artifacts for that workflow run using GitHub's Actions artifacts API. GitHub exposes REST endpoints to list, retrieve, download, and delete workflow artifacts.
4. Prefer the final `orun-run-<exec-id>-index` artifact.
5. If the final index is missing, reconstruct from:
   - `orun-plan-<exec-id>`
   - all `orun-job-<exec-id>-*` artifacts
6. Validate:
   - plan checksum
   - schema version
   - artifact digests when available
   - job IDs exist in plan
7. Hydrate local `.orun/executions/{exec-id}/`.
8. Print a compact status.

Example:

```
✓ found run gh-26185145757-1-a1b2c3d4
✓ downloaded index artifact
✓ downloaded 18 job artifacts
✓ hydrated .orun/executions/gh-26185145757-1-a1b2c3d4

EXECUTION gh-26185145757-1-a1b2c3d4  ✗ failed  14/18 jobs  4m31s
Plan: a1b2c3d4

  ✗ cloudflare-hyperdrive@stage-preview.plan     terraform-init failed
  ✓ supabase@stage-preview.plan                  completed
  ○ cloudflare-worker@prod.apply                 skipped
```

For private repos, local access should rely on `gh auth token` or a fine-grained token with Actions: read. GitHub's artifact API allows public read access for public resources, but private repositories require authenticated access, and fine-grained tokens need Actions read permission.

**GitHub Actions shape**

Use a three-job pattern.

```yaml
jobs:
  plan:
    runs-on: ubuntu-latest
    outputs:
      exec_id: ${{ steps.plan.outputs.exec_id }}
      plan_id: ${{ steps.plan.outputs.plan_id }}
      matrix: ${{ steps.plan.outputs.matrix }}
    steps:
      - uses: actions/checkout@v4

      - name: Compile Orun plan
        id: plan
        run: |
          orun plan --from-ci github --output .orun/artifacts/plan/plan.json

          plan_id="$(jq -r '.metadata.checksum' .orun/artifacts/plan/plan.json | cut -c1-12)"
          exec_id="gh-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-${plan_id}"

          orun artifact prepare-plan \
            --exec-id "$exec_id" \
            --plan .orun/artifacts/plan/plan.json \
            --out .orun/artifacts/plan

          echo "exec_id=$exec_id" >> "$GITHUB_OUTPUT"
          echo "plan_id=$plan_id" >> "$GITHUB_OUTPUT"
          echo "matrix=$(cat .orun/artifacts/plan/matrix.json)" >> "$GITHUB_OUTPUT"

      - name: Upload Orun plan artifact
        uses: actions/upload-artifact@v4
        with:
          name: orun-plan-${{ steps.plan.outputs.exec_id }}
          path: .orun/artifacts/plan/
          retention-days: 14

  run:
    needs: plan
    strategy:
      fail-fast: false
      matrix:
        job: ${{ fromJson(needs.plan.outputs.matrix) }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run Orun job
        id: orun
        continue-on-error: true
        env:
          ORUN_EXEC_ID: ${{ needs.plan.outputs.exec_id }}
        run: |
          orun run "${{ needs.plan.outputs.plan_id }}" \
            --job "${{ matrix.job.id }}" \
            --gha \
            --json \
            --artifact-dir ".orun/artifacts/job/${{ matrix.job.uid }}"

      - name: Finalize Orun job artifact
        if: always()
        run: |
          orun artifact finalize-job \
            --exec-id "${{ needs.plan.outputs.exec_id }}" \
            --job "${{ matrix.job.id }}" \
            --out ".orun/artifacts/job/${{ matrix.job.uid }}"

      - name: Upload Orun job artifact
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: orun-job-${{ needs.plan.outputs.exec_id }}-${{ matrix.job.uid }}
          path: .orun/artifacts/job/${{ matrix.job.uid }}/
          retention-days: 14

      - name: Fail if Orun job failed
        if: always()
        run: |
          orun artifact assert-job \
            --dir ".orun/artifacts/job/${{ matrix.job.uid }}"

  index:
    needs: [plan, run]
    if: always()
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Download Orun job artifacts
        uses: actions/download-artifact@v5
        with:
          pattern: orun-job-${{ needs.plan.outputs.exec_id }}-*
          path: .orun/collect/jobs
          merge-multiple: false

      - name: Download Orun plan artifact
        uses: actions/download-artifact@v5
        with:
          name: orun-plan-${{ needs.plan.outputs.exec_id }}
          path: .orun/collect/plan

      - name: Build Orun run index
        run: |
          orun artifact index \
            --exec-id "${{ needs.plan.outputs.exec_id }}" \
            --plan .orun/collect/plan/plan.json \
            --jobs .orun/collect/jobs \
            --out .orun/artifacts/index

      - name: Upload Orun run index
        uses: actions/upload-artifact@v4
        with:
          name: orun-run-${{ needs.plan.outputs.exec_id }}-index
          path: .orun/artifacts/index/
          retention-days: 14
```

GitHub supports uploading named artifacts and downloading specific or all artifacts in a workflow run; when downloading all artifacts, each artifact is placed in its own directory by artifact name.

**What Orun should implement**

I would add these internals:

```
internal/artifacts/
  schema.go
  writer.go
  reader.go
  indexer.go
  hydrate.go

internal/githubartifacts/
  client.go
  list.go
  download.go
  resolve.go

cmd/orun/artifact/
  prepare-plan
  finalize-job
  index
  assert-job

cmd/orun/github/
  runs
  pull
  status
  logs
```

User-facing commands:

```bash
# CI-side helpers
orun artifact prepare-plan
orun artifact finalize-job
orun artifact index

# Local developer UX
orun github runs
orun github pull --latest
orun github pull --run-id 26185145757
orun github pull --sha HEAD
orun github pull --failed

# Existing commands work after hydration
orun status
orun logs --failed
orun describe run latest
```

Later, the same interface can support:

```bash
orun pull --from github-artifacts
orun pull --from orun-backend
orun pull --from s3
orun pull --from r2
```

That makes GitHub artifacts a backend adapter, not a one-off feature.

**Important limitations**

GitHub artifacts are good for post-run retrieval, not strong live orchestration.

They are not ideal for:
- DAG locking
- live heartbeats
- dependency waits
- streaming logs while a job is still running
- frequent state mutation
- long-term history

For that, keep `orun-backend` as the real distributed execution backend. Orun's current remote-state model already delegates DAG coordination to the backend, with each GitHub matrix job running one job and the backend enforcing dependency ordering.

So the clean product model is:

| Mode | Backend | Best for |
|------|---------|----------|
| local | `.orun/` directory | local runs |
| github-artifacts | GitHub artifact snapshots | CI evidence, post-run local debugging |
| remote-state | Orun backend / Cloudflare | live DAG coordination, streaming logs, dashboards |

**Security and retention**

Keep artifact retention short by default, for example 7–14 days. GitHub's default retention for artifacts and logs is 90 days; public repos can set 1–90 days, and private repos can set 1–400 days depending on org/enterprise limits.

Also:
- Never store full resolved env values in artifacts.
- Store env key names, source level, and maybe redacted/hash metadata only.
- Redact known secret patterns before writing logs.
- Store raw logs and redacted logs separately only if there is a strong need.
- Default local `orun github pull` to redacted logs.
- Add `--include-raw` only for trusted maintainers.
- Include `github.run_id`, `run_attempt`, `sha`, `ref`, `actor`, and `event_name` in `github.json`.
- Include artifact digests in `index.json`.

**Final model**

The best strategy is to make a **GitHub Artifact Run Bundle**:

- Plan = immutable desired execution
- Job artifacts = immutable per-job evidence
- Index artifact = queryable run summary
- Local hydrate = convert remote evidence back into `.orun/executions`

This gives Orun a very strong developer experience:

```bash
orun github pull --latest-failed
orun status
orun logs --failed
orun describe job cloudflare-hyperdrive@stage-preview.plan
```

And it keeps the architecture clean: GitHub artifacts are an evidence store, while Orun's local state and future remote backend remain the canonical execution-state models.

---

## User

For the artifact upload and consolidation there shouldn't be any separate jobs orun should upload to github artifact by himself and no consolidation. When locally orun github runs or pulls it should do the consolidation on the go.

## Assistant

I agree with that constraint. The cleaner model is: each `orun run --job ...` uploads its own immutable shard, and the local CLI performs lazy fan-in when you inspect or pull the run.

Yes — that is the better Orun-native model.

The revised strategy should be:

```
GitHub Actions job runs Orun
        ↓
Orun writes local .orun/executions/<exec-id>/...
        ↓
Orun uploads its own run shard to GitHub Artifacts
        ↓
No collector job
        ↓
Local `orun github pull` downloads shards and consolidates locally
```

That fits Orun's separation model better: the runner consumes the compiled plan, records execution state, and exposes evidence; it should not require extra CI glue to assemble Orun's own runtime state. Orun already treats the plan as compiled truth and execution records as runtime evidence, so GitHub artifacts should simply become a remote storage adapter for those records.

**Revised architecture**

1. **Orun uploads artifacts directly**

Each CI invocation of Orun should be responsible for its own upload.

```bash
orun plan --from-ci github --artifact github
orun run --plan latest --job "$ORUN_JOB_ID" --gha --artifact github
```

Or via env:

```bash
ORUN_ARTIFACT_BACKEND=github
ORUN_ARTIFACT_UPLOAD=true
orun run --job "$ORUN_JOB_ID" --gha
```

No `actions/upload-artifact` step should be required in the workflow.

One nuance: GitHub's public REST API supports listing, retrieving, downloading, and deleting workflow artifacts, but not a normal public "upload artifact" REST endpoint. The official `upload-artifact` action is powered by the `@actions/artifact` package, which is the programmatic artifact client used inside Actions. So Orun has two implementation paths: implement the GitHub Actions artifact protocol directly in Go, or ship a tiny internal helper that calls `@actions/artifact` when running inside GitHub Actions. The second path is probably safer initially.

2. **Artifact model: shards, not index**

Do not create a consolidated artifact in CI.

Instead, upload immutable shards:

```
orun.v1.<exec-id>.plan.<plan-id>
orun.v1.<exec-id>.job.<job-uid>.completed
orun.v1.<exec-id>.job.<job-uid>.failed
orun.v1.<exec-id>.job.<job-uid>.skipped
```

Example:

```
orun.v1.gh-26185145757-1-a1b2c3d4.plan.a1b2c3d4
orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.failed
orun.v1.gh-26185145757-1-a1b2c3d4.job.a91bcb2391d45a12.completed
```

This works well because GitHub artifact v4 behavior is immutable, and separate artifacts per producer avoids parallel upload conflicts. GitHub's docs also expose artifact APIs for later listing and downloading, which is exactly what local Orun needs.

**Shard contents**

Plan artifact:

```
manifest.json
plan.json
matrix.json
trigger.json
git.json
checksums.json
```

`manifest.json`:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "GitHubArtifactShard",
  "role": "plan",
  "execId": "gh-26185145757-1-a1b2c3d4",
  "planId": "a1b2c3d4",
  "github": {
    "repository": "sourceplane/multi-tenant-saas",
    "runId": "26185145757",
    "runAttempt": "1",
    "workflow": "orun",
    "sha": "abc123",
    "ref": "refs/pull/27/merge",
    "eventName": "pull_request"
  },
  "files": {
    "plan": "plan.json",
    "matrix": "matrix.json",
    "trigger": "trigger.json",
    "git": "git.json"
  }
}
```

Job artifact:

```
manifest.json
job.json
state.json
steps.jsonl
events.jsonl
summary.md
logs/
  terraform-init.log
  terraform-plan.log
```

`manifest.json`:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "GitHubArtifactShard",
  "role": "job",
  "execId": "gh-26185145757-1-a1b2c3d4",
  "planId": "a1b2c3d4",
  "jobUid": "7f6a9c21d4e8b012",
  "jobId": "cloudflare-hyperdrive@stage-preview.validate",
  "component": "cloudflare-hyperdrive",
  "environment": "stage-preview",
  "composition": "terraform",
  "profile": "plan-only",
  "status": "failed",
  "startedAt": "2026-05-23T05:41:00Z",
  "finishedAt": "2026-05-23T05:42:31Z",
  "files": {
    "job": "job.json",
    "state": "state.json",
    "steps": "steps.jsonl",
    "events": "events.jsonl",
    "summary": "summary.md",
    "logs": "logs/"
  }
}
```

The job artifact must be self-contained. Local consolidation should not need a final index from CI.

**Local consolidation happens on demand**

`orun github pull` becomes the fan-in engine.

```bash
orun github runs
orun github pull --latest
orun github pull --run-id 26185145757
orun github pull --exec-id gh-26185145757-1-a1b2c3d4
orun status
orun logs --failed
```

The algorithm:

1. Resolve repo from git remote.
2. Resolve workflow run from `--run-id`, `--exec-id`, `--sha`, `--branch`, or latest.
3. List artifacts for that GitHub workflow run.
4. Filter names starting with `orun.v1.`
5. Group by `execId`.
6. Download the plan shard and job shards.
7. Validate checksums and schema versions.
8. Reconstruct local `.orun/executions/<exec-id>/`.
9. Run normal local status/log commands against hydrated state.

Hydrated local layout:

```
.orun/
  plans/
    a1b2c3d4.json
  executions/
    latest -> gh-26185145757-1-a1b2c3d4
    gh-26185145757-1-a1b2c3d4/
      metadata.json
      github.json
      plan.json
      state.json
      shards.json
      logs/
        cloudflare-hyperdrive@stage-preview.validate/
          terraform-init.log
          terraform-plan.log
```

`state.json` is locally synthesized from all downloaded job shards.

That means `orun status` and `orun logs` do not need to know GitHub once the pull is done.

**`orun github runs` without full download**

For fast listing, artifact names should carry enough information to group runs without downloading every ZIP.

Good artifact name format:

```
orun.v1.<exec-id>.<role>.<id>.<status>
```

Example:

```
orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.failed
```

Then `orun github runs` can show an approximate summary by listing GitHub workflow runs plus artifact metadata:

```
EXECUTION                         STATUS    JOBS       SHA       AGE
gh-26185145757-1-a1b2c3d4         failed    17✓ 1✗     abc123    12m
gh-26184210001-1-d9e8f7a6         passed    18✓        def456    1h
```

For exact details, it downloads only manifests or the selected run:

```bash
orun github runs --details
orun github pull --latest-failed
```

**Workflow becomes very small**

No upload step. No consolidation job.

```yaml
jobs:
  plan:
    runs-on: ubuntu-latest
    outputs:
      matrix: ${{ steps.plan.outputs.matrix }}
      plan_id: ${{ steps.plan.outputs.plan_id }}
      exec_id: ${{ steps.plan.outputs.exec_id }}
    steps:
      - uses: actions/checkout@v4

      - name: Plan
        id: plan
        run: |
          orun plan \
            --from-ci github \
            --event-file "$GITHUB_EVENT_PATH" \
            --artifact github \
            --output .orun/plans/latest.json

          echo "matrix=$(orun get jobs --plan latest -o json --matrix)" >> "$GITHUB_OUTPUT"
          echo "plan_id=$(orun describe plan latest -o json | jq -r .checksumShort)" >> "$GITHUB_OUTPUT"
          echo "exec_id=$(orun describe plan latest -o json | jq -r .ci.execId)" >> "$GITHUB_OUTPUT"

  run:
    needs: plan
    strategy:
      fail-fast: false
      matrix:
        job: ${{ fromJson(needs.plan.outputs.matrix) }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run Orun job
        env:
          ORUN_EXEC_ID: ${{ needs.plan.outputs.exec_id }}
          ORUN_ARTIFACT_BACKEND: github
          ORUN_ARTIFACT_UPLOAD: "true"
        run: |
          orun run latest \
            --job "${{ matrix.job.id }}" \
            --gha
```

Orun handles:
- plan artifact upload
- job artifact upload
- log capture
- state capture
- redaction
- artifact naming
- artifact manifest writing

**Best CLI contract**

I would make this first-class:

```bash
# CI
orun plan --artifact github
orun run --artifact github

# Local
orun github runs
orun github pull --latest
orun github pull --latest-failed
orun github status --run-id 26185145757
orun github logs --run-id 26185145757 --failed
```

But internally:

- `orun github status` = list/download enough GitHub artifact shards → synthesize local execution view → render with normal status renderer
- `orun github pull` = full hydrate into `.orun/`

This keeps GitHub as a source adapter, not a special execution model.

**Final recommended model**

The clean design is:

- No collector job.
- No final index artifact.
- No artifact upload steps in workflow YAML.
- Each Orun invocation uploads one immutable shard.
- Local Orun lists/downloads shards and performs consolidation on demand.

That gives the best UX:

```bash
orun github pull --latest-failed
orun status
orun logs --failed
```

And it keeps CI simple: GitHub runs Orun; Orun owns Orun's runtime evidence.