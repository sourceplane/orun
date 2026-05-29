# Spec Clarification: rapid import path in test-plan.md

**Filed by:** Task 0002 Implementer (M1 — `internal/triggerctx`)
**Date:** 2026-05-30
**Target spec:** `specs/orun-state-redesign/test-plan.md`
**Type:** Clarification (non-behavioral edit; corrects a stale import path)

## Problem

`specs/orun-state-redesign/test-plan.md` references the rapid property-testing
library twice using the legacy import path:

- §1 ("Test tiers") — `github.com/flyingmutant/rapid (already pinned by the TUI
  cockpit spec).`
- §3 ("Property-based tests") — the example code block does not name a path,
  but its intro paragraph inherits the §1 reference.

The repository's `go.mod` (post-M0) pins the canonical current path:

```
pgregory.net/rapid v1.1.0
```

`github.com/flyingmutant/rapid` is the original (pre-transfer) import path for
the same library by the same maintainer; it has been deprecated for years and
is no longer published. Any implementer who takes the spec text at face value
will either (a) add a second require line that fails `go mod tidy`, or (b)
type the wrong import and fail to compile.

`specs/orun-state-redesign/design.md §13` already correctly names
`github.com/flyingmutant/rapid` as "already pinned per the TUI cockpit spec" —
this proposal addresses that line too, since it shares the same drift.

## Proposed edit

Replace every occurrence of `github.com/flyingmutant/rapid` in
`specs/orun-state-redesign/` with `pgregory.net/rapid`. Specifically:

1. `test-plan.md §1` — update the introduction paragraph.
2. `design.md §13` — update the dependency-additions section.

The library is the same; the API is the same; the only delta is the import
path. No behavioral spec change is implied.

## Implementer behavior in M1

Task 0002 uses `pgregory.net/rapid` in `internal/triggerctx/ids_test.go`
(`TestTriggerKey_PropertyStabilityAndFormat`,
`TestTriggerKey_PropertyDirtyAlwaysLocalDirty`). This proposal is filed so the
spec text catches up to the code.

Per `agents/orchestrator.md`'s "Spec Change Proposals" rules, a pure
clarification may be folded into the implementing PR. The PR for Task 0002
includes the spec edit alongside this proposal.

## Acceptance

- [ ] `rg "flyingmutant" specs/` returns zero matches after the edit.
- [ ] `go test ./internal/triggerctx/...` continues to pass (no code change
      needed — the implementer chose the canonical path up front).
