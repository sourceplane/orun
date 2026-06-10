# CLI Surface

Exact behavioral spec for every new and modified `orun` command. Paired with
`compatibility-and-migration.md` — every existing command preserves prior
output unless explicitly noted.

---

## 1. Global flags (additions)

| Flag | Scope | Effect |
|------|-------|--------|
| `--catalog-source <selector>` | `plan`, `run`, `catalog *` | Resolve catalog by ref selector (`current`, `main`, `branches/<name>`, `prs/<n>`, or explicit `cat-<key>`). |
| `--catalog-snapshot <key>` | `plan`, `run`, `catalog *` | Bypass refs; pin to an explicit `catalogSnapshotKey`. |
| `--catalog-strict` | `plan`, `catalog *` | Promote validation warnings to errors. |
| `--no-catalog-refresh` | `plan` | Skip resolver; write revision with empty source/catalog keys (see compat §5). |
| `--no-infer` | `catalog refresh` | Disable inference layer (stage 6). |
| `--json` | every new `catalog *` command | Stable machine-readable output (see §11). |

`--source` is accepted as an alias for `--catalog-source` only on
`orun catalog *`.

## 2. `orun catalog refresh`

Materialize a `(SourceSnapshot, CatalogSnapshot)` pair for the current
workspace.

```bash
orun catalog refresh
orun catalog refresh --source main
orun catalog refresh --strict
orun catalog refresh --no-infer
orun catalog refresh --json
orun catalog refresh --sync         # Phase 2: emits "remote sync not configured"
```

Exit codes:

| Code | Meaning |
|------|---------|
| 0 | Snapshot created or reused (idempotent). |
| 1 | Validation error (or any warning if `--strict`). |
| 2 | Resolver internal error. |
| 3 | StateStore conflict / persistence failure. |

Default text output:

```text
✓ Catalog snapshot created

Source:    src-branch-main-cdef456a-t5ab21c3
Catalog:   cat-c8e91d2a
Ref:       refs/heads/main
Tree:      5ab21c3
State:     clean
Mode:      authoritative
Components: 42
Systems:    6
APIs:       12
Resources:  18
Path:      .orun/sources/src-branch-main-cdef456a-t5ab21c3/catalogs/cat-c8e91d2a/catalog.json
```

Idempotent reuse output:

```text
↺ Catalog up to date

Source:   src-branch-main-cdef456a-t5ab21c3
Catalog:  cat-c8e91d2a
```

Dirty workspace banner (always):

```text
⚠  Dirty worktree: snapshot is local-only.
    Use --sync-dirty-preview when remote sync is configured (Phase 3).
```

## 3. `orun catalog list`

```bash
orun catalog list
orun catalog list --source main
orun catalog list --owner team/platform-edge
orun catalog list --domain edge
orun catalog list --type cloudflare-worker
orun catalog list --status failed
orun catalog list --json
```

Default columns: `COMPONENT`, `TYPE`, `OWNER`, `SYSTEM`, `LAST EXEC`,
`STATUS`. Status is the `latestExecutionStatus` for that component in the
selected catalog.

`--json` emits one object per row with the keys
`{componentKey, name, type, owner, system, lastRevisionKey,
lastExecutionKey, lastExecutionStatus, sourceSnapshotKey,
catalogSnapshotKey}`.

## 4. `orun catalog describe <component>`

```bash
orun catalog describe api-edge
orun catalog describe api-edge --source main
orun catalog describe api-edge --source pr-139
orun catalog describe api-edge --json
```

Default text output sections (in order): Component, Ownership, Source,
Environments, Profiles, Dependencies, APIs, Resources, Runtime inference,
Latest executions, Resolution provenance.

`--json` emits the full `ComponentManifest` plus the catalog-local
`ComponentExecutionIndex` entry under
`{manifest, executions: []}`.

Component selectors:

- Bare name (`api-edge`) → resolved within the current workspace's repo.
- Fully qualified key (`sourceplane/orun/api-edge`) → exact match.
- Ambiguous bare name across repos → exit 4 with a list of candidates.

## 5. `orun catalog tree`

```bash
orun catalog tree
orun catalog tree api-edge
orun catalog tree --direction both
orun catalog tree --source current
orun catalog tree --json
```

`--direction` ∈ `out` (default), `in`, `both`. The text output is a
left-aligned tree with `→` arrows and edge-type annotations.

`--json` emits `{nodes: [...], edges: [...]}` matching `CatalogGraph` shape.

## 6. `orun catalog diff`

```bash
orun catalog diff --base main --head current
orun catalog diff --base main --head pr-139
orun catalog diff api-edge --base main --head current
orun catalog diff --json
```

Sections in default output: `Changed components`, `Added components`,
`Removed components`, `Graph changes`. Each component-level change shows
the field path, the base value, and the head value. Arrays are diffed
order-insensitively for set-shaped fields (`tags`, `providesApis`,
`consumesApis`) and order-sensitively for `dependsOn`.

Exit 0 even when differences exist; exit 5 only on resolver failure.

## 7. `orun catalog history <component>`

```bash
orun catalog history api-edge
orun catalog history api-edge --source main
orun catalog history api-edge --trigger github-push-main
orun catalog history api-edge --profile worker.release
orun catalog history api-edge --environment production
orun catalog history api-edge --json
```

Default columns: `TIME`, `REVISION`, `EXEC`, `TRIGGER`, `PROFILE`, `ENV`,
`STATUS`. Sorted newest-first. Default limit 50; `--limit N` adjusts.

## 8. `orun catalog refs`

```bash
orun catalog refs
orun catalog refs --json
```

Lists every catalog ref (`main`, `current`, branches, PRs) with the resolved
source/catalog keys and the `authoritative` flag.

## 9. `orun catalog validate`

```bash
orun catalog validate
orun catalog validate --strict
orun catalog validate --source current
orun catalog validate --rebuild-indexes      # added in C8
orun catalog validate --json
```

Reports the typed validation result list from
`resolution-pipeline.md` §6 + §8. Exit 1 on any error; exit 0 with warnings
unless `--strict`.

## 10. Modified commands

### 10.1 `orun plan`

New flags from §1 are accepted. Default behavior:

- Auto-refresh the catalog if missing or stale (catalog input hash changed).
- `orun plan --changed` resolves the catalog for the same source scope used
  by changed-detection.
- `orun plan --from-ci` resolves source/catalog from CI event context
  (provider trigger).

Output adds two lines after the existing trigger summary:

```text
Source:   src-branch-main-cdef456a-t5ab21c3
Catalog:  cat-c8e91d2a
```

### 10.2 `orun run`

No new flags. Output adds the same two lines after the revision summary.

### 10.3 `orun describe revision <selector>`

Output now reads:

```text
Revision: rev-main-def456a-p8f31c09
Source:   src-branch-main-cdef456a-t5ab21c3
Catalog:  cat-c8e91d2a
Trigger:  github-push-main
Jobs:     12
Path:     .orun/sources/.../revisions/rev-main-def456a-p8f31c09/plan.json
```

`orun describe execution`, `orun describe trigger` gain analogous Source +
Catalog lines.

### 10.4 `orun status`

Unchanged columns. The header gains an optional one-line summary when a
catalog is present:

```text
Catalog: cat-c8e91d2a (main, authoritative) — 42 components
```

Suppressed by `--quiet` and when no catalog has been refreshed.

## 11. JSON envelope

Every `--json` output uses the existing Orun envelope:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "CatalogListResult",
  "data": [...],
  "warnings": [...]
}
```

`kind` is one of `CatalogListResult`, `CatalogDescribeResult`,
`CatalogTreeResult`, `CatalogDiffResult`, `CatalogHistoryResult`,
`CatalogRefsResult`, `CatalogValidateResult`, `CatalogRefreshResult`.

## 12. Help text rules

- Every new command has a `--help` page with one-line summary, longer
  description, full flag list, two example invocations, and an "Exit codes"
  section.
- `orun catalog` (no subcommand) prints the subcommand index plus a one-liner
  per subcommand.
- All help text is fixture-tested in `cmd/orun/catalog_help_test.go` to
  prevent silent drift.
