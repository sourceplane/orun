# CLI Surface

> Behavior of existing commands on the new model, plus new porcelain. The
> hybrid model means the object store is the truth and porcelain + the working
> view provide inspectability.

## 1. Existing commands (behavior on the new model)

| Command | New behavior |
|---------|--------------|
| `orun plan` | Tolerant-strict walk steps 1–4: resolve source → catalog (memoized) → revision (Has-gated reuse) → record trigger. Writes objects + moves refs. `-o FILE` still writes a plan copy. `--name X` writes `refs/named/X`. Prints the resolved ids + humanKeys + a reuse note ("revision reused: sha256:ccc…"). |
| `orun run` | Walk steps 1–6. Opens an execution working tree, drives the runner (native job/step), seals on terminal, moves `refs/executions/latest`. `--revision <id|name>` runs an existing revision; `--plan FILE` materializes a `system.manual` revision from a file. |
| `orun status [<exec>]` | Resolves via `refs/executions/latest` or an id/key; reads sealed `execution.json` or the live working tree. No legacy scan. `--all` lists from `objindex`. |
| `orun logs <exec> [job] [step]` | Resolves `StepAttempt.logId` → log blob/chunks. Streams from the working tree while live. |
| `orun describe <revision|execution|component>` | Reads the node + its closure via `ModelReader`. `--source`/`--catalog` describe parents. |
| `orun get plans` | Lists revisions from `objindex` (newest first), with humanKey + reuse/dedup annotation. |
| `orun catalog {list,tree,describe,diff,history,refs,validate,refresh}` | Read the catalog node + components/graph; `refresh` runs the resolver and writes a (possibly reused) catalog node + moves `refs/catalogs/*`. `diff` compares two catalog ids (cheap: Merkle subtree compare). |
| `orun gc` | Reachability GC (`object-store.md` §7). |

All commands accept either a **content id** (`sha256:…`), a **humanKey**, a
**ref name** (`@latest`, `main`, `pr-139`), or a **named alias** where it makes
sense; resolution order is documented per-command and tested.

## 2. New porcelain (git-style introspection)

| Command | Purpose |
|---------|---------|
| `orun cat <id>` | Print an object's canonical body (decompressed). `--pretty` re-indents JSON. |
| `orun show <ref\|id>` | Human summary of a node + its immediate edges (like `git show`). |
| `orun log [--component K] [--ref R]` | Walk the event history (triggers/executions) newest-first, like `git log`. |
| `orun ls-tree <treeId>` | List a tree's entries (`name kind id`). |
| `orun rev-parse <ref\|humanKey>` | Resolve a name to a content id. |
| `orun fsck` | Verify every object hashes to its id; report dangling edges among reachable objects; verify ref targets exist. |
| `orun reindex` | Rebuild `.orun/index/` from refs + objects. |
| `orun checkout [<ref\|id>]` | Materialize the working view (`.orun/current/`) for a node closure. |
| `orun push\|pull\|sync [remote]` | Object substitution with a remote (`remote-and-consumers.md`). |
| `orun migrate [--dry-run]` | One-shot legacy ingest (`compatibility-and-migration.md`). |

Porcelain commands that emit raw objects are **hidden** from the top-level help
group (advanced), grouped under `orun objects …` aliases if preferred during
implementation.

## 3. Output discipline

- Human output names both the **content id** (truncated `sha256:ccc…`) and the
  **humanKey** so users keep the readable lineage they had in Phase 1/2.
- A reuse/dedup event is surfaced explicitly ("catalog reused", "revision
  reused") so the dedup behavior is observable, not magic.
- `--json` on read commands emits canonical JSON for scripting.
- No secrets/tokens/emails in any log line (cross-cutting rule).

## 4. Flags that change semantics

- `--strict` — promote resolution *errors* (not validation *issues*) to hard
  failures; under `--strict`, a catalog with error-severity issues fails the
  walk. Default: tolerant.
- `--no-catalog` — skip catalog resolution; revision is still written but with
  no `catalogId` edge (degenerate; for emergency/no-repo use). Surfaced in
  output as a warning.
- `--remote <name>` — target a remote store for the operation (read or
  start-run), enabling SaaS-backed flows from the CLI/TUI.
