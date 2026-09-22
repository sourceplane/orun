---
title: orun spec
description: Spec docs are markdown files in the repository, annotated to an epic — push the committed copy with its pointer, and list what an epic carries.
---

`orun spec` connects a **spec doc** — a markdown file git owns — to an
**epic** in the task plane, the way component docs are annotated to
components. `push` uploads the copy committed at `HEAD` together with its
pointer (repository, path, commit sha), and the platform stores it sealed
beside the epic, where the console, the MCP, and the tracker sync read it
without a git credential. Pushes are idempotent by content hash, so CI can
run one on every merge with nothing to think about.

```bash
orun spec push <file>... --epic <epc_…|slug> [--repo owner/name]
orun spec list --epic <epc_…|slug>
```

Both subcommands take the shared cloud flags: `--workspace <org id|slug>`
(defaults to the linked repository's workspace), `--backend-url <url>`, and
`--json`. `--epic` is required on both; it accepts the `epc_…` id or the
epic's slug.

## `push`

Pushes one or more markdown files to an epic. Each file is read **as
committed at `HEAD`**: the sha must describe the bytes, so a file with
working-tree edits is pushed in its committed form and a note says so; a
file that is not committed at `HEAD` at all is refused with `commit it
first`. The slug derives from the filename (lowercased, everything outside
`[a-z0-9-]` folded to a dash, at most 80 characters); the title comes from
the first `# ` heading.

| Flag | Meaning |
|---|---|
| `--epic <ref>` | The epic to annotate. Required. |
| `--repo owner/name` | The repository's display identity. Defaults to what the `origin` remote says. |

```text
$ orun spec push specs/billing/README.md specs/billing/design.md --epic billing-v2
pushed readme → billing-v2 (4e2a9f10 as of 7b1c0d3e)
unchanged design → billing-v2 (91aa3c55 as of 7b1c0d3e)
note: specs/billing/design.md has working-tree changes — the COMMITTED copy was pushed; commit and push again to update
```

`unchanged` means the server already held that content hash — a no-op that
is safe to repeat. `--json` returns
`{"epic": …, "docs": [{"slug", "path", "sha", "updated", "contentHash"}, …]}`.

A typical CI step, after merge:

```bash
orun spec push specs/billing/*.md --epic billing-v2
```

## `list`

Lists an epic's spec docs: slug, title, the file pointer, the commit the
content was read at, and the content seal.

```text
$ orun spec list --epic billing-v2
SLUG     TITLE                 FILE                               AS OF     SEAL
readme   Billing v2            acme/api/specs/billing/README.md   7b1c0d3e  4e2a9f10
design   Billing v2 — design   acme/api/specs/billing/design.md   7b1c0d3e  91aa3c55
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Pushed (or unchanged) and listed. |
| `1` | `--epic` missing, a file outside a git repository or not committed at `HEAD`, a filename that yields no slug, no workspace or login, or a platform error. |

## Related

- [`orun task`](./orun-task.md) — epics, milestones, and tasks
- [Task plane](../concepts/task-plane.md)
- [`orun workspace`](./orun-workspace.md), [`orun auth`](./orun-auth.md)
