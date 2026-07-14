# Implementation Plan — scaffolding & instantiation (unified)

> Milestone-based. Each states **goal**, **deps**, and **done when**. The engine
> harness + sandbox (SCF0) freezes the locked `text/template` + funcmap decision;
> input collection (SCF1) and the three placement modes (SCF2) are the core; the
> single-component command + gate (SCF3) delivers the smallest useful scale;
> sources (SCF4), DAG-ordered multi-module instantiation (SCF5), and provenance +
> upgrade (SCF6) grow it to the repo scale; SCF7 proves it by authoring the lumen
> baseline's blueprint and retiring the Node fork scripts.
>
> **Gated on `orun-service-catalog` SC7** for the single-component/template scale
> (the composition envelope + typed `contract`). The repo scale (SCF4+) reuses
> orun's existing discovery, DAG, object store, and OCI fetch — no external gate.

```
[ SC7 ] ─► SCF0 engine harness + funcmap sandbox
                 │
                 ▼
          SCF1 inputs → prompts/validation (incl. secret)
                 │
                 ▼
          SCF2 placement modes: template · copy · consume
                 │
                 ▼
          SCF3 orun new (single-component) + output gate      ◄─ smallest useful scale
                 │
                 ▼
          SCF4 source resolution: inline · dir · oci · git (pinned to object store)
                 │
                 ▼
          SCF5 multi-module instantiation, ordered by internal/planner's DAG
                 │
                 ▼
          SCF6 provenance.lock + `orun … upgrade` (3-way)
                 │
                 ▼
          SCF7 author lumen blueprint.yaml + retire tooling/fork + tooling/rebrand
```

---

## SCF0 — `internal/scaffold` skeleton + `text/template` harness + funcmap sandbox
**Goal:** the locked engine, sandboxed and pure, with nothing to place yet.
- Stand up `internal/scaffold` (new): a `Render(body, values) ([]byte, error)`
  seam over stdlib `text/template`; the **constrained funcmap** (pure
  string/case/slug/quote/default — design §7.2) and the **structural import
  denylist** (no `os`/`exec`/`net`/`io`/`time`/`rand`) wired into the existing
  import gate. Add the **ecosystem-neutrality lint** (no `pnpm`/`npm`/`wrangler`/
  `cloudflare` literals in the package — invariant 8).

**Deps:** SC7. **Done when:** a trivial template renders against a values map;
the funcmap exposes only the allow-list; an import-gate test fails the build on a
banned import; the neutrality lint fails on a planted `wrangler` literal.
**Design:** §7, §12.

## SCF1 — `inputs` → typed prompts / flags / validation
**Goal:** collect a validated values map from a blueprint's `inputs` (= SC7
`contract.inputs`).
- `CollectInputs(inputs, flags/prompts) (values, error)`: per-field type
  (`string|number|boolean|enum|object|array`), `default`, `values` (enum),
  `pattern`, `required`. `--<input>` flag or interactive prompt. `secret: true`
  collected without echo, in memory only, flagged for the no-disk rule (§8).

**Deps:** SCF0. **Done when:** every input type validates
(required/pattern/enum/default exercised); a secret never appears in returned
non-secret state; invalid input yields the exit-`1`-class error; ≥90% coverage.
**Design:** §7 (collection), §8.

## SCF2 — Placement modes: `template` · `copy` · `consume`
**Goal:** turn a module + validated inputs into placed output (or a recorded
dependency).
- `template`: render each file under `from` (and the `to` path) through the
  engine; enforce `bind` (a non-`bind` file referencing `.inputs` is a lint
  error). `copy`: verbatim bytes, no engine. `consume`: record a pinned
  dependency, emit nothing. All modes enforce **path containment** (§9); `template`
  + `copy` enforce the **secret rule / sweep** (§8). Assert **determinism** (§9)
  with a property test.

**Deps:** SCF1. **Done when:** a fixture module renders (template) / copies
(copy) / records (consume); identical inputs → byte-identical output; a
`..`/absolute/escaping `to` is rejected; a secret-in-`template` fails and a
`copy` byte-matching a secret fails; ≥90% coverage. **Design:** §4, §8, §9.

## SCF3 — `orun new` (single-component) + output gate
**Goal:** the smallest useful scale — a catalog-valid component by construction.
- Wire `cmd/orun`: `orun new --blueprint <ref> [--out dir]`, with `orun create
  <composition>` fronting the same flow for the paved-road experience
  (`cli-surface.md` §6). Resolve a one-module (`template`) blueprint or a
  Composition entity's `scaffold` block (§3), collect inputs (SCF1), place
  (SCF2), write under `--out`. Add the **output gate** (§10, component depth):
  plan-engine parser + catalog parser + **clean resolve onto the source
  composition**. Exit codes: `0` resolves · `1` input-validation failure · `6`
  unknown blueprint/composition. Fail closed.

**Deps:** SCF2. **Done when:** scaffolding a fixture service produces a
`component.yaml` that passes both parsers and resolves onto its composition
(exit `0`); a deliberately invalid scaffold fails the gate; the exit-code matrix
is covered; `orun create` and `orun new` share one flow. **Design:** §3, §10.

## SCF4 — Source resolution: `inline` · `dir` · `oci` · `git` (pinned)
**Goal:** let a blueprint pull module content from a baseline, reproducibly.
- Implement `sources[]` resolution feeding a single source-agnostic read
  interface: `inline` (carried), `dir` (snapshot to object store), `oci` (reuse
  `internal/composition` fetch), and the **new `git` resolver** (resolve
  `repo@ref` → commit digest → materialize the tree into the object store). Every
  source pinned by digest before any module reads it.

**Deps:** SCF3. **Done when:** a blueprint with a `git` source resolves to an
immutable digest and places a `copy` module from it; re-running with the same ref
is byte-identical; the same module places identically from a `dir` snapshot of
the same tree (source-agnostic). **Design:** §5.

## SCF5 — Multi-module instantiation, ordered by `internal/planner`'s DAG
**Goal:** the full-repo scale — many modules placed in dependency order.
- Lower `modules[]` into `internal/planner`'s graph (`graph.go`): SCC-batched
  topological order over **declared** `dependsOn`/`wiring` edges; cycles error
  unless a `cycleBreak` marker defers named edges (§6). Place batches in order.
  Add the **repo-scale gate** (§10): after placement run `orun validate` + `orun
  plan --dry-run`; the **idempotence check** (re-instantiate a baseline with its
  own values ⇒ empty diff modulo blueprint-owned files). Expose `orun
  instantiate --blueprint … --values … --out …` as the repo-scale alias of `orun
  new`.

**Deps:** SCF4. **Done when:** a multi-module fixture (incl. a declared binding
cycle → one SCC batch, and a `consume` module that emits nothing) instantiates in
correct order; the tree passes `orun validate` + `plan --dry-run`; the
idempotence check is green; ordering matches `orun plan --view dag`. **Design:**
§4, §6, §10.

## SCF6 — Provenance + `orun … upgrade`
**Goal:** make it a factory, not a one-shot generator.
- Write `.orun/provenance.lock` on success (blueprint@digest, source@digest(s),
  inputs-hash, per-module mode/target) at every scale. Implement `orun … upgrade`:
  re-render a newer blueprint/source against the lock and **3-way-merge** into the
  target as a reviewable diff under the file-ownership convention (blueprint owns
  manifests/templates/generated config; humans own feature code; conflicts
  surface, never overwrite).

**Deps:** SCF5. **Done when:** a scaffold writes a valid lock; bumping the
blueprint and running `upgrade` produces a merge diff that touches only
blueprint-owned files and preserves human edits; a component-scale scaffold can
also `upgrade`. **Design:** §11.

## SCF7 — Author the lumen baseline blueprint + retire the Node fork scripts
**Goal:** prove the model on the real baseline and delete the duplication.
- Author `blueprint.yaml` at the lumen repo root describing lumen as a blueprint
  of itself: modules for every component with declared `mode`
  (`template`/`copy`/`consume` — contracts/sdk/db/policy-engine = `consume`) and
  declared `dependsOn`/`wiring` edges (transcribed from what `components.mjs`
  sniffed). Move the `rebrand.mjs` rename map into `bind`/`overlays`. Declare the
  pnpm-lockfile resync and Cloudflare two-pass as `hooks` (§12). **Retire**
  `tooling/fork/components.mjs` and fold `tooling/rebrand/rebrand.mjs` into the
  blueprint; keep only what is genuinely a runtime hook.

**Deps:** SCF6 (+ coordination with the baseline repo). **Done when:** `orun
instantiate --blueprint . --values instance.yaml --out <tmp>` reproduces lumen
(idempotence green); the ordered instantiation matches the batches
`components.mjs --order` produced; `tooling/fork/components.mjs` is removed with no
loss of capability. **Design:** §3, §6, §12; retires baseline BF10–BF14.

---

## Cross-cutting (every milestone)
- **One engine, one language:** single-component, repo, and multi-baseline are
  the same `internal/scaffold` over the same `kind: Blueprint`; no second
  ordering implementation (invariants 1, 4).
- **Engine locked:** stdlib `text/template` + constrained funcmap; no file/exec
  primitive reachable; ecosystem-neutral core (invariants 3, 8).
- **Valid by construction, fail closed:** the gate scales with scope but never
  ships a stub (invariant 6). **No secret on disk**, **path containment**,
  **determinism** — each guarded by a test (invariant 5).
- **Provenanced at every scale** (invariant 7) — the capability that justifies
  living in the orun binary rather than a baseline script.
- This epic **consumes** SC7's `contract`/`scaffold` types for the
  single-component scale and introduces no parallel schema (S-11). It subsumes
  the retired SCF v2 single-service spec, BF10–BF14, and the `tooling/fork` +
  `tooling/rebrand` scripts.
