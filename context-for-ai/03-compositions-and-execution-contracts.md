# Compositions and execution contracts

Compositions are the contract between component intent and executable jobs. They are where platform teams encode validation, job templates, profiles, capability selection, runner hints, and runtime behavior.

## Source resolution

Composition sources are declared once in `intent.yaml`:

```yaml
compositions:
  sources:
    - name: platform
      kind: dir
      path: ./compositions
    - name: overrides
      kind: oci
      ref: oci://ghcr.io/example/platform-overrides:v1.2.3
  resolution:
    precedence:
      - overrides
      - platform
    bindings:
      terraform: platform
```

Source kinds:

| Kind | Meaning |
| --- | --- |
| `dir` | Local composition package directory. |
| `archive` | Local packaged `.tgz` archive. |
| `oci` | Remote OCI-hosted stack or composition package. |

Orun resolves sources into a cache and writes `.orun/compositions.lock.yaml` with the resolved digests. That lock file is evidence for reproducible planning.

## Stack package format

The recommended package format is a Stack:

```text
compositions/
├── stack.yaml
└── compositions/
    └── terraform/
        ├── composition.yaml
        ├── schema.yaml
        ├── jobs/
        │   └── terraform-validate.yaml
        └── profiles/
            ├── terraform-pull-request.yaml
            ├── terraform-verify.yaml
            └── terraform-release.yaml
```

`stack.yaml` carries package metadata and optional OCI publishing information. Each composition type is a directory with split-kind documents.

## Split-kind documents

| Kind | Owns | AI guidance |
| --- | --- | --- |
| `Composition` | Public type facade, default job, default profile, references to schema/jobs/profiles. | Update when adding or renaming a type, job, or profile. |
| `ComponentSchema` | JSON Schema for component inputs. | Update when component input contract changes. |
| `JobTemplate` | Steps, capabilities, labels, runner defaults, timeouts, retries. | Update when execution behavior changes for all users of the type. |
| `ExecutionProfile` | Which jobs/steps/capabilities run in a context. | Update when PR, verify, release, or deploy behavior should differ. |

## Component type binding

`component.type` is the stable logical contract name. It should not contain registry URLs or package paths.

The source of the contract is resolved by:

1. `component.compositionRef`, if present.
2. `intent.compositions.resolution.bindings[type]`.
3. First matching export according to `resolution.precedence`.
4. Legacy `--config-dir` fallback, if used.

Use `compositionRef` sparingly for one-off experiments or migrations. For normal repo behavior, declare sources and bindings centrally in intent.

## Profiles and capabilities

Profiles allow one component type to behave differently by context without duplicating templates.

Example:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: ExecutionProfile
metadata:
  name: terraform-pull-request
spec:
  jobs:
    validate:
      includeCapabilities:
        - terraform.setup
        - terraform.fmt
        - terraform.init
        - terraform.validate
        - terraform.plan
      stepOverrides:
        init:
          run: terraform -chdir={{.terraformDir}} init -backend=false -input=false
```

Prefer `includeCapabilities` over brittle step lists when the composition uses capability tags. Capabilities describe intent at the step level and survive step ID refactors better.

## Template context

Composition steps render with values from the component instance:

- `.Component`
- `.Environment`
- `.Type`
- merged component input fields, such as `.terraformDir`, `.chartPath`, `.nodeVersion`

Do not rely on undeclared values. If a template needs a new value, add it to the component schema and supply it through component inputs or defaults.

## When to change a composition

Change a composition when:

- A component type needs a new required or optional input.
- A validation rule should apply to every component of that type.
- A step should change for every component of that type.
- A new profile is needed for a planning context.
- Reusable behavior is being copied across multiple components.

Do not change a composition for a single component's desired state. Use component inputs for that.

## Validation checklist for composition changes

1. Update `ComponentSchema` first if inputs changed.
2. Update `JobTemplate` steps and capability tags.
3. Update `ExecutionProfile` selections or step overrides.
4. Update `Composition` references and default profile/job if needed.
5. Run `orun compositions --intent intent.yaml --long`.
6. Run `orun validate --intent intent.yaml`.
7. Run `orun plan --intent intent.yaml --view dag`.
8. Inspect affected jobs in the generated plan.

