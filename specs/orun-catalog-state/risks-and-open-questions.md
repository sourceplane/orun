# Risks & Open Questions

> Live register. Decisions with a stated default are settled unless re-opened via
> the proposal protocol. Risks carry likelihood/impact + mitigation. The
> sharpness register from `design.md` §10 (S-1…S-8) is the source of most of
> these.

## Decisions (settled, with defaults)

| # | Question | Default decision | Rationale |
|---|----------|------------------|-----------|
| D-1 | Incremental vs full resolve in orun | **Full resolve, always** | Confidence/correctness; the I/O-bound win of partial resolve has an O(all-components) global-merge floor and is YAGNI until full resolve is measured slow. The impact index serves the *fast* path remotely (the worker), not orun. |
| D-2 | Where the affected computation lives | **First-class in orun (`internal/affected`)**, shared by cockpit + plan + run | One definition of affected, over the catalog. A remote/edge mirror is future + **under review** (`specs/orun-affected-worker/`), not a driver of this spec. |
| D-3 | Cockpit read mutating the store | **Write-through on stale: best-effort, non-blocking, CAS-safe** | User wants the cockpit to drive freshness; content-addressing + GC make preview churn cheap. Render never depends on the write. Rejected: scratch-ref / never-write (would mean the cockpit isn't "served from state" on staleness). |
| D-4 | What of `impact/` ships now | **`ownership.json` + `fingerprints/`** | Both have a **local** consumer: orun's change-detection engine (ownership = path→component; fingerprints = the cockpit's content-aware source). Dependency edges already exist as `CatalogGraph`. Only the reverse-closure *materialization* is deferred (the engine walks the graph in-process). Every published artifact has a local consumer (design §6). |
| D-10 | Migration safety | **`plan`/`run --changed` selection MUST be unchanged; parity-gated** | Consolidation, not redesign. The old `main.go` path is removed only after the parity gate is green (CD-4, `test-plan.md` §2). |
| D-11 | The edge worker | **Under review — out of scope; no orun accommodation** | The user will review worker logic separately and then specify any orun support. This spec only notes (design §7) that the artifacts are content-addressed and remotable. |
| D-5 | Object-model catalog losslessness | **Add `Path` (CS1) — prerequisite** | The graph catalog silently drops `Path`; the parity test would surface any future drop. |
| D-6 | Bias of the affected engine | **Over-report (CD-1, MUST)** | A false-missing component is a broken `--changed` build; a false-extra only wastes work. The engine marks `structural`/`global` and escalates (`NeedsFullResolve`) rather than guessing. |
| ~~D-7~~ | ~~Retire `internal/catalogstore`~~ | **✅ DONE (`specs/orun-legacy-retirement/` Bucket 1)** | The legacy catalog/revision store — `catalogstore`, `statestore`, `revision`, `executionstate`, `catalogsync` — is deleted; every `orun catalog *` command reads the object model and the lint gate bans the imports. The CLI surface is unregressed. |
| D-8 | Refresh trigger + debounce | **Refresh on _any_ orun command (best-effort, time-bounded, non-fatal); resolve at most once per `refreshTTL` per source** | The user wants "served from state" continuously. The gate keeps the clean case free; the TTL bounds the dirty-edit case to one resolve per window (S-9). Default `refreshTTL` ≈ 1s (proposed; tune by measured resolve cost). `plan`/`catalog refresh` resolve authoritatively and bypass the debounce. |
| D-9 | Cockpit live view + cadence | **Render from a live in-memory `CatalogView`; background ticker re-runs the gate every `refreshInterval` (default ~2–5s); `ctrl+r` forces a tick** | A long-lived TUI must pick up edits and other processes' writes without keystrokes; resolving off the UI thread keeps the cockpit responsive. In-memory snapshot = live; object store = shared persistent copy the ticker converges. |

## Open questions (need a call before/within the cited milestone)

| # | Question | Options | Needed by |
|---|----------|---------|-----------|
| ~~Q-1~~ | ~~`RefreshCatalog` seam location~~ | **RESOLVED → (b)** a shared `RefreshCatalog` helper called from the universal command hook **and** the cockpit ticker (D-8/D-9). Extract its catalog-only body from `objplan.writeCatalogMemoized` (source + catalog, no revision/trigger). | — |
| ~~Q-2~~ | ~~Cockpit changed/affected view~~ | **RESOLVED → YES (in scope).** An integral philosophy: the cockpit badges `DirectlyChanged`/`Dependents` via `internal/affected` (CS6), the same engine as `plan`/`run --changed`. | — |
| Q-3 | Empty `.orun` / first-run behavior | (a) resolve-on-open + write-through (uniformly store-fed); (b) keep the intent loader as a fallback | CS4 |
| Q-4 | Dirty-tree write-through churn ceiling | propose: rely on GC + retention; revisit if preview source/catalog objects measurably bloat the store | CS4, revisit post-merge |
| Q-5 | Should `orun catalog affected` default `--head` to the working tree (dirty) or `HEAD` (clean)? | propose: working tree locally (dirty-first-class), `HEAD` in CI / when `--base` given | CS5 |
| ~~Q-6~~ | ~~component→executions join (G-1)~~ | **RESOLVED → (a) scan + filter** via `objread.PlanSummary` for v1 (no new index). An `objindex` component index is deferred and tied to D-7 (avoid a third history index). | — |
| ~~Q-7~~ | ~~cockpit run scoping (G-2)~~ | **RESOLVED → component-scoped, single environment** (`environments.md`): exactly one env (selected or `intent.yaml` default), never all-env. | — |
| Q-8 | catalog state-change **watch** vs poll (consumers G-4) | poll via the D-9 ticker now; add a catalog `watch` (like `cockpit/watch.Run` for runs) later | non-blocking; poll is v1 |
| Q-9 | Cockpit affected overlay env-filtering (env G-8) | show only `active-in-E`, or grey out inactive | CS6 (UX) |

> **Moved to the `orun-env-scoping` epic** (no longer open here): no-default
> resolution, `defaultEnvironment` placement, multi-env CI/promotion ergonomics,
> trigger single-env (former Q-9…Q-11 / env G-5/G-6/G-7/G-9/G-10). The cockpit
> ships on the existing env model (`environments.md`); these are finalized in the
> epic.

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Lossy graph catalog → cockpit shows wrong data (the `Path` class) | Med | High | CS1 `Path` fix + PG-1 parity test as a standing guard (S-6). |
| Naive `structural` detection under-reports on dep-edge edits | Med | **High** | CD-2: **any** `component.yaml` content edit is `structural`; CB-2 fixture (S-3). |
| Affected engine under-reports (false negative) | Low | **High** | CD-1 over-report MUST; the never-under-select assertion (`test-plan.md` §3) fails on any under-report (S-4). |
| **Migration silently changes `--changed` selection** | Med | **High** | D-10 parity gate (CD-4): new engine == old selection on the golden corpus; old path removed only after parity (S-2). |
| Intent-impact `watch` semantics lost in migration | Med | Med | CD-3 preserved verbatim; CB-5b/5c fixtures (S-10). |
| Cockpit write-through surprises users / races CI | Low | Med | D-3: best-effort, non-blocking, CAS-safe; render pure (S-5). |
| Refresh-on-every-command adds latency to read commands on a churning tree | Med | Med | D-8 debounce (≤1 resolve per `refreshTTL`); gate is free on a clean tree; hook is time-bounded and never blocks the command's primary work (S-9). |
| Cockpit ticker resolves too aggressively (CPU on a big repo) | Low | Med | D-9 `refreshInterval` is tunable; resolve runs off the UI thread; the gate short-circuits when the source is unchanged. |
| Two materializations coexist too long | Med | Low | Cockpit-scoped convergence now; D-7 tracks full `catalogstore` retirement; no regression to its CLI (S-7). |
| Fingerprint non-determinism across machines (macOS NFD/NFC, separators) | Med | Med | Project git's normalization for committed files; canonical-encode the overlay; P-7 cycle test (S-11). |
| Dirty-tree write-through churn fills disk | Low | Med | GC + retention; source/catalog objects tiny; unchanged component blobs dedup; Q-4 revisit. |
| One-time catalog-id change (from `Path`) confuses tooling | Low | Low | Documented in CS1; content-addressing absorbs it; memo misses once. |

## Deferred / needs-later-attention register

> Consolidated record of everything intentionally pushed out of this spec, so
> nothing is lost. Each names the trigger that should pull it back in.

| # | Item | Why deferred | Pull back in when |
|---|------|--------------|-------------------|
| **L-1** | **System-wide single-env enforcement** — remove the no-`--env`=all-env default and `--env a,b` across `orun plan`/`run`/triggers/CI; add `defaultEnvironment` schema + validation + migration | Breaking run-path change beyond the catalog/cockpit boundary; needs its own deprecation window | **Epic created: `specs/orun-env-scoping/`** (status: to be designed). This spec ships only the cockpit env selector + run on the *existing* model (`environments.md`). |
| **L-2** | `objindex` component→execution index (consumers G-1 option b) | v1 uses scan + filter; an index is only worth it at scale | scan + filter measured too slow, **and** D-7 decided (where history lives) |
| ~~**L-3**~~ | Full `internal/catalogstore` retirement (D-7) — **✅ DONE in `specs/orun-legacy-retirement/` Bucket 1** | catalogstore/statestore/revision/executionstate/catalogsync deleted; object model is the single persistence stack | — |
| **L-4** | Catalog state-change **watch/notify** (consumers G-4) | Poll (D-9 ticker) is the v1 | polling cost or latency becomes a problem |
| **L-5** | SaaS **web UI** (read-only consumer) | Out of scope to build; the read/action seam split keeps it un-precluded (`consumers.md` §7) | the web UI is designed |
| **L-6** | Drill-down **UX** review (consumers G-3) | v1 placeholder: catalog → component → job → logs; the UX will be reviewed | the cockpit UX review |
| **L-7** | Reverse-closure **materialization** + per-component-fingerprint **content diff at the edge** | The engine walks the graph in-process; fingerprints already serve the cockpit | a remote consumer needs them |
| **L-8** | The **edge worker** (`specs/orun-affected-worker/`) | Under review; not a requirement (D-11) | the worker spec is reviewed and approved |

## Explicitly deferred (not risks for this spec)

- The edge worker (`specs/orun-affected-worker/`): reverse-closure
  materialization, per-component fingerprints, the Cloudflare runtime, per-branch
  index selection, reconciliation mechanics.
- Full `internal/catalogstore` retirement (D-7).
- Remote/SaaS push of the impact index (rides the existing `objremote` closure —
  no change needed here).
- A richer cockpit catalog view (inferred languages/frameworks/owners) — additive
  on top of Pillar A.
