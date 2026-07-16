# Implementation Plan — workflow actions v2

> Milestone-based. Each states **goal**, **deps**, and **done when**. Order is
> the leverage order from the README: truth (WX0–WX1) before scope (WX2) before
> pinning (WX3) before flow (WX4) — because a grant check and an outputs model
> are only worth building against a contract that actually executes. WX1 and the
> torkflow half of WX4 land in the torkflow repo against the shared fixtures;
> everything else is orun-side.

```
   WX0 contract/v1 — vendored schema + golden fixtures + conformance harness (both repos)
                 │
                 ▼
   WX1 torkflow `backend` mode — implements contract/v1, per-step credential fan-out   ◄─ first REAL end-to-end run
                 │
                 ▼
   WX2 connections: grant — compile-checked mapping, mapped-only injection
                 │
                 ▼
   WX3 engine as plan content — digest in plan.json/lock, OCI resolution, runDir real
                 │
                 ▼
   WX4 outputs — spec.outputs (torkflow) · steps.X.outputs.Y (orun) · allowlist seal · nested cockpit
                 │
                 ▼
   WX5 portability — workflows in composition Stacks; digest folds action-store manifests
                 │
                 ▼
   WX6 resume — retry { resume: true } over the engine's run store
                 │
                 ▼
   WX7 approval gates + docs IA + end-to-end conformance proof
```

---

## WX0 — `contract/v1`: the vendored wire contract
**Goal:** one contract, two signatures — the schema and golden fixtures both
repos test against.
- Author `contract/v1`: a JSON Schema for Request/Response (design §3) plus
  golden fixtures (success, workflow-failure, paused, unknown-contract,
  malformed). Vendor identical copies at `internal/workflowbackend/contract/v1/`
  (orun) and the mirrored path in torkflow. Add orun's conformance harness:
  every fixture round-trips through the marshal/unmarshal layer; the harness
  fails on any local edit that diverges from the fixture bytes (a checksum over
  the vendored dir). Rework `workflowbackend.Request` to the v1 shape —
  `connections` (keyed by connection name) replaces `credentials`; add
  `contract: "v1"`; `Result` gains `outputs` and drops the raw context field.

**Deps:** none. **Done when:** both repos carry byte-identical fixtures; orun's
conformance suite is green; an engine answering with an unknown `contract` or a
v0 shape produces a clear versioned error; `credentials` no longer appears in
the wire types. **Design:** §3.

## WX1 — torkflow `backend` mode (torkflow repo)
**Goal:** the contract gets a real counterparty — the first true end-to-end run.
- In torkflow: add the `backend` subcommand — read a v1 Request on stdin, run
  the engine with `with` as Trigger, fan each injected `connections[name]`
  payload to steps referencing `connection: name` (bypassing the file
  registry/secret store), evaluate `spec.outputs` (stub returning `{}` until
  WX4 lands the field), write a v1 Response on stdout. Vendor the same fixtures;
  run the same conformance suite in torkflow CI.
- orun-side: an integration test (build-tagged) that runs a real workflow
  through a real `torkflow backend` binary when present in CI.

**Deps:** WX0. **Done when:** `orun run` executes a `workflow:` step through an
actual torkflow binary end to end — authenticated action included — with no fake
engine involved; both CIs run conformance against the shared fixtures; torkflow's
interactive `run`/`view` are unchanged. **Design:** §3.

## WX2 — the `connections:` grant
**Goal:** least-privilege credentials, proven at compile time.
- Add `Connections map[string]string` (connection name → `secret://` ref) to the
  step and hook models. At compile/scaffold time, parse the pinned workflow
  file's declared connections (tolerant reader over `spec.steps[].connection`);
  enforce: every referenced connection mapped, no mapping to an undeclared
  connection. Materialize the mapping (names + refs only) into `plan.json` /
  `provenance.lock`; it folds into the checksum. At run time resolve exactly
  the mapped refs and inject keyed by connection name — delete the
  inject-everything path. The compile error for an unmapped connection prints a
  ready-to-paste `connections:` block (S-8).

**Deps:** WX1. **Done when:** a workflow step compiles only with a complete,
declared-only grant; the plan diff shows the grant; the engine receives exactly
the mapped payloads (asserted via the WX1 integration test); the job's wider
SecretEnv provably does not cross (a canary secret test); hooks behave
identically. **Design:** §4.

## WX3 — the engine is plan content
**Goal:** close the v1 pinning drift — "which engine ran this" becomes plan
content.
- Materialize the resolved engine digest into `plan.json` (a
  `workflowEngine` entry beside `compositionSources`) and into
  `provenance.lock`; fold into the checksum. Re-hash the engine at run time;
  mismatch is fail-closed. Add OCI engine resolution through
  `internal/composition` fetch + lock, with `ORUN_TORKFLOW_ENGINE` demoted to a
  documented dev override that still records its digest. Wire `runDir`: a
  per-step scratch dir under the run tree, passed in the request, treated as
  scratch (never the durable record).

**Deps:** WX1. **Done when:** two plans over identical inputs and engines are
byte-identical and a swapped engine flips the checksum; a tampered engine binary
fails pre-flight; the engine resolves from an OCI ref in CI; `runDir` arrives
populated in the WX1 integration test. **Design:** §6.

## WX4 — outputs: data-flow across the step boundary
**Goal:** the founding use case — structured output consumable by the next step.
- torkflow-side: `spec.outputs` (name → expression) evaluated at run end;
  `backend` returns them in the Response.
- orun-side: parse declared output names from the pinned file at compile time;
  validate every `{{ steps.<id>.outputs.<name> }}` in later steps of the same
  job (undeclared name / cross-job reference = compile error, S-4). At run
  time, inject returned outputs into subsequent steps' template/env context;
  seal **only** declared outputs (redacted) plus the structured `steps[]`
  timeline — delete the full-context dump from the sealed log. Cockpit renders
  the timeline as nested substeps with status + duration.

**Deps:** WX1 (wire), WX2 (grant precedent for compile-time file reading).
**Done when:** the two-step oncall example (`get-oncall` → `page`) runs end to
end with the email flowing between steps; an undeclared output name fails
compilation; the sealed record contains declared outputs and the timeline and
**not** the raw context (asserted); a secret-valued output arrives redacted;
`orun logs`/TUI show nested substeps. **Design:** §5.

## WX5 — portability: workflows travel with golden paths
**Goal:** a composition that calls a workflow can ship it.
- Let composition packages include workflow files; resolve them through the
  existing Stack fetch + lock so a packaged workflow pins identically to a
  local one (one digest function over resolved bytes). Widen `WorkflowDigest`
  to fold in the action-store module manifests the workflow references (a
  provider contract change flips the digest).

**Deps:** WX3. **Done when:** a fixture Stack ships a composition + its
workflow; a consuming repo with no local copy plans and runs it; the plan pin is
byte-identical local-vs-packaged; editing a referenced action manifest flips the
digest and fails the stale plan. **Design:** §7.

## WX6 — resume-from-failed-step
**Goal:** durability without re-execution.
- `retry: { attempts: N, resume: true }` on workflow steps: on retry, pass the
  prior attempt's `runDir`; the engine (file-backed run store) re-executes only
  non-succeeded steps. Guard: resume only when workflow **and** engine digests
  match the failed attempt, else re-run. Seal replayed-vs-skipped per step.

**Deps:** WX3 (runDir), WX4 (structured timeline), torkflow-side resume entry
in `backend`. **Done when:** a workflow failing at step 3 of 4 resumes and
executes exactly steps 3–4 (asserted from the sealed record); a digest change
between attempts forces re-run; default behavior without `resume` is unchanged.
**Design:** §8.

## WX7 — approval gates, docs IA, end-to-end proof
**Goal:** human-in-the-loop as a run fact — and the epic proven whole.
- `approval: { prompt, timeout, onTimeout }` on workflow steps (declaration
  validated: timeout + policy mandatory). Pause before engine invocation,
  sealed as a run fact; `orun approve` + cockpit surface and resolve it; the
  verdict (who/when/what) seals. Extend the plan-hash test to approval fixtures
  (a plan is byte-identical whether later approved or rejected, S-9). Reuse the
  same pause primitive for the scaffolder's planned phase gates.
- Docs: rename the website "Workflows" examples category to "Guides"; document
  connections/outputs/resume/approvals; v2 release notes.
- Prove: one example spanning the epic — scaffold with an open-PR hook, a plan
  with an oncall workflow whose output feeds the next step, an approval on a
  promote job — run against a real engine in CI.

**Deps:** WX4 (sealed timeline), WX6. **Done when:** the spanning example is
green in CI against a real torkflow binary; a timed-out gate follows its
declared policy; plan hashes are approval-invariant; the docs category rename
ships with redirects. **Design:** §9, §10.

---

## Cross-cutting (every milestone)
- **Only names are intent** (invariant 11): every new field passes the v1 test —
  *would it differ between two runs with identical inputs?* Plan-hash
  determinism tests extend to connections, outputs grammar, engine digest, and
  approval declarations as each lands.
- **Bilateral conformance** (invariant 9): no orun milestone that touches the
  wire merges without the torkflow fixture suite green, and vice versa.
- **Mapped-only, allowlist-only** (invariants 10, 12): canary tests assert the
  unmapped secret never crosses and the undeclared context never seals.
- **Fail-closed pinning** (invariant 13): workflow, engine, and packaged-source
  digests are all re-verified at run time; mismatch never degrades to a warning.
- **v1 compatibility:** plans without `workflow:` steps are byte-unchanged
  throughout; the one breaking change (the §4 grant replacing inject-everything)
  ships with a compile error that writes the migration for you (S-8).
