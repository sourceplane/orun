---
title: Extending orun
description: The seams orun is built to grow along — commands, compiler stages, executors, agent drivers, MCP tool providers, object kinds, and workflow actions — and the file where each is registered.
---

`orun` is designed so new commands, stages, and runtime capabilities can be added without collapsing the planner architecture. Each seam below names the interface to implement and the file where implementations are registered.

## Add a new CLI command

Follow the existing command pattern under `cmd/orun`:

1. keep Cobra wiring in a dedicated `command_*.go` file
2. place business logic in `internal/*`
3. reuse existing stage packages instead of duplicating parsing or normalization

That keeps the command layer readable and testable. Commands are attached to the root in `cmd/orun/commands_root.go`.

## Extend the planner

Prefer changes that preserve the current package contracts:

- `internal/model` stays the stage contract
- `expand` remains a pure transformation from normalized input to component instances
- `render` remains deterministic and side-effect free except for file writing

## Add a runner backend

Execution backends implement `executor.Executor` in `internal/executor/executor.go`:

```go
type Executor interface {
    Name() string
    Prepare(ctx ExecContext) error
    RunStep(ctx ExecContext, job model.PlanJob, step model.PlanStep) (output string, err error)
    Cleanup(ctx ExecContext) error
}
```

An executor that needs job-level cleanup also implements `JobFinalizer`. Backends are looked up by name through `executor.Get` (`internal/executor/registry.go`); the local, Docker, and GitHub Actions backends live beside it. When adding one, document its semantics in [execution runtime](../architecture/execution-runtime.md) and add tests for preparation, step execution, and cleanup.

## Add an agent driver

The agent runtime drives a coding agent behind the `driver.Driver` seam in `internal/agent/driver/driver.go`:

```go
type Driver interface {
    ID() string
    Launch(ctx context.Context, b Brief, io IO) (Proc, error)
}
```

`Launch` must be non-blocking: it starts the agent and returns a `Proc`; events flow on `io.Events`. Drivers are registered with `driver.Register` — a duplicate ID panics, like a duplicate Cobra command — and the shipped set (`Stub`, `ClaudeCode`, `Bootstrap`) is registered in `cmd/orun/command_agent_run.go`. `orun agent drivers` lists what is registered; `internal/agent/driver/conformance.go` holds the conformance checks a new driver should pass.

## Add an MCP tool provider

`orun mcp serve` composes tool planes over one stdio transport. A plane implements `mcpserve.ToolProvider` in `internal/mcpserve/server.go`:

```go
type ToolProvider interface {
    Tools() []ToolDef
    Call(ctx context.Context, name string, args json.RawMessage) (Result, bool)
}
```

`Call` returns `owned=false` for a tool that is not this provider's; an owned failure maps to an `isError` result, never a protocol fault. A provider may additionally implement `ResourceProvider` (resource templates and reads) or `PromptProvider`; both are type-asserted at dispatch. Providers are assembled in `assembleMcpProviders` in `cmd/orun/mcp_serve.go` — today the provenance pen (`internal/penmcp`) and the platform plane (`internal/platformmcp`), plus the connection-info provider.

## Add an object kind

The object model's typed nodes are a closed list in `internal/nodes/kinds.go` (`KindSourceSnapshot` … `KindAgentTypeSnapshot`, `KindTask`). A new kind needs:

1. a `Kind…` constant in `kinds.go`
2. a record type with canonical encoding and a `Validate()` method (see `internal/nodes/validate.go`, `agents.go`, `tasks.go`)
3. a writer path through `internal/nodewriter` and, if it is a root, a ref layout under `internal/objectstore/refstore`
4. coverage that keeps `make test-object-model` green — the object-model lint gate (`scripts/check-object-model.sh`) forbids `time.Now()` and other non-determinism in these packages

Everything here is content-addressed: change the encoding of an existing kind and you change every hash, so add kinds rather than reshape them.

## Add a workflow action or hook action

There are two closed action registries:

| Registry | Used by | Registered in |
| --- | --- | --- |
| Workflow built-ins (`http.request`, `script`, `sleep`) | The `action:` verb of a `kind: Workflow` step | The `registry` map in `internal/flow/actions.go`; each `Action` carries a `Validate` and an `Invoke` function |
| Blueprint hook actions (`<namespace>/<verb>@v<major>`) | Hooks declared by a scaffolding blueprint | `register(Spec, RunFunc)` in `internal/actions/actions.go`, called from each action's `init()` (`doctor.go`, `forge_actions.go`, `http_probe.go`, `pr_land.go`, `secrets_reconcile.go`, `task_ensure.go`) |

Both are closed on purpose: an action id that is malformed or duplicated panics at startup rather than shipping a registry that lies about itself. `orun workflow` and the [workflow schema](../reference/workflow-schema.md) document the built-ins; the [baselines](../concepts/baselines.md) page covers where hook actions run.

## Typical extension ideas

- `graph` output for richer DAG visualizations
- `lint` for policy-only validation passes
- new composition types for additional platform domains
- extra execution backends when the local, Docker, and GitHub Actions set is not enough
- a second coding-agent driver behind the same brief and session log

## Related

- [Contributing](./contributing.md) — the development loop and sign-off.
- [Internals](../architecture/internals.md) — every package, by subsystem.
- [Agent runtime](../concepts/agent-runtime.md) — how briefs, drivers, and sessions fit together.
- [`orun mcp`](../cli/orun-mcp.md) — the composed MCP server these providers mount into.
