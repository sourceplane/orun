---
title: Service catalog
description: The typed entity model derived from your declared sources — components, systems, APIs, owners, environments, and golden paths, with a unified relation graph and a live deployment plane.
---

The catalog is orun's answer to "what exists, who owns it, how is it
connected, and where is it running?" — derived entirely from the sources you
already declare. It is not a database you curate: it is a **projection** of
`component.yaml` files, the intent, `CODEOWNERS`, the composition lock, and
recorded execution history. If the sources change, the catalog changes; if
something is in the catalog, a source put it there.

```text
component.yaml · intent.yaml · CODEOWNERS · compositions.lock.yaml
                          │  resolve
                          ▼
        ┌──────────────────────────────────────┐
        │   catalog @ source snapshot          │
        │   entities/<Kind>/…   (orun.io/v1)   │
        │   relations.json      (typed edges)  │
        │   indexes             (impact, refs) │
        └──────────────────────────────────────┘
                          │  read
                          ▼
   orun catalog list · describe · tree · affected · cockpit
                          +
        live plane (deployments · executions),
        derived on read from execution history
```

This page covers the entity model. For the on-disk mechanics see the
[state model](state-model.md); for the commands see
[`orun catalog`](../cli/orun-catalog.md).

## Entities and kinds

The catalog is multi-kind. Components are the authored core, and around them
the resolver *derives* the rest of the organizational picture:

| Kind | What it represents | Derived from |
|---|---|---|
| `Component` | A deployable/operable unit | `component.yaml`, resolved and enriched |
| `System` | A set of components delivering a capability | `spec.system` declarations |
| `Domain` | A policy/ownership area | Component domains and groups in the intent |
| `Group` | An owning team | Ownership refs (`group:<name>`) |
| `API` | A provided/consumed interface | Component contracts and composition effects |
| `Resource` | Infrastructure a component relies on | Component declarations and composition effects |
| `Environment` | A runtime context as a catalog citizen | The intent's environment matrix |
| `Composition` | A golden path, versioned and lifecycle-staged | The composition lock |

```bash
orun catalog list                      # components (the default)
orun catalog list --kind System        # any other kind
orun catalog list --kind Environment
orun catalog describe api-edge-worker  # the full envelope for one entity
orun catalog tree api-edge-worker --direction both
```

Derived entities are not stubs. A System knows its members (`spec.members`,
surfaced as a MEMBERS count in list output); an Environment knows what is
deployed to it; a Composition knows which components ride its golden path.

Entity keys follow one grammar across kinds — `<namespace>/<repo>/<name>` for
component-like entities, `group:<name>` / `user:<name>` for owners — so a
relation edge always points at exactly one thing.

## The entity envelope

Every entity, authored or derived, resolves into the same `orun.io/v1`
envelope. `orun catalog describe` renders it in full; `--json` emits it
machine-readable:

| Section | Contents |
|---|---|
| `metadata` | Name, title, description, labels, tags |
| `ownership` | Owner (typed ref), system, domain — **with provenance** |
| `lifecycle` | Stage: `experimental` · `production` · `deprecated` · `retired` |
| `spec` | Kind-specific declaration (type, environments, parameters, members…) |
| `relations` | Typed edges to other entities |
| `contracts` | APIs provided and consumed |
| `integrations` | Join keys for external platforms (Datadog, PagerDuty, …) |
| `docs` / `links` | Documentation pointers and external URLs |
| `extensions` | Namespaced vendor blocks (`x-<vendor>`), preserved verbatim |
| `provenance` | Where each fact came from: manifest hash, resolver version, inference trail |

The envelope is the catalog's contract: portals, scripts, and the cockpit all
read the same shape regardless of kind.

## Ownership, with provenance

Every entity has an owner, and the catalog tells you **how it knows**:

1. An authored `spec.owner` always wins — `ownership.source: authored`.
2. Otherwise the resolver reads the workspace `CODEOWNERS` file and applies
   the last matching rule — `ownership.source: CODEOWNERS`.
3. Derived entities inherit ownership from their members —
   `ownership.source: inherited`.

This means ownership is never a second bookkeeping system. The file your code
review process already enforces is the file the catalog reads, and the
provenance field keeps the derivation honest.

## The relation graph

All connections in the catalog live in one typed graph (`relations.json`):
`dependsOn`, `partOf`, `ownedBy`, `providesApi`, `consumesApi`, `runsOn`,
`deployedTo`, `composedBy` — with inverse edges materialized on read.

One graph, two consumers:

- **Catalog reads** — `orun catalog tree` walks it; `describe` renders an
  entity's edges; portals query it.
- **Change detection** — the same engine behind `--changed` and
  [`orun catalog affected`](../cli/orun-catalog.md) consumes the same edges.

That convergence is the point: *"what depends on this component?"* and *"what
should this pull request run?"* are the same question asked by different
tools, so they get the same answer.

## Compositions in the catalog

Golden paths are entities too. A composition carries a semver `version` and a
`lifecycle` stage (`stable` · `beta` · `deprecated`), appears in the catalog
with `composes` relations to the components that ride it, and may declare
**effects** — what running it contributes:

```yaml
# composition.yaml (authored by the platform team)
spec:
  version: 2.3.0
  lifecycle: stable
  effects:
    integrations:
      cloudflare: { product: workers }
    provides:
      - default/orun/cloudflare-edge    # → Resource entity
    exposes:
      - default/orun/edge-http-api      # → API entity
    scorecards:
      satisfies: [has-deploy-pipeline]
```

A component that adopts this golden path gets its integrations, Resources, and
APIs in the catalog *for free* — declared once by the platform team, not
copy-pasted into every `component.yaml`. Pinning a deprecated composition
version surfaces a warning at resolve time, where it can actually change
behavior.

Effects are **authored intent**. What actually happened is the live plane's
job.

## The live plane

Deployments, latest executions, and health are **derived on read** from the
recorded execution history — the same objects `orun status` reads. They are
never persisted into catalog blobs, so they can never be stale relative to the
runs that produced them. `orun catalog describe` appends them to the envelope:

```text
Live deployments
  production    rev cat-2a2cd793 · succeeded · 2026-06-10T14:02Z
```

This is the catalog's desired/observed split: the envelope says what the
sources declare; the live plane says what the record shows.

## Catalog identity and snapshots

A catalog is content-addressed: resolving the same sources yields the same
catalog id, no matter which command did the resolving (`orun plan`, `orun
catalog refresh`, the cockpit's auto-refresh). Snapshots are immutable;
named refs (`catalogs/current`, `branches/<name>`, `prs/<n>`) select them, and
`orun catalog diff` compares any two. History costs almost nothing — identical
content is stored once.

## Adopting the v1 model

The richer authoring surface (`system`, `lifecycle`, `integrations`, `links`,
`docs`, `extensions`) is **opt-in and additive**. Three guarantees make
adoption safe:

- **Old catalogs read as new.** Snapshots written under the earlier
  `v1alpha1` model stay byte-identical on disk and up-convert to the v1
  envelope on read. No migration step, no flag.
- **`orun catalog migrate` is advisory.** It lints authored `component.yaml`
  files for v1 readiness — a legacy `apiVersion`, a missing owner, a missing
  `lifecycle` stage, a missing `system` — and prints per-file findings. It
  never rewrites a file.
- **Un-migrated components still resolve.** A bare-minimum manifest is a
  valid catalog entity; enrichment only adds.

```bash
orun catalog migrate          # what would make each component a first-class v1 entity?
orun catalog migrate --json   # the structured result, for CI
```

## Design notes

Why the catalog works the way it does:

- **Derived, not curated.** Catalogs maintained by hand drift the day they
  ship. This one is a pure function of sources + records, so it is exactly as
  current as your repository.
- **Same envelope, every kind.** One schema for consumers means a portal, a
  script, and the cockpit never special-case entity types.
- **Provenance everywhere.** Every derived fact says where it came from. A
  catalog you can't audit is a rumor.
- **Live state is a projection.** Persisting "what's deployed" would create a
  second source of truth; deriving it from sealed executions keeps one.

## See also

- [The resource model](../overview/resource-model.md) — how entities fit
  orun's wider declare/compile/record model.
- [State model](state-model.md) — where catalogs live on disk.
- [`orun catalog`](../cli/orun-catalog.md) — the full command reference.
- [Change detection](change-detection.md) — the other consumer of the
  relation graph.
