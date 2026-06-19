# orun-native-coordination — CLI Surface

Status: Draft. Commands/flags this spec adds or changes. The headline: **no new
top-level commands** — coordination is an internal transport change behind
`orun run`, `status`, and `logs`. The visible deltas are caching feedback and a
couple of flags.

## Changed behavior (no new verbs)

| Command | Change |
|---|---|
| `orun run --remote-state` | Coordinates via append/fold against the new contract. Cockpit shows **cached (memoized)** jobs distinctly from executed ones; takeover of a lapsed lease is shown as a re-run, not a failure. |
| `orun run` (local) | Same fold over a local event log — unchanged UX, new internal model. |
| `orun status [--remote-state]` | Folds the run stream (live tail for active runs) instead of reading row snapshots. |
| `orun logs --follow` | Tails `LogChunk` events (SSE/long-poll); reads the sealed `log` object once the job completes. |

## Flags

| Flag | On | Purpose |
|---|---|---|
| `--no-cache` | `orun run` | Disable memoization for this run — execute even hermetic jobs with a cache hit (debug / force-rebuild). |
| `--local` | `orun run` | Force the local event log even when configured for cloud (existing escape hatch; unchanged). |
| `--backend-url`, `--org`, `--project` | `orun run`/`status`/`logs` | Unchanged — scope/endpoint resolution (flag > env > intent > config). |

## Plan/intent surface

- A job may declare `hermetic: true` in its composition/plan to opt into
  memoization. Default is **off** — a job is never cache-skipped unless declared
  hermetic. (`jobInputHash` derivation is contract D2.)
- No change to `intent.yaml` tenancy keys (`execution.state.backendUrl`, `org`,
  `project`) beyond the default URL flipping at platform cutover (BM6).

## Removed

- The legacy `--remote-state` v1 client (relational `/v1/runs` dialect) is
  replaced wholesale; there is no flag to opt back into it. Older pinned CLIs keep
  working only during the platform's transient cutover drain window (BM6), not via
  a flag here.
