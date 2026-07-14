# Spec: orun-scaffolding (scaffolding & instantiation)

**One engine, one language, two scales.** Scaffolding a single component and
instantiating a whole repo (or composing one from several baseline repos) are
the *same operation* at different sizes: resolve typed inputs → resolve
source(s) → order a set of **modules** by dependency → place each module
(**render / copy / consume**) → validate → record provenance. A single
component is a Blueprint with **one** module; a repo is a Blueprint with
**many**. Nothing in the grammar changes with scale — only the module count and
which placement modes appear. This is delivered as **one orun-binary feature**
(`internal/scaffold` + `orun new`), not two tools.

This epic **absorbs and unifies** what were previously three separate strands:
the single-service golden-path scaffolder (the old SCF engine, extracted from
`orun-service-catalog` SC9), the SaaS "bootstrap factory" Blueprint/Instance
contracts + instantiator (was `orun-cloud`/`lumen` **BF10–BF14**), and the
per-component baseline fork tooling (was the baseline's `tooling/fork` +
`tooling/rebrand` Node scripts). All three collapse into the Blueprint model
below. The engine stays a pure, sandboxed Go package; SaaS/Node/Cloudflare
policy stays in the *baseline's own `blueprint.yaml` + declared hooks*, never in
orun.

> **The defining law is single-artifact ownership at every scale.** A Blueprint
> is a *render* of things that already exist — a composition's `contract`, or a
> baseline repo's components — never a parallel template tree that drifts
> independently (Backstage's mistake). The thing that scaffolds you in is the
> thing that keeps producing your catalog truth; the thing that instantiates a
> product is the same baseline that upgrades it.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (v3 — unified) — for review** |
| Supersedes | SCF single-service scaffolding (v2, this folder) · `orun-cloud`/`lumen` **BF10–BF14** (Blueprint + Instance contracts + instantiator) · the baseline's `tooling/fork/components.mjs` + `tooling/rebrand/rebrand.mjs` (their generic graph/ordering/rename logic folds into orun; their ecosystem specifics become declared hooks) |
| Builds on | `orun-service-catalog` **SC7** (Composition-as-Entity: the envelope + typed `contract`, for the single-component/template scale); orun's existing **discovery** (`internal/loader`, `discovery.roots`), **dependency DAG** (`internal/planner/graph.go` — topo + SCC + cycle detection), **content-addressed object store** (`orun objects`, for pinned reproducible sources + provenance), and **package fetch** (`internal/composition` dir/archive/oci) |
| Engine decision (locked) | Go stdlib `text/template` with a **constrained funcmap** (no file/exec/net/time/rand); rendering is sandboxed + deterministic. Placement adds two non-templating modes — verbatim **copy** and dependency-only **consume** — over the same sandbox |
| Decisions locked | one Blueprint artifact per scale (no separate Template); inputs are a typed `contract.inputs` schema (SC7); module order is orun's own DAG over **declared** edges (never sniffed from `wrangler`/`package.json`); secrets never written to a generated file (CR-1); every generated `component.yaml` MUST pass both parsers + resolve; a repo instantiation MUST pass `orun validate` + `orun plan --dry-run`; a `.orun/provenance.lock` records `blueprint@digest + source@digest + inputs-hash`; ecosystem post-steps are **declared hooks**, executed outside the sandbox |
| Milestone prefix | **SCF** (`SCF0 → SCF7`) |

## The one-paragraph thesis

orun today is a planner/executor + object-catalog + OCI package manager: it
discovers `component.yaml` from `discovery.roots`, builds a real dependency DAG
with topological sort and cycle detection, keeps a git-like content-addressed
object store, and renders plan steps through `text/template`. It is **not** a
scaffolder — there is no `create`/`new`/`instantiate`/`internal/scaffold` today.
This epic adds exactly that, as **one** feature: a Blueprint describes typed
`inputs`, zero-or-more `sources`, and an ordered set of `modules`, each placed
by `template | copy | consume`. With zero sources and one `template` module it
is the single-service golden-path scaffolder (catalog-valid by construction).
With a `git`/`oci`/`dir` baseline source and many modules it instantiates a
whole product repo from `values.yaml`, copying/consuming components **in orun's
own DAG order** — the same order the planner already computes, so the JS
reimplementation in `tooling/fork/components.mjs` is retired. Provenance + a
`… upgrade` path make it a factory, not a one-shot generator, at either scale.

## The flow (one pipeline, both scales)

```
orun new --blueprint <ref> [--values f.yaml] [--out dir]
  (aliases: orun create <composition> · orun instantiate --blueprint …)
────────────────────────────────────────────────────────────────
  resolve Blueprint        ─► kind: Blueprint (inline | Composition entity | repo-root blueprint.yaml)
        │                       └─ inputs schema (SC7 contract.inputs) + sources[] + modules[]
        ▼
  collect inputs           ─► typed prompts/flags (string/number/bool/enum/object/array); secret redacted
        │                       └─ validate required · pattern · enum · default
        ▼
  resolve sources          ─► inline | dir | oci | git → pinned into the object store (reproducible)
        │                       └─ zero sources ⇒ pure inline template (single-component scale)
        ▼
  order modules            ─► orun's DAG (internal/planner) over DECLARED dependsOn/wiring edges
        │                       └─ SCC-batched topo sort; cycles are an error (declared cycle-break markers only)
        ▼
  place each module (in order):
        │  template ─► text/template + constrained funcmap  (→ files, incl. component.yaml)
        │  copy     ─► verbatim bytes, path-contained
        │  consume  ─► record a pinned dependency; emit no bytes
        ▼
  gate (scales with scope) ─► component: both parsers + resolve · repo: orun validate + plan --dry-run
        │                       └─ fail closed: a non-validating scaffold is an error, not a stub
        ▼
  run declared hooks       ─► e.g. `pnpm install --lockfile-only` (outside the sandbox; declared, not built-in)
        ▼
  write provenance.lock    ─► blueprint@digest + source@digest + inputs-hash  (enables `… upgrade`)
        ▼
  exit 0
```

## Read order

1. **`design.md`** — the unified Blueprint model (§3), the module atom + the
   three placement modes (§4), source resolution (§5), ordering via orun's own
   DAG (§6), the sandbox/determinism/secret/path-containment contracts (§7–§9,
   carried from SCF v2), the scope-scaled output gate (§10), provenance +
   upgrade (§11), the hooks seam (§12), invariants, and the sharpness register.
2. **`implementation-plan.md`** — milestones **SCF0 → SCF7**.

## Scale table — the same grammar at three sizes

| Scale | `sources` | `modules` | Gate |
|-------|-----------|-----------|------|
| **Single component** (was SCF / SC7 `orun create`) | `[]` / inline | one, `mode: template`, `to:` inside an existing repo | both `component.yaml` parsers + clean resolve |
| **Full repo from a baseline** (was BF11/BF12) | one `git`/`oci`/`dir` source | many; `template` + `copy` + `consume` | `orun validate` + `orun plan --dry-run` + provenance |
| **Compose from several baselines** | a `sources[]` list | modules select their `source` | as repo, plus per-source pin in provenance |

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| `internal/scaffold` (new): the sandboxed `text/template` + constrained funcmap engine; the `copy` and `consume` placement modes; `contract.inputs` → typed prompts/flags/validation (incl. `secret`); source resolution (`inline`/`dir`/`oci`/`git`) pinned via the object store; module ordering **by reuse of `internal/planner`'s DAG** over declared edges; the scope-scaled output gate; `.orun/provenance.lock`; the `orun new` command (+ `create`/`instantiate` aliases) and the `… upgrade` 3-way re-render | The composition envelope + typed `contract` themselves (parent **SC7**, consumed not re-specified); the `effects` producer model (SC8); `build`/`deploy` execution (`internal/composition` runtime, unchanged); the *baseline's* `blueprint.yaml` content and its rename/`bind` map (authored in the baseline, not here — orun ships the contract + engine only); the **execution runtime** for arbitrary hooks beyond declaring the seam and a minimal audited allow-list (a full hook sandbox is a follow-on, §12); a web/GUI form (the same `inputs` schema feeds it; the build is the portal's) |

## Out-of-band references

- Parent epic: `specs/orun-service-catalog/` — `compositions.md` §3 (`contract`),
  §5 (the authored `scaffold` block, which this epic consumes for the
  single-component scale), §11; `cli-surface.md` §6 (`orun create` surface).
- Retired predecessors this epic subsumes: `orun-cloud`/`lumen`
  `specs/epics/saas-bootstrap-factory/` **BF10–BF14** (now: "author the
  baseline's `blueprint.yaml` against this contract"); the baseline's
  `tooling/fork/components.mjs` (its Tarjan/SCC ordering is subsumed by
  `internal/planner`) and `tooling/rebrand/rebrand.mjs` (its rename map becomes
  a Blueprint `bind`/`overlays` declaration).
- orun capabilities reused (code reality, not specs): `internal/loader` +
  `discovery.roots` (discovery); `internal/planner/graph.go` (DAG: topo sort,
  SCC, cycle detection); `internal/composition` (dir/archive/oci fetch);
  `orun objects` / the content-addressed store (pinned sources + provenance);
  `internal/planner` `text/template` step rendering (the engine baseline).
- Two `component.yaml` parsers (both must accept generated files): the
  permissive plan engine (`internal/model.ComponentManifest`) and the strict
  catalog (`internal/catalogmodel.ComponentYAML`).
