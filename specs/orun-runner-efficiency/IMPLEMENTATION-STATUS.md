# orun-runner-efficiency — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **RE0** | ✅ #741 | The analysis, the fleet model (`model.py`) and the expected effect; the scheduler probes committed as regression tests with RE1 |
| **RE1** | 🟡 in review | A whole plan at concurrency > 1 runs on the lane scheduler (`runScheduled`, one scheduler for `--lane` and the plan): each job starts when its upstreams finish; `--component-concurrency` defaults to 0 and is a gate in front of the slot gate when set; `--job` with incomplete upstreams is refused at any concurrency; regression tests in `internal/runner/schedule_test.go` |
| RE2 | ⏳ | `level`, `upstreamLanes`, `upstreamJobs`, `estimatedSeconds`, `vcpu` on lanes and the lane matrix; `lane-matrix-levels` |
| RE3 | ⏳ | `concurrency: auto` from `NumCPU`; `--max-lanes` / `ORUN_MAX_LANES` |
| RE4 | ⏳ | `.git` never staged; `isolation: component`; lane-scoped setup steps; lane-level caches |
| RE5 | ⏳ | Declared `resources`; weighted, critical-path packing; `--weights` |
| RE6 | ⏳ | Signal drain, resume at step, the server's lease TTL in the client |
| RE7 | ⏳ | Event-driven waits; default job and step timeouts |
