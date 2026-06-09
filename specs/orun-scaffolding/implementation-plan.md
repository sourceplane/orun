# Implementation Plan

> Milestone-based. Each states **goal**, **deps**, and **done when**. Agents may
> split/merge while keeping each PR reviewable and green. The engine harness +
> sandbox (SCF0) freezes the locked `text/template` + constrained-funcmap
> decision first; input collection (SCF1) and rendering (SCF2) are the core; the
> CLI + the output-validation gate (SCF3) deliver the golden-path guarantee.
>
> **Gated on `orun-service-catalog` SC7** (Composition-as-Entity: the envelope
> + typed `contract` must exist before there is anything to render). This epic
> *is* the parent's deferred SC9 detail (`compositions.md` §11, "scaffold
> template engine").

```
[ orun-service-catalog SC7 ] ─► SCF0 engine harness + funcmap sandbox
                                       │
                                       ▼
                                SCF1 contract.inputs → prompts/validation
                                       │
                                       ▼
                                SCF2 render scaffold.template → files + component.yaml
                                       │
                                       ▼
                                SCF3 orun compositions scaffold / orun create + output gate
```

---

## SCF0 — `internal/scaffold` skeleton + `text/template` harness + funcmap sandbox
**Goal:** the locked engine, sandboxed and pure, with nothing to render yet.
- Stand up `internal/scaffold` (new): a `Render(scaffold, values) ([]File, error)`
  seam over Go stdlib `text/template`. Define the **constrained funcmap**
  (pure string/case/slug/quote/default helpers — `design.md` §3.2) and the
  **structural import denylist** (no `os`/`exec`/`net`/`io`/`time`/`rand` in the
  package) wired into the existing import gate. No fs-read-to-render, no exec.

**Deps:** parent SC7. **Done when:** the package renders a trivial template
against a values map; the funcmap exposes only the allow-list; an import-gate
test fails the build if a banned package is imported; `internal/scaffold` has no
store/fs/exec dependency. **Design:** `design.md` §3.

## SCF1 — `contract.inputs` → typed prompts / flags / validation
**Goal:** collect a validated values map from the composition's `contract.inputs`.
- `CollectInputs(contract.inputs, flags/prompts) (values, error)`: per-field
  type (`string|number|boolean|enum|object|array`), `default`, `values` (enum),
  `pattern`, `required`. Surface each input as a `--<input>` flag or an
  interactive prompt. **`secret: true`** fields are collected without echo, held
  in memory only, and flagged for the no-disk rule (`design.md` §6). A validation
  failure returns a typed error (→ exit `1`).

**Deps:** SCF0. **Done when:** every `contract.inputs` type validates
(required/pattern/enum/default exercised); a `secret` value never appears in
returned non-secret state; invalid input yields the `1`-class error;
≥90% coverage. **Design:** `design.md` §4 (stage 2), §6; `compositions.md` §3.

## SCF2 — Render the composition `scaffold.template` → files + `component.yaml`
**Goal:** turn validated inputs + the `scaffold` block into an emitted file set.
- Render each `scaffold.files[]` entry — **both** the `from` body and the `to`
  path — through `text/template` against the values map (`compositions.md` §5).
  Enforce **path containment** (D-2): reject any `to` escaping `--out`. Enforce
  the **no-secret-on-disk** rule (D-... §6): interpolating a `secret` into a file
  fails/redacts. Emit `component.yaml` as one of the rendered files. Assert
  **determinism** (D-3) with a property test.

**Deps:** SCF1. **Done when:** a fixture composition renders starter files + a
`component.yaml`; identical inputs → byte-identical output (determinism test); a
`..`/absolute/escaping `to` is rejected; a secret-in-file template fails; ≥90%
coverage. **Design:** `design.md` §4 (stage 3), §5, §6.

## SCF3 — `orun compositions scaffold` / `orun create` CLI + output-validation gate
**Goal:** the golden-path self-service create, catalog-valid by construction.
- Wire `cmd/orun`: `orun compositions scaffold <composition> [--out dir]` +
  `orun create` fronting the same flow (`cli-surface.md` §6) — resolve the
  composition, collect inputs (SCF1), render (SCF2), write under `--out`. Add the
  **output-validation gate** (`design.md` §7): the generated `component.yaml`
  MUST pass the **plan-engine parser**, the **catalog parser**, and a **clean
  resolve onto the source composition**. Exit codes per `cli-surface.md` §6:
  `0` scaffold resolves cleanly · `1` contract-input validation failure · `6`
  unknown composition. **Fail closed** (D-4) on any gate failure.

**Deps:** SCF2. **Done when:** scaffolding a fixture service from a composition
produces a `component.yaml` that passes both parsers and resolves onto that
composition's golden path (exit `0`); a deliberately invalid scaffold fails the
gate (no false success); the exit-code matrix is covered; `orun create` and
`orun compositions scaffold` share one flow. **Design:** `design.md` §4 (stage
4), §7; `cli-surface.md` §6.

---

## Cross-cutting (every milestone)
- **Engine is locked:** Go stdlib `text/template` + the constrained funcmap; no
  alternate engine, no file/exec primitive reachable from a template
  (`design.md` §3, invariant 1).
- **Both parsers + resolve** gate every generated `component.yaml` before exit
  `0` (invariant 6) — never ship a stub.
- **No secret on disk** (invariant 3, CR-1); **path containment** (invariant 5);
  **determinism** (invariant 4) — each guarded by a test, not just asserted.
- This epic **consumes** SC7's `contract`/`scaffold` types directly; it
  introduces no parallel schema (S-7). It is the parent's SC9 detail.
