# orun-bootstrap-engine — Design

Status: Normative for BE-O. The baseline's half is cirrus
`specs/epics/saas-bootstrap-engine/`; the console's is orun-cloud
`specs/epics/saas-bootstrap-engine/`.

## §1 — The schema delta

```go
type Hook struct {
    ID   string
    Run  []string          // argv escape — unchanged
    Uses string            // NEW: "<namespace>/<verb>@<version>"
    With map[string]any    // NEW: typed, validated at parse time
    // Workflow string     // DELETED by BE-O8
    // With/Connections move to the action level
}

type Phase struct {
    Name, Description string
    Modules  []string
    Title    string              // NEW: what a human calls it
    When     string              // NEW: CEL over inputs
    Retry    *RetrySpec          // NEW
    Requires *PhaseRequires      // NEW: {Phases []string, Probe []Hook}
    Narrate  *Narration          // NEW: {Start, Await, Done, Failed string}
    Hooks    PhaseHooks          // CHANGED: {Pre, Post, Await []Hook}
    ExpectedMinutes int          // NEW
}
```

`Phases` already imposes the barrier and already forbids a `dependsOn` edge
pointing forward across it. That property is what lets a baseline delete its
phase-splitting script: the split exists only to prune edges that the barrier
makes illegal anyway.

## §2 — The action registry

A closed, versioned set compiled into the binary. Each action declares a typed
parameter struct, a typed output struct and a typed error set; `with:` is
validated against the struct at blueprint parse time.

| Action | Replaces (in a baseline's `flows/`) |
|---|---|
| `orun.doctor/check@v1` | `preflight.sh` |
| `orun.task/ensure@v1` · `orun.task/rollup@v1` | `track.sh` |
| `orun.integrations/reconcile@v1` | `create-secrets.sh` |
| `orun.pr/land@v1` | `land-pr.sh`, `push-main.sh` |
| `orun.run/watch@v1` | `converge.sh` + `ghrest.sh` |
| `orun.http/probe@v1` | `verify-endpoints.sh` |
| `orun.repo/ensure@v1` | phase 01's `gh repo create` |
| `orun.secrets/exists@v1` · `orun.cloudflare/subdomain@v1` | new — `requires.probe` and input prefill |

Three notes on specific actions:

- **`run/watch` is the one that gets *better* by moving.** A baseline today
  polls GitHub's Actions API to ask about an orun execution, then calls
  `orun run --retry`. The binary knows which lanes are its own, which are
  retriable and which memoized jobs stay done. "Transient vs. regression"
  becomes a judgement it can actually make.
- **`integrations/reconcile` removes a duplication, not just lines.** A
  baseline's manifest already transcribes `create-secrets.sh`'s table with a
  conformance test holding the copies in step. Make the manifest the input and
  the transcription, the test and the drift go together.
- **`pr/land` needs nothing new.** The pen (`orun pr open --task`) exists; what
  is missing is the tail — wait for checks (passing when the repo has none
  yet), merge with admin bypass, return to a pulled main, degrade to untracked
  on a refused pen.

`run:` remains for genuine ecosystem escapes. That is invariant 8 held exactly:
an **action** is orun's own surface; **argv** is how a blueprint reaches `node`.

## §3 — The phase lifecycle

```
for phase in phases where When:
    requires.phases  → derived check, never a lock-file read
    requires.probe   → actions; a `pending` result blocks with a reason
    hooks.pre        → task creation, readiness
    place modules    → the existing engine, barrier-ordered
    hooks.post       → rebrand (run:), land
    hooks.await      → watch, probe; `pending` PARKS the phase
```

`await` is `orun-workflows-v3`'s `poll:`/`until:` relocated: expressed as a hook
*result* rather than a step verb. That is what makes a phase resumable, which
is the promise `Phase`'s doc comment already makes.

## §4 — Derivation and resume

`orun new --status` computes, from scratch, in any container:

| Question | Derivation |
|---|---|
| Placed? | re-apply — `orun new` is byte-deterministic, so an empty diff means placed |
| Landed? | git log + the task's rung |
| Converged? | the platform's run for the landing commit |
| Live? | `http/probe` against the manifest's declared URLs |

`--resume` runs everything not done; `--phase X` one phase; `--until X` through
one. Inputs are recovered from the product repo (`.rebrand/values.json`), never
retyped — and a `--set` that disagrees with the recorded `InputsHash` is
**refused**, because a product half-branded one way and half another is a
silent failure.

## §5 — The event stream

```jsonc
{ "runId": "run_…", "seq": 42, "at": "…", "phase": "05-edge", "step": "converge",
  "state": "running", "level": "info",
  "narration": "Waiting for the deploy to converge. Usually about five minutes.",
  "detail": "convergence run 18234567 (resume budget: 3)",
  "meta": { "execId": "…", "attempt": 1 } }
```

`detail` is the machine's line; `narration` is the line **the baseline
authored** in its `narrate:` block. The engine renders narration through the
same constrained funcmap as module templates (no file/exec/net) and enforces
two rules: a narration may not set state, and a missing one degrades to a
generated line — never silence, never a claim.

The *"done, summary, now next"* transition line is **composed by the engine**
from the previous phase's `narrate.done`, derived facts, and the next phase's
`narrate.start` + `expectedMinutes`. The YAML supplies the prose, the engine
supplies the numbers, neither can lie about the other.

`--progress json` emits the raw stream, which is what a baseline's end-to-end CI
asserts against — the operator-visible output becomes testable, which it cannot
be while a model generates it.

## §6 — `orun baseline`

The CLI has **no baseline verbs today**: `grep -rli baseline cmd/orun/` hits
only `tasks.go` and `agent_serve.go`, and `internal/remotestate/` has
`catalog.go`, `tasks.go`, `epics.go`, `skills.go` and no `baselines.go`. Every
registry surface is console-or-API, so the "one command" story breaks at step
one — an operator must visit the console to learn an id and a tag.

```
orun baseline list [--org ws_…] [--all]        GET /v1/baselines | …/organizations/{org}/baselines
orun baseline show  <id[@tag]> --org ws_…      GET …/agents/blueprints/{id}
orun baseline check <id>       --org ws_…      readiness only; non-zero exit = CI gate
orun baseline new   <id[@tag]> --org ws_… --repo owner/name
orun baseline status --repo owner/name         §4
orun baseline register --file …                POST …/baselines
orun baseline publish  --tag baseline-v6       the ordered two-repo handshake, as one verb
```

Every read maps onto a route that already exists.

**`new` has two modes, and the distinction is the security boundary.**
`--via-platform` (default) POSTs to the bootstrap door: the platform mints the
time-boxed admin grant, starts the runner and records `workspace_baselines`.
`--local` resolves the row, pins the blueprint at the registry's tag and runs
`orun new --resume` here, as the operator. The CLI **never** re-implements the
paid gate, the grant or the runner — `--local` does not bypass them, it does
not need them.

**`publish`** collapses the handshake that must be ordered — push the tag
*first*, or every build 404s — into one verb that verifies the tag exists,
verifies the brief, umbrella and manifest resolve at it, then opens the registry
pull request.

## §7 — The reciprocal test

When an action changes, which baselines break? A contract job runs the static
and dry-instantiation tiers against **every registered baseline's
`repo-blueprint.yaml` at its published tag**. This is the vendored-parity
discipline `orun-mcp` UM0–UM6 established for the tool manifest, applied to the
action registry — and it is what makes a closed action set safe to depend on.
