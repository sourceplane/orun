# orun-native-coordination — Implementation Plan (NC0–NC4)

Status: **Ready for implementation** — client decisions locked
(`risks-and-open-questions.md`); contract vendored + checksum-guarded.
Milestones pair with the platform's **BM** cluster; each NC milestone verifies
against the BM milestone that serves it on stage. The spine is **NC0 → NC2 →
NC3**; NC1 (results) can land alongside NC2; NC4 (OIDC golden path) trails.

## NC0 — Vendor contract + shared fold — 🗓️ Planned (pairs BM0)

- Vendor `coordination-api.md` into `specs/orun-native-coordination/vendored/` +
  `CHECKSUM` + a `TestVendoredCoordinationChecksum` drift guard (mirror of
  `internal/remotestate/contract_vendor_test.go`).
- Port the shared **fold** (`events → run/jobs/frontier`) into
  `internal/statebackend` as a pure, table-tested function kept identical to the
  platform's `reduce()` (golden vectors shared across repos).

**Done when:** `go test ./...` green; the vendored copy matches the checksum; the
fold passes the shared golden vectors; no transport change yet.

## NC1 — Result plane + cache-aware claim — 🗓️ Planned (pairs BM1)

- Compute `jobInputHash` for `hermetic` jobs (contract D2); push `job-result` and
  `log` objects via the existing object-model digest negotiation.
- Handle `:claim` `cached:true` → adopt the memoized result, skip execution, show
  it distinctly in the cockpit; `--no-cache` bypasses.

**Done when:** a hermetic job with a prior result is skipped on stage and the
cockpit marks it memoized; a non-hermetic job is never skipped; `--no-cache`
forces execution.

## NC2 — Event-log client — 🗓️ Planned (pairs BM2)

- Reshape `statebackend.Backend` to `Claim/Heartbeat/Complete` (conditional-append
  semantics + the result mapping) and `ReadLog(from)`.
- The runner loop schedules from the local fold, claims via the server gate, waits
  on the log for `deps_not_ready`, and stops on `lease_lost`.
- `internal/remotestate` speaks the §3 verbs + `…/events` primitive; idempotency
  by `(jobID, kind, leaseEpoch)`.

**Done when:** a full DAG runs end-to-end on stage (deps gating, heartbeat,
complete-with-result, takeover), with exactly-one-runner-per-job under a
two-runner race; `lease_lost` halts the losing runner cleanly.

## NC3 — Read-the-log UX + offline log — 🗓️ Planned (pairs BM3)

- `bridge.Source` folds the run stream; `status`/cockpit live-tail active runs via
  SSE/long-poll, read the projection snapshot for finished runs.
- `logs --follow` tails `LogChunk`/sealed `log` objects.
- Offline: a local event log in `.orun/`; cloud sync ships/pulls appends and
  re-folds; `--local` escape hatch.

**Done when:** cloud `status --watch` and `logs --follow` reach parity with local
on stage; an offline run then synced to cloud reconciles with no double-apply; a
network blip mid-run recovers on retry.

## NC4 — CI OIDC golden path + conformance — 🗓️ Planned (pairs BM5)

- GitHub Actions OIDC exchange (`OIDCTokenSource`, audience `orun-cloud`) as the
  default CI auth on the new surface.
- Conformance suite (claim race, takeover, memoized skip, log tail, deps gating)
  run vs stage in CI.

**Done when:** a stock GHA workflow coordinates a multi-job run via OIDC on stage
with no static token; the conformance suite is green vs stage.

## Sequencing note

NC0 is the strict prerequisite (no client work before the contract + fold are
vendored/ported). NC2 is the heart and needs BM2 live on stage. NC1 is
independent of NC2's claim path and can land in parallel. NC3 polishes the
surface NC2 enables; NC4 trails BM5. All of NC is human-independent except the
stage gates, which depend on the paired BM milestones being deployed.
