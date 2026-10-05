---
title: Secrets
description: "Secrets in orun are references, never values, and that standard is enforced at plan time: a literal in a secret slot fails the plan. This page covers secretEnv, optionalSecretEnv, and secretOutputs."
---

"Secrets are references, never values" is a standard orun enforces rather than recommends: a literal value in a secret slot fails `orun plan`, so the rule cannot be broken by a paste.

`orun` moves secrets by **reference, never by value**. Manifests declare
[`secret://` references](../reference/scope-references.md); values are resolved
at run time from the platform (or from local overrides), injected into the job
environment in-memory, and swept from every log and sealed output by the
redactor. No secret value ever appears in a manifest, a plan, or the object
model.

This page covers the three declaration surfaces: required secret env
(`secretEnv`), best-effort secret env (`optionalSecretEnv`), and job **output**
secrets (`secretOutputs`). For managing the secrets themselves, see
[`orun secrets`](../cli/orun-secrets.md); for integration-minted credentials,
see [`orun integrations`](../cli/orun-integrations.md).

## `secretEnv` — fail-closed references

A map of environment variable name → `secret://` reference. At plan time each
reference materializes into the plan (reference only — content-addressed like
any field); at run time the values are resolved and injected into the job's
environment. A reference that cannot be resolved **fails the job before its
first step** — a required secret is required.

```yaml
# component.yaml
spec:
  secretEnv:
    DATABASE_URL: secret://acme/api/prod/DATABASE_URL
```

Every value must parse as a `secret://` reference — a literal value in a
`secretEnv` slot is rejected, so a pasted secret can never land in a manifest.
Prefer the contextual form (`secret://DATABASE_URL`) so one line serves every
environment — see [Scope references](../reference/scope-references.md).

## `optionalSecretEnv` — wire now, seed later

A component-level map beside `secretEnv` with one difference in resolve
posture: a reference whose key is **not seeded in the backend is skipped** —
no env var, no failure — instead of failing the job.

```yaml
# component.yaml
spec:
  secretEnv:
    DATABASE_URL: secret://acme/api/prod/DATABASE_URL      # fail-closed
  optionalSecretEnv:
    SENTRY_DSN: secret://acme/api/prod/SENTRY_DSN          # skipped if unseeded
```

This is the *wire-now-seed-later* shape: declare what a component *can*
consume, seed the value later, and the next run picks it up with no repo
change.

- `optionalSecretEnv` is **component-level only** — unlike `env`/`secretEnv`
  it has no intent, environment, or subscription merge layers.
- The same leak guard applies: every value must parse as a `secret://`
  reference.
- A key may appear in only one posture. Declaring it in both `env` and
  `optionalSecretEnv`, or both `secretEnv` and `optionalSecretEnv`, is a
  compile error (`pick one resolve posture`).
- In the plan, an optional reference carries `optional: true` on its
  `PlanSecretRef` — content-addressed like everything else, so flipping a
  reference between postures changes the plan.

**Local runs:** outside remote state, secret values come from
`ORUN_SECRET_<KEY>` environment overrides. A missing override skips an
optional reference the same way; a missing override for a *required*
reference still fails the job before its first step.

## `secretOutputs` — job output secrets

Jobs *produce* secrets too — a Terraform apply mints a database password; the
next deploy needs it. `secretOutputs` publishes such values to the platform
directly from the job, so they never transit CI logs, artifacts, or a human.

Declare the outputs as a component parameter — a comma-separated
`KEY=producer-hint` list. Only the `KEY` halves matter to orun (keys must
match `^[A-Z][A-Z0-9_]{0,127}$`); the hint half is the composition's own
note, e.g. the Terraform output name:

```yaml
# component.yaml
spec:
  parameters:
    secretOutputs: "DB_PASSWORD=rds_password,API_KEY=api_key"
```

Declaring it exports **`ORUN_SECRET_OUTPUTS`** to every step of the job — the
path of a per-job sink file. Steps append entries; there is no CLI to call and
no auth to plumb:

```bash
# single-line value
terraform output -raw rds_password | { read -r v; echo "DB_PASSWORD=$v" >> "$ORUN_SECRET_OUTPUTS"; }
```

```
# multi-line values use a heredoc marker
KEY<<__ORUN_EOF__
multi
line value
__ORUN_EOF__
```

After the job's steps succeed, the runner:

1. parses the sink — a garbled line is a parse error, never silently dropped;
2. enforces the **declared-key allow-list** — an undeclared key fails the job
   (`secret output "X" is not declared in secretOutputs`), so there is no
   silent side-channel;
3. registers every value with the redactor;
4. publishes the batch over the run's **lease-bound** channel. The platform
   derives the target scope (the project/environment rung) from the leased job
   itself — nothing on the wire names a scope, so a job cannot aim its outputs
   at somebody else's environment.

An empty sink is a no-op (plan-only lanes share the component's parameters, so
an empty sink is normal). Local runs without a remote-state backend report and
skip.

## Where the values go, and don't

| Surface | Secret values? |
|---|---|
| Manifests (`intent.yaml`, `component.yaml`) | Never — references only, enforced |
| The plan / object model | Never — references (+ `optional` flag) only |
| Job environment | Yes — injected in-memory at run time |
| Step logs and sealed outputs | Swept by the redactor |
| CI logs / artifacts | Never — output secrets go straight from the sink to the platform |

## See also

- [Scope references](../reference/scope-references.md) — the `secret://` grammar
- [`orun secrets`](../cli/orun-secrets.md) — create, rotate, reveal, import
- [`orun integrations`](../cli/orun-integrations.md) — provider-minted (brokered / rotated) secrets
- [`orun policy`](../cli/orun-policy.md) — who may resolve what, tested locally
- [Terraform state](../execute/terraform-state.md) — the other de-AWS channel
