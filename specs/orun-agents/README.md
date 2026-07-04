# orun-agents — the agent runtime (orun half) — holding

> Cross-repo epic, holding entry. The authoritative design lives in
> **`sourceplane/orun-cloud`** (`specs/epics/saas-agents/`, cluster **AG**):
> remote sandboxed agent sessions (Daytona first) attached to Orun Cloud
> through session-scoped service principals, consuming the work plane as
> their task source. This is "the later Agents epic" that
> [`../orun-work/agents-and-mcp.md`](../orun-work/agents-and-mcp.md) §5
> reserves — dispatch is assignment; the Agents surface is UI over rails
> the work spec lays.

| | |
|---|---|
| **Status** | Holding (single README) — promoted to a full doc set when orun-side work starts |
| **Authoritative spec** | `orun-cloud/specs/epics/saas-agents/` (README + design + plan + risks) |
| **Cluster** | **AG** (cloud) — orun-side milestones will be registered here on promotion |
| **Pairs with** | `orun-work` (WP — the four agent tools, dispatch rails, WP5 MCP), `orun-secrets` (SEC — `how: agent-session` execution-platform fact), `orun-cloud` (OC — session auth patterns) |

## What this repo will own (promotion scope)

The in-sandbox half of the runtime rides tools this repo already ships or
has specced; the orun-side work is deliberately thin:

- **`orun spec pull <spec>@<hash>` in-sandbox** (WP4): the frozen brief +
  `affects` component subgraph an implementation run starts from — tracked
  in `orun-work`, consumed here.
- **The orun MCP over stdio in the sandbox** (WP5): the four-tool write
  surface + work/catalog reads, launched by the session supervisor with the
  session credential — tracked in `orun-work`, consumed here.
- **`catalog affected` as the design run's blast-radius oracle**: shipped
  (`internal/affected`); design runs consume it via the MCP.
- **Conformance fixtures**: golden design-run / implementation-run briefs
  shared with `orun-cloud`'s `tests/agents` evals (the byte-identical
  fixture pattern `internal/worklens` established), so both repos agree on
  what a well-formed brief and a well-formed contract proposal are.
- **Possible later**: an `orun agents` CLI verb group (spawn/attach from
  the terminal against Orun Cloud) — parked until the cloud surface exists.

## Design rules (inherited, not restated)

The cloud epic's decisions bind this repo's half: agents are service
principals with a mandatory responsible owner; no status-write surface
exists anywhere (lifecycle stays a derived fold); secret values flow only
through the lease-bound resolve; the sandbox holds no long-lived
credential. See `orun-cloud/specs/epics/saas-agents/design.md`.
