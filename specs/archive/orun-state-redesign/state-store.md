# StateStore Contract

The `StateStore` is the *only* path through which new layout files are written
in Phase 1 (legacy compatibility writes are explicit exceptions documented
in `compatibility-and-migration.md`). The interface is engineered so that a
future R2/S3/Cloud driver can be added without changing callers.

This document defines the interface, the local-driver behavior, the path
conventions, the atomicity guarantees, and the error taxonomy.

---

## 1. Interface

```go
package statestore

import (
    "context"
    "time"
)

type StateStore interface {
    // Root returns the root identifier of the store (local: absolute fs path;
    // remote: bucket+prefix). Used only for diagnostics and logging.
    Root() string

    // Read returns the bytes and metadata for the given logical path.
    // Returns ErrNotFound if absent.
    Read(ctx context.Context, path string) ([]byte, ObjectMeta, error)

    // Write atomically replaces the object at path with data.
    // Concurrent writes are serialized via temp-file + rename on local FS.
    Write(ctx context.Context, path string, data []byte, opts WriteOptions) (ObjectMeta, error)

    // CreateIfAbsent writes data only if no object exists at path.
    // Returns ErrExists on collision.
    CreateIfAbsent(ctx context.Context, path string, data []byte) (ObjectMeta, error)

    // CompareAndSwap writes data only if the current object's Revision equals
    // oldRev. Returns ErrConflict on revision mismatch, ErrNotFound if the
    // object does not exist.
    CompareAndSwap(ctx context.Context, path string, oldRev string, data []byte) (ObjectMeta, error)

    // List returns object info for every object whose logical path begins with
    // prefix. Order is unspecified.
    List(ctx context.Context, prefix string) ([]ObjectInfo, error)

    // Delete removes a single object. No-op if absent. Phase 1 does NOT support
    // recursive deletion.
    Delete(ctx context.Context, path string) error
}

type ObjectMeta struct {
    Path      string
    Size      int64
    Revision  string    // content-derived revision identifier
    UpdatedAt time.Time
}

type ObjectInfo struct {
    Path      string
    Size      int64
    UpdatedAt time.Time
}

type WriteOptions struct {
    // Future: ContentType, Cache, etc. Empty in Phase 1.
}
```

---

## 2. Path convention

- Paths are **logical**, forward-slash separated, root-relative, no leading
  slash. Example: `revisions/rev-pr139-def456a-p8f31c09/plan.json`.
- Path components are restricted to `[a-zA-Z0-9._-]` with `/` as separator. No
  `..`, no absolute paths, no Windows backslashes.
- The local driver translates `/` to `os.PathSeparator` and roots paths at the
  configured local root (default `.orun/` under the workspace).
- A future remote driver prepends a routing prefix (e.g.
  `orgs/<org>/projects/<project>/.orun/`) and uses the logical path as the
  remainder of the key verbatim.

### 2.1 Path helpers

`internal/statestore/paths.go` exposes:

```go
func RevisionDir(revKey string) string                  // "revisions/<key>"
func PlanPath(revKey string) string                     // "revisions/<key>/plan.json"
func TriggerPath(revKey string) string                  // "revisions/<key>/trigger.json"
func RevisionDocPath(revKey string) string              // "revisions/<key>/revision.json"
func ManifestPath(revKey string) string                 // "revisions/<key>/manifest.json"
func ExecutionDir(revKey, execKey string) string        // "revisions/<key>/executions/<execKey>"
func ExecutionDocPath(revKey, execKey string) string    // "revisions/<key>/executions/<execKey>/execution.json"
func SnapshotPath(revKey, execKey string) string        // "revisions/<key>/executions/<execKey>/snapshot.latest.json"
func EventPath(revKey, execKey string, seq uint64, kind string) string

func LatestRevisionRefPath() string                     // "refs/latest-revision.json"
func LatestExecutionRefPath() string                    // "refs/latest-execution.json"
func TriggerLatestRefPath(name string) string           // "refs/triggers/<name>/latest.json"
func TriggerScopeRefPath(name, scope string) string     // "refs/triggers/<name>/<scope>.json"
func NamedRefPath(name string) string                   // "refs/named/<name>.json"

func RevisionIndexPath(revKey string) string            // "indexes/revisions/<key>.json"
func ExecutionIndexPath(execKey string) string          // "indexes/executions/<execKey>.json"
```

Callers MUST go through these helpers; raw string concatenation is forbidden so
the central path policy stays enforceable.

---

## 3. Local-driver semantics

### 3.1 Write — atomic

```
1. mkdir -p dirname(translated)
2. tmp := CreateTemp(dirname, ".orun-tmp-*")
3. tmp.Write(data); tmp.Sync(); tmp.Close()
4. os.Rename(tmp.Name(), translated)
5. return ObjectMeta{Revision: sha256(data)}
```

Guarantees:

- A concurrent reader sees either the old bytes or the new bytes — never a
  partial write.
- A `kill -9` between steps 2 and 4 leaves a `.orun-tmp-*` file in place. A
  best-effort cleanup runs at store construction; orphan tempfiles older than
  1 hour are removed.
- On cross-device rename failure (rare on local FS but possible with mounts),
  the driver retries with a copy.

### 3.2 CreateIfAbsent — exclusive

```
fd, err := os.OpenFile(translated, O_WRONLY|O_CREATE|O_EXCL, 0o644)
if errors.Is(err, fs.ErrExist) { return ErrExists }
fd.Write(data); fd.Sync(); fd.Close()
```

Used by the revision writer for `revision.json` and `plan.json` so two
concurrent `orun plan` invocations cannot overwrite each other.

### 3.3 CompareAndSwap

Phase 1 uses content-derived revisions:

```
cur, meta, err := Read(ctx, path)
if err == ErrNotFound { return ErrNotFound }
if meta.Revision != oldRev { return ErrConflict }
return Write(ctx, path, data, opts)
```

There is a small race between `Read` and `Write`. For Phase 1 this is
acceptable because CAS is used only on refs and indexes, where loser-retries
are cheap and the worst case is a one-cycle stale ref (the resolver always
falls back to a scan).

A future remote driver will use the object-store's native conditional update
(R2 `If-Match`, S3 versioning) and remove the race.

### 3.4 Read / List / Delete

- `Read` returns `ErrNotFound` for missing objects; never panics; respects
  context cancellation.
- `List` walks the translated directory tree rooted at the translated prefix.
  Symlinks are not followed (Phase 1 layout doesn't introduce any).
- `Delete` removes a single file. Removing a non-empty directory is forbidden;
  use a recursive helper outside the store if ever needed (no callers do).

---

## 4. Error taxonomy

```go
var (
    ErrNotFound = errors.New("statestore: object not found")
    ErrExists   = errors.New("statestore: object already exists")
    ErrConflict = errors.New("statestore: compare-and-swap conflict")
    ErrInvalid  = errors.New("statestore: invalid path or argument")
)
```

All errors returned by the local driver wrap one of these sentinels via
`fmt.Errorf("%w: ...", ErrX, ...)` so callers can use `errors.Is`. String
sniffing is forbidden.

---

## 5. Constructor and configuration

```go
type LocalConfig struct {
    Root string // absolute path to ".orun" directory
}

func NewLocalStore(cfg LocalConfig) (*LocalStore, error)
```

- `Root` must exist OR the caller passes the workspace state root (`.orun/`)
  and `NewLocalStore` creates it on first use.
- `NewLocalStore` runs orphan-tempfile cleanup once.
- No global state. Each `LocalStore` is independent; tests construct their own.

---

## 6. Atomicity contract summary

| Operation | Atomicity | Failure mode |
|-----------|-----------|--------------|
| `Write` | Single object atomic via temp + rename | Pre-rename crash leaves orphan tempfile; post-rename success guarantees full visibility. |
| `CreateIfAbsent` | Strong exclusive create via `O_EXCL` | Loser sees `ErrExists`. |
| `CompareAndSwap` | Best-effort on local; native on remote (future) | Loser retries; resolver scan covers stale refs. |
| Multi-object compound writes (e.g. revision + ref) | **Not transactional.** Callers order writes so that the body lands before the ref; ref scan is the consistency fallback. |

---

## 7. What does NOT belong in `StateStore`

- Domain logic (revision key generation, trigger resolution, manifest
  composition). These live in `internal/revision`, `internal/triggerctx`,
  `internal/executionstate`.
- Caching. Phase 1 has no in-process cache; each operation hits the FS.
- Compression. Reserved for Phase 2 if the remote driver opts in.
- Encryption. Out of scope; future SaaS phases will layer this above the
  interface.

---

## 8. Testing requirements

The driver is the foundation. Coverage targets:

- ≥ 95 % statement coverage of `internal/statestore`.
- Atomicity test: 100 goroutines repeatedly `Write` and `Read` the same path;
  the reader must see complete JSON every time.
- Exclusivity test: 100 goroutines call `CreateIfAbsent`; exactly one succeeds.
- CAS conflict test: two concurrent CAS calls, one wins, one returns
  `ErrConflict`.
- Property-based test (`rapid`): for arbitrary path components in the allowed
  alphabet, round-trip Write→Read returns the input bytes.

See `test-plan.md` for the full harness.
