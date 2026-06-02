# Remote Substitution & Consumers (SaaS, TUI)

> How the same object graph serves remote sync, the SaaS backend, and the TUI
> cockpit — for both viewing and *starting* runs. Content addressing makes all
> three the same problem.

## 1. Remote substitution (the git-push / Nix-binary-cache model)

Because every immutable node is named by the hash of its content, **the same
object has the same id on every machine and in the cloud.** Sync is therefore a
set difference, not a replication protocol.

```go
package objremote

// RemoteStore is an ObjectStore over a remote bucket (file://, R2, S3, Orun Cloud).
// It is the SAME interface as the local store (object-store.md §4) plus auth/config.

// Push: for each object in the local closure of `root`, if !remote.Has(id) then
//       remote.PutBlob/PutTree(local.Get(id)). Then move the remote ref (CAS).
func Push(ctx, local, remote ObjectStore, refs RefStore, root ObjectID, refName string) error

// Pull: mirror image. Fetch objects the local store lacks for `root`, then move
//       the local ref.
func Pull(ctx, local, remote ObjectStore, refs RefStore, root ObjectID, refName string) error
```

- **Dedup is global.** If the SaaS already has a catalog object (because another
  teammate pushed it), your push sends only the objects it lacks — typically a
  trigger blob and a sealed execution closure. A `main` catalog shared by a
  whole org is stored once in the cloud.
- **Integrity end-to-end.** The remote verifies `hash(body)==id` on `Put`; a
  tampered object is rejected by construction.
- **No bespoke schema migration for sync.** The wire payload *is* the object
  bytes. Adding a node field changes the hash, producing a new object — old and
  new coexist; nothing to migrate on the wire.
- **Drivers in scope:** `file://` reference remote (a second `LocalStore` over a
  different root — used by tests and the E2E push/pull walk). R2/S3 is a thin
  `Has/Put/Get` adapter over the bucket (key = `objects/<algo>/<aa>/<rest>`),
  with **hardening deferred to Phase-3** but the seam landed here.

## 2. The SaaS shape (so the model is built right for it)

The SaaS is **the same object graph + ref store + an index service**, hosted:

```
Orun Cloud
├─ Object bucket (R2/S3)         content-addressed blobs/trees (immutable, dedup'd org-wide)
├─ Ref store (CAS-capable KV)    refs/sources, refs/catalogs, refs/executions, …  per project
├─ Index service (Supabase/D1)   the L3 indexes as queryable tables (rebuildable from objects)
└─ API                           Has/Put/Get/UpdateRef + index queries + "start run"
```

- **Routing:** a remote object key is `orgs/<org>/projects/<project>/objects/…`
  and refs `orgs/<org>/projects/<project>/refs/…`. The *logical* id is identical
  to local; the routing prefix is added by the remote driver (this is the
  Phase-1 "remote-shaped path" promise, now realized for objects).
- **Indexes server-side:** the SaaS rebuilds L3 indexes into a real database for
  fast queries (component → revisions → executions, status dashboards), but the
  database is **derived** — the objects + refs remain the source of truth and
  can rebuild it. No fact lives only in the database.
- **Auth/multi-tenant** are Phase-3; this spec only ensures the key/ref/closure
  shapes don't have to change to add them.

## 3. TUI cockpit consumption

The TUI reads and writes the **same** model — no second data path.

**Viewing (read):**
- Source/catalog/component pages: read `refs/catalogs/current` → catalog tree →
  `components/` + `graph/`, and `objindex` for reverse lookups.
- Runs list / status: read `index/executions/by-time` + `by-status`; resolve
  each `executionId` to its sealed (or live) `execution.json`.
- Live runs: watch `refs/executions/live/<id>` (heartbeat payload) + the working
  tree under `.orun/run/<id>/`; the TUI's existing watch loop points at these
  paths instead of `internal/state`.
- Logs: resolve `StepAttempt.logId` → log blob (or chunk tree); stream from the
  working tree while live, from the sealed object when done.

**Starting runs (write):**
- The TUI invokes the **same `nodewriter` tolerant-strict walk** the CLI uses:
  resolve source/catalog/revision, record a `TriggerOccurrence` (with
  `actor:"tui"`), open an execution working tree, and drive the runner. It does
  not duplicate any logic — it calls the same entry point `orun` does.
- Remote (SaaS-backed) runs: the TUI targets a `RemoteStore`; "start run"
  pushes the revision closure (usually a no-op — the cloud already has it) and
  posts a trigger via the SaaS API. The cloud schedules the execution; the TUI
  pulls sealed execution objects to display results.

**Consequence:** the cockpit spec (`.kiro/specs/orun-tui-cockpit/`) consumes a
single seam — `nodes` + `objindex` + `refstore` + `objremote` — for both local
and remote, instead of branching on local-vs-remote state representations.

## 4. Consumer seam (the only API consumers depend on)

```go
// Read seam — TUI, SaaS API, porcelain all use these; nobody reads files directly.
type ModelReader interface {
    ResolveRef(ctx, name string) (ObjectID, error)
    Catalog(ctx, id ObjectID) (CatalogView, error)        // catalog + components + graph
    Revision(ctx, id ObjectID) (RevisionView, error)
    Execution(ctx, id ObjectID) (ExecutionView, error)    // sealed or live
    ComponentHistory(ctx, componentKey string) (HistoryView, error)
    ListExecutions(ctx, filter Filter) ([]ExecSummary, error)
}

// Write seam — CLI, TUI, SaaS all start runs through here (the tolerant-strict walk).
type RunStarter interface {
    Plan(ctx, opts PlanOptions) (RevisionView, error)     // walk steps 1–4
    Run(ctx, opts RunOptions) (ExecutionHandle, error)    // walk steps 1–6
}
```

`ModelReader`/`RunStarter` are backed by a local store or a remote store
interchangeably (same `ObjectStore`/`RefStore` interfaces). This is what makes
"the TUI and SaaS leverage remote state via the same model" literally true: they
hold a `RemoteStore` instead of a `LocalStore` and everything above is identical.

## 5. Offline / hybrid

- Local-first: all commands work fully offline against `.orun/`.
- `orun push`/`pull`/`sync` reconcile with a remote when present.
- A team member can `orun pull` a teammate's revision closure and re-run it
  locally (provenance + plan are content, so the run is reproducible) — the
  "reproduce a CI run locally" pain point from Phase 1, solved by construction.
