# Backward Compatibility and Migration

Phase 1 lands additively. No existing user invocation may break; no existing
on-disk state may be destroyed. Migration into the new layout is a separate,
explicit, idempotent action.

---

## 1. What must keep working

| Workflow | Status | Phase 1 behavior |
|----------|--------|------------------|
| `orun plan` | preserved | Writes revision-first layout AND compat aliases. |
| `orun plan -o /tmp/plan.json` | preserved | Exports raw `plan.json` to the user path; revision-first layout still written. |
| `orun run --plan /tmp/plan.json` | preserved | Materializes a `system.manual` revision in-memory from the file; creates execution under it. |
| `orun run <hash>` | preserved | Resolution chain (see §3). |
| `orun run --exec-id <value>` | preserved | `executionKey = SanitizeExecID(value)`; original retained in `OriginalKey`. |
| `orun status`, `orun status --all` | preserved | Reads new refs first; falls back to legacy `.orun/executions/` scan. |
| `orun logs` | preserved | Resolves latest → revision+execution → job → step. |
| `orun describe` | preserved | Existing aliases still work; new `revision`, `trigger`, `execution` aliases added. |
| `orun get plans` | preserved | Prefers revision list; falls back to legacy `.orun/plans/` when empty. |
| Reading a repo that has never seen the new binary | preserved | Reader synthesizes in-memory `system.manual` trigger/revision metadata for display. |

---

## 2. Compatibility writes

In Phase 1, `orun plan` writes BOTH:

```
.orun/revisions/<revisionKey>/plan.json                  # canonical (new)
.orun/plans/<checksum>.json                              # alias  (legacy)
.orun/plans/latest.json                                  # alias  (legacy)
```

The legacy aliases are byte-identical copies of the canonical `plan.json`.

This is gated by a single internal flag:

```go
type WriterConfig struct {
    StateCompatibilityWrites bool // default: true in Phase 1
    // ...
}
```

Phase 2 flips the default; Phase 3 deletes the code path.

Compatibility writes go through `internal/statestore` if the legacy paths can
be expressed as logical paths (`plans/<checksum>.json`) — Phase 1 chooses to
keep them in the same store so atomic semantics carry over.

---

## 3. `orun run <arg>` resolution chain

`internal/revision.ResolveRevision` is the single resolver. Order:

```
1. arg is empty                  → refs/latest-revision.json
2. arg matches an existing file  → load plan from file; synthesize system.manual revision in-memory
3. arg matches revision key regex → revisions/<arg>/plan.json
4. arg matches a named ref       → refs/named/<arg>.json → revision key
5. arg is hex (legacy plan hash) → .orun/plans/<arg>.json (legacy load + synthesize migrated revision in-memory)
6. arg matches a component name  → existing component-run behavior (unchanged)
7. otherwise                      → typed error "ambiguous or unknown run target"
```

Branches 2 and 5 *do not write* a revision on disk by default. They produce a
runtime revision so the execution can attach. Users get the new lineage in the
display without forcing a write during a `--dry-run` or read-only command.

A `--persist-revision` flag is reserved for future addition that would
materialize the synthesized revision on disk; Phase 1 does not implement it.

---

## 4. Reader fallback

`internal/executionstate.ResolveExecution` and `internal/revision.ResolveRevision`
both implement a fallback strategy:

1. Try the new layout via `StateStore`.
2. On `ErrNotFound`, try the legacy filesystem path directly (`os.Stat` + raw
   `os.ReadFile`). The legacy data is unmarshalled into the new model in
   memory with `triggerType: "system"`, `triggerName: "system.migrated"`,
   `source.workingTree: "unknown"`.
3. If neither yields anything, return `ErrNotFound`.

The fallback is read-only. It never writes back to legacy paths and never
materializes synthesized records on disk.

---

## 5. Migration command (hidden)

```
orun state migrate            # writes
orun state migrate --dry-run  # plans + prints only
```

Registered with `Hidden: true` on the Cobra command. Lives at
`cmd/orun/command_state_migrate.go`.

### 5.1 Algorithm

```
load .orun/plans/*.json
for each legacy plan:
    synthesize TriggerOccurrence:
        triggerType:  "system"
        triggerName:  "system.migrated"
        mode:         "migration"
        source:       best-effort from filename / git
    compute revisionKey from synthesized trigger + plan hash
    if revision dir exists and revision.json matches the same plan hash: skip
    else: write revision-first layout via internal/revision.WriteRevision

load .orun/executions/*/state.json
for each legacy execution:
    look up its plan hash (from the execution's metadata.json)
    if a revision exists with that plan hash:
        attach: bridge.MirrorRunnerOutput(execKey=legacyExecID, revKey=match)
    else:
        attach to rev-migrated-unknown-p<hash> (created on demand)
emit summary: revisions_created, executions_attached, orphans
exit non-zero on any per-item error (but continue processing remaining items)
```

### 5.2 Properties

- **Idempotent.** Running twice produces no new files. The revision writer's
  `CreateIfAbsent` and a `(planHash, triggerKey)` equality check on existing
  revisions guarantee this.
- **Non-destructive.** No legacy file is deleted. The migration only reads
  from `.orun/plans/` and `.orun/executions/`, and writes to
  `.orun/revisions/`, `.orun/refs/`, `.orun/indexes/`.
- **Resumable.** A crash mid-migration leaves the new layout in a valid
  partial state; a re-run picks up where the previous left off.

### 5.3 Dry-run output

```
$ orun state migrate --dry-run

Plan migration:
  .orun/plans/a1b2c3d4.json
    → revision: rev-migrated-a1b2c3d4-p8f31c09
    → trigger:  trg-migrated-a1b2c3d4 (system.migrated)
  ...

Execution attachment:
  .orun/executions/exec-12345
    plan hash: a1b2c3d4...
    → revision: rev-migrated-a1b2c3d4-p8f31c09
    → execution key: exec-12345

Orphans:
  .orun/executions/exec-77777 (plan hash missing)
    → revision: rev-migrated-unknown-pdeadbeef
    → execution key: exec-77777

Summary (dry run):
  revisions to create:    7
  executions to attach:   23
  orphans:                1
```

The summary is printed to stdout. Non-zero exit only on error, not on
orphans.

---

## 6. When does Phase 1 ever delete?

Never. Phase 1 has no delete code path for legacy state. Phase 2 introduces a
`--prune-legacy` opt-in flag after the user explicitly migrates and verifies.
