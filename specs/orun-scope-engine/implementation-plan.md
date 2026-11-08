# Implementation plan — orun-scope-engine (SG1 → SG4)

> Every slice lands behind tests and is independently shippable. Two invariants
> hold throughout: **`Parse` never echoes its input** (a failing value in a
> secret slot may *be* a secret), and **a plan object never carries a secret
> value** — `secretEnv` stays references-only, while `config://` deliberately
> resolves *to* content.
>
> Pairs `orun-cloud/specs/epics/saas-scope-engine/` — SG1↔SE4, SG2↔SE3,
> SG3↔SE4, SG4↔SE6. Each SG slice rides recorded fixtures of its SE counterpart,
> so neither repo blocks the other.

---

## SG1 — `internal/scoperef` + the shared vectors

- **Build.**
  - New `internal/scoperef`: `Scheme`, `Ref`, `Parse`, `String`, `IsRef` per
    `design.md` § 1. Closed dimension registry; canonical render order;
    unknown dimension ⇒ typed parse error; one-bare-segment ⇒ workspace;
    four-bare-segments ⇒ deprecated positional, normalized on read.
  - `internal/scoperef/testdata/refs.vectors.json`, byte-identical to
    `packages/contracts`' fixture, plus a `make check-ref-vectors` target and a
    CI step that fails when the two drift.
  - `internal/secretref` becomes a thin alias (`Parse`/`IsRef`/`Ref` delegating
    to `scoperef` with `SchemeSecret`), deprecated in doc comments, retained for
    one release.
  - Retarget callers: `internal/expand`, `internal/planner/secret_bindings.go`,
    `internal/runner`, `internal/render`, `cmd/orun/run_secrets.go`.
  - **`ttlSeconds` re-resolve** (`design.md` § 6): the runner re-resolves a
    job's secrets under a fresh lease check when `TTLSeconds` elapses mid-job,
    re-seeding the redactor with any changed values before they can be echoed.
    Landed here because it touches the same call path.
- **Done when.** Every accept/reject vector passes; every existing secrets test
  passes unmodified; the legacy 4-tuple round-trips through canonicalization
  with unchanged meaning and **no plan-digest change** for an unmodified
  manifest; a `Parse` failure never contains the input (asserted by a test that
  greps the error string for the value); a job exceeding its TTL re-resolves and
  a lease lost during re-resolve fails the job closed.

---

## SG2 — The component dimension

- **Build.**
  - `component:` parses, canonicalizes, and renders.
  - `internal/planner/secret_bindings.go`: `mergeBindingRefs` emits the
    component segment when the instance resolves one; the existing
    binding-vs-`secretEnv` conflict check compares **canonical** forms, so
    `secret://acme/api/prod/K` and `secret://acme/project:api/env:prod/K` are
    one reference, not a conflict.
  - `internal/render/plan.go`: `PlanSecretRef` round-trips the segment; the plan
    stays structurally value-free.
  - `orun plan` renders the component rung in its secrets view.
- **Done when.** A component-scoped ref survives expand → plan → runner
  round-trip; a ref whose component disagrees with the run's job is rejected by
  the backend with a 400 the CLI surfaces intelligibly (fixture test — the CLI
  asserts, the server decides); plan digests are stable across re-plans.

---

## SG3 — Contextual omission

> **Status — binding shipped.** `bindContext` binds omitted workspace, project
> and env in the planner (`internal/planner/secret_bindings.go`) for `secretEnv`
> and `optionalSecretEnv`, from the same scope the composition `secretBindings`
> already use. Fully pinned references keep their authored spelling, so their
> plan digest does not change. Without a workspace or project to bind from (an
> unlinked repo planned offline) a reference is kept as authored rather than
> failing the plan, so nothing that compiled before stops compiling. The
> component dimension is left to the lease. **Still to do:** the `{{…}}`
> reference-slot deprecation warning.

- **Build.**
  - `bindContext` (`design.md` § 2): omitted dimensions bind from the component
    instance at expand. Never overrides an authored value; a required slot whose
    dimension cannot be bound is a compile error naming the slot and the missing
    dimension.
  - Deprecate `{{…}}` interpolation **in reference slots only**: a `secretEnv`
    or `env` value that is `IsRef` and contains `{{` warns loudly, names the
    contextual replacement, and — for the specific broken `{{env}}` case that
    today silently yields `//` — errors rather than producing an invalid ref.
  - Update `specs/orun-secrets/data-model.md` § 2.1's example, which is
    currently unrunnable.
- **Done when.** `secretEnv: { DATABASE_URL: "secret://DATABASE_URL" }` resolves
  correctly in every subscribed environment from one manifest line; the four-layer
  merge precedence and both existing guards (literal-in-`secretEnv`,
  `env`-shadows-`secretEnv`) are unchanged; the documented example in
  `data-model.md` is executed by a test.

---

## SG4 — The config plane

- **Build.**
  - `config://` accepted in `spec.env` (and the intent/environment/subscription
    `env` layers, same four-layer precedence); rejected in `secretEnv`.
    `secret://` in `env` stays a compile error.
  - Resolution at expand through `internal/configsurface` (extend the existing
    client with the config read; reuse `internal/remotestate/auth.go`'s token
    precedence — OIDC → `ORUN_TOKEN` → CLI session — with no new auth path).
  - Resolved values become ordinary plan `env` entries and enter the digest.
  - Local fallback: `ORUN_CONFIG_<KEY>`, mirroring the secrets local resolver's
    fail-closed posture and error wording.
  - `masked` visibility: redacted in plan *rendering*, present in the digest.
  - `orun config list|get|diff` per `design.md` § 4.
  - `flag://` parses but is rejected from every manifest slot, with an error
    naming the SDK call instead.
- **Done when.** A `config://` ref resolves at plan time and appears in the plan
  digest; **changing the value rescopes its consuming components under
  `--changed`** (the SE-D8 consequence, asserted by test); an unresolvable ref
  is a compile error naming the key and the chain walked; `orun config diff`
  reports real cross-env deltas; a `flag://` in `env:` errors with the SDK
  pointer; `orun plan --json` goldens updated once, deliberately.

---

## Sequencing notes

- **SG1 first, and alone.** It is a mechanical retarget plus a fixture harness;
  landing it before any semantic change keeps every later diff readable.
- **SG2 before SG3.** Add the dimension before teaching dimensions to disappear,
  so the vectors are written once to their final shape.
- **SG4 last.** It is the only slice that changes what a plan digest *contains*,
  so it should land against a grammar that has stopped moving.
- Each slice rides recorded fixtures of its SE counterpart, so the two repos can
  land in either order; only SG4's live path needs SE6 deployed.
