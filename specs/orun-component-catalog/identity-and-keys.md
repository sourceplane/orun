# Identity and Keys

This document fixes the **construction rules** for every catalog identifier.
Once shipped, these rules are part of the on-disk contract; changing them
forces a Phase 3 migration.

---

## 1. Vocabulary

| Term | Meaning | Format |
|------|---------|--------|
| `headRevision` | Git commit at workspace HEAD | 12-char short SHA (lowercase hex) |
| `treeHash` | `git rev-parse HEAD^{tree}` short | 7-char short SHA |
| `dirtyHash` | SHA-256 over catalog-relevant dirty files | `d` + 9 hex chars in keys, `sha256:<full>` in JSON |
| `catalogInputHash` | SHA-256 over canonical resolver inputs | `sha256:<full>` (JSON only) |
| `catalogHash` | SHA-256 over canonical resolved catalog | `sha256:<full>` (JSON), short 6–16 hex in keys |
| `manifestHash` | SHA-256 over canonical resolved manifest | `sha256:<full>` |

## 2. SourceSnapshot key

```text
src-<scope>[-<branch-or-pr>]-c<headShort>-t<treeShort>[-d<dirtyShort>]
```

| Scope | Examples |
|-------|----------|
| `branch-main` | `src-branch-main-cdef456a-t5ab21c3` |
| `branch-<name>` | `src-branch-feature-x-cabc1234-t9aa7710` |
| `pr<num>` | `src-pr139-cabc1234-t9aa7710` |
| `tag-<name>` | `src-tag-v0.18.0-cdef456a-t5ab21c3` |
| `local-dirty` | `src-branch-feature-x-cabc1234-t9aa7710-d91aa77b2` |
| `local-nogit` | `src-local-nogit-d91aa77b2` |
| `ci-event` | `src-ci-pr139-cabc1234-t9aa7710` (provider-injected) |

Rules:

1. Lowercase hex only in `c…`/`t…`/`d…` segments.
2. Branch names sanitized: `[^a-z0-9-]` → `-`; collapse runs of `-`; trim
   leading/trailing `-`. Max 40 chars; longer names truncated and suffixed
   with first 8 chars of `sha256(branch)`.
3. PR scope wins over branch scope when both apply (CI events).
4. `local-nogit` does not include `c…`/`t…`; only `d…`.
5. Total key length ≤ 128 characters; if violated, the trailing scope
   segment is hashed: `src-branch-<sha8>-c…-t…`.

## 3. CatalogSnapshot key

```text
cat-<catalogHashShort>
```

- `catalogHashShort` = first 6–16 hex chars of `catalogHash` (start at 8;
  expand on collision with existing catalog under the same `SourceSnapshot`).
- Collision policy: append `-x<n>` only if the same source already has a
  different catalog with the same prefix at every length up to 16.

## 4. Component key

```text
<namespace>/<repo>/<componentName>
```

- `namespace` defaults to `intent.yaml` `catalog.namespace` if unset on the
  component.
- `repo` is the workspace's Git repo short name (`<owner>/<repo>` collapsed
  to `<repo>` for component-key purposes; the full `<owner>/<repo>` is kept
  in `identity.repo`).
- `componentName` is `metadata.name` from `component.yaml`.

Validation:

- Each segment matches `^[a-z0-9._-]+$`. Uppercase characters are rejected
  (warn → error in `--strict`); the resolver does not silently lowercase.
- Component identity **never** includes environment.

Filesystem sanitization for index filenames:

```
sourceplane/orun/api-edge   →   sourceplane-orun-api-edge.json
```

Slashes become single dashes; the index file's `componentKey` field stores
the original slash form.

## 5. Revision and execution keys

Unchanged from `specs/orun-state-redesign/`:

```text
rev-<scope>-<headShort>-p<planHashShort>[-x<n>]
run-<NNN>      (numeric, zero-padded, scoped per revision)
gh-<run_id>-<attempt>-<sha>   (GitHub Actions ExecID, preserved verbatim)
```

Phase 2 only **adds** parent identifiers to revision/execution JSON; the keys
themselves are untouched.

## 6. ULID prefixes

Internal IDs (used in JSON, never in folder names):

| Object | Prefix | Source |
|--------|--------|--------|
| `SourceSnapshot.sourceSnapshotId` | `src_` | `oklog/ulid/v2` (Phase 1 dep) |
| `CatalogSnapshot.catalogSnapshotId` | `cat_` | same |
| `ComponentManifest.identity.componentId` | `cmp_` | same |

ULIDs are monotonic per-process via the Phase 1 generator pattern
(`internal/triggerctx/ids.go`).

## 7. Dirty hash inputs

`dirtyHash` is computed from a deterministic-ordered tar-stream of
catalog-relevant files only. **Do not hash the entire dirty workspace** — it
churns on every keystroke and breaks resolver caching.

Catalog-relevant files (when present and modified vs `treeHash`):

- `intent.yaml`
- every `component.yaml` discovered by the resolver
- composition / stack reference files (`stack.yaml`, `composition.yaml`)
- `package.json` files referenced by the inference layer
- `Dockerfile` files referenced by the inference layer
- `README.md` files for components when
  `intent.yaml` `catalog.inference.readme = true`
- Helm `Chart.yaml` and Terraform `*.tf` only when their respective
  inference flags are on

Each entry is:

```text
<repo-relative-posix-path>\0<sha256-of-file-bytes>\n
```

Sorted lexicographically by path, joined, and hashed once with SHA-256.
`dirtyShort` = first 9 hex chars.

## 8. `catalogInputHash`

Distinct from `dirtyHash` — even on a clean tree this hash gates resolver
caching. Inputs (in this canonical order):

1. `treeHash`
2. `dirtyHash` (empty string when `workingTree = clean`)
3. `resolver.orunVersion`
4. `resolver.resolverVersion`
5. `resolver.schemaVersion`
6. Sorted `resolver.stackSources`
7. Canonical JSON of `intent.yaml.catalog` (with empty defaults inlined)

Concatenate with `\n` separators, hash with SHA-256. Stored as
`sha256:<full>` in `SourceSnapshot.catalogInputHash`.

## 9. `catalogHash`

Inputs:

1. `catalogInputHash`
2. The canonical (sorted-keys, no-whitespace) JSON encoding of every
   `ComponentManifest`, ordered by `componentKey`. Each manifest is hashed
   independently to produce `manifestHash`; the catalog hash inputs are the
   list of `(componentKey, manifestHash)` pairs.
3. The canonical encoding of `CatalogGraph` (each graph file).
4. `resolver.resolverVersion`.

Stable across runs given the same inputs (verified by property test
**T-IDK-1** in `test-plan.md`).

## 10. `manifestHash`

Computed over the canonical encoding of `{identity, metadata, spec, runtime}`
of the resolved `ComponentManifest`. The `source.manifestHash` field is
**set after** computation; it is excluded from its own input.

Property: changing only `resolution.inheritedFrom` provenance must NOT
change `manifestHash`. Changing any resolved value MUST change it.

## 11. Collision and retry policy

| Object | Collision detection | Resolution |
|--------|---------------------|------------|
| `SourceSnapshot` | `CreateIfAbsent` returns `ErrExists` with byte-identical content | Treat as success (idempotent refresh). |
| `SourceSnapshot` | `ErrExists` with different content | Programming error; abort with `ErrConflict`. Same source key cannot map to two source bodies. |
| `CatalogSnapshot` | Same → idempotent. Different → expand `catalogHashShort` by 2 hex chars and retry up to length 16. | If still colliding at 16: append `-x<n>`. |
| `ComponentManifest` | Same name twice in one catalog | Resolver error before persistence; never reach the writer. |
| `ComponentHistoryEvent` | Same `<seq>` | Counter is allocated via `CreateIfAbsent` on a sentinel `seq.lock` file under the events dir. |

## 12. Sanitizers

A small library of sanitizers lives in `internal/catalogmodel`:

- `SanitizeBranch(name string) string` — branch → key segment.
- `SanitizeComponentKey(componentKey string) string` — slash → dash for index
  filenames.
- `SanitizeEventKind(kind string) string` — `.` → `-` for filename use.
- `ShortHex(full string, n int) string` — lowercase hex, length-checked.

All sanitizers are pure, total, and tested via property tests — no panics.
