# Task 0037 — C5 PR-1 Verifier

Verify PR #176 (`feat/c5-pr1-catalog-cli`), the Task 0036 implementer output:
the `orun catalog refresh` + `orun catalog refs` commands, the shared CLI
foundation (root, RefSelector parser, §11 envelope), and the two new pure
seams (`bundle.go` AssembleBundle, `listrefs.go` ListRefs).

Implementer report: `ai/reports/task-0036-implementer.md`
Implementer prompt: `ai/tasks/task-0036.md`
Spec: `specs/orun-component-catalog/cli-surface.md` §1/§2/§8/§11/§12

## Acceptance criteria (from task-0036)

- `orun catalog refresh` resolves the current workspace, builds + commits a
  snapshot, reports created vs reused (idempotent re-write exits 0), prints
  the dirty-worktree banner when dirty, emits the `CatalogRefreshResult`
  envelope under `--json`, and honours §2 exit codes.
- `orun catalog refs` enumerates all refs with source/catalog keys +
  `authoritative`; `--json` emits `CatalogRefsResult`.
- `--source`/`--catalog-source`/`--catalog-snapshot` parse into ONE shared
  `RefSelector` helper with unit tests for every selector form + the
  malformed-selector error.
- The two new seams ship with unit + golden/determinism tests.
- `orun catalog` root with a subcommand index help page.

## Verification steps

1. Scope review: PR maps to exactly one task; no unrelated churn; no
   spec-drift without a proposal under `ai/proposals/`.
2. Code inspection:
   - No raw FS (`os`/`filepath`) in `internal/catalogstore` non-test files
     except `paths.go`.
   - `catalogstore` does NOT import `catalogresolve` in a way that inverts
     the dependency; bundle/listrefs live in `catalogstore` for the right
     reason.
   - `AssembleBundle` performs no I/O; deterministic.
   - Exit-code plumbing in `main.go` unwraps `ExitCode()` correctly.
   - No secrets / no token-bearing log lines.
3. Local checks: `go build ./...`, `go vet`, `go test -race -count=1` on
   `cmd/orun` + `internal/{catalogstore,catalogresolve,sourcectx}`,
   `make verify-generated`.
4. Coverage: re-measure `internal/catalogstore` (floor 90.2%). Path-(a)
   attach tests on the PR branch if a floor slips (recurring C4 pattern).
5. CI: confirm PR #176 checks green (test, Orun Plan, Harness dry-run).
6. Merge decision per the PASS/FAIL gate; squash-merge on PASS.

## Boundary

Do NOT expand scope to C5 PR-2 (list/describe/tree/history/validate). Those
reuse the shared parser + envelope + both seams and are a later PR.
