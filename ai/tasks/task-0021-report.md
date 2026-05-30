# Task 0021 — M6 implementer report

## Summary

Closes M6 (end-to-end + property gates) for the orun-state-redesign spec.
M6 is test-only / coverage-gate work: no production code paths were
modified. The PR adds an end-to-end walk through the revision-first
state pipeline, two property-based regression gates around the
revision-key derivation + collision resolver, targeted unit coverage to
hold the `internal/revision` floor at ≥ 90 %, and wires the new tests
into `make test-state-redesign` under `-race`.

## Files changed (LOC)

| File                                                  | LOC | Kind |
|-------------------------------------------------------|-----|------|
| `cmd/orun/state_e2e_test.go`                          | 340 | NEW  |
| `internal/revision/keys_property_test.go`             | 188 | NEW  |
| `internal/revision/m6_coverage_test.go`               | 160 | NEW  |
| `Makefile` (test-state-redesign target)               |  +7 / -5 | MOD |

Total: 4 files, ~700 LOC test-only.

## Test surface

* `TestStateE2E` (cmd/orun/state_e2e_test.go) — 14 sub-steps covering
  test-plan.md §4 steps 2–15: plan synthesis → revision documents on
  disk → refs/latest-revision.json → legacy plans/ mirror → execution
  setup + finalize → execution.json terminal status → refs/latest-execution.json
  → indexes/executions/<execKey>.json → read-side resolver (status/logs)
  → describe revision latest → get plans table → state migrate dry-run
  + idempotence (sha256 byte-equal across two non-dry runs).
* `TestRevisionKey_PropertyDeterminismAndDistinctness` — same inputs ⇒
  same key (determinism).
* `TestRevisionKey_PropertyDistinctInputsDistinctKeys` — meaningfully
  different inputs ⇒ different keys (rules out scope-truncation /
  short-hash regression). Draws that legitimately share the 8-char
  prefix are skipped, not failed.
* `TestResolveCollision_PropertySuffixContiguity` — N forced collisions
  yield `-x1`, `-x2`, …, `-xN` with no gaps and no skips.
* M6 coverage unit tests — `ScanLegacyPlanHashes` happy path + filter
  rules + nil-store error, `WriteLegacyNamedPlan` (nil store / reserved
  "latest" / invalid component / happy path), `RevisionKey` rejects
  empty trigger + short plan hash.

Property tests use `pgregory.net/rapid` (already a project dep). They
allocate isolated `LocalStore`s per iteration via `t.TempDir` so
`-race` is safe.

## Coverage (measured under -race)

| Package                          | Floor  | Measured | Δ vs main |
|----------------------------------|--------|----------|-----------|
| `internal/statestore`            | ≥ 95.0 | 95.7 %   | unchanged |
| `internal/revision`              | ≥ 90.0 | 90.3 %   | +5.4 pts  |
| `internal/executionstate`        | ≥ 90.0 | 90.0 %   | unchanged |
| `internal/triggerctx`            | ≥ 90.0 | (passes) | unchanged |

`internal/revision` was at 84.9 % on main tip; the floor was nominally
90.0 % but the gate had been silently dormant because nothing previously
exercised it under `make test-state-redesign`. M6 lifts it past the
documented floor without lowering the threshold (per task guardrails).

## Verification commands

```
go test ./... -race -count=1 -timeout 600s   # all green
make test-state-redesign                      # all 4 gates pass under -race
go vet ./...                                  # clean
go test ./cmd/orun/ -run TestStateE2E -race -count=3   # E2E stable across 3 iterations
```

## Notes for the verifier

* `cmd/orun/state_e2e_test.go` invokes the same package-level
  subroutines that `command_run.go`'s Cobra RunE handlers call
  (`synthesizeRevisionForRun`, `setupRevisionExecution`,
  `finalizeRevisionExecution`) rather than re-driving the full Cobra
  root via `os/exec`. This avoids paying for a full intent.yaml +
  component tree fixture per step while still asserting the exact same
  on-disk artefacts test-plan.md §4 enumerates. Each numbered spec
  step has its own `t.Run` subtest for unambiguous failure attribution.
* `step05_legacy_plan_mirror_present` strips the canonical `sha256:`
  prefix when constructing the expected mirror path —
  `writer.writeCompatibilityMirror` normalises through
  `normalizeLegacyChecksum` before persisting.
* The Makefile target now runs the E2E test and propagates `-race` to
  every command, so the coverage gate failures will be visible in CI
  output rather than swallowed by silent `-count=1` runs.

## What was NOT changed

* No production source files under `internal/` or `cmd/` were modified.
* No coverage thresholds were lowered.
* No new sentinels, no new public types, no new statestore paths.
* `MirrorModeHardlink` fate remains deferred (the M6 evidence is the
  E2E + bridge property test combo; no behavioural change observed
  that would justify removing the knob in this PR).
