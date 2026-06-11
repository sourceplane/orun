# CLI Surface

> The work plane is SaaS-first; the CLI surface is deliberately small and
> read-leaning: pull frozen specs for agents and humans, peek at work without
> leaving the terminal. Mutation from the CLI goes through the same backend
> mutators (one write path) and stays minimal in v1 — the board lives in the
> SaaS. Output follows the cockpit conventions (`internal/cockpit`): human
> rendering by default, `--json` falls through the same viewmodels.

## 1. `orun spec pull`

Fetch a sealed `SpecSnapshot` (and its object closure) into the local store —
the agent/human handshake for "implement against exactly this".

```sh
# latest sealed snapshot of an epic
orun spec pull acme/platform/epics/orun-work

# frozen at a specific snapshot id (what a dispatcher hands an agent)
orun spec pull acme/platform/epics/orun-work@sha256:9f86d0…

# print the resolved snapshot id only (for scripting/dispatch)
orun spec pull acme/platform/epics/orun-work --quiet --id-only
```

Behavior:

- Set-difference pull via the existing remote walk (`internal/objremote`):
  objects the local store already has are never re-fetched.
- Materializes a working view under `.orun/specs/<epic-slug>/` — the epic doc,
  task contracts, and the `affects` component subgraph summary — read-only
  (WD-7; editing a pulled snapshot is meaningless by construction).
- `--catalog` additionally pulls the `CatalogSnapshot` the spec's component
  keys resolved against, so `affects` keys resolve identically offline.
- Exit codes: `0` ok; `4` unknown epic/ref; `5` auth/scope failure; `7` remote
  unreachable (structured under `--json`).

## 2. `orun work`

```sh
orun work list --epic orun-work --status in_progress,-released --assignee @me
orun work view ORN-142            # envelope + contract + links + recent events
orun work status ORN-142 in_review --note "PR up"   # minimal mutation set, v1
```

- `list`/`view` read the backend query API; offline they degrade to the last
  pulled snapshots with a staleness banner.
- Mutation is deliberately limited to `status` + `comment` in v1; everything
  else is SaaS/MCP. CLI mutations are events like any other
  (`actor.via: cli`).
- Task keys resolve in project context (`ORN-142`) or fully qualified
  (`acme/platform/ORN-142`).

## 3. `orun work import` (dogfood path, W6)

```sh
orun work import specs/ --project sourceplane/orun --dry-run
```

Parses the repo's spec tree (epic READMEs, `implementation-plan.md` milestones,
`IMPLEMENTATION-STATUS.md` tables) into epics + tasks with contracts
(`Goal/Deps/Done when/Design refs` → `contract.*`), emits `imported` events,
and seals an initial `SpecSnapshot` per epic. `--dry-run` prints the mapping
without writing. Lossless: the source markdown becomes the epic `doc` verbatim
(Q-4).

## 4. Non-goals (CLI)

No board/TUI rendering of work in v1 (the cockpit may grow a work pane later —
deferred, L-3); no contract editing from the CLI; no offline mutation queue
(rejected mutations need the interactive verdict loop the SaaS/MCP provide).
