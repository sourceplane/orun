# Spec: orun-work (v2)

**orun gains a work plane built on one principle: author intent, derive state,
coordinate socially. Every other tracker is a ledger of human opinions about
the state of the world — people drag cards to simulate reality. orun owns the
world (the component graph, the diffs, the gates, the deployments), so the
work plane stores no opinions about anything the platform can observe:
lifecycle is a query, not a column.** The entire plane is two append-only logs
— what people *intend* (the coordination log) and what the world *did* (the
observation log) — and every read surface (board, lifecycle, blast radius,
drift inbox, epic progress, agent brief) is a pure function of them.

> **Extended by v4** (`specs/orun-work-v4/` + orun-cloud
> `specs/epics/orun-work-v4/`, cluster **WH**): the planning hierarchy —
> Initiative → Design → Epic → Milestone → Task, with an authored, human-only,
> content-addressed approval ladder on intent. Additive only: the logs, the
> fold, and the derived delivery ladder below are unchanged.
>
> **v2 supersedes the archived v1**
> (`specs/archive/orun-work-v1/`). v1 stored status and pointed automation at
> the column; v2 deletes the column. v1 spec'd Cloudflare D1 + per-project
> Durable Objects; v2 is native to the platform as it exists — Postgres
> (orun-cloud), the shipped service-catalog graph (SC1–SC8), workspace
> tenancy (`ws_`), and Teams RBAC. v1's proven invariants (append-only
> events, mandatory actor provenance, one write path, seal determinism)
> carry forward unchanged.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (v2) — replaces v1 wholesale; v1 archived + code removed** |
| Builds on | the object model (`specs/orun-object-model/`, shipped), the service-catalog entity + relation graph (`specs/orun-service-catalog/`, SC1–SC8 shipped), `internal/affected` + `internal/objcatalog`, the Deployment live plane (`internal/objread.ComponentDeployments`), native coordination v2 (the default execution-truth wire), orun-cloud Postgres + membership/Teams RBAC |
| Coordinates with | orun-cloud `specs/epics/orun-work/` (the cloud-half pointer epic); `saas-resources-runtime` (`liveObservation` feeds Released); `teams-ownership` (owner→team resolver feeds reviewer suggestion); `saas-integrations`/`saas-integration-tenancy` (the GitHub webhook → observation path) |
| Prior art | Linear (the feel bar: local-first store, optimistic apply, views as queries) — **but not its ontology**: Linear made stating opinions instant; orun makes most opinions unnecessary. The repo's own `specs/*/implementation-plan.md` milestone convention remains the seed of the task contract |
| apiVersion | `orun.io/v1` (`Spec`, `Task`, `CoordinationEvent`, `Observation`, `SpecSnapshot`) |
| Decisions locked | (WP-1) **three planes by truth source** — intent (authored, sealed content), fact (derived at read, never stored), coordination (the only hot mutable store); (WP-2) **two append-only logs** — coordination (human/agent/automation-authored, mandatory actor) and observation (world-authored facts); every read model is a rebuildable fold; (WP-3) **lifecycle is a derived query** — Draft/Ready/In Progress/In Review/Done/Released each owned by a named truth source; the only authored lifecycle facts are existence, cancellation, and *pins* (public, attributed overrides the platform continuously audits); (WP-4) **two nouns** — `Spec` (the grouping document of intent; the epic) and `Task` (the atom); initiatives and cycles are saved views, not entities; (WP-5) work kinds are **entities in the shipped service-catalog graph from day one** — no side link table; (WP-6) **one mutator surface** for UI/MCP/CLI; observations enter only through ingestion, never through mutators; (WP-7) tasks are **workspace-scoped** (`<PREFIX>-<seq>` per workspace); delivery topology is expressed by `affects`, never by the partition key; (WP-8) **principals are the platform's principals** (users, service principals, teams from the membership context) — no work-local identity; (WP-9) sync is **log-native**: the logs are the sync protocol (snapshot bootstrap + cursor replay over SSE/LISTEN-NOTIFY); no bespoke realtime transport; the mutation/verdict contract is the permanent engine-agnostic seam; (WP-10) agents write through the MCP only, may never author Done/pins, and their briefs are sealed `spec@hash` snapshots |
| Milestone prefix | **WP** (`WP0 → WP5`) |

## The one-paragraph thesis

A tracker's quality has always been measured by how pleasant it makes
maintaining the simulation — Linear won by making opinion-entry instant. orun
can skip the simulation: it already owns the change engine that maps any diff
to components, the execution truth of every run and gate, and the deployment
overlay that knows what is live. So the work plane splits by truth source.
*Intent* — spec docs and task contracts (`goal / affects / doneWhen / gates`)
— is authored, review-worthy, and seals content-addressed like everything else
orun authors. *Fact* — lifecycle position, PR links, gate results,
released-ness, blocked-ness, progress — is derived at read from the
observation log the world writes (webhooks, CI, the run stream, the deploy
overlay), in the exact pattern the catalog's Deployment live plane already
proves (CR-1: derived, never persisted). *Coordination* — assignment,
priority, ordering, comments, cancellation, pins — is the one small hot store:
an append-only event log with a mandatory actor on every event. Fold the two
logs and you get every pixel: the board, the drift inbox ("a merged PR no open
task claims"), epic progress that cannot lie, and **Released** — the status no
webhook tracker can have. The repo's own `specs/` tree imports as the first
project, so orun plans orun from the first week, and the first thing anyone
sees is their own stale status tables coming true on their own.

## Read order

1. **`design.md`** — the founding observation, the three planes, the two logs,
   the derived lifecycle ladder, sync-as-log-replay, decisions, invariants,
   what we refuse to build.
2. **`data-model.md`** — the four tables, the entity shapes, the contract, the
   coordination-event and observation vocabularies, the fold, sealed objects.
3. **`agents-and-mcp.md`** — principals (= platform principals), the MCP
   surface, guardrails, dispatch-as-assignment.
4. **`cli-surface.md`** — `orun work`, `orun spec pull`, `orun work import`.
5. **`implementation-plan.md`** — milestones **WP0 → WP5** (dogfood-first).
6. **`risks-and-open-questions.md`** — decision ledger, open questions,
   deferred register.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| The two-noun model (`Spec`, `Task`) + the task contract; the coordination log + mutator surface (orun-cloud Postgres); the observation log + ingestion (GitHub webhooks via the integrations tenancy, the native-coordination run stream, the deployment overlay feed); the lifecycle fold incl. **Released**; work kinds as catalog-graph entities; the drift inbox; sealing (`SpecSnapshot`) + `orun spec pull`; the `specs/` import; the MCP; the console contract (local-first store, snapshot + cursor replay, optimistic apply + verdicts) | The console UI *implementation* (orun-cloud owns pixels; this spec fixes the contract); a bespoke realtime transport (WP-9: log replay over SSE; revisit only on measured need); Initiative/Cycle *entities* (saved views only, WP-4); CRDT doc editing; external tracker adapters; notification delivery channels (reuse `notifications-worker`); agent dispatch UI (rails only, per `agents-and-mcp.md`) |

## What v2 deletes from v1 (and why)

| v1 | v2 | Why |
|---|---|---|
| Stored `work_status` column + automation writing to it | Lifecycle = `fold(observations, coordination)` at read | Nothing stored can rot; there is nothing to keep honest |
| Initiative/Epic/Task/Cycle entity hierarchy | `Spec` + `Task`; initiatives/cycles are saved views | Entities are forever, queries are cheap; ship the atom |
| `work_links` side table "until SC2 lands" | Entities + typed edges in the shipped catalog graph | SC1–SC8 landed; the deferred future is the present |
| D1 + per-project Durable Object + bespoke WebSocket protocol | Postgres logs; SSE/LISTEN-NOTIFY replay behind the verdict seam | The platform is Postgres; the log *is* the sync protocol |
| Work-local `prn_` principals table | Membership subjects (users, service principals, teams) | A second identity system is a future federation bug |
| Per-project task keys | Per-workspace keys; `affects` carries delivery topology | Planning topology must not mirror deploy topology |
| Triage/Backlog/Todo authored statuses | Readiness derived from the contract; priority is coordination | The old statuses smeared readiness (derived) into ordering (authored) |

## Convention over configuration (locked)

A title is enough for a task to exist. A contract makes it **Ready** — the
same predicate as agent-ready, one definition of "actionable" for humans and
agents. Everything after Ready is observed: a branch or draft PR moves it to
In Progress, an open PR to In Review, merge + gates verified green to Done,
the deployment overlay to Released. Blocked, progress, drift, and reviewer
suggestions are derived. The only authored lifecycle acts are creating,
canceling, and pinning — and a pin is rendered *next to* what the platform
observes, never instead of it.

## Out-of-band references

- Object graph + remote: `specs/orun-object-model/` (shipped; sealing and
  `spec pull` ride it).
- Entity + relation graph: `specs/orun-service-catalog/` (SC1–SC8 shipped;
  work kinds join it, WP-5).
- Change engine: `internal/affected` (blast radius, auto-claim, drift).
- Deployment live plane: `internal/objread.ComponentDeployments` (the CR-1
  derive-at-read pattern v2 generalizes; feeds Released with
  `saas-resources-runtime`).
- Execution truth: native coordination v2 (the default run/gate event wire).
- Cloud half: orun-cloud `specs/epics/orun-work/` — Postgres schema,
  ingestion workers, console contract, membership/Teams binding.
- Archived predecessor: `specs/archive/orun-work-v1/`.
