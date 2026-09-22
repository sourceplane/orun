---
title: Scope references
---

`secret://`, `config://`, and `flag://` references share one grammar — the
**scope-reference grammar** — parsed identically by the orun CLI (Go) and the
Orunbase platform (TypeScript), asserted against shared test vectors so the
two planes can never drift.

```
<scheme>://[<workspace>/][<dim>:<value>/]*<KEY>[@<version>]
```

```
secret://acme/project:api/env:prod/DATABASE_URL      # named dimensions
secret://acme/api/prod/DATABASE_URL                  # legacy positional (same reference)
secret://DATABASE_URL                                # fully contextual
secret://component:worker/API_KEY@3                  # component dimension + version pin
config://env:staging/FEATURE_TIER
flag://project:api/env:prod/NEW_CHECKOUT
```

## Segments

| Segment | Rule |
|---|---|
| `scheme` | `secret`, `config`, or `flag`. |
| `workspace` | Optional first segment (a workspace slug or id). Omitted → the run's workspace. |
| `dim:value` | Zero or more named dimensions. Allowed dimensions: `project`, `env`, `component` — except `flag://`, which accepts only `project` and `env`. Each dimension at most once. |
| `KEY` | `^[A-Za-z][A-Za-z0-9._-]{0,127}$`. A key can never contain `:`, which is what makes `:` safe as the dimension delimiter. |
| `@version` | Optional pin, `^[1-9][0-9]{0,8}$`. Absent (or `0`) means head-at-resolve-time. |

Dimension values match `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`. The canonical
render order is `workspace / project / env / component / KEY`.

## Contextual omission

An omitted dimension **binds to the run context at expand time**. It is *not*
a wildcard and never widens a lookup: `secret://DATABASE_URL` in a component
resolves against exactly the workspace, project, environment (and component)
the running job already has.

This is the property that lets one manifest line work in every environment —
and it removes the old need for `{{…}}` interpolation inside references. That
interpolation silently stripped unknown `{{…}}` segments to empty strings,
producing invalid references; with contextual omission it is simply
unnecessary. Prefer omitting a dimension over templating it.

:::warning "Stored at" is not "referenced as"
A fully-pinned reference (`secret://acme/project:api/env:prod/KEY`) names one
environment forever. Pasting it into a `component.yaml` that ships to several
environments is silently wrong everywhere except `prod`. In manifests, write
the contextual form and let the run bind the dimensions.
:::

## Parsing is fail-closed

An unknown dimension name is a hard error — a typo like `enviroment:prod`
must not quietly resolve broader than intended. The typed error classes
(shared between Go and TypeScript):

`not_a_ref` · `unknown_scheme` · `unknown_dimension` · `duplicate_dimension` ·
`dimension_not_allowed` · `bad_key` · `bad_value` · `bad_version` · `bad_shape`

Error messages **never echo the input** — a failing value in a secret-shaped
slot may itself be a pasted secret.

## Compatibility

The legacy positional 4-tuple (`secret://<workspace>/<project>/<env>/<KEY>`)
and the named spelling denote the **same** reference; comparison is canonical.
The authored spelling is preserved end-to-end, so switching an existing
manifest to named dimensions — or leaving it positional — does not change the
plan digest.

## See also

- [Secrets](../concepts/secrets.md) — where `secret://` references are declared and resolved
- [Plan schema](./plan-schema.md) — how references materialize into a plan
