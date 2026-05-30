# Task 0009 — Verifier Report (M3 PR-A)

## Result: PASS

## Checks

| Step | Command | Outcome |
| --- | --- | --- |
| 1. Repo state | `git fetch origin && git status -s && git log --oneline -5 origin/main` | Clean (orchestrator scratchpad only). `origin/main` HEAD = `cd8b3e8` (Task 0005 / PR #156). |
| 1. PR sanity | `gh pr view 157 --json …` | `state=OPEN`, `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`, head = `500218c0bcbb0a671d528453b24043ebc8da4d53`. Both required CheckRuns SUCCESS. Five SKIPPED matrix legs (legitimate empty-matrix). |
| 2. Diff stat | `git diff --stat origin/main...pr-157` | 13 files, +2292 / −0. `Makefile` + 4 prod files + 3 test files in `internal/revision/`, plus 5 `ai/` task/report files. |
| 2. Overreach scan (must be empty) | `git diff origin/main...pr-157 -- cmd/orun internal/state internal/runner internal/runbundle` | 0 lines. ✅ |
| 2. Frozen-surface scan (must be empty) | `git diff origin/main...pr-157 -- internal/triggerctx internal/statestore internal/testfx/statefs` | 0 lines. ✅ M0/M1/M2 byte-identical to `origin/main`. |
| 2. PR-B leakage (must NOT exist) | `git ls-tree -r pr-157 --name-only \| grep -E "internal/revision/(manifest\|resolver)\.go"` | absent. ✅ |
| 3. Spec conformance — `model.go` | Hand-read vs `data-model.md` §3 | `PlanRevision` field order + JSON tags byte-match spec; `RevSummary` matches; `StateStoreVersion` matches §1. ✅ |
| 3. Spec conformance — `keys.go` | Hand-read vs `data-model.md` §3.1 + `state-store.md` §3 | `revisionKeyPattern` = `^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$` exactly. `ResolveCollision` uses `CreateIfAbsent` exclusively; cap = 99; on exhaustion wraps `ErrConflict`. `PlanShortHash` strips `sha256:` prefix and validates 8 hex chars; errors wrap `ErrInvalid`. ✅ |
| 3. Spec conformance — `writer.go` | Hand-read vs `design.md` §5.1 + `cli-surface.md` §1.2 + `state-store.md` §3/§6 | Seven-step ordered list with explicit step-0 reservation (claim-first); body writes 1→3 (trigger.json, revision.json, plan.json verbatim); refs 4→5 (CAS-with-bootstrap on `latest-revision.json`, unconditional trigger refs); index finalize 6 (overwrites reservation with real `RevisionIndexEntry`); version doc 7 idempotent. CAS retry budget = 5 (bounded). All errors wrap one of the four statestore sentinels. No new sentinels. ✅ |
| 3. Spec conformance — `version.go` | Hand-read vs `data-model.md` §1 | `marshalCanonicalJSON` uses `SetEscapeHTML(false)` + `SetIndent("", "  ")` and emits trailing `\n` (encoder default). `stateStoreVersionPath()` returns `"version.json"` (statestore is rooted at `.orun`). ✅ |
| 3. Path helpers | `grep -n "fmt.Sprintf.*\"/" internal/revision/*.go` | No string concatenation for paths in writer/keys; all writes go through `statestore.{TriggerPath,RevisionDocPath,PlanPath,RevisionIndexPath,LatestRevisionRefPath,WriteTriggerRef}`. The single local helper `stateStoreVersionPath()` returns a constant. ✅ |
| 3. Doc comments | Read every exported symbol | `PlanRevision`, `RevSummary`, `StateStoreVersion`, `APIVersion`, `KindName`, `StateStoreVersionKind`, `StateStoreLayoutRevisionFirst`, `StateStoreVersionCurrent`, `CollisionSuffixCap`, `PlanShortHash`, `RevisionKey`, `ValidateRevisionKey`, `ResolveCollision`, `Config`, `WithCompatibilityWrites`, `WriteRevision`, `EnsureStateStoreVersion` — all carry doc comments. ✅ |
| 3. Compat seam | Read `Config.CompatibilityWrites` + `writeCompatibilityMirror` | Default-true via `resolveDefaults` (with `compatibilityWritesSet` to disambiguate explicit-false from unset). `writeCompatibilityMirror` body is a no-op `return nil` carrying a `// TODO(m5):` comment naming the legacy mirror paths. NOT a real body write — confirmed against constraint #3. ✅ |
| 4. `go build ./...` | exit 0 | ✅ |
| 4. `go vet ./...` | exit 0 | ✅ |
| 4. `go test -race -count=1 ./internal/revision/...` | `ok 2.473s` | ✅ |
| 4. `go test -race -count=1 ./internal/statestore/...` | `ok 17.565s` | ✅ |
| 4. `go test -race -count=1 ./internal/triggerctx/...` | `ok 5.298s` | ✅ |
| 4. `make test-state-redesign` | `revision: measured 93.3 %` (≥90 %), `statestore: measured 96.1 %` (≥95 %) | ✅ |
| 4. `go test -cover` second-source | `revision 93.3 %`, `statestore 96.1 %` | ✅ |
| 4. Leaf-clean import audit | `go list -f '{{ join .Imports "\n" }}' ./internal/revision \| grep "/orun/internal/"` | Direct internal imports = `internal/statestore` + `internal/triggerctx` exactly. Transitive `model`/`trigger` come through `triggerctx`. No `cmd/`, no `internal/state`, no `internal/runner`, no `internal/runbundle`. ✅ |
| 5. CI / Orun Plan run `26672937657` | `gh run view 26672937657 --log` | Real `orun plan --artifact github …` invocation against `examples/intent.yaml`; legitimate empty-matrix shape `0 components × 3 envs → 0 jobs`; plan artifact uploaded (`orun.v1.gh-26672937657-1-c79e82401b64.plan.sha256-c79e8.created`, 2276 bytes). ✅ |
| 5. Harness dry-run guard run `26672937641` | `gh run view 26672937641 --log` | Full `[guard] PASS:` battery present: bash syntax, command-count thresholds (foundation@dev.smoke ≥2 / api@dev.smoke ≥1), command/assertion markers, duplicate-claim helper PASS×2 + FAIL×4, status helper PASS×2 + FAIL×5, exported env asserts (`ORUN_EXEC_ID`, `ORUN_REMOTE_STATE`, `assert_exactly_one_duplicate_claimant`). ✅ |
| 5. Skipped matrix legs | Inspected via `gh pr view 157 statusCheckRollup` | Five SKIPPED legs (`${{ matrix.component }}/${{ matrix.env }}`, `Compile plan`, `Run: ${{ matrix.job }}`, `Env fanout: ${{ matrix.env_name }}`, `Verify remote status and logs`) — same legitimate-empty-matrix shape as #152/#155/#156. Not silently-failed required checks. ✅ |
| 6. Secret hygiene | `grep -rin -E "(token\|password\|secret\|key=)" internal/revision/` | Only hit is `latest.RevisionKey` in a test error message. No plaintext tokens, no secret material logged. ✅ |
| 6. Local kiox plan/run | `kiox -- orun validate / plan --changed / run --dry-run` | Skipped; the local composition-cache quirk (`stack.yaml at ~/.orun/cache/compositions/c41fc08… has no spec.compositions`) is the documented environment artifact carried since Task 0001. CI is authoritative per task constraint #7. |

## CI Log Review

`CI / Orun Plan` (run `26672937657`, job `78619489862`) executed `orun plan --artifact github` against `examples/intent.yaml`, produced the legitimate `0 components × 3 envs → 0 jobs` empty-matrix line, and uploaded the plan artifact (`✓ uploaded plan artifact: orun.v1.gh-26672937657-1-c79e82401b64.plan.sha256-c79e8.created (2276 bytes)`). 44 s wall-clock. SUCCESS.

`orun remote-state conformance / Harness dry-run guard` (run `26672937641`, job `78619489786`) emitted the full `[guard] PASS:` battery — every helper PASS-case + FAIL-case + every exported env assert. 18 s wall-clock. SUCCESS.

The five SKIPPED legs are legitimate empty-matrix shape, identical to PRs #152/#155/#156. Required-check coverage is exactly the two SUCCESS entries above.

## Claim-First Adjudication: ACCEPT-AND-DOCUMENT

`cli-surface.md` §1.2 lists the writer-flow as `bodies → refs → indexes` (indexes last). The implementer chose to **reserve** the index slot via `CreateIfAbsent` BEFORE writing any body file, then **finalize** the same index entry AFTER the bodies and refs land. I accept this deviation for the following reasons:

1. **Atomicity primitive.** `CreateIfAbsent` is the only exclusive primitive in the frozen `state-store.md` §3 contract. Without the claim-first reservation, two concurrent writers with the same `(TriggerKey, planHash)` would both pass `RevisionKey` validation, both write trigger.json/revision.json/plan.json into the *same* `revKey` directory, and only race at the index step — corrupting the body directory. Claim-first is the only ordering that produces distinct revision keys before any body write occurs.
2. **Proven primitive.** Task 0004's 100-goroutine atomicity property test on `CreateIfAbsent` is the underwriting evidence. Same primitive, same exclusivity guarantee.
3. **Crash-recovery invariant preserved.** `state-store.md` §6 requires refs to land *after* the bodies they point at. Claim-first does NOT violate this: refs (step 4–5) still write *after* bodies (step 1–3). The index-write split (reservation step 0 / finalization step 6) is on the index document only — and a reservation `{"reserved":true}` is self-identifying for crash-recovery tooling.
4. **`cli-surface.md` §1.2 is descriptive.** The text reads as a high-level write sequence sketch, not a normative atomicity proof. The implementer's seven-step list (steps 0..7) is a strict superset of the spec's intent: every body/ref write the spec requires happens; the only structural addition is the slot-reservation precondition that makes concurrent collision-resolution actually work.

No `ai/proposals/task-0007-spec-update.md` is filed. The `cli-surface.md` text remains accurate at the abstraction level it operates on; the writer's docstring (`writer.go` lines 133–161) cross-references both `design.md` §5.1 and `cli-surface.md` §1.2 explicitly so future readers don't miss the structural detail.

## Issues

None blocking. None non-blocking that warrant code change in PR-A.

## Risk Notes

1. **`stateStoreVersionPath()` lives in `internal/revision`, not `internal/statestore`.** Task 0007's "Open Items For Verifier" flagged the option of relocating to a `statestore.StateStoreVersionPath()` helper. The current location is sound: the path is logically a single string constant `"version.json"` (statestore is rooted at `.orun`), and adding a public helper to `internal/statestore` for one constant would inflate the M2 surface that PR-C just sealed at 96.1 % coverage. **Defer** the relocation; if M5 ever needs a statestore-side helper for migration tooling, that PR can lift the constant up.
2. **`writeCompatibilityMirror` is a no-op stub gated by `Config.CompatibilityWrites=true`.** The flag default-true semantics mean every WriteRevision call from M5 onwards will invoke the stub. This is intentional (the seam exists so M5 lands one body-only diff) — but until M5 ships, callers configuring `CompatibilityWrites=true` get NO legacy mirror despite the flag being on. PR-A test coverage exercises both true and false branches, but this asymmetry should be called out in the M5 implementer prompt so it doesn't surprise.
3. **`RevSummary.JobCount` is left at zero in PR-A.** `summaryFromScope` does not populate it because the planner-supplied count threads through in PR-B (`WriteManifest`). `data-model.md` §3 documents `jobCount` without a non-zero requirement, so this is spec-compliant in PR-A; PR-B must surface it.
4. **`internal/revision` does not export a `package revision_test` API surface yet** (no `package revision` doc.go aside from the model.go header comment). The `model.go` package comment is comprehensive and acceptable; flagging only as a stylistic preference for M3 PR-B/C if more surface lands.
5. **M3 PR-B scope still owed.** `manifest.go`, `resolver.go`, the legacy mirror body, and CLI/runner wiring all remain unwritten. Task 0010 is the immediate next implementer cycle.

## Spec Proposals

None required. The claim-first ordering is accepted in-place; no `cli-surface.md` amendment is filed because the spec text remains accurate at its intended abstraction level.

## Recommended Next Move

Proceed to **Task 0010 — M3 PR-B implementer**: deliver `internal/revision/manifest.go` (`WriteManifest`, `UpdateLatestExecutionSummary`), `internal/revision/resolver.go` (`ResolveRevision` seven-branch resolver per `compatibility-and-migration.md` §3), and the legacy `.orun/plans/<checksum>.json` mirror body promoting `writeCompatibilityMirror` from `// TODO(m5)` stub to a real conditional write gated by `Config.CompatibilityWrites`. Coverage gate stays ≥ 90 % on `internal/revision`; resolver test matrix MUST cover all seven branches; compat-writes flag MUST exercise both true and false body paths.

## PR Number / Merge SHA

PR **#157** (`impl/task-0007-m3-revision-pra`). Merge commit SHA: filled in post-merge below.
