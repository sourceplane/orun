---
title: State model
---

`orun` keeps a complete history of every plan it has compiled and every
execution it has run on disk under `.orun/`. Since v2.10.0 that history is
**trigger-first, revision-first** — every plan is filed under the
`TriggerOccurrence` that produced it, and every execution is filed under
that revision.

This page documents the layout. It is the same layout the future R2/S3 and
Orun Cloud drivers will use; only the storage backend changes.

## Why revision-first

Pre-v2.10.0, plans lived as flat `.orun/plans/<sha>.json` files. The plan
told you *what* would run but not *why* it was compiled, and `orun status`
had to scan execution directories to reconstruct the link back to the plan.
The new layout makes the chain explicit:

```
TriggerOccurrence  →  PlanRevision  →  ExecutionRun(s)
   why                  what               how it went
```

Every `orun plan` resolves a `TriggerOccurrence` first, even for ad-hoc
local invocations — those become a `system.manual` trigger so the
trigger / revision / execution chain is unbroken.

## On-disk layout

```
.orun/
├── version.json                            # storage version marker
├── revisions/
│   └── rev-<scope>-<shortSha>-p<planHash8>/
│       ├── trigger.json                    # TriggerOccurrence (why)
│       ├── plan.json                       # the compiled plan (what)
│       ├── revision.json                   # PlanRevision summary
│       ├── manifest.json                   # latest execution summary
│       └── executions/
│           └── run-NNN/
│               ├── execution.json          # ExecutionRun
│               ├── snapshot.latest.json    # current job/step state
│               └── logs/<job>/<step>.log   # raw step output
├── refs/
│   ├── latest-revision.json                # → newest revision key
│   ├── latest-execution.json               # → newest execution under newest revision
│   └── triggers/
│       └── <name>/
│           ├── latest.json                 # latest revision for this trigger
│           └── <scope>.json                # per-scope pin (e.g. pr-139)
├── indexes/
│   ├── revisions/<key>.json                # quick lookup → revision summary
│   └── executions/<key>.json               # quick lookup → execution summary
├── plans/                                  # legacy compatibility mirror
│   ├── <checksum>.json
│   └── latest.json
└── executions/                             # legacy compatibility mirror
    └── <legacy-exec-id>/...                # hardlinked from revisions/.../executions/run-NNN/
```

The compatibility mirror is enabled by default so existing tooling that
reads `.orun/plans/` and `.orun/executions/` continues to work. Disable it
with `--state-compat-writes=false` once you have migrated.

## TriggerOccurrence

Captures the **why** of a plan. Every plan resolves one of:

| Trigger type | When emitted |
| --- | --- |
| `system.manual` | Bare `orun plan` / `orun run` with no `--trigger` and no `--from-ci`. |
| `system.manual-changed` | Manual invocation with `--changed`. |
| `system.replay` | A re-plan from an existing revision. |
| `system.api` | Plan compiled by the backend / a programmatic caller. |
| `system.migrated` | Synthesized by `orun state migrate` for legacy plans. |
| `declared` | Matched a `triggerBindings:` entry in `intent.yaml` (`--trigger NAME` or `--from-ci github --event-file …`). |

Schema fields:

```json
{
  "kind": "TriggerOccurrence",
  "triggerId":   "trg_01JX...",
  "triggerKey":  "trg-pr139-def456a",
  "triggerType": "declared",
  "triggerName": "github-pull-request",
  "mode":        "event-file",
  "provider":    "github",
  "event":       "pull_request",
  "action":      "synchronize",
  "source": {
    "repo":         "sourceplane/orun",
    "ref":          "refs/pull/139/head",
    "sourceScope":  "pr-139",
    "headRevision": "def456a...",
    "baseRevision": "abc1239...",
    "workingTree":  "clean"
  },
  "planScope": {
    "mode":              "changed",
    "base":              "abc1239...",
    "head":              "def456a...",
    "changedComponents": ["api-edge-worker"]
  }
}
```

See [Trigger bindings](./trigger-bindings.md) for how declared triggers
match a CI event.

## PlanRevision

A `PlanRevision` is the immutable pairing of a `TriggerOccurrence` and a
plan checksum. The revision **key** has the form

```
rev-<scope>-<shortHeadSha>-p<planHash8>
```

so `rev-pr139-def456a-p8f31c09` reads as *the plan compiled for PR 139 at
commit `def456a` whose plan hashes to `8f31c09…`*. Re-running the same
trigger with an unchanged plan returns the same revision (idempotent);
recompiling against a changed plan (or different commit) creates a new
revision next to it.

`refs/latest-revision.json` always points at the most recently created
revision. Per-trigger refs under `refs/triggers/<name>/` let you look up
"the latest revision for trigger `github-pull-request`" or "the latest
revision for `pr-139`" in a single read.

## ExecutionRun

Every `orun run` writes an `ExecutionRun` under its revision:

```
revisions/rev-pr139-def456a-p8f31c09/
└── executions/
    └── run-001/
        ├── execution.json
        ├── snapshot.latest.json
        └── logs/...
```

Subsequent runs of the same revision become `run-002`, `run-003`, …
deterministically. `refs/latest-execution.json` points at the newest run
across all revisions; `manifest.json` inside a revision tracks the latest
run for that revision specifically.

The runner still writes its native `.orun/executions/<legacy-id>/` tree;
the `executionstate.Bridge` mirrors each tick into `revisions/.../run-NNN/`
via hardlinks (with a copy fallback on cross-device file systems). This
lets every existing reader keep working unchanged while new readers prefer
the revision-first path.

## Resolution chain

`orun status`, `orun logs`, `orun describe`, and `orun run` follow the
same resolver:

1. `--revision <key>` → exact revision lookup.
2. `--exec-id <key>` → use `indexes/executions/` to find the revision +
   run.
3. `--plan <hash|name>` → resolve through the revision summary (plan
   hash → revision).
4. (default) → `refs/latest-execution.json`.
5. (compat fallback) → scan `.orun/executions/` for the legacy id when no
   new-layout match exists.

## Migrating a pre-v2.10.0 workspace

The hidden `orun state migrate` command walks `.orun/plans/` and
`.orun/executions/`, synthesizes a `system.migrated` trigger per legacy
plan, and rehomes each plan + its executions under the new layout. It is
idempotent — re-running it after new state has landed only fills in the
gaps. See [`orun state` →
`migrate`](../cli/orun-state.md) for the exact flags.

## What is *not* in Phase 1

- **R2 / S3 / Cloud `StateStore` drivers.** The local driver is the only
  driver shipping today. The interface is frozen so remote drivers can be
  added without changing callers.
- **Distributed locking.** Concurrent writes from a single host are safe
  via `CreateIfAbsent` and `CompareAndSwap`; cross-host coordination is a
  Phase 3 problem.
- **Cross-plan evidence reuse.** Reusing artifacts from a previous
  revision in a later plan is reserved for Phase 3.
- **TUI surface changes.** The TUI continues to read through the cockpit
  bridge; surfacing trigger / revision metadata directly in the TUI panes
  is on the cockpit roadmap, not this redesign.

## References

- [`orun plan`](../cli/orun-plan.md) — emits a fresh revision on every
  successful compile.
- [`orun run`](../cli/orun-run.md) — `--revision` flag and resolution
  chain.
- [`orun describe`](../cli/orun-describe.md) — `revision`, `trigger`, and
  `execution` aliases.
- [`orun state`](../cli/orun-state.md) — hidden `migrate` command.
- [Trigger bindings](./trigger-bindings.md) — how declared triggers
  match CI events.
