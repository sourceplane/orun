---
title: Internals
description: A map of the orun codebase — every package under internal/, grouped by the subsystem it belongs to, with the one-line job of each.
---

The `orun` binary is one Go module. Cobra command wiring, flag definitions, and the thin handlers live in `cmd/orun`; everything else lives under `internal/`. The packages are grouped below by subsystem. The one-line descriptions come from each package's own doc comment; when a package has none, the description names its primary types.

## Compiler

| Package | Role |
| --- | --- |
| `internal/loader` | Load intent, discover component manifests, and load composition assets (`LoadIntent`, `LoadResolvedIntent`, `LoadJobRegistry`) |
| `internal/schema` | JSON Schema validation for intent, components, and job registries (`Validator`) |
| `internal/normalize` | Canonicalize the raw intent before expansion (`NormalizeIntent`) |
| `internal/expand` | Materialize environment × component instances, merge parameters, and resolve dependencies (`Expander`, `DependencyResolver`, `ComponentAnalyzer`) |
| `internal/planner` | Bind component instances to jobs, resolve job dependencies, detect cycles, and order the DAG (`JobPlanner`, `JobGraph`) |
| `internal/render` | Materialize the deterministic plan artifact (`Renderer`, `PlanViewer`) |
| `internal/model` | The stage contracts: intent, component manifests, compositions, jobs, component instances, and plans |
| `internal/composition` | Composition packages: resolving sources, archives, dependency modes, profiles, and publish plans |
| `internal/preset` | Intent presets: load `extends` references and merge them into the resolved intent |
| `internal/discovery` | Walk upward from the working directory to find `intent.yaml` |
| `internal/trigger` | Match normalized trigger events against trigger bindings |
| `internal/triggerctx` | The trigger context model and resolver — captures *why* a plan was compiled as a `TriggerOccurrence` |
| `internal/revkey` | Derive the human revision key (`rev-<scope>-<sha7>-p<planHash8>`) from a trigger occurrence and a plan hash |

## Runtime and workflows

| Package | Role |
| --- | --- |
| `internal/runner` | Execute a compiled plan: ordering, state, lifecycle hooks, per-job workspace isolation, job output secrets, workflow steps and approval gates |
| `internal/executor` | The executor interface and its local, Docker, and GitHub Actions backends |
| `internal/gha` | The GitHub Actions compatibility engine: `use:` steps, file commands, expression evaluation |
| `internal/execmodel` | Durable execution value types — the in-memory shape of a run's per-job and per-step state |
| `internal/flow` | The orun workflow language (`kind: Workflow`): `run:` / `action:` / `workflow:` verbs, `needs:` edges, CEL expressions, typed inputs, remote references |
| `internal/approval` | Human-in-the-loop gates for workflow steps — the pending request and the decision are run facts under `.orun/approvals`, never plan content |
| `internal/runbundle` | Hydrate run shards from remote artifacts (`HydrateOptions`, `PlanShard`) |
| `internal/artifactstore` | The generic artifact `Store` interface with GitHub Actions and in-memory implementations |
| `internal/statebackend` | The remote state and coordination backend contract: init, claim, heartbeat, log chunks, terminal updates, hermetic memoization |

## Catalog and change detection

| Package | Role |
| --- | --- |
| `internal/catalogmodel` | Every persisted component-catalog data type, the canonical-JSON encoder, sanitizers, ID helpers, and the embedded `component.yaml` JSON Schema |
| `internal/catalogresolve` | The catalog resolution pipeline: discover manifests, parse, and build the graph |
| `internal/catalogrefresh` | The shared "resolve the workspace into the object-model catalog" engine used by `orun catalog refresh`, `orun plan`, and the cockpit |
| `internal/catalogdiff` | Compare two resolved catalog snapshots and report component- and graph-level differences |
| `internal/catalogext` | Typed extension registry for `x-<vendor>` blocks in the catalog envelope |
| `internal/codeowners` | Parse a GitHub-style `CODEOWNERS` file so the resolver can derive ownership |
| `internal/sourcectx` | The source-context resolver that produces a `SourceSnapshot` for the workspace at refresh time |
| `internal/affected` | The single change-detection engine behind `plan --changed`, `run --changed`, the cockpit, and `orun catalog affected` |
| `internal/git` | Git change detection and intent diffing (`ChangeDetector`, `IntentDiffResult`) |
| `internal/ci` | Detect the CI provider and the refs it exposes (`DetectedRefs`) |

## Object model

| Package | Role |
| --- | --- |
| `internal/objectstore` | The L0 content-addressed object store — the only path through which object bytes are written; `objectstore/refstore` holds the refs |
| `internal/nodes` | The L1 typed-node layer: record schemas, canonical encoding, validation, and the closed kind list (`SourceSnapshot` … `Task`) |
| `internal/nodewriter` | Composes objectstore, refstore, and nodes into the tolerant-strict write walk |
| `internal/objplan` | Adapts resolver outputs into object-model nodes so `orun plan` writes the content-addressed graph |
| `internal/objread` | The native read layer: reconstruct execution detail from objects and refs, or from the live working tree |
| `internal/objindex` | L3 derived indexes under `<root>/index/` — rebuildable caches, never authoritative |
| `internal/objgc` | The reachability garbage collector: retention, then sweep |
| `internal/objremote` | Object substitution between two endpoints — syncing is a set difference |
| `internal/objrun` | Session glue that drives the object model from a runner's lifecycle: open a working tree, project state on every tick, stream logs, seal on terminal |
| `internal/objview` | Adapts object-model execution views into the legacy `execmodel` value types the renderers and cockpit consume |
| `internal/objmodel` | The unified read seam (`ModelReader`) the cockpit, CLI porcelain, and hosted console all consume |
| `internal/objcatalog` | The read view over the object-model component catalog |
| `internal/execseal` | Seals a finished execution into the content-addressed graph — the "commit" half of the working-tree/seal model |
| `internal/runworktree` | The live, mutable half: the atomically rewritten working file the runner mutates as jobs and steps progress |
| `internal/workingview` | The L4 inspection surface: `fsck`, a human-readable checkout, and the read primitives behind `orun objects` |
| `internal/clock` | Injectable wall clock; object-model production code never calls `time.Now()` directly |

## Cockpit

| Package | Role |
| --- | --- |
| `internal/cockpit` | The shared view layer: `bridge` (one read path over `.orun/` or a remote backend), `viewmodel`, `render`, `style` (design tokens), `surface`, `watch` (polling stream), and `catalogread` |
| `internal/tui` | Cockpit v1 — the default `orun tui` |
| `internal/tui2` | Cockpit v2 — `orun tui-next`: `shell`, `frame`, `store`, `design` (Northwind Mono), `data`, `surfaces/*`, `agentfold`, `demo` |
| `internal/ui` | ANSI colour handling, the GitHub Actions output renderer, and the live region used by non-TUI commands |

## Cloud client

| Package | Role |
| --- | --- |
| `internal/cliauth` | CLI login flows, the OS credential store with file fallback, `~/.orun/config.yaml`, and repo links |
| `internal/remotestate` | HTTP client, token resolution, plan conversion, and run-ID derivation for the remote state backend; holds `DefaultCloudURL` |
| `internal/configsurface` | Client for the platform's workspace/project/environment-scoped config surface (secrets) |
| `internal/cloudflare` | Cloudflare REST client used to provision a self-hosted backend (D1, R2, Workers, queues) |
| `internal/backendbundle` | The embedded self-hosted backend artifacts: Worker bundle, D1 migrations, manifest |
| `internal/githubretry` | When a GitHub API read is worth retrying |

## Secrets and policy

| Package | Role |
| --- | --- |
| `internal/scoperef` | The scope-reference grammar (`secret://`, config, flag) — the only value-shaped thing allowed in intent, plans, or objects |
| `internal/secretref` | Deprecated secret-only face of the grammar; delegates to `scoperef` |
| `internal/secretpolicy` | Loader and model for the portable `kind: SecretPolicy` document |
| `internal/redact` | Masks resolved secret values in step output before any sink — console, live tail, remote chunks, sealed blobs |
| `internal/materialize` | Deploy-time delivery of resolved secrets into an application's native store (v1: Cloudflare Worker bindings) |

## Agent runtime and MCP

| Package | Role |
| --- | --- |
| `internal/agent` | The agent runtime: the delegation loop that turns a frozen brief into a PR; subpackages `driver` (the `AgentDriver` seam), `attach` (the attach protocol), `live` (live sessions), `ground` (grounding a sandbox session in a repository) |
| `internal/agenttype` | Loads `agents/*.md` agent-type definitions and seals them into `AgentTypeSnapshot` nodes |
| `internal/mcpserve` | The shared stdio MCP transport: one JSON-RPC loop composing `ToolProvider`s |
| `internal/platformmcp` | The platform tool plane over the public API (catalog, runs, tasks, secrets, …) |
| `internal/penmcp` | The provenance pen over MCP: the `pr_open` tool |
| `internal/integrationscli` | Renders registry-served integration verb trees as Cobra commands |

## Scaffolding, baselines, and provenance

| Package | Role |
| --- | --- |
| `internal/scaffold` | The unified scaffolding and instantiation engine: one language for scaffolding a component and bootstrapping a whole repository |
| `internal/actions` | The closed, versioned registry of typed actions a blueprint hook may call (`<namespace>/<verb>@v<major>`) |
| `internal/forge` | The small GitHub surface bootstrap actions need: does this repository exist, has this workflow run finished |
| `internal/provenance` | The pen: every PR carries its lineage in the branch name, the body manifest, and the `Orun-Task` commit trailer |

## Tasks

| Package | Role |
| --- | --- |
| `internal/contract` | The agent runtime's task contract — goal, blast-radius ceiling, done-when list, and gates |
| `internal/taskfile` | Reads authored `tasks/<KEY>.TaskContract.yaml` documents |
| `internal/taskobj` | Seals tasks into the object graph under `refs/tasks/<key>` and reads them back |

## Test fixtures and conformance

| Package | Role |
| --- | --- |
| `internal/testfx` | Test helpers: `objfs` for the object model, `statefs` for the state redesign |
| `internal/objgolden` | Cross-language golden vectors that pin the Go and TypeScript object-model readers to the same bytes |
| `internal/objmodele2e` | The end-to-end plan → seal → index → fsck → push → pull → gc walk |

## Design constraints

- planning stages are deterministic — identical inputs produce byte-identical plans
- runtime behaviour stays explicit in the plan artifact
- command handlers stay thin; business logic lives under `internal/`
- `internal/model` is the stable contract between compiler stages
- the object model is written only through `objectstore`; indexes are caches, never authority
- secrets are references in every artifact; values exist only at resolve time and are redacted at every sink

## Related

- [Compiler pipeline](./compiler-pipeline.md) — the stages the compiler packages implement.
- [Execution runtime](./execution-runtime.md) — what `runner` and the executors do with a plan.
- [Cockpit architecture](../cockpit/architecture.md) — `cockpit`, `tui`, and `tui2` in depth.
- [Extending orun](../contributing/extending-orun.md) — where to register a new command, driver, tool provider, or action.
