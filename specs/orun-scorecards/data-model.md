# Data Model

> Every persisted schema for scorecards: the `Scorecard` definition
> (config-as-code), the L2 scorecard overlay, and the v1 allowlisted predicate
> vocabulary. JSON is the on-disk form; `lowerCamelCase`, object IDs
> `"<algo>:<hex>"`, RFC 3339 / Z. "MUST/SHOULD/MAY" carry RFC 2119 weight here.
> Go shapes live in `internal/catalogmodel` (the definition) and the live-plane
> writer (the overlay); this doc is the contract they generate/serialize. The
> `Scorecard` definition is **MOVED** from `orun-service-catalog` `data-model.md`
> §7; the overlay shape from its §6.

## 1. The `Scorecard` definition (config-as-code)

A scorecard is a versioned definition resolved like any other config, evaluated by
`internal/scorecard` (`design.md` §3). Authored as `kind: Scorecard`; the
struct-generated schema lives in `internal/catalogmodel`.

```jsonc
{ "apiVersion":"orun.io/v1","kind":"Scorecard","id":"production-readiness",
  "appliesTo":{"kind":"Component","tier":["tier-1","tier-2"]},
  "rules":[{"id":"has-owner","expr":"ownership.owner != 'unknown'","weight":1},
           {"id":"deploys-clean","expr":"live.deployments.production == 'healthy'","weight":3},
           {"id":"on-golden-path","expr":"spec.composition.source != ''","weight":2}],
  "levels":{"bronze":0.5,"silver":0.75,"gold":0.9} }
```

### 1.1 Field contract

| Field | Req. | Notes |
|-------|------|-------|
| `apiVersion` | MUST | `orun.io/v1`; older blobs up-convert on read (parent §8). |
| `kind` | MUST | `Scorecard`. |
| `id` | MUST | stable scorecard id, unique within the catalog; the L2 overlay key (`scorecards[].id`). |
| `appliesTo.kind` | MUST | the `EntityKind` this scorecard grades (`Component`, `API`, …). |
| `appliesTo.tier` | MAY | list of `lifecycle.tier` values the scorecard restricts to; absent → all tiers of `kind`. |
| `rules` | MUST (≥1) | weighted predicate rules (§1.2). |
| `rules[].id` | MUST | stable rule id; the per-check key (`checks[].id`) and the credit key for composition `effects.scorecards.satisfies` (`design.md` §4). |
| `rules[].expr` | MUST | a single allowlisted predicate over `envelope.*` + `live.*` (§3). An out-of-vocabulary `expr` is a **load-time error** (invariant 3). |
| `rules[].weight` | MUST | positive integer; the rule's contribution to the weighted score. |
| `levels` | MUST | ascending `bronze`/`silver`/`gold` thresholds in `[0,1]` (§1.3). |

### 1.2 Scoring

`score = Σ(weight · [verdict == pass]) / Σ(weight)` over the rules matched for the
entity. `unknown` and `fail` contribute **0** to the numerator (`design.md` §3.3).
The level is `max(name : score >= levels[name])`, highest-first; a score below the
lowest threshold yields **no level** (`""`).

### 1.3 `levels`

`bronze <= silver <= gold`, all in `[0,1]`. Thresholds are part of the versioned
definition — changing a bar is a config change with provenance, not a silent
re-grade (`design.md` §3.4, S-9).

## 2. The L2 scorecard overlay (NOT in the catalog tree)

Mutable, keyed by `entityKey`, persisted **outside** the catalog tree (sibling to
the catalog refs, per parent `data-model.md` §6 / Q-2). **Never** content-addressed
into an L1 blob (CR-1). Holds the **latest** result per scorecard plus a short
bounded **history**; GC'd with the object sweep when the `entityKey` leaves the
catalog (`design.md` §5).

```jsonc
{
  "entityKey": "default/orun/identity-worker",
  "scorecards": [
    { "id": "production-readiness", "score": 0.86, "level": "gold",
      "checks": [
        { "id": "has-owner",      "pass": true,  "weight": 1 },
        { "id": "deploys-clean",  "pass": true,  "weight": 3 },
        { "id": "on-golden-path", "pass": false, "weight": 2 }   // verdict: fail | unknown distinguished below
      ],
      "evaluatedAt": "2026-06-09T12:00:00Z" }
  ]
}
```

### 2.1 Field contract

| Field | Req. | Notes |
|-------|------|-------|
| `entityKey` | MUST | the overlay key; `<namespace>/<repo>/<name>`. |
| `scorecards[]` | MUST (may be `[]`) | one entry per applicable scorecard; latest result. A short prior-result `history` rides under each entry (bounded retention, §2.2). |
| `scorecards[].id` | MUST | scorecard `id` from the definition (§1). |
| `scorecards[].score` | MUST | weighted score in `[0,1]` (§1.2). |
| `scorecards[].level` | MUST | `"" \| "bronze" \| "silver" \| "gold"`. |
| `scorecards[].checks[]` | MUST | per-rule, in definition order. |
| `scorecards[].checks[].id` | MUST | rule `id`. |
| `scorecards[].checks[].pass` | MUST | `true` iff `verdict == pass`. |
| `scorecards[].checks[].verdict` | SHOULD | `"pass" \| "fail" \| "unknown"` — distinguishes a checked-bad rule from a missing-input rule (`design.md` §3.3). `pass` is the boolean projection of `verdict == "pass"`; readers MUST treat absent `verdict` with `pass == false` as `"fail"` for back-compat. |
| `scorecards[].checks[].weight` | MUST | the rule weight. |
| `scorecards[].evaluatedAt` | MUST | RFC 3339 / Z evaluation time. |

> **Distinguishing `fail` from `unknown` (MUST).** `pass:false` alone is
> ambiguous. The overlay carries `verdict` so a missing-input check (`unknown`)
> is distinct from a checked-and-failed check (`fail`); the CLI renders them
> distinctly (`design.md` §8). Both contribute 0 to the score.

### 2.2 History + GC

Each `scorecards[]` entry retains a **short bounded history** of prior results
(latest + N) so the CLI/portal can show a trend without unbounded growth. The
entire overlay record — latest + history — is **swept with the object sweep** when
its `entityKey` is absent from the latest catalog (`design.md` §5, invariant 6).

## 3. The v1 allowlisted predicate vocabulary

A rule `expr` is parsed into exactly one of the following typed predicates over
`envelope.*` (the entity envelope) or `live.*` (the live plane). Anything outside
this table is a **load-time error** (`design.md` §3.2, invariant 3). The grammar
is a strict subset of CEL, so each `expr` is forward-compatible with the CEL
upgrade path.

| Predicate | `expr` form | Example | `unknown` when |
|-----------|-------------|---------|----------------|
| **field-exists** | `<path> != null` | `ownership.escalation != null` | the path is unresolvable (kind has no such field) |
| **field-absent** | `<path> == null` | `provenance.attestation == null` | (never `unknown`; absence is the assertion) |
| **field-equals** | `<path> == '<lit>'` / `<path> != '<lit>'` | `ownership.owner != 'unknown'` | the referenced field is unset/null |
| **field-nonempty** | `<path> != ''` | `spec.composition.source != ''` | the field is unset/null |
| **field-in** | `<path> in ['a','b',…]` | `lifecycle.stage in ['production','deprecated']` | the field is unset/null |
| **count≥N** | `count(<listPath>) >= <N>` | `count(docs.runbooks) >= 1` | the list path is unset/null |
| **numeric-compare** | `<path> == <N>` / `<path> <= <N>` / `<path> >= <N>` | `live.vulnerabilities.critical == 0` | the value has not been ingested (`live.*` absent) |
| **deployment-status** | `live.deployments.<env> == '<status>'` | `live.deployments.production == 'healthy'` | no deployment record for `<env>` (`live.*` absent) |

Notes:
- `<path>` is a dotted path rooted at an envelope block (`ownership`, `lifecycle`,
  `spec`, `contracts`, `docs`, `relations`, `provenance`, `metadata`) or at
  `live` (the live plane).
- String literals are single-quoted; lists are bracketed single-quoted literals;
  `<N>` is an integer.
- A predicate referencing a `live.*` path when **no live plane is present** yields
  `unknown` for that check; the CLI exits **6** when the live plane is entirely
  absent rather than scoring every `live.*` rule `unknown` (`design.md` §8).
- The `unknown when` column is the load-bearing anti-drift rule (D-9): a missing
  input is `unknown`, never a silent `pass`.

## 4. Schema generation discipline

The `Scorecard` definition is **struct-generated** (`internal/catalogmodel/schema/
gen` + `go generate`), per the catalog schema source-of-truth rule — edit the Go
struct and regenerate, never hand-edit the JSON schema. Every authored field MUST
be accepted by both `component.yaml`-class parsers' config-loading path and present
in the generated schema (parent `design.md` invariant 5). The L2 overlay is **not**
content-addressed and is **not** struct-generated into the catalog schema; it is a
mutable index serialized by the live-plane writer.
