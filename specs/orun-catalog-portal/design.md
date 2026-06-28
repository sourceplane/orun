# orun-catalog-portal — Design

Status: In progress. Extends `specs/orun-service-catalog/data-model.md`; the
durable invariant (catalog content is git-derived) stays normative in
`orun-cloud/specs/components/18-state.md`.

## 1. What exists today

`internal/objcatalog` already reads a rich component view
(`CatalogComponentView`): `Name`, `Namespace`, `Repo`, `Path`, `Type`,
`Domain`, `System`, `Owner`/`OwnerSource`, `Stage`, `Tier`, `Environments`,
`DependsOn`, `Relations`, plus the open `Metadata`, `Docs`, `Links`, `Spec`
blocks carried verbatim from the resolved component. `EntityView` carries the
derived non-Component entities with `Ownership`/`Lifecycle` projected out.

So `system` and `owner` already flow. What the portal design adds end to end is
two human-facing fields and the tags list — all of which a component already
declares in its source and which `objcatalog` already has in hand via the
spec/metadata blocks; they are simply not projected as first-class fields.

## 2. The field convention

| Field | Source (in priority order) | Projected as |
|---|---|---|
| `description` | `spec.description` → `metadata.description` → `docs.summary` | `CatalogComponentView.Description` (new) + `EntityView` projection |
| `language` | `spec.language` → `metadata.language` → first of `spec.languages[]` | `CatalogComponentView.Language` (new) |
| `tags` | `spec.tags[]` → `metadata.tags[]` | `CatalogComponentView.Tags` (new, `[]string`) |

Reading is tolerant: each falls back through its chain and defaults to empty
(`""` / `nil`) when absent. No schema bump is required to *read* them (they ride
the existing open blocks); the change is to *project* them as named fields so the
snapshot envelope and downstream consumers do not have to re-parse `Spec`.

`system` keeps its existing `CatalogComponentView.System` source; this spec only
guarantees it is included in the projected/pushed envelope (it is part of the
identity the design groups by).

## 3. Projection

In `objcatalog.go`:

- Add `Description string`, `Language string`, `Tags []string` to
  `CatalogComponentView`, populated in the component reshape alongside the
  existing `Stage`/`Owner`/`System` projection, using the §2 fallback chains
  (a small `stringField` / `stringSliceField` helper over `Spec`/`Metadata`/
  `Docs`).
- Project the same onto the non-Component `EntityView` where the source carries
  them (entities derived from a spec with a `description`), so resources/APIs in
  the drawer can show a description too.
- Keep everything additive: existing fields and ordering are untouched; a
  component that declares none of the new fields yields empty values and changes
  no existing test's golden output.

## 4. Push wire (CPF1)

The catalog snapshot pushed by `orun catalog push` (OC4) serializes the catalog
views into the snapshot envelope. The new fields are included in that
serialization so the platform projection (`saas-orun-platform` OV6 →
`OrgCatalogEntity`) can map them to the additive optional contract fields
`description` / `system` / `language` / `tags`. The envelope change is additive;
a platform reading an older envelope simply sees the fields absent.

## 5. Testing

- `internal/objcatalog` unit tests: a component declaring `description` /
  `language` / `tags` in `spec`/`metadata` projects them; a component declaring
  none projects empty; the fallback precedence (`spec` over `metadata` over
  `docs`) is asserted.
- A golden/round-trip test that the snapshot envelope carries the fields (CPF1).
- No existing catalog golden changes for components that omit the fields
  (additive-only proof).
