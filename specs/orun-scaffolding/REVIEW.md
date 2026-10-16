# Review: orun-scaffolding epic — against code reality and the SaaS direction

> Scope: a concrete review of `specs/orun-scaffolding/` (README + design +
> implementation-plan) against (a) the **as-built** orun code, and (b) where the
> orun-cloud SaaS product is actually headed on self-service "create a service".
> Status reviewed: epic is **Draft (v2) — for review, not scheduled**.

## TL;DR verdict

The epic is **well-designed in isolation** — the engine decision (`text/template`
+ constrained funcmap), the secret-on-disk rule, path containment, determinism,
and the both-parsers-plus-resolve output gate are the right calls and are
internally coherent. **But its central premise is wrong against reality**: it
bills itself as *"an extraction-and-detail spec, not a new model … consumes SC7's
`contract`/`scaffold` types directly (no parallel schema)"* (README, design §S-7).
**Those types do not exist in code.** The parent marked SC7 "landed", but landed
only a thin Composition-as-Entity; the typed `contract.inputs` (§3) and the
`scaffold` block (§5) that this epic renders were **never built**. And on the
SaaS side, the portal that this epic defers the GUI form to is heading in the
**opposite architectural direction** (a Backstage-style template registry — the
very model this epic's thesis rejects).

So the epic is not a small detail-fill on a finished foundation. It sits on two
unbuilt foundations and one cross-repo contradiction. None of that is fatal, but
it changes what the epic *is* and what it must schedule first.

Two blocking findings (**B1**, **B2**), two grounding corrections (**C1**, **C2**),
and sequencing notes below.

---

## B1 — The foundation the epic "consumes" does not exist (blocking)

The epic is *gated on `orun-service-catalog` SC7* and repeatedly states it renders
the composition's typed `contract.inputs` (design §3) and its `scaffold` block
(design §4, `compositions.md` §5), consuming them "directly … no parallel schema"
(design invariant 2, S-7).

As-built reality:

| Assumed to exist (SC7/§3/§5) | Actual code | Where |
|---|---|---|
| Typed `contract.inputs` (per-field `type/default/values/pattern/required/**secret**`) on the composition | Untyped `ParameterSchema map[string]interface{}` — a raw draft-07 JSON Schema, and in the current examples it's **externalized** to a separate `kind: ComponentSchema` doc via `schemaRef` | `internal/model/composition.go:61`; `internal/composition/registry.go:1065` `compileSchema`; `examples/compositions/.../schema.yaml` |
| A `scaffold` block (`files:[{from,to}]`, `postCreate`) on the composition | **No `Scaffold` type at all**; no `scaffold:` in any composition YAML | grep: only hit is a comment in `internal/model/preset.go` |
| SC7's typed contract carried onto the resolved `Composition` entity | Entity carries only `source/digest/version/lifecycle/effects` — **no `contract`, no `scaffold`** | `internal/nodes/assemble.go:361` `compositionSpec`; `internal/objplan/compositions.go:21` `CompositionMeta` |
| `secret: true` per-input marker (the whole basis of §6) | Does not exist; schema is arbitrary JSON Schema with no secret-field concept | — |

The parent's own records confirm this is a *deferral dressed as "landed"*:
`orun-service-catalog/IMPLEMENTATION-STATUS.md` SC7 row reads **"Landed
(Composition-as-Entity from the lock; authored contract/semver pending)"**, and
the model-strengthening pass added only `version`+`lifecycle`, never the typed
`contract`. Meanwhile `risks-and-open-questions.md` **D-18** asserts *"the
composition `scaffold` block stays an authored foundation"* — but no such
foundation was ever built. Compositions haven't even migrated off
`apiVersion: sourceplane.io/v1alpha1`.

**Consequence.** SCF0 cannot begin as written — there is nothing to render and no
`secret` marker to enforce §6 against. Before any SCF milestone, someone must do
the *parent's* deferred §3+§5 authoring-model work: add the typed `contract` (with
`secret`) and the `scaffold` block to `internal/model`, carry them through
`internal/catalogresolve`/`internal/objplan`, and project them onto the
`Composition` entity in `internal/nodes`/`catalogmodel`. That is a real epic in
its own right, and it is a hard prerequisite this spec currently hides behind a
"landed" checkbox.

**Recommendation.** Either (a) re-open and schedule the parent's `contract.inputs`
+ `scaffold` authoring work as an explicit **SCF-prerequisite milestone** in this
epic (honest about the scope), or (b) fold that work into SCF0 and drop the
"extraction-and-detail, not a new model" framing — it *is* new model.

---

## B2 — The SaaS side contradicts the single-artifact thesis (blocking, cross-repo)

The epic's defining claim is **single-artifact ownership**: one composition owns
`create → build → deploy`; scaffolding is a *pure render* of that composition's
`contract.inputs` + `scaffold` block; and the web form is explicitly deferred as
*"the portal's build … fed by the same `contract.inputs` schema"* (README
non-goals; design §2). This is positioned as the anti-Backstage moat.

Where orun-cloud is actually headed (verified across `specs/` and `apps/`):

- **`contract.inputs` has zero occurrences anywhere in orun-cloud** — no SaaS spec
  or code commits to consuming a composition's typed inputs.
- **`saas-catalog-portal` (CP0–CP5) shipped read-only.** Its README puts the
  scaffolder **explicitly out of scope** ("the golden-path scaffolder … owned by
  `saas-service-catalog` SC6/SC7"). The "Register service" button in the shipped
  console is an **inert design placeholder** — `catalog-portal.tsx` renders
  `<CatalogHeader/>` with no `onRegister` handler wired. So "the portal's build"
  is, today, nobody's build.
- **`saas-service-catalog` SC7 (the nominal owner) is Draft/unstarted** and — this
  is the crux — designs scaffolding as a **template registry**, not a composition
  renderer: `listCatalogTemplates` + a zod-form over *the template's own params* +
  `scaffold(templateId, params)` that **opens a PR into a git repo** via the
  integrations broker; the result becomes catalog-valid *later* through the normal
  `orun catalog push`. No `contract.inputs`, no composition, no "catalog-valid by
  construction at exit."

That SaaS design **is** the Backstage model this epic's thesis rejects: a template
artifact owned/versioned independently of the runtime, emitting a stub PR that
becomes valid downstream. The two repos are heading in opposite directions on the
exact thing this epic is about, and the seam the epic leans on ("same
`contract.inputs` feeds the portal form") is **not adopted anywhere** on the SaaS
side.

**Recommendation — pick one, explicitly, with the SC7 owner:**
1. **Converge (preferred if the moat matters):** re-point SaaS SC7 to render a
   composition's `contract.inputs`/`scaffold` — the web form becomes a *second
   front-end over the same engine/contract* this epic builds, and the CLI
   `internal/scaffold` is the shared source of truth. This makes the "one artifact"
   claim true across both surfaces.
2. **Diverge (accept two paths):** drop this epic's "the portal builds the form
   from the same schema" non-goal and state plainly that SaaS "create" is a
   separate template-registry path. The CLI scaffolder then stands alone and the
   single-artifact thesis is a CLI-only property.

Leaving it as-is ships a spec whose headline moat is contradicted by the product's
own roadmap.

---

## C1 — "Structural import denylist wired into the existing import gate" — no such gate exists

design §3.2 and SCF0 rely on *"the existing import gate, cf. `orun-service-catalog`
parent precedent for banned imports"* to structurally forbid `os/exec/net/io/time/
rand` in `internal/scaffold`. There is **no generic Go-import gate** in the repo:
no `depguard`/`forbidigo`/golangci import rules, and the "banned" precedent
(`internal/nodes/assemble_test.go:77`) forbids banned **output tokens in emitted
JSON**, not package imports. The mechanism the funcmap sandbox depends on must be
**built** (a small AST/import-set test, or a depguard config), not "wired into"
something already there. Cheap to do — but SCF0's "Done when" presumes it exists.

## C2 — "Resolve cleanly onto the source composition" is heavier than a single call

The output gate (design §7, SCF3) is well-motivated, and the two parsers it needs
are real (`internal/model.ComponentManifest` permissive; `internal/catalogmodel.
ComponentYAML` strict). But **"resolve"** in this codebase is only the
whole-catalog `catalogresolve.BuildCatalog(ctx, opts, inputs)` — there is no
single-component "resolve this generated file onto its composition" entry point.
Two real costs the plan understates:

- Validating one generated `component.yaml` means either running a full catalog
  resolve with the new file injected, or building a new narrow resolve path.
- "Lands *on the composition it was scaffolded from*, not merely parses" (§7.3,
  S-3) needs an explicit assertion that the resolved component's
  `spec.composition` binding equals the source composition — which itself only
  exists once B1's `contract`/binding work lands. Worth calling out as its own
  SCF3 sub-task.

---

## What's solid (keep as-is)

- **Engine decision.** `text/template` + constrained funcmap is right, and it's
  *already the house convention*: the planner renders step fields with stdlib
  `text/template` (`internal/planner/planner.go:307` `renderTemplateString`).
  One consistency note: the planner's model is an **ambient `.orun.*` namespace**
  with **no funcmap**, whereas this epic mandates a **flat `.serviceName` model**
  with a funcmap. Two template dialects in one tool is defensible (the flat model
  is the safer choice for untrusted templates) but should be a *stated* divergence,
  and the epic should reuse `renderTemplateString`'s caching/error-wrapping shape
  rather than re-invent it.
- **Secret-reference-not-literal** (§6) is grounded: `internal/secretref` +
  `internal/secretpolicy` already model `secret://` references, so a generated
  `component.yaml` referencing (not materializing) a secret is consistent with
  authored components.
- **Determinism, path containment, fail-closed** are the correct invariants and
  are all buildable with stdlib once B1 lands.

---

## Sequencing note

Even setting B1 aside, this epic is **ahead of demand**. On the SaaS roadmap,
self-service create is the *last, detachable, highest-lift tail* (SC7), gated on
IG4 (integrations broker, itself Draft) and a premium entitlement; the first
catalog slice is deliberately browse/graph/insights with **zero authoring**. If
the CLI scaffolder is the near-term deliverable, that's a fine standalone bet —
but then B2 option 2 (diverge) should be the honest framing, and the epic should
stop claiming the portal as its GUI. If convergence (B2 option 1) is the goal, this
epic should be *upstream* of SaaS SC7 and SC7 re-drafted against it — which is not
the current sequencing on either side.

## One-line asks for the epic owner

1. Reclassify the epic: it depends on unbuilt parent §3+§5 model work — schedule
   that as an explicit prerequisite, or fold it in and drop "not a new model" (B1).
2. Reconcile with SaaS SC7 now, before scheduling — converge on one engine or
   declare two paths (B2).
3. Fix two grounding claims: build (don't assume) the import gate (C1); scope the
   single-component resolve + composition-binding check (C2).
