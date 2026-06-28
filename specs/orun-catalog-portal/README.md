# Spec: orun-catalog-portal

**Carry the git-authored fields a world-class catalog portal renders —
`description`, `system`, and `language`/`tags` — from the component source
through `orun catalog push` into the snapshot the platform projects.** This is
the CLI/object-model half of the catalog-portal experience; the console half is
[`orun-cloud/specs/epics/saas-catalog-portal/`](../../../orun-cloud/specs/epics/saas-catalog-portal/)
(cluster **CP**).

The design these fields feed:
`orun-cloud/specs/epics/saas-catalog-portal/design/Service_Catalog.dc.html`.

## Status

| Field | Value |
|-------|-------|
| Status | **✅ Shipped** — CPF0 merged (PR #422): `description`/`language`/`tags` project from the component source through `objcatalog`. CPF1 (push wire) is satisfied by the existing snapshot format — the component manifest blob already carries the `spec`/`metadata`/`docs` blocks these fields read from, so the pushed snapshot carries them and the platform projection reads them directly (orun-cloud CP4, PR #194). |
| Cluster | **CPF** (catalog portal fields) |
| Target branch | `claude/orun-cloud-catalog-2ailwz` |
| Builds on | `internal/objcatalog` (the catalog read views — already carries `System`, `Owner`, `Domain`, `Stage`, `Tier`, `Metadata`, `Docs`, `Links`, `Relations`), `specs/orun-service-catalog/` (the catalog data-model + push), `specs/orun-cloud/` (OC4 catalog push / heads — the snapshot envelope this ships in) |
| Pairs with | `orun-cloud/specs/epics/saas-catalog-portal/` (**CP**) — consumes these fields as additive optional fields on `OrgCatalogEntity` |
| Decisions locked | (1) **Git stays the source of truth** — these are *git-authored snapshot fields*, surfaced by extending what the snapshot projects, never authored downstream (`18-state` invariant). (2) **Additive, tolerant** — older catalogs without the fields project them as absent; the console degrades. (3) **No new top-level model** — `description`/`language`/`tags` ride the component spec/metadata that `objcatalog` already reads. |

## Thesis

The catalog read-model already carries the structural spine the portal needs —
kind, name, owner, system/domain, lifecycle stage, typed relations. The design
adds three human-facing, git-authored fields it does not yet surface end to end:

- **`description`** — the one-line "what is this" shown on every drawer and used
  by the readiness "Docs published" check.
- **`system`** — already modeled (`CatalogComponentView.System`); this spec
  ensures it reaches the platform projection and the `OrgCatalogEntity` row.
- **`language` / `tags`** — the implementation language chip + free tags; carried
  via the component spec/metadata `objcatalog` already decodes.

None of these are new authoring surfaces. They are fields a component already
declares in its source (`component.yaml`/`catalog-info`-style), that we make
sure survive resolution → snapshot → push → projection, so the console can show
them. Where a component omits them, they project as empty and the UI degrades.

## Read order

1. `README.md` (this file).
2. `design.md` — the field convention, where each is read from `objcatalog`, and
   the snapshot envelope/projection contract.
3. `implementation-plan.md` — CPF0–CPF1 with "done when".

## Milestones at a glance

| ID | Milestone | Status |
|----|-----------|--------|
| CPF0 | **Field convention + projection** — define how `description` / `language` / `tags` are read from the component spec/metadata in `internal/objcatalog`, project them onto the catalog views (alongside the existing `System`/`Owner`), with tolerant defaults for older catalogs. | ✅ (PR #422) |
| CPF1 | **Push wire** — the fields ride the component manifest blob's existing `spec`/`metadata`/`docs` blocks already serialized into the pushed snapshot, so no envelope change was needed; the platform projection (orun-cloud CP4) reads them directly with the same precedence. | ✅ (satisfied by existing snapshot format) |

## Scope boundary

| In scope | Out of scope |
|----------|--------------|
| Reading/projecting `description` · `language` · `tags` from the component source through `objcatalog` views; carrying them in the pushed snapshot envelope; tolerant handling of catalogs that omit them | The platform-side projection and `OrgCatalogEntity` mapping (→ `saas-catalog-portal` CP4 / `saas-orun-platform`); runtime signals (health/SLO/incidents/deploys — not git facts); scorecards/insights (computed downstream); any new authoring command |
