# Risks & Open Questions

Live register. Decisions with a stated default are settled unless re-opened. The
Z-decisions are in `design.md` §5; the D-rows below are the gap resolutions
(`design.md` §7).

## Decisions (settled)

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| **D-1** | Selection at plan time or run time | **Plan time; `run` is faithful** (Z-1) | The plan is already a trigger/changed-shaped artifact; the plan you review must be what runs. Always-full-plan select-at-run was rejected (`design.md` §8). |
| **D-2** | Default of a bare command | **`plan` = all (read-only); mutating `run` = fail-closed** (Z-2/Z-3) | Fail-to-nothing is strictly safer than a default `local` env (fail-to-sandbox) and equally scalable (flat, zero default blast radius). |
| **D-3** | All-environments flag name | **`--all-envs`** | `--all` is taken by the root component/CWD-scoping flag (`commands_root.go`); reuse would collide. |
| **D-4** | `--dry-run` and the guard | **`--dry-run` is exempt** (read-only escape) | A preview can never mutate, so it never needs the guard; gives users a friction-free "see everything" path. |
| **D-5** | Pruning scope | **Uniform** — any dangling edge (promotion or component), warn, record `PrunedEdge` | One rule, easy to reason about; keeps the scoped plan faithful (G4). |
| **D-6** | `--component` selection mode | **Exact** (no closure) in v1 | Predictable; `--with-deps` closure is a future affordance (G6). |
| **D-7** | Promotion mechanism | **In-plan `dependsOn` ordering** (Z-5, Option B) | Mostly already implemented; failed prerequisite blocks dependents within the run. |
| **D-8** | Built-in `local` env | **None** (Z-7) | Its only real job (safe default) is better served by fail-closed `run`; a sandbox is a normally-declared env. |
| **D-9** | Bare-mutating-`run` rollout | **Deprecation window: warn → error** (G7) | Behavior change; give users one release to add a selection. |
| **D-10** | Cross-plan `Satisfy` modes | **Made inert, not removed** | Avoids an intent-file break; Option C (L-1) revives cross-plan semantics later. |

## Open questions (need a call within the cited milestone)

| # | Question | Options | Needed by |
|---|----------|---------|-----------|
| Q-1 | Should repeated `--env a --env b` be accepted alongside the comma form? | (a) comma only (matches today); (b) also `StringSlice` repeatable | ES1; propose (a) for parity, revisit if users ask |
| Q-2 | Interactive `--all-envs` confirmation default | (a) prompt unless `--yes` / non-TTY; (b) always require `--yes` | ES4; propose (a) |
| Q-3 | Does `--all-envs` on `plan` (read-only) need any guard at all? | (a) no — `plan` is read-only; (b) symmetric with `run` | ES1; propose (a) |
| Q-4 | Phase B (warn→error) timing | next minor vs. next major | before Phase B PR |

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| A scoped plan silently drops a promotion gate edge and is used in automation → unguarded run | Med | **High** | `--all-envs` is the documented CI path (never prunes); warnings on every prune; hardening to error-in-CI tracked (L-2). |
| Users surprised by bare mutating `run` change | Med | Med | Phase A warn window (D-9) + clear upgrade note (`compatibility-and-migration.md` §3). |
| Pruning a *component* hard-dependency hides a real ordering requirement during local testing | Low | Med | Warn + list `PrunedEdge`; exact selection is opt-in (you narrowed deliberately). |
| `--all-envs` mutating run fans out to prod by accident | Low | **High** | Explicit flag + interactive confirmation/`--yes` (Q-2); `plan`/`--dry-run` are the safe preview. |
| In-plan-only promotion misleads users who split envs across pipelines | Med | Med | Documented limitation (G2); Option C (L-1) is the upgrade path. |
| `metadata.selection` non-determinism breaks plan-hash dedup | Low | Med | Sorted, map-order-free encoding; P6 determinism test. |

## Deferred / needs-later-attention register

| # | Item | Why deferred | Pull back in when |
|---|------|--------------|-------------------|
| **L-1** | **Option C — cross-invocation source-status promotion gate.** Component-level, keyed by source head: a dependent env's run reads prior executions under the same revision to confirm the prerequisite completed. | In-plan ordering (Option B) covers single-run promotion; cross-pipeline gating is a larger, state-store-coupled change. | Teams need staging-in-pipeline-1 to gate prod-in-pipeline-2 (G2). |
| **L-2** | **Pruning hardening** — pruned *promotion* edges become an error in CI / require `--prune-deps`. | Warning is enough for v1; convention (`--all-envs` in CI) covers the safe path. | A pruned gate in automation causes an incident, or before broad rollout. |
| **L-3** | **Idempotent cross-run job skip** — skip jobs already completed at the same source head across re-runs. | Resume already skips within a plan; cross-run skip needs the fingerprint key. | Large multi-env runs make re-execution cost painful. |
| **L-4** | **`--with-deps` closure selection** for `--component`/`--env`. | v1 is exact selection; closure is additive. | Users want "run X and everything it needs" ergonomically. |
| **L-5** | **Remove inert `Satisfy` modes** from the intent schema. | Kept inert to avoid an intent break (D-10). | Option C (L-1) defines the replacement and a migration. |

## Explicitly out of scope

- Any built-in `local`/sandbox environment (D-8).
- The single-environment-everywhere invariant, mandatory `defaultEnvironment`, and
  idempotent-skip-as-source-of-truth — all explored and set aside (`design.md` §8).
