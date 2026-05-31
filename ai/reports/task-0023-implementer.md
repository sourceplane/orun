# Implementer Report — task-0023 (Phase 2 Milestone C0)

- **Branch:** `impl/task-0023-c0-catalogmodel`
- **PR:** https://github.com/sourceplane/orun/pull/168
- **Commit:** `63faff755c89fa66a6e2e9d4824e32ce536cafb1`
- **Status:** Ready for verification

## Scope

Implements **Milestone C0** of `specs/orun-component-catalog/implementation-plan.md`: pure data models, deterministic encoders, ID/key construction, sanitizers, JSON Schema for `component.yaml`, golden fixtures, property tests, and the `internal/sourcectx` type/key/hash skeleton. No FS, no resolver, no CLI changes.

## Deliverables

### `internal/catalogmodel/` (pure data — no `internal/*` imports)

| File | Purpose |
|---|---|
| `doc.go` | Package overview + spec back-references |
| `source_snapshot.go` | §1 `SourceSnapshot` + scope enum |
| `catalog_snapshot.go` | §2 `CatalogSnapshot` + `ManifestRef` + `CatalogObjects` |
| `component_manifest.go` | §3 `ComponentManifest` (identity/source/metadata/spec/runtime/resolution) |
| `component_yaml.go` | §6 `ComponentYAML` authored shape (+ `go:generate`) |
| `graph.go` | §4 `CatalogGraph` + node/edge |
| `events.go` | §5 `ComponentHistoryEvent` |
| `refs.go` | §8 `SourceRef` + `CatalogRef` |
| `indexes.go` | §9 `ComponentGlobalIndex` |
| `entity_ref.go` | Generic `EntityRef` for cross-kind lookups |
| `sanitize.go` | §12 sanitizers (branch/componentKey/eventKind/shortHex) |
| `keys.go` | §6 ID prefixes; §2/§3/§4 key formatters + validators |
| `entropy.go` | Package-private monotonic ULID entropy |
| `canonical.go` | `CanonicalEncode` + `PrettyEncode` |
| `hashes.go` | `ManifestHash` (excludes Source + provenance per §10), `CatalogInputHash` wrapper |
| `schema/component-yaml.schema.json` | Generated JSON Schema (committed) |
| `schema/gen/main.go` | Reflection-based schema generator |
| `testdata/golden/*.json` | 9 byte-stable canonical fixtures |
| `testdata/genfixtures/main.go` | Bootstrapper that regenerates the goldens from Go values |
| `*_test.go` | Roundtrip + property + unit tests |

### `internal/sourcectx/` (skeleton only — C1 adds resolver)

| File | Purpose |
|---|---|
| `doc.go` | Package overview |
| `model.go` | `WorkspaceState` + `Scope()` precedence per §2 |
| `keys.go` | `BuildSourceSnapshotKey` → delegates to `catalogmodel` |
| `hash.go` | `DirtyHash` (§7) + `CatalogInputHash` (§8) |
| `sourcectx_test.go` | Scope, key, hash determinism tests |

### Build / CI

- `Makefile`: `test-state-redesign` extended to run new packages and gate `Sanitize*` coverage at **== 100%**. New `verify-generated` target reruns `go generate` and fails on drift.
- `.github/workflows/state-redesign-tests.yml`: gates `make test-state-redesign` and `make verify-generated` on every PR + push to main.

## Spec coverage (C0 line-items)

| C0 item | Where |
|---|---|
| Pure structs per `data-model.md` §§1–9 | `internal/catalogmodel/*.go` |
| `CanonicalEncode` (sorted, no-whitespace) | `canonical.go` |
| `PrettyEncode` (sorted, 2-space) | `canonical.go` |
| ID prefixes `src_/cat_/cmp_` | `keys.go` |
| `SourceSnapshotKey` formatter + validator | `keys.go` |
| `CatalogSnapshotKey` formatter + validator | `keys.go` |
| `componentKey` validator | `keys.go` |
| `SanitizeBranch/ComponentKey/EventKind/ShortHex` | `sanitize.go` |
| `manifestHash` (provenance + source excluded) | `hashes.go` |
| `dirtyHash` (sorted-tar SHA-256) | `sourcectx/hash.go` |
| `catalogInputHash` (§8 ordered bundle) | `sourcectx/hash.go` |
| `component.yaml` JSON Schema (committed) | `schema/component-yaml.schema.json` |
| Golden fixtures + roundtrip | `testdata/golden/`, `roundtrip_test.go` |
| **T-IDK-1** order-invariant canonical encode | `property_test.go` |
| **T-IDK-3** `manifestHash` provenance invariant | `property_test.go` |
| **T-IDK-5** sanitizers total / no panics | `property_test.go` |
| `Sanitize*` 100% coverage gate | `Makefile` |

## Verification commands

```bash
make test-state-redesign   # green; Sanitize* coverage = 100%
make verify-generated      # ✅ generated artifacts up-to-date
go test ./...              # all packages green (with -race)
go vet ./...               # clean
go build ./...             # clean
```

## Out of scope / explicitly deferred

- Git probe + resolver populating `WorkspaceState` from a real workspace → **C1**.
- CLI commands, on-disk writer, indexes/refs persistence → **C2/C3**.
- Phase 1 → Phase 2 migration.
- Schema generator at the moment uses reflection over Go types; we may swap for `invopop/jsonschema` if the schema needs richer constraints.

## Risk notes for verifier

1. **Schema generator** is bespoke (reflection over `ComponentYAML`). Keep an eye on whether the produced JSON Schema is rich enough for downstream UI/validation use; can be replaced without spec impact since the schema file is the contract.
2. **`itoa` helper** in `sourcectx/keys.go` is a small zero-alloc decimal renderer used only for PR numbers; could swap for `strconv.Itoa` if simpler-is-better wins out.
3. **`testdata/genfixtures/`** is a `main` package that ships in the repo so fixtures are reproducible — it does not run on `go test ./...`. If unwanted, can move to `_testdata/` or behind a build tag.
4. **`internal/catalogmodel/schema/gen/`** is also a `main` package included in the module; same reasoning applies.

All of the above were chosen to keep C0 contained and inspectable. No spec deviations.
