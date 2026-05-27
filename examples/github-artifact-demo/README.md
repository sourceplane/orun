# GitHub Artifact Demo

This example demonstrates CI plan generation, artifact upload, and verification for Orun's GitHub Actions integration.

## What This Demonstrates

1. **Plan Generation**: `orun plan --artifact github` creates a plan shard
2. **Job Matrix**: Plan generates a matrix of component jobs with dependencies
3. **Artifact Upload**: Each job uploads its shard to GitHub Artifacts
4. **Metrics**: Execution status tracks completed/failed/pending counts
5. **Pull & Verify**: `orun github pull` retrieves and hydrates artifacts

## Component DAG

```
foundation@default (no deps)
api@default (depends on foundation@default)
```

When run in CI:
- Plan shard is uploaded: `orun.v1.gh-{run_id}-{attempt}-{sha}.plan.{checksum}.created`
- Job shards are uploaded: `orun.v1.gh-{run_id}-{attempt}-{sha}.job.{uid}.{status}`

## CI Workflow

The workflow `.github/workflows/ci-artifact-verify.yaml` runs:

1. **Plan Job**:
   - Builds orun
   - Runs `orun plan --artifact github --github-output`
   - Outputs job matrix and execution ID to downstream jobs
   - Uploads plan shard to GitHub Artifacts

2. **Run Jobs** (matrix):
   - `foundation@default` runs first (no dependencies)
   - `api@default` runs after foundation completes
   - Each job uploads its shard on completion

3. **Verify Job**:
   - Lists artifacts with `orun github runs`
   - Pulls artifacts with `orun github pull --latest`
   - Verifies hydrated execution in `.orun/verification/executions/`
   - Shows execution status

## Local Testing

```bash
cd examples/github-artifact-demo

# Validate the intent
orun validate --intent intent.yaml

# Generate a plan (locally, no upload)
orun plan --name demo-plan --all

# Run locally
orun run demo-plan --job foundation@default
```

## Metrics Output

After execution, the status shows:

```
EXECUTION gh-123456-1-abc123  ● completed  2/2 shards
  foundation@default  completed
  api@default         completed
```

If a job fails:
```
EXECUTION gh-123456-1-abc123  ✗ failed  1/2 shards
  foundation@default  completed
  api@default         failed
```

If some shards are missing (e.g., cancelled run):
```
EXECUTION gh-123456-1-abc123  ◐ partial  1/2 shards
  foundation@default  completed
  api@default         pending
```

## Artifact Inspection Commands

```bash
# List recent workflow runs with artifact counts
orun github runs --limit 5

# Quick status (downloads only manifests)
orun github status

# Full pull and hydrate
orun github pull --latest

# Download logs for a specific run
orun github logs --exec-id gh-123456-1-abc123
```

## Verifying Upload Success

After a CI run, verify from any machine with `GITHUB_TOKEN`:

```bash
export GITHUB_TOKEN=ghp_xxx
orun github runs --branch main --limit 1
orun github pull --exec-id gh-123456-1-abc123
orun status --exec-id gh-123456-1-abc123
```

## File Layout

```
github-artifact-demo/
├── intent.yaml              # Intent with 2 components
├── compositions/
│   ├── stack.yaml
│   ├── terraform/
│   │   └── compositions.yaml  # Foundation steps + artifact verification
│   └── helm/
│       └── compositions.yaml  # API steps + artifact verification
├── .github/
│   └── workflows/
│       ├── ci-artifact-verify.yaml  # Full workflow with verification step
│       └── example-workflow.yaml    # Minimal example workflow
└── README.md
```