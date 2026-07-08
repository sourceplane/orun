# orun-agents — Data Model (the object kinds)

Status: Draft (normative once AG0 lands)

> This is the heart of the epic: **agent types and agent sessions are
> content-addressed objects in the same graph as sources, catalogs, and
> specs.** Everything here mirrors `specs/orun-object-model/` (the L0 store,
> canonical JSON, framing, refs) and the `SpecSnapshot` seal/pull pattern in
> `specs/orun-work/data-model.md` §8. No new persistence stack; three new node
> kinds and one new ref namespace.

All records are canonical JSON (`object-store.md` §3), framed and addressed as
`blob`/`tree` per §2, `sha256` in v1. Refs live under `refs/agents/…`
(`object-store.md` §6). Identity is content: the same bytes always yield the
same id, dedup across the workspace, GC by reachability.

---

## 1. The doc-blob spine (why `.md` fits the graph unchanged)

orun already stores documentation bodies as content-addressed blobs and
points at them by hash — a `Spec`'s doc is `docRef: "sha256:…"`
(`orun-work/data-model.md` §2, P-4: "doc bodies content-addressed verbatim").
An agent-type `.md` is the same shape of thing: a markdown body with a typed
header. So it stores the same way:

- The **persona body** (the markdown below the frontmatter) → a `blob`,
  addressed as `bodyRef: "sha256:…"`. Verbatim; byte-identical personas dedup.
- The **capability frontmatter** (YAML) → parsed into the typed
  `AgentTypeSnapshot` envelope (§2). It is *not* stored as raw text — it is
  normalized to canonical JSON so two files that differ only in key order or
  whitespace seal to the same id.

This is the identical split specs use (doc body = blob; envelope = typed
node). An `agents/` tree is therefore just another authored doc tree the
object graph already knows how to hold.

---

## 2. `AgentTypeSnapshot` — `agent-type.json`

The sealed, frozen definition of an agent type. A `tree` node whose entries
point at the record blob, the persona body blob, and the resolved base-literacy
blob — so a `pull` fetches the whole closure and the type is fully
self-describing offline.

```jsonc
{ "kind": "AgentTypeSnapshot", "apiVersion": "orun.io/v1",
  "name": "implementer",                       // human key, unique per workspace
  "harness": "claude-code",                    // the AgentDriver id (§ driver registry)
  "model": "claude-opus-4-8",
  "runtime": { "effort": "high", "temperature": 0, "maxTokens": 64000,
               "contextBudget": 200000 },       // the model-tuning surface
  "autonomyDefault": "assist",                  // manual|assist|auto-dispatch|full
  "tools": {                                    // capability contract — deny-by-default
    "allow": ["work_query","work_get","spec_get","catalog_get_component",
              "catalog_affected","task_comment"],
    "ask":   ["contract_propose","task_assign"],
    "deny":  ["*"] },
  "mayAffect": ["sourceplane/orun-cloud/billing-*"],   // component-key globs; blast-radius ceiling
  "secrets": { "use": ["secret://*/billing/*"] },      // Layer-2 SecretPolicy pin (orun-secrets)
  "owner": "sourceplane/team/payments",         // responsible owner — MANDATORY (membership subject)
  "extends": "base-orun-literacy@sha256:…",     // resolved base-literacy id (§4)
  "bodyRef": "sha256:…",                        // persona markdown blob (§1)
  "catalog": "sha256:…" }                        // OPTIONAL: CatalogSnapshot the mayAffect globs resolved against
```

Rules:

- **`owner` is mandatory** (the work-plane rule, adopted here): an agent type
  with no responsible owner cannot seal (`ErrInvalid`). Accountability is in
  the object, not a side table.
- **`name` uniqueness** is per workspace; the ref (§5) enforces it. Renames
  mint a new ref chain and are visible in history — they never rewrite ids.
- **The envelope carries no persona text and no secrets values** — only the
  `bodyRef` hash and `secret://` *references*. A sealed agent type is safe to
  sync anywhere (it is the `orun-secrets` SD-1 carve-out, inherited).
- **Frontmatter → envelope is a total, canonical mapping** (`agent-type-format.md`
  is the schema); unknown keys fail the seal (closed schema, forward-compatible
  via `apiVersion`).

### 2.1 Tree layout

```
AgentTypeSnapshot (tree)
├── agent-type.json   (blob)  the §2 record
├── body.md           (blob)  the persona, verbatim  → bodyRef
└── base-literacy.md  (blob)  resolved base context   → extends
```

Identity = the tree's Merkle root. Change the persona → new `bodyRef` → new
tree id → new sealed version, prior version still addressable. This is exactly
how a `SpecSnapshot`'s doc closure works.

---

## 3. `AgentSessionSnapshot` — `agent-session.json`

A sealed agent run: the **system of proof** for what an agent did, pinning
every input by hash. Extends the source→catalog→spec→task Merkle chain through
the agent to the PR.

```jsonc
{ "kind": "AgentSessionSnapshot", "apiVersion": "orun.io/v1",
  "sessionId": "as_7f3c…",                     // ULID-ish; also the DO key in cloud
  "runKind": "implementation",                 // design|implementation|interactive|fix
  "agentType": "sha256:…",                     // AgentTypeSnapshot id @ dispatch (frozen)
  "brief": "sha256:…",                         // AgentBrief id (§3.1)
  "workRef": "work://sourceplane/ORN-142@sha256:…",  // task + pinned SpecSnapshot (orun-work §8.3)
  "catalog": "sha256:…",                       // catalog the run resolved affects against
  "principal": "sp_…",                          // who it acted as (cloud); "usr_…" when local
  "segments": ["sha256:…","sha256:…"],         // AgentSessionSegment chain (§3.2)
  "transcript": "sha256:…",                    // tree of content-addressed transcript chunks
  "outcome": { "status": "completed",
               "pr": "https://github.com/…/pull/412",
               "branch": "agent/ORN-142-lease-sweep",
               "startedAt": "…Z", "endedAt": "…Z",
               "tokens": 481233, "sandboxMinutes": 12 } }
```

The snapshot embeds **no lifecycle and no gate verdicts** — those live in the
work plane's observation log, derived. The session records what the *agent*
did (its inputs by hash, its event segments, its PR); whether that PR reached
Done/Released is the fold's job. Same discipline as `SpecSnapshot` pinning
cursors instead of embedding fold output.

Ref: `refs/agents/sessions/<ws>/<sessionId>`. `orun agent replay
<session>@<hash>` fetches the closure and re-renders the transcript offline —
byte-identical, because it is content.

### 3.1 `AgentBrief` — `brief.json`

The frozen input an agent runs from — sealed **before** the driver launches, so
a local run and a cloud run from the same `brief` id are the same run.

```jsonc
{ "kind": "AgentBrief", "apiVersion": "orun.io/v1",
  "runKind": "implementation",
  "spec": "sha256:…",            // SpecSnapshot (orun-work §8.1) — doc + task envelopes + contracts
  "task": "ORN-142",             // the atom this run targets (design runs omit)
  "affected": "sha256:…",        // a sealed AffectedSet blob: catalog affected over the task's affects[]
  "literacy": "sha256:…",        // base-literacy blob (redundant pin for offline determinism)
  "instructions": "sha256:…" }   // the assembled system prompt blob (literacy + persona + contract, rendered)
```

The `affected` pin is `internal/affected`'s output frozen as content — the
design run's blast radius and the implementation run's context, reproducible.

### 3.2 `AgentSessionSegment` — the tamper-evident event log

The session event stream seals as chained segments, exactly like
`WorkCoordinationSegment` (`orun-work/data-model.md` §8.2):

```jsonc
{ "kind": "AgentSessionSegment", "fromSeq": 0, "toSeq": 512,
  "sessionId": "as_7f3c…",
  "entries": [ /* closed-vocabulary session events (§3.3) */ ],
  "prev": "sha256:…" }          // chain → tamper-evident end to end
```

Bulk payloads (harness output, tool results) are `transcript` chunks
(content-addressed blobs); segment entries carry refs + small metadata, so
segments stay bounded. The `prev` chain means an audited session cannot be
silently rewritten.

### 3.3 Session event vocabulary (closed)

`state_changed · harness_event · message_user · message_agent · tool_call ·
tool_result · approval_requested · approval_resolved · artifact_produced ·
cost_sample · error`. CHECK-enforced (cloud, in `session_events`) and
schema-enforced (orun, in the segment encoder). Note what is **absent**: no
`status_asserted`, no `lifecycle_set` — the honesty invariant is enforced at
the vocabulary level, not by vigilance.

---

## 4. Base literacy — orun understanding as a versioned object

`base-orun-literacy` is a blob shipped **with the binary** (embedded, versioned
by orun release) containing the durable agent-facing understanding of orun: the
object model, the catalog/affected semantics, the MCP tool surface, and the
invariants (chiefly: *lifecycle is derived; you have no status-write tool;
ambiguity over-reports, never drops*). `orun agent context` prints it;
`orun agent context --seal` stores it and returns its id.

An `AgentTypeSnapshot.extends` pins a base-literacy id. Effect: **an agent's
understanding of orun tracks the orun version it runs inside** — upgrade the
binary, re-seal the type, the literacy advances; the persona `.md` never
restates orun mechanics and never rots. The base literacy is also a catalog
entity (kind `AgentLiteracy`) so "which literacy version did this run use?" is
a graph query, not a guess.

---

## 5. Refs

| Ref | Points at | Notes |
|---|---|---|
| `refs/agents/types/<ws>/<name>/latest` | newest `AgentTypeSnapshot` | history = prior objects; `@<hash>` pins a version for dispatch |
| `refs/agents/sessions/<ws>/<sessionId>` | the `AgentSessionSnapshot` | one per run; immutable once sealed |
| `refs/agents/literacy/<version>` | the `base-orun-literacy` blob | version = orun release; append-only |

Routed under the workspace `ws_` identity (not slugs — renames must not break
the chain), mirroring `orun-work` §8. Remote sync is `objremote`
set-difference: push the agent-type/session objects the remote lacks, move the
ref — the same "git push / Nix cache" substitution the whole graph uses.

---

## 6. Catalog projection

A sealed `AgentTypeSnapshot` projects into the catalog through
`internal/objcatalog` (the seam every content kind uses), emitting:

- an **entity** `AgentType/<name>` with facets `harness`, `model`,
  `autonomyDefault`, `owner`, and a resolved-persona summary;
- edges: `ownedBy` (→ the membership subject), `mayAffect` (→ each component
  matched by the `mayAffect` globs — a typed graph edge, so blast radius is
  queryable), and `usesLiteracy` (→ the `AgentLiteracy` version);
- for sessions: `AgentSession/<id>` with `ranType` (→ the AgentType@hash),
  `implementedBy`-style `producedPR`, and `actedAs` (→ the principal).

So the catalog answers, with no new API: *"what agent types does this
workspace define, who owns them, which components may each touch, and what did
they run against?"* Agents become as discoverable and governable as services —
which is the whole point of putting them in the object graph rather than a
config file. The projection obeys `18-state` (cloud) / the catalog invariant:
it is **derived from sealed git-authored content**, never console-authored.

---

## 7. The full provenance chain (what this buys)

```
SourceSnapshot → CatalogSnapshot → SpecSnapshot → task contract
      → AgentBrief(spec@h, affected@h, literacy@h)
      → AgentSessionSnapshot(agentType@h, brief@h, segments…, transcript@h)
      → PR → gate_result (observed) → revision_live (observed)
```

Every hop is content-addressed. Given a live revision you can walk back to the
exact agent type, the exact brief, the exact catalog, and the exact spec that
produced it — a Merkle chain from intent to production, **agents included**.
No other coding-agent framework has this, because no other one stores the
agent's inputs and actions as content in the same graph as the code's own
provenance. This is the moat the object model was always going to enable; this
epic spends it.

---

## 8. Implementation seam & the two-mechanism landing

orun has **two** ways content becomes an object, and agent types use both — for
different jobs. This matters because one is shipped today and one rides
`orun-work` WP4.

| | Catalog **entity kind** (shipped) | Sealed **snapshot** (rides WP4) |
|---|---|---|
| Pattern | doc-blob + `entities/<Kind>/<name>.json` in the catalog tree | `SpecSnapshot`-style typed node under `refs/…` |
| Gives | **discoverability now** — projection is automatic | **dispatch pin + `pull @<hash>`** — frozen, portable runs |
| Code today | `internal/objcatalog.readEntities` decodes any `entities/<Kind>/` blob generically → `CatalogView.Entities`/`CountsByKind`, no reader change | `objremote.Pull` is kind-agnostic; the seal/`spec pull` machinery is specced (WP4), not yet in Go |

**Landing path (AG1):**

1. **Entity-kind route first (projection, ships immediately).** Add
   `EntityKindAgentType` to `internal/nodes/assemble.go`; the resolver emits
   agent types as `nodes.Entity{Kind:"AgentType"}` via
   `CatalogSnapshot.DeclaredEntities` (the seam the `Repo` self-entity uses),
   with the persona in `PendingDocs`. `AssembleCatalog`/`assembleEntities`
   then write `entities/AgentType/<name>.json` + the persona blob under `docs/`
   into the catalog Merkle id; `objcatalog.readEntities` projects them with
   **no reader change** (§6). Agent types are discoverable, owned, and
   `mayAffect`-linked the moment they're authored.

2. **Snapshot route for dispatch (co-lands with WP4).** The
   `AgentTypeSnapshot`/`AgentBrief`/`AgentSessionSnapshot` nodes (§2–§3) follow
   the `SpecSnapshot` shape and reuse the **same** unbuilt seal/pull plumbing
   `orun spec pull` needs — so AG1's snapshot half and WP4 share one
   implementation (`internal/nodes` kind + `Validate` + `Assemble*` + pure id
   in `ids.go` + `nodewriter.Write*` + a thin `cmd/orun` pull command). Sync
   and GC are **free**: `objremote.Sync/Push/Pull` and `internal/objgc` operate
   on any ref closure by content hash, unchanged.

Exact touch-points (from the object-model packages): kind discriminator in
`internal/nodes/kinds.go`; schema in `model.go`; `Validate()` in `validate.go`
(assert kind, key grammar, `validID(bodyRef)`, and **exclude** annotation
fields like timestamps from identity — the identity-purity invariant); writer
in `internal/nodewriter`; refs via `internal/objectstore/refstore` CAS. The
single approved encoder is `nodes.Encode` → `objectstore.CanonicalEncode`
(ad-hoc `json.Marshal` of a record is lint-banned), which is what guarantees
two `.md` files differing only in key order seal identically.

**Identity vs. annotation (decide consciously):** `bodyRef`, `harness`,
`model`, `runtime`, `tools`, `mayAffect`, `owner`, `extends` are **identity**
(re-tuning the model is a new version — correct). Session `outcome.tokens`,
`sandboxMinutes`, and wall-clock timestamps are **annotation** and must stay
out of the hashed bytes, exactly as `SourceSnapshot` excludes `resolvedAt` and
`PlanRevision` excludes trigger/timestamp fields (`validate.go`).
