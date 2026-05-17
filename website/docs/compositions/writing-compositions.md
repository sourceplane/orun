---
title: Writing compositions
---

Author a new composition package when you want to introduce a new component type without changing the core planner.

## 1. Create a Stack package root

The recommended format is a `stack.yaml` manifest with a `compositions/` subdirectory. Each composition type lives in its own subdirectory using either inline or split-kind authoring.

```text
my-platform/
├── stack.yaml
└── compositions/
    └── mytype/
        ├── composition.yaml
        ├── schema.yaml
        ├── jobs/
        │   └── mytype-apply.yaml
        └── profiles/
            ├── mytype-verify.yaml
            └── mytype-deploy.yaml
```

## 2. Define the Stack manifest

`stack.yaml` describes the package and an optional OCI registry target. When `spec.compositions` is omitted, the packager auto-discovers composition files by walking the directory tree.

```yaml
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: my-platform-stack
  version: 1.0.0
  description: Platform compositions for my-platform
  owner: my-org
registry:
  host: ghcr.io
  namespace: my-org
  repository: my-platform-stack
  visibility: public
```

## 3. Choose an authoring style

### Split-kind authoring (recommended)

Split each concern into its own file and kind. This makes each piece independently versionable, reusable, and easier to review.

| Kind | Owns | Changes when |
|------|------|-------------|
| `Composition` | Type binding and defaults | You introduce/rename a component type |
| `ComponentSchema` | Input validation | Component contract changes |
| `JobTemplate` | Steps, capabilities, runner defaults | Runtime implementation changes |
| `ExecutionProfile` | Which behavior to use per trigger/env | PR/release/env behavior changes |

**composition.yaml** — The public facade:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: mytype
spec:
  type: mytype
  description: My component type
  schemaRef:
    name: mytype-component
  defaultJob: apply
  defaultProfile: verify
  jobs:
    - name: apply
      templateRef:
        name: mytype-apply
  profiles:
    - name: verify
      profileRef:
        name: mytype-verify
    - name: deploy
      profileRef:
        name: mytype-deploy
```

**schema.yaml** — Input validation contract (`kind: ComponentSchema`):

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: ComponentSchema
metadata:
  name: mytype-component
spec:
  type: mytype
  schema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    required: [name, type, parameters]
    properties:
      name:
        type: string
      type:
        const: mytype
      parameters:
        type: object
        required: [target]
        properties:
          target:
            type: string
```

**jobs/mytype-apply.yaml** — Reusable execution template (`kind: JobTemplate`):

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: JobTemplate
metadata:
  name: mytype-apply
spec:
  description: Apply mytype changes
  runsOn: ubuntu-22.04
  timeout: 15m
  capabilities:
    - mytype.setup
    - mytype.apply
  steps:
    - id: setup
      name: setup
      capability: mytype.setup
      run: mytool version
      onFailure: stop
    - id: apply
      name: apply
      capability: mytype.apply
      run: mytool apply {{.parameters.target}}
      onFailure: stop
```

**profiles/mytype-verify.yaml** — Behavior overlay (`kind: ExecutionProfile`):

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: ExecutionProfile
metadata:
  name: mytype-verify
spec:
  description: Verify without applying
  jobs:
    apply:
      includeCapabilities:
        - mytype.setup
        - mytype.apply
      stepOverrides:
        apply:
          run: mytool apply --dry-run {{.parameters.target}}
```

### Inline authoring

For simpler compositions, define everything in a single `compositions.yaml`:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: mytype
spec:
  type: mytype
  defaultJob: apply
  parameterSchema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    properties:
      type:
        const: mytype
      parameters:
        type: object
        required: [target]
        properties:
          target:
            type: string
  executionProfiles:
    verify:
      jobs:
        apply:
          stepsEnabled:
            - setup
            - apply
  jobs:
    - name: apply
      runsOn: ubuntu-22.04
      steps:
        - id: setup
          name: setup
          run: mytool version
        - id: apply
          name: apply
          run: mytool apply {{.parameters.target}}
```

## 4. Test with an example component

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: demo
spec:
  type: mytype
  subscribe:
    environments:
      - name: development
        profile: verify
  parameters:
    target: demo-service
```

## 5. Validate and inspect

```bash
orun pack --root ./my-platform
orun validate --intent examples/intent.yaml
orun compositions mytype --intent examples/intent.yaml
orun plan --intent examples/intent.yaml --output /tmp/mytype-plan.json
```

## 6. Publish to a registry

```bash
orun login ghcr.io
orun publish --root ./my-platform
```

Teams consuming the stack reference it by OCI ref:

```yaml
compositions:
  sources:
    - name: my-platform
      kind: oci
      ref: oci://ghcr.io/my-org/my-platform-stack:1.0.0
```

See [Stacks](../concepts/stacks.md) for the full packaging and distribution guide.

## Authoring guidelines

- Use split-kind authoring when your composition has multiple profiles or jobs that might be reused across types.
- Use inline authoring for simple compositions with 1-2 profiles and a single job.
- Tag steps with `capability` fields so profiles can select behavior semantically.
- Prefer `includeCapabilities` over `stepsEnabled` — step IDs are implementation details.
- Use `stepOverrides` in profiles to alter behavior (e.g., `--dry-run`) without duplicating steps.
- Add `policies` to release profiles for enforcement rules like `requireApproval`.
- Keep schemas strict enough to reject invalid inputs early.
- Set `defaultJob` and `defaultProfile` explicitly.
