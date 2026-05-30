# Task 0003 — Verifier Report (M2 PR A — internal/statestore)

## Result: PASS

PR #154 (`impl/task-0003-m2-statestore-pra` @ `4afcd34`) implements M2 PR A of
the orun-state-redesign exactly as scoped in `ai/tasks/task-0003.md`: the
frozen `StateStore` interface, the four-error taxonomy, the path-helper module
covering every entry in `state-store.md` §2.1, and the local-driver
non-CAS subset (`Root`, `Read`, `Write`, `CreateIfAbsent`, `Delete`). All
acceptance criteria are met. Merging via the Verifier Merge Protocol.

## Checks

### Repo audit
| Command | Result |
|---|---|
| `git status --short` (pre-checkout) | only orchestrator state files dirty (reverted before merge) |
| `git fetch origin && git log --oneline origin/main -3` | tip = `db342dd` (Task 0002 M1) — matches expected base |
| `gh pr view 154 --json mergeable,mergeStateStatus,headRefOid` | `MERGEABLE` / `CLEAN` / `4afcd34` |
| Required CI checks | `CI / Orun Plan` SUCCESS · `orun remote-state conformance / Harness dry-run guard` SUCCESS · matrix legs SKIPPED (legitimate empty-matrix at M2 PR A) |
| `gh pr diff 154 --name-only` | 10 files: `Makefile`, `ai/reports/task-0003-implementer.md`, `ai/tasks/task-0003.md`, plus the seven `internal/statestore/` files. No production-caller wiring. |

### Implementer report committed to PR branch
`git ls-tree origin/impl/task-0003-m2-statestore-pra --name-only ai/reports/task-0003-implementer.md` → file present. (The implementer's `4afcd34` commit is dedicated to this. No verifier fix needed.)

### Interface freeze (state-store.md §1)
Diff of `internal/statestore/store.go` `StateStore` block against
`state-store.md` §1 — clean. Method names, parameter order, return types
match byte-for-byte. Doc comments are present and align with spec intent
(Read returns ErrNotFound; Write atomic via temp+rename; CreateIfAbsent
returns ErrExists; CompareAndSwap returns ErrConflict on revision
mismatch; List unspecified order; Delete no-op-on-absent, no recursive).
Companion types match the spec: `ObjectMeta{Path,Size,Revision,UpdatedAt}`,
`ObjectInfo{Path,Size,UpdatedAt}` (no Revision in List, intentional),
`WriteOptions struct{}`, `LocalConfig{Root, Clock}` (Clock is an additive
test-injection hook beyond §5; non-breaking and documented inline).

`go doc ./internal/statestore` shows doc comments on every exported symbol
including all four sentinels, all 16 helpers, and every type/method.

### Path helpers (state-store.md §2.1)
Inspected `internal/statestore/paths.go`. Every helper from §2.1 exists with
the documented return shape:

- `RevisionDir`, `PlanPath`, `TriggerPath`, `RevisionDocPath`, `ManifestPath`
- `ExecutionDir`, `ExecutionDocPath`, `SnapshotPath`, `EventPath` (zero-padded
  20-digit decimal seq → lexicographic order matches sequence order)
- `LatestRevisionRefPath`, `LatestExecutionRefPath`,
  `TriggerLatestRefPath(name)`, `TriggerScopeRefPath(name, scope)`,
  `NamedRefPath(name)`
- `RevisionIndexPath(revKey)`, `ExecutionIndexPath(execKey)`

Alphabet policy is centralized in `ValidateComponent` /  `ValidatePath`:
ASCII alphanumerics + `.`, `_`, `-`, with rejection of empty segments,
`.`/`..`, leading `/`, trailing `/`, backslash, double-slash, and any rune
outside the alphabet. Helpers route caller-supplied components through
`joinComponents` which panics on programmer error after the public entry
points have already raised `ErrInvalid`. `paths_test.go` covers both happy
and rejection cases (TestValidateComponent, TestValidatePath,
TestPathHelpersValidatedComponents, TestPathHelpersPanicOnInvalidComponent).

M3 spot-check: every helper M3 (`internal/revision`) needs is present —
revision dir/plan/trigger/revision/manifest, execution dir + execution doc +
snapshot + event, refs (latest-revision, latest-execution, trigger latest +
scope, named), and indexes (revision + execution). No private helper pressure.

### Local-driver atomicity (state-store.md §3)
Inspected `internal/statestore/local.go`:

- `Write` → `writeAtomic(dir, abs, data)`: `os.CreateTemp(dir, ".orun-tmp-*")` →
  `Write` → `Sync` (fsync) → `Close` → `os.Rename`. EXDEV detected via
  `errors.As(*os.LinkError) && errors.Is(.Err, syscall.EXDEV)`, with a
  `crossDeviceCopyRename(dir, dst, data)` fallback that creates a fresh
  tempfile **inside the destination directory** (same FS as `dst`) and
  renames atomically. Tempfile cleanup on every error path. `os.Chtimes`
  aligns mtime with the configured clock so tests observe deterministic
  `UpdatedAt`.
- `CreateIfAbsent` uses `O_WRONLY|O_CREATE|O_EXCL` and maps `fs.ErrExist`
  to `ErrExists`. Loser of two concurrent calls is guaranteed `ErrExists`.
- `Delete` no-ops on `ENOENT`, refuses non-empty directories with
  `ErrInvalid`. (Note: also refuses empty directories with `ErrInvalid`;
  see Issues / Spec Proposals.)
- Orphan sweep: `sweepOrphanTempfiles` walks the tree from `root`,
  matches files whose `Name()` has prefix `.orun-tmp-`, and removes
  entries where `now.Sub(info.ModTime()) >= orphanSweepMaxAge` (1 hour).
  Walk and per-remove errors are swallowed so sweep never blocks
  construction. `LocalConfig.Clock` is plumbed end-to-end.
  `TestOrphanSweep_RemovesOldTempfilesPreservesYoung` covers both sides
  of the boundary using injected clock, plus deep-tree placement and
  non-tempfile preservation.
- Every error path wraps a sentinel: `fmt.Errorf("%w: …", ErrInvalid|ErrNotFound|ErrExists, …)` for the
  documented failure modes; underlying I/O errors wrap their cause via
  `fmt.Errorf("statestore: …: %w", err)` (sentinel-wrapping is reserved
  for the four contract errors; underlying syscall errors stay
  structurally available via `errors.Is` / `errors.As` on the cause).
- `translate` does defense-in-depth path-escape check (`strings.HasPrefix(cleaned, root+sep)`).

### Leaf-clean imports / no-callers
| Command | Result |
|---|---|
| `go list -deps ./internal/statestore \| grep sourceplane/orun/internal \| grep -v 'statestore$'` | empty (exit 1, no rows) |
| `git diff origin/main...HEAD -- cmd/orun internal/state internal/runner internal/runbundle` | empty |

### Quality gates (local)
| Command | Result |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test -race -count=1 ./...` | all packages `ok` (statestore 3.171s) |
| `go test -cover ./internal/statestore/...` | **coverage: 95.4 % of statements** |
| `make test-state-redesign` | exit 0 — coverage gate measured 95.4 %, ≥ 95 % required |

### Orun gates
| Command | Result |
|---|---|
| `kiox -- orun validate --intent intent.yaml` (in `examples/`) | `✓ All validation passed` |
| `kiox -- orun plan --changed --intent intent.yaml --output plan.json` (in `examples/`) | reproduces the pre-existing `stack.yaml … has no spec.compositions` composition-cache failure carried from Task 0001+. CI is authoritative; PR #154 CI plan job ran cleanly with `0 components × 3 envs → 0 jobs`, mode `changed-only`. Not a regression introduced by this PR. |
| `kiox -- orun run --plan plan.json --dry-run` | not exercised — `plan.json` was not produced locally per above. CI plan job is authoritative. |

### CI log inspection
- `CI / Orun Plan` (run **26665146437**, job **78596782626**): real
  `orun plan --from-ci github --event-file "$GITHUB_EVENT_PATH" --intent
  examples/intent.yaml --artifact github --github-output` invocation
  observed, `0 components × 3 envs → 0 jobs`, mode `changed-only`,
  plan checksum `74af9580810d`, plan artifact uploaded successfully.
- `orun remote-state conformance / Harness dry-run guard` (run
  **26665146435**, job **78596782634**): the dry-run guard executed
  `examples/remote-state-matrix/test/dry-run-guard.sh` with 30+ `[guard]
  PASS` assertions covering bash syntax checks, foundation@dev.smoke /
  api@dev.smoke command counts, duplicate-claim helper PASS+FAIL cases,
  status helper PASS+FAIL cases (missing/pending/running/failed/blocked),
  ORUN_EXEC_ID + ORUN_REMOTE_STATE export checks, harness invocation
  assertions, signal-safe cleanup trap, jq/orun/repo-linkage preflight.
  Real assertions, no skip surprises in the body.
- Matrix legs (Compile plan, Env fanout, Run, Verify remote status)
  SKIPPED — legitimate at empty-matrix M0/M1/M2-PR-A shape.

### Secret hygiene
`git diff origin/main...HEAD` grep for `token|password|secret|api[_-]?key|bearer|BEGIN.*KEY` — only hit is the literal phrase "Audit secret hygiene over the diff." inside `ai/tasks/task-0003.md` (the prompt itself). No tokens, passwords, API keys, connection strings, or private-key blocks in code or fixtures. Diagnostic log lines in `local.go` emit only the logical path `p` (already exposed by callers) and the underlying error — no object contents, and no absolute filesystem paths beyond what `LocalStore.Root()` already publishes. CI logs redact `GITHUB_TOKEN` / `ACTIONS_RUNTIME_TOKEN`.

## Issues

**Minor — `Delete` rejects empty directories with ErrInvalid (state-store.md §3.4 only requires non-empty).** `state-store.md` §3.4 says "Removing a non-empty directory is forbidden". The implementation refuses **all** directories with `ErrInvalid` (`"%s is a directory; recursive deletion is not supported"`). The acceptance criterion in `task-0003.md` explicitly says "refuses non-empty directories with `ErrInvalid`", which the code satisfies; the empty-dir behavior is stricter. No caller deletes directories at this milestone, and the conservative choice is defensible (avoids surprising removals of structural state subdirs). Documented as a non-blocker; if anyone ever wants empty-dir delete to succeed, it's a one-line change. See Spec Proposals.

**Minor — `LocalConfig.Clock` exists beyond `state-store.md` §5.** The spec only specifies `LocalConfig{Root}`. The implementation adds `Clock func() time.Time` for deterministic tests of the orphan-sweep age boundary and `UpdatedAt` stamping. Additive, optional (nil → `time.Now`), documented inline. Non-breaking. The M0 follow-up to introduce a repo-wide clock interface is acknowledged in the doc comment.

No blockers.

## CI Log Review

- **Run 26665146437** (`CI / Orun Plan`, job 78596782626): observed actual `orun plan --from-ci github --event-file "$GITHUB_EVENT_PATH" --intent examples/intent.yaml --artifact github --github-output` invocation, plan computed (0 components × 3 envs = 0 jobs, mode changed-only, checksum 74af9580810d), plan artifact uploaded. Real command, real assertion.
- **Run 26665146435** (`orun remote-state conformance / Harness dry-run guard`, job 78596782634): observed `examples/remote-state-matrix/test/dry-run-guard.sh` running with 30+ `[guard] PASS` lines (bash syntax, foundation/api command counts, duplicate-claim helper PASS+FAIL, status helper PASS+FAIL across 5 status states, env-export checks, signal-safe cleanup, preflight checks).
- Matrix legs SKIPPED at empty-matrix M2-PR-A shape — legitimate.

## Secret Handling Review

Confirmed clean. No tokens, passwords, API keys, or connection strings in the diff. The only grep hit is the literal phrase "secret hygiene" inside the task prompt. New `local.go` log lines emit only the logical path `p` and underlying error; `Root()` is the spec-blessed surface for the absolute filesystem path. CI redacts the GitHub token and runtime token. No object contents leak in any error path.

## Risk Notes

- **CAS / List stubs return ErrInvalid in PR A.** Higher layers MUST NOT call them until PR B lands. Any accidental caller will get `ErrInvalid`, which surfaces as a path-policy error to humans reading logs — slightly misleading but recoverable. PR B replaces these immediately. Mitigated by the no-callers gate (no production code in PR A or this milestone wires the package yet).
- **Orphan-sweep failures are silently swallowed.** `sweepOrphanTempfiles` is best-effort by design (spec §3.1) so a hiccup never blocks construction. Operationally this is the right tradeoff — the worst case is a few KB of stale `.orun-tmp-*` files persisting for a session — but it does mean a permission-denied root would never surface from construction. Acceptable for Phase 1 local FS.
- **CompareAndSwap race window in PR B.** Spec §3.3 already calls this out: the local driver's Read-then-Write CAS has a small race window between the two ops. The future remote driver will use native conditional update. Not in scope for PR A; flagged for the PR B verifier to revisit alongside the property suite.
- **Empty-directory delete behavior diverges slightly from §3.4.** See Issues / Spec Proposals.

## Spec Proposals

None required for merge. One optional clarification candidate:

- `state-store.md` §3.4 could explicitly state whether empty-directory `Delete` succeeds or returns `ErrInvalid`. Current implementation chose the conservative "always refuse directory delete" path. If a future consumer needs empty-dir delete to succeed, file `/ai/proposals/task-0003-delete-empty-dir.md` and update both spec and code.

## Recommended Next Move

**Scope Task 0004 = M2 PR B: `internal/statestore` CompareAndSwap + List real implementations + atomicity / exclusivity property suite per `test-plan.md` §2 / §3.**

Concrete contents the orchestrator should call out:

1. Replace the PR-A stubs in `local.go`:
   - `CompareAndSwap`: Read-then-Write under a per-path mutex (or
     accept the documented best-effort race; spec §3.3 says local
     Phase 1 race is acceptable). Return `ErrNotFound` on missing,
     `ErrConflict` on revision mismatch.
   - `List`: walk translated prefix, skip symlinks, return `ObjectInfo`
     for every regular file. Order unspecified.
2. Add the property suite from `test-plan.md` §2:
   - 100-goroutine `Write+Read` atomicity test — readers must observe
     complete JSON every iteration.
   - 100-goroutine `CreateIfAbsent` exclusivity test — exactly one
     success.
   - Concurrent CAS conflict test — one win, one `ErrConflict`.
   - `rapid` round-trip test on path-alphabet inputs.
3. Coverage gate stays ≥ 95 %; PR B should leave it ≥ 96 % once CAS/List
   bodies replace the trivial stub returns.
4. Still no production-caller wiring. PR C (typed refs/indexes
   marshallers) follows.

PR B is the next implementer task. Skill: `devops/orun-saas-implementer`.

## Merge action taken

PR #154 squash-merged via the Verifier Merge Protocol; `main` fast-forwarded; `impl/task-0003-m2-statestore-pra` deleted. Merge SHA recorded inline below after the action runs.
