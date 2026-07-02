# Agents & the orun MCP

> Agents are the second user, designed in, not bolted on. Principals are the
> platform's principals (WP-8): agents are membership service principals with
> a mandatory responsible owner. The orun MCP is the only agent surface;
> writes go through the same mutator surface as the console keyboard, so
> policy, verdicts, and attribution live in one place (WP-6, WP-10). The
> design goal is stated bluntly: **the less agents can assert, the less they
> can hallucinate into planning state** — an agent's whole implement-a-task
> loop authors exactly two coordination events; everything else it "reports"
> is observed off its PR like anyone else's.

## 1. Principals

- Every actor resolves to a membership subject: `usr_…` (human), `sp_…`
  (service principal — agents live here), `team_…`. No work-local identity
  table exists (WP-8).
- An agent service principal MUST name a responsible `owner` (a user or
  team) at registration. Agent events render with distinct provenance in
  every feed from day one (invariant 3).
- AuthZ rides the existing membership RBAC (`role_assignments`, team grants,
  effective-access with provenance); a work-plane write requires member on
  the workspace, same as any other plane. No work-specific permission system.
- Authentication: humans via existing sessions; agents via scoped tokens
  minted per service principal (CI agents via the existing OIDC path).

## 2. The MCP server

A thin server over surfaces that already exist — it adds no semantics of its
own (the moment it does, console/CLI/MCP diverge):

| Concern | Backed by |
|---|---|
| catalog reads | `internal/objcatalog` |
| change/blast radius | `internal/affected` |
| frozen briefs | `SpecSnapshot` pull via `internal/objremote` (set-difference) |
| work reads | the fold query API (same one the console consumes) |
| work writes | the coordination mutators (one surface, WP-6) |

## 3. Tool surface (v1)

**Read — situational awareness:**

| Tool | Returns |
|---|---|
| `catalog_get_component(key)` | resolved manifest: owner, deps, lifecycle, environments |
| `catalog_affected(base, head)` | the `affected` engine result |
| `catalog_graph(kind?)` | typed relation edges, work edges included |
| `spec_get(specKey, hash?)` | a `SpecSnapshot` — frozen at `hash` if given, else latest sealed |
| `work_query(filter)` | tasks/specs by derived lifecycle, assignee, component, label |
| `work_get(key)` | envelope + contract + derived lifecycle + derived edges + recent events/observations |

`work_query`/`work_get` return the *fold's* output with each rung's evidence
attached ("In Review because PR #412 open; gate `parity` red per run …") —
an agent reasons over evidence, not bare enums.

**Write — through the mutators, policy-checked, attributed:**

| Tool | Effect |
|---|---|
| `task_create(specKey?, title, contract?)` | `item_created` (discovered follow-up work) |
| `task_comment(key, body)` | `comment_added` |
| `task_assign(key, subject)` | `assigned` (self-assignment for claiming work) |
| `contract_propose(key, contract)` | `contract_edited` flagged for human acknowledgement |

That is the entire agent write surface. There is deliberately **no**
`task_update_status`: lifecycle is not authored by anyone (WP-3), so the tool
cannot exist — the category of "agent lies about progress" is unrepresentable.
An agent moves work forward by doing the work: its branch, PR, and gates are
observed like a human's.

## 4. Guardrails (enforced in the mutator, not the client)

- Agents MUST NOT author `pinned` or `canceled`-of-others'-work events
  (WP-10, invariant 3). Humans pin; the world moves rungs.
- `contract_propose` applies but flags for human acknowledgement (an inbox
  item) — an agent cannot quietly redefine its own definition of done.
- Scope: an agent token acts within its workspace grants only; rate/size
  limits per principal; rejections return the same structured verdicts as
  optimistic-mutation rejections (one shape, design §7).
- Nobody — human or agent — can delete or edit events (append-only,
  invariant 2).

## 5. Dispatch is assignment (rails now, section later)

Assigning a task to an agent service principal — gated on **Ready** (the
contract-completeness predicate, §data-model 3) — triggers, in the later
Agents epic:

1. `orun spec pull <spec>@<hash>` — the frozen brief + the `affects`
   component subgraph;
2. a worktree + the coding agent of choice, contract as the brief, MCP as
   hands;
3. the agent implements, opens a PR (branch name carries the task key),
   comments;
4. the observation log takes over: PR opened → In Review, gates green +
   merge → Done, overlay → Released — indistinguishable from a human's PR.

No new model, no new write path, no new permissions. The Agents section is
UI over rails this spec lays.
