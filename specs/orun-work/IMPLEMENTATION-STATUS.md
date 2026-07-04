# Implementation Status (as-built)

> As-built ≠ intent. This file records what has actually shipped, kept
> distinct from the design/plan docs. Each milestone links the PR(s) that
> landed it. (Ironically, this hand-edited table is exactly what the epic
> exists to replace — it retires at the WP1 dogfood gate, design §6.4.)

## Milestones

| ID | Milestone | Status |
|----|-----------|--------|
| WP0 | Import + the derived read (dogfood zero) | ✅ Shipped — orun #454 (worklens oracle + import dry-run) + orun-cloud #318 (two-log substrate) + the WP1 surface PRs (query API, console list, import apply, `orun work import`/`list`) |
| WP1 | Coordination + the board | 🏗️ In progress — fold query API with evidence + mutator verdicts + read-only console Work page + import apply landed (orun-cloud WP1 PR; orun: remotestate work client, `orun work import` apply, `orun work list`); optimistic store + SSE replay + pin/comment UI pending |
| WP2 | Observation ingestion: PRs + drift | ✅ Shipped — orun-cloud #327: the webhook drain projects normalized scm.* PR/branch events into the fact log (same-tx, semantic dedupe, task-key parse); the `ci` producer endpoint carries affected sets; the WP0 claim join + drift inbox light up from live facts |
| WP3 | Gates → Done, overlay → Released | 🏗️ Mostly shipped — orun-cloud WP3 PR: run-stream gate verdicts from terminal job phases keyed to the run's git revision (P-3: execution truth, never GitHub statuses); the deploy-overlay → revision_live bridge built + tested; the In Review → Done → Released walk proven from facts. Remaining: the runtime call site awaits saas-resources-runtime |
| WP4 | Sealing + `orun spec pull` | ✅ Shipped — seal core + `orun spec pull <slug>[@sha256:…]` (pin verification, read-only view, --id-only), and the remote leg: `--push` stores the sealed blob in the object store and set-difference-syncs `refs/work/specs/<slug>/latest` to the org/project-routed remote (the catalog's push spine). Sealing deliberately stays in Go: one canonicalizer, one determinism contract |
| WP5 | The orun MCP | ✅ Shipped — `orun mcp serve` (stdio, dependency-free JSON-RPC 2.0): reads return the fold with evidence (`work_query`/`work_get`) and sealed intent-only briefs (`spec_get`); the write surface is exactly four tools through the cloud mutators (`task_create`/`task_comment`/`task_assign`/`contract_propose` — applied AND flagged for human review). No lifecycle write tool and no pin tool exist (WP-3/WP-10: the lie is unrepresentable), asserted by test |

## WP0 — as-built

**orun (`internal/worklens`, this repo):** the conformance oracle.

- The closed vocabularies (2 kinds, 9 coordination event kinds — deliberately
  no lifecycle-write kind, 6 observation kinds, 3 actor types), envelopes
  (`Spec`/`Task`), the contract with `Complete()` (= Ready = agent-ready),
  and write-time validation (mandatory actor, closed sets, agents-may-not-pin).
- **The fold** (`fold.go`): lifecycle as a derived query — claim join (key
  parse wins; component overlap claims only when unambiguous, else suggests),
  the observed ladder (branch→In Progress, PR→In Review, merge+gates
  verified→Done, overlay→Released; unknown-to-orun parks In Review), pins
  rendered beside truth with auto-expiry, the blocked flag, the drift
  standing query, spec progress.
- **Shared conformance fixtures** (`fixtures/conformance.json`, 16 cases)
  replayed byte-identical by the orun-cloud TypeScript fold (the fixture
  file is copied verbatim; both suites fail if they diverge).
- **`orun work import --dry-run`** (`cmd/orun/work.go`): specs tree →
  deterministic import plan (epic READMEs → Specs with content-addressed doc
  digests; `implementation-plan.md` milestones → Tasks with
  Goal/Deps/Done-when contracts; gates deliberately undeclared — P-7 honest
  degradation; **no lifecycle imported**, asserted by test). Golden fixture
  + a smoke parse of this repo's real `specs/` tree (orun-work itself maps
  to 6 milestone tasks).
- Package coverage ~90%.

**orun-cloud (`@saas/db/work` v2 + migration `560_work_foundation_v2`):**

- The `work` schema recreated as the two-log design: `work.events`
  (coordination log; CHECK-closed 9-kind vocabulary, typed-actor constraint,
  per-workspace seq), `work.observations` (fact log; CHECK-closed 6-kind
  vocabulary, versioned source, `dedupe_key` idempotency), `work.specs` /
  `work.tasks` (droppable fold caches), `work.sequences`. **No status column
  exists anywhere.** Workspace-scoped (WP-7), no project partition.
- `model.ts` — the TS fold mirroring the oracle (replays the shared
  fixtures), validation, vocabularies. `envelopes.ts` — cache rebuild from
  the coordination log alone (invariant 1). `MemoryWorkRepository` — the
  two-log design taken literally (no caches at all); `createWorkRepository`
  — Postgres, one-event-per-mutator in one tx, `rebuildCaches` as the
  executable invariant-1 proof.
- Proven in tests: one event per mutation; actor-less writes rejected; agent
  pins rejected in the mutator; no lifecycle mutator exists (asserted);
  PREFIX-n allocation per workspace; observation dedupe; end-to-end
  ready → in_review → done → released walk from the logs with a pin
  rendering beside truth and auto-expiring on catch-up.

## WP1 — as-built (in progress)

**orun-cloud:** the fold query API (`GET /v1/organizations/{org}/work` —
rungs WITH evidence, drift, suggestions, per-spec progress, log cursors),
the coordination-log events endpoint, the mutator routes with structured
verdicts (agent pin → 422 `verdict_rejected`; no set-status route exists),
the idempotent import apply (milestone-dep tokens rewrite to allocated
keys; every event `via: import`; NO lifecycle imported), `work.read`/
`work.write` policy actions, the SDK `WorkClient`, and the read-only
console Work page (rung badges with evidence, pinned-beside-truth,
blocked flags, drift inbox, claim suggestions).

**orun:** `internal/remotestate` work client (`ImportWork`,
`GetWorkSummary`), `orun work import` apply (scope/auth preamble shared
with catalog push), `orun work list` (evidence-bearing rungs in the
terminal).

**Still open for WP1:** the local-first console store (snapshot + cursor
replay over SSE/LISTEN-NOTIFY), optimistic apply, and the pin/comment UI —
then the dogfood gate (this table retires).

## WP5 — as-built

**orun (`internal/workmcp` + `orun mcp serve`):** a minimal, dependency-free
MCP server (newline-delimited JSON-RPC 2.0 over stdio) over the `WorkAPI`
seam (`remotestate.Client`). The tool surface is closed at 7:

- Reads: `work_query` (the fold summary with evidence), `work_get` (one
  task), `spec_get` (a client-side-sealed, content-addressed SpecSnapshot —
  fold output asserted absent from the sealed bytes).
- Writes: `task_create`, `task_comment`, `task_assign`, and
  `contract_propose` (contract applied through the mutators AND flagged
  with a review comment — an agent cannot quietly redefine its own
  definition of done). Tool failures return MCP `isError` results (verdicts
  the agent reasons about), never protocol faults.
- **No `task_update_status` and no pin tool exist** — asserted by test; the
  cloud mutator additionally rejects agent pins server-side (defense in
  depth, not client-side trust).

Remaining epic-wide: the resources-runtime call site for `revision_live`
(gated on saas-resources-runtime P2 — the bridge is built and tested), a
push-based SSE transport if the events-cursor poll ever misses the latency
bar (the seam is transport-agnostic by design), and `orun work import` of
both repos' spec trees against a live workspace (the dogfood gate that
retires this table).