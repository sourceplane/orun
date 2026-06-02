# Implementation Status — orun-object-model

Status of the content-addressed object-model rewrite (`specs/orun-object-model/`)
as implemented. This is the as-built record; the design docs describe the intent.

## Summary

Milestones **M0–M11 + M13 are implemented, merged to `main`, and tested
end-to-end.** The object model is a complete, content-addressed (git/Nix-shaped)
state layer that runs **additively behind two feature flags** — with the flags
unset, `orun` behavior is byte-identical to before. The one milestone **not**
done is the true cutover (**M12** — native runner rewrite + legacy deletion),
which is scoped in `M12-native-runner-rewrite.md`.

| Field | Value |
|-------|-------|
| Milestones done | M0, M1, M2, M3, M4, M5, M5b, M6, M7, M7b, M7c, M8, M9, M10, M11, M13 |
| Milestone open | **M12** (native runner rewrite + legacy delete) |
| PRs merged | 19 (#191 spec … #208 e2e) |
| Test status | full module suite green; object-model gate green; `-race` clean; `verify-generated` clean |
| Flags | `ORUN_OBJECT_MODEL=1` (plan writes), `ORUN_OBJECT_RUNNER=1` (run seals) — default off |
| Isolation | object graph lives under `.orun/objectmodel/`; legacy `.orun/` untouched |

## What was implemented

### Layers (L0–L4)

| Package | Layer | Responsibility | Coverage |
|---------|-------|----------------|----------|
| `internal/objectstore` | L0 | content-addressed blob/tree store: sha256 framing, canonical JSON, zstd, atomic loose writes, `Has`/`Walk`/`Iterate`/`Delete`, `MemStore` | 91.1% |
| `internal/objectstore/refstore` | L2 | mutable `name→id` refs with compare-and-swap (per-ref lockfile) | 88.5% |
| `internal/clock` | — | injectable wall clock (keeps `time.Now()` out of gated dirs) | (tested) |
| `internal/nodes` | L1 | every node schema, canonical encode, validation, Merkle tree assembly, pure id helpers | 91.5% |
| `internal/nodewriter` | — | tolerant-strict write walk (`Has`-gated reuse, ref moves) | 91.8% |
| `internal/objplan` | — | adapter: `sourcectx`/`catalogresolve` → node types; resolve memo cache; degenerate `local-nogit` | 92.0% |
| `internal/workingview` | L4 | `fsck`, materialized checkout, `cat`/`ls-tree`/`rev-parse` read primitives | 85.7% |
| `internal/execseal` | — | seal a finished run into an immutable `ExecutionRun` tree + publish refs | 100% |
| `internal/objindex` | L3 | derived executions index (build/reindex/list) with walk fallback | 88.2% |
| `internal/objgc` | — | reachability mark-sweep GC + retention + grace window | 91.0% |
| `internal/objremote` | — | object substitution (push/pull = closure set-difference + ref move) | 96.8% |
| `internal/objexec` | bridge | legacy `ExecState` → native execution seal input (**transitional**) | 98.6% |
| `internal/objmigrate` | bridge | one-shot legacy `.orun/` ingest (**transitional**) | 89.8% |
| `internal/objmodele2e` | test | end-to-end walk + dedup/disk-win assertion | (e2e) |

The two **bridge** packages deliberately import legacy `internal/state`; they are
excluded from the object-model lint gate and are removed at the M12 cutover.

### CLI surface (live behind flags)

```
ORUN_OBJECT_MODEL=1 orun plan     # write source → catalog → revision → trigger
ORUN_OBJECT_RUNNER=1 orun run     # + seal ExecutionRun (native job/attempt/step)

orun objects cat <id|ref>         # pretty-print an object
orun objects ls-tree <id|ref>     # list tree entries
orun objects rev-parse <ref>      # resolve a ref to an id
orun objects log                  # executions newest-first
orun objects fsck                 # integrity + closure verification
orun objects checkout [ref]       # materialize a readable tree
orun objects reindex              # rebuild derived indexes
orun objects gc [--dry-run] [--keep N] [--grace DUR]
orun objects migrate [--dry-run]  # ingest legacy .orun/
orun objects push <remote-dir> [ref]
orun objects pull <remote-dir> [ref]
```
The `objects` group is hidden from top-level help until the M12 cutover.

### On-disk layout (under `.orun/objectmodel/` during coexistence)

```
objectmodel/
  objects/<algo>/<aa>/<rest>     # zstd-compressed blobs & trees, content-addressed
  refs/
    sources/{current,main} · sources/branches/<b> · sources/prs/<pr>
    catalogs/{current,main} · catalogs/branches/<b> · catalogs/prs/<pr>
    revisions/latest · revisions/by-hash/<checksum>   (migrate)
    triggers/<name>/latest
    executions/latest · executions/by-id/<execId>
  index/executions/all.json      # derived, rebuildable
  cache/resolve/<srcId>-rv<n>.json  # catalog resolve memo (derived)
  current/                       # materialized checkout (derived)
```
The M12 cutover relocates this to the `.orun/` root and makes it canonical.

### Properties verified by tests

- **Content integrity** — every object hashes to its id (`fsck`, read-time verify).
- **Revision dedup across triggers** — identical plan ⇒ one revision; each trigger a distinct event.
- **Catalog memoization** — unchanged source skips the resolver; clean-tree re-plan is near-free.
- **Reachability GC** — orphans swept, reachable graph intact, grace window protects in-flight seals, safe to interrupt.
- **Object substitution** — push copies only the delta; second push is a near-no-op; pull round-trips into a fresh store.
- **Idempotent migration** — re-running ingests the same objects and moves refs to the same targets; never deletes legacy.
- **Disk win (measured)** — 50 plans against one catalog store the catalog **once** (13 objects); each extra plan adds **~4** objects; total **214** vs. a naive copy-per-plan **~650**.

## Test results (as of this doc)

```
go build ./...            OK
go vet ./...              OK
go test ./...             all packages ok, 0 failures (incl. cmd/orun, legacy suites)
make test-object-model    all 12 packages 85–100% + e2e — green
make verify-generated     OK
go test -race (obj pkgs)  clean
CLI smoke (real binary)   objects fsck/log/migrate/gc all run
```

## The remaining gap — M12 (true cutover)

The object model **bridges from** legacy state: `orun run` (under
`ORUN_OBJECT_RUNNER`) lets the legacy runner write its `state.json`, then
`objexec` reads that and seals a native execution. Therefore **`internal/state`
cannot be deleted** — the seal depends on it.

True cutover requires rewriting `internal/runner` to write the working-tree/seal
**natively** (no legacy read), satisfying the parity matrix in
`runner-integration.md` §4, then flipping the default and deleting the legacy
module + the two bridges. That work is specified in
`M12-native-runner-rewrite.md`.
