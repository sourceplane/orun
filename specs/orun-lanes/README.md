# Spec: orun-lanes (the unit a runner takes)

**A plan job is the smallest thing orun can run. It is also, today, the only
thing a CI runner can be handed.** Every plan job becomes one matrix entry, one
fresh machine, one checkout, one install, one toolchain download. For a job
that deploys a Cloudflare worker that is ~20 seconds of work inside ~90 seconds
of setup, and a 49-job convergence spends most of its wall-clock waiting for
16 runner slots to come round (measured on a Cirrus bootstrap: 6m59s for
work a single runner finishes in about three).

This epic adds the **lane**: the unit a runner takes. A lane is one or more
plan jobs from **one environment**, run in-process by `orun run --lane <id>`
with the plan's dependency DAG, per-job isolation and `--concurrency` that
already exist. A job is the smallest lane and an environment the largest.
Which jobs share a lane is declared in `intent.yaml` (a mode, named groups),
annotated on a component (`spec.lane`), or left to `auto`, which groups a big
plan into a few lanes and leaves a small one alone.

## What it looks like

```yaml
# intent.yaml
execution:
  lanes:
    mode: auto            # job (default) | component | env | auto
    maxLanes: 16
    concurrency: 6
    groups:
      fleet:   { concurrency: 8 }
    rules:
      - mode: job
        when: { triggerRef: github-pull-request }
```

```yaml
# component.yaml
spec:
  lane: fleet             # or: isolated
```

```yaml
# .github/workflows/ci.yml — changes once, then never for grouping again
strategy:
  matrix: { include: ${{ fromJson(needs.plan.outputs.lane-matrix) }} }
name: ${{ matrix.name }}
steps:
  - run: orun run --plan plan.json --lane "${{ matrix.lane }}" --exec-id "${{ github.run_id }}"
```

## Backward compatibility is the first requirement

- No `execution.lanes` and no `spec.lane` anywhere → the plan carries **no
  `lanes` block** and is byte-identical to today's. Every existing golden
  plan, checksum and revision key is unchanged.
- `mode: job` (the default) gives one lane per job whose id **is** the job id,
  so `--lane` and `--job` name the same thing and the lane matrix has exactly
  today's entries.
- `--job`, `matrix` (`--github-output`), `index` and the managed-runner label
  keep their meaning. `lane-matrix` is a new, additional output.

## Documents

| Document | What it holds |
|---|---|
| [design.md](design.md) | The model, the invariants, auto mode, cycles, execution, the GitHub surface |
| [implementation-plan.md](implementation-plan.md) | Milestones LN0–LN7, each one landable on its own |
| [IMPLEMENTATION-STATUS.md](IMPLEMENTATION-STATUS.md) | What has shipped |

## Out of scope

**Measured weights** (a committed `lanes.lock`, `--weights`, platform-suggested
timings). Auto mode in this epic uses a fixed cost model with no per-repo
tuning. Weights are a follow-up once auto mode has run on real plans.
