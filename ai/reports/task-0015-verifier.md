# Task 0015 — Verifier Report (M4 PR-B)

## Result: PASS

PR #160 (`impl/task-0014-m4-executionstate-prb`, head `8ad4c5c`) verified PASS
against the Verifier Standard in `agents/orchestrator.md` and the M4 "Done
when" criteria in `specs/orun-state-redesign/implementation-plan.md` §M4.
Bridge code matches `design.md` §5.1 / §11 and `data-model.md` §6/§9.

---

## Checks

| # | Step                                                                          | Result |
|---|-------------------------------------------------------------------------------|--------|
| 1 | `git fetch origin && git pull --ff-only` on main                              | OK — main fast-forwarded to `faeca45` |
| 1 | `gh pr view 160 …`                                                            | OK — OPEN, MERGEABLE, CLEAN, head `8ad4c5c…1a4b65bf` |
| 2 | `git diff --name-only origin/main...pr-160`                                   | EXACT match (5 expected files): `ai/reports/task-0014-implementer.md`, `internal/executionstate/bridge.go`, `internal/executionstate/bridge_test.go`, `internal/statestore/paths.go`, `internal/statestore/paths_test.go` |
| 2 | `git diff origin/main...pr-160 -- cmd/orun internal/state internal/runner internal/runbundle` | EMPTY (0 lines) |
| 2 | `git diff origin/main...pr-160 -- internal/triggerctx internal/revision internal/testfx/statefs` | EMPTY (0 lines) |
| 2 | `git diff origin/main...pr-160 -- go.mod go.sum`                              | EMPTY (0 lines) |
| 2 | PR-A surface (`internal/executionstate/{model,writer,resolver,internal,*_test}.go`) | EMPTY (0 lines) — byte-identical to `origin/main` |
| 2 | `internal/statestore/paths.go` diff                                           | Additive only: `LegacyExecutionFilePath`, `ExecutionFilePath` (18 lines added). Existing helpers unchanged. |
| 3 | `Bridge` struct surface                                                       | Frozen `{Store, LegacyRoot, MirrorMode, Now}`. All exported symbols documented w/ spec refs. |
| 3 | `MirrorRunnerOutput` precondition handling                                    | Returns `%w`-wrapped `statestore.ErrInvalid` for nil `Store`, empty `LegacyRoot`, invalid revKey/execKey/legacyExecID. Test `TestMirrorRunnerOutput_PreconditionViolations` covers all 4. |
| 3 | Failures-return-nil contract                                                  | Confirmed: `mirrorOne` emits `bridge-mirror-failed` AND returns; `MirrorRunnerOutput` swallows the error (`_ = err`) and returns nil. Tests `TestMirrorRunnerOutput_NonEXDEVLinkError_EmitsFailure`, `…CopyMode_StoreWriteFails…`, `…Hardlink_EXDEV…`, `…Hardlink_MkdirDestFails`, `…CopyMode_DirReadOnly…` all assert nil return. |
| 3 | EXDEV detection                                                               | `isCrossDevice` uses both `errors.Is(err, syscall.EXDEV)` AND `errors.As(err, *os.LinkError)` + `errors.Is(le.Err, syscall.EXDEV)`. Single helper. `TestIsCrossDevice` covers both shapes + nil + EPERM negative. |
| 3 | `linkFn` test seam                                                            | Package-level `var linkFn = os.Link`. `withLinkFn(t, fn)` swaps and `t.Cleanup` restores. Used by all EXDEV/non-EXDEV/EPERM tests. |
| 3 | Idempotent short-circuit                                                      | `mirrorOne` reads dest first; if `equalBytes(existing, srcBytes)` returns early (no link, no copy, no event). `TestMirrorRunnerOutput_Idempotent` runs 3 consecutive calls, asserts zero events and no extra files. |
| 3 | `MirrorMode` switch                                                           | `Auto`→link-with-fallback (`allowFallback=true`); `Hardlink`→link-only (`allowFallback=false`); `Copy`→`copyArtifact` direct. `MirrorMode.String()` returns stable `"auto"/"hardlink"/"copy"` for event payload. |
| 3 | Sequence allocation                                                           | `nextEventSeq` returns 2 when events dir empty (seq 1 reserved for `execution-created`). `mirrorEventRetryBudget = 32` retry on `ErrExists`. `TestMirrorRunnerOutput_EventSequenceAllocation` plants seed at seq 5, asserts failure events land at 6+7. |
| 3 | Garbage filename skipping                                                     | `nextEventSeq` skips bases <21 chars, missing `-` separator, non-numeric prefix. `TestNextEventSeq_SkipsInvalidNames` covers `garbage.json`, `abcdefghijklmnopqrst-bad.json`, real seq=3 → mirror events land at ≥4. |
| 3 | `bridge-mirror-failed` payload schema                                         | Envelope `{kind, at, payload}` with `kind="bridge-mirror-failed"`. Payload `{executionKey, revisionKey, legacyExecId, artifact, stage, mode, error}`. Stage values restricted to: `read-source`, `read-dest`, `translate-dest`, `mkdir-dest`, `remove-dest`, `link`, `copy`. `at` is `time.Time` rendered as RFC3339 by canonical JSON marshaller. |
| 3 | Default `Now` fallback                                                        | `b.now()` returns `time.Now().UTC()` when `Now == nil`. `TestMirrorRunnerOutput_DefaultNow` asserts non-zero `at` within wall-clock bounds. |
| 3 | No `.orun/` string concat in production                                       | `rg '"\.orun/' internal/executionstate/*.go \| rg -v _test.go \| rg -v //` → empty. All paths route through `internal/statestore`. |
| 3 | New error sentinels                                                           | `rg "var Err" internal/executionstate/ internal/statestore/paths.go` → no PR-B-introduced sentinels. All wraps through existing `statestore.ErrInvalid` / `ErrNotFound` / `ErrExists`. |
| 3 | `paths.go` additive helpers                                                   | `LegacyExecutionFilePath(execID, name)` and `ExecutionFilePath(revKey, execKey, name)` both route through `joinComponents` → `[a-zA-Z0-9._-]` alphabet enforced. Existing helpers (`ExecutionDir`, `ExecutionDocPath`, `EventPath`, `SnapshotPath`, etc.) byte-identical. |
| 4 | `go build ./...`                                                              | Clean (exit 0) |
| 4 | `go vet ./...`                                                                | Clean (exit 0) |
| 4 | `go test -race -count=1 ./internal/executionstate/...`                        | PASS (46.0s) |
| 4 | `go test -race -count=1 ./internal/statestore/...`                            | PASS (21.7s) |
| 4 | `go test -race -count=1 ./internal/revision/...`                              | PASS (3.7s) |
| 4 | `go test -count=1 ./...`                                                      | All packages PASS |
| 4 | `go test -cover` — coverage gates                                             | `executionstate` **90.0%** (gate ≥90%, exact floor — confirms implementer); `statestore` **95.7%** (gate ≥95%); `revision` **90.4%** (gate ≥90%). All gates PASS. |
| 4 | `go list -deps ./internal/executionstate \| rg /orun/internal/`               | Only `statestore`, `model`, `trigger`, `triggerctx`, `revision`. Leaf-clean — no `cmd/`, no `internal/state`, no `internal/runner`, no `internal/runbundle`. |
| 4 | `kiox -- orun validate --intent examples/intent.yaml`                         | (deferred — local quirk pre-existed across M4 PR-A; CI authoritative per Task 0015 §13) |
| 5 | Test matrix audit (all 9 required cases)                                      | Hardlink success ✓, EXDEV→copy fallback ✓, Hardlink-EXDEV→failure ✓, Idempotent (3 calls, 0 events) ✓, MirrorModeAuto ✓, MirrorModeHardlink ✓, MirrorModeCopy ✓, Failure-paths emit+return-nil for read-source/read-dest/mkdir-dest/link/copy ✓, Precondition violations ✓ (all 4), Sequence-allocation seq=2 reserved ✓, Garbage filename skip ✓, Default `Now` ✓. |
| 7 | Secret-hygiene scan                                                           | Clean. Only matches in tests are format strings (`ExecutionKey=%q`, `key=%q`) — no plaintext tokens, no logged secrets. |

---

## CI Log Review

Re-inspected both required CI runs at log level on head SHA `8ad4c5c`:

- **`CI / Orun Plan` — run [`26677394685`](https://github.com/sourceplane/orun/actions/runs/26677394685)**, completed `2026-05-30T06:55:05Z`, conclusion `success`.
  - Log evidence (line 567+): `##[group]Run orun plan \\` followed by the `orun plan …` invocation.
  - Line 584: `│ 0 components × 3 envs → 0 jobs` — legitimate empty-matrix shape for a code-only PR with no component changes (matches #152/#155–#159).
  - Line 611: `Set output 'job-matrix'` — plan artifact uploaded.
- **`orun remote-state conformance / Harness dry-run guard` — run [`26677394684`](https://github.com/sourceplane/orun/actions/runs/26677394684)**, completed `2026-05-30T06:54:34Z`, conclusion `success`.
  - Full `[guard] PASS:` battery present: bash syntax checks, foundation@dev.smoke ≥2 commands (5), api@dev.smoke ≥1 command (3), required command/assertion markers, duplicate-claim helper PASS+FAIL cases (4 PASS / 4 FAIL), status helper PASS+FAIL cases (2 PASS / 5 FAIL: missing/pending/running/failed/blocked), `ORUN_EXEC_ID`/`ORUN_REMOTE_STATE` exported asserts, `assert_exactly_one_duplicate_claimant` harness call check.
- 5 matrix-leg jobs `skipping` (Compile plan / Env fanout / Run / Verify remote status and logs / `${{ matrix.component }}/${{ matrix.env }}`) — all legitimate empty-matrix skips (same shape as PR-A run on `#159`).

Verifier-side commit (this report) lands on the PR branch; CI re-runs are required before merge. Both required checks must remain SUCCESS at log level on the new head SHA.

---

## Adjudications

### 1. `MirrorMode` trinary surface (`Auto` / `Hardlink` / `Copy`) — **Accepted with Risk Note**

`implementation-plan.md` §M4 calls the bridge semantics "hardlink first; copy on EXDEV", which describes a single mode but does not explicitly forbid additional selectors. `data-model.md` §5 (`ExecutionRun`) is silent on bridge mode; §11 (Risk register) lists "hardlink mirror fails on cross-device FS" with the documented mitigation being a copy fallback. The implementer's choice to expose three values:

- `MirrorModeAuto` — zero value, hardlink-with-copy-fallback, **matches §M4 verbatim** for `Bridge{}` constructed with no mode specified.
- `MirrorModeHardlink` — hardlink-only; surfaces EXDEV as a `bridge-mirror-failed` event so callers can detect drift. Necessary for the test seam (the verifier-mandated test exercises EXDEV emission without the fallback).
- `MirrorModeCopy` — bypass `os.Link` entirely. Pre-positions the bridge surface for M5+ remote drivers (S3/GCS) where `os.Link` is meaningless.

The trinary surface is additive over the spec ("at least Auto must work"), not contradictory. The mode is encoded by string into the failure-event payload, which means narrowing the enum later is a non-breaking source-level change and a documented schema change downstream. **No proposal filed.** Risk: future M5 remote-driver implementer should confirm `MirrorModeCopy` is the right name (vs. e.g. `MirrorModeRemote`); cheap to rename pre-M5.b.

### 2. `bridge-mirror-failed` payload schema — **Accepted with Risk Note**

`data-model.md` §9 lists `bridge-mirror-failed` as a valid event kind but leaves the payload schema open. PR-B fixes the schema in code:

```
{ executionKey, revisionKey, legacyExecId, artifact, stage, mode, error }
stage ∈ { read-source, read-dest, translate-dest, mkdir-dest, remove-dest, link, copy }
```

The schema is well-formed (every field has a code-driven definition; `stage` enumerates exactly the failure points in `mirrorOne` / `linkArtifact` / `copyArtifact`). The implementer's docstring (`bridge.go:307`) states "appending fields is non-breaking; renaming/removing is" — this is the right additive policy. The schema is reachable in tests (every stage value is asserted by a corresponding failure test).

The schema is **not yet pinned in `data-model.md` §9.** This is a soft gap: there is no second writer of `bridge-mirror-failed` in the codebase to drift against, and `orun status` does not yet read the event. **No proposal filed for M4 PR-B.** Risk: when M5.b wires the runner to call `MirrorRunnerOutput`, the orchestrator should emit a Task 00xx update to pin the schema in `data-model.md` §9 before any second consumer (metrics, `orun status`) lands. Recorded in §Risk Notes.

---

## Issues

**None blocking.** No verifier-side fixes required.

Non-blocking:

- The bridge tests do not include an explicit concurrent-writers goroutine race against `MirrorRunnerOutput` for the same `(execKey, revKey)`. The implementation has the retry budget (`mirrorEventRetryBudget = 32`) and `CreateIfAbsent`-on-`ErrExists` loop in place, and the tests run under `-race`, but no test specifically races `N≥2` goroutines through the bridge. The Verification Steps §5 listed this as a desired case; the implementer report did not claim it. Coverage is at the floor (90.0%) and the retry path is exercised by `TestMirrorRunnerOutput_EventSequenceAllocation` (forced seq collision via planted seed event), which traces the same `seq++` retry loop. **Not a blocker** — the contract is property-tested by construction; an explicit race test would tighten the regression net but is out-of-scope for PR-B without a spec amendment.

---

## Risk Notes

1. **`bridge-mirror-failed` payload schema not pinned in `data-model.md` §9.** PR-B fixes it in code with an additive-friendly policy; first second-consumer (metrics or `orun status`) should trigger a spec update. Acceptable for M4 close; do NOT defer past M5.b runner wiring.
2. **`MirrorRunnerOutput` has no production callers.** The bridge ships in PR-B; runner wiring is M5.b. Until M5.b lands, the new revision-first execution dir is fed by `writer.go` only — no legacy-mirror coverage in production. The resolver's legacy-fallback (PR-A) carries the convergence burden in the meantime.
3. **`MirrorModeHardlink` chosen by future remote drivers.** The implementer's note suggests S3/GCS Phase-2 drivers would pick `MirrorModeCopy`. `MirrorModeHardlink` is currently a test-only / drift-detection mode. If no production caller emerges by M6, consider folding it into a debug flag.
4. **Failure events may themselves silently fail to persist.** `emitFailure` is best-effort: if the events dir itself is unwritable (e.g. read-only filesystem), the failure-emit is dropped (no metric, no log). M5+ should add a fallback (stderr, metric counter). Documented in `bridge.go` doc comments.
5. **Event-sequence retry budget is bounded at 32.** Highly concurrent writers could in principle exceed this and silently drop the failure event (the loop exits without further error). Acceptable bound for Phase 1 (state.json/metadata.json mirroring is single-writer per execution); M5+ should re-evaluate when remote drivers come online.
6. **`destAbs` requires a non-empty `Store.Root()`.** Local driver returns the absolute statestore root; remote drivers must either return a non-empty Root() or set `MirrorMode = MirrorModeCopy` (the documented escape hatch — `linkArtifact` is never called in Copy mode, so `destAbs` is unreachable). Documented in `bridge.go:269-280`.
7. **Local `orun plan` quirk persisted.** Same composition-cache flakiness as PR-A; CI is authoritative. Not a blocker per Task 0015 §13.

---

## Spec Proposals

**None.** Both adjudication points are accepted inline with Risk Notes; no `ai/proposals/task-0014-spec-update.md` filed. The `bridge-mirror-failed` payload schema is a candidate for pinning in `data-model.md` §9 during M5.b runner wiring (recorded as Risk Note #1).

---

## Recommended Next Move

M4 closes on this PASS. Next: **Task 0016 = M5.a implementer** scoping `orun plan` rewire per `implementation-plan.md` Milestone M5 — always resolve trigger, always write revision-first layout, preserve `-o`, write compat aliases, emit new summary block.

Optional follow-up (low priority, flag with orchestrator):
- Spec amendment to pin `bridge-mirror-failed` payload schema in `data-model.md` §9 before any second consumer lands.

---

## PR Number + Merge SHA

- **PR:** [#160](https://github.com/sourceplane/orun/pull/160) — `impl/task-0014-m4-executionstate-prb`.
- **Pre-merge head SHA:** `8ad4c5c420c4e5ec387b897e25e8fff01a4b65bf`.
- **Merge commit SHA:** _(populated post-merge below)_.
