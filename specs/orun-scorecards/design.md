# Design

> orun gains a built-in scorecard engine: named, versioned scorecard definitions
> are evaluated as typed predicates over the entity envelope + the live plane,
> producing weighted scores and bronze/silver/gold levels. Results live in a
> mutable L2 overlay keyed by `entityKey` (never an L1 blob — CR-1);
> `lifecycle.maturity` is a recomputed denormalization of the latest level.
> Composition `effects.scorecards.satisfies` feeds credit. This doc fixes the
> engine, the locked expr-language decision, the missing-input rule, levels,
> persistence, the CLI, invariants, and the sharpness register. Schemas are in
> `data-model.md`.

## 1. Problem

1. **The entity catalog has data but no judgement.** The envelope (SC1) carries
   `ownership`, `lifecycle`, `relations`, `contracts`; the live plane (SC5)
   carries `deployments`/`health`. Nothing turns that into a graded answer to "is
   this service production-ready?" A portal needs scorecards.
2. **Maturity is unmodelled.** `lifecycle.maturity` exists in the envelope as a
   `bronze|silver|gold` field, but there is no engine that *computes* it; today it
   would be authored — and an authored maturity drifts the moment the service
   changes.
3. **The obvious failure mode is a hand-maintained checklist.** Scorecards in
   other portals rot because pass/fail is curated by humans against stale data. A
   derived-truth catalog must derive the grade too.
4. **External-data rules tempt a false-pass.** A rule like "no critical vulns"
   needs a vendor feed; if the feed is absent the naive answer is "pass" — exactly
   the silent failure that erodes trust.
5. **Compositions already know what they contribute.** A golden-path run that
   emits an SBOM or runs tests *should* credit every component on the path, but
   nothing connects `effects` to scorecard checks.

## 2. Goals / non-goals

**Goals**
- A built-in **scorecard engine** (`internal/scorecard`) that evaluates named,
  versioned scorecard definitions against an entity and produces a weighted score
  + a bronze/silver/gold level.
- **Derived-truth scoring**: rules are typed predicates over orun-native data
  first, so a scorecard is meaningful with zero external integrations (S-5).
- **No silent pass**: a predicate whose inputs are absent evaluates to `unknown`
  (counts as not-passed, rendered distinctly), never `pass` (D-9).
- **L2 persistence**: results (latest + a short history) live in a mutable overlay
  keyed by `entityKey`, GC'd with the object sweep — **never** in an L1 blob (CR-1).
- **Maturity denormalization**: `lifecycle.maturity` in the envelope is recomputed
  from the latest level every resolve, never authored.
- **Composition credit**: `effects.scorecards.satisfies` (SC8) feeds named-check
  credit so every component on a golden path inherits maturity.
- A kind-aware **`orun catalog scorecard`** CLI.

**Non-goals**
- Building **Google CEL** (named as the upgrade path only — §3.2).
- Defining the entity envelope or the live plane (owned by SC1/SC5).
- The composition `effects` *authoring* model (owned by SC8; consumed here).
- Vendor connectors for incidents/cost/vulnerabilities (the overlay reserves the
  `live.*` shape; adapters ship separately).
- The SaaS portal render of scorecards (the read seam stays open).

## 3. The scorecard engine (`internal/scorecard`)

`internal/scorecard` is a **typed predicate evaluator**. Given a resolved entity
envelope and the entity's live plane (`live.*`), and a set of `Scorecard`
definitions (`data-model.md` §1), it:

1. selects the definitions whose `appliesTo` (`{kind, tier}`) matches the entity;
2. evaluates each rule's `expr` to one of `{pass, fail, unknown}` against the
   envelope + `live.*`;
3. computes a weighted score `score = Σ(weight · pass) / Σ(weight)` over the
   matched rules, where `unknown` and `fail` contribute **0** to the numerator;
4. maps the score to a level via the definition's `levels` thresholds (§3.3);
5. emits a result record per scorecard into the L2 overlay (§5).

```go
package scorecard

type Verdict int // Pass | Fail | Unknown

type Evaluator interface {
    // Evaluate runs every applicable scorecard against one entity + its live plane.
    Evaluate(ent Entity, live LivePlane, defs []Definition) []Result
}

type Result struct {
    ID          string         // scorecard id, e.g. "production-readiness"
    Score       float64        // weighted, 0..1
    Level       string         // "" | "bronze" | "silver" | "gold"
    Checks      []CheckResult  // per-rule, in definition order
    EvaluatedAt time.Time
}

type CheckResult struct {
    ID      string  // rule id
    Verdict Verdict // Pass | Fail | Unknown
    Weight  int
}
```

The evaluator is **pure** (no store imports): the envelope, the live plane, and
the definitions are inputs; the result is the output. The live-plane writer (SC5)
owns persisting `[]Result` to the overlay (§5); `internal/catalogresolve` reads
the latest level back to recompute `lifecycle.maturity` (§7).

### 3.2 The expr language decision (LOCKED)

**v1 ships a small allowlisted predicate vocabulary, not a general expression
language.** A rule `expr` is parsed into one of a fixed set of typed predicates
over `envelope.*` + `live.*` paths (`data-model.md` §3): field-exists,
field-equals, field-in, count≥N, and deployment-status. Anything outside the
allowlist is a definition-load error, not a runtime surprise.

> **Google CEL is named as the upgrade path.** If richer logic is demanded
> (boolean composition, arithmetic, cross-field comparison beyond the allowlist),
> the `expr` field graduates to [CEL](https://github.com/google/cel-go) — a
> sandboxed, typed, non-Turing-complete expression language. The allowlist is a
> deliberate *strict subset* of what CEL expresses, so the grammar in
> `data-model.md` §3 is forward-compatible: an allowlisted `expr` string is also a
> valid CEL expression. Until that demand is proven, the allowlist keeps
> evaluation auditable and the failure modes bounded.

This resolves `orun-service-catalog` Q-3 in favour of option (a) for v1, with (b)
(an embedded evaluator) reframed as the CEL upgrade path.

### 3.3 Missing input → `unknown` (LOCKED)

A predicate whose referenced input is **absent** (the envelope path is `null`/
unset, or the `live.*` value has not been ingested) evaluates to **`unknown`**.
`unknown`:

- **counts as not-passed** — it contributes 0 to the score, exactly like `fail`;
- is **rendered distinctly** from `fail` in the CLI (§8) and the overlay
  (`CheckResult.Verdict == Unknown`), so an operator can tell "we don't know" from
  "we checked and it's bad";
- is **never** silently treated as `pass`.

This is the load-bearing anti-drift property (D-9, S-5): external-data rules (e.g.
"no critical vulns") degrade to `unknown` when the feed is absent, so a service
cannot reach gold by virtue of orun simply not knowing.

### 3.4 Scorecard levels (bronze / silver / gold)

Each `Scorecard` definition carries a `levels` block of ascending thresholds
(`data-model.md` §1):

```jsonc
"levels": { "bronze": 0.5, "silver": 0.75, "gold": 0.9 }
```

The level is `max(name : score >= threshold)`, evaluated highest-first; a score
below the lowest threshold yields **no level** (`""`, "unrated"), distinct from
bronze. Thresholds are part of the versioned definition, so changing a bar is a
config change with provenance, not a silent re-grade.

## 4. Composition `effects.scorecards.satisfies` credit

A composition is a catalogued producer (SC8); its `effects.scorecards.satisfies`
declares which named checks running the golden path provides — e.g.
`["runs-tests", "emits-sbom"]`. When an entity is composed by such a composition
(a `composes`/`composedBy` relation), the matching rules score `pass` via the
composition credit, so **every component on the path inherits maturity for free**.

Credit is **declared intent, reconciled against actuals** (S-7, D-6): the live
plane records only what `objrun` actually produced. A composition that *declares*
`emits-sbom` but whose executions never emit one is a **declared-vs-actual
divergence** — itself a scorecard signal (a dedicated rule), not silent credit.
The exact reconciliation depth is `orun-service-catalog` Q-5 (resolved within SC8);
this spec consumes the resolved `effects.scorecards` shape.

## 5. Where results persist — the L2 overlay (restating CR-1)

Scorecard results are **mutable** (they change as the live plane changes, with no
source change), so they live in the **L2 live plane**, never in an immutable L1
blob.

- **Storage**: a mutable overlay keyed by `entityKey`, persisted **outside** the
  catalog tree (sibling to the catalog refs, per `orun-service-catalog`
  `data-model.md` §6 / Q-2). The overlay holds **latest + a short history** of
  results per scorecard.
- **CR-1 (restated, MUST)**: nothing that changes without a source change may enter
  an L1 blob. Scorecard `score`/`level`/`checks` are L2. The *definitions* (`kind:
  Scorecard`) are authored config and resolve into L1 like any other config; the
  *results* never do.
- **GC**: overlay records are garbage-collected **with the object sweep** — when an
  `entityKey` is no longer present in the latest catalog, its overlay (including
  scorecard history) is swept; history is bounded to a short retention window so it
  does not grow without bound.

`lifecycle.maturity` in the envelope is the **only** scorecard-derived value that
appears in L1, and it is a *recomputed denormalization* (§7), never the authored
or trusted source.

## 6. Data flow

```
component.yaml / composition.yaml          scorecards/*.yaml (kind: Scorecard, config-as-code)
        │ (resolve — SC1/SC8)                       │ (loader — SCR0)
        ▼                                            ▼
entity envelope (L1)  ── + live plane (L2, SC5) ── + Definition[]
        │                        │                   │
        └────────────┬──────────┴───────────────────┘
                     ▼
        internal/scorecard.Evaluate  (pure: allowlisted predicates → pass/fail/unknown → score → level)
                     │
        ┌────────────┴───────────────────────────────┐
        ▼                                              ▼
   L2 scorecard overlay (latest + history,      catalogresolve: recompute
   keyed by entityKey; GC'd w/ object sweep)    lifecycle.maturity = latest level (L1 denorm)
                     ▲
        composition effects.scorecards.satisfies (SC8) ─► named-check credit
                     │
        orun catalog scorecard (CLI, §8) ◄── objcatalog reads the overlay
```

## 7. `lifecycle.maturity` recompute

`lifecycle.maturity` in the envelope (`orun-service-catalog` `data-model.md` §2) is
the **denormalized** `max(level)` across the entity's latest scorecard results,
recomputed at **every resolve** from the L2 overlay (read-only), **never**
authored and **never** trusted across a source change (CR-1). It is a convenience
for the portal and search facets; the authoritative grade is always the L2 result.
The recompute is a deterministic function of (latest overlay results, definitions),
so a re-resolve with an unchanged overlay is byte-stable.

## 8. The `orun catalog scorecard` CLI

A kind-aware command that evaluates and/or reports scorecards over the catalog.

| Flag | Effect |
|------|--------|
| `--scorecard <id>` | restrict to one scorecard definition (default: all applicable) |
| `--kind <Kind>` | restrict to entities of one kind (`Component`, `API`, …) |
| `--min-level <bronze\|silver\|gold>` | show only entities at/above a level |
| `--failing` | show only entities with at least one `fail`/`unknown` check |
| `--json` | machine-readable output (the overlay result shape, `data-model.md` §2) |

- **Exit 6 if no live plane.** Scorecards evaluate over `live.*`; with no live
  plane present (SC5 not materialized / overlay absent), the command exits **6**
  rather than silently scoring every `live.*` rule as `unknown` across the board.
  (`--json` still emits a structured "no live plane" error.)
- `unknown` checks are rendered distinctly from `fail` (§3.3) — e.g. a `?` glyph
  vs `✗`.

## 9. Invariants

1. **CR-1** — scorecard `score`/`level`/`checks` are L2; only the recomputed
   `lifecycle.maturity` denormalization touches L1 (§5, §7).
2. **No silent pass** — a missing-input predicate is `unknown`, never `pass`; it
   contributes 0 to the score and renders distinctly (§3.3, D-9).
3. **Allowlist closure** — every accepted `expr` parses to a member of the v1
   predicate vocabulary; an out-of-vocabulary `expr` is a load-time error, not a
   runtime fallback (§3.2).
4. **Determinism** — for a fixed (envelope, live plane, definitions), `Evaluate`
   is a pure function: identical score, level, and per-check verdicts; the
   `lifecycle.maturity` recompute is byte-stable on an unchanged overlay.
5. **Declared-vs-actual** — composition `effects.scorecards` credit is reconciled
   against `objrun`-actuals; over-claim is itself a signal, not silent credit (§4).
6. **GC coupling** — overlay records (incl. history) are swept with the object
   sweep when their `entityKey` leaves the catalog (§5).

## 10. Sharpness register

| # | Sharp edge | Resolution |
|---|------------|------------|
| S-1 | Scorecards become a hand-maintained checklist that drifts | Rules evaluate orun-native data first; results are derived, never authored; definitions are versioned config (§2, §3). |
| S-2 | External-data rule false-passes when the feed is absent | Missing input → `unknown` (counts as not-passed, rendered distinctly), never `pass` (§3.3, D-9). |
| S-3 | A general expr language is unbounded / un-auditable in v1 | Locked allowlisted predicate vocabulary; CEL named as the upgrade path; the allowlist is a strict CEL subset (§3.2). |
| S-4 | Scorecard results leak into an L1 blob and break drift-free guarantees | CR-1 restated; results are L2; only the recomputed `lifecycle.maturity` denorm is L1 (§5, §7). |
| S-5 | Composition `effects` over-claim a gold rating | Credit is declared intent reconciled vs `objrun`-actuals; divergence is a signal, not silent credit (§4, S-7 of parent). |
| S-6 | Overlay history grows without bound | Short retention window; GC'd with the object sweep when the `entityKey` leaves the catalog (§5). |
| S-7 | `lifecycle.maturity` and the L2 result disagree | Maturity is a recomputed denorm of the latest level, not an independent source; the L2 result is authoritative (§7). |
| S-8 | `--json` consumers can't distinguish "no live plane" from "all unknown" | Exit 6 + a structured "no live plane" error, distinct from per-check `unknown` (§8). |
| S-9 | A level threshold change silently re-grades every entity | Thresholds are part of the versioned definition; a change has config provenance (§3.4). |
