---
title: orun epic
---

`orun epic` manages **frozen epic briefs** — the approval-sealed snapshot an
agent implements an epic from, introduced by the work lens's planning
hierarchy (specs/orun-work-v4).

Where [`orun spec pull`](./orun-spec.md) seals a spec's *current* intent on
demand, `orun epic pull` fetches the `EpicSnapshot` a **human approval
sealed on the server**: the epic envelope and resolved doc revision, the
milestone ladder with its `ladderHash`, informative task contracts grouped
under their milestones, the adopted design revision (when the epic was
minted from one), the approval record (who, when, at which revision), and
the catalog/log cursors it reflects. An unapproved epic has no brief —
`pull` fails with the epic's actual intent state instead.

```bash
orun epic pull <epic-slug>[@sha256:<hex>] [flags]
```

## `orun epic pull`

```bash
orun epic pull payments-v2                       # latest sealed brief
orun epic pull payments-v2@sha256:9f2c…          # pin to one snapshot id
orun epic pull payments-v2 --id-only             # print the id, for scripting
orun epic pull payments-v2 --push                # also sync refs/work/epics/<slug>/latest
```

The snapshot is **content-addressed**: the id *is* `sha256(canonical bytes)`,
and the CLI re-verifies that equality after download — a brief that doesn't
hash to its id is rejected, so the trust chain is the digest, not the
transport. The sealed bytes are additionally scanned for hot-state tokens
(rung, assignees, pins): a brief structurally cannot smuggle in lifecycle.

On success it materializes a **read-only** view:

```
.orun/epics/<slug>/snapshot.json   # the sealed canonical bytes (0444)
.orun/epics/<slug>/BRIEF.md        # the human/agent-readable face (0444)
```

`BRIEF.md` renders the epic doc, then the milestone ladder — each milestone's
goal and done-when with its task contracts beneath it — then the approval
record. It is the "implement against exactly this" artifact: the same bytes
the MCP's `epic_brief` tool serves.

| Flag | Effect |
| --- | --- |
| `--workspace <ref>` | Target workspace (org id or slug; defaults to the linked repo's) |
| `--backend-url <url>` | Backend URL (Orun Cloud or self-hosted) |
| `--id-only` | Print only the snapshot id (for scripting/dispatch) |
| `--push` | Also store the sealed brief in the object store and sync `refs/work/epics/<slug>/latest` |

## Why sealing lives on the server here

`orun spec pull` seals locally (one canonicalizer, in Go). Epic briefs are
sealed by the cloud **in the same transaction as the approval** — the
approval event and the snapshot id it stamps are inseparable — and every
consumer (this CLI, the MCP, the console) verifies the digest rather than
re-canonicalizing. One sealer, many verifiers: no cross-language
canonicalization drift can exist.

## Related

- [`orun spec`](./orun-spec.md) — the v2 spec brief this generalizes
- [`orun work`](./orun-work.md) — import and inspect the hierarchy
- [`orun mcp`](./orun-mcp.md) — `epic_brief` serves the same sealed bytes
