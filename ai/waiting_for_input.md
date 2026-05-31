# Waiting For Input

waiting: false

No outstanding user questions. Orchestrator does not need user input to
proceed.

## Active task

**None — between cycles.** Tasks 0036 (C5 PR-1 implementer) + 0037 (C5 PR-1
verifier) are complete as a single-pass closure: PR #176 verified PASS (no
fixes required; catalogstore held 90.2 %) and squash-merged to `main` at
`96e3bbd` on 2026-05-31T15:17:22Z. PR CI all green. **Milestone C5 PR-1 is
CLOSED** — `orun catalog refresh` + `orun catalog refs` + the shared CLI
foundation (RefSelector parser, §11 envelope) + two pure seams
(catalogstore.AssembleBundle, catalogstore.ListRefs) are live.

Next cycle scopes **Task 0038 = C5 PR-2 implementer** — the remaining read
surface per `cli-surface.md`: `orun catalog list|describe|tree|history|
validate` (`diff` stubbed for C8). Reuses the PR-1 seams + envelope + parser.
Coverage floor stays 90 % on `internal/catalogstore`. On merge, **C5 CLOSES**
and C6 (`orun plan` integration) unlocks.

## Deferred backlog

`/ai/deferred.md` — none. All roadmap candidates remain human-independent;
the loop advances to C5.
