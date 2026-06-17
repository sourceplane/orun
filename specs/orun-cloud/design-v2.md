# orun-cloud — Design v2 (CLI side)

> Status: Proposed (supersedes the OC5+ deltas of `design.md`). The client
> counterpart to `orun-cloud/specs/epics/saas-orun-platform/design-v2.md`.
> OC0–OC4 stay as the shipped substrate. Milestones here are **OCv2-1..3**.

## Thesis

v1 pointed `--remote-state` at a run-coordination server behind
`statebackend.Backend`. v2 re-anchors the client on orun's own **content-
addressed object model**: the cloud becomes a hosted `ObjectStore` + `RefStore`
+ index, and the CLI/TUI consume it through the *same* `ModelReader` /
`RunStarter` seam they already use locally (`specs/orun-object-model/
remote-and-consumers.md`). "Cloud" stops being a separate data path and becomes
a `RemoteStore` swapped in for the `LocalStore`.

## 1. The read seam: widen `bridge.Source` to `ModelReader`

Today `internal/cockpit/bridge` exposes a thin `Source` (`LoadRun`,
`ListRuns`), with `FromBackend` (remote) and `FromObjectReader` (local). Remote
`ListRuns` is a Phase-2 stub, and catalog/source selection has no remote path.

v2 widens the seam to the object model's `ModelReader`
(`ResolveRef/Catalog/Revision/Execution/ComponentHistory/ListExecutions`) and
adds `FromRemoteModel(remoteStore)`. Then:

- `orun tui --remote`, `orun status`, `orun logs --follow`, `orun catalog …`,
  and `orun log` render cloud data through the **same viewmodels** as local —
  source/head selection, catalog browse, and execution history included, with
  zero new render code.
- Local and cloud differ only in whether the seam holds a `LocalStore` or a
  `RemoteStore`; everything above is identical.

Run coordination (`statebackend.Backend`: Claim/Heartbeat/Update) stays for the
runner's distributed-execution path; its results seal into execution objects +
`refs/executions/*`, so reads are uniform.

## 2. Source/head selection = ref resolution

The CLI already resolves refs (`internal/objplan/refs.go`):
`refs/sources/{current,main,branches/<b>,prs/<pr>}`,
`refs/catalogs/{…}`, `refs/executions/{latest,live/<id>,by-id/<id>}`. Against a
`RemoteStore` these resolve server-side, so `orun status --remote --ref
prs/139`, `orun catalog tree --remote --ref main`, and the TUI source picker
work over the cloud's org-wide history (richer than the local checkout).

## 3. Tenancy declared in intent.yaml (DV5)

- Add `execution.state.org` (slug or `org_…`) — and optionally `project` — to
  intent.yaml. This is the **claimed target**, sent in the OIDC exchange body
  and the API-key request, checked server-side against the link/installation
  (⊆, never escalating). It disambiguates a repo linked across orgs and routes
  org-scoped keys.
- **This reverses `design.md:163`** ("org/project come from the RepoLink, never
  from intent"): intent declares the target; the server authorizes it.
- Scope precedence unchanged otherwise: `--org` flag → `ORUN_ORG` →
  `intent.execution.state.org` → cached `RepoLink`.

## 4. Credential-agnostic CI (DV4)

The three token sources are unchanged in shape; both CI credentials resolve to
one server-side `ActorContext{org, project}`:

- `OIDCTokenSource`: fix the audience default `"orun"` → **`"orun-cloud"`**
  (`internal/remotestate/auth.go:48`) and add the real call to
  `POST /v1/auth/oidc/exchange` (today it sends GitHub's raw JWT —
  `auth.go:59`). The server validates `repository_id ∈ installation` repos +
  per-link CI settings and returns a scoped token.
- `StaticTokenSource` (`ORUN_TOKEN=sk_…`): the non-GitHub-runner fallback;
  project-scoped key = repo-scoped, org-scoped key routes via intent.yaml.
- Selection order in CI is unchanged: OIDC if `ACTIONS_ID_TOKEN_REQUEST_URL`,
  else static, else session.

## 5. GitHub-native flows (client view)

With the GitHub App installed (server-side bridge,
`saas-integrations/bridge-to-state.md`), pushes auto-materialize source +
catalog in the cloud and PRs get Check Runs — no CLI step required. The CLI
stays the manual/local path and the escape hatch: `orun run --remote-state`,
`orun catalog push`, `orun push|pull|sync` reconcile with the same object graph
the App populates. Content addressing makes "the App pushed it" and "the CLI
pushed it" the same objects — dedup is global.

## 6. Milestones (CLI side, v2)

| ID | Milestone | Pairs with |
|----|-----------|------------|
| **OCv2-1** | Widen `bridge.Source` → `ModelReader`; `FromRemoteModel`; cloud source/head selection, catalog, history through shared viewmodels | OV1 |
| **OCv2-2** | intent.yaml `org`/`env`; `OIDCTokenSource` audience fix + real exchange call; intent-claim scope resolution | OV2/OV3 |
| **OCv2-3** | Object/catalog push into the hosted object graph (digest negotiation, heads) feeding the org-global catalog | OV4/OV6 |

OC6 (CI golden path) from v1 folds into OCv2-2 plus the server bridge. **OC5
(secrets) stays as-is** — the runner-resolve + redaction implementation slice of
the canonical `orun-secrets` epic (`specs/orun-secrets/`), which supersedes the
old OC5/§6 secret-store sketch. `orun backend init` OSS self-host stays parked
(D5).

## 7. Unchanged

Local-first forever; the degradation table (`design.md` §7); the frozen wire
identifiers (audience `orun-cloud`, issuer `https://api.orun.dev`); same digests
local and remote. v2 changes which *seam* the cloud presents (object model, not
just coordination), not the local-first contract.
