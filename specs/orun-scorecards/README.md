# Spec: orun-scorecards

**orun gains a built-in scorecard engine: named, versioned scorecard
*definitions* (config-as-code) are evaluated as typed predicates over the entity
envelope + the live plane, producing weighted scores, bronze/silver/gold levels,
and a `lifecycle.maturity` denormalization — all *derived* from orun's own data,
never a hand-maintained checklist.** Choosing the paved road makes a service
gold-rated *for free*; missing inputs render as `unknown`, never a silent pass.

This is an evaluation-and-overlay spec extracted from `specs/orun-service-catalog/`
(the entity catalog + live plane), not a new store. It builds directly on that
epic's **entity envelope** (SC1) and **live plane** (SC5), and folds in the
**composition `effects` producer model** (SC8). It (1) introduces the
`internal/scorecard` typed predicate evaluator, (2) locks a small allowlisted
predicate vocabulary for v1 (with Google CEL named as the upgrade path), (3)
persists results in a **mutable L2 overlay keyed by `entityKey`** (never in an
immutable L1 blob — CR-1), and (4) wires composition `effects.scorecards.satisfies`
credit and the `lifecycle.maturity` recompute.

> **The defining property is that a scorecard is meaningful with zero external
> integrations.** Because most predicates reference orun-native data — owner
> resolved? deploys clean? on a golden path? — orun ships real scores before any
> vendor connector, and external-data rules degrade to `unknown` (counts as
> not-passed, rendered distinctly), never a false-pass. Nothing the engine
> computes is trusted across a source change.

## Status

| Field | Value |
|-------|-------|
| Status | **Status: Draft (v2) — for review, not scheduled** |
| Builds on | `specs/orun-service-catalog/` (the entity catalog + live plane) |
| Depends on | **SC1** (the entity envelope), **SC5** (the live plane), **SC8** (composition `effects`) — see "Gating" |
| Extracts | `orun-service-catalog` `design.md` §6 (scorecards), `data-model.md` §7 (the `Scorecard` definition) and §6 (the L2 overlay), Q-3 (the `expr` language) and D-9 (missing-input → `unknown`) |
| apiVersion | `orun.io/v1` (scorecard definitions and the L2 overlay) |
| Decisions locked | derived-truth scoring; the **expr language is a small allowlisted predicate vocabulary for v1** (CEL is the upgrade path); **missing-input → `unknown`** (never a silent pass); results live in a **mutable L2 overlay keyed by `entityKey`**, never in an L1 blob (CR-1); `lifecycle.maturity` is a recomputed denormalization, never authored; composition `effects.scorecards.satisfies` feeds credit; scorecard *definitions* are versioned config-as-code |
| Milestone prefix | **SCR** (`SCR0 → SCR4`) |

## The one-paragraph thesis

The entity catalog gives every service a typed envelope and a live plane
(deployments, health), but nothing turns that data into a *judgement*: is this
service production-ready? A developer portal needs scorecards — named, weighted
checklists that grade an entity — and it needs them to grade *automatically*,
without humans curating pass/fail. orun is uniquely placed: it resolves the
envelope from git and owns the execution truth behind the live plane, so a
scorecard rule can be a typed predicate over `ownership.owner`, `live.deployments`,
`spec.composition.source` — orun's own data — and produce a real score with no
external integration. This spec adds the `internal/scorecard` evaluator over a
**locked, allowlisted predicate vocabulary** (field-exists / equals / in /
count≥N / deployment-status), names **Google CEL** as the upgrade path if richer
logic is demanded, degrades missing inputs to **`unknown`** rather than a silent
pass, persists latest + a short history in a **mutable L2 overlay keyed by
`entityKey`** (GC'd with the object sweep), folds composition `effects` credit so
every component on a golden path inherits maturity, and recomputes
`lifecycle.maturity` in the envelope from the latest level — so a scorecard is a
derived, drift-free grade, not a checklist that rots.

## Read order

1. **`design.md`** — the problem, goals/non-goals, the scorecard engine
   (`internal/scorecard`), the locked expr-language decision (allowlist for v1,
   CEL as upgrade path), missing-input → `unknown`, scorecard levels, where
   results persist (the L2 overlay, restating CR-1), composition `effects`
   credit, the `lifecycle.maturity` recompute, the `orun catalog scorecard` CLI,
   invariants, and the sharpness register.
2. **`data-model.md`** — the `Scorecard` definition schema (config-as-code), the
   L2 scorecard overlay shape, and the v1 allowlisted predicate vocabulary table.
3. **`implementation-plan.md`** — milestones **SCR0 → SCR4**.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| `internal/scorecard` (the typed predicate evaluator); the `Scorecard` definition loader (config-as-code); the locked v1 allowlisted predicate vocabulary; missing-input → `unknown`; bronze/silver/gold levels; the **L2 scorecard overlay** (latest + short history, keyed by `entityKey`, GC'd with the object sweep); composition `effects.scorecards.satisfies` credit; the `lifecycle.maturity` recompute from the latest level; the `orun catalog scorecard` CLI | Google CEL (named as the **upgrade path** only — not built here); the entity envelope + live plane themselves (owned by `orun-service-catalog` SC1/SC5); the composition `effects` *authoring* model (owned by SC8; this spec only consumes `effects.scorecards`); incidents/cost/vulnerability *ingestion* (the overlay reserves `live.*` shape; vendor connectors ship separately); SLO/error-budget scoring math (the vocabulary stays allowlisted in v1); the SaaS portal render of scorecards (read seam kept open) |

## Gating

The whole epic is **gated on `orun-service-catalog` SC5 (the live plane) and SC8
(composition effects)** — `internal/scorecard` evaluates over the entity envelope
(SC1) + the live plane (SC5), and `effects.scorecards.satisfies` credit requires
the composition producer model (SC8). SCR0–SCR3 need SC1 + SC5; SCR4 needs SC8.

## Convention over configuration (locked)

The developer authors the *minimum*; orun derives the grade. Scorecard
*definitions* are versioned config-as-code (`kind: Scorecard`); the *results* are
derived — never authored. `lifecycle.maturity` in the envelope is a recomputed
denormalization of the latest level (CR-1), not a hand-set field. Most rules
reference orun-native data, so a scorecard scores meaningfully before any external
integration; rules whose inputs are absent degrade to `unknown`, never a silent
pass (D-9).

## Document conventions

- Go for interfaces, JSON for on-disk schemas. Forward-slash logical paths,
  root-relative to `.orun/objectmodel/`.
- Object IDs `"<algo>:<hex>"`. `lowerCamelCase` JSON. RFC 3339 / Z timestamps.
- "MUST / SHOULD / MAY" carry RFC 2119 weight in `data-model.md` (the schema /
  correctness contract).
- Entity keys are three-segment `<namespace>/<repo>/<name>` per
  `specs/archive/orun-component-catalog/identity-and-keys.md`, paired with `kind`.

## Out-of-band references

- Parent epic: `specs/orun-service-catalog/` (envelope SC1, live plane SC5,
  composition effects SC8). This spec is **extracted** from its `design.md` §6 and
  `data-model.md` §6/§7.
- Predecessor specs: `specs/orun-object-model/`, `specs/archive/orun-component-catalog/`,
  `specs/archive/orun-catalog-state/` (`objcatalog`, the object sweep / GC).
- Packages changed: `internal/scorecard` (new), `internal/catalogmodel`
  (the `Scorecard` definition struct + generated schema), `internal/objcatalog`
  (read the overlay), the live-plane writer (SC5), `internal/catalogresolve`
  (`lifecycle.maturity` recompute), `internal/composition` (`effects.scorecards`),
  `cmd/orun` (`catalog scorecard`).
