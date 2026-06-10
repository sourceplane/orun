# Compatibility and Migration

Phase 2 must not break Phase 1 callers. This document is the contract every
implementer PR is verified against.

---

## 1. Preserved CLI workflows

Every command below must behave identically from the user's perspective
after Phase 2 lands. Behavior listed in `specs/orun-state-redesign/cli-surface.md`
is preserved.

| Command | Phase 1 behavior | Phase 2 addition |
|---------|------------------|------------------|
| `orun plan` | Resolve trigger, write revision under `.orun/revisions/<revKey>/`. | Also writes under `sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/`. With `stateCompatibilityWrites = true`, writes both. |
| `orun plan --changed --intent intent.yaml --output plan.json` | Same. | Resolver block in plan metadata is added but does not change `plan.json` semantics consumed by `orun run`. |
| `orun run --plan plan.json` | Resolve revision, write execution under `.orun/executions/<execKey>/`. | Reads revision via global index (works for both old and new layout); execution lives under catalog-parented revision directory; mirror under `.orun/executions/` continues. |
| `orun status` | Reads from `.orun/executions/` and `.orun/revisions/` indexes. | Prefers Phase 2 layout via global execution index; falls back to legacy paths on miss. |
| `orun logs <execKey>` | Read execution logs by key. | Index-driven lookup; legacy path fallback retained. |
| `orun describe revision latest` | Show latest revision details. | Output gains `Source:` and `Catalog:` lines but otherwise identical. |
| `orun get plans` | Iterate revisions. | Iterates the global revision index, which now covers both layouts. |
| `orun state migrate` (hidden) | Phase 1 migrator. | Extended to optionally bucket legacy revisions into `cat-orphan-<srcKey>` when no catalog can be inferred. |

## 2. Resolution chain

Every revision/execution lookup follows the chain in
`catalog-store.md` §4. The chain is **read-only fallback** — it never writes
to legacy paths (writes go through the new layout, then optionally mirror
via the compatibility alias).

```text
1. Global index (Phase 1, unchanged shape; new fields are additive).
2. Walk catalog tree (Phase 2 canonical layout).
3. Walk legacy `.orun/revisions/<revKey>/` (Phase 1 layout).
4. (orun status only) Walk `.orun/executions/<execKey>/` (Phase 1 mirror).
```

A miss at the end of the chain returns `ErrNotFound`; the CLI surfaces a
human-readable error with the selector.

## 3. Compatibility writes

Default: `stateCompatibilityWrites = true` for the entire Phase 2 lifetime.
Behavior:

- `orun plan` writes the canonical revision under the catalog parent AND
  copies `plan.json` into `.orun/revisions/<revKey>/plan.json`.
- `internal/executionstate.Bridge` continues to mirror execution state into
  `.orun/executions/<execKey>/` exactly as in Phase 1.
- Refs (`refs/revisions/latest.json`, `refs/executions/latest.json`) keep
  their Phase 1 shapes; the `sourceSnapshotKey` and `catalogSnapshotKey`
  fields are appended (additive).

When users explicitly set `OOR_DISABLE_COMPAT=1` (env) or
`intent.yaml.state.compatibilityWrites: false` (config), the alias copies
are skipped. The CLI prints a one-line warning the first time per process.

## 4. Schema additivity

Phase 1 JSON shapes (`PlanRevision`, `ExecutionRun`, refs, indexes) gain
**additive fields only** in Phase 2:

```json
{
  "sourceSnapshotKey": "src-...",
  "catalogSnapshotKey": "cat-..."
}
```

No field is renamed. No field is removed. No field changes type. Old
readers (Phase 1 `internal/state` or external consumers) ignore the new
fields without error.

A property test (T-COMPAT-1) loads every fixture from
`internal/state/testdata/` (Phase 1) into the Phase 2 model and asserts a
clean roundtrip.

## 5. Catalog absence

Repos that have not run `orun catalog refresh` at all behave like Phase 1:

- `orun plan` auto-refreshes the catalog before compilation (default).
- `orun plan --no-catalog-refresh` skips the resolver and writes a revision
  with `sourceSnapshotKey = ""` and `catalogSnapshotKey = ""`. The plan is
  marked `metadata.catalog.skipped = true`. Compatibility writes proceed
  normally.
- `orun status` and `orun logs` operate fully without any catalog data.
- `orun catalog *` commands return a typed `ErrCatalogNotFound` with a hint
  pointing at `orun catalog refresh`.

## 6. Migration command

The Phase 1 `orun state migrate` command is extended (still hidden):

```bash
orun state migrate --to catalog [--dry-run] [--orphan-bucket cat-orphan]
```

Behavior:

- For each Phase 1 revision in `.orun/revisions/<revKey>/`, attempt to
  resolve a `(srcKey, catKey)` pair from the revision's stored
  `triggerOccurrence` and the worktree at the time of migration.
- Success → write a Phase 2 canonical revision tree (atomic
  `CreateIfAbsent`); leave the legacy directory in place if compatibility
  writes are still enabled.
- Failure → bucket under `sources/src-migrated-<sha>/catalogs/cat-orphan-<sha>/revisions/<revKey>/`
  with a `migration.json` metadata file explaining why.
- `--dry-run` prints the migration plan without writing.

Migration is **opt-in**. `orun catalog refresh` and ordinary `orun plan`
runs do not trigger automatic migration.

## 7. Sunset window

| Event | Trigger |
|-------|---------|
| Add `state.compatibilityWrites: false` to default `intent.yaml` | Phase 3 design accepted |
| Stop writing `.orun/revisions/<revKey>/` aliases | Two minor releases after Phase 3 starts |
| Drop legacy fallback in resolver chain | Phase 3 milestone "P3-M0" closes |

Phase 2 itself never removes Phase 1 paths. The sunset is owned by Phase 3.

## 8. Backwards compatibility checklist

Every implementer PR that touches `orun plan`, `orun run`, `orun status`,
`orun logs`, or `orun describe` must check:

- [ ] Existing fixture-based E2E tests in `cmd/orun/*_test.go` still pass
      unchanged.
- [ ] Phase 1 `internal/state/testdata` fixtures decode without error using
      the Phase 2 models.
- [ ] CI green on the existing `make test-state-redesign` gate (Phase 1
      coverage floors).
- [ ] If `stateCompatibilityWrites` is disabled, Phase 1 only commands
      surface a clear error rather than silently failing.
- [ ] `gh-{run_id}-{attempt}-{sha}` ExecID format is preserved in every code
      path.
