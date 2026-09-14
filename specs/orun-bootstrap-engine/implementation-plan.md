# orun-bootstrap-engine — Implementation Plan

Status: Normative for BE-O1–BE-O8. Model in [`design.md`](./design.md).
Cross-repo: **BE1–BE6** cirrus, **BE-K1–BE-K4** orun-cloud.

Two ordering rules hold, and both are safety properties rather than preferences:

> **BE-O8 must not land before BE-O5.** Deleting `Hook.Workflow` before the
> full action set exists leaves a baseline with no way to express a bootstrap.
>
> **BE-O2 must land before BE-O4.** Derivation ships before any cache exists,
> so the cache can never quietly become the source of truth.

## BE-O1 — Typed actions

`Hook.Uses` + `Hook.With`; `internal/actions` with a registry keyed
`<namespace>/<verb>@<version>`; each action declares typed params, outputs and
errors. `Hook.validate()` becomes XOR over `run`/`uses`/`workflow`. Parse-time
validation of `with:` against the param struct, with a line number. Hook outputs
become addressable in later hooks of the same phase.

Ship **two** actions — `orun.pr/land@v1` and `orun.run/watch@v1` — because they
are the highest-value pair and neither needs anything new: the pen exists, and
`orun run --retry` exists.

**Done when** a blueprint declaring `uses: orun.pr/land@v1` with a misspelled
parameter fails `orun validate` with the offending line, and a phase's `land`
output is readable by its `converge` hook.

## BE-O2 — Derivation (`--status`, `--phase`, `--until`, `--resume`)

Compute phase state from the product repo, the task plane and probes; no state
file. `--status` prints, `--status --json` emits. Inputs recovered from the
product tree; a `--set` disagreeing with the recorded `InputsHash` is refused
with both values named.

**Done when** `orun new --status` against a product repo in a fresh container,
with no local artifacts, reports exactly the phases that are done.

## BE-O3 — `requires`, `when`, `retry`, the lease

`Phase.Requires{Phases, Probe}`; `Phase.When` as CEL over inputs;
`Phase.Retry{Attempts, Backoff}`; a per-(workspace, product repo) lease with a
TTL and a holder, so a second runner reports who holds it and since when.

**Done when** `--phase 04-workers` refuses on a product whose phase 03 never
published its wiring secrets, and names which probe failed.

## BE-O4 — `await` hooks and the cache

`hooks.{pre,post,await}`; an action may return `pending{reason, retryAfter}`,
which parks the phase rather than failing it. `.orun/run.state` holds only the
in-flight exec id and attempt count — gitignored, and **deleting it must change
nothing but a re-derivation**, asserted by a test.

## BE-O5 — The remaining actions

`doctor/check`, `task/ensure`, `task/rollup`, `integrations/reconcile`,
`http/probe`, `repo/ensure`, `secrets/exists`, `cloudflare/subdomain`.
`task/ensure` is find-or-create by identity — the semantics a baseline's
`track.sh` hand-rolls over `orun task create`. `integrations/reconcile` takes
the baseline manifest as its desired state.

**Done when** cirrus's `flows/common/` has no script whose behaviour is not
reachable through an action, verified by porting its contract tests here.

## BE-O6 — The event stream

The event type; `narrate:` parsing and rendering through the constrained
funcmap; the two enforcement rules (narration may not set state; a missing one
degrades to a generated line). `--progress auto|plain|verbose|json`, and
`orun baseline logs --follow`. Publish batches to orunbase under the run's
credential, monotonic `seq`.

**Done when** a bootstrap's operator-visible output is byte-identical across two
runs of the same blueprint at the same tag.

## BE-O7 — `orun baseline`

`internal/remotestate/baselines.go` + `cmd/orun/baseline.go`. `list`, `show`,
`check`, `status`, `register`, `publish`, and `new` with `--via-platform`
(default) and `--local`. The CLI never re-implements the paid gate, the grant
or the runner.

**Done when** an operator can go from "I have a workspace" to a running
bootstrap without opening the console, and `publish` makes the tag-before-row
ordering unfailable.

## BE-O8 — Delete the workflow hook

Remove `Hook.Workflow`, `ProvHook.Workflow`, `hookRunner.runWorkflow` and the
`internal/flow` dependency from `internal/scaffold`. The `Connections` grant
moves to the action level, where a typed action declares which credential
fields it wants — a narrower blast radius than a workflow that could ask for
anything. `orun workflow` itself is untouched; it simply is no longer part of
instantiation.

**Done when** `internal/scaffold` does not import `internal/flow`, and a
blueprint with `workflow:` in a hook fails to parse with a message pointing at
the action registry.

## Cross-repo edges

| Milestone | Unblocks |
|---|---|
| BE-O1 | cirrus BE1, orun-cloud BE-K1 |
| BE-O2 | cirrus BE1 |
| BE-O5 | cirrus BE4 |
| BE-O6 | cirrus BE2, orun-cloud BE-K2 |
| BE-O7 | cirrus BE6, orun-cloud BE-K4 |
| BE-O8 | cirrus BE4 |
