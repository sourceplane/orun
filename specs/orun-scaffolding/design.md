# Design — scaffolding & instantiation (unified)

> Scaffolding a component and instantiating a repo are one operation at two
> scales. This doc fixes the **unified Blueprint model** (§3), the **module**
> atom and its three placement modes — `template | copy | consume` (§4), **source
> resolution** (§5), **ordering by reuse of orun's own DAG** (§6), the
> sandbox/determinism/secret/path-containment contracts (§7–§9, carried intact
> from the v2 SCF engine), the **scope-scaled output gate** (§10), **provenance +
> upgrade** (§11), the **declared-hooks seam** (§12), the invariants, and the
> sharpness register. RFC 2119 keywords are binding.

## 1. Problem

1. **There is no self-service create, at any scale.** orun discovers
   components, builds a dependency DAG, and executes plans, but a developer
   starting a new *service* still hand-writes `component.yaml` and hopes it
   resolves, and a team starting a new *product* still runs ad-hoc scripts.
   Nothing in the binary turns "a new Cloudflare worker" **or** "a new SaaS from
   the lumen baseline" into a validated tree.
2. **The same graph is being computed twice.** orun already computes a
   topological, SCC-batched, cycle-checked component order
   (`internal/planner/graph.go`). The baseline's `tooling/fork/components.mjs`
   re-derives that same order in JavaScript (Tarjan over sniffed `wrangler`
   service bindings + `package.json` deps + `wiringComponents`). One of these is
   redundant; the orun one is authoritative.
3. **Three "scaffolding" strands drifted apart.** The single-service SCF engine
   (this folder, v2), the SaaS Blueprint/instantiator (BF10–BF14), and the fork
   scripts each invented their own inputs, ordering, and rename handling. They
   are the same pipeline and should be one feature.
4. **A generator without provenance is a permanent fork.** A one-shot copy
   (Backstage stub; a `git archive` snapshot) has no lineage, so instances can
   never be upgraded from the baseline. orun's content-addressed store makes a
   Merkle-pinned `provenance.lock` and a `… upgrade` re-render native — the one
   capability the Node scripts structurally cannot have.
5. **A scaffold can leak or escape.** A template that reads files, shells out,
   or interpolates a `secret` into a generated file is a supply-chain and
   secret-leakage hazard (carried from SCF v2, still binding for every mode).

## 2. Goals / non-goals

**Goals**
- **One** engine (`internal/scaffold`) and **one** language (a `kind: Blueprint`
  document) that cover the single-component, full-repo, and multi-baseline
  scales with no grammar change — only module count and mode mix differ (§3).
- A **module atom** with three placement modes: `template` (render), `copy`
  (verbatim), `consume` (pinned dependency, no bytes) (§4).
- **Source resolution** for `inline`/`dir`/`oci`/`git`, each pinned into the
  content-addressed object store so a scaffold is reproducible and provenanced
  (§5).
- **Ordering by reuse of `internal/planner`'s DAG** over **declared** edges —
  never by sniffing framework files. `components.mjs`'s ordering is retired
  (§6).
- The v2 **locked engine** (Go stdlib `text/template` + constrained funcmap; no
  file/exec/net/time/rand), **sandboxing + determinism**, **secret handling**,
  and **path containment** — unchanged and binding for the `template` mode; the
  containment + secret rules also bind `copy` (§7–§9).
- A **scope-scaled output gate** (§10): a component scaffold passes both
  `component.yaml` parsers + resolve; a repo instantiation additionally passes
  `orun validate` + `orun plan --dry-run`; fail closed.
- **Provenance + upgrade** (§11): `.orun/provenance.lock`; `orun … upgrade`
  re-renders a newer blueprint/source against the lock and 3-way-merges.
- A **declared-hooks seam** (§12) for ecosystem post-steps (pnpm lockfile
  resync, a Cloudflare two-pass cycle-break) — declared in the blueprint,
  executed outside the template sandbox, never compiled into orun.

**Non-goals**
- The composition envelope + typed `contract` (parent **SC7**; consumed for the
  single-component scale, not re-specified).
- `effects` (SC8); `build`/`deploy` execution (`internal/composition`,
  unchanged).
- The *content* of any baseline's `blueprint.yaml` (its module list, `bind`
  map, hooks) — that is authored in the baseline; orun ships the contract +
  engine.
- A full hook execution sandbox beyond a minimal audited allow-list (§12,
  deferred). A web/GUI form (the `inputs` schema feeds it; the build is the
  portal's).

## 3. The unified Blueprint model

A Blueprint is a single typed document (`kind: Blueprint`). Its three sections
are scale-independent; a one-module blueprint with no sources **is** the
single-service scaffolder, and the same schema with a `git` source and many
modules **is** the product instantiator.

```yaml
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: cloudflare-worker            # single-component blueprint …
  # name: lumen-saas                 # … or a whole-repo blueprint

# (1) INPUTS — the SC7 contract.inputs schema, verbatim. One schema powers
#     prompts, flags, and a portal form; identical for one component's params
#     or a whole instance's values.
inputs:
  serviceName: { type: string, pattern: "^[a-z][a-z0-9-]*$", required: true }
  runtime:     { type: enum, values: [node, python], default: node }
  orgName:     { type: string, required: true }
  # secret: true → collected without echo, in-memory only, never written (§8)

# (2) SOURCES — where module content comes from. Zero ⇒ pure inline templates
#     (single-component scale). One ⇒ a baseline fork. A list ⇒ compose from
#     several baselines. Each source is pinned by digest into the object store.
sources:
  - name: baseline
    kind: git                        # inline | dir | oci | git
    repo: github.com/sourceplane/lumen
    ref: <tag-or-digest>

# (3) MODULES — the atom of scaffolding. One module = a component; many = a repo.
modules:
  - name: worker
    mode: template                   # template | copy | consume
    source: baseline                 # omit ⇒ inline body carried in the blueprint
    from: apps/{{ .serviceName }}    # path in the source (templated)
    to:   apps/{{ .serviceName }}    # path in the target (templated, contained)
    bind: [wrangler.template.jsonc, component.yaml]   # files that take inputs
    dependsOn: [contracts]           # extra prerequisite edges — DECLARED
  - name: contracts
    mode: consume                    # pinned dependency, not copied
    source: baseline
    from: packages/contracts

# (4) HOOKS — declared, ecosystem-specific, run outside the sandbox (§12).
hooks:
  postInstantiate:
    - id: pnpm-lockfile
      run: ["pnpm", "install", "--lockfile-only"]
```

**Why this is genuinely one model, not two dressed alike:**

- **`inputs` is one typed contract** for every scale (§7 collection is
  identical). Secret handling is identical.
- **`module` is the sole unit.** A component is one module; a repo is N. A
  module MAY be `kind: blueprint` to nest (a repo blueprint referencing
  component blueprints) — recursion is defined but v2 (§13).
- **Ordering is always orun's DAG** (§6). A single component is a one-node
  graph (trivially ordered); a repo is the SCC-batched order the planner already
  produces.
- **Modes are the only thing repo-scale adds.** `template` alone serves the
  from-scratch component; `copy`/`consume` are simply unused there.
- **The gate scales, the law does not** (§10): *fail closed, valid by
  construction* holds at both sizes.

## 4. The module atom and its three placement modes

`internal/scaffold` places each module by `mode`. All three obey path
containment (§9); `template` and `copy` obey the secret rule (§8).

- **`template` (render).** Each file under `from` (and the `to` path) is
  rendered through `text/template` + the constrained funcmap (§7) against the
  validated `inputs`. `bind` names the files that legitimately interpolate
  inputs; a template outside `bind` that references `.inputs` is a lint error
  (keeps the interpolation surface auditable). This is the SCF v2 behavior,
  generalized from "one component's files" to "a module's file subtree." One
  rendered file MAY be a `component.yaml` (gated in §10).
- **`copy` (verbatim).** Bytes are copied unrendered from `from` to `to`. Used
  for baseline files that must arrive byte-identical (fixtures, generated
  lockfiles, assets). A `copy` module still passes through the **secret sweep**
  (§8) and **path containment** (§9); it does not touch the template engine.
- **`consume` (dependency, no bytes).** The module is recorded as a pinned
  dependency of the instance (source + digest) and **emits no files**. This is
  how a fork depends on `contracts`/`sdk`/`db`/`policy-engine` without copying
  them (the BF11 copied-vs-consumed decision, now first-class). `consume`
  targets participate in ordering (§6) and provenance (§11) but never in the
  output tree.

The mode set is closed. A fourth mode is a reviewed change, not an open
extension point (mirrors the funcmap allow-list discipline, §7.2).

## 5. Source resolution

`sources[]` declares where module content is fetched from; `modules[].source`
selects one by name (absent ⇒ the module carries an inline template body in the
blueprint). Every resolved source is **pinned by digest into the
content-addressed object store** before any module reads it — so a scaffold of
the same `(blueprint, sources, inputs)` is byte-reproducible and its provenance
is a Merkle link (§11).

| `kind` | Resolution | Reuses |
|--------|-----------|--------|
| `inline` | template bodies carried in the blueprint (no fetch) | — |
| `dir` | a local path, snapshotted into the object store | `orun objects` write/checkout |
| `oci` | an OCI artifact (a packaged baseline/stack), pulled + extracted | `internal/composition` OCI fetch (`resolveOCISource`) |
| `git` | a repo at a ref, resolved to a commit digest and snapshotted | **new** — a `git`-kind resolver feeding the object store |

`git` is the one genuinely new fetch path (orun has no `kind: git` today; §
"code reality" in README). It resolves `repo@ref` to an immutable commit,
materializes the tree into the object store, and hands `internal/scaffold` a
read-only, digest-pinned view — the same interface `dir`/`oci` present, so the
placement engine is source-agnostic.

**Multiple sources** (compose a product from several baselines) is expressed as
a `sources[]` list with modules selecting their `source`. v1 SHOULD implement
single-source fully and MUST keep the list shape; cross-source path collisions
are resolved by explicit `to:` (last-writer is an error, not silent — §10 fail
closed).

## 6. Ordering — reuse orun's DAG, over declared edges

Module order is computed by **`internal/planner`'s existing graph machinery**
(`graph.go`: `TopologicalSort` (Kahn), SCC condensation, `DetectCycles`), not by
a second implementation. `internal/scaffold` lowers `modules[]` into that graph:

- **Edges are declared, never sniffed.** A module's prerequisites come from its
  `dependsOn` (and `wiring`/`requires` where present) in the blueprint — *not*
  from parsing `wrangler.jsonc` service bindings or `package.json` like
  `components.mjs` does. The baseline author moves that knowledge into the
  blueprint once; orun stays framework-agnostic. (Migration note: the baseline's
  existing edge sources are transcribed into module `dependsOn` when its
  `blueprint.yaml` is authored — SCF7.)
- **SCC batches are honored.** A declared binding cycle (e.g. the
  `{billing, membership, events, notifications}` cluster) surfaces as one SCC
  and is placed as one atomic batch, exactly as the planner condenses it.
- **Cycles are an error** unless the blueprint declares a `cycleBreak` marker
  naming the feedback edges to defer (the generic form of the baseline's
  `cycle-break.mjs` two-pass; the *deploy-time* two-pass itself is a declared
  hook, §12).
- **Single component ⇒ trivial.** A one-module blueprint is a one-node graph;
  ordering is a no-op. The same code path serves both scales.

## 7. The template engine (carried from SCF v2, LOCKED)

### 7.1 Engine decision (LOCKED)
**Go stdlib `text/template` with a CONSTRAINED funcmap.** No new dependency, no
new sandbox to audit; the model's `.` is exactly the validated `inputs` map
(`{{ .serviceName }}`), with no ambient `env`/`os`/host namespace; determinism
is by construction (§7.3). Rejected: sprig/`gomplate`-class engines (their
`os`/`file` funcs are the exact surface this must exclude) and a bespoke
mini-language (unnecessary for a flat typed map).

### 7.2 The constrained funcmap (MUST)
Only **pure string/data-shaping** helpers: `lower`, `upper`, `title`,
`kebab`/`slug` (DNS-safe), `trim`, `quote`, `default`, `indent` (finalized in
SCF0). It MUST NOT expose any function that reads the filesystem, executes a
process, opens a socket, reads env, or returns nondeterministic data
(time/random/UUID). The **denylist is structural**: no `os`/`exec`/`net`/`io`/
`time`/`rand` in the package import set, enforced by the existing import gate.
Any addition is a reviewed change, not an open extension point.

## 8. Secret handling (MUST)
`inputs` fields with `secret: true` are collected without echo and held **in
memory only**. A `secret` MUST NOT be written into any generated file (including
`component.yaml`): a `template` that interpolates a secret **fails** (or redacts
to a placeholder); a `copy` module whose bytes match a collected secret value
trips the **secret sweep** and fails. Generated manifests reference secrets the
way authored ones do — a reference, never a literal (CR-1). This binds every
placement mode.

## 9. Path containment + determinism (MUST)
- **Path containment (MUST).** Every rendered/copied `to` MUST resolve **inside**
  `--out` after templating; a `to` that escapes (`..`, absolute, symlink out)
  MUST be rejected before any write. Binds `template` and `copy`.
- **Determinism (MUST).** `place(module, inputs, source@digest)` is pure:
  identical inputs + pinned source ⇒ byte-identical output. A property test
  guards it (mirrors the parent's resolve-determinism guard).
- **Fail closed (MUST).** A parse/exec error, containment or secret violation,
  ordering cycle, or gate failure is an error; the command MUST NOT report
  success for an unvalidated scaffold, and MUST NOT leave a half-written tree
  presented as success.

## 10. The output gate — scaled by scope (MUST)
The gate is *fail-closed valid-by-construction* at both scales; only its depth
scales.

- **Single component (any blueprint that emits a `component.yaml`):** the
  generated `component.yaml` MUST (1) pass the permissive plan-engine parser
  (`internal/model.ComponentManifest`), (2) pass the strict catalog parser
  (`internal/catalogmodel.ComponentYAML`), and (3) **resolve cleanly** — for a
  from-a-composition scaffold, onto the composition it was scaffolded from.
- **Full repo instantiation:** additionally MUST pass `orun validate` (intent +
  all components) and `orun plan --dry-run` (the whole DAG compiles offline)
  before exit `0`. An **idempotence check** (re-instantiating a baseline with
  its own values reproduces it, modulo blueprint-owned files) is the CI guard
  that keeps blueprint and reality from drifting (BF12's idempotence test,
  retained).

A scaffold that parses but does not validate/resolve is a failure (exit `1`),
never a warning.

## 11. Provenance + upgrade
- **`.orun/provenance.lock`** is written on success at every scale: the
  `blueprint@digest`, each resolved `source@digest`, the `inputs` hash, and the
  per-module mode/target. Even a single scaffolded component records its lineage.
- **`orun … upgrade`** re-renders a newer blueprint/source version against the
  lock and 3-way-merges into the target as a reviewable diff, under a
  file-ownership convention (blueprint owns manifests/templates/generated
  config; humans own feature code; conflicts surface, they do not overwrite).
  This is BF14's `factory upgrade`, native to the object store and available to
  component scaffolds too. Without this, an instance is a permanent fork — the
  defining reason the feature lives in orun, not in a throwaway script.

## 12. The declared-hooks seam
Ecosystem post-steps that orun must **not** internalize — `pnpm install
--lockfile-only` (Node/pnpm), the Cloudflare two-pass cycle-break (deploy
config), secrets-sync seeding — are declared in the blueprint's `hooks` and run
**after** placement, **outside** the template sandbox. This is the same
"declare the seam, defer the runtime" posture the v2 SCF spec took for
`postCreate`. v1 ships a **minimal audited allow-list** executor (explicit
`run: [argv]`, no shell, logged, opt-in per instantiation); a general hook
sandbox is a follow-on. The rule that keeps orun ecosystem-neutral: **no pnpm,
npm, wrangler, or Cloudflare string appears in `internal/scaffold`** — the
baseline declares them, orun executes declared argv.

## 13. Invariants
1. **One engine, one language.** Single-component, repo, and multi-baseline are
   the same `internal/scaffold` over the same `kind: Blueprint` (§3).
2. **Module is the atom.** A component is one module; a repo is many; modes
   (`template|copy|consume`) are the only scale-dependent variable (§4).
3. **Pure render.** `internal/scaffold` does no fs-read-to-render, no exec, no
   net, no env; sources are provided pinned (§5/§7).
4. **Order is orun's DAG over declared edges.** No sniffing of framework files;
   `components.mjs` ordering is retired (§6).
5. **No secret on disk**, any mode (§8). **Path containment**, any mode (§9).
   **Determinism** (§9).
6. **Valid by construction, fail closed** — the gate scales, the law holds
   (§10).
7. **Provenanced.** Every scaffold writes a lock enabling upgrade (§11).
8. **Ecosystem-neutral core.** Framework specifics live in the baseline's
   blueprint + declared hooks, never in orun (§12).

## 14. Sharpness register
| # | Sharp edge | Resolution |
|---|-----------|-----------|
| S-1 | **Template injection / path escape** (any mode) | `text/template` has no exec/file primitive; funcmap is a pure allow-list with a structural import denylist (§7.2); path containment rejects escaping `to` for both `template` and `copy` (§9). |
| S-2 | **Secret leakage into generated files** | in-memory only; interpolation fails/redacts; `copy` bytes pass a secret sweep (§8); CR-1 keeps it out of L1. |
| S-3 | **Generated component parses but doesn't resolve** | gate runs both parsers *and* a clean resolve; repo scale adds validate + plan (§10); fail closed. |
| S-4 | **Two parsers drift** | the gate runs *both* parsers on every generated `component.yaml` (§10), inheriting the parent's parser-parity discipline. |
| S-5 | **Non-determinism creeps in** (time/random) | funcmap forbids it; sources pinned by digest; determinism property test (§9). |
| S-6 | **Ordering re-implemented / drifts from the planner** | ordering *is* `internal/planner`'s DAG; no second implementation; declared edges only (§6). |
| S-7 | **Ecosystem specifics leak into orun** | hooks are declared argv run outside the sandbox; a lint forbids pnpm/wrangler/cloudflare strings in `internal/scaffold` (§12, invariant 8). |
| S-8 | **Copy-vs-consume gets it wrong** (fork copies what it should depend on) | `consume` is a first-class mode recorded in provenance; the blueprint declares mode per module; the idempotence gate catches a mis-placed module (§4/§10). |
| S-9 | **Instance can't be upgraded** (permanent fork) | `provenance.lock` + `… upgrade` 3-way-merge, native to the object store (§11). |
| S-10 | **Cross-source collision** (multi-baseline) | explicit `to:`; a silent last-writer is an error under fail-closed (§5/§9). |
| S-11 | **Drift from SC7's `contract`/`scaffold` shape** | this epic *consumes* SC7's types for the single-component scale (no parallel schema); gated on SC7 so the shape is fixed first. |
