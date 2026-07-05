# Orun Catalog Docs — CLI half

> **Cross-repo spec.** The platform epic and the **normative shared model** live
> in **`sourceplane/orun-cloud`** (`specs/epics/saas-catalog-docs/`, model in
> `model.md`). This file is the **`sourceplane/orun` (CLI/engine, Go)** half:
> the `docs.pages` intent surface, the universal doc walk, commit provenance +
> the pinned-commit read, the `catalog.entities` enrichment block, and the CLI
> read surface. Successor to `specs/orun-workspace-overview/` (WO3), chartered
> by orun-cloud's post-ship review
> (`specs/epics/saas-workspace-overview/review-2026-07-05.md` there).

| | |
|---|---|
| **Status** | Draft — design (CD0) in review; no code landed |
| **Cluster** | **CD** (catalog docs). CLI milestones: **CD1** (doc set + universal walk + provenance + caps), **CD2** (enrichment) |
| **Repos** | `sourceplane/orun` (CLI, Go — this half) · `sourceplane/orun-cloud` (platform, TS) · `sourceplane/ogpic` (reference adopter) |
| **Target branch** | `claude/overview-epic-architecture-7d28im` |
| **Pairs with** | orun-cloud CD3–CD5 (doc index projection + Docs surfaces), `specs/orun-workspace-overview/` (the WO3 spine this generalizes), `specs/orun-service-catalog/` (the entity model) |
| **Normative model** | orun-cloud `specs/epics/saas-catalog-docs/model.md` |

## 1. Problem

WO3 taught the CLI to carry **one** repo-authored document: the `Repo` entity's
`docs.overview`, read (best-effort, working-tree) at plan time and walked into
the catalog closure as a content-addressed blob. The review of the shipped epic
found the aperture far narrower than the model it ratified:

1. **Only `Repo` ships bytes.** `EntityDocs.Overview` exists on every kind
   (`internal/catalogmodel/entity_envelope.go:138`), but `PendingDocs` is
   populated solely in `repoEntity()` (`internal/objplan/catalog.go:112-124`);
   components emit bare path strings (`docsBlock()`, same file `:349-365`).
   A component/System/Domain overview can be declared but never rendered.
2. **One doc per entity.** No surface for a runbook, an architecture doc, an
   ADR set — the platform's Docs tab fabricates them from catalog fields
   instead, which is the invariant leak the successor epic exists to close.
3. **Provenance can't be honest.** The emitted ref is `{path, sha, digest}`
   where `sha` is the *content* sha256 (`internal/catalogresolve/resolve_full.go:188-191`)
   — no commit is recorded, so "From `<repo>@<sha>`" has nothing true to say.
4. **The pinned-commit read (WO model §3a) was never implemented** — the read
   is working-tree, "best-effort", with no dirty-path refusal. Only the
   autopush clean-tree gate keeps provenance honest today.
5. **Derived kinds have no declaration site.** `System`/`Domain` are derived
   from component spec strings and can carry no description, owner, or docs.

## 2. Current reality (cited)

- **The attach seam is already generic and needs no change**:
  `nodes.Entity.PendingDocs map[string][]byte` (`internal/nodes/model.go:141-146`);
  `assembleEntities` writes every pending key as a content-addressed blob,
  stamps `Docs[key].digest`, dedups closure entries, sorts for determinism
  (`internal/nodes/assemble.go:451-477`). CD1 populates this seam everywhere;
  it does not rebuild it.
- **Docs structs**: `EntityDocs{Overview, TechDocs, Runbooks, ADRs}` shared by
  every kind (`entity_envelope.go:137-142`); YAML twin in
  `internal/catalogresolve/intent.go:49` and the component-yaml path.
- **The Repo walk**: `resolveRepoBlock` reads overview bytes from the working
  tree (`resolve_full.go:183-191`), `repoEntity` maps them onto
  `PendingDocs["overview"]` (`objplan/catalog.go:112-124`).
- **Read surface (WO3.1, shipped)**: `catalog describe` is kind-aware;
  `orun catalog docs <entity>` prints the overview bytes with digest parity to
  the snapshot's `doc_ref`.
- **Push spine unchanged**: `objremote.Sync` set-difference syncs the closure;
  doc blobs ride `Orun-Object-Kind: blob` (legal server-side since orun-cloud
  migration `250`). Nothing in this epic touches the wire.

## 3. What CD1/CD2 add (summary — the model is normative in orun-cloud)

### 3a. The doc set (CD1)

```yaml
# any docs block — component.yaml spec.docs, intent.yaml repo.docs, enrichment
docs:
  overview: docs/overview.md          # reserved front page (unchanged)
  pages:
    - { path: docs/architecture.md, role: architecture }        # key/title defaulted
    - { path: ops/runbook.md, key: runbook, title: On-call runbook, role: runbook }
```

- `DocPage{Path, Key, Title, Role}` + `Pages []DocPage` on the shared structs.
  Validation: slugs, reserved `overview`, unique keys (including defaulted
  stems), ≤ 24 pages/entity. Roles: well-known set `guide|architecture|runbook|
  adr|reference|changelog|faq|onboarding`, unknown slugs allowed.
- **Universal walk**: every emitted entity with docs populates `PendingDocs`
  (component path + repo path + enriched entities). Legacy
  `techdocs`/`runbooks`/`adrs` stay bodyless pointers (wire-compatible).
- **Wire shape** per entity (`model.md §2b`): `overview` gains `commit`; each
  page emits `{key, title, role, path, commit, digest, size}`; a page whose
  bytes can't attach emits **without `digest`** + a logged reason.
- **Caps**: 256 KiB/doc · 24 pages/entity · 8 MiB doc budget/closure; over-cap
  ⇒ skip + warn; a doc problem never fails a plan.

### 3b. Provenance + the pinned-commit read (CD1)

At attach time the resolver records the commit the head will be advanced at on
every attached ref, and reads bytes **from the git object at that commit** —
falling back to refusal (path-pointer only + logged warning) when the path is
dirty/untracked or the repo state can't be established. The autopush
clean-tree fast path may keep reading the working tree (provably equal there).
This makes WO §3a true instead of incidental, and closes review F3/F4.

### 3c. Enrichment for derived kinds (CD2)

```yaml
# intent.yaml
catalog:
  entities:
    domain/identity:
      description: Sign-in, sessions, and workforce identity.
      owner: group:platform
      docs: { overview: docs/domains/identity.md }
    system/billing:
      docs: { overview: docs/systems/billing.md }
```

Merged onto entities the resolve already derives (`system|domain|group|
environment` at v1): fill-empty metadata, own the docs block. **Enrich, never
create** — a target that doesn't materialize is a validation warning, and an
enrichment for a declared kind (`component/*`, `repo/*`, …) is an error (one
declaration site per entity). Enriched docs walk the same CD1 pipeline.

### 3d. CLI read surface (CD1)

`orun catalog docs <entity>` grows from "print the overview" to the shelf:
list the doc set (key · title · role · path · attached/declared-only), with
`--key <k>` printing one body — digest parity with the snapshot remains the
acceptance check.

## 4. What deliberately does NOT change

- **No new wire call, no new object kind** — pages ride the existing closure
  and set-difference sync; unchanged docs never re-upload.
- **No graph/relations work** — docs are leaf content on existing entities;
  `buildGraphs()` is untouched (unlike WO3's `Repo` kind, there is no new kind
  here; enrichment decorates derived entities that already exist).
- **Legacy pointers** (`techdocs`/`runbooks`/`adrs`) — parsed and emitted as
  today; fold-in is an explicit later decision (orun-cloud risks Q2).
- **`Product`** — still WO6; the doc set applies to it unchanged when it lands.

## 5. Ownership split (keep in sync with the orun-cloud epic)

| Milestone | Repo | What |
|-----------|------|------|
| CD0 | all | design lock (this spec + the orun-cloud epic + ogpic adoption note) |
| **CD1** | **orun** | doc set structs + validation · universal `PendingDocs` walk · commit provenance + pinned-commit read/refusal · caps · `catalog docs` shelf |
| **CD2** | **orun** | `catalog.entities` enrichment (parse · merge · enrich-never-create validation) |
| CD3–CD6 | orun-cloud | `state.catalog_docs` projection · list endpoint · real entity Docs tab · Docs hub/reader · sibling links |
| adoption | ogpic | `docs.pages` + a `domain` enrichment on the first orun release carrying CD1/CD2 |

Read next: `implementation-plan.md` (this dir) for the CD1/CD2 step-by-step
against the real code sites.
