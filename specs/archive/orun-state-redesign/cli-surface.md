# CLI Surface

Exact behavioral changes for every affected `orun` command in Phase 1. The
constraint is: every existing invocation that worked before must still work.
New flags are additive.

---

## 1. `orun plan`

### 1.1 New on-success output

```
✓ Plan revision created

Revision: rev-pr139-def456a-p8f31c09
Trigger:  github-pull-request / pr-139 / def456a
Jobs:     12
Path:     .orun/revisions/rev-pr139-def456a-p8f31c09/plan.json
```

For system-manual:

```
✓ Plan revision created

Revision: rev-manual-def456a-p8f31c09
Trigger:  system.manual / manual / def456a
Jobs:     12
Path:     .orun/revisions/rev-manual-def456a-p8f31c09/plan.json
```

When `-o/--output` is set, the user-specified path is also printed.

### 1.2 Internal flow change

Before:

```
load intent
maybe resolve trigger (only if --trigger / --from-ci)
compile plan
write .orun/plans/<checksum>.json
```

After:

```
load intent
ALWAYS resolve TriggerOccurrence via internal/triggerctx
compile plan with trigger metadata embedded in plan.metadata
compute plan hash
derive revisionKey
write canonical layout via internal/revision.WriteRevision:
    trigger.json, plan.json, revision.json, manifest.json
update refs/latest-revision.json
update refs/triggers/<name>/{latest.json, <scope>.json}
update indexes/revisions/<key>.json
if stateCompatibilityWrites: also write .orun/plans/<checksum>.json + latest.json
```

### 1.3 Plan metadata embedding

Every compiled plan now includes a `metadata.trigger` and `metadata.revision`
block, not only triggered plans:

```json
{
  "metadata": {
    "trigger": {
      "type": "system",
      "name": "system.manual",
      "mode": "manual",
      "provider": "orun",
      "event": "manual",
      "scope": "full"
    },
    "revision": {
      "key": "rev-manual-def456a-p8f31c09",
      "planHash": "sha256:8f31c09..."
    }
  }
}
```

---

## 2. `orun run`

### 2.1 Resolution chain

See `compatibility-and-migration.md` §3 for the canonical chain.

### 2.2 Internal flow change

Before:

```
resolve plan from latest/hash/file/name
create .orun/executions/<execID>
execute jobs
write state/logs
```

After:

```
resolve PlanRevision via internal/revision.ResolveRevision
if no revision exists for the resolved plan:
    materialize system.manual revision in-memory (no disk write unless --persist-revision)
create execution via internal/executionstate.CreateExecution:
    derive executionKey (run-NNN or SanitizeExecID)
    write execution.json, snapshot.latest.json
    write indexes/executions/<execKey>.json
    write refs/latest-execution.json
hook the runner snapshot stream into internal/executionstate.Bridge.MirrorRunnerOutput
execute jobs (runner unchanged)
on each runner tick: bridge mirrors legacy .orun/executions/<legacyExecID> into the new layout
on terminal status: update execution.json, refs/latest-execution.json, manifest summary
```

### 2.3 New flags

- `--revision <key>` — execute the named revision (skips resolution chain).
- `--persist-revision` — *reserved*, not wired in Phase 1.

Existing flags (`--plan`, `--exec-id`, `--dry-run`, `--runner`, …) unchanged.

---

## 3. `orun status`

### 3.1 Default

Reads `refs/latest-execution.json`. On miss, scans `.orun/executions/` and
synthesizes a display row from the most recent.

### 3.2 New flags

- `--revision <key>` — list all executions under a revision.
- `--exec-id <key>` — show a specific execution by key.
- `--all` — list all executions across all revisions (uses
  `indexes/executions/`).

Existing flags (`--watch`, `--json`, `--detailed`) unchanged.

---

## 4. `orun logs`

Resolution order:

1. `--revision <revKey> --exec-id <execKey>` — exact lookup.
2. `--exec-id <execKey>` — uses `indexes/executions/`.
3. (default) — latest execution from `refs/latest-execution.json`.

Per-job, per-step targeting flags (`--job`, `--step`) unchanged.

Legacy `.orun/executions/<id>/logs/` is read transparently when the new layout
has no match (compat fallback).

---

## 5. `orun describe`

### 5.1 New aliases

```
orun describe revision latest
orun describe revision <revKey>
orun describe trigger latest
orun describe trigger <triggerName>
orun describe execution <execKey>
```

Each renders a structured detail view sourced from the relevant JSON
documents.

### 5.2 Existing aliases

`orun describe run latest`, `orun describe run <execID>` continue to work and
now resolve via the new execution resolver with legacy fallback.

---

## 6. `orun get plans`

### 6.1 Default output

When the new layout has revisions:

```
REVISION                              TRIGGER                    PLAN     JOBS  LATEST EXEC      STATUS
rev-pr139-def456a-p8f31c09            github-pull-request        8f31c09  12    run-001          completed
rev-manual-7a91ff0-pb91e72f           system.manual              b91e72f   8    run-003          failed
```

When only legacy plans exist:

```
PLAN HASH    CREATED
a1b2c3d4    2026-05-20 14:32
```

`--json` returns structured output keyed appropriately.

---

## 7. `orun state migrate` (hidden)

See `compatibility-and-migration.md` §5. Registered with `Cobra.Hidden: true`
so it does not appear in `--help` but is fully callable.

---

## 8. New command summary

| Command | Status |
|---------|--------|
| `orun state migrate` | new, hidden |
| `orun state migrate --dry-run` | new, hidden |

No other top-level commands are added in Phase 1.

---

## 9. What NOT to add this phase

- `orun revision …` namespace. Reserved; do not add. Use `orun describe
  revision …` instead.
- `orun ref pin <name>` and the `refs/named/` writer CLI. Format is reserved
  in `data-model.md`; CLI deferred.
- `orun state prune` / `--prune-legacy`. Out of scope until Phase 2.
- TUI changes. The cockpit spec consumes `StateStore` later.
