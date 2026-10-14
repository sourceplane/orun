# orun-runner-efficiency — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **RE0** | ✅ #741 | The analysis, the fleet model (`model.py`) and the expected effect; the scheduler probes committed as regression tests with RE1 |
| **RE1** | 🟡 in review | A whole plan at concurrency > 1 runs on the lane scheduler (`runScheduled`, one scheduler for `--lane` and the plan): each job starts when its upstreams finish; `--component-concurrency` defaults to 0 and is a gate in front of the slot gate when set; `--job` with incomplete upstreams is refused at any concurrency; regression tests in `internal/runner/schedule_test.go` |
| **RE2** | 🟡 in review | `plan.lanes[]` carries `level` (depth in the lane graph, not the jobs' lowest level: what a levelled workflow needs), `upstreamLanes` and `upstreamJobs`; `lane-matrix` entries carry `level` and `upstreamLanes` (a lane-less plan: the job's depth and dependencies); `--github-output` adds `lane-matrix-<k>` per level, `lane-matrix-levels` and `lane-level-count`; `--view lanes` shows level and upstreams; the level-by-level recipe in `concepts/lanes.md`. `estimatedSeconds` and `vcpu` wait for their sources (RE5, RE3) |
| RE3 | ⏳ | `concurrency: auto` from `NumCPU`; `--max-lanes` / `ORUN_MAX_LANES` |
| RE4 | ⏳ | `.git` never staged; `isolation: component`; lane-scoped setup steps; lane-level caches |
| RE5 | ⏳ | Declared `resources`; weighted, critical-path packing; `--weights` |
| RE6 | ⏳ | Signal drain, resume at step, the server's lease TTL in the client |
| RE7 | ⏳ | Event-driven waits; default job and step timeouts |
