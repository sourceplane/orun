# Implementation status — orun-tui-v2

Living document. Update per merged PR.

| ID | Milestone | Status | Packages/dirs | Notes |
|----|-----------|--------|---------------|-------|
| TR0 | Kernel (shell, frame, store, perf harness) | ✅ Shipped | `internal/tui2/{shell,frame,store}` | Promotes the uncommitted `internal/tui/profile.go` harness |
| TR1 | Northwind Mono design system | ☐ Not started | `internal/tui2/design` | Tokens from `internal/cockpit/style` |
| TR2 | Data plane (Source, fs-watch, step events) | ☐ Not started | `internal/tui2/data`, `internal/runner` (hooks PR) | Runner-hook PR is standalone |
| TR3 | Agents surface | ☐ Not started | `internal/tui2/surfaces/agents`, `internal/tui2/agentfold` | Reuses `internal/agent/attach` unchanged |
| TR4 | Activity surface | ☐ Not started | `internal/tui2/surfaces/activity` | Retires Run Dashboard / Log Explorer / History |
| TR5 | Catalog surface + Compose flow | ☐ Not started | `internal/tui2/surfaces/catalog` | Absorbs Plan Studio |
| TR6 | Work surface | ☐ Not started | `internal/tui2/surfaces/work` | Cloud lane built against fixtures |
| TR7 | Home + palette + Events | ☐ Not started | `internal/tui2/surfaces/{home,events}`, command registry | Help generated from registry |
| TR8 | Cloud connect + default flip | ☐ Not started | `internal/tui2/data` (CloudSource), `cmd/orun/command_tui.go` | Needs stage token for live smoke |
| TR9 | Cutover (delete v1, rename, hardening) | ☐ Not started | `internal/tui` (delete), `internal/tui2`→`internal/tui` | After ≥1 release soaked |
