# Orun Catalog Docs — CLI implementation plan (CD1/CD2)

Status: Draft (CD0 in review). The normative model is orun-cloud
`specs/epics/saas-catalog-docs/model.md`; this plan maps it onto the real code
sites in this repo. No coordination with the platform is required for landing
order: pages ride the existing `blob` kind, so snapshots carrying pages are
inert on an older orun-cloud until its CD3 projection lands.

---

## CD1 — The doc set: `docs.pages`, universal walk, provenance, caps

### 1. Structs + parsing

- `internal/catalogmodel/entity_envelope.go`: add
  `DocPage{Path, Key, Title, Role string}` and `Pages []DocPage` to
  `EntityDocs` (and the `ComponentDocs` twin), `json:"pages,omitempty"`.
- `internal/catalogmodel/component_yaml.go` + `internal/catalogresolve/intent.go`
  (`:49`, the repo-block docs struct): parse the YAML shape — string-or-object
  page entries are **not** supported; a page is always a mapping with `path`
  required (keeps the surface unambiguous; `docs.overview` remains the only
  scalar convenience).
- Unknown-field validation (`internal/catalogresolve/unknownfields*.go`) learns
  the new keys.

### 2. Validation (resolve time, per entity)

New checks in `internal/catalogresolve/validate.go`:

- `key`/`role` slugs (`[a-z0-9][a-z0-9-]*`, key ≤ 64 chars); key `overview`
  reserved → error.
- Key uniqueness after defaulting (filename stem) — a collision between two
  defaulted stems is an error naming both paths.
- ≤ 24 pages per entity → error (declared shape, not a byte cap — authors fix
  the manifest).
- Roles outside the well-known set (`guide|architecture|runbook|adr|reference|
  changelog|faq|onboarding`) are **allowed** (no error, no warning — free
  taxonomy like `kind`).

### 3. Title defaulting

At attach time, when `title` is empty: first ATX `# H1` line of the doc bytes
(trimmed, ≤ 120 chars), else the filename stem title-cased. Recorded on the
wire so the platform never parses markdown to build a list row.

### 4. The universal walk (closes review F1)

- `internal/catalogresolve/resolve_full.go` / `types.go`: generalize the
  repo-only `Overview{Content,SHA}` fields into a per-entity resolved-doc list
  `[]ResolvedDoc{Key, Title, Role, Path, Commit, Bytes}` populated for **every**
  entity that declares docs (components via the assemble path, the repo block,
  CD2 enrichments).
- `internal/objplan/catalog.go`: replace the `repoEntity()`-only
  `PendingDocs` population (`:112-124`) with a shared
  `attachDocs(e *nodes.Entity, docs []ResolvedDoc)` used by the component
  mapping (today's `docsBlock()` at `:349` keeps emitting the legacy pointer
  fields; the attached refs are written per the wire shape) and the repo path.
- `internal/nodes/assemble.go` (`:451-477`): **unchanged** — it already walks
  arbitrary keys, dedups blob entries, and stamps digests. Verify the doc-key →
  `Docs` mapping for `pages` writes into the `pages[]` array entries (keyed by
  `key`), not a flat map — this is the one real change in assembly: today it
  assumes `Docs[key]` is a top-level map entry; pages live in an array. Extend
  the stamp step to locate the page by key.

### 5. Provenance + pinned-commit read (closes review F3/F4)

- Resolve the head commit once per plan (the same commit already recorded on
  the snapshot / `advanceCatalogHeadRequest.commit`).
- Read doc bytes via the git object at that commit (`git cat-file blob
  <commit>:<path>` equivalent through the existing git plumbing used by the
  autopush gate), not `os.ReadFile` (`resolve_full.go:188` today).
- Refusal path: path dirty/untracked at that commit, or no usable git state →
  emit the entry **without** `digest`/`commit` bytes attached, log
  `docs: skipped <path> (<reason>)` once per path. Never fail the plan.
- Fast path: when the autopush clean-tree gate has already proven
  working-tree == HEAD, the working-tree read is permitted (identical bytes by
  construction).
- Stamp `commit` on every attached ref; keep emitting the deprecated content
  `sha` on `overview` only (wire compat with WO4's projector).

### 6. Caps (closes WO Q4)

Enforced in the walk, logged never silent: per-doc 256 KiB (skip doc),
8 MiB doc budget per closure (stop attaching further docs, log the cutoff).
Constants in one place with the rationale comment; the ≤ 24 pages check is
validation (§2).

### 7. CLI read surface

`cmd/orun/catalog_docs*.go` (WO3.1's command): no-args prints the shelf
(key · title · role · path · `attached@<short-commit>` or `declared-only
(<reason>)`); `--key <k>` prints one body. Digest parity with the snapshot
stays the acceptance check.

**CD1 done when:** a repo where a component declares two pages and the repo
block declares one produces entity JSONs carrying
`pages[].{key,title,role,path,commit,digest,size}` + `overview.commit`;
unchanged re-push uploads zero doc bytes; a dirty page path and an over-cap doc
each attach nothing and log why; `orun catalog list`/`describe`/`docs`
round-trip the set; all existing WO3 tests stay green (wire compat).

---

## CD2 — Enrichment: `catalog.entities` (closes review F6)

1. **Parse**: `internal/model/intent.go` + `internal/catalogresolve/intent.go`
   — `catalog.entities` map keyed `<kind>/<name>` (lowercase kind), values
   `{description, owner, links, tags, docs}` (docs = the full CD1 struct).
2. **Validate**: allowed kinds `system|domain|group|environment`; an
   enrichment for a declared kind (`component|repo|api|resource|…`) is an
   **error** (one declaration site per entity); malformed keys error with the
   expected shape.
3. **Merge** (in `resolve_full.go`, after derivation): for each derived entity
   with a matching enrichment — fill-empty `description`/`owner`/`links`/`tags`,
   set the docs block, queue its `ResolvedDoc`s through the CD1 walk. An
   enrichment whose target never materializes logs a **warning**
   (`catalog.entities: domain/payments enriched but no component references it`)
   and produces nothing.
4. **Tests**: enriched Domain carries description + docs; orphaned enrichment
   warns, creates nothing; `component/*` enrichment rejected; enriched docs
   respect CD1 caps/provenance.

**CD2 done when:** the ogpic-shaped example in the README (§3c) resolves to a
`Domain` entity whose JSON carries the enriched metadata + attached docs, and
the org projection on a CD3 platform renders it — with the derived-model
invariant (no phantom entities) verified by test.

---

## Sequencing & compat

- CD1 lands first (pure additive wire growth); CD2 is independent after CD1.
- **Wire compat contract**: `overview` keeps `{path, sha, digest}` + new
  `commit`; WO4's projector (`docRefOf`) reads `docs.overview` only and is
  unaffected by `pages` until orun-cloud CD3.
- Version note for adopters: ogpic (pins orun via kiox) adopts `docs.pages` +
  one `domain` enrichment on the first release carrying CD1/CD2 — its
  pre-authored doc files land ahead of time so adoption is a manifest-only
  diff (see ogpic `specs/epics/catalog-docs-adoption/`).
