# Risks & Open Questions

> The living register for `orun-work`. Decisions are WD-n (locked in
> `README.md`/`design.md` §8); open questions Q-n (sharpness register,
> `design.md` §11, expanded here); deferred items L-n; gaps G-n.

## Decision ledger (summary — full table in `design.md` §8)

WD-1 hot-state-never-content · WD-2 event-sourced with actor provenance ·
WD-3 one write path · WD-4 catalog edge grammar + `affected` reuse ·
WD-5 task contract from the spec-milestone convention · WD-6 status from
delivery truth · WD-7 DB authoritative, seals are projections · WD-8 DO-based
sync over the provisioned backend · WD-9 ULIDs + Linear-style human keys ·
WD-10 agents are principals behind the MCP.

## Open questions

**Q-1 (D1 write throughput).** Event-append + projection-update per mutation on
a hot org vs D1 limits.
- Mitigations available without schema change: per-project DO serialization
  (already the design), batched comment/typing events, org-level DB sharding.
- Resolution path: W1 soak rig records p95 echo + sustained events/sec; a
  budget is locked before W2.

**Q-2 (how much sync engine do we own).** Presence, conflict UX, offline depth
can grow until the DO protocol rivals an off-the-shelf engine (Zero/Replicache).
- Locked: the *mutator + verdict contract* is engine-agnostic; the W1 client
  hides transport behind it.
- Trigger to revisit: if W1's soak misses the latency bar or the protocol's
  maintenance cost dominates a quarter, swap the transport, keep the contract.

**Q-3 (gate verification source).** "Gates green" must come from orun execution
truth, not re-derived GitHub statuses — but the mapping from `contract.gates`
names to runs/checks recorded in the backend needs fixing in W3.
- Risk if sloppy: Done automation either lies (status-based) or never fires
  (unmatchable names). The W3 fixture must include a gate that GitHub reports
  green but orun has no record of → task parks, surfaced.

**Q-4 (markdown round-trip on import).** Imported epic docs must survive
import → seal → pull byte-identical, or diffing against the source tree breaks
and trust in the import dies.
- Locked: doc bodies are stored and sealed verbatim (no normalization); golden
  fixtures over this repo's own specs.

**Q-5 (contract `affects` drift).** Component keys rot when components rename
or move repos.
- Locked: validation at edit time against the current catalog; existing links
  degrade to `unresolved` (rendered, never dropped); the drift inbox lists
  epics with unresolved links after a catalog change.

**Q-6 (epic doc authoring vs git review).** Some teams will want design prose
to stay PR-reviewed in git while tracking lives in the work plane.
- Position: supported by construction — an epic's `doc` may be a thin pointer
  (`designRefs`) at in-repo documents; the import path proves the inverse
  direction. No forced migration of prose.

**Q-7 (multi-repo projects).** A project spanning repos means `affects` keys
from multiple catalogs.
- v1: one catalog per project (the common case); the snapshot records which
  (`SpecSnapshot.catalog`). Multi-catalog joins are deferred (L-4).

## Risk register

| Risk | Severity | Mitigation |
|---|---|---|
| The UI ships before the bridge and reads as "another tracker" | High (product) | W2/W3 are the differentiation releases; Released + drift inbox lead the narrative; W0/W1 are deliberately invisible substrate |
| Automation mis-moves erode trust faster than manual rot | High | Invariant 5 (conservative automation), every auto-move attributed + one-click revert (an event, not a delete) |
| Two stores drift (D1 vs seals) | Medium | Seals carry `ledgerSeq`; `orun fsck`-style verification walks the segment chain against D1 ranges |
| DO/WebSocket protocol becomes a second product | Medium | Q-2 seam + revisit trigger |
| Agent writes spam or corrupt planning state | Medium | §4 guardrails in the mutator, rate/scope limits, contract-propose flagging |

## Deferred register

- **L-1 (Agents section).** Dispatch-on-assign, fleet view, agent activity UI —
  rails land in W0/W4/W5 (`agents-and-mcp.md` §5); the section is its own epic.
- **L-2 (collaborative doc editing).** CRDT (Yjs-class) for epic doc bodies
  only — never the structured model. Until then: last-write-wins + edit events.
- **L-3 (cockpit work pane).** A TUI work view over the same query API; v1 CLI
  stays read-leaning.
- **L-4 (multi-catalog projects).** Cross-repo `affects` resolution (Q-7).
- **L-5 (external tracker adapters).** Linear/Jira import for adoption; after
  W6 proves the native path.
- **L-6 (cycles analytics).** Velocity/burnup projections over the event log;
  the log already carries everything needed.

## Gaps

- **G-1 (notification delivery).** The drift inbox and review requests need a
  channel (email/Slack/in-app). The event log is the source; delivery is
  unspecified — owned by the SaaS portal epic, consumed here.
- **G-2 (permissions model depth).** v1 scopes are project-level
  (viewer/member/admin) on the backend's GitHub identities; per-epic or
  per-team write fencing is unspecified and may ride the service-catalog
  `Group` kind when it lands.
