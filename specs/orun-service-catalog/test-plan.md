# Test Plan

> Coverage targets, the determinism + round-trip parity guards (the envelope's
> losslessness contract), the **change-engine parity gate** (the CV-1 safety net),
> the hash-discipline tests, the scorecard/effects fixtures, the up-conversion
> parity gate, the examples gate, and the E2E walk. Every test ties back to an
> invariant in `design.md` §11 and a sharpness edge S-1…S-9 (§12). The posture
> mirrors the predecessor packs (`orun-component-catalog/test-plan.md`,
> `orun-catalog-state/test-plan.md`): coverage gates fail CI, parity gates run
> both paths until the old one is removed, and property tests assert the
> invariant by name.

## 1. Coverage targets

House norm is **≥90%** statement coverage; the pure store-shaped packages hold
**≥95%**. A drop in any listed package fails CI via the existing
`scripts/check-coverage.sh` parser, extended with the new package list. The new
umbrella target `make test-orun-catalog` runs these with `-race`.

| Package | Target | What it proves |
|---------|--------|----------------|
| `internal/catalogmodel` | ≥95% | Envelope + per-kind `spec` + `EntityKind` enum are pure data; sanitizers, the `Owner→Group` alias, and the `v1alpha1→v1` converter are total. |
| `internal/catalogresolve` | ≥90% | Resolver determinism; CODEOWNERS ownership derivation; `relations.json` builder; `effects` fold. Stays pure (no store imports — `doc.go`). |
| `internal/nodes` | ≥90% | Envelope blob shapes + `AssembleCatalog`; `manifestHash` excludes `provenance`; per-kind round-trip is lossless (S-6). |
| `internal/objplan` | ≥90% | `mapManifest`→`mapEntity` (kind-aware); deterministic blob + `relations.json` + `catalog.json` materialization; `Deployment`/`Environment` emission. |
| `internal/objcatalog` | ≥95% | Kind-aware read view; inverse-edge materialization; tolerant of missing `impact/`; up-conversion on read. |
| `internal/affected` | ≥90% | Selection over `relations.json`; `optional`/`include` semantics preserved (CV-1). |
| `internal/composition` | ≥90% | `contract`/`lifecycle`/`version`/`extends` on the resolved node; `Composition` entity projection; `extends` deep-merge + cycle rejection. |
| `internal/scorecard` | — | **Extracted — tested in `specs/orun-scorecards/` (v2).** |
| `internal/scaffold` | — | **Extracted — tested in `specs/orun-scaffolding/` (v2).** |
| `cmd/orun` (`catalog *`, `compositions *`, `create`) | command + parity tests | Surface wiring; `--json` shape; migrate exit codes; help goldens. |

## 2. Determinism (Invariant 2, P-2 class)

Extends the existing `internal/objplan/catalog_determinism_test.go`
(`TestAssembleCatalog_DeterministicAcrossOrderings`) from the flat manifest to
the full envelope.

- **DT-1.** Two resolves of one source produce **byte-identical** entity blobs
  (every kind), `relations.json`, and `catalog.json`. Asserted over **≥100**
  random orderings of components, kinds, and authored field maps (`pgregory.net/
  rapid`), all collapsing to one object id per artifact.
- **DT-2 (edge ordering).** `relations.json` edges are sorted by
  `(from, fromKind, type, to)` (`data-model.md` §3); shuffling the resolver's
  input edge set yields a byte-identical blob.
- **DT-3 (counts).** `catalog.json.countsByKind` is order-independent and equals
  the materialized `entities/<Kind>/` cardinality per kind.

## 3. Round-trip / losslessness parity (Invariant 3 / S-6, the `Path` class)

The S-6 guard, modeled on the `orun-catalog-state` CG-1/CG-2 `Path`-drop
precedent: a fixture exercising **every** envelope block survives
`map → write → read` field-for-field. A field added to the struct but not the
mapper, or dropped by the reader, fails here.

- **RT-1.** One golden fixture populates **all** envelope blocks — `metadata`,
  `ownership`, `lifecycle`, `spec`, `relations`, `contracts`, `integrations`,
  `docs`, `links`, `provenance`, `extensions` (incl. an unknown `x-*` block and
  `annotations`). Assert `read(write(map(src)))` equals the source field-for-field;
  unknown `x-*`/`annotations` survive verbatim (`data-model.md` §8, S-6).
- **RT-2 (per kind).** RT-1 runs once per kind — `Component`, `API`, `Resource`,
  `System`, `Domain`, `Group`, `Composition`, `Environment`, `Deployment` — so
  each `spec` block (`data-model.md` §4/§5) round-trips losslessly.
- **RT-3 (null/absent parity).** An absent optional block and an explicit `null`
  read back identically (`data-model.md` §1) — `contracts`/`integrations`/
  `docs`/`links`/`extensions`.

## 4. Hash discipline (Invariant 4 / S-1)

| ID | Subject | Property |
|----|---------|----------|
| HD-1 | `manifestHash` | Covers the envelope **minus `provenance`** (`data-model.md` §10). |
| HD-2 | provenance-only change | Mutating only `provenance` (e.g. `inheritedFrom`, `resolver`) does **not** move the entity id. |
| HD-3 | envelope change | Mutating **any** non-`provenance` block **does** move the id. |
| HD-4 | `catalogHash` | Covers all `entities/**` blobs + `relations.json` + `catalogInputHash` + `resolverVersion`; changing any input moves it. |
| HD-5 | `resolverVersion` bump (S-1) | The envelope reshape moves every id exactly once; a re-resolve at the same `resolverVersion` re-stabilizes (the CS1 `Path` precedent). |

## 5. Change-engine parity gate (Invariant 6 / CV-1, SC2)

The single most important migration gate, following the `orun-catalog-state` CS8
precedent: `internal/affected` selection over `relations.json` MUST equal the
legacy five-graph (`graph/dependencies.json`) path. Both run in CI until parity
is green; the five-graph builder is removed only after.

- **CE-1.** A golden corpus of `(multi-component fixture, ChangeOptions)` cases;
  for each, the new `relations.json`-driven `Affected` set == the goldens captured
  from the five-graph path before migration.
- **CE-2.** Parity holds across `optional`, `include: always`, and
  `include: if-selected` edges; multi-change inputs; and
  `--intent-impact all|watch|none` (`data-model.md` §3 carries these verbatim).
- **CE-3 (never-under-select, S-4 inherited).** For every case, `Affected` is a
  **superset** of the truly-changed set. False positives pass; **any** false
  negative fails.

## 6. Ownership derivation (S-2)

| ID | Case | Expected |
|----|------|----------|
| OW-1 | CODEOWNERS longest-prefix over `identity.path` | `ownership.owner` set; `ownership.source: CODEOWNERS`. |
| OW-2 | authored `metadata.owner` present | authored value wins; `ownership.source: authored` (precedence authored > CODEOWNERS > inherited). |
| OW-3 | system-inherited claim, no direct match | owner inherited; `ownership.source: inherited`; recorded in `provenance.inheritedFrom`. |
| OW-4 | no claim anywhere | `ownership.owner: unknown`; `ownership.source: unknown`; flagged by the `has-owner` scorecard rule. |

CODEOWNERS is provided to the resolver as an **input** (the resolver stays pure,
`design.md` §10) — the test injects the parsed claim set, not a filesystem read.

## 7. Scorecard engine — tested in `specs/orun-scorecards/` (v2)

The scorecard predicate evaluator, `unknown`-on-missing, level rollup, and the
CR-1 L2-isolation assertion are covered by the **`specs/orun-scorecards/`** test
plan (v2). This epic asserts only the scorecard **foundations**:

- **LP-1 (live plane).** `catalog describe` for a live entity shows
  per-environment deployments/health derived purely from `objrun`; nothing is
  persisted into an L1 blob (CR-1).
- **LP-2 (maturity reserved).** `lifecycle.maturity` is emitted `null` in v1 and
  round-trips as `null` (the field exists; no evaluator populates it here).

## 8. Composition effects (S-7, SC8)

| ID | Case | Property |
|----|------|----------|
| EF-1 | `effects.graph` | Declared `deploysTo`/`produces`/`provisions`/`exposes` emit the declared `Environment`/`Deployment`/`Resource`/`API` entities + the `deployedTo`/`runsOn` edges into `relations.json`. |
| EF-2 | `effects.integrations` | Declared join-keys populate the component's `integrations` **declaration** (L1); the resolved **value** stays L2 (CR-1, `compositions.md` §4.2). |
| EF-3 | `effects.scorecards.satisfies` | Every component on the path inherits scorecard credit for the listed rule ids. |
| EF-4 (declared-vs-actual, S-7, MUST) | over-claim | A composition declaring `effects.integrations: datadog` (or `satisfies: [runs-tests]`) whose run produced no such effect surfaces the divergence as a **scorecard signal**, never silent credit. The live plane records only what `objrun` produced. |
| EF-5 | `extends` merge | A base + overlay composition deep-merges (`contract`/`effects` union, `policy.gates` overlay-wins); an `extends` cycle is rejected (`compositions.md` §7). |

## 9. Migration up-conversion (S-3, SC11) — Invariant 7

The lazy-conversion gate from `migration.md` §2. Up-conversion is in-memory only;
the persisted blob is byte-identical before and after.

- **UC-1 (totality).** Every `v1alpha1` fixture envelope up-converts without
  error; an unconvertible blob is a defect, not a fallback.
- **UC-2 (round-trip parity, MUST).** `convert(read(v1alpha1Blob))` equals the
  result of resolving the equivalent `v1` source — the parity property
  `migration.md` §2 requires.
- **UC-3 (`Owner→Group` alias).** A persisted `Owner` node and its `owns`/
  `ownedBy` edges read back as `Group` (`migration.md` §4); no on-disk blob is
  rewritten (assert the blob id is unchanged).
- **UC-4 (unknown preserved).** `x-*` extensions + `annotations` survive
  conversion verbatim (S-6).
- **UC-5 (`migrate --check` drift).** `orun catalog migrate --check` over a
  `v1alpha1` authored fixture **exits 5** (diff-pending), `1` on lint errors,
  `0` when clean (`migration.md` §8).
- **UC-6 (both-parsers + generated-schema gate).** Every migrated authored field
  is accepted by **both** the strict `catalogmodel.ComponentYAML.OpenSchema()`
  and the permissive `model.ComponentManifest` parser, **and** present in the
  generated schema; `make verify-generated` is green (Invariant 5,
  `migration.md` §6).

## 10. Examples gate (SC10)

- **EX-1.** `make examples-validate` and `make examples-plan` pass over the
  migrated `examples/**`.
- **EX-2.** `orun catalog refresh` over `examples/` yields the expected
  **multi-kind counts** (`catalog.json.countsByKind` matches a golden:
  Component/API/Resource/System/Domain/Group/Composition + any derived
  Environment/Deployment), with owners derived from the examples' CODEOWNERS.
- **EX-3.** Both `component.yaml` parsers accept **every** migrated example file
  (the two-parser invariant exercised over the real repo corpus, not just unit
  fixtures).

## 11. E2E walk

`cmd/orun/catalog_e2e_test.go`, extended through SC8/SC10. One end-to-end test
asserting the full author→produce loop, each step against a golden and the
`--json` shape:

```text
1.  Spin up an isolated workspace; author intent.yaml + component.yaml + a
    composition.yaml carrying contract/effects/policy + CODEOWNERS.
2.  orun catalog refresh → entities/<Kind>/, relations.json, catalog.json.
3.  Assert a populated MULTI-KIND catalog (countsByKind > 1 kind).
4.  orun catalog describe <component> --json → full envelope (ownership/
    lifecycle/contracts), ownership.source rendered (S-2).
5.  orun catalog graph / tree → renders from relations.json (one artifact, CV-1).
6.  orun run (objrun, dry-run) the golden-path composition.
7.  Assert DERIVED Deployment + Environment entities emitted from execution (SC4),
    with deployedTo/runsOn edges.
8.  orun catalog scorecard <component> → a level computed on orun-native data,
    raised by the composition's effects.scorecards.satisfies credit (SC8).
9.  Assert effects.integrations populated the component's integrations
    DECLARATION in L1; the resolved value lives in the L2 overlay (CR-1).
10. Assert determinism: an unchanged re-refresh yields byte-identical artifact
    ids (Invariant 2).
```
