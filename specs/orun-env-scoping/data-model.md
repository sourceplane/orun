# Data Model

This feature is mostly *behavioral*; it adds **one** new record (`PrunedEdge`) and
**one** plan-metadata block (`selection`). The environment/component selection is
already carried by existing schemas — it is **not** re-modeled here.

Conventions: Go for in-memory/types, JSON for plan output. `lowerCamelCase`.

---

## 1. Existing fields that already carry the selection (no change)

The plan/revision schemas from `orun-state-redesign` already record what was
selected; this feature populates them, it does not change their shape:

- `TriggerOccurrence.PlanScope`
  (`orun-state-redesign/data-model.md` §2):
  `{ mode, activationMode, activeEnvironments[], changedComponents[] }`.
- `RevSummary` (`revision.json`):
  `{ scope, activeEnvironments[], changedComponents[] }`.

`activeEnvironments` **remains a list** — multi-env plans are first-class in this
model (the earlier "constrain to length 1" idea is dropped). A scoped plan simply
has fewer entries; a full plan lists all.

---

## 2. Plan selection metadata — `metadata.selection`

`orun plan` embeds a selection block in the compiled plan (additive to the
`metadata.trigger`/`metadata.revision` blocks from `orun-state-redesign`):

```json
{
  "metadata": {
    "selection": {
      "envs": ["staging"],
      "components": ["api", "web"],
      "mode": "scoped",
      "allEnvs": false,
      "prunedEdges": [
        { "kind": "promotion", "from": "staging", "to": "dev",
          "reason": "env-not-selected" },
        { "kind": "component", "from": "api", "to": "shared-libs",
          "reason": "component-not-selected" }
      ]
    }
  }
}
```

| Field | Type | Notes |
|-------|------|-------|
| `envs` | `[]string` | Environments included in the plan. Empty/omitted ⇒ all. |
| `components` | `[]string` | Components included. Empty/omitted ⇒ all (subject to `--changed`). |
| `mode` | string | `"full"` (no narrowing) or `"scoped"`. |
| `allEnvs` | bool | True when the selection was an explicit `--all-envs` (vs. the implicit default). Lets `run` distinguish a deliberate all-env plan from an unscoped one (§ fail-closed, `design.md` §2.2). |
| `prunedEdges` | `[]PrunedEdge` | §3. Empty for a full plan. |

`mode`/`allEnvs` are deterministic functions of the selection; they carry no
timestamps and fold into the plan hash like the rest of `metadata`.

---

## 3. `PrunedEdge`

The single new record — an edge dropped because its endpoint is not in the
expanded plan (`design.md` §3). Go and JSON:

```go
type PrunedEdge struct {
    Kind   string `json:"kind"`   // "promotion" | "component"
    From   string `json:"from"`   // selected endpoint (env name or componentKey)
    To     string `json:"to"`     // dropped endpoint (env name or componentKey)
    Reason string `json:"reason"` // "env-not-selected" | "component-not-selected"
}
```

- `kind` distinguishes a cross-env **promotion** `dependsOn` from a cross-component
  `dependsOn`.
- `from` is always in the plan; `to` is always absent from it.
- Emitted to the warning stream at plan time **and** under
  `metadata.selection.prunedEdges` **and** in `orun plan --json`.

---

## 4. Promotion ordering (no new schema)

`promotion.dependsOn` (existing `model.EnvironmentPromotion` /
`PromotionDependency`) is realized as **job ordering edges within the plan** by
`internal/planner/promotion.go` (the existing same-plan path). No new persisted
shape: dependents' jobs gain a `dependsOn` on prerequisites' jobs, exactly as a
multi-env plan produces today. Cross-plan `Satisfy` modes
(`"previous-success"`, `"same-plan"`) become inert under this feature
(`design.md` §9); the source-status gate that would revive cross-plan semantics is
Option C (deferred).

---

## 5. Validation rules

- `metadata.selection.mode ∈ {"full","scoped"}`; `"scoped"` REQUIRES at least one
  of `envs`/`components` to be a strict subset, or `prunedEdges` non-empty.
- `PrunedEdge.kind ∈ {"promotion","component"}`; `reason ∈
  {"env-not-selected","component-not-selected"}`.
- Env names MUST exist in `intent.environments`; an `--env` naming an unknown
  environment is a hard error (not a prune).
- `prunedEdges` MUST be byte-deterministic: sorted by `(kind, from, to)`; no map
  iteration order leaks.
- A full plan (no narrowing) MUST emit `mode:"full"`, `prunedEdges: []`.
