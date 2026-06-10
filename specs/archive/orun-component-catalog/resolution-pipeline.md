# Resolution Pipeline

How repo files become a `CatalogSnapshot`. This is the only place where
inheritance and inference rules live; the writer in `catalog-store.md` is
purely persistence.

---

## 1. Pipeline

`internal/catalogresolve.Resolve(ctx, opts) (*ResolvedCatalog, error)` runs
the following stages in order. Each stage is pure (no FS writes); only stage
results feed the next stage. The full pipeline is deterministic given the
same inputs.

```text
1.  Resolve SourceSnapshot          → internal/sourcectx
2.  Discover component.yaml files   → walk repo from intent.yaml root
3.  Load intent.yaml catalog block  → defaults, validation flags, inference flags
4.  Load stack/composition metadata → from referenced stack sources
5.  Load component-authored YAML    → parse + schema-validate per component
6.  Infer runtime metadata          → from package files, Dockerfiles, READMEs
7.  Apply inheritance               → defaults < composition < component
8.  Resolve dependency refs         → cross-component, API, resource entity refs
9.  Validate manifests              → required fields, cycle policy, owner policy
10. Compute manifestHash[]          → per-component deterministic hash
11. Build CatalogGraph              → nodes + edges (dependency, system, api, resource, owner)
12. Compute catalogHash             → see identity-and-keys.md §9
13. Assemble CatalogSnapshot        → summary, objects, resolver block
14. Return ResolvedCatalog          → no FS side-effects up to this point
```

The writer (`internal/catalogstore`) consumes the `ResolvedCatalog` and
performs the ordered writes in `catalog-store.md` §3.

## 2. Discovery

- Roots: every directory recursively under the workspace root, **excluding**
  paths matched by `intent.yaml` `catalog.discovery.exclude` (default:
  `node_modules/`, `.git/`, `dist/`, `build/`, `vendor/`, `.orun/`).
- File pattern: `component.yaml` (case-sensitive). `component.yml` is also
  accepted; both forms in the same directory is a validation error.
- Each discovered file produces one `(authoredComponent, sourceFile)`
  tuple before stage 5.

## 3. Inheritance precedence

Lower wins **only when the higher layer left the field unset.** Explicit
values are never overridden by lower layers.

```text
1. component.yaml explicit fields           ← highest
2. component-local inferred files (stage 6)
3. composition / stack defaults (stage 4)
4. intent.yaml catalog defaults (stage 3)
5. (Phase 3) org/project SaaS defaults      ← lowest
```

For map-valued fields (`metadata.labels`, `metadata.annotations`), each key
is treated independently — explicit keys are not overwritten, but missing
keys are filled from lower layers.

For list-valued fields (`metadata.tags`, `dependencies.components`), explicit
lists win wholesale; lower layers fill only when the list is unset (not just
empty — empty `[]` is treated as explicit).

## 4. Inference

Inference layer (stage 6) is **disabled** when `intent.yaml`
`catalog.inference.enabled = false`. Otherwise, each toggle gates one source
of inferred runtime facts:

| Flag | Inputs | Outputs |
|------|--------|---------|
| `inference.packageJson` | `package.json`, `pnpm-lock.yaml`, `yarn.lock`, `package-lock.json` | `runtime.inferred.languages` += `typescript`/`javascript`; `runtime.inferred.packageManagers`; `runtime.inferred.frameworks` (heuristic match on dependencies — `hono`, `express`, `next`, `vite`, …) |
| `inference.dockerfile` | `Dockerfile`, `Containerfile` | `runtime.inferred.infra` += `docker`; `runtime.files.dockerfile` |
| `inference.terraform` | `*.tf`, `terraform.tf.json` | `runtime.inferred.infra` += `terraform` |
| `inference.helm` | `Chart.yaml` | `runtime.inferred.infra` += `helm` |
| `inference.readme` | `README.md` | falls back to first paragraph as `metadata.description` if unset |

Inference is **additive**: explicit `runtime.inferred.*` lists in
`component.yaml` (rare, advanced) are preserved and inference results are
unioned in.

For every inferred field the resolver records provenance under
`resolution.inferredFrom` — a list of repo-relative file paths.

## 5. Dependency resolution

Stage 8 turns authored references into resolved entity refs:

- `spec.dependsOn[*].component` is resolved against the discovered component
  set. A short name (`identity-worker`) resolves within the same `repo`; a
  fully qualified key (`sourceplane/orun/identity-worker`) is allowed
  cross-repo (same workspace).
- `spec.providesApis[*]` and `spec.consumesApis[*]` are recorded as nodes in
  `graph/apis.json`. Phase 2 does not enforce that consumed APIs have a
  declared provider; that becomes a `--strict` validation in C8.
- `spec.dependencies.resources.uses[*]` is recorded as nodes in
  `graph/resources.json`.

Cycle policy: cycles are **warned** by default. They become validation
errors when the dependency type is `deploy-after` (i.e. a deployment ordering
constraint) or when `--strict` is set.

## 6. Validation rules

| Rule | Default | `--strict` |
|------|---------|------------|
| `metadata.name` is set | error | error |
| Component key uniqueness | error | error |
| `metadata.owner` is set | warn | error |
| `spec.lifecycle` is set | warn | error |
| `spec.system` is set | off | error |
| `spec.composition.type` is set | warn | error |
| Dependency target exists | warn | error |
| Cycle in `calls` / `depends-on` edges | warn | error |
| Cycle in `deploy-after` edges | error | error |
| Component name matches `^[a-z0-9._-]+$` | warn | error |
| Owner is in known-owners list | off | error iff `validation.allowUnknownOwners=false` |

`intent.yaml` `catalog.validation.*` flags shift defaults per repo.

## 7. Determinism

Stage 11 (graph build) and stage 13 (snapshot assembly) sort all collections:

- `nodes` and `edges` ordered by `(kind, key, type)`.
- `objects.components` ordered by `componentKey`.
- `summary.*` counts derived from sorted collections.
- All map-valued fields encoded with sorted keys when hashed.

Stage 6 (inference) is the most likely source of non-determinism. Mitigation:

- File walk uses `filepath.WalkDir` with deterministic directory sort (Go's
  default lexical order).
- Heuristic framework detection uses a fixed-order rule list; a tied match
  resolves to the first match in the list.
- All file reads go through a tiny IO shim with a `clock.Time` seam so tests
  can pin timestamps; the resolver itself never reads `time.Now()`.

A property test (T-RES-1) asserts that running `Resolve` twice on the same
fixture produces byte-identical `ResolvedCatalog`.

## 8. Errors

Typed errors in `internal/catalogresolve`:

- `ErrComponentInvalid{Path, Reason}`
- `ErrDuplicateComponent{Key, Paths []string}`
- `ErrDependencyMissing{From, To}`
- `ErrCycle{Path []string, EdgeType string}`
- `ErrIntentInvalid{Path, Reason}`
- `ErrInferenceFailed{Path, Reason, Underlying error}` — never fatal in
  default mode; logged and inference for that file is skipped.
- `ErrResolverInternal{Stage int, Underlying error}` — bug bucket; surfaces
  the failing stage number for triage.

Validation results carry severity: `Error` aborts the resolver,
`Warning` is collected into a `[]ValidationIssue` returned alongside the
`ResolvedCatalog` so the writer can persist them into
`catalog.json.validation` (a non-hashed sidecar field).

## 9. Caching

`internal/catalogresolve` may cache `ResolvedCatalog` keyed by
`SourceSnapshot.catalogInputHash` for the duration of a single CLI process.
Cross-process caching is out of scope for Phase 2 (the on-disk catalog
itself is the cache); a future Phase 3 may introduce a memoized resolver
service.

## 10. Authored model schema check

Stage 5 validates `component.yaml` against an embedded JSON Schema generated
from `internal/catalogmodel`. Schema generation is part of `go generate
./internal/catalogmodel` and committed (no runtime codegen). Unknown fields
trigger a warning in default mode and an error in `--strict`.
