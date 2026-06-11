# Agents & the orun MCP

> Humans and agents are principals over one write path. The orun MCP is the
> *only* agent surface: reads serve the catalog, the change engine, and frozen
> spec snapshots; writes go through the same Worker mutators as the SaaS UI, so
> policy, gates, and attribution are enforced in one place (WD-3, WD-10). The
> Agents *section* (dispatch, fleet view) is out of scope for this epic; this
> doc fixes the rails that make it a feature flip later (§5).

## 1. Principals

- Every actor is a `Principal` (`data-model.md` §6): `type: human | agent`.
  Assignment, permissions, and attribution are uniform — "assign ORN-142 to
  @claude-agent" is the same mutation as assigning a human.
- An agent principal MUST name an `owner` (a responsible human/team). Agent
  events render with distinct provenance in every feed from day one
  (invariant 4) — the Agents section inherits a complete audit trail instead of
  retrofitting one.
- Authentication rides the backend's existing stack
  (`internal/remotestate/auth.go`): humans via GitHub OAuth sessions, agents via
  scoped tokens minted per principal (CI agents via OIDC, mirroring the
  `orun-secrets` identity model).

## 2. The MCP server

A thin Go server over libraries that already exist — it adds no semantics of
its own (the moment it does, the CLI/UI/MCP views diverge):

| Concern | Backed by |
|---|---|
| catalog reads | `internal/objcatalog` |
| change/blast-radius | `internal/affected` |
| spec snapshots | `internal/objremote` pull (set-difference) |
| work reads/writes | the Worker mutator + query API (one write path) |

## 3. Tool surface (v1)

**Read — the agent's situational awareness:**

| Tool | Returns |
|---|---|
| `catalog_get_component(key)` | the resolved manifest: owner, deps, lifecycle, environments |
| `catalog_affected(base, head)` | the `affected` engine result: directly-changed / dependents / selection |
| `catalog_graph(kind?)` | typed relation edges (work edges included) |
| `spec_get(epicKey, hash?)` | a `SpecSnapshot` — frozen at `hash` if given, else latest sealed |
| `work_query(filter)` | tasks/epics by status, assignee, component, cycle |
| `work_get(key)` | one item: envelope + contract + links + recent events |

**Write — through the mutators, policy-checked, attributed:**

| Tool | Effect |
|---|---|
| `task_update_status(key, status, note?)` | a `status_changed` event; subject to guardrails (§4) |
| `task_comment(key, body)` | a `comment_added` event |
| `task_link(key, type, target)` | `link_added` (e.g. `implementedBy` a PR the agent opened) |
| `task_create(epicKey, title, contract?)` | new task under an epic (e.g. discovered follow-up work) |
| `contract_propose(key, contract)` | a `contract_edited` event flagged for human review when the actor is an agent |

Every write carries the agent principal; the mutator stamps
`actor: {type: agent, id, via: mcp}`. There is no MCP-only bypass: a tool call
and a keyboard shortcut produce indistinguishable events except for provenance.

## 4. Guardrails (enforced in the mutator, not the client)

- An agent MUST NOT move a task to **Done**: Done is automation-only (gates
  verified) or human-overridden. Agents move work to `in_review` and attach the
  PR; the bridge does the rest.
- An agent MUST NOT edit another principal's comments, delete events (nobody
  can — append-only), or touch items outside projects its token scopes.
- Contract edits by agents are `contract_propose` — applied but flagged for
  human acknowledgement (a triage-inbox item), so an agent cannot quietly
  redefine its own definition of done.
- Rate/size limits per principal; rejections return structured verdicts the
  agent can reason about (same shape as optimistic-mutation rejections,
  design §7).

## 5. Later: the Agents section (out of scope, designed-for)

Dispatch is **assignment**: assigning a task to an agent principal (given a
complete contract — the `agentReady` badge) triggers, in the later epic:

1. `orun spec pull <epic>@<hash>` — the frozen brief + the component subgraph
   it touches;
2. a worktree + the coding CLI of choice (Claude Code or others), with the
   contract as the task brief and the MCP as hands;
3. the agent implements, opens a PR, links it (`task_link`), moves to
   `in_review`;
4. the existing bridge takes over: gates → Done, deployment → Released —
   identical to a human's PR.

Nothing in that flow needs new data model, new write paths, or new permissions:
W0–W5 already shipped principals, contracts, frozen pulls, the MCP, and the
bridge. The Agents section is UI (fleet view, dispatch button, agent activity)
over rails this spec lays — which is the point of laying them now.
