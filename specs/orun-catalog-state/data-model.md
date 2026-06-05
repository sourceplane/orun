# Data Model

> The impact-index schema, the catalog read-view types, the `Path` fix, and the
> on-disk/ref layout. All persisted records are canonical JSON blobs (see
> `orun-object-model/object-store.md` §3); `*Id` fields are `"<algo>:<hex>"`
> edges; optional fields omitted when empty; `lowerCamelCase`; RFC 3339 / Z.

## 1. On-disk layout (additions)

The catalog tree gains one sibling subtree, `impact/`:

```
catalogs/<key>/                  ← CatalogSnapshot Merkle root (unchanged identity rules)
  catalog.json                   ← CatalogSnapshot record (unchanged)
  components/<name>.json         ← ComponentManifest blobs (gains identity.path — §4)
  graph/<edgeKind>.json          ← CatalogGraph slices (unchanged; dependencies live here)
  impact/
    ownership.json               ← NEW — the ownership map + classification rules
    fingerprints/<name>.json     ← NEW — per-component input fingerprints (the virtual Merkle tree)
```

`impact/` and its `fingerprints/` subtree are **always present** (possibly empty)
so the catalog tree shape is uniform and its Merkle id stays deterministic — same
discipline `AssembleCatalog` already applies to `components/`/`graph/`.

Both `impact/` artifacts ship in this spec because **orun's change-detection
engine consumes them** (`change-detection.md`): `ownership.json` maps changed
paths to components; `fingerprints/` is the cockpit's content-aware change source.

> **Deferred (no local consumer):** `impact/closure.json` (reverse-transitive
> dependency closure). The engine walks `graph/dependencies.json` in-process at
> component scale; a materialized closure is only worth it for a remote consumer
> (`specs/orun-affected-worker/`, under review).

## 2. Ownership map — `impact/ownership.json`

Edge: belongs to its `CatalogSnapshot` (lives inside the catalog tree; not a
separate addressable root). Identity: content of this blob (folds into the
catalog Merkle root). Deterministic function of the resolved catalog +
discovery — carries **no timestamps**.

```json
{
  "kind": "ImpactOwnership",
  "schemaVersion": 1,
  "components": {
    "apps/api-edge": "sourceplane/orun/api-edge",
    "apps/web":      "sourceplane/orun/web",
    "libs/shared":   "sourceplane/orun/shared"
  },
  "globalPaths": ["intent.yaml"],
  "globalBlocks": ["catalog.defaults", "catalog.inference", "catalog.discovery", "metadata.namespace", "metadata.repo"],
  "structuralFilenames": ["component.yaml", "component.yml"],
  "ignoreDirs": [".git", ".orun", "build", "dist", "node_modules", "vendor"]
}
```

Field-by-field:

- `kind` — `"ImpactOwnership"`. Required.
- `schemaVersion` — integer; **bumped on any shape change** so a consumer (the
  worker, a different orun version) can reject an index it doesn't understand.
  Required. (Risk register R-3; cross-version drift guard.)
- `components` — map of **workspace-relative component directory** →
  `componentKey`. The directory is the `dirname` of the component's
  `Identity.Path` (the `component.yaml` location). Deepest-prefix wins when a
  changed file is under nested component dirs — the consumer resolves a path by
  longest matching key.
- `globalPaths` — exact workspace-relative files whose change is `global`
  (currently just `intent.yaml`; future: workspace-root config files).
- `globalBlocks` — the `intent.yaml` blocks that are catalog-relevant. A
  consumer that can parse `intent.yaml` uses these to decide `global` vs
  `ignore`; a consumer that cannot **MUST** treat any `globalPaths` change as
  `global` (the conservative reading — CD-1).
- `structuralFilenames` — basenames whose add/remove/edit is `structural`.
- `ignoreDirs` — directory basenames pruned by discovery (mirrors
  `catalogresolve.defaultExcludes` + intent excludes); paths under them are
  `ignore`.

**Classification algorithm (the reference, executed by the `internal/affected`
engine — `change-detection.md` §2):**

```
classify(path):
  if path ∈ globalPaths:                      → global   (block-level refine if parseable)
  if basename(path) ∈ structuralFilenames:    → structural
  if any segment(path) ∈ ignoreDirs:          → ignore
  k := longest key in components that prefixes path
  if k exists:                                 → component(components[k])
  else:                                        → ignore   (file owned by no component)
```

This is intentionally tiny: the consumer does map lookups, not resolver
semantics (CD-4).

## 2b. Component fingerprints — `impact/fingerprints/<name>.json` (virtual Merkle tree)

Per-component input fingerprint, derived at resolve time. Deterministic; no
timestamps; folds into the catalog Merkle root.

```json
{
  "kind": "ComponentFingerprint",
  "schemaVersion": 1,
  "componentKey": "sourceplane/orun/api-edge",
  "dir": "apps/api-edge",
  "subtree": "sha256:…",                 // hash over the input file set (the leaf-set root)
  "files": {                              // workspace-relative → content hash (git blob sha for committed)
    "apps/api-edge/component.yaml": "sha256:…",
    "apps/api-edge/package.json":   "sha256:…",
    "apps/api-edge/Dockerfile":     "sha256:…"
  },
  "globalDigest": "sha256:…"              // hash of the catalog-relevant intent.yaml blocks (shared leaf)
}
```

- `files` covers the resolver's verified read-set: the component dir
  **non-recursive**, restricted to `component.yaml` + the inference candidates
  (`package.json`, lockfiles, `Dockerfile`/`Containerfile`, `*.tf`,
  `terraform.tf.json`, `Chart.yaml`, `README.md`). Over-approximating to the whole
  non-recursive dir is permitted (sound — `change-detection.md` §3).
- `subtree` is the comparison key: the cockpit computes the *current* subtree hash
  (git projection ⊕ dirty overlay) and a mismatch ⇒ that component changed.
- For committed files the content hash is the **git blob sha** (no re-hashing).
  The dirty overlay (local only) hashes the changed/untracked subset.
- **Determinism (S-11):** keys sorted; separators normalized to `/`; project
  git's path normalization for committed files; canonical-encode the overlay so a
  clean→dirty→clean cycle returns to the same `subtree`.

## 3. Read / engine view types — `internal/objcatalog` + `internal/affected`

Presentation-neutral, the catalog analogue of `objread.ExecutionView`. Go, not
persisted:

```go
// CatalogView is one resolved catalog, read from the graph.
type CatalogView struct {
    SourceID   string                  // edge: the source it was built from (freshness gate)
    HumanKey   string
    Components  []CatalogComponentView
    Graph       map[string]GraphView   // edgeKind → nodes/edges (dependencies, systems, …)
    Ownership  *OwnershipView          // nil if impact/ absent (older catalogs)
}

// CatalogComponentView mirrors a ComponentManifest, flattened for rendering.
type CatalogComponentView struct {
    ComponentKey string
    Name         string
    Namespace    string
    Repo         string
    Path         string                // from identity.path (requires §4)
    Type         string
    Domain       string                // spec.domain
    Environments map[string]EnvView    // spec.environments: {profile, active}
    DependsOn    []string              // spec.dependencies.components[].key (also in Graph)
    Metadata     map[string]any        // carried verbatim for the richer view
    Spec         map[string]any
}
```

The cockpit maps `[]CatalogComponentView → []services.ComponentSummary` in
`internal/tui/services` (not `objview`). Mapping:

| `ComponentSummary` | source |
|--------------------|--------|
| `Name` | `Name` |
| `Type` | `Type` |
| `Domain` | `Domain` |
| `Path` | `Path` (needs §4) |
| `Envs` | keys of `Environments` (or where `active`) |
| `Profile` | resolved per-env profile (default-profile rule preserved) |
| `DependsOn` | `DependsOn` |

### 3b. The change-detection result — `internal/affected.Result`

The single shape every surface (cockpit, plan, run, `orun catalog affected`)
consumes. Go, not persisted; full semantics in `change-detection.md` §2.

```go
type Result struct {
    DirectlyChanged  []string       // components whose own inputs changed
    Dependencies     []string       // forward deps of DirectlyChanged
    Dependents       []string       // transitive reverse deps of DirectlyChanged
    Affected         []string       // the selection surfaces act on (parity with existing --changed; §6)
    IntentMode       string         // "none" | "global" | "components"
    Confidence       string         // "high" | "low"
    NeedsFullResolve bool           // structural/global uncertainty
    Explain          []ExplainEntry // provenance for --explain
}
```

The `--json` projection of this (for `orun catalog affected`) is the
`CatalogAffectedResult.data` shape in `cli-surface.md` §3.

## 4. The `Path` fix (`nodes.ComponentIdentity`)

`catalogmodel.ComponentManifest.Identity.Path` exists but is dropped on the way
into the object model. Add `path` to the node identity:

```json
// nodes.ComponentIdentity — ComponentManifest.identity
{
  "componentKey": "sourceplane/orun/api-edge",
  "name": "api-edge",
  "namespace": "sourceplane",
  "repo": "orun",
  "path": "apps/api-edge/component.yaml"     // ADDED
}
```

- `path` is the workspace-relative `component.yaml` location (the resolver's
  `Identity.Path`). Required when known; omitted for a synthetic root component.
- `objplan/catalog.go:mapManifest` maps `cm.Identity.Path → m.Identity.Path`.
- **Identity impact:** adding the field changes the manifest blob hash → catalog
  Merkle id → `catalogs/current` target on the next resolve. Content-addressing
  absorbs it; the resolve memo (`cache/resolve/<srcId>-rv<n>.json`) misses once.
  No migration; old catalogs remain readable (the field is optional on read,
  `Path` empty for them — the parity test only runs against freshly-written
  catalogs).

## 5. Validation rules

- `ImpactOwnership.schemaVersion` MUST be present and ≥ 1.
- `components` keys MUST be workspace-relative, slash-separated, no leading `./`
  or trailing `/`; values MUST match the `componentKey` grammar
  `<namespace>/<repo>/<name>` (reuse the existing validator).
- The ownership map MUST be byte-deterministic: keys sorted lexically; arrays
  sorted; no map iteration order leaks (reuse `nodes.Encode` canonical
  encoding).
- `objcatalog.Load` MUST tolerate a missing `impact/` (older catalog) by
  returning `Ownership == nil` — never an error (forward/backward read
  compatibility).
