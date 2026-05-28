## Best model: **Orun Cockpit TUI**

Build `orun tui` as a **component-native control plane**, not just a wrapper around `orun plan/run/status/logs`.

Orun already has the right primitives: desired-state repos compile from `intent.yaml + component.yaml + composition sources` into a deterministic plan DAG and explicit runtime execution . The TUI should make that model visible and navigable.

### The core experience

```text
┌─ Orun Cockpit ─────────────────────────────────────────────────────────────┐
│ repo: sourceplane/multi-tenant-saas   plan: latest a1b2c3d   run: running │
├───────────────┬──────────────────────────────────────┬───────────────────┤
│ NAVIGATOR     │ MAIN                                 │ INSPECTOR         │
│               │                                      │                   │
│ Components    │ component → env → job DAG/list       │ selected item     │
│ Environments  │                                      │ inputs/env/policy │
│ Plans         │ live run timeline                    │ deps/gates        │
│ Runs          │                                      │ commands/actions  │
│ Logs          │ failed/running/pending priority view │                   │
├───────────────┴──────────────────────────────────────┴───────────────────┤
│ / search  g generate plan  d dry-run  r run  l logs  e env  ? help       │
└───────────────────────────────────────────────────────────────────────────┘
```

The winning UI pattern is **k9s + lazygit + GitHub Actions run viewer**, adapted to Orun’s component DAG.

k9s is a strong reference because it continuously watches Kubernetes resources and lets users navigate, observe, and act from a terminal UI ([GitHub][1]). Lazygit is a good reference for command-discoverability and fast keyboard workflows around a complex graph of Git operations ([GitHub][2]). Lazydocker is relevant for live logs/stats plus direct operational actions from a terminal UI ([LazyDocker][3]).

---

## Use **Bubble Tea + Bubbles + Lip Gloss**

For Go, I would use the Charm stack:

| Layer            | Choice         | Why                                                                                                |
| ---------------- | -------------- | -------------------------------------------------------------------------------------------------- |
| App architecture | **Bubble Tea** | Elm-style state/update/view model, good for complex full-window TUIs ([GitHub][4])                 |
| Widgets          | **Bubbles**    | Lists, filtering, pagination, spinner, viewport, help patterns are already available ([GitHub][5]) |
| Styling/layout   | **Lip Gloss**  | Declarative, CSS-like terminal styling and layout primitives ([GitHub][6])                         |
| Alternative      | `tview`        | Good rich widgets, but less ideal for Orun’s event-driven streaming/log/run model ([GitHub][7])    |

`Bubble Tea` fits Orun better than `tview` because Orun TUI needs to react to streams: plan generation, run events, status polling, remote state updates, log appends, resize events, filtering, and command palette actions.

---

## Product model: five primary modes

### 1. **Browse mode**

Default entry screen.

Shows:

* components
* environments
* composition type
* profile
* path
* changed/not changed
* dependency count
* last run status

This maps directly to Orun’s current `get` model, which already lists plans, runs, jobs, components, and environments .

Best interactions:

```text
/        fuzzy search
enter    inspect component
e        filter by environment
t        filter by type/composition
c        show changed-only
d        show dependency tree
p        generate plan for selection
```

### 2. **Plan Studio**

This is where Orun can feel next-level.

Instead of asking users to remember flags, let them compose a plan visually:

```text
Plan target:
  scope        full | changed | selected component | selected env
  environment  dev, stage, prod
  trigger      github-pull-request, github-push-main
  base/head    main...HEAD
  mode         dry-run first
```

Then show:

* planned jobs
* dependency DAG
* profile selected per component/env
* gates
* unsafe/destructive capabilities
* composition source digest
* generated plan checksum

Orun already supports plan generation with env/component filters, `--changed`, trigger simulation, DAG views, and saved plan output . The TUI should turn those into an interactive plan transaction.

### 3. **Execution Dashboard**

For active execution:

```text
RUN release-candidate · running · 14/38 jobs · 2 failed · 3 running

● api-edge-worker@production.verify     01:22
✗ cloudflare-hyperdrive@stage.plan      failed at terraform.plan
✓ platform-shared@production.build      00:18
○ web-console@production.deploy         waiting on api-edge-worker
```

Use the same status semantics Orun already exposes: running, completed, failed, pending, detailed step view, watch mode, local or remote state .

Key interactions:

```text
enter   inspect job
l       open logs
r       retry failed job
s       show step timeline
x       stop/cancel if supported later
```

### 4. **Log Explorer**

This should be better than raw `orun logs`.

Layout:

```text
left: jobs/steps
main: log stream
right: failure summary / extracted errors / command metadata
```

Capabilities:

* jump to first error
* show only failed steps
* follow live logs
* collapse successful setup noise
* copy failed command
* open raw log file path
* compare previous run logs for same job

Orun already stores logs per execution as `.orun/executions/{exec-id}/logs/{job}/{step}.log`, and remote logs can be fetched from the backend . The TUI should expose logs as structured step artifacts, not one giant stream.

### 5. **History / Replay**

This is where Orun can beat normal CI UX.

Show:

```text
Runs
  ✓ local-a1b2c3-20260528     38/38   4m12s   main
  ✗ local-d4e5f6-20260528     35/38   3m09s   feature/x
  ● gha-26562411779-a1b2c3    14/38   running PR #139
```

For each run:

* plan checksum
* job count
* failed jobs
* duration
* trigger
* git ref/sha
* runner
* local vs remote
* replay options

Orun execution records already track `state.json`, `metadata.json`, and per-step logs, enabling resume, retry, and immutable log review .

---

## The best UI navigation model

Use a **resource-first model**, similar to Kubernetes tooling:

```text
orun tui
  Components
  Environments
  Plans
  Runs
  Jobs
  Steps
  Logs
  Compositions
```

Then support slash commands:

```text
:plan changed
:plan component api-edge-worker env stage
:run latest dry-run
:run latest
:logs failed
:describe job api-edge-worker@stage.verify
:filter type terraform
:filter env prod
```

This gives both beginner discoverability and power-user speed.

---

## Architecture inside the existing Go CLI

Do this as a thin Cobra command plus internal TUI package:

```text
cmd/orun/command_tui.go

internal/tui/
  app.go              Bubble Tea program root
  model.go            global TUI state
  keymap.go           global key bindings
  theme.go            Lip Gloss styles
  services/
    orun_service.go   high-level Orun operations
    plan_service.go
    run_service.go
    log_service.go
    history_service.go
  views/
    dashboard.go
    components.go
    plan_studio.go
    run_view.go
    logs.go
    inspector.go
    command_palette.go
  events/
    watcher.go
    run_events.go
    log_tail.go
```

Keep with Orun’s current internal architecture: command handlers should stay thin, planning should remain deterministic, runtime behavior should stay explicit in the plan, and internal model contracts should remain stable between stages .

### Service boundary

Do not make the TUI shell out to `orun` internally as the primary design. Use internal packages directly.

```go
type OrunService interface {
    LoadWorkspace(ctx context.Context, req WorkspaceRequest) (*WorkspaceSnapshot, error)
    GeneratePlan(ctx context.Context, req PlanRequest) (*PlanResult, error)
    RunPlan(ctx context.Context, req RunRequest) (<-chan RunEvent, error)
    ListRuns(ctx context.Context, req ListRunsRequest) ([]RunSummary, error)
    Describe(ctx context.Context, ref ResourceRef) (*ResourceDescription, error)
    TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error)
}
```

Shelling out to existing commands can be a temporary bootstrap, but the end state should reuse `internal/loader`, `internal/planner`, `internal/runner`, and log/state readers.

---

## Critical design principle: plan-first safety

The TUI should never make `run` feel like an accidental keypress.

Recommended flow:

```text
Generate Plan → Review DAG → Dry Run → Execute
```

Execution should require a confirmation pane that shows:

* selected scope
* environments
* destructive-looking capabilities
* runner backend
* concurrency
* plan checksum
* affected components
* failed/pending gates

This matters because Orun’s own docs distinguish non-destructive validation/plan/dry-run from potentially destructive execution like Terraform apply, Helm deploy, cloud deploys, and `orun run` without dry-run .

---

## Features that make it “next level”

### 1. **Explain any node**

On any component/job/step:

```text
Why is this here?
Why this profile?
Why this environment?
Why is it waiting?
Why did this component become changed?
Which dependency blocks it?
Which composition source produced it?
```

This fits Orun perfectly because the plan is the compiled truth.

### 2. **Plan diff**

Show:

```text
previous plan a1b2c3 → new plan d4e5f6

+ 3 jobs added
- 1 job removed
~ 5 jobs changed
! production gate added
```

Diff by:

* jobs
* steps
* rendered commands
* env vars
* parameters
* dependencies
* composition source digests

### 3. **Failure workbench**

For a failed job:

```text
Failed: cloudflare-hyperdrive@stage.validate
Step: terraform.plan
Exit: 1

Likely issue:
  Missing secretsmanager permission: CreateSecret

Actions:
  r retry job
  l open raw log
  c copy command
  p generate component-only plan
  d show dependency chain
```

### 4. **Context-aware launch**

When run inside a component directory, open scoped to that component and its dependencies. Orun already has context-aware discovery and auto-scoping behavior in the CLI flow .

### 5. **Remote execution cockpit**

Support:

```text
orun tui --remote-state
```

Then show remote runs, status, logs, and dependency waiting from the backend. Orun already supports remote status/log inspection and distributed execution via remote state .

---

## MVP implementation order

### Phase 1 — Read-only cockpit

Implement:

```bash
orun tui
```

Features:

* discover intent root
* list components
* list environments
* list plans
* list runs
* list jobs from latest plan
* describe selected component/job/plan/run
* open logs for selected job/step

This is low-risk because it uses existing Orun state and JSON-capable commands.

### Phase 2 — Plan Studio

Add:

* generate plan
* changed plan
* env/component filters
* trigger simulation
* named plan creation
* DAG/list view
* plan checksum and plan summary

Default to dry-run workflow.

### Phase 3 — Run Dashboard

Add:

* run selected plan
* dry-run
* live status
* live logs
* retry failed job
* run selected job
* background run attach

### Phase 4 — Advanced Orun-native UX

Add:

* plan diff
* failure workbench
* dependency graph
* explain mode
* remote-state runs
* command palette
* component profile/env matrix view

---

## Final recommendation

Build **`orun tui` as a Bubble Tea-powered Orun Cockpit**.

The UI should be:

* **k9s-like** for resource browsing and live status
* **lazygit-like** for keyboard-first command discovery
* **GitHub Actions-like** for run/job/step/log navigation
* **Orun-native** in its core model: intent → component → composition → profile → plan DAG → execution record

The strongest product idea is this:

> Orun TUI should make the plan DAG feel alive: browse the repo as components, generate a plan as a reviewable transaction, execute safely, then replay every past run down to job, step, command, log, and dependency reason.

That would make Orun feel less like a CLI command and more like a local CNCF-grade DevOps control plane.

[1]: https://github.com/derailed/k9s?utm_source=chatgpt.com "K9s - Kubernetes CLI To Manage Your Clusters In Style!"
[2]: https://github.com/jesseduffield/lazygit?utm_source=chatgpt.com "jesseduffield/lazygit: simple terminal UI for git commands"
[3]: https://lazydocker.com/?utm_source=chatgpt.com "LazyDocker - Simple Docker Terminal UI Tool"
[4]: https://github.com/charmbracelet/bubbletea?utm_source=chatgpt.com "charmbracelet/bubbletea: A powerful little TUI framework"
[5]: https://github.com/charmbracelet/bubbles?utm_source=chatgpt.com "charmbracelet/bubbles: TUI components for Bubble Tea"
[6]: https://github.com/charmbracelet/lipgloss?utm_source=chatgpt.com "charmbracelet/lipgloss: Style definitions for nice terminal ..."
[7]: https://github.com/rivo/tview?utm_source=chatgpt.com "rivo/tview: Terminal UI library with rich, interactive widgets"
