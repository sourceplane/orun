# Test Plan

Phase 1 ships with three test tiers:

1. **Unit tests** colocated with the package under test (`*_test.go`).
2. **Property-based tests** using `pgregory.net/rapid` (the canonical current
   import path; already pinned at `v1.1.0` by the TUI cockpit spec — see
   `ai/proposals/task-0002-spec-update.md` for the path clarification).
3. **End-to-end CLI tests** under `cmd/orun/state_e2e_test.go`.

A `make test-state-redesign` target gates coverage on the four new packages.

---

## 1. Coverage targets

| Package | Statement coverage | Why |
|---------|--------------------|-----|
| `internal/triggerctx` | ≥ 90 % | Public model + key helpers must be airtight. |
| `internal/revision` | ≥ 90 % | Writer order and resolver matrix must be exhaustive. |
| `internal/statestore` (local driver) | ≥ 95 % | Foundation. Error paths matter as much as happy paths. |
| `internal/executionstate` | ≥ 90 % | Bridge is high-risk; mirror failure paths must be exercised. |

A coverage drop in any of these packages fails CI.

---

## 2. Atomicity suite (statestore)

```
N := 100
workers spawn:
    for i := 0; i < N; i++ {
        go store.Write(ctx, "atomic.json", encode(payload{i}))
    }
    for i := 0; i < N; i++ {
        go func() {
            for {
                b, _, err := store.Read(ctx, "atomic.json")
                if err == nil {
                    p := decodeStrict(b)        // must always succeed
                    require.NoError(t, err)
                }
                if ctx.Done() { return }
            }
        }()
    }
assert: zero decode errors over the run
```

Plus:

- **Exclusivity**: 100 goroutines call `CreateIfAbsent` on the same path;
  exactly one returns nil, the rest return `ErrExists`.
- **CAS conflict**: two goroutines `CompareAndSwap` with the same `oldRev`;
  one wins, the other returns `ErrConflict`.
- **Crash safety**: simulate by writing via temp + abort before rename; the
  reader sees only the previous version.

---

## 3. Property-based tests

### 3.1 `internal/triggerctx`

```go
rapid.Check(t, func(t *rapid.T) {
    scope := rapid.StringMatching(`[a-z0-9-]{1,20}`).Draw(t, "scope")
    sha   := rapid.StringMatching(`[a-f0-9]{40}`).Draw(t, "sha")
    occ := newOccurrence(scope, sha)
    k1 := TriggerKey(occ)
    k2 := TriggerKey(occ)
    require.Equal(t, k1, k2)                                              // stability
    require.Regexp(t, `^trg-[a-z0-9-]+-(([a-f0-9]{7})|local-dirty|no-git)$`, k1) // format
})
```

### 3.2 `internal/revision`

- Revision key uniqueness: for arbitrary `(trig, planHash)` tuples, identical
  inputs produce identical keys; differing inputs produce differing keys
  modulo collision suffix.
- Collision suffix correctness: forcing key collision N times yields
  `-x2`…`-xN+1` without gaps.

### 3.3 `internal/executionstate`

- `NextExecutionKey` monotonicity: N concurrent `CreateExecution` calls
  produce distinct keys in `run-001`…`run-NNN`.
- `SanitizeExecID`: arbitrary input strings produce keys matching
  `^[a-z0-9-]+$` and bounded length.

---

## 4. End-to-end test (`cmd/orun/state_e2e_test.go`)

```
1. Create a temp workspace with a minimal intent.yaml + one component.
2. Run `orun plan` (programmatic invocation through cobra command).
3. Assert .orun/revisions/<key>/{plan,trigger,revision,manifest}.json exist.
4. Assert .orun/refs/latest-revision.json points to the new key.
5. Assert legacy .orun/plans/<checksum>.json + latest.json also exist (compat).
6. Run `orun run --dry-run`.
7. Assert .orun/revisions/<key>/executions/run-001/execution.json exists.
8. Assert .orun/refs/latest-execution.json updated.
9. Assert .orun/indexes/executions/run-001.json updated.
10. Run `orun status` (capture stdout). Assert it reports run-001 / completed.
11. Run `orun logs` (capture stdout). Assert it does not error.
12. Run `orun describe revision latest`. Assert it prints the revision key
    and trigger fields.
13. Run `orun get plans`. Assert the revision row appears.
14. Run `orun state migrate --dry-run`. Assert exit 0, summary printed.
15. Run `orun state migrate`. Assert idempotence on second run (file checksums
    of the four canonical revision documents unchanged).
```

The test uses `internal/testfx/statefs.NewWorkspace` for isolation. Each
sub-assertion lives in its own `t.Run` for granular failure attribution.

---

## 5. Compatibility tests

Separate from the E2E walk:

- `orun plan -o /tmp/plan.json` writes both the user-specified file and the
  revision layout.
- `orun run --plan /tmp/plan.json --dry-run` synthesizes a `system.manual`
  revision in-memory and creates an execution under it (assert the execution
  exists under a `system.manual` revision).
- `orun run <legacy-hash>` (with only `.orun/plans/` present, no revisions)
  succeeds via the legacy fallback.
- `orun status` against a pure-legacy workspace prints latest legacy
  execution via fallback.

---

## 6. Negative tests

- Invalid `triggerType` in `trigger.json` → reader returns typed
  unmarshal error; CLI prints actionable diagnostic.
- Corrupt `refs/latest-revision.json` → resolver falls back to scan + logs a
  warning.
- Missing `executions/run-001/execution.json` but ref points to it → fallback
  scan, never crash.
- `orun run --exec-id "evil/../path"` → `SanitizeExecID` strips path
  separators; execution key is safe.

---

## 7. Performance smoke

Not a gate, but a `Benchmark` for sizing:

- `BenchmarkLocalStore_Write` for 4 KiB JSON payloads — must stay within
  3× the cost of `os.WriteFile` (temp+rename overhead).
- `BenchmarkResolveLatestRevision` over a workspace with 1000 revisions —
  must complete in under 5 ms thanks to refs.

If either regresses by >25 % across the milestone series, treat as a release
blocker.

---

## 8. CI integration

```
make test-state-redesign:
    go test -race -coverprofile=cov.out \
        ./internal/triggerctx/... \
        ./internal/revision/... \
        ./internal/statestore/... \
        ./internal/executionstate/...
    go tool cover -func=cov.out | awk '...'  # enforce coverage thresholds
    go test -race ./cmd/orun -run TestStateE2E
```

CI hooks:

- Required check on every PR touching the four packages or the CLI commands
  listed in `cli-surface.md`.
- Soft check on every other PR (runs but does not block).
