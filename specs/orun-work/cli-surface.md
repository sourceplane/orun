# CLI Surface

> The work plane is console-first; the CLI is deliberately small: pull frozen
> briefs, peek at work, import the spec tree. CLI mutation goes through the
> same mutator surface (`actor.via: cli`) and stays minimal. Output follows
> cockpit conventions (`internal/cockpit`): human rendering by default,
> `--json` through the same viewmodels.

## 1. `orun spec pull`

Fetch a sealed `SpecSnapshot` (and its object closure) into the local store —
the human/agent handshake for "implement against exactly this".

```sh
orun spec pull sourceplane/specs/orun-work                     # latest sealed
orun spec pull sourceplane/specs/orun-work@sha256:9f86d0…      # frozen (dispatch)
orun spec pull sourceplane/specs/orun-work --quiet --id-only   # scripting
```

- Set-difference pull via `internal/objremote`; already-present objects are
  never re-fetched.
- Materializes a read-only view under `.orun/specs/<slug>/`: the doc, task
  contracts, and the `affects` component-subgraph summary. Editing a pulled
  snapshot is meaningless by construction (intent mutates through mutators;
  facts are not in the snapshot at all).
- `--catalog` additionally pulls the pinned `CatalogSnapshot` so `affects`
  keys resolve identically offline.
- Exit codes: `0` ok; `4` unknown spec/ref; `5` auth/scope; `7` remote
  unreachable (structured under `--json`).

## 2. `orun work`

```sh
orun work list --spec orun-work --lifecycle in_review,-released --assignee @me
orun work view ORN-142        # envelope + contract + derived lifecycle w/ evidence
orun work comment ORN-142 "parity fixture needs the WO3 corpus"
```

- `list`/`view` read the fold query API; every rung prints with its evidence
  ("In Review — PR #412 open; gate `parity` red"). Offline they degrade to
  the last pulled snapshots with a staleness banner (intent only — no
  lifecycle offline, honestly rendered as such).
- CLI mutation is `comment` + `assign` only. There is no
  `orun work status`: lifecycle is not authored (WP-3). Pins are
  console-only, deliberately — an override should cost a click and be seen.

## 3. `orun work import` (dogfood path, WP0)

```sh
orun work import specs/ --workspace sourceplane --dry-run
```

Parses the repo's spec tree — epic `README.md`s → `Spec`s (doc bodies
content-addressed verbatim, P-4), `implementation-plan.md` milestones →
`Task`s with contracts (`Goal/Deps/Done when/Design refs` → `contract.*`) —
writing ordinary `item_created`/`contract_edited` events with `via: import`.
`--dry-run` prints the mapping without writing. `IMPLEMENTATION-STATUS.md`
tables are **not** imported as status: lifecycle derives from real
observations, which is the point — the first demo is the imported tree's
lifecycle lighting up from git/GitHub history alone.

## 4. Non-goals (CLI)

No board/TUI (the cockpit may grow a work pane later); no contract editing
from the CLI; no offline mutation queue; no lifecycle mutation anywhere
(there is none to mutate).
