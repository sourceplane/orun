# Design — env scoping

> **Status: the core model is LOCKED.** The single-environment invariant, the
> resolution order, the `intent.defaultEnvironment` placement, and the built-in
> `local` environment are **decided** (see §3, *Decisions — locked*). The
> remaining run-path mechanics — multi-env CI fan-out, promotion rework, `local`
> state isolation, auto-subscribe, and the "am-I-local" contract — are captured as
> **open gaps** in §5 and must be finalized before implementation. `local`-env
> **safety enforcement is intentionally deferred** (§6, decision B).

## 1. Current model (as-built — what we change)

- `intent.environments` is a `map[string]Environment`. There is **no
  `defaultEnvironment`** concept anywhere (`internal/model/intent.go`).
- The expander produces a `ComponentInstance` for **each environment × component
  pair** (`internal/expand/expander.go`).
- `--env` (`-e`) is a **filter** that accepts a **comma-separated** list
  (`cmd/orun/command_plan.go`, parsed via `parseCommaSeparated`).
- Therefore today: **no `--env` → all environments**; **`--env a,b` → multiple**.
- Trigger resolution (`trigger.ResolveActiveEnvironments`) returns `[]string` and
  can activate multiple environments; the resolved list is recorded at
  `Plan.Metadata.Trigger.ActiveEnvironments []string` (`internal/model/plan.go`,
  on `PlanTrigger` — **not** a top-level `Plan` field); `PlanJob.Environment` is
  already scalar. Promotion (`EnvironmentPromotion` /
  `internal/planner/promotion.go`) models env→env ordering, either as cross-env
  DAG edges within one multi-env plan (`Satisfy: "same-plan"`) or as cross-plan
  gates (`Satisfy: "previous-success"`).
- There is **no built-in/implicit environment** and **no TTY/CI detection**
  (`go-isatty` is only an indirect dependency; `IsTerminal` in the code refers to
  job *status*, not the terminal).

## 2. Target model (LOCKED)

### 2.1 Single-environment invariant

- Every plan / run is scoped to **exactly one** environment. No all-env, no
  multi-env plan.
- A workspace with exactly one declared environment is treated **identically** to
  a multi-env workspace — there is **no sole-env special case** (ENV-2).

### 2.2 Resolution order (LOCKED)

Exactly one environment is resolved, in this order:

1. **Explicit.** A trigger-bound environment is **authoritative**; an `--env <one>`
   / TUI selection presented alongside a trigger must **match** the
   trigger-resolved env (mismatch → **error**, never silent redirect). For
   non-triggered runs, `--env`/TUI selection *is* the explicit choice.
2. **`intent.defaultEnvironment`.** Optional top-level scalar; validated to name a
   real environment.
3. **Built-in `local` environment.** Terminal fallback, **interactive/local
   invocations only**.
4. **Error.** In CI / non-interactive contexts, when none of the above resolved.

`--env` accepts a **single** value; `a,b` → error (after a deprecation window).
Absence → the order above, **never** all-env.

> Single-env ≠ all-components. After the one environment is chosen, per-component
> activation (`Subscribe` / `ComponentEnvironment.Active`, the expander's
> `getApplicableComponents`) still decides which components run in it. That filter
> is **orthogonal and unchanged**.

### 2.3 The built-in `local` environment (LOCKED concept)

- **Always present** without declaration; the name `local` is **reserved**.
- **Auto-subscribed** by every component (auto-subscribe mechanics + opt-out are
  open — G-new-4).
- Uses each component's composition **`DefaultProfile`** for profile resolution.
  **orun enforces no safety** here — a safe local profile is the **composition
  author's duty** (decision B; see §6).
- **Excluded** from promotion graphs and from activation/trigger matrices: it is a
  sink, never a promotion *source*, and only local/interactive runs target it.
- **Purpose:** a zero-config terminal fallback (interactive runs never dead-end) +
  a clean iteration context kept separate from declared deployment targets (its
  own params/state, no promotion/activation baggage).
- **Not a safety guarantee.** Given B, `local` is only as safe as
  *(composition `DefaultProfile`) × (what `local` is configured to target)* — see
  §6 and G-new-6.

## 3. Decisions — locked

| # | Decision | Note |
|---|----------|------|
| **ENV-1** | One environment per plan/run; no all-env / multi-env plan | breaking run-path change |
| **ENV-2** | No sole-env special case — one declared env resolves like many | retires the old G-5 fallback branch |
| **ENV-3** | Resolution order: explicit → `defaultEnvironment` → `local` (interactive) → error (CI) | §2.2 |
| **ENV-4** | `intent.defaultEnvironment` is an **optional** top-level scalar, validated to name a real env | **not mandatory** (resolves G-9) |
| **ENV-5** | Trigger-bound env is **authoritative**; a conflicting `--env` is an error | precedence among explicit sources |
| **ENV-6** | Built-in `local` env: reserved name, auto-subscribed, interactive-only fallback, excluded from promotion + activation | §2.3 |
| **ENV-7** | `local` uses composition `DefaultProfile`; **safety enforcement deferred** — author's duty (option B) | §6 |

## 4. Enforcement surfaces (everything that must change)

| Surface | Change |
|---------|--------|
| `intent.yaml` schema + validation (`internal/model/intent.go`, `assets/config/schemas/intent.schema.yaml`) | add optional top-level `defaultEnvironment`; validate it names a real env |
| `--env` flag (`plan`/`run`) | single value only; `a,b` → error; absence → resolution order (§2.2), not all-env |
| environment resolver (`trigger.ResolveActiveEnvironments`, `cmd/orun/main.go`) | resolve to **exactly one** env per §2.2; add the built-in `local` env + the interactive-vs-CI fork; drop the no-`--env`=all-env default |
| `internal/expand` expander | scope expansion to the one resolved env; auto-subscribe components to `local` (G-new-4) |
| `Plan.Metadata.Trigger.ActiveEnvironments` (`internal/model/plan.go`) | constrain to **length 1** (or replace with a scalar). `PlanJob.Environment` already scalar — no change |
| promotion (`EnvironmentPromotion`, `internal/planner/promotion.go`) | promotion becomes a gated run *in the target env* across **separate** single-env plans; the `Satisfy: "same-plan"` cross-env-DAG path is removed (G-old-1) |
| triggers | a single event that today activates N envs becomes **N single-env runs**; ownership/ordering open (G-old-2) |
| CI workflows / docs | multi-env pipelines become N single-env invocations |

## 5. Open gaps (remaining — finalize before implementation)

Ranked by leverage; the top three constrain the rest.

| # | Gap | Why it matters |
|---|-----|----------------|
| **G-old-1** | **Promotion `same-plan` removal.** Single-env makes cross-env DAG edges in one plan impossible. Define how a gate reads prior-env success **across separate plans/state**, and how existing `Satisfy: "same-plan"` configs migrate. | Largest structural change; constrains G-old-2 and G-new-5 |
| **G-old-2** | **Trigger fan-out ownership.** Who runs the N single-env plans (CI vs an `orun … each-of` mode), how per-plan trigger provenance is recorded, and how ordering is enforced when promotion gates exist. | The multi-env story's mechanics |
| **G-new-5** | **`local` state isolation.** No per-env state keying exists today → a `local` run could clobber/pollute shared real-env state and **promotion evidence** (`PromotionMatch.Revision`). `local` must use isolated, ideally ephemeral/per-developer state, and never emit gate-satisfying evidence. Now also a *safety* mechanism, given B. | Confirm against `orun-state-redesign` |
| **G-new-4** | **Auto-subscribe to `local`.** Today `Subscribe` is explicit. Define the implicit "active in `local`" rule, the opt-out (`subscribe: { local: false }`? a label?), components whose composition cannot run locally, and the interaction with `selectors`. | Resolver behavior |
| **G-new-6** | **`local` env config / target.** `parameterDefaults`, backend, `dependencyMode` (propose `advisory`/`disabled`), and whether a user may augment it via an optional `environments.local` block. Reserved-name collision rule if a user already declared `local`. | Determines what `local` actually targets — and, under B, its de-facto safety |
| **G-new-3** | **"Am I local?" contract.** No TTY/CI signal exists. Define the interactive-vs-CI fork explicitly (e.g. `--ci` / `ORUN_CI` + trigger-mode), not fragile TTY-sniffing. | Drives step 3→4 of the resolution order |
| **G-doc** | **Missing spec docs.** `data-model.md`, `cli-surface.md`, `implementation-plan.md`, `test-plan.md`, `compatibility-and-migration.md` (incl. the concrete deprecation-window length/mechanism). | Promotion epic → ready |

## 6. Deferred — `local`-environment safety enforcement

> **Decision (locked): option B** — `local` reuses each composition's
> `DefaultProfile`; **orun enforces no safety**. A safe local profile is the
> composition author's responsibility. The options below are recorded verbatim so
> the decision can be revisited without re-deriving them.

**Key finding (why this needed a decision).** orun has **no machine-checkable
notion of "safe / non-mutating"** today. An `ExecutionProfile` is
`{Description, Policies, Jobs}` (`internal/model/composition.go`); `ProfilePolicies`
is `{RequireCleanGitTree, RequirePinnedTerraformVersion, RequireApproval}` —
**requirements/gates, not effects**. A profile is "safe" only *emergently*, by
which steps it enables. orun cannot currently tell whether a profile mutates.

**Options considered:**

| # | Mechanism | Maps to | Pros | Cons |
|---|-----------|---------|------|------|
| **A** | Named-profile convention — `local` resolves to a conventionally-named (`local`/`plan-only`) profile per composition | resolution rule over `ResolveProfileRef` | author-accurate per tech | new contract every composition must honor; needs a missing-profile fallback; can't add to 3rd-party compositions |
| **B** ✅ | Reuse `composition.DefaultProfile`, no enforcement | existing fallback — **no change** | trivial; zero new contract | **no safety guarantee**; if default mutates, `local` mutates |
| **C** | Declarative effect marker — add `effect: read-only \| mutating` to `ProfilePolicies`; `local` requires a profile marked safe; validate at resolve | new `ProfilePolicies` field | machine-checkable, auditable, **reusable** (CI "no-apply" gates, cockpit badge) | new schema field; only meaningful once compositions annotate; author's *claim*, not verified; retrofit cost |
| **D** | orun forces dry-run — inject a no-op overlay regardless of profile | synthesized `ProfileStepPatch` / capability filter | safe even if author did nothing | **brittle/dangerous** — generic mutation-neutralization across tf/helm/pulumi/shell isn't achievable; a shell `aws s3 rm` slips through → *false* safety. **Avoid** |
| **E** | Capability gating — tag mutating steps (`cloud-write`/`apply`) via the existing `Capabilities`/`IncludeCapabilities`; `local` runs with mutating caps excluded | existing capability mechanism | granular (a profile's read-only steps can still run); declarative | needs a reserved capability taxonomy + tagging discipline; untagged-step default (allow=unsafe / deny=breaks) is its own call |

**Why B is acceptable now.** It is consistent with orun staying unopinionated and
delegating tech-specifics to compositions. It deletes an entire mechanism (no new
schema, no validation, no skip/error fallback).

**Consequence of B (recorded honestly).** `local` is **not** safe-by-default.
Safety = *(composition `DefaultProfile`) × (what `local` is configured to target,
G-new-6)*. If the built-in `local` env ships with no `parameterDefaults`/backend,
mutating profiles will simply fail to find a real target — *de facto*, not
designed, safety. Whether "targets nothing real unless configured" is the intended
posture is part of G-new-6.

**Revisit when** a guaranteed-safe local sandbox becomes a hard requirement (a
shared/managed `local`, or untrusted/3rd-party compositions). Most likely upgrade
path: **C** (declarative `effect`) as the source of truth + **A** (a
conventionally-named local profile) as the selector, with **skip-or-error** (not
silent default) when no safe profile exists. **Do not** adopt **D**.

## 7. Migration / deprecation (to finalize — G-doc)

- Removing the no-`--env`=all-env default and `--env a,b` is **breaking**. Needs a
  deprecation window (warn → error) with a clear upgrade note: set
  `defaultEnvironment` (optional) and split multi-env pipelines into N single-env
  runs.
- Removing `Satisfy: "same-plan"` promotion is breaking for any intent using it
  (G-old-1 defines the replacement + migration).
- Existing `intent.yaml` files need no change for **interactive** use (they fall to
  `local`); **CI** invocations must specify `--env` or set `defaultEnvironment`.
- Concrete window length + the warn/error mechanism are specified in
  `compatibility-and-migration.md` (not yet written).

## 8. What ships before this epic (in `orun-catalog-state`)

The cockpit gets an **env selector** (key `e`) over the **existing** env model and
runs **component-scoped for one selected env** via the current run path — no schema
change, no removal of the all-env path (`orun-catalog-state/environments.md`,
CS6). This epic supersedes that with the finalized semantics above when it is
promoted; the cockpit's selection then feeds the §2.2 resolution order (as the
explicit/TUI tier) with no UI change.
