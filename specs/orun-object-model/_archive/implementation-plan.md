# Implementation Plan

> Milestone-based, not a rigid task list. Each milestone states **goal**,
> **dependencies**, **suggested PR scope**, **done when**, and **design refs**.
> Agents may split or merge milestones across PRs while keeping each PR
> reviewable and dependencies respected. Sequence is chosen so the contract
> layers (M1–M2) freeze early and everything above is additive until the M12
> cutover.

```
M0  Foundation ──► M1 Object store ──► M2 Refs ──► M3 Node schemas ──► M4 Node writer (plan)
                                                                          │
                                       M5 Resolve memo + tolerant walk ◄──┘
                                       M6 Working view + porcelain
                                       M7 Runner (working tree + seal + native job/step) [flagged]
                                       M8 Read rewire (status/logs/describe/get) + objindex
                                       M9 GC + retention
                                       M10 Migration (orun migrate)
                                       M11 Remote substitution + consumer seam (TUI/SaaS)
                                       M12 Cutover: flip default, delete legacy (parity matrix green)
                                       M13 E2E + property gates + disk-size assertion
```

---

## M0 — Foundation
**Goal:** unblock everything; no production behavior change.
- Add deps: `github.com/klauspost/compress/zstd` (zstd), keep `oklog/ulid/v2`.
- `internal/testfx/objfs`: temp object-store workspace, `AssertObject`,
  `ReadNode[T]`, golden-id helpers.
- `clock.Clock` helper (if not already shared) + injectable ULID generator.
- Lint/grep gate banning `json.Marshal` of records outside `nodes` and banning
  object-path construction outside `objectstore`.
- `Makefile` target `test-object-model` (initially the testfx package only).

**Deps:** none. **PR scope:** 1 PR. **Done when:** build+test green; objfs has
unit tests; gate script runs in CI. **Design:** `claude-goals.md`, `object-store.md` §3.

## M1 — `internal/objectstore` (L0)
**Goal:** the frozen object-store contract + local/mem drivers.
- `object.go` — `ObjectID`, `Kind`, framed serialization, canonical tree encode.
- `hash.go` — sha256, pluggable algo.
- `canonical.go` — `CanonicalEncode` (moved/generalized from catalogmodel).
- `store.go` — `ObjectStore` interface + errors.
- `local.go` — `LocalStore`: `PutBlob`/`PutTree`/`Get`/`GetTree`/`Has`/`Walk`/
  `Iterate`/`Delete`; zstd; atomic temp+fsync+rename; read-time hash verify.
- `mem.go` — `MemStore`.

**Deps:** M0. **PR scope:** 2–3 PRs (interface+blob; tree+walk; mem+property
tests). **Done when:** ≥95% coverage; property tests for *idempotent put*,
*content integrity*, *tree Merkle dedup*, *concurrent identical put*. **Design:**
`object-store.md` §1–5, §8.

## M2 — `internal/objectstore/refstore` (L2)
**Goal:** mutable pointers with CAS.
- `Ref` record, `RefStore` interface, local driver (atomic write + per-ref lock
  CAS), `List`/`Delete`.
**Deps:** M1. **PR scope:** 1 PR. **Done when:** ≥95% coverage; CAS property test
(N=100 concurrent updaters, exactly one winner per round); publish-ordering test
(ref never resolves to absent object). **Design:** `object-store.md` §6,
`identity-and-keys.md` §8.

## M3 — `internal/nodes` (L1 schemas)
**Goal:** every node schema with marshal/validate/tree-assembly + edge accessors.
- Record structs for all kinds (`data-model.md`), `CanonicalEncode` round-trips,
  validators (componentKey regex, status enums, no-self-id check), and
  tree-assembly helpers (`AssembleCatalogTree`, `AssembleRevisionTree`,
  `AssembleExecutionTree`).
- Identity helpers: `RevisionID(plan, catalogId)`, `CatalogID(tree)`, etc., that
  delegate hashing to `objectstore`.
**Deps:** M1. **PR scope:** 2 PRs (schemas+validate; tree-assembly+identity).
**Done when:** ≥90% coverage; property test: identical inputs ⇒ identical id;
no-self-id invariant enforced; round-trip canonical equality. **Design:**
`data-model.md`, `identity-and-keys.md` §3–§6.

## M4 — `internal/nodewriter` (plan path)
**Goal:** persist a plan as the content spine with Has-gated reuse.
- `WriteSource`, `WriteCatalog` (from a resolved view), `WriteRevision`
  (Has-gated dedup), `RecordTrigger`; ref moves via `refstore`.
- Wire `orun plan` onto it (replaces `revision.WriteRevision` +
  `catalog_plan_resolve` persistence; resolver *inputs* reused).
- Closes parity rows 1, 2.
**Deps:** M2, M3. **PR scope:** 2 PRs (writer; CLI wire). **Done when:** `orun
plan` writes objects + refs; revision dedup proven (two identical plans → one
revision id, one trigger each); ≥90% coverage. **Design:** `design.md` §2–§3,
`identity-and-keys.md` §10.

## M5 — Resolve memoization + tolerant-strict walk
**Goal:** make the walk cheap and total.
- `cache/resolve/<srcId>-rv<n>.json` memo; early cutoff on catalog id match.
- Degenerate `local-nogit` source + empty catalog as valid terminals.
- `--strict` / `--no-catalog` semantics.
**Deps:** M4. **PR scope:** 1–2 PRs. **Done when:** clean-tree re-plan skips the
resolver (asserted); non-git workspace produces valid nodes; strict/tolerant
matrix tested. **Design:** `identity-and-keys.md` §7, `design.md` §3.

## M6 — Working view (L4) + porcelain
**Goal:** inspectability.
- `internal/workingview`: materialize `.orun/current/` from a node closure.
- Porcelain: `orun cat|show|log|ls-tree|rev-parse|checkout|fsck|reindex`.
**Deps:** M3, M4. **PR scope:** 2–3 PRs. **Done when:** `fsck` verifies a fresh
store; `checkout` materializes readable JSON; porcelain has command tests.
**Design:** `cli-surface.md` §2, `design.md` §2.5.

## M7 — Runner: working tree + seal + native job/step  [behind `ORUN_OBJECT_RUNNER`]
**Goal:** the runner writes the model natively; no bridge.
- `.orun/run/<execId>/` working tree; native JobRun/Attempt/Step writes;
  heartbeat via live ref; seal-on-terminal → execution Merkle root + ref move;
  crash recovery.
- Closes parity rows 3, 4, 5, 6, 7, 12, 15(local).
**Deps:** M2, M3, M4. **PR scope:** 2–3 PRs (working tree+writes; seal; crash
recovery). **Done when:** flagged `orun run` executes end-to-end, seals, and
`orun status`/`logs` read it; property test: `executionKey` monotonicity; seal
idempotence; crash-mid-run recovers to a valid state. **Design:**
`runner-integration.md` §1–§3, §6.

## M8 — Read rewire + `internal/objindex` (L3)
**Goal:** all read commands + reverse lookups on the model.
- `objindex`: build/rebuild component/execution/source/catalog indexes from
  refs+objects; resolvers with graph-walk fallback on index miss.
- Rewire `status`, `logs`, `describe`, `get plans`, `catalog *`, and
  `runbundle` reads onto `nodes`/`objindex`.
- Closes parity rows 8, 9, 10, 11, 16.
**Deps:** M4, M7. **PR scope:** 3–4 PRs. **Done when:** every read command works
with no legacy scan; `reindex` reproduces byte-identical indexes; ≥90% coverage.
**Design:** `design.md` §2.4, `cli-surface.md` §1.

## M9 — GC + retention
**Goal:** reclaim disk safely.
- Mark-sweep from refs + retention policy; grace window; gc-fence vs seal;
  `orun gc` flags.
- Closes parity row 13.
**Deps:** M1, M2, M7. **PR scope:** 1–2 PRs. **Done when:** GC-safety property
test (never removes reachable/recent; interruptible); disk reclaimed on a churn
corpus; `gc --dry-run` accurate. **Design:** `object-store.md` §7.

## M10 — `orun migrate`
**Goal:** ingest legacy `.orun/` additively.
- Ingest plans + executions → objects; link by checksum; orphan bucket; ref
  moves; `--dry-run`; `--prune-legacy` (opt-in, post-fsck).
- Closes parity row 14.
**Deps:** M4, M7. **PR scope:** 1–2 PRs. **Done when:** idempotent (two runs →
same ids); dry-run stable; fsck green post-migrate. **Design:**
`compatibility-and-migration.md`.

## M11 — Remote substitution + consumer seam (TUI/SaaS)
**Goal:** the same model over a remote; consumers read/start via one seam.
- `internal/objremote`: `RemoteStore` (file:// driver in scope; R2/S3 adapter
  seam), `Push`/`Pull`/`Sync` (set-difference), remote refs (CAS via conditional
  writes).
- `ModelReader`/`RunStarter` seam; point TUI services + cockpit at it; start-run
  through the shared walk with `actor:"tui"`.
- Closes parity rows 15(remote), 17, 18, 19.
**Deps:** M7, M8. **PR scope:** 3–4 PRs (remote store+sync; seam; TUI rewire).
**Done when:** push/pull E2E between two `file://` stores (dedup observed —
second push sends only the delta); TUI lists + starts runs via the seam against
local and remote. **Design:** `remote-and-consumers.md`.

## M12 — Cutover & legacy deletion
**Goal:** the new model is the default; legacy is gone.
- Flip `ORUN_OBJECT_RUNNER` default on; remove the flag.
- Verify **all 19 parity rows green**, then delete `internal/state`,
  `internal/statebackend/file`, `executionstate/bridge.go`, Phase 1/2
  dual-write writers, and the catalog-parent mirror.
**Deps:** M4–M11. **PR scope:** 2 PRs (flip+default; deletion). **Done when:**
repo builds with legacy removed; full suite green; no import of `internal/state`
remains (grep gate). **Design:** `runner-integration.md` §4,
`compatibility-and-migration.md` §1.

## M13 — E2E + property gates + disk assertion
**Goal:** lock in correctness + the efficiency win.
- `cmd/orun/object_model_e2e_test.go`: full `plan → run → status → logs →
  describe → push → pull` walk; dedup/reuse assertions; migrate walk.
- Aggregate property tests; `make test-object-model` coverage gate.
- **Disk-size assertion**: the object store for the E2E corpus is smaller than
  the Phase 2 layout for the same history (goal #4).
**Deps:** M12. **PR scope:** 1 PR. **Done when:** `go test -race ./...` green;
`make test-object-model` green; disk assertion passes. **Design:**
`test-plan.md`, `design.md` §5.

---

## Cross-cutting (every milestone)
- Doc comments on exported symbols; no panics in prod; `clock.Clock` not
  `time.Now()`; canonical JSON only; `errors.Is/As`; no secrets in logs.
- Each PR cites milestone id + design sections + closed parity rows.
- Coverage gates per `test-plan.md`. Disk-size never regresses (M13 guard).
