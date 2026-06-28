# orun-catalog-portal — Implementation plan

CPF0–CPF1 on `claude/orun-cloud-catalog-2ailwz`. Verification gate:
`go build ./...` + `go test ./internal/objcatalog/...` (and the catalog push
tests for CPF1) green.

## CPF0 — Field convention + projection

- Add `Description string`, `Language string`, `Tags []string` to
  `CatalogComponentView` (`internal/objcatalog/objcatalog.go`).
- Populate them in the component reshape using the fallback chains in
  `design.md §2`, via small tolerant `stringField` / `stringSliceField` helpers
  over the `Spec` / `Metadata` / `Docs` open blocks.
- Project `description` onto `EntityView` where the source declares it.
- Unit tests in `internal/objcatalog`: precedence, presence, and absent-default
  cases; assert no golden change for components that omit the fields.

**Done when:** the new fields project from a component's `spec`/`metadata`/`docs`
with the documented precedence, default empty when absent, and the existing
objcatalog tests still pass.

## CPF1 — Push wire

- Ensure `description` / `system` / `language` / `tags` are serialized into the
  catalog snapshot envelope shipped by `orun catalog push` (pairs `orun-cloud`
  OC4).
- Round-trip test: a resolved catalog with the fields → snapshot → parsed
  envelope retains them; an older envelope without them parses with the fields
  absent (additive-only).

**Done when:** the pushed snapshot carries the fields and the round-trip test
passes; the platform side (`saas-catalog-portal` CP4) can map them to
`OrgCatalogEntity`.

## Notes

- This spec only *reads and carries* git-authored fields; it authors nothing and
  introduces no new command. The platform projection and the console rendering
  are the paired `saas-catalog-portal` work.
