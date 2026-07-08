# orun-agents — Design (the runtime)

Status: Draft (normative once AG0 lands)

The agent runtime, in the binary. Written against repo reality as of
2026-07-08: the object model is shipped and unconditional
(`specs/orun-object-model/`, M0–M13; `internal/objectstore`, `internal/nodes`,
`internal/nodewriter`, `internal/objremote`, `internal/objcatalog`,
`internal/objmodel`); the TUI is a Bubble Tea cockpit with modes `1..5`
(`internal/tui`, `ModeCatalog`/`ModePlanStudio`/`ModeRun`/`ModeLogExplorer`/
`ModeHistory`); cloud auth exists (`internal/cliauth` loopback/device login +
`internal/remotestate/auth.go` OIDC/session refresh); the affected engine is
shipped (`internal/affected`, `orun catalog affected`); the work model + `spec
pull` are specified (`specs/orun-work/`, WP4 unbuilt); `agents/orchestrator.md`
is the lone hand-authored agent character. There is no agent runtime yet —
this epic adds `internal/agent`.

---

## 1. The delegation model: a coding agent is an executor

orun already converges plans on **swappable run backends** (local-shell,
Docker, GitHub Actions). Delegating a task to a coding agent is the same
category of thing: an executor that takes a frozen input (the brief) and
produces an output (a PR). So the runtime reuses orun's discipline rather than
inventing a subsystem:

- The coding agent (Claude Code, …) is an **`AgentDriver`** behind a seam
  (§3), the way a container runtime is a run backend.
- Its input is a **sealed `AgentBrief`** (`data-model.md` §3.1) — content, not
  a prompt string, so a run is reproducible and portable.
- Its work is judged by **orun's own truth**: `orun run` gates and the work
  plane's observation log. The runtime asserts nothing about outcomes.

`internal/agent` is thin by construction: it loads an agent type, assembles a
brief, launches a driver, wires the MCP, records an append-only event log, and
seals the session. Everything with *meaning* — what exists, what's affected,
what's Ready, whether gates passed — is owned by packages that already exist.

```
internal/agent/
  runtime.go     the loop: load type → assemble brief → launch driver → stream → seal
  brief.go       AgentBrief assembler (agent-type + task contract + affected + literacy)
  mcp.go         MCP manager: launch orun MCP (stdio), write driver MCP config, tool policy
  session.go     append-only session event log + segment sealing (AgentSessionSegment)
  literacy.go    embedded, versioned base literacy (data-model §4)
  driver/        the AgentDriver seam
    driver.go      interface + registry + the stdio protocol
    claudecode.go  first driver (headless stream-JSON)
    conformance.go the oracle a new driver must pass (AG4)
internal/agenttype/
  load.go        parse agents/*.md (frontmatter + body) → AgentTypeSnapshot
  seal.go        seal/pull via nodes + nodewriter + objremote
cmd/orun/
  command_agent.go   run | tui | serve | pull | import | replay | context | lint
internal/tui/
  ModeAgent + views/agent.go   the 6th cockpit mode (§6)
```

---

## 2. Three run modes, one runtime

The same `internal/agent` loop drives all three; they differ only in transport.

| Mode | Command | Transport | Cloud? |
|---|---|---|---|
| **Interactive (local)** | `orun agent` → TUI Agent mode | Bubble Tea | none |
| **Headless one-shot** | `orun agent run --task ORN-142` | stdout/exit code | none (or `--remote` reads) |
| **Cloud-attach** | `orun agent serve --session as_…` | event stream ↔ cloud DO | yes |

`orun agent run` is the primitive CI, scripts, and the cloud box all call.
`orun agent` (TUI) is the local product. `orun agent serve` is the identical
loop with its event stream pointed at the per-session Durable Object in the
paired cloud epic (§7). **No mode has capabilities another lacks** — cloud adds
governance and persistence around the same runtime, not new runtime behavior.

---

## 3. The `AgentDriver` seam (Claude Code first, any binary next)

```go
package driver

type Driver interface {
    ID() string                                   // "claude-code"
    Launch(ctx context.Context, b Brief, io IO) (Proc, error)  // headless
    // Proc is steered/approved/stopped through the IO channels.
}

type IO struct {
    Events   chan<- Event      // harness → runtime (stream-JSON, normalized)
    Steer    <-chan Message    // runtime → harness (user turns mid-run)
    Approve  <-chan Verdict    // runtime → harness (tool-permission answers)
    MCPConfig string           // path to the MCP server config the driver hands its agent
}
```

- **Claude Code first**: headless mode, stream-JSON events in/out, an MCP
  config file, permission-prompt callbacks mapped to the approval channel.
- **"Any binary" is a contract, not a hope.** The driver stdio protocol
  (normalized `Event`/`Message`/`Verdict` JSON over stdin/stdout) is
  documented, and `driver/conformance.go` (AG4) is an **oracle**: a stub
  driver must pass the full session lifecycle — launch, stream, steer,
  approve, produce artifact, stop — untouched. This is orun's conformance-
  oracle discipline (`internal/worklens` ↔ Go/TS fixtures) applied to drivers.
  A new agent binary is an adapter that passes the suite, never a change to
  `internal/agent`.
- **The driver never gets raw credentials.** It receives the MCP config (which
  points at the orun MCP + platform MCP with the session credential managed by
  the runtime) and env the runtime injected; it never sees the session token
  mint or refresh.

---

## 4. Brief assembly (the frozen input)

Before any driver launches, `brief.go` seals an `AgentBrief` (`data-model.md`
§3.1):

1. **Resolve the spec** — `spec pull <spec>@<hash>` (or, offline, the local
   `.orun/specs/…` view) → the `SpecSnapshot` doc + task envelopes + contracts.
2. **Compute the affected set** — `internal/affected` over the task's
   `affects[]` (implementation) or the spec's named components (design); freeze
   it as a content-addressed `AffectedSet` blob. This is the blast radius —
   frozen, so the run is reproducible and the human can see exactly what the
   agent was told it may touch.
3. **Render instructions** — base literacy (`extends`) + the persona body +
   the task contract, composed into one system-prompt blob. Layered, not
   concatenated ad hoc: literacy is the binary's, persona is the type's,
   contract is the task's.
4. **Seal** — the `AgentBrief` node pins `spec`, `affected`, `literacy`,
   `instructions` by hash. `orun agent run --dry-run` prints the brief and its
   id without launching — the reviewable "here is exactly what the agent will
   see" artifact.

Determinism is the point: `briefId` is a function of (spec, contract, catalog,
literacy, persona). Same inputs → same brief → a local run and a cloud run are
the same run.

---

## 5. MCP as hands + tool policy

`mcp.go` launches the **orun MCP over stdio** in-process/in-sandbox (WP5's
server — work + catalog reads, the four writes) and, when cloud-attached, adds
the **platform MCP** (remote Streamable-HTTP, `saas-mcp-server`). It writes the
driver's MCP config to point at both.

Tool policy (`tools.{allow,ask,deny}` from the agent type) is enforced **by the
runtime**, between the driver and the MCP:

- `deny` (default): the tool is absent from the driver's config — unreachable.
- `allow`: passes through.
- `ask`: the runtime intercepts the call, emits `approval_requested` into the
  session log, and blocks until a `Verdict` arrives (from the TUI locally, or
  the console over the DO in cloud). Both outcomes are logged events —
  attributable, replayable.

Enforcement is **layered by context** (this is the security keystone):

- **Local**: the agent acts as *you* (your `cliauth` session / your env model
  key). Tool policy is orun-enforced guardrails on top of your own grants.
- **Cloud**: the agent acts as the profile's **service principal**; every MCP
  call physically re-enters api-edge, so platform RBAC re-enforces the same
  contract independently. The `.md` is the same; the principal and the second
  enforcement layer differ.

The work plane's **no-status-write** property is inherited for free: the MCP
exposes no such tool, so "agent lies about progress" is unrepresentable at the
runtime, not policed by it.

---

## 6. The TUI Agent mode (local-first product)

A 6th mode in the cockpit (`internal/tui`), reached by `orun agent` or key `6`,
alongside Catalog/Plan/Run/Logs/History. Layout follows the existing
sidebar│stage│inspector shell:

- **Sidebar**: sessions (this run + recent local sessions from
  `refs/agents/sessions/…`), agent types (from the catalog projection).
- **Stage**: the live transcript (streamed from the session event log), with
  the sealed brief and the **live affected subgraph** inline — you see what the
  agent may touch as it works.
- **Inspector**: the current tool call / approval, the contract, cost sample,
  and the PR link when it lands.
- **Approvals**: `ask` tools surface as actionable cards; a keystroke sends the
  `Verdict`.

This runs with **zero cloud**. It reads the local `.orun/` object store, the
local catalog, the local affected engine; the model key comes from the
environment. This is the whole framework on a laptop — the funnel into cloud,
and a first-class surface in its own right, not a preview.

`orun agent run` (headless) is the same loop with the TUI replaced by
structured stdout (`--json` through the cockpit viewmodels, per
`internal/cockpit` conventions) — for CI, scripts, and the cloud box.

---

## 7. Cloud-attach: the boundary with `saas-agents` (AG5–AG11)

`orun agent serve --session as_<id>` is the in-sandbox entrypoint. The split is
deliberate and thin:

**orun owns (this spec):** the runtime loop, driver, brief, MCP wiring, tool
policy, the session event log + sealing. Inside the sandbox, orun authenticates
to the cloud with the existing `cliauth`/`remotestate` machinery, pulls the
frozen brief (`spec pull @hash` + `agent pull <type>@hash`), runs, and streams
`AgentSessionSegment` entries + transcript chunks out over the attach channel.

**The cloud owns (paired epic):** provisioning the Daytona box, minting the
session-bound service-principal token (injected as the bootstrap credential the
supervisor exchanges), the per-session Durable Object that receives orun's
event stream and fans it to the console over SSE, and dispatch (assignment →
`spawn` → `orun agent serve`).

The attach protocol is the session event vocabulary (`data-model.md` §3.3) over
a single outbound channel (the sandbox dials out; the cloud never reaches in) —
NAT-safe and provider-portable. A suspend snapshots the box; resume re-runs
`orun agent serve` and re-bootstraps credentials (tokens never survive a
snapshot). Because the runtime is identical to local, the cloud team integrates
against a binary they can run on a laptop — the seam is testable without
Daytona.

---

## 8. What "world-class" means here (the design bets, summarized)

1. **Local-first runtime** — the agent is the binary, obeying orun's
   constitution; cloud is the same binary in a box. One code path, two
   contexts.
2. **Agent types are content** — `agents/*.md` seals to an object, catalog-
   projected, version-pinnable, syncable. Governable like a service, not a
   dotfile.
3. **Sealed, reproducible runs** — brief and session are content-addressed;
   `orun agent replay <session>@<hash>` re-renders any run byte-identically.
4. **Provenance to production** — the session snapshot extends the
   source→catalog→spec→task Merkle chain through the agent to the live
   revision (`data-model.md` §7). Walk back from any deploy to the exact agent
   and inputs.
5. **Structural honesty** — no status-write surface anywhere; the observation
   log is the only truth about progress. Inherited from the work plane, not
   re-policed.
6. **Any binary, proven** — the driver seam plus a conformance oracle make
   harness pluggability real.
7. **Understanding tracks the binary** — base literacy ships versioned with
   orun; personas stay small and never rot.

These are the properties no bolt-on agent product can copy without orun's
object graph underneath — which is the argument for building the runtime
*here*, in the binary, rather than as a cloud feature that happens to call an
API.
