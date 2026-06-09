# Design

> Self-service "create a new service" is a *render* of an existing golden-path
> composition: `internal/scaffold` (new) renders the composition's
> `scaffold.template` against the developer's answers to the composition's typed
> `contract.inputs`, emitting starter files plus a `component.yaml` that is
> catalog-valid by construction. This doc fixes the engine (a locked
> `text/template` + constrained-funcmap decision), the flow, sandboxing +
> determinism, the relationship to the composition `scaffold` block, the
> output-validation gate, the invariants, and the sharpness register. The
> authoring shape it consumes is `specs/orun-service-catalog/compositions.md`
> §3/§5. RFC 2119 keywords are binding.

## 1. Problem

1. **There is no self-service create on the golden path.** The parent epic makes
   a composition a catalogued, owned, versioned entity with a typed `contract`
   (`compositions.md` §2/§3), but a developer starting a new service still
   hand-writes `component.yaml` and hopes it resolves onto a paved road. Nothing
   turns "I want a new Cloudflare worker" into a working component *on* the
   composition that builds Cloudflare workers.
2. **The template engine was deferred.** `compositions.md` §5 sketches a
   `scaffold` block (`files: [{ from, to }]`, `postCreate`) and §11 explicitly
   lists *"scaffold template engine — the templating language and its sandboxing
   for `internal/scaffold` (deferred to the SC9 detailed design)"* as an open
   edge. The engine choice, its sandbox, its input binding, and the
   output-validation contract are unspecified.
3. **A scaffold can leak or escape.** A template that can read files, shell out,
   or interpolate a `secret` input into a generated file is a supply-chain and
   secret-leakage hazard. The parent's CR-1 (`design.md` §3) forbids secrets in
   L1 blobs; nothing yet forbids a `secret` input from being written into a
   *generated* file on disk.
4. **A generated stub is not a golden-path component.** Backstage emits files
   and a catalog-info *stub* and walks away (`compositions.md` §1/§5). orun's
   promise is stronger: the generated `component.yaml` must be **catalog-valid
   and resolve cleanly** — anything less ships a service that is *not* actually
   on the paved road.

## 2. Goals / non-goals

**Goals**
- A single scaffolding engine, `internal/scaffold` (new), that renders a
  composition's `scaffold.template` against a developer's `contract.inputs`
  values into starter files + a `component.yaml`.
- A **locked engine decision** (§3): Go stdlib `text/template` with a
  **constrained funcmap** — no file/exec access — driven by the composition's
  `contract.inputs` schema as the model.
- **Typed input collection** from `contract.inputs`: prompts and/or flags with
  per-field type, `default`, `values` (enum), `pattern`, `required`, and
  first-class **`secret`** handling.
- **Sandboxed, deterministic** rendering: identical inputs + composition → byte
  identical output; no host/ambient state reachable from a template.
- An **output-validation gate** (§7): the generated `component.yaml` MUST pass
  *both* `component.yaml` parsers and resolve cleanly before the command exits
  `0`.
- One artifact for `create → build → deploy`: the scaffold reuses the
  composition's `contract.inputs` and emits a component on that same path
  (`compositions.md` §5 — the Backstage contrast).

**Non-goals**
- The composition envelope + typed `contract` (delivered by parent **SC7** — a
  hard dependency, consumed here, not re-specified).
- The `effects` producer model (parent SC8); `build`/`deploy` execution (today's
  `internal/composition` runtime, unchanged).
- A web/GUI scaffolding form (the same `contract.inputs` schema feeds it; the
  build is the portal's).
- A general-purpose hook *runtime*: `postCreate` (`compositions.md` §5) is
  declared as a seam here; the hook taxonomy + sandbox for executing it is a
  follow-on (§9, deferred register).
- Remote template fetching beyond the composition's existing OCI distribution
  (`compositions.md` §9) — templates ride inside the composition package.

## 3. The scaffolding engine (`internal/scaffold`)

`internal/scaffold` is a new, pure package: given a resolved `Composition`
entity (its `contract.inputs` schema + its `scaffold` block) and a map of
validated input values, it produces an in-memory set of `(path, bytes)` files.
It does **not** read the host filesystem to render, does **not** execute
processes, and does **not** write to disk itself (the command layer writes the
returned set under `--out`).

### 3.1 Engine decision (LOCKED)

**The template engine is Go stdlib `text/template` with a CONSTRAINED funcmap.
Inputs are driven by the composition's `contract.inputs` schema; rendering has
no file or process access.** Rationale:

- **No new dependency, no new sandbox to audit.** `text/template` is already in
  the toolchain; its execution model is data-in, string-out with an explicitly
  supplied function map — there is no built-in `include`, `exec`, `readFile`, or
  network primitive. The sandbox is the *absence* of dangerous funcs, not a
  bolted-on jail.
- **The model is the contract.** The template's `.` is **exactly** the validated
  `contract.inputs` map (`compositions.md` §3): `{{ .serviceName }}`,
  `{{ .runtime }}`, etc. There is no ambient `env`, `os`, or host namespace.
- **Determinism by construction** (§5): `text/template` over a fixed model with
  a fixed, side-effect-free funcmap is a pure function.

Rejected alternatives (recorded for review): a richer engine
(`Jinja`-class / `gomplate`-class) — rejected because its sprig/`os`/`file`
funcs are exactly the file/exec surface this epic must *exclude*, and reproducing
its safe subset is more audit surface than stdlib; a bespoke mini-language —
rejected as unnecessary given the model is a flat typed map.

### 3.2 The constrained funcmap (MUST)

The funcmap MUST contain only **pure, string/data-shaping** helpers. It MUST
NOT expose any function that reads the filesystem, executes a process, opens a
network connection, reads environment variables, or returns nondeterministic
data (time, random, UUID). The initial allow-list (illustrative, finalized in
SCF0) is string/case/slug/quote/default helpers — e.g. `lower`, `upper`,
`title`, `kebab`/`slug` (DNS-safe), `trim`, `quote`, `default`, `indent`. Any
addition to the funcmap is a reviewed change, not an open extension point: the
**denylist is structural** (no `os`, `exec`, `net`, `io`, `time`, `rand` in the
package's import set — enforceable by the existing import gate, cf.
`orun-service-catalog` parent precedent for banned imports).

### 3.3 Package boundary & data flow

```
Composition entity (kind: Composition, SC7)
   ├─ contract.inputs   (schema: type/default/values/pattern/required/secret)
   └─ scaffold          (files: [{from, to}], postCreate)
        │
        ▼
internal/scaffold:
   CollectInputs(contract.inputs, flags/prompts) ─► validated values map  (secrets in-memory only)
   Render(scaffold, values)                       ─► []File{path, bytes}    (text/template + funcmap)
        │
        ▼
cmd/orun (compositions scaffold / create):
   write []File under --out
        │
        ▼
output gate:  parse(component.yaml) × 2  +  catalogresolve  ─► exit 0 | exit 1
```

`internal/scaffold` stays pure (no store, no fs-render, no exec) the way
`catalogresolve` stays pure in the parent (`design.md` §10): the composition +
inputs are *provided* to it; writing and validating happen at the command layer.

## 4. The flow (`orun compositions scaffold` / `orun create`)

The command surface is `compositions.md`-aligned (`cli-surface.md` §6):
`orun compositions scaffold <composition> [--out dir]`, with `orun create`
fronting the identical flow for the paved-road experience. Four stages:

1. **Pick the golden path.** Resolve the named composition to its `Composition`
   entity (SC7). `--out <dir>` defaults to cwd. Unknown composition → exit `6`
   (matches `cli-surface.md` §6).
2. **Prompt for `contract.inputs`.** Each input is collected by **type**
   (`string|number|boolean|enum|object|array`, per `compositions.md` §3) — as a
   `--<input>` flag when supplied, otherwise an interactive prompt. Each value
   is validated against the field's `required` / `pattern` / `values` (enum) /
   `default`. A field with `secret: true` is collected redacted (no echo) and
   handled per §6. A `contract.inputs` validation failure → exit `1`
   (`cli-surface.md` §6).
3. **Render `scaffold.template`.** Each `scaffold.files[]` entry renders its
   `from` template body and its `to` path **both** through `text/template`
   against the validated values map (`compositions.md` §5: `to:
   "src/{{ .serviceName }}.ts"`). One of the rendered files is the
   `component.yaml` (the §5 example renders `component.yaml.tmpl → component.yaml`).
4. **Emit + validate.** Write the rendered set under `--out`, then run the
   output-validation gate (§7). Exit `0` only on a scaffold that **resolves
   cleanly** on the golden path; otherwise the command fails (and SHOULD NOT
   leave a half-written, non-resolving tree presented as success — see §7).

`postCreate` (e.g. `git-init`, `compositions.md` §5) is recognized as a declared
seam; *executing* it is gated by the deferred hook design (§9) and is not
required for SCF0–SCF3.

## 5. Sandboxing + determinism (MUST)

- **No host reach (D-1, MUST).** A template MUST NOT be able to read a file,
  execute a process, open a socket, read an env var, or observe wall-clock /
  randomness. Enforced structurally by the funcmap allow-list (§3.2) and the
  package import denylist — `text/template` has no such primitives built in, so
  the only way to add them is a (banned) import or a (reviewed) funcmap entry.
- **Path containment (D-2, MUST).** Every rendered `to` path MUST resolve to a
  location **inside** `--out` after rendering; a `to` that escapes (`..`,
  absolute path, symlink target outside `--out`) MUST be rejected before any
  write. Template injection into `to` (S-1) cannot write outside the output dir.
- **Determinism (D-3, MUST).** `Render(scaffold, values)` is a pure function:
  identical `(composition, values)` MUST produce a byte-identical file set.
  Combined with the no-nondeterminism funcmap rule (§3.2), two scaffolds of the
  same inputs are reproducible — a property test mirrors the parent's resolve
  determinism guard (`orun-service-catalog` design §11.2).
- **Fail closed (D-4, MUST).** A template parse/exec error, a path-containment
  violation, or an output-gate failure is an error; the command MUST NOT report
  success for a scaffold that did not validate.

## 6. Secret handling (MUST)

`contract.inputs` fields carry `secret: true` (`compositions.md` §3). The parent
CR-1 forbids secrets in L1 blobs; this epic adds the on-disk-render rule:

- A `secret` input MUST be collected without echo and held **in memory only**.
- A `secret` input MUST NOT be written into any generated file — including the
  generated `component.yaml`. If a `scaffold.files[]` template interpolates a
  `secret` field, rendering MUST fail (or the value MUST be redacted to a
  placeholder), never silently emit the cleartext (S-2).
- The generated `component.yaml` references secrets the way authored components
  do (a secret *reference*, not a literal) — scaffolding never materializes a
  secret value into the catalog or onto disk.

## 7. Output validation (the golden-path guarantee, MUST)

Scaffolding produces a **catalog-valid component, not a stub** (`compositions.md`
§5). Before exit `0`, the generated `component.yaml` MUST:

1. **Pass the permissive plan-engine parser** (`internal/model.ComponentManifest`
   → `model.Component`) — the authoritative authoring contract consumed by
   `orun plan`.
2. **Pass the strict catalog parser** (`internal/catalogmodel.ComponentYAML`,
   validated against the struct-generated JSON schema) — consumed by
   `orun catalog refresh`.
3. **Resolve cleanly on the golden path** via `catalogresolve` — the resolved
   component must land *on the composition it was scaffolded from*, not merely
   parse.

This is the parent's "both parsers + generated schema" discipline
(`orun-service-catalog` design §11.5, README "Convention over configuration")
applied to *generated* output: the same file the human would hand-author must
pass the same gates. A scaffold that parses but does not resolve onto the path
is a failure (exit `1`), not a warning.

## 8. Invariants

1. **Pure render.** `internal/scaffold` performs no fs-read-to-render, no exec,
   no network, no env read; the composition + inputs are provided (§3.3).
2. **Model = contract.** The template model is exactly the validated
   `contract.inputs` values — one schema powers the form, the prompts, and the
   resolver's `parameters` validation (`compositions.md` §3).
3. **No secret on disk.** No `secret` input is written to any generated file,
   including `component.yaml` (§6, CR-1).
4. **Determinism.** Identical inputs + composition → byte-identical output (§5,
   D-3).
5. **Path containment.** Every emitted file lands inside `--out` (§5, D-2).
6. **Catalog-valid by construction.** The generated `component.yaml` passes both
   parsers and resolves on the golden path before exit `0` (§7).
7. **One artifact.** The scaffold renders an existing `Composition` entity;
   there is no separate, independently-versioned Template (README thesis).

## 9. Deferred / needs-later-attention

- **`postCreate` hook runtime.** The taxonomy + sandbox for executing hooks
  (`git-init` and beyond) is deferred; SCF declares the seam only. Executing
  arbitrary post-create commands reintroduces the exec surface §3.2 excludes, so
  it needs its own guardrails.
- **Web/GUI scaffolding form.** Out of scope; the `contract.inputs` schema is
  designed to feed it (it is the same form the portal renders, `compositions.md`
  §3).
- **Composition-to-composition scaffolding chains.** `contract.provides` /
  `requires` (`compositions.md` §3) could let one scaffold feed another; flat
  single-composition scaffolding is v1.

## 10. Sharpness register

| # | Sharp edge | Resolution |
|---|------------|------------|
| S-1 | **Template injection** — a hostile/careless template (or a `to` path built from input) writes outside `--out` or executes code. | `text/template` has no exec/file primitives; the funcmap is a pure allow-list (§3.2) with a structural import denylist; path containment (D-2) rejects any `to` escaping `--out` (§5). |
| S-2 | **Secret leakage into generated files** — a `secret: true` input is interpolated into a file (incl. `component.yaml`) and persisted in cleartext. | Secrets in-memory only; interpolating a secret into a file fails or redacts, never emits cleartext (§6); CR-1 keeps it out of L1 too. |
| S-3 | **Generated component parses but doesn't resolve** — a "stub" that looks valid but isn't on the golden path. | Output gate MUST run both parsers *and* a clean resolve onto the source composition before exit `0` (§7); fail closed (D-4). |
| S-4 | **Two parsers drift** so a generated file passes one and fails the other. | The gate runs *both* the plan-engine and catalog parsers (§7); inherits the parent's "both parsers + generated schema" rule and the project's parser-parity discipline. |
| S-5 | **Non-determinism creeps in** (time/random/UUID helper) and breaks reproducible scaffolds. | Funcmap forbids nondeterministic funcs (§3.2); determinism property test (D-3) guards it. |
| S-6 | **Half-written tree on failure** presented as success. | Fail closed (D-4): a gate/render/containment failure is an error; the command does not report success for an unvalidated scaffold (§4/§7). |
| S-7 | **Drift from the parent's `scaffold`/`contract` shape** if `compositions.md` §3/§5 evolves. | This epic *consumes* SC7's `contract`/`scaffold` types directly (no parallel schema); the model is the contract (invariant 2); gated on SC7 so the shape is fixed first. |
