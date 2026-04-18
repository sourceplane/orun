---
title: Writing compositions
---

Author a new composition when you want to introduce a new component type without changing the core planner.

## 1. Create a new composition directory

```text
assets/config/compositions/mytype/
├── job.yaml
└── schema.yaml
```

The directory name becomes the component `type`.

## 2. Define the schema first

Start by expressing the type contract in `schema.yaml`.

```yaml
$schema: http://json-schema.org/draft-07/schema#
title: MyType Component
type: object
properties:
  type:
    const: mytype
  inputs:
    type: object
    properties:
      target:
        type: string
      timeout:
        type: string
    required: [target]
```

That makes validation fail early before you have to debug runtime behavior.

## 3. Add a job registry

```yaml
apiVersion: sourceplane.io/v1
kind: JobRegistry
metadata:
  name: mytype-jobs
jobs:
  - name: apply
    runsOn: ubuntu-22.04
    steps:
      - name: apply
        run: mytool apply {{.target}}
```

## 4. Test with an example component

Create a sample component manifest and point it at the new type.

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: demo

spec:
  type: mytype
  subscribe:
    environments: [development]
  inputs:
    target: demo-service
```

## 5. Validate and inspect

```bash
arx validate --intent examples/intent.yaml --config-dir assets/config/compositions
arx compositions mytype --config-dir assets/config/compositions
arx plan --intent examples/intent.yaml --config-dir assets/config/compositions --output /tmp/mytype-plan.json
```

## Authoring guidelines

- Keep schemas strict enough to reject invalid inputs early.
- Put reusable defaults in schemas or job registries, not in every component.
- Prefer explicit step phases and timeouts when ordering matters.
- Test the composition with dry-run first, then execute through the runtime you expect to use in CI.