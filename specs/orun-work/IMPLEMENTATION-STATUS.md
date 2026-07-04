# Implementation Status (as-built)

> As-built ≠ intent. This file records what has actually shipped, kept
> distinct from the design/plan docs. Each milestone links the PR(s) that
> landed it. (Ironically, this hand-edited table is exactly what the epic
> exists to replace — it retires at the WP1 dogfood gate, design §6.4.)

## Milestones

| ID | Milestone | Status |
|----|-----------|--------|
| WP0 | Import + the derived read (dogfood zero) | 🏗️ In progress — substrate landed both repos; read surface rides WP1's first PR |
| WP1 | Coordination + the board | 🗓️ Not started |
| WP2 | Observation ingestion: PRs + drift | 🗓️ Not started |
| WP3 | Gates → Done, overlay → Released | 🗓️ Not started |
| WP4 | Sealing + `orun spec pull` | 🗓️ Not started |
| WP5 | The orun MCP | 🗓️ Not started |

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

**Still open for WP0/WP1:** the api-edge query surface + read-only console
list (re-sliced into WP1's first PR), and the import apply path (the CLI
refuses non-`--dry-run` until it exists).
