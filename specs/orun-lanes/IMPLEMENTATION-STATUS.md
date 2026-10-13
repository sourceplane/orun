# orun-lanes — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **LN0** | ✅ #722 | The design: the lane (one environment, one runner, many jobs), configuration and resolution, the planner's invariants, auto mode without weights, cycle repair by level, `orun run --lane`, the lane matrix and per-job check runs, and the compatibility table |
| **LN1** | ✅ #723 | `execution.lanes` (mode, maxLanes, concurrency, onCycle, groups, rules) and `spec.lane` on the intent and component models, carried through the component cache and the catalog's component.yaml type (schema regenerated); `trigger.ValidateLanes` at every place dependency rules are validated. Inert: nothing reads the configuration yet, plans are unchanged |
| **LN2** | ✅ #724 | `internal/lanes`: resolution (rules, annotations, mode) and lanes for job, component, env, groups and isolated under the environment and `runsOn` boundaries; ids, names, order and index (§5); cycle repair over enforced edges — split by level with neighbouring slices joined back while acyclic, or rejected under `onCycle: reject` / `--strict-lanes`; `plan.lanes` + `jobs[].lane` assigned before the checksum; `orun plan --view lanes` and the `lanes` summary line. A plan without `execution.lanes` is byte-identical (the 38-job example plan compared against main) |
| **LN3** | ✅ this PR | `mode: auto` without weights (§6): at most `maxLanes` unplaced jobs keep the job layout; otherwise jobs are partitioned by (environment, runsOn), the lane budget is shared in proportion (largest remainder, at least one each), enforced-edge chains cluster and are cut by level only when larger than a lane's share, and clusters are packed longest-first. Auto lanes caught in a cycle merge and renumber. Tested on a 50-job cirrus-shaped plan (≤16 lanes, migrate→notifications chain kept together, no repairs) and the 38-job example (8 lanes) |
| **LN4** | 🗓️ planned | `orun run --lane` |
| **LN5** | 🗓️ planned | `lane-matrix`, per-job check runs, grouped logs |
| **LN6** | 🗓️ planned | Docs and release |
| **LN7** | 🗓️ planned | Cirrus on `mode: auto` |
