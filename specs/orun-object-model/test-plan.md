# Test Plan

> Coverage targets, the property tests that lock the invariants, and the E2E
> walk. The four contract/identity packages carry the highest bars because
> their behavior is frozen.

## 1. Coverage gates (`make test-object-model`)

| Package | Min coverage | Notes |
|---------|--------------|-------|
| `internal/objectstore` | **90%** | frozen contract. The 95% aspiration is gated at 90% in practice: the residual lines are defensive filesystem-error returns in the atomic write path (temp/fsync/rename failures) that require a fault-injection FS to exercise — and the test env runs as root, so permission-based injection is bypassed. Building that seam is deferred (not worth the production-path indirection for error-wrapping branches). |
| `internal/objectstore/refstore` | **95%** | frozen contract |
| `internal/nodes` | **90%** | schemas + identity |
| `internal/nodewriter` | **90%** | the walk |
| `internal/objindex` | **90%** | derived, must rebuild exactly |
| `internal/objremote` | **85%** | file:// driver + sync |
| `internal/workingview` | **85%** | derived |
| `internal/runner` (object path) | **85%** | working tree + seal |

All run with `-race`. A drop below threshold fails CI.

## 2. Property-based tests (`pgregory.net/rapid`)

Map 1:1 onto `design.md` §5 invariants:

1. **Content integrity** — for arbitrary blobs/trees, `Get` returns bytes whose
   hash equals the id; a flipped byte on disk yields `ErrCorrupt`.
2. **Idempotent put** — putting identical content N times yields one object,
   one id, one on-disk file; concurrent identical puts all observe the final
   object.
3. **Tree Merkle dedup** — two trees sharing a subtree share that subtree's
   object; changing one leaf changes only the spine ids to the root, not
   siblings.
4. **Revision dedup across triggers** — for arbitrary (plan, catalog) pairs,
   compiling the same plan twice under different triggers yields one revision id
   and two distinct trigger objects.
5. **No-self-id / identity purity** — no canonical node body contains its own
   id; perturbing only a timestamp/trigger field does NOT change a content
   node's id (because those fields are excluded by schema).
6. **Ref CAS** — N concurrent `Update` calls with the same `oldTarget` produce
   exactly one success per round; publish-ordering: a ref target's closure is
   always fully present.
7. **GC safety** — for arbitrary ref sets + retention policy, GC removes exactly
   the unreachable, non-grace objects and is interruptible (kill at any sweep
   point ⇒ valid store; reachable set intact).
8. **Derived rebuildability** — delete `index/` + `current/`, rebuild, assert
   byte-identical to a fresh build.
9. **Execution key monotonicity** — N concurrent `CreateExecution` under one
   revision yield unique, monotonic `run-NNN` keys.
10. **Seal idempotence** — sealing the same working tree twice yields the same
    execution id; re-seal is a no-op.
11. **Migration idempotence** — `migrate` twice ⇒ identical object set + ref
    targets; dry-run twice ⇒ identical output.

## 3. Unit / table tests

- `nodes`: every schema round-trips canonical encode/decode; validators reject
  bad componentKey/status/edges.
- `nodewriter`: tolerant-strict branch matrix (main/branch/pr/local-nogit ×
  empty/non-empty catalog × strict/tolerant × catalog-reuse hit/miss).
- `objectstore`: framed-serialization golden vectors (pinned ids for fixed
  inputs so accidental encoding changes are caught); name/tree validation;
  `Walk` cycle/dedup; `Iterate` completeness.
- `refstore`: CAS conflict, absent-expect, list-by-prefix, delete.
- porcelain: `cat`/`show`/`log`/`ls-tree`/`rev-parse`/`fsck`/`reindex` command
  tests with golden output (truncated ids stabilized).

## 4. End-to-end (`cmd/orun/object_model_e2e_test.go`)

A full walk against a temp workspace + a temp `file://` remote:

```
1. orun plan                      → assert source/catalog/revision ids + refs
2. orun plan (again, no change)   → assert ALL ids reused (no new objects)
3. edit a non-catalog file, plan  → assert source id unchanged (dirty-hash scope)
4. edit a component.yaml, plan    → assert catalog id changes, unrelated manifests reused
5. orun run                        → working tree → seal; status/logs read sealed
6. second trigger, identical plan → assert revision REUSED, new trigger+execution only
7. orun gc                         → unreachable churn objects reclaimed; reachable intact
8. orun push file://remote         → remote gains the closure
9. orun push again                 → assert near-zero delta (Has hits)
10. fresh local, orun pull         → reproduce + orun run the pulled revision
11. orun fsck                      → green on both stores
```

## 5. Migration E2E

- Synthesize a legacy `.orun/` (plans + executions with state.json/metadata.json).
- `orun migrate --dry-run` (stable across two runs); `orun migrate`; `orun fsck`
  green; `orun status`/`get plans` read migrated content; second `migrate` is a
  near no-op.

## 6. Disk-size assertion (efficiency guard, M13)

- Build a fixed corpus (e.g. 50 revisions sharing 3 catalogs, 100 executions).
- Assert `du(.orun/objects)` (new) < `du(.orun)` legacy/Phase-2 layout for the
  same corpus by a recorded margin. Failing this is a dedup/compression
  regression.

## 7. CI wiring

- `make test-object-model` runs §1 gates + §2 property tests + §4/§5 E2E.
- Add a grep gate: no `internal/state` import outside migration (pre-M12) / at
  all (post-M12); no `json.Marshal` of records outside `nodes`; no object-path
  literals outside `objectstore`.
