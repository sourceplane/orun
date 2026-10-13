# orun-lanes — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **LN0** | ✅ this PR | The design: the lane (one environment, one runner, many jobs), configuration and resolution, the planner's invariants, auto mode without weights, cycle repair by level, `orun run --lane`, the lane matrix and per-job check runs, and the compatibility table |
| **LN1** | 🗓️ planned | Schema: `execution.lanes`, `spec.lane`, validation |
| **LN2** | 🗓️ planned | Explicit modes in the plan, cycle repair, `--view lanes` |
| **LN3** | 🗓️ planned | `mode: auto` |
| **LN4** | 🗓️ planned | `orun run --lane` |
| **LN5** | 🗓️ planned | `lane-matrix`, per-job check runs, grouped logs |
| **LN6** | 🗓️ planned | Docs and release |
| **LN7** | 🗓️ planned | Cirrus on `mode: auto` |
