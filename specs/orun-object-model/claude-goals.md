# Goals for the Implementing Claude

> Operating contract for the Claude agent(s) building this spec. Written for a
> large-context model that can hold the whole spec at once and implement
> milestones efficiently. Read this before writing any code.

## 1. Prime directive

**Get the data structures right; the code follows.** This is a data-model
rewrite. Every decision is judged by whether it keeps the object graph
immutable, content-addressed, and dedup'd. When in doubt, re-read `design.md`
§5 (invariants) and `object-store.md`.

## 2. Success goals (what "done" means for the whole spec)

1. A single content-addressed object graph is the source of truth; the CLI,
   runner, and consumer seam read/write only through `objectstore` + `refstore`
   + `nodes` + `nodewriter`.
2. `internal/state`, `internal/statebackend/file`, the executionstate bridge,
   and the Phase 1/2 dual-write paths are **deleted**, with the parity matrix
   (`runner-integration.md` §4) fully green.
3. All 10 correctness invariants (`design.md` §5) are regression-tested,
   including property-based tests for content integrity, dedup, and GC safety.
4. Disk usage on the E2E corpus is **lower** than the Phase 2 layout for the
   same history (dedup + zstd), measured and recorded.
5. `orun migrate` is idempotent; `orun fsck` is green on migrated + fresh stores.
6. The consumer seam (`ModelReader`/`RunStarter`) backs both a local and a
   `file://` remote store interchangeably, with a passing push/pull E2E.

## 3. Hard constraints (non-negotiable; a PR violating these is rejected)

- **Immutability.** Never mutate a written object. There is no `Update` on the
  object store. All mutation is in `refstore` via CAS.
- **One write path.** Every object write goes through `objectstore`; every node
  write goes through `nodewriter`. No package outside `objectstore` constructs
  object paths or hashes. Raw `json.Marshal` of a record is banned — use
  `nodes.CanonicalEncode`. (Add a grep/lint gate in M0.)
- **Identity purity.** No node embeds its own id. No timestamp or trigger leaks
  into a content node that must dedup (revision, catalog, source — see
  `identity-and-keys.md`). If a test shows two "identical" inputs producing
  different ids, that is a bug in the schema, not the test.
- **Publish ordering.** Always write the full object closure before moving a
  ref. The ref move is the only publish point. (Invariant 6.)
- **Determinism.** No `time.Now()` in production paths — inject a `clock.Clock`.
  No map-iteration-order-dependent encoding — `CanonicalEncode` sorts keys. No
  randomness except ULID minting for events (and that goes through an injectable
  generator).
- **No panics in production paths.** Errors flow through `errors.Is`/`As` with
  the typed taxonomy. No string-sniffing.
- **No secrets** (tokens, emails) in logs or object bodies.
- **Staging discipline.** Do not delete legacy code before its parity row is
  green and the flag default flips (M12). Until then, both paths coexist.

## 4. How to work efficiently in this codebase

- **Reuse the resolver inputs.** `internal/sourcectx` and
  `internal/catalogresolve` already produce content-addressable values. Do not
  rewrite resolution logic — only move its *persistence* into the object model.
- **Lean on existing tests.** Phase 1/2 have extensive tables and property
  tests; port the relevant invariants rather than inventing new fixtures.
- **Small, coherent PRs per milestone slice.** Each PR cites the milestone id
  and the design sections it implements, and states which parity rows (if any)
  it closes. Keep PRs reviewable (≤ ~600 LOC of non-test diff where possible).
- **Test-first for the contract layers.** `objectstore` and `refstore` are
  frozen contracts — write the property tests (atomicity, idempotent put, CAS,
  GC safety) alongside the implementation, not after.
- **Use `MemStore` for unit tests**, `LocalStore` for integration, a temp
  `file://` `RemoteStore` for sync E2E.
- **Measure disk.** Add a benchmark/asserted size check (goal #4) so dedup
  regressions are caught.

## 5. Definition of done (per PR)

- `go build ./...` and `go test -race ./...` green.
- New/changed exported symbols have doc comments.
- Coverage gates met for the touched package (see `Makefile` target added in
  M0; thresholds in `test-plan.md`).
- The PR description maps to milestone "done when" criteria and lists closed
  parity rows.
- No invariant from `design.md` §5 is weakened (state explicitly if a PR
  touches one).

## 6. When to stop and ask

Use the proposal protocol (write under `/ai/proposals/` or open a discussion)
rather than silently deviating when:

- A schema change would break an invariant or the dedup property.
- A milestone's "done when" cannot be met without touching an out-of-scope
  area (e.g. needing a real R2 driver to finish M11).
- The parity matrix has a row with no clean native home (escalate before
  shimming).

## 7. Anti-goals (do NOT do these)

- Do not implement packfiles, delta compression, or a custom on-disk database.
  Loose objects + zstd + reachability GC only (this phase).
- Do not add a second representation of any fact "for convenience" — that is the
  exact mistake this rewrite removes.
- Do not couple `plan`/`run` runnability to catalog *validation* correctness
  (tolerant-strict: walk always, fail only on resolution errors under `--strict`).
- Do not let an index or the working view become authoritative — they are
  always rebuildable caches.
