# orun-integrations-cli — Implementation Plan

Status: Draft. Pairs orun-cloud IR7 (`saas-integration-registry`); can land
against recorded fixtures before IR0 merges server-side.

## ICL0 — Registry client + cache

**Scope**
- `internal/configsurface`: `GetIntegrationRegistry(ctx, org)` →
  `[]IntegrationDescriptor` (ETag-aware); types per `design.md` §1.
- Cache read/write under `.orun/integrations/registry-<org>.json` + etag
  sidecar; 24h soft TTL; corrupt cache = cache miss (never fatal).
- `orun integrations sync` (force refresh, prints provider/verb counts).
- Fixtures: recorded IR0-contract responses for
  github/slack/cloudflare/supabase (+ dormant aws) driving all unit tests.

**Done when** sync writes a cache from fixtures; TTL/staleness notes render;
ETag round-trip verified; corrupt-cache recovery tested.

## ICL1 — Runtime renderer

**Scope**
- `internal/integrationscli`: descriptor → cobra subtree builder; standard
  verbs derived from capabilities (explicit server entries win); arg
  parsing per `CliArg` (positional/flag, enum validation, repeat, kv).
- `orun integrations` listing (name, category, connected count, status).
- Typo suggestions (`unknownIntegrationVerb`); dormant/roadmap rendering;
  no-cache fallback preserving today's behavior + `sync` hint.
- Unknown `Invoke.Op` → verb rendered disabled with "update orun" hint.

**Done when** the cloudflare fixture renders its full tree with
help/completion; a manifest-only fixture change (new verb on aws) appears
with zero Go changes; SP5's static path still works with no cache.

## ICL2 — Verb execution

**Scope**
- The op allowlist + invoke mapper; missing configsurface wire methods
  (connections list/get/revoke, health, templates list, minted list/revoke,
  sandboxes list — all existing server endpoints).
- Uniform output (tables, `--json`), typed-error decode, `confirm`-gated
  mutations with `--yes`.
- Golden tests: SP5 `secret create` (brokered + rotated + deprecation
  message) byte-identical through the mapper.

**Done when** every standard verb executes against fixtures; goldens pass;
no request/response path can carry a secret value (asserted by the existing
value-free test style); exit codes documented.

## ICL3 — Native-extension seam

**Scope**
- `RegisterExtension(provider, cmd)` + collision rule (served wins, debug
  log); wiring point in `cmd/orun`.
- First extension: `orun integrations cloudflare recipe` — prints the
  connect token recipe from the cached descriptor for air-gapped prep.
- Arch test: extend the ecosystem-literal invariant to
  `internal/integrationscli` (no provider API hostnames).

**Done when** the extension mounts under the served tree; a deliberate
collision fixture shows served-wins + log; arch test green.

## Sequencing

ICL0 → ICL1 → ICL2 → ICL3. Ship behind nothing — the dynamic layer is
strictly additive under `orun integrations`; rollback is deleting the cache.
