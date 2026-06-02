# Object Store Contract (L0)

> The frozen contract. Everything else is built on this. RFC 2119 keywords are
> normative.

## 1. Object model

There are exactly two **structural** object kinds:

- **`blob`** — an opaque, ordered byte string. Used for record bodies
  (`source.json`, `catalog.json`, a `ComponentManifest`, `plan.json`,
  `execution.json`, a `JobRun`/attempt/step record) and for raw artifacts
  (log segments, captured outputs).
- **`tree`** — an ordered set of entries. Each entry is
  `{ name: string, kind: "blob"|"tree", id: ObjectID }`. Entries MUST be sorted
  by `name` (byte order) and `name` MUST be unique within a tree and MUST match
  `^[A-Za-z0-9._-]+$` (no `/`, no `.`/`..`).

A node (Source, Catalog, Revision, …) is **not** a third kind: it is a `tree`
whose entries point at the node's record blob and child trees. The node's
*identity* is its tree id (the Merkle root). The only exception is
`TriggerOccurrence`, which is a single record blob (it has no children worth a
tree); its identity is the blob id.

## 2. Object identity

```
ObjectID := "<algo>:<lowerhex>"          // wire form, e.g. "sha256:9f86d0…"
on-disk  := objects/<algo>/<hex[:2]>/<hex[2:]>
```

- **`algo` is `sha256` in v1.** The algorithm is a first-class field so a future
  `blake3` is a config swap, not a migration crisis. A store MAY contain objects
  under multiple algos; references always carry the full `<algo>:<hex>`.
- The hash is computed over the **uncompressed canonical serialization**
  (§3), NOT over the compressed on-disk bytes. Compression MUST NOT affect
  identity.
- The canonical serialization is a git-style framed payload:

  ```
  serialize(obj) := type ++ " " ++ decimal(len(body)) ++ "\x00" ++ body
      type ∈ {"blob","tree"}
      body  = the blob bytes,  OR  the canonical tree encoding (§2.1)
  id(obj) = "<algo>:" + hex(H(serialize(obj)))
  ```

  Framing the type+length prevents type-confusion and second-preimage tricks
  across kinds (same reason git frames its objects).

### 2.1 Canonical tree encoding

```
for each entry in name-sorted order:
    kind ++ " " ++ name ++ "\x00" ++ rawhex_id      // rawhex_id = the hex, no "algo:" prefix, fixed width
```

Trees within a single store use a single algo; the algo is implied by the
store. (If multi-algo is ever enabled, tree encoding gains an algo byte — out of
scope for v1.)

## 3. Canonical JSON (record blobs)

All record blobs (`*.json` nodes) MUST be encoded canonically so identical
records hash identically:

- UTF-8, object keys sorted ascending by byte order, recursively.
- No insignificant whitespace (no spaces after `:`/`,`, no indentation, no
  trailing newline).
- Integers as JSON numbers without exponent; no `-0`; floats are forbidden in
  schemas (use strings for anything non-integer).
- Absent optional fields are **omitted**, never `null` (omitempty), so a record
  has exactly one canonical form.
- RFC 3339 / Z timestamps as strings.

`internal/nodes` MUST expose `CanonicalEncode(v any) ([]byte, error)` and use it
for every record; ad-hoc `json.Marshal` of a record is banned (lint/grep gate).

## 4. The `ObjectStore` interface

```go
package objectstore

type ObjectID string // "<algo>:<hex>"

type Kind string
const ( KindBlob Kind = "blob"; KindTree Kind = "tree" )

type TreeEntry struct {
    Name string
    Kind Kind
    ID   ObjectID
}

type ObjectStore interface {
    // Root returns a diagnostic identifier (local: abs path; remote: bucket+prefix).
    Root() string

    // PutBlob stores bytes as a blob and returns its content id.
    // Idempotent: storing identical bytes returns the same id and is a no-op on disk.
    PutBlob(ctx context.Context, data []byte) (ObjectID, error)

    // PutTree stores a tree (entries need not be pre-sorted; the store sorts+validates).
    // Returns ErrInvalid on duplicate/illegal names or a referenced id absent
    // when opts.Strict is set.
    PutTree(ctx context.Context, entries []TreeEntry) (ObjectID, error)

    // Get returns the uncompressed canonical body and the kind. ErrNotFound if absent.
    Get(ctx context.Context, id ObjectID) (kind Kind, body []byte, err error)

    // GetTree decodes a tree object. ErrNotFound / ErrInvalid as appropriate.
    GetTree(ctx context.Context, id ObjectID) ([]TreeEntry, error)

    // Has reports presence without reading the body. The hot path for reuse.
    Has(ctx context.Context, id ObjectID) (bool, error)

    // Walk visits every object reachable from root (DFS), invoking fn once per id.
    // Used by GC marking and fsck. Cycle-safe (ids are acyclic by construction,
    // but Walk dedups visited ids regardless).
    Walk(ctx context.Context, root ObjectID, fn func(ObjectID, Kind) error) error

    // Iterate enumerates every object id present in the store (GC sweep, fsck).
    Iterate(ctx context.Context, fn func(ObjectID) error) error

    // Delete removes a single object by id. Used only by GC; callers MUST have
    // proven unreachability. No-op if absent.
    Delete(ctx context.Context, id ObjectID) error
}
```

Notes:
- There is **no `Update`**. Objects are immutable. Mutation lives entirely in
  the ref store (§6).
- `PutBlob`/`PutTree` MUST be atomic (temp-file + `fsync` + rename on local FS)
  and safe under concurrent identical puts (both observe the final object).
- `Get` MUST verify `hash(body) == id` and return `ErrCorrupt` on mismatch
  (cheap integrity at read time; `orun fsck` does the full sweep).

## 5. Compression & on-disk format

- Each loose object file is `zstd(serialize(obj))`. The decompressor recovers
  the framed payload; the store strips the frame and returns the body.
- zstd level is configurable (`ORUN_OBJECT_ZSTD_LEVEL`, default 3 — fast). Level
  does not affect identity.
- A loose object path is `objects/<algo>/<hex[:2]>/<hex[2:]>`. The two-char
  fanout keeps directory sizes sane without packfiles.
- **Packfiles are out of scope for v1** (see `risks-and-open-questions.md`). The
  interface above is packfile-ready: `Iterate`/`Get`/`Has` hide the storage form
  so a later packed driver is transparent.

## 6. Refs (the mutable layer) — `refstore`

```go
package refstore

type Ref struct {
    Kind      string    `json:"kind"`      // "Ref"
    Target    string    `json:"target"`    // ObjectID
    UpdatedAt time.Time `json:"updatedAt"`
    Writer    string    `json:"writer"`    // "cli"|"runner"|"tui"|"saas"|"migrate"
}

type RefStore interface {
    Read(ctx, name string) (Ref, error)                 // ErrNotFound if unset
    // CAS: write only if current target == oldTarget (""=expect-absent). ErrConflict on mismatch.
    Update(ctx, name, oldTarget, newTarget string) error
    List(ctx, prefix string) ([]string, error)
    Delete(ctx, name string) error                       // GC of stale live-exec handles
}
```

- Ref names are logical paths under `refs/` with the same alphabet as object
  tree names per segment.
- A ref write is an atomic object-style write of the `Ref` record at
  `refs/<name>.json` (temp+rename). `Update` reads-then-CAS with a per-ref
  lockfile on local FS; remote drivers use conditional writes (ETag / If-Match).

## 7. Garbage collection

GC is mark-and-sweep over reachability from refs plus a retention policy.

```
roots := all ref targets
      ∪ retained-execution closure (policy: keep last N sealed execs per ref scope,
                                    keep all named, keep all reachable from kept)
mark  := closure(roots) via Walk
sweep := for id in Iterate: if id ∉ mark: Delete(id)
```

- GC MUST be **safe to interrupt**: sweeping deletes only proven-unreachable
  objects; a half-run leaves a valid store.
- GC MUST NOT run concurrently with a seal that has written objects but not yet
  moved its ref. Mitigation: the seal holds a short *gc-fence* lock; GC respects
  a grace window (objects newer than `gracePeriod`, default 1h, are never swept
  even if currently unreachable) so in-flight closures aren't collected.
- `orun gc [--prune-older-than DUR] [--keep N] [--dry-run]`. Reports bytes
  reclaimed and objects removed.

## 8. Error taxonomy

`ErrNotFound`, `ErrExists` (reserved), `ErrConflict` (ref CAS), `ErrInvalid`
(bad name/tree/id), `ErrCorrupt` (hash mismatch on read). All surfaced via
`errors.Is`/`errors.As`. String-sniffing is banned.

## 9. Drivers

- **`LocalStore`** — `objects/` + `refs/` under a root (default `.orun/`).
  Atomic temp+rename, fsync, lockfiles. The v1 production driver.
- **`MemStore`** — in-memory, for tests.
- **`RemoteStore`** (seam, `internal/objremote`) — same interface over an object
  bucket (`file://` reference driver in scope; R2/S3 a thin adapter). Because
  ids are content hashes, `Has`-before-`Put` makes push/pull a set difference.
  See `remote-and-consumers.md`.
