# Spec: orun-integrations-cli — registry-served integration commands

**Every integration becomes a CLI namespace the binary renders, not code it
carries.** Today the binary ships exactly one integration verb —
`orun integrations {provider} secret create` — validated against the
secrets-capability read and deliberately catalog-free
(`cmd/orun/command_integrations.go`, `internal/configsurface`). That pattern
(server declares, CLI renders) proved itself at n=1. This spec generalizes it
to the whole integration surface: orun-cloud's new **Integration Registry**
(`orun-cloud/specs/epics/saas-integration-registry/`, cluster IR) serves an
`IntegrationManifest` per provider that includes a declarative **CLI verb
tree**; the binary fetches the registry, renders each provider's tree as
cobra commands at runtime (help, completion, typo suggestions included), and
executes verbs as typed calls onto the existing authed config/state planes.
Connecting a new provider server-side lights up its CLI namespace with **zero
orun releases**; rich local behavior grafts in through a native-extension
seam that can extend a namespace but never contradict it.

## Status

| Field | Value |
|-------|-------|
| Status | **ICL0–ICL3 implemented** (this PR) — registry client + cache, runtime renderer, allowlisted execution, native-extension seam; SP5 surface byte-identical (goldens unchanged) |
| Cluster | **ICL** (integration CLI — pairs `orun-cloud` epic **IR**, milestone IR7) |
| Owner(s) | `internal/configsurface` (registry read + cache) · `cmd/orun/command_integrations.go` (renderer mount) · new `internal/integrationscli` (descriptor → cobra renderer, invoke mapper, native-extension registry) |
| Target branch | `claude/integration-registry-unified-ui-wg3c6o` |
| Builds on | SP5 (`orun integrations {provider} secret create`, capability-driven validation, `--from-broker` deprecation UX) · `internal/configsurface` client + `SecretsCapability` · `internal/remotestate` token-source precedence (env → session → OIDC) · the `unknownSecretsSubcommand` typo-suggestion UX · the materialize `Adapter` registry (`internal/materialize`) as the delivery-target seam |
| Decisions locked | (1) **The CLI carries no catalog** — provider list, verb trees, args, and help all derive from the registry read (`GET …/integrations/registry`); the shipped secret verbs are re-expressed as the first served tree, byte-identical UX; (2) **descriptors are data, never capability** — a verb maps onto an allowlisted operation of the existing config/integrations planes through the same `TokenSource`; no descriptor can name a raw URL, header, or local exec; (3) **offline help, online truth** — a per-org manifest cache under `.orun/` (24h soft TTL, `orun integrations sync` to refresh) keeps help/completion working offline; every invocation is server-validated regardless of cache state; (4) **native extends, served wins** — Go-registered subcommands may extend a provider namespace (e.g. local token verify); on a path collision the served verb wins and the binary logs the shadowed extension — server truth is never silently overridden; (5) **standard verbs come from capabilities** — `connections list|get|revoke`, `health`, `templates list`, `secret create`, `credentials list|revoke` render for any provider whose manifest declares the matching capability, so a provider gets a working namespace by existing. |
| Gate | ICL0–ICL2 ride recorded fixtures of the IR0 registry read and are human-independent; ICL3 (native seam) is pure Go. If IR0 has not merged server-side, the fixtures pin the agreed contract shape from `orun-cloud/specs/epics/saas-integration-registry/design.md` §9. |

## Thesis

The binary already trusts the server for *what a provider can do with
secrets*; it should trust the server for *what a provider can do at all*.
The alternative — hand-writing cobra trees per provider — recreates in Go
the exact console disease IR is curing in TypeScript (three catalogs, drift,
special cases). The CLI's job is rendering and transport discipline: parse
the descriptor, build the tree, map args onto typed SDK-shaped requests,
keep auth/tenancy/output conventions uniform. Everything provider-specific
is server truth.

What a user gets:

```
$ orun integrations                        # providers + connect state, from the registry
$ orun integrations cloudflare             # rendered verb tree + help
$ orun integrations cloudflare connections list
$ orun integrations cloudflare secret create CF_TOKEN \
    --template workers-deploy --scope env:prod        # SP5 verb, now served
$ orun integrations anthropic health       # re-homed AI providers included (IR5)
$ orun integrations daytona sandboxes list
```

## Milestones at a glance

| ID | Milestone | Status |
|----|-----------|--------|
| ICL0 | Registry client + cache: `configsurface.GetIntegrationRegistry`, per-org cache under `.orun/`, `orun integrations sync`, fixtures pinned to the IR0 contract | 🗓️ Planned |
| ICL1 | Runtime renderer: descriptor → cobra tree at command-init (registry cache read is lazy + non-fatal); `orun integrations` listing; provider trees with help/completion; typo suggestions extended | 🗓️ Planned |
| ICL2 | Verb execution: allowlisted invoke mapper onto configsurface/remotestate ops; uniform output (table default, `--json`), uniform errors (typed server errors decoded, never raw); SP5 secret verbs re-expressed byte-identically (golden tests) | 🗓️ Planned |
| ICL3 | Native-extension seam: `integrationscli.RegisterExtension(provider, cmd)`; collision rule (served wins, shadowed logged); first extension: `cloudflare` local recipe printer for air-gapped connect prep | 🗓️ Planned |

## Scope boundary

| In scope | Out of scope |
|----------|--------------|
| Registry read + cache; runtime cobra rendering; the invoke allowlist + arg binding; standard verb UX; offline help; native-extension seam; re-expressing SP5's verbs | Server-side manifests and verb declarations (IR7, orun-cloud); any new endpoint; the materialize adapters (unchanged; delivery-target ids keep validating against the capability read); the agent sandbox env contract (`orun agent serve` — untouched); workflow-action ecosystem integrations (still forbidden in core per `internal/workflowbackend/arch_test.go` — this surface talks to *orun-cloud*, never to provider APIs directly) |

## Read order

1. `README.md` (this file).
2. `design.md` — descriptor contract, renderer, invoke mapper, cache, seam.
3. `implementation-plan.md` — ICL0–ICL3 with "done when".
