# Risks & Open Questions

> Live register. Decisions with a stated default are settled unless re-opened
> via the proposal protocol. Risks carry likelihood/impact + mitigation.

## Decisions (settled, with defaults)

| # | Question | Default decision | Rationale |
|---|----------|------------------|-----------|
| D-1 | Inspectability model | **Hybrid** (CAS store + materialized working view + porcelain) | Keep `cat`/`jq`-grade introspection without storing history twice. |
| D-2 | Revision dedup across triggers | **Yes** — content-addressed revisions; trigger→revision many-to-one | The core efficiency + provenance win; matches user intent. |
| D-3 | Packing | **Defer** — loose + zstd + reachability GC in v1 | zstd+dedup captures most of the win at a fraction of the complexity; revisit on profiling. |
| D-4 | Runner rewrite | **Staged behind `ORUN_OBJECT_RUNNER`**, deleted at M12 | Reversible, reviewable on real executions. |
| D-5 | Hash algorithm | **sha256**, pluggable `algo` field | Boring, proven, already emitted; blake3 a future swap. |
| D-6 | `resolvedAt` in SourceSnapshot | **Excluded** from the content record | Maximizes source dedup; the observation time lives on the ref/trigger event. |
| D-7 | Logs addressing | Content blobs; **chunk into a tree above a size threshold** | Dedup identical logs; bound single-object size. Threshold TBD (R-5). |

## Open questions (need a call before/within the cited milestone)

| # | Question | Options | Needed by |
|---|----------|---------|-----------|
| Q-1 | Should `--no-catalog` revisions be allowed to seal executions, or is a catalog edge mandatory for `run`? | (a) allow degenerate runs (emergency); (b) require catalog | M5/M7 |
| Q-2 | Retention policy defaults for GC (keep last N executions per ref scope; age cutoff) | propose N=200 sealed execs/scope, 90d age, keep all named/tagged | M9 |
| Q-3 | Working-tree GC: delete `.orun/run/<id>/` immediately after seal, or keep for fast re-inspect until GC? | propose: keep until `gc`, then prune sealed working trees | M7/M9 |
| Q-4 | Index storage: JSON files (inspectable) vs an embedded KV (bbolt) for large repos | default JSON; revisit if index file counts explode (>10k) | M8 |
| Q-5 | Log chunk threshold + chunking scheme (fixed-size vs content-defined) | propose: single blob < 1 MiB, else fixed 1 MiB chunks → tree | M7 |
| Q-6 | ULID vs object-hash as the *primary* user-facing handle for executions | keep `executionId` (ULID/CI form) as the human handle; sealed object id is internal/provenance | M7 |
| Q-7 | Remote ref CAS over S3 (no native CAS) | use conditional writes (If-None-Match / ETag) where available; document the eventual-consistency caveat | M11 |

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Building "mini-git" balloons in scope | Med | High | Hard anti-goal on packfiles/deltas/custom DB (`claude-goals.md` §7). Loose+zstd+GC only. Contract frozen at M1–M2. |
| Inspectability regression vs Phase 1/2 (`cat .orun`) | Med | Med | Hybrid: working view + porcelain landed at M6 before any read rewire (M8). |
| Live-execution crash leaves corrupt state | Med | High | Working tree is the only mutable surface; seal is atomic publish; partial objects inert+GC'd; crash-recovery test (invariant 10, M7). |
| GC races a concurrent seal and collects an in-flight closure | Low | High | Grace window (objects newer than `gracePeriod` never swept) + seal gc-fence; GC-safety property test. |
| Catalog resolver coupling breaks `plan`/`run` | Med | Med | Tolerant-strict: resolution *errors* fail only under `--strict`; validation *issues* never block. Memo cache makes the walk cheap. |
| Dedup doesn't materialize (a stray timestamp/trigger leaks into a content node) | Med | High | No-self-id + identity-purity property test (invariant 5); D-6 excludes `resolvedAt`; schema review gate. |
| Source-snapshot churn on dirty worktrees fills disk | Low | Med | dirty-hash scoped to catalog-relevant files (no churn on code edits); GC + retention; source objects are tiny. |
| Migration mis-links legacy executions to revisions | Med | Med | Checksum match only; orphan bucket `rev-migrated-unknown`; `--dry-run` mandatory by convention; idempotent re-run. |
| Remote (S3) lacks CAS → ref races across machines | Med | Med | Conditional writes where available; document caveat; SaaS ref store (KV) provides real CAS (Q-7). |
| Disk win not realized (compression/dedup misconfigured) | Low | Med | M13 disk-size assertion guards it as a test. |
| Two state systems coexist too long during staging | Med | Med | Parity matrix is the explicit gate; M12 cutover deletes legacy; grep gate forbids new `internal/state` imports. |

## Explicitly deferred to Phase-3 (not risks for this spec)

- Packfile delta compression; production R2/S3 hardening + retries/backoff;
  SaaS auth + multi-tenant isolation; Supabase/D1 index service; Durable-Object
  run coordination; distributed locking; blake3 migration.
