# Test Plan

Two tiers: unit tests colocated with the changed packages, and an end-to-end CLI
walk (`cmd/orun/envscoping_e2e_test.go`). The correctness properties below are the
acceptance gate.

---

## 1. Correctness properties (the gate)

| # | Property | Where |
|---|----------|-------|
| **P1 — faithful plan** | The compiled plan's `metadata.selection` exactly reflects the flags; `run <plan>` executes that scope and nothing else. | `internal/expand`, e2e |
| **P2 — fail-closed mutating run** | A mutating `run` with no selection and no `--all-envs` does not implicitly mutate all envs (warns in Phase A, errors in Phase B). `--dry-run` is exempt. | `cmd/orun` |
| **P3 — pruning is faithful + visible** | Any edge whose endpoint is absent from the expanded plan is dropped, recorded as a `PrunedEdge`, warned, and present in `--json`. A full plan prunes nothing. | `internal/expand` |
| **P4 — promotion ordering** | In a plan containing related envs, dependents run after prerequisites; a **failed prerequisite blocks its dependents** in the same run. | `internal/planner/promotion` |
| **P5 — selection composition** | envs = trigger ∩ (`--env`/`--all-envs`); components = `--component`/`--changed`/all; `--env` outside trigger envs errors; `--all-envs`+`--env` errors. | `internal/expand`, `cmd/orun` |
| **P6 — determinism** | `metadata.selection` (incl. `prunedEdges`) is byte-identical across runs for the same inputs (sorted, no map-order leak). | unit |

A regression in P1–P4 fails CI.

---

## 2. Unit tests

### 2.1 Selection composition (`internal/expand`)

Table-driven over: `{trigger envs} × {--env} × {--all-envs} × {--component} ×
{--changed}` → expected `Selection{Envs, Components, Mode, AllEnvs}` or expected
error. Includes:

- `--all-envs` + `--env x` → error.
- `--env x` where `x ∉ intent.environments` → error.
- `--env x` where `x ∉ trigger-activated envs` → error.
- no flags → all envs, `mode:"full"`.

### 2.2 Pruning (`internal/expand`)

- `dev→staging→prod`, select `staging` → one `PrunedEdge{promotion, staging→dev}`,
  `staging→prod` kept only if `prod` selected (it isn't) so also pruned.
- component `api dependsOn shared`, select `api` only → one
  `PrunedEdge{component, api→shared}`.
- full plan → `prunedEdges: []`, `mode:"full"`.
- determinism: shuffle input maps, assert identical sorted `prunedEdges` (P6).

### 2.3 Promotion ordering (`internal/planner/promotion`)

- ordering edges present for envs in the plan; absent (pruned) for envs not in it.
- failed prerequisite job → dependents marked blocked/not-run (P4).
- inert `Satisfy` modes: a `previous-success`/`same-plan` config does not error.

---

## 3. End-to-end (`cmd/orun/envscoping_e2e_test.go`)

```
workspace: intent with envs {dev, staging, prod}, prod/staging promotion deps,
           components {api, web, shared}, api dependsOn shared.

1.  orun plan                      → mode:"full", all envs, prunedEdges:[]
2.  orun plan --env staging        → envs:[staging], warns staging→dev pruned;
                                     --json prunedEdges matches golden
3.  orun plan --all-envs           → mode:"full", allEnvs:true
4.  orun plan --component api       → component web/shared interplay; api→shared
                                     pruned with warning
5.  orun run                       → (Phase A) deprecation WARNING, runs all
                                     (Phase B) ERROR, exit non-zero
6.  orun run --dry-run             → no warning, read-only, all envs
7.  orun run --env staging         → runs staging only
8.  orun run --all-envs --yes      → runs all; dev→staging→prod ordered
9.  fail a dev job, orun run --all-envs --yes
                                   → staging/prod dependents blocked (P4)
```

Each step is its own `t.Run`. Uses an isolated temp workspace.

---

## 4. Negative tests

- `orun run --all-envs --env x` → error (mutually exclusive).
- `orun plan --env nope` → "unknown environment" error, not a prune.
- Interactive `orun run --all-envs` without `--yes` and no TTY → treated as
  non-interactive ack (CI), proceeds; with a faked TTY → prompts.

---

## 5. Migration tests

- Phase A: bare mutating `orun run` warns + runs all (back-compat).
- Phase B: same invocation errors; `--all-envs` is the documented fix.
- `--dry-run` unaffected across both phases.

---

## 6. CI integration

- Required check on PRs touching `internal/expand`, `internal/planner/promotion`,
  or the `plan`/`run` commands.
- The e2e walk runs under `go test -race ./cmd/orun -run TestEnvScopingE2E`.
