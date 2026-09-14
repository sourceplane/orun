# Spec: orun-bootstrap-engine (BE — the binary's leg)

**The Blueprint already has the shape; it is missing the verbs, the wait and
the words.** `internal/scaffold/blueprint.go` carries `Phases` with per-phase
`Hooks` and placement barriers, `CycleBreak`, `Ignore`, provenance and
`upgrade.go`. Its own doc comment names what is left:

> *"Approval gates + resumable pausing are a planned follow-on; this overlay
> adds the grouping, the barrier, and the hook attachment point."*

BE is that follow-on, plus two things the follow-on implies: **typed orun
actions** so a hook is a validated call rather than an argv, and a **build
event stream** so a bootstrap can report itself without a model paraphrasing
it. With those, a baseline's `flows/` — 4,971 lines in cirrus — is deleted and
`kind: Workflow` leaves the bootstrap path entirely.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (not started)** |
| Cluster | **BE** — **BE-O1–BE-O8** here · **BE1–BE6** [cirrus](https://github.com/sourceplane/cirrus/tree/main/specs/epics/saas-bootstrap-engine) (landed: sourceplane/cirrus#45) · **BE-K1–BE-K4** [orun-cloud](https://github.com/sourceplane/orun-cloud/tree/main/specs/epics/saas-bootstrap-engine) (landed: sourceplane/orun-cloud#1534) |
| Owner(s) | `internal/scaffold` (the schema, the engine, hooks, provenance) · a new `internal/actions` (the registry) · `internal/remotestate` (a new `baselines.go`) · `cmd/orun/{new,baseline}.go` · `internal/flow` (removed from the scaffold path by BE-O8) |
| Builds on | `orun-scaffolding` SCF0–SCF7 (the Blueprint engine, the DAG, the object store, provenance, upgrade) · `orun-workflows-v3` (whose `poll:`/`until:` primitive BE **relocates** into the phase engine rather than consuming) · `orun-baseline-tracking` BT-O1–BT-O4 (`orun task epic/milestone/create`, the pen) · `orun-mcp` UM0–UM6 (the vendored-manifest parity pattern, reused for the manifest and the action registry) |
| Decisions locked | (1) **Typed actions, not argv.** `uses: <id>@<v>` + `with:` validated at parse time; `run:` survives only as the ecosystem escape. (2) **Phase state is derived; a stored file is a cache and must be safe to delete.** (3) **Narration is rendered from a baseline's declaration, never generated.** (4) **`Hook.Workflow` is deleted**, and with it `kind: Workflow` from the bootstrap path. (5) **The closed action set is the audit boundary** — hooks already run outside the template sandbox. |

## Why typed actions rather than `run: ["orun", …]`

Five differences, all load-bearing:

| | `run:` argv | `uses:` action |
|---|---|---|
| A bad parameter | exit 2, mid-bootstrap, in a customer's repo | validation error naming the line, before placement |
| `orun` on `PATH` | required | not required — in-process call into the same package the CLI verb uses |
| Outputs | none — `hookRunner` sends stdout to stderr and discards it | typed and addressable: `{{ .phase.hooks.land.outputs.execId }}` |
| Failure | an exit code | a typed error a surface can render |
| Audit | arbitrary argv, run **outside the sandbox** | a closed set |

## Milestones

| ID | What |
|----|------|
| **BE-O1** | `Hook.Uses`/`With` + `internal/actions` with two actions: `orun.pr/land@v1`, `orun.run/watch@v1`. `Hook.validate()` becomes XOR over `run`/`uses`/`workflow`. |
| **BE-O2** | Derivation: `orun new --status` / `--phase` / `--until` / `--resume`, computed from the product repo + task plane + probes. **No state file.** |
| **BE-O3** | `Phase.requires.{phases,probe}`, `Phase.when` (CEL), `Phase.retry`, and the per-product lease. |
| **BE-O4** | `hooks.{pre,post,await}`; an action may return `pending`, which parks the phase. `.orun/run.state` as a **pure cache** — safe to delete, never read as truth. |
| **BE-O5** | The remaining actions: `doctor/check`, `task/ensure`, `task/rollup`, `integrations/reconcile`, `http/probe`, `repo/ensure`, `secrets/exists`, `cloudflare/subdomain`. |
| **BE-O6** | The build event stream: `{runId, seq, phase, step, state, narration, detail, meta}`; `narrate:` templating with the constrained funcmap; `--progress auto\|plain\|verbose\|json`; publish to orunbase. |
| **BE-O7** | `orun baseline list\|show\|check\|new\|status\|register\|publish` + `internal/remotestate/baselines.go`. `new` has `--via-platform` (default) and `--local`. |
| **BE-O8** | Delete `Hook.Workflow`, `ProvHook.Workflow` and `internal/flow` from the scaffold path. The `Connections` grant moves to the action level. |

**Ordering is a safety property, not a preference.** BE-O8 must not land before
BE-O5, or a baseline has no way to express a bootstrap in between. BE-O2 must
land before BE-O4, so the cache can never quietly become the source of truth.

## The derivation rule, and why it is locked

`.orun/*` is gitignored in cirrus and in every product it scaffolds (phase 01
writes `.orun/` into the new repo's `.gitignore`), so the per-phase provenance
the flows archive has **never** survived a container. The paced bootstrap —
*"run one phase today and the next whenever"*, from a fresh container with two
env tokens — works today **because** nothing is stored.

| Layer | Where | Survives a new session | Answers |
|---|---|---|---|
| product repo (git) | the product | yes | which modules are placed; `.rebrand/values.json` — the inputs |
| task plane | platform | yes | rungs and derived verdicts |
| provider | live | yes | is it actually deployed |
| `.orun/run.state` | container | **no** | which exec id is being watched *right now* |

Two consequences: **inputs on resume are a conflict, not an override** (pin on
the recorded `InputsHash`; re-inputting is `upgrade`'s operation), and a
**lease** is required because sessions are plural.
