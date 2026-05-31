# Task 0029 — Verifier (C3: snapshot + graph builder + catalogHash)

Agent: Verifier

## Current Repo Context
- Phase 2 component-catalog work. Milestone **C3** implementer landed as
  PR #172 on branch `task-0028-catalog-c3-snapshot-graph` (head:
  `ffb5ee9` per `git log`; HEAD on `main` is `101b5d4`, the implementer
  report-only commit).
- C2 closed cleanly (PR #170 + #171). C3 wires
  `BuildCatalog(ctx, opts, ResolverInputs) (*CatalogView,
  []ValidationIssue, error)` on top of the existing `Resolve` —
  resolution-pipeline §1 stages 11 (graph) → 12 (catalogHash) → 13
  (snapshot assemble + key + back-fill).
- Implementer report at `ai/reports/task-0028-implementer.md` claims:
  three new sibling files in `internal/catalogresolve/`
  (`graph.go`, `catalog_hash.go`, `catalog_snapshot.go`) plus three
  test files; coverage on `internal/catalogresolve` 90.2 → 90.9
  (+0.7 pp); all Phase 1 + Phase 2 floors held; `make verify-generated`
  clean; `go test ./... -race -count=1` green; PR #172 mergeable, all
  CI checks green at brief time.
- Two durable assumptions to confirm against spec, not just code:
  `summary.systems` ← `spec.system` and `summary.domains` ← `spec.domain`
  (the spec's `resolution-pipeline.md` line 114 references `spec.system`;
  there is no `metadata.system` / `metadata.domain` on
  `ComponentManifest`). The implementer's choice is spec-consistent.
- No new proposals owed by orchestrator. No deferred entries open.

## Objective
Verify PR #172 against Task 0028's acceptance criteria, the C3
"done when" list in `specs/orun-component-catalog/implementation-plan.md`,
the resolver-pipeline contract in
`specs/orun-component-catalog/resolution-pipeline.md` §1 stages 11–13,
identity-and-keys §3 + §9, and `data-model.md` §2 + §4. Confirm
production-grade basics, then PASS-and-merge or FAIL-and-block per the
Verifier Merge Protocol.

## PR Boundary
Exactly the boundary scoped by Task 0028:
- Three new files inside `internal/catalogresolve/` (`graph.go`,
  `catalog_hash.go`, `catalog_snapshot.go`) plus their `*_test.go`
  siblings.
- New exported entrypoint `BuildCatalog(ctx, opts, ResolverInputs)
  (*CatalogView, []ValidationIssue, error)` and types
  `ResolverInputs`, `CatalogView`, `ErrResolverInputsIncomplete`.
- No edits to existing source files in `catalogresolve` beyond strict
  wiring (additive convention from C2 PR-1). No edits anywhere in
  `internal/catalogmodel/`, `internal/sourcectx/`, Phase 1 packages,
  or `internal/catalogsync`. No FS writes. No CLI surface. No
  integration with `orun plan` / `orun run`. No
  `ComponentHistoryEvent`, `internal/catalogdiff`, or `catalogstore`.
- If the PR diff exceeds this surface, that is a FAIL signal —
  flag overreach.

## Read First
- `ai/tasks/task-0028.md` (the implementer prompt, full)
- `ai/reports/task-0028-implementer.md` (the implementer's claims)
- `agents/orchestrator.md` — Verifier Standard + Verifier Merge Protocol
- `specs/orun-component-catalog/README.md` (read order)
- `specs/orun-component-catalog/implementation-plan.md` §C3 (done-when)
- `specs/orun-component-catalog/resolution-pipeline.md` §1 stages 11–13
- `specs/orun-component-catalog/identity-and-keys.md` §3 (key shape)
  and §9 (catalogHash input ordering)
- `specs/orun-component-catalog/data-model.md` §2
  (`CatalogSnapshot` schema, `objects.components[*].path`,
  cross-reference rules) and §4 (`CatalogGraph` ordering rules)
- `specs/orun-component-catalog/test-plan.md` §1 coverage targets, §3
  property-based determinism tests
- Reference only: `specs/orun-state-redesign/data-model.md` (Phase 1
  invariant — must not be weakened)
- PR #172 diff and CI run logs (use `gh pr view 172 --json` / `gh pr
  diff 172` / `gh run view`)

## Required Outcomes
Produce `ai/reports/task-0029-verifier.md` with:
- `Result: PASS` or `Result: FAIL` and a short justification.
- `Checks` — every command run with exit status.
- `Issues` — empty list if clean, else severity-tagged.
- `Risk Notes` — residual risk after verification.
- `Spec Proposals` — links if any drift demands a proposal.
- `Recommended Next Move` — PASS ⇒ scope C4 implementer; FAIL ⇒ exact
  remediation surface inside Task 0028's PR.

## Non-Goals
- No scope expansion. Do not request features beyond C3.
- Do not bump `active_milestone` past C3 inside the verifier report —
  the orchestrator does that next cycle.
- Do not start C4 work, even speculatively, in this PR.
- Do not edit specs from inside the verifier task; raise a proposal if
  a real drift is found.

## Constraints
1. Use `/Users/irinelinson/.local/bin/kiox -- orun ...` if `kiox` is not
   on PATH.
2. Local-only Phase 2: no HTTP, no SaaS, no DB.
3. Phase 1 coverage floors are byte-for-byte: `internal/statestore`
   ≥ 95.7 %, `internal/revision` ≥ 90.3 %, `internal/executionstate`
   ≥ 90.0 %, `internal/triggerctx` passes.
4. Phase 2 floors held: `internal/catalogmodel` ≥ 91.1 %,
   `internal/sourcectx` ≥ 91.1 %, `internal/catalogresolve` ≥ 90.2 %.
   Implementer claims 90.9 % on catalogresolve — confirm.
5. `make verify-generated` must be clean on the PR head.
6. No probabilistic coverage gaps. If `go test -coverprofile` on
   `catalogresolve` shows any function whose coverage depends on rapid
   seed luck, the verifier must require a deterministic backstop test
   in the same PR (mirror the C1/C2 pattern).
7. The resolver MUST NOT invent `authoritative`, `preview`,
   `sourceSnapshotKey`, `catalogInputHash`, `headRevision`,
   `treeHash`, or `workingTree`. Confirm by reading
   `validateResolverInputs` and the call sites.
8. Preserved CLI workflows are unaffected (Phase 2 invariant).

## Integration Notes
- `ResolverInputs` is fully caller-supplied; `BuildCatalog` rejects
  missing fields with `ErrResolverInputsIncomplete`. Walk the
  `validateResolverInputs` source and confirm each required field is
  enforced.
- Source key back-fill (`Source.{SourceSnapshotKey,
  CatalogSnapshotKey, HeadRevision, TreeHash, WorkingTree}`) happens
  AFTER `manifestHash` was already computed at C2 stage 10. Confirm in
  code that `manifestHash` is not recomputed after back-fill, and that
  the existing `hash.go` exclusion of the `Source` block from the
  hashed payload is unchanged.
- `summary.systems` ← `spec.system`; `summary.domains` ← `spec.domain`
  is spec-consistent (`resolution-pipeline.md:114`). Confirm the
  computed values equal the sorted-distinct enumeration over resolved
  manifests.
- `objects.components[*].path = "components/<name>/manifest.json"` is
  the writer's expected on-disk layout (data-model.md §2). Confirm
  the resolver emits exactly that path string per component.
- `catalogSnapshotKey` width 8 per identity-and-keys.md §3.
  Collision-policy width expansion belongs to C4; the resolver must
  emit the un-suffixed form only.

## Acceptance Criteria
1. PR #172 `mergeable=MERGEABLE`, branch up-to-date, all required CI
   checks `SUCCESS` (Orun Plan, Harness dry-run guard, state-redesign
   tests). No `FAILURE`/`ERROR` rows in `statusCheckRollup`.
2. PR diff is bounded to the surface above. No collateral edits.
3. Local `go build ./...` exit 0; `go vet ./...` exit 0;
   `go test ./... -race -count=1` exit 0; `make verify-generated`
   exit 0.
4. `internal/catalogresolve` coverage on PR head ≥ 90.2 % (verify the
   90.9 % claim with `go test -coverprofile=cover.out
   ./internal/catalogresolve/... && go tool cover -func=cover.out
   | tail -1`).
5. T-IDK-1 (1000 random orderings ⇒ identical `catalogHash`) is
   present, uses `pgregory.net/rapid`, and passes.
6. Owner-edit acceptance: a deterministic test asserts a
   `metadata.owner` edit changes both `manifestHash` AND `catalogHash`.
7. Provenance-only stability: a deterministic test asserts a
   `resolution.inheritedFrom` edit does NOT change `manifestHash` AND
   does NOT change `catalogHash` (T-IDK-2 propagated to the C3 layer).
8. Two consecutive `BuildCatalog` calls produce byte-identical encoded
   `(*CatalogSnapshot, []*CatalogGraph)` (a determinism test exists
   and passes).
9. `summary.*` counts equal sorted-distinct enumeration (test
   present and passing for components, systems, apis, resources,
   owners, domains).
10. `catalogSnapshotKey` matches `^cat-[a-f0-9]{6,16}$` and passes
    `catalogmodel.ValidateCatalogSnapshotKey` (test present).
11. `ErrResolverInputsIncomplete` is returned (and is `errors.Is`-
    compatible) when any required `ResolverInputs` field is missing.
12. Phase 1 floors held byte-for-byte; Phase 2 floors held.

## Verification
1. `git fetch origin && git checkout task-0028-catalog-c3-snapshot-graph
   && git pull --ff-only origin task-0028-catalog-c3-snapshot-graph`
2. `gh pr view 172 --json title,state,mergeable,mergeStateStatus,
   statusCheckRollup,headRefName,baseRefName,files`
3. `gh pr diff 172 | head -400` (then page) — confirm bounded surface.
4. `go mod tidy && go build ./... && go vet ./...`
5. `go test ./... -race -count=1`
6. `go test -coverprofile=cover.out ./internal/catalogresolve/...
   && go tool cover -func=cover.out | tail -1`
7. `make verify-generated`
8. `/Users/irinelinson/.local/bin/kiox -- orun validate
   --intent intent.yaml` (if `intent.yaml` exists)
9. `/Users/irinelinson/.local/bin/kiox -- orun plan --changed
   --intent intent.yaml --output plan.json` (if applicable)
10. `/Users/irinelinson/.local/bin/kiox -- orun run --plan plan.json
    --dry-run --runner github-actions` (record no-op when no jobs)
11. `gh run view <run-id> --log` for the latest CI run on PR #172;
    confirm the test job actually executed Go tests and the harness
    dry-run guard exercised the expected commands (per Verifier
    Standard "inspect logs, not just status summaries").
12. Read each new file:
    - `internal/catalogresolve/graph.go` — confirm sorted nodes (by
      `key`) and sorted edges (by `(from, to, type, optional)`) for all
      five graphs.
    - `internal/catalogresolve/catalog_hash.go` — confirm the hash
      input ordering is exactly identity-and-keys §9: `(catalogInputHash,
      sorted (componentKey, manifestHash) pairs, canonical encoding of
      each CatalogGraph in the §9 fixed order, resolver.resolverVersion)`.
    - `internal/catalogresolve/catalog_snapshot.go` — confirm
      `validateResolverInputs` rejects every missing
      caller-supplied field; `assembleSnapshot` does not invent any
      provenance value; source key back-fill happens after
      `manifestHash` is already finalised.
13. Read each new test file and confirm the determinism tests are
    deterministic backstops (not rapid-only) for the four signals
    listed in Acceptance #5–#8.

## PR Creation Requirement
The Implementer has already created PR #172. The verifier does not
open a new PR. If the verifier needs to attach a small verification-
only fix (e.g. a deterministic backstop test, a doc nit), commit it
to `task-0028-catalog-c3-snapshot-graph`, push, and wait for CI to
re-run before merging.

## Verifier Merge Protocol (per `agents/orchestrator.md`)
- If PASS:
  1. `gh pr merge 172 --squash --delete-branch` (or `--admin` only if
     branch protection blocks the squash and the policy allows it).
  2. `git checkout main && git pull --ff-only origin main`.
  3. `git status --short` — must be clean.
  4. Confirm post-merge `git log --oneline -1` shows the squash commit.
- If FAIL:
  1. Leave the PR open.
  2. Document each blocker in `Issues` with severity, file:line, and
     the exact remediation expected.
  3. Do NOT merge under any circumstance with unresolved blockers.
- Never merge with failing CI checks. Re-poll
  `gh pr checks 172` immediately before `gh pr merge`.

## When Done Report
- Path: `ai/reports/task-0029-verifier.md`
- Required sections: `Result`, `Checks`, `Issues`, `CI Log Review`,
  `Coverage Numbers`, `Spec Proposals`, `Risk Notes`,
  `Recommended Next Move`.
- After merge (PASS path): commit the verifier report to `main` along
  with `ai/state.json`, `ai/context/current.md`, and the appended
  `ai/context/task-ledger.md` entry. Push and confirm `git status` is
  clean before ending the verifier task.
