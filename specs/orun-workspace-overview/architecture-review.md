# Orun Workspace Overview (CLI half) — Architecture review

Status: Review (2026-07-01) — **ADOPTED.** The CLI half (README §3,
`implementation-plan.md`, now milestone **WO3**) was revised to incorporate every
finding below; this document remains as the rationale record. **The full
cross-repo review is normative in `orun-cloud`
`specs/epics/saas-workspace-overview/architecture-review.md`.** This
file carries the findings that land on the **`sourceplane/orun` (CLI/engine)**
half — WO2a — grounded against the code as it is today, not as the spec describes
it.

The epic is sound and the CLI design is the right shape: `Repo`/`Product` and the
doc bytes ride the **existing** catalog-snapshot push (`objremote.Sync` +
`AdvanceCatalogHead`) with no new wire call and no provider coupling. Keep that.
The corrections below are where the CLI spec states the code more cleanly than the
code behaves.

## CLI-half findings

### A1 — `CatalogSnapshot.Repo` is not normalized; don't mint the `Repo` ref from it

`CatalogSnapshot.Repo` is a verbatim passthrough of `ResolverInputs.Repo`
(`internal/catalogresolve/catalog_snapshot.go`) — e.g. `sourceplane/orun`, no
host, no scheme-stripping, no lowercasing. The spec keys `repo:<host>/<owner>/<name>`
off it and claims it matches the server's `state.workspace_links.remote_url`
normalization; **there is no such normalization on the CLI side.** Minting a ref
from that string makes the CLI's ad-hoc formatting a cross-repo contract.

**Do:** mint the `Repo` ref from the durable project/`ws_` id the platform already
trusts as the join key (`saas-workspace-id`), or, if the ref must be
remote-derived, add a normalization function that provably matches the server's
`remote_url` normalization and freeze it in the wire contract. The projected
`state.repo_facet` is already keyed `(org_id, source_project_id)`, so the repos
list does **not** need the ref string — bias toward the id.

### A2 — Adding `Repo`/`Product` is emit-path + graph work, not an `allEntityKinds` poke

`allEntityKinds` (`internal/catalogmodel/entity_ref.go`) is array-driven for
`IsEntityKind`/`NormalizeEntityKind`/`AllEntityKinds` and `--kind` validation —
those are one-liners. But a declared kind that carries relations also needs:

- graph wiring in `internal/catalogresolve/graph.go` `buildGraphs()` — the graph
  types (dependencies/systems/apis/resources/owners) are hardcoded; `Product`→
  `System` `partOf`/`hasPart` and `Repo` membership are net-new builder code;
- a resolver stage that **emits** `entities/Repo/*.json` + `entities/Product/*.json`
  — `System`/`Domain` today are *derived* from component specs, so there is no
  existing "emit a declared top-level entity" path to reuse;
- `internal/model/intent.go` `Repo`/`Products` struct fields (additive but real).

Re-scope WO2a Step 2–3 to "register + emit + relate," ~3–4 sites of real logic.

### A3 — "Read the doc at HEAD" is really "read the working tree" — make the pin real

The resolver reads the **working tree**, not a git object at a commit
(`internal/catalogresolve/*`). On the autopush path (clean default branch) that
equals HEAD and the point-in-time claim holds. But `plan --push-catalog` can run on
a **dirty** tree, and then the pushed `doc` bytes reflect uncommitted edits while
`doc_ref.{ref,sha}` and the provenance line point at a commit that never contained
them — silent drift on the one surface whose pitch is *drift-free*.

**Do:** when walking `docs.overview` into the closure, read the bytes from the git
object at the resolved commit, or refuse to attach doc objects on a dirty tree
(the gate the autopush path already enforces) and log why.

### B — Confirm whether `doc` needs to be its own object kind

Snapshot constituents already travel as `blob`/`tree` closure objects (write-time
`OBJECT_KINDS`; `objremote.Sync` walks that closure), so a `docs.overview` file is
mechanically just another content-addressed `blob` that `doc_ref.digest` locates.
A distinct `doc` kind is only worth the header value + the WO2a→WO2b coordination
("server must accept `Orun-Object-Kind: doc` before the CLI pushes it") if it is
the **quota/GC/retention boundary** for repo-authored prose. If it is, keep it and
say so; if it is only a label, push docs as ordinary closure blobs and delete the
coordination step. Decided in the orun-cloud review §B.

### Notes

- The 25 MiB single-shot / multipart split (`internal/remotestate/objsync.go`
  `singleShotMaxBytes`, `defaultPartSize`) already covers doc blobs; the
  overview-only-by-default + tree size-cap bound (Step 4, Q4) is the right guard
  — keep the "truncated → log, never silently drop" rule.
- Ref/relation tests (Step 5) should assert the ref-derivation decision from A1,
  not just "a ref is produced."

## What does not change

The invariants the CLI half preserves are correct and should stay verbatim: no new
wire call, no `run`-path change, no scope/auth change, no provider integration —
docs are pushed bytes, never fetched. See README §4.
