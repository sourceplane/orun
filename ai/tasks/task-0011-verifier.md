# Task 0011 — Verifier (M3 PR-B)

Agent: Verifier

## Current Repo Context

- Active spec: `specs/orun-state-redesign/` (Phase 1, local-only). Active milestone: **M3 — `internal/revision`**. M3 PR-A landed on `main` at `7f1e53d` (PR #157, verified PASS via Task 0009 on 2026-05-30).
- Implementer Task 0010 (M3 PR-B — `manifest.go` + `resolver.go` + legacy `.orun/plans/<checksum>.json` mirror body promoting the `// TODO(m5)` stub to a real conditional write gated by `Config.CompatibilityWrites`, plus `JobCount` plumbing) shipped on branch `impl/task-0010-m3-revision-prb`. Implementer report at `ai/reports/task-0010-implementer.md` names PR **#158**.
- PR **#158** state at orchestrator emission time:
  - `state=OPEN`, `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`
  - head SHA `ec74af10244dd5a50d949f8c154d750aed6924d7` (commit `ec74af1` "docs(ai): task-0010 implementer report"; parent `e538b90` "feat(revision): land M3 PR-B — manifest, resolver, compat-mirror, JobCount")
  - Required CI both SUCCESS at log level:
    - `CI / Orun Plan` — run `26674136935`, job `78622775713`, completed 2026-05-30T04:12:02Z
    - `orun remote-state conformance / Harness dry-run guard` — run `26674136948`, job `78622775747`, completed 2026-05-30T04:11:21Z
  - Five matrix legs SKIPPED legitimately (empty matrix at M3 PR-B — same shape as #152/#155/#156/#157).
  - Diff stat (12 files, +1966 / -22):
    - new: `internal/revision/{errors,legacy,manifest,resolver}.go` and tests `internal/revision/{manifest,resolver,writer_compat,coverage_extra}_test.go`
    - edits: `internal/revision/{model,writer}.go` (added `ManifestKind`; compat-mirror body, `Config.JobCount`, `summaryFromScope` plumbing)
    - artifacts: `ai/tasks/task-0010.md`, `ai/reports/task-0010-implementer.md`
- Implementer report `ai/reports/task-0010-implementer.md` claims:
  - **Option A** chosen for `JobCount` — planner-supplied via `Config.JobCount`, persisted exactly (0 means "unknown" / no parse of `plan.json`).
  - **Branch 3 (revision-key) on `ErrNotFound` falls THROUGH** rather than bubbling — a regex-matching arg can also be a known named ref or legacy hash; only returns `ErrAmbiguousArg` if branches 4–7 also fail.
  - **Branch 5 length floor** of `planShortHashLen` (8); shorter hex falls through to branch 7. Tested.
  - Compat mirror: `Config.CompatibilityWrites=true` writes both `.orun/plans/<hash>.json` (`CreateIfAbsent`, `ErrExists` is treated as idempotent success) and `.orun/plans/latest.json` (`Write`, last-write-wins per spec §2). Default preserved via `WithCompatibilityWrites(false)` and an internal `compatibilityWritesSet` flag.
  - Two new typed sentinels (`ErrAmbiguousArg`, `ErrComponentRunUnchanged`) live in `internal/revision/errors.go`. Resolver-shaped, `errors.Is`-routable, no analogue at `statestore` byte layer.
  - Coverage: `internal/revision` **90.4 %** (gate ≥ 90 %); `internal/statestore` **96.1 %** (M2 floor preserved). `make test-state-redesign` and `go test ./...` green; `go mod tidy` clean.
- After PR #158 merges and is verified, M3 closes. M4 (`internal/executionstate` + runner bridge) is the next milestone.

## Objective

Validate Task 0010 / PR #158 against the Verifier Standard in `agents/orchestrator.md` and the **M3 "Done when"** criteria in `specs/orun-state-redesign/implementation-plan.md`. Specifically confirm the manifest writer, the seven-branch `ResolveRevision`, the compat-mirror body, and the `JobCount` plumbing all match `data-model.md` §3 / §4, `compatibility-and-migration.md` §2 / §3, and `state-store.md` §3 / §6 within the PR-B scope. Confirm no overreach into `cmd/orun`, `internal/state`, `internal/runner`, `internal/runbundle`, `internal/statestore`, `internal/triggerctx`, or `internal/testfx/statefs`. On PASS, merge PR #158 per the Verifier Merge Protocol, fast-forward `main`, and leave the working tree clean.

## PR Boundary

- Verification only. Verifier may commit to the PR branch:
  - `ai/reports/task-0011-verifier.md` (this report).
  - Optionally `ai/proposals/task-0010-spec-update.md` if a spec amendment is required (e.g. branch-3 fallback decision needs to be reflected in `compatibility-and-migration.md` §3 / §4, or `JobCount` semantics need to be normative in `data-model.md` §3 / §4).
  - Any tiny verification-only fix that is strictly necessary for mergeability (typo, stray TODO removal). Anything beyond that → FAIL with explicit blockers; do not edit production code.
- Out of scope: M4 work (`internal/executionstate` + bridge), production-caller wiring, `--persist-revision` flag, hidden `orun state migrate` command, refactor of M0/M1/M2/M3 PR-A surface.

## Read First

1. `agents/orchestrator.md` — Verifier Standard + Verifier Merge Protocol.
2. `specs/orun-state-redesign/README.md` — index + read order.
3. `specs/orun-state-redesign/implementation-plan.md` — Milestone **M3** goal, suggested PR scope, "Done when" checklist (≥ 90 % coverage, key uniqueness/collision property, **resolver covers all 7 branches**, **compat-writes flag exercises both true and false paths**).
4. `specs/orun-state-redesign/data-model.md` — §3 (`PlanRevision`), §3.1 (`planHash` `sha256:` prefix), §4 (`RevisionManifest`), §6 (Refs), §7 (Indexes).
5. `specs/orun-state-redesign/compatibility-and-migration.md` — §2 (Compatibility writes — `.orun/plans/<hex>.json` and `latest.json`), §3 (seven-branch resolution chain, normative ORDER), §4 (Reader fallback).
6. `specs/orun-state-redesign/state-store.md` — §1 (frozen interface), §3 (`CreateIfAbsent` / `CompareAndSwap` / `Write` semantics), §6 (CAS retry / crash-recovery invariants).
7. `specs/orun-state-redesign/design.md` — §5 (architecture / writer ordering), §5.1 (writer-order list), §6 (revision lifecycle / `stateCompatibilityWrites` flag).
8. `specs/orun-state-redesign/test-plan.md` — §1 (coverage), §3 (CAS / property tests), §6 (resolver matrix).
9. `ai/tasks/task-0010.md` — implementer prompt.
10. `ai/reports/task-0010-implementer.md` — implementer report (especially the **Option A: JobCount**, **Compatibility-mirror posture**, and **Resolver branch ordering** sections).
11. PR **#158** diff and commits on branch `impl/task-0010-m3-revision-prb`.

## Required Outcomes

- [ ] Verifier report at `ai/reports/task-0011-verifier.md` with sections: Result, Checks, CI Log Review, Issues, Risk Notes, Spec Proposals, Recommended Next Move, PR Number + merge SHA.
- [ ] Explicit adjudication of three implementer decisions, each either accepted-with-Risk-Note OR backed by a filed proposal at `ai/proposals/task-0010-spec-update.md`:
  1. **Branch 3 (revision-key) `ErrNotFound` fallthrough** — does the seven-branch chain in `compatibility-and-migration.md` §3 require bubble-up on first regex match, or is fallthrough consistent with §4 reader-fallback intent?
  2. **`JobCount` Option A** — planner-supplied via `Config.JobCount`, with `0` meaning "unknown". Confirm this matches `data-model.md` §3 `RevSummary` semantics (or file a spec clarification).
  3. **Compat-mirror `latest.json` last-write-wins** via `Write` (not `CreateIfAbsent`). Confirm against `compatibility-and-migration.md` §2.
- [ ] PR #158 either squash-merged into `main` (PASS) or left OPEN with explicit blockers (FAIL).
- [ ] If PASS: local `main` fast-forwarded to `origin/main`; PR branch deleted; `git status --short` clean (resolve any verifier-created scratchpad edits before ending the task).
- [ ] `/ai/state.json` `task_agent` updated to `/ai/tasks/task-0011-verifier.md` while this task is in flight (orchestrator sets it on emission; verifier flips to its own report path on completion if that is the most recently produced file).

## Constraints

1. **No production-code edits** beyond a verifier-only typo/TODO fix strictly required for mergeability. The M3 PR-B surface (`internal/revision/{errors,legacy,manifest,resolver}.go`, edits to `model.go` / `writer.go`) stays as authored.
2. **Spec edits only via proposal.** If any of the three adjudication points needs a spec amendment, write `ai/proposals/task-0010-spec-update.md` per the Proposal template in `agents/orchestrator.md`; do NOT edit `compatibility-and-migration.md` / `data-model.md` directly.
3. **No M4 scope.** `internal/executionstate`, runner bridge, `Bridge{...}`, `MirrorRunnerOutput`, `--persist-revision` flag, and `orun state migrate` MUST NOT appear. The `UpdateLatestExecutionSummary` helper SHIPS in PR-B, but its call site (M4) does not.
4. **No production-caller wiring.** `cmd/orun`, `internal/state`, `internal/runner`, `internal/runbundle` MUST be byte-identical to `origin/main`.
5. **Leaf-clean.** `internal/revision` may import `internal/triggerctx` and `internal/statestore` (test-only `internal/testfx/statefs`); MUST NOT import `cmd/`, `internal/state`, `internal/runner`, `internal/runbundle`, or anything outside the documented dependency set in `design.md` §13. Verify with `go list -deps ./internal/revision | rg "/orun/internal/"`.
6. **Coverage gates.** `internal/revision` ≥ 90 % (M3 gate; implementer reported 90.4 %). `internal/statestore` ≥ 95 % (M2 floor; should still be ~96.1 %). PR-A's 93.3 % was on a smaller surface — do NOT mechanically require ≥ 93.3 % on the wider PR-B surface; the gate is 90 %.
7. **Existing milestones unchanged.** `internal/triggerctx`, `internal/statestore`, and `internal/testfx/statefs` files MUST be byte-identical to `origin/main`.
8. **Resolver branch ORDER is normative** per `compatibility-and-migration.md` §3. The seven branches must be tested in the spec's order with explicit `ResolveSource` assertions.
9. **Compat-writes flag MUST exercise BOTH paths.** Per M3 "Done when": `CompatibilityWrites=true` writes both `.orun/plans/<hash>.json` and `.orun/plans/latest.json` byte-identical to `plan.json`; `CompatibilityWrites=false` writes neither. Idempotent re-run on the same plan succeeds with no error.
10. **No new error sentinels in `internal/statestore`.** `ErrAmbiguousArg` and `ErrComponentRunUnchanged` MUST live in `internal/revision/errors.go` and be `errors.Is`-routable.
11. **No string concatenation for paths.** Legacy paths route through `paths.go`-style helpers (the implementer added `legacy.go`; confirm it does not concat strings outside that file).
12. **CI is authoritative for `orun plan --changed`.** The local composition-cache quirk is a known environment artifact carried since Task 0001. Reproducing it is not a blocker; failing CI is.
13. **Merge gate**: never merge unless BOTH local quality gates AND the two required CI checks (`CI / Orun Plan`, `Harness dry-run guard`) are SUCCESS at log level on the final PR head SHA (re-checked after any verifier-side commit).

## Verification Steps

Run, in order, from a clean workspace.

### 1. Repo State

```bash
git fetch origin
git checkout main
git pull --ff-only origin main
git status --short
gh pr view 158 --json number,state,mergeable,mergeStateStatus,headRefName,headRefOid,statusCheckRollup
```

Confirm: PR is OPEN, MERGEABLE, CLEAN, head SHA = `ec74af10244dd5a50d949f8c154d750aed6924d7`. If a new commit has been pushed since orchestrator emission, re-evaluate from the new head and inspect any new diff.

### 2. Diff Audit (overreach detection)

```bash
git fetch origin pull/158/head:pr-158
git diff --stat origin/main...pr-158
# Surfaces that MUST be empty:
git diff origin/main...pr-158 -- cmd/orun internal/state internal/runner internal/runbundle
git diff origin/main...pr-158 -- internal/triggerctx internal/statestore internal/testfx/statefs
# Surfaces that MUST NOT exist (M4 leakage):
git diff origin/main...pr-158 -- internal/executionstate
# Confirm only the expected new files exist:
git ls-tree --name-only -r pr-158 -- internal/revision/ | sort
```

The "MUST be empty" calls have to return zero lines. `internal/executionstate/` MUST NOT exist in the PR tree. `internal/revision/` should contain exactly: `coverage_extra_test.go`, `errors.go`, `keys.go`, `keys_test.go`, `legacy.go`, `manifest.go`, `manifest_test.go`, `model.go`, `resolver.go`, `resolver_test.go`, `version.go`, `writer.go`, `writer_compat_test.go`, `writer_test.go`, `coverage_test.go`. If any of these fail, FAIL with the exact lines.

### 3. Spec Conformance — Code-Path Inspection

Open these in parallel with the spec sections:

- `internal/revision/manifest.go` ↔ `data-model.md` §4 (`RevisionManifest`) + `state-store.md` §3 (`CreateIfAbsent` / `CompareAndSwap`) + §6 (CAS retry).
- `internal/revision/resolver.go` ↔ `compatibility-and-migration.md` §3 (branch order) + §4 (reader fallback).
- `internal/revision/legacy.go` ↔ `compatibility-and-migration.md` §2 (`.orun/plans/<hex>.json` and `latest.json`) + `data-model.md` §3.1 (`planHash` `sha256:` prefix strip).
- `internal/revision/writer.go` (PR-B edits only) ↔ `design.md` §5.1 + §6 (`stateCompatibilityWrites`).
- `internal/revision/errors.go` ↔ `state-store.md` §1/§4 (no new sentinels at the byte layer; package-local `ErrAmbiguousArg` / `ErrComponentRunUnchanged` documented at declaration).
- `internal/revision/model.go` (added `ManifestKind` only) ↔ `data-model.md` §4.

Check:

- **`WriteManifest`**:
  - JSON shape matches `data-model.md` §4 byte-for-byte (every field name + tag).
  - Writes to logical path `revisions/<revisionKey>/manifest.json` via `statestore.CreateIfAbsent` (idempotent re-runs surface `ErrExists`; verify the helper does NOT swallow it).
  - Deterministic JSON: `SetIndent("", "  ")`, `SetEscapeHTML(false)`, trailing `\n`.
  - All exported symbols carry doc comments referencing the spec section.
- **`UpdateLatestExecutionSummary`**:
  - Read → mutate `summary.latestExecutionKey` / `summary.latestExecutionStatus` → `CompareAndSwap`.
  - Loser-retries up to `casRetryBudget` (existing constant in `writer.go`); on budget exhaustion returns the wrapped `ErrConflict` from statestore unchanged.
  - **Idempotent short-circuit**: if `latestExecKey` and `latestExecStatus` already match, helper returns nil without an unnecessary CAS round-trip. Confirm tested.
  - Signature is `(ctx, cfg, revKey, execKey, execStatus)` — minimal, M4 owns status semantics.
- **`ResolveRevision`** (seven branches in this exact order per `compatibility-and-migration.md` §3):
  1. empty arg → `refs/latest-revision.json`.
  2. arg matches an existing file → load plan bytes from file; synthesize `system.manual` revision **in-memory only** (no disk write).
  3. arg matches `^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$` → `revisions/<arg>/plan.json` + `revision.json`. **On `ErrNotFound`: implementer chose FALLTHROUGH** — adjudicate against §3 ("first match wins" reading) vs §4 reader-fallback intent. Either accept-and-document inline (Risk Notes) or file a proposal. The implementer rationale (a regex-matching arg can also be a named ref or legacy hash) is plausible but not in the spec text.
  4. arg matches a named ref → `refs/named/<arg>.json` → revision key from the ref.
  5. arg is hex (`^[a-f0-9]{8,64}$`) → `.orun/plans/<arg>.json` legacy load + synthesize migrated revision in-memory (`triggerType: "system"`, `triggerName: "system.migrated"`). Length floor `>= planShortHashLen (8)`.
  6. arg matches a component name → return `ResolveSourceComponent` with `ErrComponentRunUnchanged` (typed sentinel, M5 CLI rewires this branch).
  7. otherwise → typed `ErrAmbiguousArg`.
  - `ResolveSource` enum-ish typed string contains exactly: `ResolveSourceLatest`, `ResolveSourceFile`, `ResolveSourceRevisionKey`, `ResolveSourceNamedRef`, `ResolveSourceLegacyHash`, `ResolveSourceComponent`.
  - Resolver is **read-only** — no `Write` / `CreateIfAbsent` / `CompareAndSwap` calls in `resolver.go`.
- **`writeCompatibilityMirror`** (promoted from `// TODO(m5)` stub):
  - When `Config.CompatibilityWrites == true`:
    - `.orun/plans/<planHashHex>.json` written via `statestore.CreateIfAbsent` (legacy paths via `legacy.go` helpers, no string concat).
    - `.orun/plans/latest.json` written via `statestore.Write` (last-write-wins per spec §2). Confirm `Write` is correct vs `CreateIfAbsent`.
    - On `ErrExists` for the checksum body, treat as no-op success (idempotent re-runs of `WriteRevision` on the same plan bytes).
    - Other store errors wrapped and returned.
  - When `Config.CompatibilityWrites == false`: returns nil; writes nothing.
  - Mirror bytes are byte-identical to canonical `plan.json` — caller's `planBytes` is reused, NOT re-marshalled.
  - `<planHashHex>` strip (`strings.TrimPrefix(planHash, "sha256:")`) is in a small unexported helper with its own unit test.
- **`Config.JobCount` plumbing (Option A)**:
  - `WriteRevision` threads `Config.JobCount` into `RevSummary.JobCount`. Confirm.
  - PR-A's `RevSummary.JobCount = 0` placeholder is no longer reached via the hot path when the caller knows the job count.
  - Callers that don't know the count (none in tree today) keep working with `JobCount = 0`. Manifest reflects that exact value (does not lie about "0 jobs" vs "unknown").
- **Errors**:
  - `internal/revision/errors.go` declares `ErrAmbiguousArg` and `ErrComponentRunUnchanged` only. Each has a doc comment.
  - All resolver / writer / manifest errors that wrap `statestore` sentinels use `fmt.Errorf("%w: …", ErrX, …)` so `errors.Is` / `errors.As` work.
  - No new sentinels in `internal/statestore`.
- **Imports**:
  - `go list -deps ./internal/revision | rg "/orun/internal/"` returns only `internal/statestore` and `internal/triggerctx` (plus their transitives like `internal/model` via triggerctx; allowed). No `cmd/`, no `internal/state`, no `internal/runner`, no `internal/runbundle`.
  - Test files may import `internal/testfx/statefs`; production code MUST NOT.

### 4. Local Quality Gates

```bash
go build ./...
go vet ./...
go test -race -count=1 ./internal/revision/...
go test -race -count=1 ./internal/statestore/...
go test -race -count=1 ./internal/triggerctx/...
go test -race -count=1 ./...
make test-state-redesign        # confirm coverage gate prints "measured: <≥90.0>%" for revision and "<≥95.0>%" for statestore
go test -cover ./internal/revision/... ./internal/statestore/...   # second-source coverage measurement
go list -deps ./internal/revision | rg "/orun/internal/" || echo "no internal imports beyond allowed set"
/Users/irinelinson/.local/bin/kiox -- orun validate --intent examples/intent.yaml
/Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent examples/intent.yaml --output /tmp/plan-0011.json || \
  echo "EXPECTED: composition-cache quirk on local; CI is authoritative"
/Users/irinelinson/.local/bin/kiox -- orun run --plan /tmp/plan-0011.json --dry-run --runner github-actions || \
  echo "skipped because plan not produced; record no-op"
```

All non-quirk steps must exit 0. Coverage thresholds:
- `internal/revision` ≥ 90 % (M3 gate; implementer reported 90.4 %)
- `internal/statestore` ≥ 95 % (M2 floor; should still be ~96.1 %)

### 5. Test Matrix Audit

Open the test files and confirm explicit coverage of each "Done when" requirement:

- `manifest_test.go`:
  - Happy-path `WriteManifest`.
  - Idempotent `WriteManifest` (re-run on same revision → `ErrExists` from store, helper does NOT swallow).
  - `UpdateLatestExecutionSummary` happy path.
  - `UpdateLatestExecutionSummary` CAS contention: forces a conflicting Write between Read and CAS, asserts retry succeeds inside `casRetryBudget`.
  - `UpdateLatestExecutionSummary` budget exhaustion: asserts eventual `ErrConflict` after `casRetryBudget`.
  - `UpdateLatestExecutionSummary` idempotent short-circuit (no CAS when current state already matches).
  - JSON byte-stability via `internal/testfx/statefs.AssertJSONFile`.
- `resolver_test.go`:
  - **All seven branches** tested with explicit `ResolveSource` assertions.
  - Branch 3 fallthrough on `ErrNotFound` (implementer-chosen behavior).
  - Branch 5 length floor (hex < 8 chars falls through to branch 7).
  - `ErrAmbiguousArg` returned when no branch matches.
  - `ErrComponentRunUnchanged` returned on branch 6.
  - Each branch test self-contained via `internal/testfx/statefs` workspace harness.
- `writer_compat_test.go`:
  - `CompatibilityWrites=true` writes both `.orun/plans/<hash>.json` and `.orun/plans/latest.json` byte-identical to `plan.json`.
  - `CompatibilityWrites=false` writes neither.
  - Idempotent re-run on the same plan succeeds with no error.
  - `<planHashHex>` strip helper has its own unit test.

### 6. CI Log Review

Inspect both required CI runs at log level (not just summary):

```bash
gh run view 26674136935 --log-failed | head -200    # CI / Orun Plan
gh run view 26674136935 --log | rg -n "orun plan" | head -20
gh run view 26674136948 --log | rg -n "guard\] PASS:" | head -50
```

Confirm:

- `CI / Orun Plan` (run `26674136935`) actually invoked `orun plan --from-ci github …` against `examples/intent.yaml`, recorded the legitimate empty-matrix shape (`0 components × 3 envs → 0 jobs` is expected for a code-only PR with no component changes), and uploaded the plan artifact.
- `Harness dry-run guard` (run `26674136948`) emitted the full `[guard] PASS:` battery (bash syntax, command-count thresholds, duplicate-claim helper PASS+FAIL, status helper PASS+FAIL, exported env asserts).
- The five SKIPPED matrix legs are legitimate empty-matrix skips (not silently-failed required checks).

If a verifier-side commit (report, optional proposal) lands on the PR branch, RE-INSPECT both runs at log level on the new head SHA before merging.

### 7. Secret Hygiene & Production-Grade Basics

```bash
rg -n -i "(token|password|secret|key=)" -- internal/revision/ || echo "clean"
```

Confirm: no plaintext tokens, no logging of sensitive material, deterministic JSON, all exported symbols have doc comments.

### 8. Decision and Merge

If every step is green and the three adjudication points (branch-3 fallthrough, JobCount Option A, latest.json `Write`-not-`CreateIfAbsent`) are each adjudicated (accepted-with-Risk-Note OR proposal filed at `ai/proposals/task-0010-spec-update.md`):

```bash
git checkout impl/task-0010-m3-revision-prb
git pull --ff-only origin impl/task-0010-m3-revision-prb
# write ai/reports/task-0011-verifier.md (and ai/proposals/task-0010-spec-update.md if needed)
git add ai/reports/task-0011-verifier.md ai/proposals/task-0010-spec-update.md 2>/dev/null || true
git commit -m "Task 0011: M3 PR-B verifier report (PASS)"
git push origin impl/task-0010-m3-revision-prb
# wait for CI to re-run on the new commit and confirm both required checks SUCCESS at log level
gh pr checks 158 --watch --interval 30
gh pr review 158 --approve --body "Verified PASS — Task 0011 (M3 PR-B). See ai/reports/task-0011-verifier.md."
gh pr merge 158 --squash --delete-branch
git checkout main
git pull --ff-only origin main
git status --short
```

If any blocker exists, leave PR OPEN and write the verifier report with `Result: FAIL` and explicit blockers. Do NOT merge.

## Acceptance Criteria

- ✅ PR #158 corresponds 1:1 to Task 0010 / M3 PR-B (manifest + resolver + compat-mirror promotion + JobCount); only `internal/revision/**`, `ai/tasks/task-0010.md`, and `ai/reports/task-0010-implementer.md` changed. No `internal/executionstate/`, no production-caller wiring.
- ✅ Both required CI checks on PR #158 are SUCCESS at log level (`CI / Orun Plan` run `26674136935`, `Harness dry-run guard` run `26674136948`) on the final head SHA — re-checked after any verifier-side commit.
- ✅ `internal/revision` coverage ≥ 90 % locally (implementer reported 90.4 %).
- ✅ `internal/statestore` coverage ≥ 95 % (no regression).
- ✅ `internal/revision` imports stay within `oklog/ulid/v2` + `internal/triggerctx` + `internal/statestore` + stdlib (production code); test files may add `internal/testfx/statefs`.
- ✅ `RevisionManifest` JSON byte-matches `data-model.md` §4.
- ✅ Resolver test matrix exercises all 7 branches with explicit `ResolveSource` assertions; ambiguity returns `ErrAmbiguousArg`; component branch returns `ErrComponentRunUnchanged`.
- ✅ Compat-writes flag exercises BOTH `true` and `false` paths; idempotent re-run on same plan succeeds.
- ✅ `<planHashHex>` strip helper has its own unit test.
- ✅ `UpdateLatestExecutionSummary` honors `casRetryBudget` and surfaces wrapped `ErrConflict` on exhaustion; idempotent short-circuit when state already matches.
- ✅ Resolver is read-only (no `Write` / `CreateIfAbsent` / `CompareAndSwap` calls).
- ✅ All exported symbols carry doc comments.
- ✅ No new sentinels in `internal/statestore`; `ErrAmbiguousArg` / `ErrComponentRunUnchanged` live in `internal/revision/errors.go`.
- ✅ `internal/triggerctx`, `internal/statestore`, and `internal/testfx/statefs` byte-identical to `origin/main`.
- ✅ No string concatenation for paths anywhere in `internal/revision/` outside the centralized helpers.
- ✅ Three adjudication points (branch-3 fallthrough, JobCount Option A, `latest.json` last-write-wins) each adjudicated inline OR via a single `ai/proposals/task-0010-spec-update.md`.
- ✅ On PASS: PR #158 squash-merged, branch deleted, local `main` fast-forwarded, `git status --short` clean.
- ✅ Verifier report `ai/reports/task-0011-verifier.md` filed.

## When Done Report

Save to: `/ai/reports/task-0011-verifier.md`

Sections:

- Result: PASS | FAIL
- Checks (every command from steps 1–7 with exit code / outcome)
- CI Log Review (run IDs, expected commands actually executed, evidence quoted)
- Adjudications (one paragraph each for branch-3 fallthrough, JobCount Option A, latest.json `Write`-not-`CreateIfAbsent`; cite spec sections)
- Issues (blocking or non-blocking, ranked)
- Risk Notes (residual risk after merge — e.g. branch-3 fallthrough not yet captured in spec, `JobCount=0` ambiguity between "no jobs" and "unknown", `UpdateLatestExecutionSummary` callers absent until M4)
- Spec Proposals (links + one-line reason; expected to be **None** OR a single `ai/proposals/task-0010-spec-update.md`)
- Recommended Next Move (M3 closes on PASS; next is **Task 0012 = M4 implementer** scoping `internal/executionstate` + runner bridge per `implementation-plan.md` Milestone M4)
- PR Number (158) and merge commit SHA (post-merge) or "OPEN — blocked" (FAIL)
