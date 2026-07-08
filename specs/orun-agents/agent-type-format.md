# orun-agents — Agent-Type File Format (`agents/*.md`)

Status: Draft (normative once AG1 lands)

An agent type is a single markdown file in the repo's `agents/` tree —
git-authored, version-controlled, and sealed to an `AgentTypeSnapshot`
(`data-model.md` §2). It has **two halves in one file**:

- **Capability** (YAML frontmatter) — the *policy* contract. Parsed into the
  typed envelope, canonicalized, enforced by orun locally and re-enforced by
  RBAC in the cloud. Closed schema; unknown keys fail the seal.
- **Character** (markdown body) — the *persona*. Stored verbatim as a content-
  addressed blob (`bodyRef`). Portable and shareable; carries no policy weight.

This mirrors the existing convention exactly — `agents/orchestrator.md` is the
prototype; this format generalizes it so every such file is sealable and
catalog-visible.

---

## 1. The file

```markdown
---
name: implementer                 # required; unique per workspace; [A-Za-z0-9._-]+
kind: agent-type                  # required; discriminates the file
apiVersion: orun.io/v1            # required

harness: claude-code              # required; an AgentDriver id (see driver registry, design §3)
model: claude-opus-4-8            # required; passed to the driver
runtime:                          # optional; the model-tuning surface
  effort: high                    #   low|medium|high|xhigh|max (driver-mapped)
  temperature: 0
  maxTokens: 64000
  contextBudget: 200000

autonomyDefault: assist           # manual|assist|auto-dispatch|full (design §8, cloud epic)

tools:                            # capability contract — deny-by-default
  allow: [work_query, work_get, spec_get, catalog_get_component, catalog_affected, task_comment]
  ask:   [contract_propose, task_assign]
  deny:  ["*"]

mayAffect:                        # component-key globs; the blast-radius ceiling
  - sourceplane/orun-cloud/billing-*
secrets:                          # optional; Layer-2 SecretPolicy pin (orun-secrets)
  use: [secret://*/billing/*]

owner: sourceplane/team/payments  # required; a membership subject (usr_/sp_/team_)
extends: base-orun-literacy       # optional; defaults to the binary's base literacy
---

# Implementer

You take **one Ready task** to a merged-quality PR. You are handed a frozen
brief — the task contract (goal, affects[], doneWhen[], gates[]) and the
affected component subgraph — and you implement against exactly that.

You do not, and cannot, assert progress: there is no status tool. You *do the
work* — push a branch that carries the task key, open one PR, comment your
reasoning — and let the observation log move the rung. When a gate is red, you
read the run evidence and fix; you do not argue with it.

Respect the blast radius: touch only components in your `affects`. If the work
needs a component outside it, say so in a comment and stop — do not widen scope
silently.
```

---

## 2. Field reference

| Field | Req | Identity? | Meaning |
|---|---|---|---|
| `name` | ✓ | ✓ | human key, unique per workspace |
| `kind` / `apiVersion` | ✓ | ✓ | schema discriminator + version |
| `harness` | ✓ | ✓ | the `AgentDriver` (Claude Code first) |
| `model` | ✓ | ✓ | model id passed to the driver |
| `runtime.*` | – | ✓ | effort / temperature / maxTokens / contextBudget — the tuning surface |
| `autonomyDefault` | – | ✓ | default rung of the autonomy ladder (cloud enforces) |
| `tools.{allow,ask,deny}` | ✓ | ✓ | MCP tool policy; deny-by-default; `ask` → approval |
| `mayAffect` | – | ✓ | component-key globs; hard blast-radius ceiling |
| `secrets.use` | – | ✓ | `secret://` reference globs the type may resolve |
| `owner` | ✓ | ✓ | responsible owner; **seal fails without it** |
| `extends` | – | ✓ | base-literacy id; defaults to the binary's |
| *body* | ✓ | ✓ (`bodyRef`) | the persona, verbatim blob |

Everything above is **identity** — re-tuning the model or widening `mayAffect`
mints a new sealed version (correct: a different capability is a different
type). Runtime annotations (a session's token count, wall-clock) are never in
the file and never in identity.

---

## 3. Resolution order (fine-tuning without forking the file)

Effective config at run time, most-specific wins:

```
runtime flag  >  workspace profile override  >  agents/<name>.md  >  base defaults
```

- `agents/<name>.md` is the **authored default** — the version-controlled truth.
- A **workspace profile override** (cloud, an `agent_profiles` row) may narrow
  (never widen) `tools`/`mayAffect`/`secrets` and may retune `model`/`runtime`
  for that workspace without editing the repo file. Narrowing-only keeps the
  git file the ceiling.
- A **runtime flag** (`orun agent run --model … --effort …`) is a one-shot
  override for local experimentation; it cannot widen `tools`/`mayAffect`
  (those are enforced against the sealed ceiling).

This is how you "fine-tune the agent model": change `runtime`/`model` in the
file for a durable change (re-seals, new version, catalog shows it), or pass a
flag for a throwaway local run.

---

## 4. `extends` and base literacy

`extends` pins the orun-understanding layer (`data-model.md` §4). Omit it and
the type inherits the binary's current base literacy at seal time. The persona
body should therefore **never restate orun mechanics** — no "here is how the
object model works", no tool catalog. Write only the character and the
job-specific judgment; orun literacy is inherited and version-tracked, so it
stays correct as orun evolves. A file that duplicates literacy is an AG9
lint warning.

Custom literacy (a workspace with house rules) is itself a sealed blob under
`refs/agents/literacy/<name>` and referenced by `extends: <name>@<hash>` — so
even house rules are content-addressed and auditable.

---

## 5. Validation (seal-time, AG1)

`orun agent lint agents/` and the seal path enforce:

1. Required fields present; `name`/keys match `^[A-Za-z0-9._-]+$`.
2. `owner` resolves to a membership subject (or, offline, is well-formed).
3. `harness` is a registered driver; `tools.*` are known MCP tool ids.
4. `mayAffect`/`secrets.use` are well-formed globs.
5. Closed schema: unknown frontmatter keys → error (forward-compat via
   `apiVersion` bumps, never silent-accept).
6. The body is non-empty (a persona is required — an agent type with no
   character is a config error).

Seal is deterministic: two files differing only in YAML key order or
whitespace produce the **same** `AgentTypeSnapshot` id (canonical JSON,
`data-model.md` §8).
