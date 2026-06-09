# Implementation Plan

> Milestone-based. Each states **goal**, **deps**, **done when**. Agents may
> split/merge while keeping each PR reviewable. The engine skeleton + loader
> (SCR0) and the predicate evaluator (SCR1) are the core; persistence (SCR2) and
> the CLI (SCR3) make it usable; composition credit + the maturity recompute
> (SCR4) close the loop.
>
> **The whole epic is GATED on `orun-service-catalog` SC5 (the live plane) and
> SC8 (composition effects).** SCR0–SCR3 require SC1 (envelope) + SC5 (live
> plane); SCR4 additionally requires SC8.

```
[gated on orun-service-catalog SC5 + SC8]

SCR0 engine skeleton + Scorecard loader ─► SCR1 predicate vocabulary + evaluator + unknown-on-missing
                                                       │
                                          SCR2 L2 overlay persistence + GC
                                          SCR3 orun catalog scorecard CLI
                                                       │
                                          SCR4 effects.scorecards credit + lifecycle.maturity recompute (needs SC8)
```

---

## SCR0 — Engine skeleton + `Scorecard` loader
**Goal:** `internal/scorecard` exists with the core types; `Scorecard` definitions
load as config-as-code.
- Scaffold `internal/scorecard` (`Evaluator`, `Result`, `CheckResult`, `Verdict`)
  per `design.md` §3; pure, no store imports.
- Add the `Scorecard` definition struct to `internal/catalogmodel` +
  struct-generated schema (`data-model.md` §1, §4); a loader that resolves
  `kind: Scorecard` configs and validates `appliesTo`/`rules`/`levels`.

**Deps:** SC1 (envelope), SC5 (live plane). **Done when:** a `Scorecard` definition
round-trips through the generated schema; the loader rejects a malformed definition;
≥90% coverage on the loader.

## SCR1 — Predicate vocabulary + evaluator + unknown-on-missing
**Goal:** the locked v1 allowlist evaluates against envelope + `live.*`, with
missing-input → `unknown`.
- Implement the allowlisted predicate vocabulary (`data-model.md` §3): parse each
  `expr` to a typed predicate; an out-of-vocabulary `expr` is a **load-time error**
  (invariant 3).
- Implement `Evaluate`: per-rule `pass/fail/unknown`, weighted score, level mapping
  (`design.md` §3.1–§3.4). **Missing input → `unknown`** (counts as not-passed,
  rendered distinctly), never a silent pass (D-9).

**Deps:** SCR0. **Done when:** the vocabulary table is fully covered by fixtures;
a `live.*` rule with no live plane scores `unknown` (not `pass`); evaluation is
deterministic for a fixed (envelope, live plane, definitions); ≥90% coverage.

## SCR2 — L2 overlay persistence + GC
**Goal:** results persist as a mutable L2 overlay keyed by `entityKey`, GC'd with
the object sweep.
- Persist `[]Result` (latest + a short bounded history) to the live-plane overlay
  (`data-model.md` §2), sibling to the catalog refs (Q-2). **Never** in an L1 blob
  (CR-1).
- Wire overlay GC into the object sweep: sweep a record when its `entityKey` leaves
  the latest catalog (`design.md` §5, invariant 6).

**Deps:** SCR1, SC5. **Done when:** an evaluation writes the overlay with `verdict`
distinguishing `fail`/`unknown`; history is bounded; a removed `entityKey`'s overlay
(incl. history) is swept; no scorecard field enters an L1 blob (CR-1 assertion).

## SCR3 — `orun catalog scorecard` CLI
**Goal:** evaluate/report scorecards over the catalog from the CLI.
- Wire `internal/scorecard` + the overlay to a command with
  `--scorecard/--kind/--min-level/--failing/--json` (`design.md` §8).
- **Exit 6** when no live plane is present (structured error under `--json`);
  render `unknown` distinctly from `fail`.

**Deps:** SCR1, SCR2. **Done when:** the flags filter as specified; `--json` emits
the overlay result shape; exit 6 with no live plane; `unknown`≠`fail` in output;
fixtures cover min-level/failing/json.

## SCR4 — Composition `effects.scorecards` credit + `lifecycle.maturity` recompute
**Goal:** golden-path credit feeds checks; the envelope's maturity is a recomputed
denormalization of the latest level.
- Consume composition `effects.scorecards.satisfies` (SC8): a `composes`/`composedBy`
  relation credits the matching named checks (`design.md` §4). Credit is **declared
  intent reconciled vs `objrun`-actuals**; over-claim is a signal, not silent credit
  (S-5/S-7).
- Recompute `lifecycle.maturity` = `max(level)` over the latest overlay results at
  every resolve in `internal/catalogresolve` (`design.md` §7); never authored; CR-1.

**Deps:** SCR2, **SC8**. **Done when:** a component on a golden path inherits the
declared checks' credit; a declared-but-not-produced effect surfaces as a divergence
signal (not credit); `lifecycle.maturity` is byte-stable on an unchanged overlay and
never read from authored input; determinism asserted.

---

## Cross-cutting (every milestone)
- **CR-1** — scorecard `score`/`level`/`checks` are L2; only the recomputed
  `lifecycle.maturity` denorm touches L1 (invariant 1).
- **No silent pass** — missing input is `unknown`, never `pass` (invariant 2, D-9).
- **Allowlist closure** — every accepted `expr` is in the v1 vocabulary; CEL is the
  upgrade path only, not built here (invariant 3, `design.md` §3.2).
- **Determinism** — `Evaluate` and the maturity recompute are pure functions of
  their inputs (invariant 4).
- Struct-generated schema for the `Scorecard` definition (`go generate`, never
  hand-edit the JSON); canonical JSON; `errors.Is/As`.
