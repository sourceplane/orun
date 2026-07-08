# Spec: orun-agents — the agent runtime

**The orun binary is the agent runtime.** orun already compiles intent,
resolves the catalog, computes the affected set, and coordinates runs; this
epic makes it also **delegate work to a coding agent** — Claude Code first,
any binary behind a driver seam. Agent *types* become first-class
content-addressed objects in the same graph as sources, catalogs, and specs
(sealed, synced, pulled, catalog-projected). An agent runs **fully local**
from a terminal (a new TUI Agent mode, no cloud), and the **identical
runtime** runs inside an Orun Cloud sandbox — the cloud just supplies the box
and the connection. This is "the later Agents epic"
[`../orun-work/agents-and-mcp.md`](../orun-work/agents-and-mcp.md) §5 reserves,
paired with `orun-cloud/specs/epics/saas-agents/` (cluster **AG**), which owns
the sandbox lifecycle, session identity, and console.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft** — authored, not yet ready to build; open decisions in `risks-and-open-questions.md` |
| Cluster | **AG** (agent framework — cross-repo; orun owns the **runtime + object kinds**, orun-cloud owns the **sandbox control plane + console**) |
| Target branch | `claude/agents-epic-design-vnvyyq` (design), then `main` (PRs merged incrementally) |
| Builds on | `orun-object-model/` (the content-addressed object graph — `object-store.md` L0, `data-model.md` node kinds; the doc-blob / `SpecSnapshot` seal+pull pattern this epic mirrors) · `orun-work/` (the four-tool agent write surface, `spec pull`, dispatch-is-assignment, the "no status write" invariant) · `internal/affected` (`catalog affected` — the blast-radius oracle) · `internal/objcatalog` (catalog projection seam) · `internal/objremote` (set-difference sync) · `internal/tui` (the Bubble Tea cockpit — modes `1..5`, gaining a 6th) · `internal/cliauth` + `internal/remotestate` (session/OIDC auth — how the in-sandbox binary reaches the cloud) · `orun-secrets/` (`how: agent-session` resolve) |
| Decisions locked | (1) **The runtime lives in the binary, not the cloud** — `orun agent` is local-first (constitution: cloud is additive, never required); the cloud runs the *same* binary in a box. (2) **Agent types are content-addressed objects** — an `agents/*.md` file seals to an `AgentTypeSnapshot` (frontmatter = typed capability envelope; body = persona blob, the doc-blob spine), addressed, synced, pulled `@<hash>`, and catalog-projected, exactly like a `SpecSnapshot`. (3) **Delegation is an executor** — the coding agent is an `AgentDriver` behind a seam (Claude Code first), the same swap-discipline as local-shell / Docker / GHA run backends; a driver conformance oracle makes "any binary" real. (4) **orun understanding ships with the binary** — a versioned **base literacy** module (the object model, the invariants, the MCP tools) that every agent type `extends`; the `.md` carries only the specialization, so understanding tracks the orun version. (5) **Sessions are sealed proof** — the session event log seals as a chained `AgentSessionSnapshot` (agent-type ref `@hash` + brief ref `@hash` + catalog pin + segment/transcript refs), extending the source→catalog→spec→task Merkle chain through the agent to the PR. (6) **No status-write surface anywhere** — the runtime inherits the work plane's structural honesty: an agent physically cannot assert progress; it does the work and the observation log speaks. |
| Gate | **Human-independent for the runtime.** AG0–AG4 (object kinds, seal/pull, the delegation loop, TUI, driver conformance) need no credentials — a local agent runs against a local `.orun/` and the model provider's key from the environment. Cloud/live gates (Daytona, session identity) live in the paired cloud epic. |

## The one-paragraph thesis

Every "AI coding agent" product bolts an agent onto a tracker. orun inverts
it: the agent is a **client of a truth engine that already exists**. orun owns
the object graph (what exists, by content hash), the catalog (who owns what,
what depends on what), the affected engine (what a change touches), and — via
the work plane — a lifecycle that is *derived, never authored*. So the agent
needs no new trust: it pulls a **frozen, content-addressed brief** (spec doc +
task contract + the exact catalog it resolves against), runs behind a
**driver seam** with the orun MCP as hands, and produces a PR the
**observation log** judges like a human's. What makes it a *framework* rather
than a script is that every input and every action is **content**: the agent
type is a sealed object (`agents/*.md` → `AgentTypeSnapshot`), the brief is a
sealed object, the session is a sealed, tamper-evident object — so a run is
reproducible, auditable, and portable between your laptop and a cloud
sandbox by hash. Local-first is not a compromise; it is the product: `orun
agent` in a terminal is the whole framework, and the cloud is that same
binary, kept running in a box, governed by the workspace's RBAC.

## Why this is the right home (vs. a cloud runtime)

| Concern | Runtime-in-the-cloud (rejected) | Runtime-in-the-binary (this spec) |
|---|---|---|
| orun's constitution | violates "local-first, cloud additive" | obeys it — local is primary |
| Local vs. cloud agents | two codebases, drift | **one code path**, two contexts |
| Reproducibility | cloud-only, opaque | sealed brief + session; replay anywhere |
| "Any binary" | a cloud integration | a driver + conformance oracle in Go |
| Truth (catalog/affected/work) | re-fetched over HTTP | native, in-process, offline-capable |
| Cloud's job | supervise the agent | provision a box + relay + dispatch |

## Read order

1. This README.
2. [`design.md`](./design.md) — the runtime: `internal/agent`, the driver
   seam, brief assembly, MCP wiring, base literacy, the three run modes, the
   TUI Agent mode, and the cloud-attach boundary.
3. [`data-model.md`](./data-model.md) — the new object kinds
   (`AgentTypeSnapshot`, `AgentSessionSnapshot`, session segments), refs,
   the seal/pull flow, and the catalog projection — the heart of "agent md
   in the object store."
4. [`agent-type-format.md`](./agent-type-format.md) — the `agents/*.md`
   schema: capability frontmatter vs. persona body, `extends`, resolution
   order.
5. [`implementation-plan.md`](./implementation-plan.md) — AG0–AG4 (runtime)
   with "done when"; the cloud AG5–AG11 live in the paired epic.
6. [`risks-and-open-questions.md`](./risks-and-open-questions.md).

## Milestones at a glance (orun-owned; AG5–AG11 in the cloud epic)

| ID | Milestone | Status |
|----|-----------|--------|
| AG0 | Object kinds + foundation: `AgentTypeSnapshot` / `AgentSessionSnapshot` / session-segment schemas (canonical JSON, framed/addressed per `object-store.md`); refs `refs/agents/…`; `internal/agent` skeleton; the versioned **base literacy** module | 🗓️ Planned |
| AG1 | Authoring + seal/pull + catalog: `agents/*.md` → `AgentTypeSnapshot` (persona → body blob, frontmatter → envelope); `orun agent import`, `orun agent pull <type>@<hash>` (set-difference via `objremote`); **catalog projection** (`AgentType` entity + edges: owner, `mayAffect`) | 🗓️ Planned |
| AG2 | The delegation runtime: `AgentDriver` seam (Claude Code first, headless stream-JSON); brief assembler (agent-type + task contract + `catalog affected` + base literacy, sealed); MCP manager + tool policy; append-only session event log; `orun agent run` (headless one-shot) | 🗓️ Planned |
| AG3 | TUI **Agent mode**: a 6th `internal/tui` mode — transcript, the sealed brief, the live affected subgraph, tool approvals, PR link; fully local, no cloud; `orun agent` opens it | 🗓️ Planned |
| AG4 | Driver conformance + session seal: the `AgentDriver` stdio protocol + a conformance oracle (a stub driver passes the full lifecycle); session seal → `AgentSessionSnapshot` + `orun agent replay <session>@<hash>` | 🗓️ Planned |

The cloud-attach entrypoint (`orun agent serve`) is specified here (design §7)
but *lands* with cloud **AG6** (it needs the session-token + DO relay from the
paired epic).

## Scope boundary

| In scope (orun) | Out of scope (→ elsewhere) |
|---|---|
| The agent runtime in the binary (`internal/agent`, `internal/agent/driver`, `internal/agenttype`); the `AgentType`/`AgentSession` object kinds + seal/pull/catalog projection; base literacy; the brief assembler; MCP wiring + tool policy enforcement; the TUI Agent mode; `orun agent run`/`pull`/`import`/`replay`; the `orun agent serve` cloud-attach protocol; the driver conformance oracle | The sandbox provider + lifecycle (Daytona), session identity/tokens, the per-session DO relay, the console Agents tab, design/dispatch UI, metering — all `orun-cloud/specs/epics/saas-agents/` (**AG5–AG11**); the work mutators + MCP tool *definitions* (`orun-work`, WP5); secret storage/resolve internals (`orun-secrets`); tenant-resource convergence (component `08`) |

## Relationship to existing work

- **`orun-object-model`** — the substrate. Agent types and sessions are new
  node kinds in the *same* graph, addressed the *same* way, synced by the
  *same* `objremote` set-difference push. No new persistence stack.
- **`orun-work`** — the task source and the honesty backbone. The runtime
  consumes `spec pull` briefs and the four agent tools; dispatch is the work
  plane's `assign`; the "no status write" invariant is inherited, not
  re-implemented. WP5 (the orun MCP) is the runtime's hands.
- **`orun-cloud/specs/epics/saas-agents` (AG5–AG11)** — the other half:
  everything that makes the runtime *hosted and governed*. The seam is
  `orun agent serve` (this spec) ↔ the session DO + token (that spec).
- **`agents/orchestrator.md`** — the existing convention this generalizes:
  today one hand-authored orchestrator character; after AG1, every
  `agents/*.md` is a sealed, catalog-visible, version-pinnable agent type.
