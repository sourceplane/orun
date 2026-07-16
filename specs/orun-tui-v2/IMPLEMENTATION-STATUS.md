# Implementation status — orun-tui-v2

Living document. Update per merged PR.

| ID | Milestone | Status | Packages/dirs | Notes |
|----|-----------|--------|---------------|-------|
| TR0 | Kernel (shell, frame, store, perf harness) | ✅ Shipped | `internal/tui2/{shell,frame,store}` | Promotes the uncommitted `internal/tui/profile.go` harness |
| TR1 | Northwind Mono design system | ✅ Shipped | `internal/tui2/design` | Tokens from `internal/cockpit/style` |
| TR2 | Data plane (Source, fs-watch, step events) | ✅ Shipped | `internal/tui2/data`, `internal/runner` (hooks PR) | Runner-hook PR is standalone |
| TR3 | Agents surface | ✅ Shipped | `internal/tui2/surfaces/agents`, `internal/tui2/agentfold` | Reuses `internal/agent/attach` unchanged |
| TR4 | Activity surface | ✅ Shipped | `internal/tui2/surfaces/activity` | Retires Run Dashboard / Log Explorer / History |
| TR5 | Catalog surface + Compose flow | ✅ Shipped | `internal/tui2/surfaces/catalog` | Absorbs Plan Studio |
| TR6 | Work surface | ✅ Shipped | `internal/tui2/surfaces/work` | Cloud lane built against fixtures |
| TR7 | Home + palette + Events | ✅ Shipped | `internal/tui2/surfaces/{home,events}`, command registry | Help generated from registry |
| TR8 | Cloud connect + default flip | ◐ Partial | `internal/tui2/data` (CloudLane), `cmd/orun/command_tui.go` | Default flipped; cloud runs lane fixture-tested. Live stage smoke + work SSE/attention/remote-sessions lanes + in-app device flow pending sign-in (needs human browser approval) — TR8.1 |
| TR9 | Cutover (delete v1, rename, hardening) | ◐ Prep landed, deletion gated | `internal/tui` (delete), `internal/tui2`→`internal/tui` | Prep: bare `orun agent` opens the v2 Agents surface. Deletion gated per plan on (a) ≥1 release soaked with the flipped default, (b) TR8.1 cloud lanes, (c) moving `internal/tui/services` out from under `internal/tui` (v2's composer wraps it) |
