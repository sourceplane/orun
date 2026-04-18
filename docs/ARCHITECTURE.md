# arx Architecture Guide

## Design Philosophy

**arx** is built around three core principles:

1. **Separation of Concerns**: Intent, Jobs, and Plans are distinct layers
2. **Schema-Driven Everything**: Configuration is validated against schemas
3. **Deterministic Compilation**: Same inputs always produce same plan

## System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    User Intent Layer                         │
│  intent.yaml + discovered component.yaml files              │
│  + groups (policies)                                        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                  Normalization Phase                         │
│  - Validate schemas                                          │
│  - Expand wildcards                                          │
│  - Normalize references                                      │
│  - Default missing fields                                    │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│               Normalized Internal Model                      │
│  - Validated intent structure                               │
│  - Canonical component/env/group representation             │
│  - Ready for expansion                                       │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│              Expansion Phase (Env × Component)               │
│  - For each environment:                                     │
│    - Select applicable components                           │
│    - Merge inputs (precedence order)                        │
│    - Resolve policies (constraints)                         │
│    - Resolve dependencies (same-env/cross-env)             │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│              Component Instances (Expanded)                  │
│  Per-environment, per-component materialization             │
│  - Full merged configuration                                │
│  - Resolved policy constraints                              │
│  - Environment-specific dependencies                        │
└────────────────────────┬────────────────────────────────────┘
                         │
           ┌─────────────┴──────────────┐
           ▼                            ▼
    ┌───────────────┐           ┌────────────────┐
    │  Job Registry │           │  Job Planner   │
    │  (jobs.yaml)  │           │  (binds types) │
    └───────────────┘           └────────────────┘
           │                            │
           └─────────────┬──────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                  Job Binding Phase                           │
│  - Map component types to job definitions                   │
│  - Create JobInstance for each comp instance               │
│  - Render step templates with merged config                │
│  - Resolve inter-job dependencies                          │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│               Job Instances (DAG Nodes)                      │
│  - Template-rendered steps                                   │
│  - Full config materialized                                 │
│  - Ready for DAG construction                               │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                DAG Validation Phase                          │
│  - Topological sort                                          │
│  - Cycle detection                                           │
│  - Dependency resolution verification                       │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                  Plan Materialization                        │
│  - Convert JobInstances to Plan format                      │
│  - Serialize to JSON/YAML                                   │
│  - Ready for execution by CI runner                         │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                    plan.json / plan.yaml                     │
│  Immutable, deterministic, runtime-agnostic DAG             │
└─────────────────────────────────────────────────────────────┘
```

## Core Abstractions

### 1. Intent

**File**: `intent.yaml` plus discovered `component.yaml` manifests  
**Purpose**: Declarative specification of WHAT to deploy

```yaml
groups:              # Ownership/policy domains
  platform:
    policies:        # Non-overridable constraints
      isolation: strict
    defaults:        # Can be overridden
      region: us-west-2

environments:        # Environment definitions
  production:
    selectors:       # Optional domain filters / legacy selectors
      domains: [platform]
    defaults:        # Env-specific config
      replicas: 3
    policies:        # Env constraints
      requireApproval: true

discovery:           # Optional discovery roots for external components
  roots: [services/, infra/, deploy/]

components:          # Optional inline execution-agnostic specs
  - name: component-charts
    type: charts     # Maps to job definition
    subscribe:       # Inline components can also self-declare envs
      environments: [development, staging, production]
```

**Key principle**: Intent and component manifests contain ZERO execution details.

### 2. Job Registry

**File**: `jobs.yaml`  
**Purpose**: Define HOW each component type executes

```yaml
jobs:
  helm:              # Component type
    name: deploy     # Job name
    timeout: 15m
    retries: 2
    steps:           # Shell commands
      - name: deploy
        run: helm upgrade --install {{.Component}} ...
    inputs:          # Job defaults
      pullPolicy: IfNotPresent
```

**Key principle**: Jobs are templates, not coupled to specific environments.

### 3. Component Instance

**Where**: Internal model  
**Purpose**: Cartesian product of (environment, component, config)

```go
type ComponentInstance struct {
    ComponentName string                    // "web-app"
    Environment   string                    // "production"
    Type          string                    // "helm"
    Domain        string                    // "platform"
    
    // Fully merged configuration
    Inputs        map[string]interface{}    // All defaults + overrides
    Policies      map[string]interface{}    // Group + env policies
    
    // Resolved dependencies
    DependsOn     []ResolvedDependency
}
```

**Merge precedence** (lowest → highest):
1. Type defaults (from schema)
2. Job defaults (from jobs.yaml)
3. Group defaults (from intent groups)
4. Environment defaults (from environments)
5. Component inputs (from intent components)

**Key rule**: Policies are never merged, only validated/enforced.

### 4. Job Instance

**Where**: Internal model  
**Purpose**: Executable job for a component in an environment

```go
type JobInstance struct {
    ID          string  // "web-app@production.deploy"
    Name        string  // "deploy"
    Component   string  // "web-app"
    Environment string  // "production"
    Type        string  // "helm"
    
    // Rendered execution steps
    Steps       []RenderedStep
    
    // Dependencies between jobs
    DependsOn   []string  // ["common-services@production.deploy"]
    
    // Full config
    Env         map[string]interface{}
    Labels      map[string]string
    Config      map[string]interface{}
}
```

### 5. Plan

**File**: `plan.json`  
**Purpose**: Immutable, deterministic execution DAG

```json
{
  "apiVersion": "arx.io/v1",
  "kind": "Plan",
  "execution": {
    "concurrency": 4,
    "failFast": true,
    "stateFile": ".arx-state.json"
  },
  "jobs": [
    {
      "id": "web-app@production.deploy",
      "steps": [
        {
          "name": "deploy",
          "run": "helm upgrade --install web-app oci://... --namespace platform-web-app ...",
          "timeout": "15m"
        }
      ],
      "dependsOn": ["common-services@production.deploy"],
      "env": { "fully merged config" },
      "config": { "same as env" }
    }
  ]
}
```

**Key principle**: No templates, no defaults. Everything explicit.

## Expansion Algorithm

The **Expansion** phase is where intent transforms into job instances.

### Phase 2.1: Component Selection

For each environment, determine applicable components:

```go
applicableComps := env.selectors.components  // "web-app", "*", etc.
for compName, comp := range allComponents {
    if matches(applicableComps, compName) && comp.enabled {
        include in instances
    }
}
```

### Phase 2.2: Input Merging

For each component instance, merge configs in order:

```
merged := empty
merged.update(groupDefaults[domain])        // 1. Group defaults
merged.update(envDefaults[env])             // 2. Env defaults
merged.update(comp.inputs)                  // 3. Component inputs
// Highest priority wins (right overwrites left)
```

**Important**: This is a **deep merge** for nested maps.

### Phase 2.3: Policy Resolution

Extract policies for this component in this environment:

```go
policies := empty
policies.update(group.policies[domain])     // Group policies
policies.update(env.policies[env])          // Env policies
// Policies are constraints, not overridable
// Violation = planning error
```

### Phase 2.4: Dependency Resolution

Convert component dependencies to job-level:

```yaml
component.dependsOn:
  - component: common-services
    environment: ""           # "__same__" marker
    scope: same-environment
    condition: success

// Resolves to:
//   common-services@<currentEnv>.deploy
//   reason: same-environment
//   trigger: on success
```

## Job Binding Algorithm

### Phase 3.1: Type Lookup

For each component instance, find job definition:

```go
jobDef := jobRegistry.jobs[compInst.type]
// Example: type="helm" → jobs.helm
```

### Phase 3.2: Job Creation

Create JobInstance for this component instance:

```go
jobID := fmt.Sprintf("%s@%s.%s",
    compInst.ComponentName,
    compInst.Environment,
    jobDef.Name,
)
// Example: "web-app@production.deploy"
```

### Phase 3.3: Step Rendering

Render each step template with merged config:

```go
context := {
    "Component": compInst.ComponentName,
    "Environment": compInst.Environment,
    ...all merged inputs...
}

for step in jobDef.steps:
    rendered := render(step.run, context)
    // {{.Component}} → "web-app"
    // {{.replicas}} → "3"
```

### Phase 3.4: Dependency Resolution

Link job dependencies:

```
JobInstance.DependsOn:
  For each resolved dependency:
    Find all jobs for that component/env
    Add to DependsOn list
```

## DAG Validation

### Cycle Detection

DFS-based cycle detection:

```go
for each job:
    if not visited:
        if hasCycle(job, visited, recStack):
            return error "cycle detected"
```

Prevents infinite loops in execution.

### Topological Sort

Kahn's algorithm for execution ordering:

```
1. Calculate in-degree for all jobs
2. Start with jobs having in-degree = 0
3. Remove job, decrement dependents' in-degree
4. Repeat until all jobs processed
5. If unprocessed jobs remain, cycle exists
```

## Merge Semantics

### Simple Merge Example

Intent:
```yaml
components:
  - name: web-app
    type: helm
    domain: platform
    inputs:
      replicas: 5

groups:
  platform:
    defaults:
      region: us-west-2
      replicas: 3

environments:
  production:
    defaults:
      replicas: 10
```

Merge order for production:
```
1. Group defaults: { region: us-west-2, replicas: 3 }
2. Env defaults: { replicas: 10 } overwrites step 1
3. Component inputs: { replicas: 5 } overwrites step 2

Final: { region: us-west-2, replicas: 5 }
```

### Policy Non-Merge

```yaml
groups:
  platform:
    policies:
      isolation: strict
      
environments:
  production:
    policies:
      approval: required
```

Result:
```
policies for web-app@production:
  isolation: strict      (from group)
  approval: required     (from env)
  
Note: Cannot be overridden by component.inputs
```

## Templating System

Steps use Go `text/template` syntax. Template context includes:

- **Standard fields**:
  - `{{.Component}}` - Component name
  - `{{.Environment}}` - Environment name
  - `{{.Type}}` - Component type

- **Merged inputs** (any config key):
  - `{{.chart}}` - From component inputs
  - `{{.replicas}}` - From merged config
  - `{{.region}}` - From env defaults

Example:

```yaml
steps:
  - name: deploy
    run: |
      helm upgrade --install {{.Component}} {{.chart}} \
        --replicas {{.replicas}} \
        --region {{.region}}
```

With context: `{ Component: web-app, chart: "...", replicas: 3, region: "us-west-2" }`

Renders to:
```bash
helm upgrade --install web-app ... \
  --replicas 3 \
  --region us-west-2
```

## Extension Points

### 1. New Component Type

Add to jobs.yaml:

```yaml
jobs:
  my-custom-type:
    name: run
    steps: [...]
```

Create intent component:

```yaml
components:
  - name: my-service
    type: my-custom-type
```

Engine handles rest automatically.

### 2. New Policy

Add to intent:

```yaml
groups:
  my-group:
    policies:
      my-policy: value
```

Add to component domain link:

```yaml
components:
  - name: my-comp
    domain: my-group
```

Policy propagates to all instances.

### 3. New Selector

Extend `EnvironmentSelectors` in future:

```yaml
environments:
  my-env:
    selectors:
      labels:           # Future: select by labels
        team: "backend"
```

Requires schema update + expander logic.

## Performance Considerations

### Time Complexity

- **Normalization**: O(C + E + G) where C=components, E=environments, G=groups
- **Expansion**: O(E × C) worst case (all comps in all envs)
- **Job Binding**: O(E × C)
- **DAG Validation**: O(V + E) topological sort

Typical: < 100ms for 1000 jobs on modern hardware.

### Space Complexity

- **In-memory plan**: ~10KB per job (config size varies)
- **Typical**: 100 jobs → ~1MB
- **Scalable** to 10K+ jobs before memory becomes concern

## Error Handling

### Schema Validation Errors

```
Failed: intent.yaml does not match schema
- component "web-app" missing required field "type"
- environment "prod" selector has 0 components
```

### Policy Violation Errors

```
Failed: policy constraint violation
- component "web-app" violates "isolation: strict"
- (Component not in allowed group)
```

### Dependency Errors

```
Failed: dependency resolution
- "web-app" depends on "unknown-component"
- Cycle detected: web-app → common-services → web-app
```

### Template Rendering Errors

```
Failed: template rendering in job "web-app@prod.deploy"
- Unresolved variable: {{.unknownField}}
- Template parse error: {{.invalid syntax
```

## Debug Mode

Enables detailed logging of each phase:

```
./arx plan --debug

📋 Loading intent...
📚 Loading job registry...
🔍 Normalizing intent...
📦 Expanding (env × component)...
  expansion: web-app@production
  expansion: web-app-infra@production
  ...
🔗 Binding jobs and resolving dependencies...
  job binding: web-app@production → helm.deploy
  ...
🔄 Detecting cycles...
  DAG validation: 10 jobs, 5 edges
📊 Topologically sorting...
  sorted order: [common-services, web-app, web-app-infra, ...]
✨ Rendering plan...
✅ Plan generated with 10 jobs
```

## Testing Strategy

### Unit Tests

- Normalizer: wildcard expansion, defaults
- Expander: merge logic, policy handling
- Planner: template rendering, dependency resolution
- DAG validator: cycle detection, topological sort

### Integration Tests

- Full pipeline: intent → plan
- Example configurations with known output
- Error cases: invalid intent, cycles, missing deps

### Regression

- Snapshot testing of plan output
- Ensure deterministic output

## Future Roadmap

- [ ] Schema validation with JSON Schema v5
- [ ] Plan diffing (arx plan diff old.json new.json)
- [ ] Incremental planning (--changed-only)
- [ ] DAG visualization (--viz dot/svg)
- [ ] Multi-file intent support
- [ ] Helm values file integration
- [ ] GitOps workflow support
