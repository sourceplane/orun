# Spec: orun-workflows-v2 (workflow actions v2 — the data-flow evolution)

**v1 made a workflow runnable. v2 makes workflows how the platform talks.**
`orun-workflows` (WF0–WF7, shipped in v2.32.0) added the third execution
vocabulary — `workflow:` — with the right architecture: a digest-pinned
reference in the plan, one backend for two surfaces, secrets in-memory, and the
load-bearing law that *a workflow is execution, never intent*. A post-ship
product review found that architecture sound and the product around it
incomplete in five specific, verified ways: the wire contract has **no real
counterparty** (orun invokes an engine mode torkflow does not implement, with a
credential shape torkflow does not read); credentials cross the boundary as an
**unscoped blob** in an **unmapped namespace**; a workflow's structured output
**dies at the step boundary** — re-creating the exact `curl | jq` problem the
feature was built to kill; the engine binary is **ambient host state** rather
than plan-pinned; and workflows are **repo-local files** while the golden paths
that reference them travel as versioned OCI artifacts. v2 closes these in order
of leverage: **truth → flow → scope → portability → durability → people.**

> **The defining law is unchanged and non-negotiable: only names are intent;
> values are execution.** v2 extends the v1 carve-out to data-flow: a workflow's
> declared **output names** and **connection names** are compile-time,
> digest-covered intent — the plan can validate every `steps.X.outputs.Y`
> reference and every credential grant before anything runs. The output
> *values*, like the run itself, remain sealed run facts. This is how v2 adds
> typed data-flow and least-privilege credentials without surrendering the
> byte-identical plan.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (v1) — for review** |
| Evolves | `specs/orun-workflows` (WF0–WF7, shipped v2.32.0). v1's invariants 1–8 carry forward verbatim; nothing here weakens them |
| Grounded in | the post-ship product review (2026-07-15), each gap **verified in code**: torkflow's CLI has `run`/`view` but no `backend` mode; orun sends `credentials` (plural, env-var-keyed) where torkflow's contract reads `credential` + `connections`; no engine digest is materialized into `plan.json` (spec §5 drift); `StepSpec.RunDir` is dead code; the sealed step log dumps the **entire** final context |
| Builds on | `internal/workflowbackend` (the one backend), the `workflow:`/`with` step + hook fields, `Provenance.Hooks`, the plan checksum, `orun-secrets` resolution + redaction, `internal/composition` OCI/lock machinery (for engine + workflow distribution), torkflow's file-backed run store (for resume) |
| Contract decision (locked) | **`contract/v1`** — a versioned, vendored wire contract (JSON Schema + golden fixtures) owned jointly: the same fixture files are committed to both repos and both CIs run conformance against them. orun sends `{contract, workflow, with, connections, metadata, runDir}`; the engine returns `{contract, status, outputs, steps[], error}`. Field-shape drift becomes a failing test, not a 2 a.m. incident |
| Decisions locked | credentials cross the boundary **only** through a declared `connections:` mapping (workflow connection name → `secret://` ref), compile-checked against the connections the workflow file declares — unmapped secrets never cross (least privilege); workflows declare **`spec.outputs`** (name → expression) and orun seals **only** declared outputs (allowlist, not firehose); later steps consume `{{ steps.<id>.outputs.<name> }}`, validated at compile time against declared names; the engine is a **plan-pinned artifact** (digest in `plan.json`, OCI-resolvable) with `ORUN_TORKFLOW_ENGINE` demoted to a dev override; `retry` gains `resume: true` (resume-from-failed-step via the engine's run store) — re-run stays the default; approval gates are a **pause sealed as a run fact**, never plan content |
| apiVersion | `orun.io/v1` (step/hook `connections`, `outputs` consumption); `torkflow/v1` gains `spec.outputs` + the `backend` mode (torkflow-side); wire: `contract/v1` |
| Milestone prefix | **WX** (`WX0 → WX7`) |

## The one-paragraph thesis

The strategic bet of v1 was drawing the determinism line before adding a runtime
engine — the thing GHA and Datadog-class workflow products can never retrofit.
v2 collects on that bet. Because the plan already pins a workflow by content
digest, the *names* inside that file — its connections, its outputs — are
already deterministic plan inputs; v2 simply starts reading them. That one move
yields the three headline capabilities at once: a **compile-checked credential
grant** (the plan proves which secrets each workflow may receive, and nothing
else crosses), **typed cross-step data-flow** (`{{ steps.get-oncall.outputs.email }}`
fails at plan time if the workflow doesn't declare `email`), and an **allowlist
seal** (the run record carries declared outputs, not an unbounded context dump).
Underneath, the wire contract stops being fiction: a vendored `contract/v1`
tested from both sides, an engine the plan actually pins, and a `backend` mode
torkflow actually implements. Above, workflows become **portable artifacts**
that ride in the same OCI Stacks as the golden paths that call them, runs become
**resumable** at the failed step instead of re-executed, and a workflow can
**pause for a human** — the pause and the approval both sealed as run facts.
Same law, grown up: *only names are intent; values are execution.*

## The flow (what v2 adds, end to end)

```
   torkflow/v1 workflow file                       composition Stack (OCI)
   spec.connections: [github-app]        ◄──── workflows ride in the same
   spec.outputs: { email: "{{ … }}" }          artifact, same lock (WX5)
              │  names = digest-covered INTENT
              ▼
   orun plan ──── compile-time proofs ────────────────────────────────
     • step.connections maps EVERY declared connection → secret://ref (WX2)
     • every {{ steps.X.outputs.Y }} resolves to a declared name     (WX4)
     • engine digest pinned into plan.json                           (WX3)
              │
              ▼
   orun run ──── contract/v1 (vendored, conformance-tested both repos, WX0/WX1)
     request:  { contract, workflow, with, connections, metadata, runDir }
     engine:   torkflow backend  ◄── real counterparty, per-step credential
     response: { contract, status, outputs, steps[], error }         fan-out
              │
              ▼
   sealed into .orun/ ──── VALUES = run facts
     • declared outputs only (allowlist, redacted)                   (WX4)
     • structured step timeline → cockpit renders nested substeps    (WX4)
     • resume-from-failed-step on retry { resume: true }             (WX6)
     • approval pause/decision sealed as run facts                   (WX7)
```

## Read order

1. **`design.md`** — the verified problem (§1), the contract (§3), the
   connections grant (§4), outputs & data-flow (§5), the pinned engine (§6),
   portability (§7), resume (§8), approval gates (§9), the extended law (§10),
   invariants (§11), and the sharpness register (§12).
2. **`implementation-plan.md`** — milestones **WX0 → WX7**.

## Pillar table — leverage order

| # | Pillar | What changes | Why this order |
|---|--------|--------------|----------------|
| 1 | **Truth** (WX0–WX1) | vendored `contract/v1` + conformance in both CIs; torkflow `backend` mode with per-step credential fan-out | everything else stacks on a contract that actually executes |
| 2 | **Scope** (WX2) | `connections:` grant — mapped-only, compile-checked, least-privilege | security model before more data crosses the boundary |
| 3 | **Pinning** (WX3) | engine digest in `plan.json` + OCI resolution; `RunDir` wired | closes the v1 spec drift; "which engine ran this" becomes plan content |
| 4 | **Flow** (WX4) | `spec.outputs` + `{{ steps.X.outputs.Y }}` + allowlist seal + structured substeps | the founding use case, finally true across the step boundary |
| 5 | **Portability** (WX5) | workflows in composition Stacks; digest covers action-store manifests | golden paths that reference workflows must carry them |
| 6 | **Durability** (WX6) | `retry: { resume: true }` via the engine's run store | don't re-post the Slack message because the PR call flaked |
| 7 | **People** (WX7) | approval gates (pause/decision as run facts) + docs IA rename | the most-loved capability in this product class, last because it rides on all of the above |

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| the `contract/v1` schema + golden fixtures (vendored in both repos) and both conformance harnesses; the torkflow `backend` mode's **contract** and orun-side integration (implementation details of the engine's internals stay torkflow's); the `connections:` step/hook field, its compile-time grant check, and mapped-only injection; engine digest in `plan.json` + OCI engine resolution; `spec.outputs` consumption, cross-step templating, allowlist sealing, structured substep records + cockpit nesting; workflow packaging in composition Stacks + the action-store-manifest digest fold; `retry.resume`; approval gates as run facts; renaming the docs "Workflows" examples category | new torkflow **providers** (github/slack/… remain torkflow-side work); the `orun-secrets` store/policy engine itself; per-module scaffolding hooks (`postModule` — still deferred, unchanged from v1 §9); an in-process Go import of the engine (still a follow-on); event-driven workflow **triggers** ("on incident, run workflow" — a future epic; v2 keeps workflows bound to plan steps, hooks, and the authoring CLI); a visual workflow builder |

## Out-of-band references

- **Predecessor:** `specs/orun-workflows` (v1) — its invariants 1–8 and
  `IMPLEMENTATION-STATUS.md` (whose "follow-ons" section WX3/WX5 absorb).
- **The product review** (2026-07-15) — the verified gap list this epic is
  grounded in; each P0/P1/P2 maps to a pillar above.
- **Sibling runtime:** `github.com/sourceplane/torkflow` — the `backend` mode
  (WX1) and `spec.outputs` (WX4) land there against the shared fixtures; its
  file-backed run store (`internal/state`) is the substrate for resume (WX6).
- **orun capabilities reused:** `internal/workflowbackend` (extended, not
  replaced); `internal/composition` OCI fetch + lock files (engine + workflow
  distribution); the plan checksum (new fields fold in automatically); the
  cockpit view-model (nested substeps); `orun-secrets` redaction (output
  sealing).
