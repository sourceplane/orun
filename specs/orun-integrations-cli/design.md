# orun-integrations-cli — Design

Status: Draft (normative once ICL0 lands). Pairs
`orun-cloud/specs/epics/saas-integration-registry/design.md` §9 (the manifest
`cli` block and IR7).

## 1. The descriptor contract (what the server serves)

The registry read returns, per provider, the manifest projection including:

```go
// internal/configsurface — additive beside SecretsCapability
type IntegrationDescriptor struct {
    Provider      string          `json:"provider"`
    DisplayName   string          `json:"displayName"`
    Category      string          `json:"category"`
    Capabilities  []string        `json:"capabilities"`
    Connected     int             `json:"connectedCount"`
    Status        string          `json:"status"` // live | dormant | roadmap
    CLI           *CliNamespace   `json:"cli,omitempty"`
}

type CliNamespace struct {
    Verbs []CliVerb `json:"verbs"`
}

type CliVerb struct {
    Path    []string  `json:"path"`     // e.g. ["secret","create"]
    Summary string    `json:"summary"`
    Args    []CliArg  `json:"args"`     // positionals + flags, typed
    Invoke  CliInvoke `json:"invoke"`
    NeedsConnection bool `json:"needsConnection,omitempty"`
}

type CliArg struct {
    Name string; Kind string  // positional | flag
    Type string               // string | int | bool | enum | kv
    Enum []string; Required bool; Repeat bool
    Help string
}

type CliInvoke struct {
    Plane string            `json:"plane"` // "config" | "integrations"
    Op    string            `json:"op"`    // allowlisted operation id
    Bind  map[string]string `json:"bind"`  // arg name -> request field
}
```

Contract rules (mirrored server-side by IR7's tests):

- `Op` values come from a **closed allowlist compiled into the binary**
  (§3). An unknown op renders the verb disabled with an "update orun"
  hint — forward-compatible, never a crash, never a guess.
- Verbs are additive per manifest version; the renderer ignores unknown
  fields (the additive-evolution rule both repos already follow).
- The shipped secret-authoring shapes (`CreateSecretRequest`,
  `SecretBrokerBinding`, `SecretRotationBinding` — all value-free) are the
  binding targets for the secret verbs; no new wire shapes.

## 2. Renderer (ICL1)

- `registerIntegrationsCommand` keeps its static skeleton
  (`orun integrations`, `sync`, and the SP5 fallback path) and mounts a
  **dynamic layer**: at command construction, if a cached registry exists
  for the resolved org, provider subtrees are built from descriptors; if
  not, `orun integrations <provider> …` falls back to today's
  capability-driven behavior and hints at `orun integrations sync`.
  Cobra construction stays cheap (descriptor parse only — no network at
  init; the read happens on `sync`, on cache miss during `integrations`
  listing, or lazily on first provider invocation).
- **Standard verbs** are rendered from capabilities even when a manifest
  omits explicit entries (server may also serve them explicitly; explicit
  wins): `connections list|get|revoke` (core), `health` (core),
  `templates list` (`credential-broker`), `secret create` (`secrets`),
  `credentials list|revoke` (`credential-broker`),
  `sandboxes list` (`provision`).
- Help/completion/typo-suggestion: descriptors carry summaries and arg
  help; the `unknownSecretsSubcommand` suggestion UX generalizes to
  `unknownIntegrationVerb` (Levenshtein over the rendered tree).
- Tenancy/auth: unchanged — `--org/--workspace` precedence, `TokenSource`
  from `internal/remotestate`. Dormant/roadmap providers list with their
  status and render no tree.

## 3. Invoke mapper (ICL2) — the security boundary

```go
// internal/integrationscli/ops.go — THE allowlist. Adding an op is a code
// change with review; descriptors can only select from it.
var ops = map[string]opSpec{
    "config.createBrokeredSecret":  {fn: invokeCreateBrokered,  plane: planeConfig},
    "config.createRotatedSecret":   {fn: invokeCreateRotated,   plane: planeConfig},
    "config.listSecretsByProvider": {fn: invokeListSecrets,     plane: planeConfig},
    "integrations.listConnections": {fn: invokeListConnections, plane: planeIntegrations},
    "integrations.getConnection":   {fn: invokeGetConnection,   plane: planeIntegrations},
    "integrations.revokeConnection":{fn: invokeRevokeConnection,plane: planeIntegrations, confirm: true},
    "integrations.connectionHealth":{fn: invokeHealth,          plane: planeIntegrations},
    "integrations.listTemplates":   {fn: invokeListTemplates,   plane: planeIntegrations},
    "integrations.listMinted":      {fn: invokeListMinted,      plane: planeIntegrations},
    "integrations.revokeMinted":    {fn: invokeRevokeMinted,    plane: planeIntegrations, confirm: true},
    "integrations.listSandboxes":   {fn: invokeListSandboxes,   plane: planeIntegrations},
}
```

- Each `fn` builds a typed request from the bind map + parsed args, calls
  through `internal/configsurface` (extended with the missing wire methods —
  connections/templates/minted reads all exist server-side today), decodes
  typed errors, and renders.
- Invariants inherited verbatim: **no request or response ever carries a
  secret value**; error paths never embed values; mutating ops
  (`confirm: true`) prompt unless `--yes`; every call is org-scoped by the
  standard tenancy resolution.
- Output: human table by default (the `orun secrets list` conventions),
  `--json` for machine consumption, exit codes uniform with the rest of the
  binary.
- Golden tests: SP5's `secret create` flows re-run through the mapper and
  must produce byte-identical requests and terminal output (the
  no-regression bar), including the `--from-broker` deprecation message.

## 4. Cache (ICL0)

- Location: `.orun/integrations/registry-<org>.json` (+ etag sidecar),
  written on `sync` and on any successful live read; 24h soft TTL — stale
  cache still renders help but prints a one-line staleness note on
  invocation; `sync` forces refresh.
- The cache is *presentation* state only: execution always revalidates
  server-side (the server rejects a verb/template that no longer exists —
  typed error, rendered with the `sync` hint).
- No cache → no tree: identical UX to today plus the hint. Never a baked-in
  provider list (the SP-A5 rule, applied to Go).

## 5. Native-extension seam (ICL3)

```go
// internal/integrationscli/extensions.go
func RegisterExtension(provider string, cmd *cobra.Command)
```

- Registered from `cmd/orun` wiring (same injection style as the
  materialize `Registry`); mounted under the provider subtree after served
  verbs. Path collision → served wins, extension is not mounted, a debug
  log names the shadowing (server truth is never silently overridden).
- Extensions are for *local* behavior only (recipe printers, verify-token
  helpers, file emitters). An extension that needs cloud data goes through
  `configsurface`/`remotestate` like everything else; extensions never
  embed provider SDKs — the `workflowbackend/arch_test.go` ecosystem-literal
  invariant is extended to `internal/integrationscli` (provider API
  hostnames are forbidden; the one grandfathered exception remains
  `internal/cloudflare`, which belongs to `orun backend`/materialize, not
  to this surface).

## 6. What deliberately does NOT change

- `orun secrets` — the substrate lens (view/manage/static create) is
  untouched; ownership split stays exactly as SP5 shipped it.
- The materialize adapter registry and delivery-target validation.
- The agent sandbox env-injection contract (`command_agent_serve.go`).
- The resolve wire shape and the CLI's zero-knowledge of secret values.
- Static command registration for everything non-integration
  (`commands_root.go` stays the single authoritative list; the dynamic
  layer lives entirely under the `integrations` subtree).
