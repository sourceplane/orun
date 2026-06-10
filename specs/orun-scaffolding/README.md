# Spec: orun-scaffolding

**Golden-path self-service "create a new service" is delivered by the *same*
composition that later builds and deploys it: one composition artifact defines
`create → build → deploy`, owned once, versioned once. A developer picks a paved
road, answers the composition's typed `contract.inputs`, and orun renders the
composition's `scaffold` template into starter files plus a catalog-valid
`component.yaml` that resolves cleanly and is already on that golden path.** The
thing that scaffolds you in is the thing that keeps producing your catalog truth.

This is an *extraction-and-detail* spec, not a new model. It lifts the
golden-path scaffolding content of `specs/orun-service-catalog/` (the §5
`scaffold` block in `compositions.md`, and the `contract.inputs` schema in §3)
into its own epic so the scaffolding **engine** — `internal/scaffold` (new) —
gets the design depth the parent deferred to "the SC9 detailed design"
(`compositions.md` §11, open edge: *scaffold template engine*). It (1) fixes the
**engine decision** (Go stdlib `text/template` with a constrained funcmap), (2)
fixes the **flow** (`orun compositions scaffold` / `orun create`), and (3) fixes
the **output-validation gate** (the generated component MUST pass both
`component.yaml` parsers and resolve).

> **The defining contrast is single-artifact ownership.** Backstage's
> **Template is a separate artifact** from the runtime that builds the
> service — owned, versioned, and free to drift independently. orun's
> `create → build → deploy` is **one composition artifact, one owner, one
> version**: the scaffold's inputs are the golden path's `contract.inputs`, and
> the file it emits resolves onto the path that produced it. Scaffolding here is
> a *rendering* of an existing catalog entity, never a parallel template tree.

## Status

| Field | Value |
|-------|-------|
| Status | **Status: Draft (v2) — for review, not scheduled** |
| Builds on | `specs/orun-service-catalog/` (compositions as catalogued golden-path entities; the SC7 envelope + typed `contract`; the §5 `scaffold` block; the three-plane model + CR-1) |
| Extracts | the golden-path scaffolding of `compositions.md` §5 + the `contract.inputs` schema of §3 into a standalone engine epic (the parent's SC9 detail, deferred at `compositions.md` §11 "scaffold template engine") |
| Gated on | `orun-service-catalog` **SC7** (Composition-as-Entity: the envelope + typed `contract` must exist before there is anything to render); SC9 in the parent plan becomes this epic |
| Engine decision (locked) | Go stdlib `text/template` with a **constrained funcmap** (no file/exec access); inputs are driven by the composition's `contract.inputs` schema; rendering is sandboxed + deterministic |
| Decisions locked | one composition artifact for create→build→deploy (no separate Template); the scaffold is a pure render of `contract.inputs` against the composition's `scaffold` block; secrets are never written to a generated file (CR-1); the generated `component.yaml` MUST pass both parsers + resolve before exit `0` |
| Milestone prefix | **SCF** (`SCF0 → SCF3`) |

## The one-paragraph thesis

The parent epic makes a composition a catalogued, owned, versioned golden path
with a typed `contract` (`compositions.md` §2/§3) and lets it *produce* catalog
truth on every run via `effects` (§4). The one piece it sketches but defers is
**self-service create**: a developer should be able to start a new service *on*
a golden path without hand-writing a `component.yaml` and hoping it resolves.
This epic delivers that. `internal/scaffold` (new) renders the composition's
`scaffold.template` against the values a developer supplies for the same
`contract.inputs` schema the portal renders as a form — emitting starter files
and a `component.yaml` that is **catalog-valid by construction**: it passes the
strict catalog parser, the permissive plan-engine parser, and a full resolve, so
the new service is on the paved road from its first commit. The engine is Go
stdlib `text/template` with a constrained funcmap (no file or process access),
so a malicious or careless template cannot read the filesystem, shell out, or
leak a `secret` input into a generated file.

## The flow

```
orun compositions scaffold <composition> [--out dir]      (orun create fronts the same flow)
────────────────────────────────────────────────────
  pick golden path        ─► resolve the Composition entity (kind: Composition, SC7)
        │                       └─ its contract.inputs schema + its scaffold block
        ▼
  prompt for contract.inputs ─► typed prompts/flags (string/number/bool/enum/object/array)
        │                       └─ validate (required · pattern · enum · default); secret: redacted, never persisted
        ▼
  render scaffold.template ─► text/template + constrained funcmap, model = validated inputs
        │                       └─ emit starter files + a component.yaml
        ▼
  validate generated output ─► component.yaml passes BOTH parsers + resolves cleanly on the golden path
        │                       └─ fail closed: a non-resolving scaffold is an error, not a stub
        ▼
  exit 0 (catalog-valid, on the golden path)
```

## Read order

1. **`design.md`** — the problem, goals/non-goals, the scaffolding engine
   (`internal/scaffold`) and the locked `text/template` + constrained-funcmap
   decision, the flow, sandboxing + determinism, the relationship to the
   composition `scaffold` block (`compositions.md` §5), output validation,
   invariants, and the sharpness register.
2. **`implementation-plan.md`** — milestones **SCF0 → SCF3**.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| `internal/scaffold` (new): the `text/template` harness + the constrained funcmap sandbox; `contract.inputs` → typed prompts/flags + validation (incl. `secret` handling); rendering the composition's `scaffold.template` → starter files + a `component.yaml`; the generated-output validation gate (both parsers + resolve); the `orun compositions scaffold <composition>` command + the `orun create` front-end | The composition envelope + typed `contract` themselves (delivered by `orun-service-catalog` SC7 — a hard dependency, not re-specified here); the `effects` producer model (parent SC8); `build`/`deploy` execution (today's `internal/composition` runtime, unchanged); `postCreate` hook *execution* design beyond declaring the seam (a `git-init`-class hook taxonomy is a follow-on); a GUI/web scaffolding form (the same `contract.inputs` schema feeds it, but the build is the portal's); remote template fetching beyond what the composition's own OCI distribution already provides (`compositions.md` §9) |

## Conventions

- Go for interfaces, JSON/YAML for on-disk schemas. Forward-slash logical paths,
  output paths root-relative to the `--out` directory.
- `lowerCamelCase` JSON. RFC 3339 / Z timestamps. Object IDs `"<algo>:<hex>"`.
- "MUST / SHOULD / MAY" carry RFC 2119 weight in `design.md` (the sandboxing,
  determinism, and output-validation contracts).
- The template model is **exactly** the validated `contract.inputs` values
  (`compositions.md` §3) — accessed as `{{ .serviceName }}` etc. — never an
  ambient or host-derived namespace.
- Entity keys are three-segment `<namespace>/<repo>/<name>` per
  `specs/archive/orun-component-catalog/identity-and-keys.md`, generalized with a `kind`
  (the generated `component.yaml` keys into this grammar like any authored one).

## Out-of-band references

- Parent epic: `specs/orun-service-catalog/` — esp. `compositions.md` §3
  (`contract`), §5 (`scaffold`), §11 (open edge: scaffold template engine);
  `design.md` §3 (CR-1, the three planes); `cli-surface.md` §6 (the
  `orun compositions scaffold` / `orun create` surface, SC9).
- Canonical house style: `specs/archive/orun-catalog-state/`.
- Two `component.yaml` parsers (both must accept the generated file): the
  permissive plan engine (`internal/model.ComponentManifest`) and the strict
  catalog (`internal/catalogmodel.ComponentYAML`, struct-generated schema).
- Packages: `internal/scaffold` (**new**), `internal/composition`
  (read the resolved `Composition` + `scaffold` block), `internal/catalogmodel`
  + `internal/model` (the two parsers, for the output gate),
  `internal/catalogresolve` (the resolve gate), `cmd/orun` (`compositions
  scaffold`, `create`).
