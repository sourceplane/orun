# Gluon Composition Packaging and Resolution Design

## Executive Summary

The current Gluon composition model is easy to understand but it will not scale well for multi-team reuse, version pinning, remote distribution, or reproducible planning:

- compositions are discovered from an ambient `--config-dir`
- the contract is folder-shaped (`<type>/schema.yaml` + `<type>/job.yaml`)
- intent does not declare which composition sources it depends on
- registry distribution is possible only indirectly by shipping the whole provider or copying folders around

The recommended direction is:

1. Keep `components[].type` as the stable logical contract name.
2. Add a top-level `compositions:` section to `intent.yaml` that declares composition sources and resolution policy.
3. Introduce first-class `kind: Composition` and `kind: CompositionPackage` documents.
4. Package composition sets as a tar+gzip archive for local use and as an OCI artifact for registry distribution.
5. Resolve sources into a local cache, record the resolved digests in a lock file and in the generated plan, and keep `--config-dir` as a backward-compatible fallback.

This gives Gluon a Helm/Crossplane-style packaging story without forcing each component to carry a raw artifact URL.

## Why Not Put A Raw `compositionRef` On Every Component?

That looks attractive at first, but it becomes noisy and hard to manage:

- many components of the same `type` would repeat the same package URI
- changing a package version would require many edits
- validation becomes dependent on per-component fetch behavior
- caching and conflict resolution get harder
- it pushes distribution concerns into every component declaration

The better default is:

- `component.type` answers: "what contract does this component need?"
- `intent.compositions` answers: "where do the contracts come from?"

Per-component `compositionRef` should exist only as an escape hatch for experiments or one-off overrides.

## Goals

- Let `intent.yaml` declare the composition sources required to compile a plan.
- Support local directory, local archive, and OCI artifact sources.
- Replace magic folder conventions with self-describing documents.
- Keep plan generation deterministic and cacheable.
- Support registry-native distribution using OCI-friendly packaging.
- Preserve backward compatibility with existing folder-based `--config-dir` usage.
- Make room for package metadata, version constraints, signatures, SBOMs, and provenance later.

## Non-Goals

- Registry-wide "search by type" as an implicit resolver. That is not deterministic enough for planning.
- Arbitrary HTTP download support in v1 of this design. `oci://`, local paths, and optionally `file://` are enough.
- Replacing component `type` with a fully qualified package URI.
- Solving runtime execution packaging in the same change.

## Architectural Decision

### Decision 1: Add top-level composition source declaration to intent

Add a new `compositions:` block to `intent.yaml`.

Recommended shape:

```yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: microservices-deployment

compositions:
  sources:
    - name: core
      kind: oci
      ref: oci://ghcr.io/sourceplane/gluon-compositions/platform-core:v1.2.0
    - name: team-overrides
      kind: archive
      path: ./dist/platform-overrides-0.3.0.tgz
  resolution:
    precedence:
      - team-overrides
      - core
    bindings:
      helm: core
      helmCommon: core
      terraform: core
      gha-helm: team-overrides
```

This keeps intent declarative and portable without duplicating source references across components.

### Decision 2: Keep `components[].type`

Do not replace `type` with a package URI.

`type` should remain the stable logical name used by:

- schema validation
- planner binding
- documentation
- CLI inspection
- migration from legacy folder-based compositions

### Decision 3: Support optional per-component override

Add an optional override only for special cases:

```yaml
components:
  - name: web-app
    type: helm
    compositionRef:
      source: team-overrides
      name: helm
```

Rules:

- `source` refers to an entry under `intent.compositions.sources`
- `name` defaults to `type` if omitted
- use this sparingly

### Decision 4: Move from folder contract to document contract

Introduce two first-class documents:

- `kind: Composition`
- `kind: CompositionPackage`

This replaces "discover a folder with `schema.yaml` and `job.yaml`" with "load YAML documents that declare what they are".

## Proposed Data Model

### Intent additions

Suggested new fields in `internal/model/intent.go`:

```go
type Intent struct {
    APIVersion   string                 `yaml:"apiVersion" json:"apiVersion"`
    Kind         string                 `yaml:"kind" json:"kind"`
    Metadata     Metadata               `yaml:"metadata" json:"metadata"`
    Discovery    Discovery              `yaml:"discovery" json:"discovery"`
    Compositions CompositionConfig      `yaml:"compositions" json:"compositions"`
    Groups       map[string]Group       `yaml:"groups" json:"groups"`
    Environments map[string]Environment `yaml:"environments" json:"environments"`
    Components   []Component            `yaml:"components" json:"components"`
}

type CompositionConfig struct {
    Sources    []CompositionSource        `yaml:"sources" json:"sources"`
    Resolution CompositionResolution      `yaml:"resolution" json:"resolution"`
}

type CompositionSource struct {
    Name       string            `yaml:"name" json:"name"`
    Kind       string            `yaml:"kind" json:"kind"` // dir, archive, oci
    Path       string            `yaml:"path,omitempty" json:"path,omitempty"`
    Ref        string            `yaml:"ref,omitempty" json:"ref,omitempty"`
    Digest     string            `yaml:"digest,omitempty" json:"digest,omitempty"`
    PullPolicy string            `yaml:"pullPolicy,omitempty" json:"pullPolicy,omitempty"`
    Verify     *VerifyPolicy     `yaml:"verify,omitempty" json:"verify,omitempty"`
    Metadata   map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

type CompositionResolution struct {
    Precedence []string                    `yaml:"precedence,omitempty" json:"precedence,omitempty"`
    Bindings   map[string]string           `yaml:"bindings,omitempty" json:"bindings,omitempty"` // type -> source name
}

type Component struct {
    Name          string                 `yaml:"name" json:"name"`
    Type          string                 `yaml:"type" json:"type"`
    CompositionRef *ComponentCompositionRef `yaml:"compositionRef,omitempty" json:"compositionRef,omitempty"`
    ...
}

type ComponentCompositionRef struct {
    Source string `yaml:"source,omitempty" json:"source,omitempty"`
    Name   string `yaml:"name,omitempty" json:"name,omitempty"`
}
```

### Composition document

A single composition should become self-contained:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: helm
  labels:
    sourceplane.io/category: deployment
spec:
  type: helm
  description: Deploy Helm-managed services
  defaultJob: deploy
  inputSchema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    properties:
      type:
        const: helm
      inputs:
        type: object
        properties:
          chart:
            type: string
  jobs:
    - name: deploy
      runsOn: ubuntu-22.04
      timeout: 15m
      retries: 2
      steps:
        - name: deploy
          run: helm upgrade --install {{.Component}} {{.chart}}
    - name: rollback
      runsOn: ubuntu-22.04
      timeout: 10m
      retries: 1
      steps:
        - name: rollback
          run: helm rollback {{.Component}}
```

Notes:

- `metadata.name` and `spec.type` should initially be required to match.
- `defaultJob` must be explicit. Do not keep "first job wins" semantics.
- `inputSchema` replaces the separate `schema.yaml`.
- `jobs` replaces the separate `job.yaml`.

### Composition package document

One package should be able to export many composition types:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: CompositionPackage
metadata:
  name: platform-core
spec:
  version: 1.2.0
  gluon:
    minVersion: ">=0.6.0"
  exports:
    - composition: helm
      path: compositions/helm.yaml
    - composition: terraform
      path: compositions/terraform.yaml
  dependencies:
    - name: base
      ref: oci://ghcr.io/sourceplane/gluon-compositions/base:v1.0.0
      optional: true
```

`exports` is important. It makes package contents discoverable without relying on folder naming.

## Package Layout

Recommended package root:

```text
platform-core/
├── gluon.yaml
├── compositions/
│   ├── helm.yaml
│   ├── helmCommon.yaml
│   └── terraform.yaml
├── examples/
│   └── intent.yaml
└── docs/
    └── README.md
```

Where:

- `gluon.yaml` is the `CompositionPackage` document
- `compositions/*.yaml` are `Composition` documents

This is intentionally similar to other CNCF package ecosystems:

- Helm packages a chart directory into a `.tgz`
- Crossplane packages a directory of YAML manifests into an OCI package

## Packaging Format

### Local package format

Use a tar+gzip archive for local/offline workflows.

Recommendations:

- the archive content must contain `gluon.yaml` at its root
- file extension can default to `.tgz`
- Gluon should not rely only on the extension; it should inspect the contents

### OCI package format

Use an OCI artifact stored with a standard OCI manifest so it works with ordinary registries and ORAS-compatible tooling.

Recommended OCI settings:

- manifest media type: standard OCI image manifest
- artifact type: `application/vnd.sourceplane.gluon.composition.package.v1`
- main layer media type: `application/vnd.sourceplane.gluon.composition.package.layer.v1.tar+gzip`
- config media type: `application/vnd.oci.empty.v1+json` or a small Gluon package config JSON

Why this is the right trade-off:

- OCI registries already store non-image artifacts
- Helm already ships chart tarballs through OCI registries
- OCI referrers let us attach signatures, SBOMs, provenance, or docs later
- the same package can be consumed from local archive or OCI without inventing two content models

## Discovery And Resolution Semantics

There are two different discovery problems. They should not be conflated.

### 1. Discovering package contents

This should happen from the package manifest (`gluon.yaml`) and the `exports` list, not from registry tag crawling and not from directory names.

### 2. Discovering which package to use for a component type

This should happen from explicit resolution rules in the intent, in this order:

1. `components[].compositionRef`
2. `intent.compositions.resolution.bindings[type]`
3. first matching export found in `resolution.precedence`
4. legacy `--config-dir` fallback

If multiple sources export the same composition type and no explicit rule resolves the conflict, Gluon should fail fast with a deterministic error.

## Caching, Locking, And Reproducibility

To keep planning deterministic, Gluon should resolve sources into a local cache and write a lock file.

### Cache

Recommended cache location:

```text
$HOME/.gluon/cache/compositions/<resolved-digest>/
```

Behavior:

- `dir` source: hash the normalized package contents and cache by content digest
- `archive` source: cache by archive digest
- `oci` source: resolve tag to digest, then cache by digest

### Lock file

Recommended lock file:

```text
.gluon/compositions.lock.yaml
```

Example:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: CompositionLock
sources:
  - name: core
    kind: oci
    ref: oci://ghcr.io/sourceplane/gluon-compositions/platform-core:v1.2.0
    resolvedDigest: sha256:abc123...
    exports:
      - helm
      - helmCommon
      - terraform
  - name: team-overrides
    kind: archive
    path: ./dist/platform-overrides-0.3.0.tgz
    resolvedDigest: sha256:def456...
    exports:
      - gha-helm
```

Rules:

- plans should include the resolved source digests used for compilation
- CI should prefer digest-pinned sources or a committed lock file
- tag-only OCI references are acceptable for authoring, but not ideal for repeatable CI without a lock

## Plan Metadata Changes

Extend the plan model so compiled output records where the compositions came from.

Suggested additions:

```go
type PlanSpec struct {
    JobBindings         map[string]string            `json:"jobBindings,omitempty" yaml:"jobBindings,omitempty"`
    CompositionSources  []ResolvedCompositionSource  `json:"compositionSources,omitempty" yaml:"compositionSources,omitempty"`
}

type ResolvedCompositionSource struct {
    Name           string   `json:"name" yaml:"name"`
    Kind           string   `json:"kind" yaml:"kind"`
    Ref            string   `json:"ref,omitempty" yaml:"ref,omitempty"`
    Path           string   `json:"path,omitempty" yaml:"path,omitempty"`
    ResolvedDigest string   `json:"resolvedDigest" yaml:"resolvedDigest"`
    Exports        []string `json:"exports,omitempty" yaml:"exports,omitempty"`
}
```

That materially improves auditability and debugging.

## Backward Compatibility

Backward compatibility is critical. The transition should not break current users.

### Legacy support rules

- `--config-dir` remains supported
- existing folder-based compositions keep working
- loader should be able to synthesize a `Composition` object from a legacy folder:
  - `schema.yaml` -> `spec.inputSchema`
  - `job.yaml` -> `spec.jobs`
  - folder name -> `metadata.name` and `spec.type`
  - first job -> temporary `defaultJob` only if the legacy format did not define one

### Deprecation strategy

Phase 1:

- support both legacy folders and packaged compositions
- keep `--config-dir`
- allow `intent.compositions` to coexist with `--config-dir`

Phase 2:

- document packaged compositions as the recommended path
- emit warnings when multiple legacy sources conflict

Phase 3:

- consider making `--config-dir` an advanced or legacy-only input

## Recommended Internal Package Boundaries

Do not pile all of this into `internal/loader`.

Recommended package split:

- `internal/composition/source`
  - parse `dir`, `archive`, `oci` source declarations
  - resolve local paths relative to the intent file
- `internal/composition/fetch`
  - open directory sources
  - unpack archive sources
  - pull OCI sources
  - manage cache
- `internal/composition/package`
  - load `CompositionPackage`
  - load exported `Composition` documents
  - validate package structure
  - adapt legacy folder format into the same internal representation
- `internal/composition/resolve`
  - precedence rules
  - binding rules
  - lock file support
- `internal/loader`
  - continue to own intent/component loading
  - consume resolved composition packages instead of raw folder trees

This separation matters because package fetching, package loading, and compiler loading are different concerns.

## CLI Changes

Recommended new CLI capabilities:

- `gluon compositions pull --intent intent.yaml`
- `gluon compositions lock --intent intent.yaml`
- `gluon compositions package build --root ./platform-core --output ./dist/platform-core-1.2.0.tgz`
- `gluon compositions package push ./dist/platform-core-1.2.0.tgz oci://ghcr.io/sourceplane/gluon-compositions`

The existing inspection command should also evolve:

- `gluon compositions --intent intent.yaml`
  - lists resolved compositions from declared sources
- `gluon compositions helm --intent intent.yaml`
  - shows the resolved source, digest, jobs, and schema info

`--config-dir` can remain available as an override and fallback during migration.

## Validation Rules

Add explicit validation around the new model:

- every `CompositionSource.name` must be unique
- `kind=dir` requires `path`
- `kind=archive` requires `path`
- `kind=oci` requires `ref`
- `bindings` may only refer to declared source names
- exported `composition` names must be unique within a package
- duplicate type exports across packages require explicit resolution
- `Composition.spec.defaultJob` must exist in `spec.jobs`
- `Component.compositionRef.source` must refer to a declared source

## Security And Supply Chain

The package model should be designed so Gluon can adopt stronger verification without redesigning the format later.

Recommended near-term behavior:

- support digest-pinned OCI references
- record digests in lock files and plans
- keep fetches content-addressed in cache

Recommended future behavior:

- verify signatures on OCI packages
- consume SBOM or provenance artifacts attached as OCI referrers
- add a strict mode that rejects unsigned or unpinned package sources

## Alternatives Considered

### Alternative A: only add `components[].compositionRef`

Rejected as the default architecture.

It works for small demos, but it does not scale for shared composition sets, reuse, or bulk upgrades.

### Alternative B: keep folder format and just allow `--config-dir` to point at extracted OCI content

Too weak.

It improves transport, but not the contract model, metadata, discoverability, or determinism.

### Alternative C: replace `type` with fully qualified names such as `ghcr.io/org/pkg/helm`

Rejected.

It makes component declarations noisy, couples authoring to distribution, and weakens the clean separation between logical type and source location.

## Phased Implementation Plan

### Phase 0: Design and schemas

- add this design document
- define `Composition`, `CompositionPackage`, and `CompositionLock` schemas
- extend `intent.schema.yaml` with the `compositions` section and optional `compositionRef`

### Phase 1: Internal representation

- add new model structs
- create a common internal `ResolvedComposition` representation
- update planner inputs so `defaultJob` is explicit

### Phase 2: Legacy adapter

- refactor current folder loading into an adapter that returns the new internal representation
- keep all existing tests passing

### Phase 3: Local packages

- load package directories with `gluon.yaml`
- load tar+gzip archives
- resolve and cache local sources

### Phase 4: OCI packages

- add OCI pull support
- resolve tag to digest
- store extracted content in the composition cache
- write the lock file

### Phase 5: CLI and plan visibility

- add lock/pull/build/push commands
- add resolved source data to `plan`
- update `gluon compositions` output to show source and digest

### Phase 6: Docs and migration

- document `intent.compositions`
- add example packaged composition repositories
- add migration docs from `assets/config/compositions/*`

## Copilot Task Breakdown

If this work is delegated, split it into these implementation tracks:

1. Schema and model track
   - update `internal/model/intent.go`
   - add new model types for packages and locks
   - extend `assets/config/schemas/intent.schema.yaml`
   - add new schemas for `Composition` and `CompositionPackage`

2. Resolution and cache track
   - implement source parsing and precedence rules
   - add local cache management
   - add lock file read/write support

3. Loader compatibility track
   - introduce package loading
   - implement legacy folder adapter
   - unify both into one `ResolvedComposition` model

4. OCI transport track
   - implement OCI pull and digest resolution
   - unpack package layers into the local cache

5. Planner and plan output track
   - require explicit `defaultJob`
   - add resolved composition source metadata to the rendered plan

6. CLI and docs track
   - update `gluon compositions`
   - add package build/pull/push/lock commands
   - update website docs and examples

## Final Recommendation

The best scalable design is not "put an OCI or local composition reference directly on every component".

The best scalable design is:

- keep `type` as the component contract name
- declare composition sources once at the top of `intent.yaml`
- package many compositions together as a versioned composition package
- store those packages as tarballs locally and OCI artifacts remotely
- resolve them into a lockable, digest-addressed cache
- preserve `--config-dir` through a compatibility layer while the ecosystem migrates

That gives Gluon a clean authoring model, a strong packaging story, and a migration path that can be implemented incrementally.

## External References

- Helm OCI registries: https://helm.sh/docs/v3/topics/registries/
- OCI image manifest artifact guidance: https://github.com/opencontainers/image-spec/blob/main/manifest.md
- OCI distribution referrers behavior: https://github.com/opencontainers/distribution-spec/blob/main/spec.md
- ORAS referrer discovery: https://oras.land/docs/commands/oras_discover/
- Crossplane package model: https://docs.crossplane.io/master/cli/command-reference/
