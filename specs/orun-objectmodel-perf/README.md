# Spec: orun-objectmodel-perf

**Deferred performance/scale optimizations for the object-model store.** Two
optional, profiling-gated items, relocated here when `orun-legacy-retirement` was
archived (its core work — retiring the legacy catalog/revision store — shipped in
v2.15.0). Neither is required for correctness; the object model is fully
functional without them. Each is pulled in only when a measured threshold is
crossed.

> Provenance: **Bucket 2** and the packfile item of **Bucket 5** of the archived
> `specs/archive/orun-legacy-retirement/`; originally `orun-catalog-state` **L-2**
> and `specs/orun-object-model/FOLLOW-UPS.md` §2.

## Status

| Field | Value |
|-------|-------|
| Status | **Holding — not started; both items profiling/scale-gated.** No work until a pull-in trigger fires. |
| Type | Optional performance/scale optimizations (no behavior change) |
| Relocated from | `specs/archive/orun-legacy-retirement/` (Bucket 2 + Bucket 5 packfile) |
| Builds on | `specs/orun-object-model/` (the content-addressed graph), `specs/orun-catalog-state/` (`internal/objread`, `orun catalog history`) |
| Constraint | Both are pure optimizations: identical content ids, identical CLI output; **do not** change `orun catalog *` semantics. |

---

## 1. `objindex` — component→execution index

*Accelerator for `orun catalog history`'s component→execution join.*

Today `orun catalog history` serves the component→execution join by **scan +
filter** over `RunSummary.Components` (`objread.PlanSummary`) — the CS6 v1 choice.
At scale (many executions) this scan can dominate.

- [ ] Build a derived component→execution index in `internal/objindex` (mirrors
  the existing executions index) with reindex + walk-fallback.
- [ ] Switch `orun catalog history` to it (identical output; the index is a cache).

**Pull-in trigger:** the scan-+-filter join is *measured* too slow on a real
history. (Its original second condition — "D‑7 underway" — is already satisfied;
the legacy store is retired.)

---

## 2. Packfile delta compression

*Storage compaction for the object store.*

The object store writes loose, zstd-compressed objects. Git-style packfiles with
delta compression would shrink on-disk size for large histories (many
revisions/executions sharing structure). The content-addressing "disk win"
already beats a copy-per-run layout (`objmodele2e.TestObjectModelDedupDiskWin`);
packing is the next lever for very large repos.

- [ ] New milestone: pack format, pack/unpack, GC interaction, read-path
  fallthrough (loose → pack).

**Pull-in trigger:** profiling on real histories shows loose objects are the
on-disk-size bottleneck. (`specs/orun-object-model/design.md` lists this as a
"deferred milestone, profiling-gated.")

---

## Definition of done

This epic **closes by deferral**: it has no committed deliverable. It is the
single home for the two object-model perf optimizations so they are not lost now
that `orun-legacy-retirement` is archived. Either item graduates to real work only
when its pull-in trigger fires; until then this spec is the standing record of
*what* and *why*.

## References

- `specs/orun-object-model/FOLLOW-UPS.md` §2 — the packfile item in full.
- `specs/orun-catalog-state/` — `orun catalog history` and the component→execution join.
- `specs/archive/orun-legacy-retirement/` — the archived program these were relocated from.
