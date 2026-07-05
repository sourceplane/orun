---
title: orun spec
---

`orun spec` manages **frozen spec snapshots** — sealed, content-addressed
briefs an agent (or human) implements against, from the work lens
(specs/orun-work v2).

A `SpecSnapshot` is the **intent plane only**: the spec envelope plus task
contracts (Goal / Affects / Done-when / Gates / Deps), pinned to the two log
cursors it reflects. It structurally cannot carry a rung, an assignee, or any
other hot state — sealing fails if such bytes appear — so the ground cannot
shift under an implementation in flight.

```bash
orun spec pull <spec-slug>[@sha256:<hex>] [flags]
```

## `orun spec pull`

Fetches the spec's current intent from the workspace fold API, seals it into
a canonical JSON snapshot (one canonicalizer, one determinism contract —
sealing deliberately lives only in Go), and materializes a **read-only** view
under `.orun/specs/<slug>/`:

```
.orun/specs/<slug>/snapshot.json   # the sealed canonical bytes (0444)
.orun/specs/<slug>/BRIEF.md        # the human/agent-readable face (0444)
```

```bash
orun spec pull checkout-flow                   # seal + materialize
orun spec pull checkout-flow@sha256:9f2c…      # verify against a pin
orun spec pull checkout-flow --id-only         # print the content id (dispatch)
orun spec pull checkout-flow --push            # also sync refs/work to the remote
```

| Flag | Meaning |
| --- | --- |
| `--workspace` | Target workspace (defaults to the linked repo's) |
| `--backend-url` | Orun Cloud or self-hosted backend URL |
| `--id-only` | Print only the snapshot id — for scripting and dispatch |
| `--push` | Store the sealed blob in the object store and set-difference-sync `refs/work/specs/<slug>/latest` to the org/project-routed remote |

With an `@sha256:` pin the sealed snapshot must match exactly; a mismatch
fails with "the spec moved since the pin was minted" — the dispatcher's
guarantee that an agent implements against exactly the brief it was handed.

`--push` rides the same push spine the catalog uses: objects already present
remotely are skipped, and the work ref advances atomically.

## Related

- [`orun work`](./orun-work.md) — import and list the work lens
- [`orun mcp`](./orun-mcp.md) — `spec_get` serves the same sealed snapshot to agents
