# Task 0003 (Verifier pass)

Agent: Verifier

## Current Repo Context

- Implementer Task 0003 landed Milestone M2 **PR A** of
  `specs/orun-state-redesign/`: the frozen `StateStore` interface, the four-error
  taxonomy (`ErrNotFound`, `ErrExists`, `ErrConflict`, `ErrInvalid`), the
  `paths.go` helper module covering every entry in `state-store.md` §2.1, and a
  local-driver implementation of the non-CAS subset (`Root`, `Read`, `Write`,
  `CreateIfAbsent`, `Delete`).
- `CompareAndSwap` and `List` are present on `*LocalStore` but stubbed
  (returning `%w: not implemented in PR A` wrapping `ErrInvalid`) so the
  interface compiles. Real implementations + atomicity property suite are
  deferred to PR B (next implementer task).
- Typed refs/indexes marshallers (PR C) are deliberately deferred; the path
  helpers themselves ship now.
- PR **#154** (`impl/task-0003-m2-statestore-pra` @ `4afcd34`) is open against
  `main` (`db342dd` after Task 0002 merge). Status: `MERGEABLE`, `CLEAN`.
  Required CI checks `CI / Orun Plan` and
  `orun remote-state conformance / Harness dry-run guard` both `SUCCESS`;
  matrix legs SKIPPED legitimately (empty matrix at this milestone).
- Implementer report is filed at `ai/reports/task-0003-implementer.md` (and
  is committed to the PR branch — the dedicated commit `4afcd34` exists for
  exactly that purpose). Reported package coverage on `internal/statestore`
  is **95.4 %**; `make test-state-redesign` enforces the ≥ 95 % gate.
- The pre-existing local `kiox -- orun plan --changed --intent
  examples/intent.yaml` composition-cache failure (`stack.yaml … has no
  spec.compositions`) reproduces from Task 0001 onward and is not a regression
  introduced by this PR. CI is authoritative for that invocation.

## Objective

Validate PR #154 against the Verifier Standard in `agents/orchestrator.md` and
the M2 PR-A "done when" criteria in
`specs/orun-state-redesign/implementation-plan.md`. Confirm the StateStore
interface signatures freeze byte-for-byte to `state-store.md` §1, that path
helpers cover §2.1, that the local driver's atomicity guarantees match
`state-store.md` §3, that `internal/statestore` is leaf-clean (no
`internal/*` imports), that no production callers were wired, and that all
quality gates pass. If everything checks out, apply the Verifier Merge
Protocol; otherwise leave PR #154 open with clear blockers.

## PR Boundary

This is verification only. The verifier may commit the verifier report
(`ai/reports/task-0003-verifier.md`) to the PR branch as a verifier-only
artifact and re-run CI. No production-code edits, no spec edits in this PR.
If a spec proposal is required, file `/ai/proposals/task-0003-*.md` on `main`
(or as a separate PR) and document it in the verifier report — do not block
merge of PR #154 on a spec-change PR landing first unless the divergence is
behavioral or contract-altering.

## Read First

- `ai/tasks/task-0003.md` — original implementer scope, constraints, and
  acceptance criteria.
- `ai/reports/task-0003-implementer.md` — the implementer's account of what
  shipped, checks run, assumptions, and the PR-A vs PR-B/C scope split.
- `agents/orchestrator.md` — Verifier Standard (responsibilities, report
  shape) and Verifier Merge Protocol (kiox/orun checks, GitHub Actions log
  audit, post-merge cleanup).
- `specs/orun-state-redesign/README.md` — entry point + read order.
- `specs/orun-state-redesign/implementation-plan.md` — Milestone M2 "done
  when" criteria; clarifies which lines belong to PR A vs PR B vs PR C.
- `specs/orun-state-redesign/state-store.md` — entire document. The interface
  in §1, the path helpers in §2.1, the atomicity contract in §3, and the
  error taxonomy in §4 are all gates here.
- `specs/orun-state-redesign/design.md` §7 (StateStore role) and §9
  (correctness properties).
- `specs/orun-state-redesign/test-plan.md` §1 (coverage targets) and §2
  (atomicity suite — the parts shipping in PR A).
- `internal/statestore/` (the new package) and the diff in PR #154.

Reference only:
- `internal/triggerctx/` and `internal/testfx/statefs/` — proves the
  package shape and leaf-clean import policy that `internal/statestore`
  must mirror.

## Required Outcomes

- [ ] Verifier report at `ai/reports/task-0003-verifier.md` with the
      mandatory sections: Result, Checks, Issues, Risk Notes, Spec
      Proposals, Recommended Next Move (plus `CI Log Review` and
      `Secret Handling Review` per the verifier execution workflow).
- [ ] Result: `PASS` if every acceptance criterion below is satisfied;
      otherwise `FAIL` with itemized blockers.
- [ ] If PASS, PR #154 squash-merged via the Verifier Merge Protocol;
      local `main` fast-forwarded to `origin/main`; the
      `impl/task-0003-m2-statestore-pra` branch deleted; working tree clean.
- [ ] If FAIL, PR #154 left open with blockers documented; no merge.

## Non-Goals

- No PR-B or PR-C work in this PR. CAS/List real implementations and
  the 100-goroutine atomicity property suite stay deferred to the next
  implementer task. The verifier MUST NOT hand-roll those tests here.
- No typed refs/indexes marshallers.
- No production-caller wiring (`cmd/orun`, `internal/state`,
  `internal/runner`, `internal/runbundle` must remain untouched in PR #154).
- No legacy migration writes.
- No CLI surface changes.

## Constraints

- Use `/Users/irinelinson/.local/bin/kiox` for `orun` invocations if `kiox`
  isn't on `PATH`.
- Inspect actual PR CI log lines (not just status summaries) for both
  required checks — confirm the test commands actually ran with the expected
  arguments.
- Do not weaken the ≥ 95 % coverage gate. If coverage measurement disagrees
  with the implementer's reported 95.4 %, that is a blocker.
- Do not merge if `mergeable != MERGEABLE` or `mergeStateStatus != CLEAN`,
  if any required check is not `SUCCESS`, or if any acceptance criterion
  below fails.
- Never include user bytes (object contents) or absolute filesystem paths
  beyond what is already exposed by `LocalStore.Root()` in any log or
  report excerpt.

## Integration Notes

- M2 PR A freezes the `StateStore` interface. Once this PR merges, the
  interface signatures are locked; future divergence requires a spec-update
  proposal. The verifier MUST cross-check the live source against
  `state-store.md` §1 byte-for-byte.
- M3 (`internal/revision`) is the first downstream consumer. Verifier should
  spot-check that `paths.go` covers every helper M3 will need (revision dir,
  plan/trigger/revision/manifest, refs, indexes) so M3 doesn't have to
  introduce a private helper.
- PR B (CAS + List + atomicity property suite) is the next implementer task
  after this verification closes; the verifier report should explicitly tee
  it up in `Recommended Next Move` so the orchestrator can scope it without
  guesswork.

## Acceptance Criteria

✅ PR #154 maps to exactly Task 0003 PR-A as scoped in
   `ai/tasks/task-0003.md`. No unrelated edits — diff confined to
   `internal/statestore/`, `Makefile`, `go.mod` / `go.sum`, and
   `ai/tasks/task-0003.md` / `ai/reports/task-0003-implementer.md`.

✅ `internal/statestore` exports exactly: `StateStore` (interface),
   `ObjectMeta`, `ObjectInfo`, `WriteOptions`, `LocalConfig`, `LocalStore`,
   `NewLocalStore`, `ErrNotFound`, `ErrExists`, `ErrConflict`, `ErrInvalid`,
   plus every helper from `state-store.md` §2.1. `go doc ./internal/statestore`
   shows doc comments on each exported symbol.

✅ `StateStore` interface signatures match `state-store.md` §1
   byte-for-byte (method names, parameter order, return types, doc-comment
   intent). Diff against the spec block is clean.

✅ `paths.go` covers every helper in §2.1 and centralizes the alphabet
   policy (`[a-zA-Z0-9._-]` per segment, no `..`, no leading `/`, no Windows
   separators, no empty segments). Validation rejections raise `ErrInvalid`.

✅ Local driver atomicity: `Write` uses temp-file + `fsync` + `os.Rename`
   with `EXDEV` cross-device copy fallback into a tempdir on the target FS,
   then atomic rename. Inspected from source (not just from passing tests).

✅ `CreateIfAbsent` uses `O_EXCL` and returns `ErrExists` on collision;
   `Delete` is a no-op on absent paths and refuses non-empty directories
   with `ErrInvalid`; orphan-tempfile sweep at construction removes
   `.orun-tmp-*` older than the configured 1-hour boundary while preserving
   younger entries. Tests cover both sides of the boundary using the
   `LocalConfig.Clock` injection.

✅ Every driver error wraps a sentinel via `fmt.Errorf("%w: …", ErrX, …)`
   so `errors.Is` / `errors.As` succeed for every documented failure mode.
   No string sniffing.

✅ `internal/statestore` has zero `internal/*` imports from this repo
   (`go list -deps ./internal/statestore | grep sourceplane/orun/internal |
    grep -v 'statestore$'` returns no rows).

✅ No production-caller wiring: `git diff main...HEAD -- cmd/orun
   internal/state internal/runner internal/runbundle` is empty.

✅ `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...`,
   `go test -cover ./internal/statestore/...`, and `make test-state-redesign`
   all exit 0 locally. Coverage on `internal/statestore` is ≥ 95 %.

✅ `kiox -- orun validate --intent intent.yaml` from the workspace that
   defines it (e.g. `examples/`) exits 0. The pre-existing
   composition-cache failure of `kiox -- orun plan --changed` is recorded
   but not treated as a blocker; CI is authoritative.

✅ PR CI log review: both `CI / Orun Plan` and
   `orun remote-state conformance / Harness dry-run guard` show real
   command invocations and assertions actually ran (no "skipped" surprises
   in the body of the required jobs). Matrix-leg `SKIPPED` results are
   legitimate at the empty-matrix M0/M1/M2-PR-A shape.

✅ Secret handling: `git diff main...HEAD` contains no tokens, passwords,
   API keys, or full connection strings. No log lines in the new code emit
   absolute paths beyond `LocalStore.Root()` for diagnostics, and no log
   lines emit object contents.

✅ MergeStateStatus is `CLEAN` and the branch is up-to-date with `main`
   at merge time.

✅ Verifier report filed; if PASS, PR #154 squash-merged, `main`
   fast-forwarded, branch deleted, working tree clean.

## Verification

Execute these steps in order. Stop and FAIL on the first blocker.

1. **Repo state and PR audit**
   - `git status --short` (must be clean before starting).
   - `git fetch origin && git log --oneline origin/main -3`.
   - `gh pr view 154 --json mergeable,mergeStateStatus,statusCheckRollup,headRefOid`
     — confirm `MERGEABLE` / `CLEAN` and every required check `SUCCESS`.
   - `gh pr diff 154 --name-only` — confirm scope confinement.

2. **Implementer report committed to PR branch**
   - `git ls-tree origin/impl/task-0003-m2-statestore-pra --name-only ai/reports/task-0003-implementer.md`
     — must list the file. (The implementer already pushed `4afcd34` for
     exactly this; the verifier should still verify, since this gap has bit
     prior tasks.)

3. **Interface and exports**
   - Read `internal/statestore/store.go`. Diff the `StateStore` interface
     block byte-for-byte against `state-store.md` §1.
   - `go doc ./internal/statestore` — check every exported symbol has a
     doc comment.

4. **Path helpers and validation**
   - Read `internal/statestore/paths.go`. Confirm every helper from
     `state-store.md` §2.1 exists with the documented return shape.
   - Read `paths_test.go`. Confirm table-driven coverage of the alphabet
     policy (rejects `..`, leading `/`, Windows separator, empty segment,
     out-of-alphabet characters → `ErrInvalid`).

5. **Local-driver semantics (code-path inspection)**
   - Read `internal/statestore/local.go`. Confirm:
     - `Write` opens a tempfile in the target's directory, calls `fsync`
       on it, then `os.Rename`s into place; on `EXDEV` it copies into a
       tempdir on the target FS, fsyncs, and renames atomically.
     - `CreateIfAbsent` uses `O_EXCL`.
     - `Delete` no-ops on `ENOENT` and rejects non-empty directories with
       `ErrInvalid`.
     - Orphan-sweep iterates `.orun-tmp-*`, compares mtime against
       `LocalConfig.Clock() - 1h`, removes older entries, errors on the
       sweep are non-fatal.
     - Every error path wraps a sentinel.

6. **Leaf-clean imports and no-callers check**
   - `go list -deps ./internal/statestore | grep sourceplane/orun/internal
     | grep -v 'statestore$'` — must return no rows.
   - `git diff origin/main...HEAD -- cmd/orun internal/state internal/runner
     internal/runbundle` — must be empty.

7. **Quality gates**
   - `go build ./...`
   - `go vet ./...`
   - `go test -race -count=1 ./...`
   - `go test -cover ./internal/statestore/...` — record the percentage.
   - `make test-state-redesign` — must exit 0 and exercise the coverage
     gate.

8. **Orun gates** (per Verifier Merge Protocol)
   - `/Users/irinelinson/.local/bin/kiox -- orun validate --intent intent.yaml`
     from `examples/`.
   - `/Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent
     intent.yaml --output plan.json` from `examples/` — record local
     composition-cache failure if it reproduces (carried from prior tasks).
   - `/Users/irinelinson/.local/bin/kiox -- orun run --plan plan.json
     --dry-run --runner github-actions` if `plan.json` was produced; record
     the no-op result otherwise.

9. **CI log inspection**
   - For each required check on PR #154, pull a few log lines via `gh run
     view <run-id> --log` (or `--log-failed` if you only want errors) to
     confirm real assertions ran. Record run IDs in the report.

10. **Secret hygiene**
    - `git diff origin/main...HEAD` and grep for token / key / password
      patterns. Must be clean.

11. **Spec drift check**
    - Compare runtime behavior against `state-store.md` §1, §2.1, §3, §4.
      Note any non-blocking drift in the report's `Spec Proposals` section.

12. **Decide and act**
    - If all of the above pass: write `PASS` report, commit to the PR
      branch, push, wait for CI to re-run, then squash-merge with
      `gh pr merge 154 --squash --delete-branch`. Switch to `main`, pull
      with fast-forward, and confirm `git status --short` is clean.
    - If anything fails: write `FAIL` report with itemized blockers,
      commit to the PR branch, push, leave PR open. Do not merge.

## When Done Report

Save to `ai/reports/task-0003-verifier.md` with these sections:

- **Result**: `PASS` or `FAIL`.
- **Checks**: every command run with exit code and (for measurements) the
  observed value. Group by category (repo audit, interface diff, path
  helpers, local-driver inspection, leaf-clean check, quality gates, orun
  gates, CI logs, secret hygiene).
- **Issues**: blockers (FAIL) or non-blocking observations (PASS). Use
  severity tags (Blocker / Major / Minor).
- **CI Log Review**: PR #154 run IDs and the lines you pulled to confirm
  the test commands actually ran.
- **Secret Handling Review**: explicit confirmation no tokens/credentials
  were committed.
- **Risk Notes**: residual risks after PR A merges (e.g., CAS/List stubs
  surfacing `ErrInvalid` to any accidental future caller; orphan-sweep
  failure path silently swallowed).
- **Spec Proposals**: links to any `/ai/proposals/task-0003-*.md` files,
  with one-line rationale. None expected if `state-store.md` was followed
  faithfully.
- **Recommended Next Move**: scope the next task — typically Milestone M2
  PR B (CAS + List + 100-goroutine atomicity / exclusivity property suite
  per `test-plan.md` §2 / §3) so the orchestrator can author it
  immediately on PASS.
- **Merge action taken**: PR #154 squash-merged at commit `<sha>` (PASS)
  or PR #154 left open (FAIL, with reason).
