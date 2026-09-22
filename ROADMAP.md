# Roadmap

This roadmap describes the direction of orun over the next few releases. It is
a statement of intent, not a commitment: items move as we learn. Every item
here traces to a design spec under [`specs/`](specs/), which is the detailed
record of what is planned and what has landed.

Dates are indicative. The project ships continuously; a minor version is cut
whenever a milestone lands.

## Recently shipped (v2.55 to v2.58, September 2026)

- **The baseline registry from the command line.** `orun baseline list`,
  `show`, `check`, `new --local`, `new --via-platform`, `register`, and
  `publish`. A registered baseline such as `cirrus` can be built into a fresh
  workspace on the platform or into a directory on your machine.
  Spec: `specs/orun-bootstrap-engine/`.
- **The bootstrap engine.** Blueprint phases gained typed hook actions,
  preconditions, waits, and derived phase state, so a build can stop, resume,
  and report itself from a fresh container.
- **The task plane.** `orun task` with epics and milestones, `orun spec`,
  and the same tools on the orun MCP. Spec: `specs/orun-baseline-tracking/`.
- **The provenance pen.** `orun pr open`, `check`, and `land` carry a task's
  lineage on every pull request.

## Now (v2.59 onward)

- **Bootstrap hardening.** Finish the long tail of a platform build: retries
  on dropped GitHub reads, resumable landings, clearer waiting states, and
  inputs filled from the workspace. Tracked as follow-ups in
  `specs/orun-bootstrap-engine/`.
- **Workflows v3: one language, one binary.** Fold the standalone workflow
  engine into orun's own step vocabulary so a workflow is an orun document,
  not a second dialect. Spec: `specs/orun-workflows-v3/`.
- **Native coordination.** Move the remote-state client from a REST claim
  dialect to an append-events, read-the-log client over the content-addressed
  store, with content-addressed job results. Spec:
  `specs/orun-native-coordination/`.
- **Documentation parity.** Every command has a reference page, every
  release has notes, and the site is rebuilt on each release. This roadmap
  entry is itself a promise to keep the documentation current.

## Next

- **Grounded sessions.** `orun agent serve` starts an agent session inside a
  real checkout of a linked repository, on a task-keyed branch. Spec:
  `specs/orun-grounded-sessions/`.
- **Cockpit v2 as the terminal head of the cloud.** `orun tui-next` graduates
  from preview once it covers the surfaces of `orun tui`. Spec:
  `specs/orun-tui-v2/`.
- **Scorecards.** Versioned scorecard definitions evaluated over the catalog
  entity envelope and live execution data, producing derived maturity levels.
  Spec: `specs/orun-scorecards/` (draft, not yet scheduled).
- **Windows builds.** Release artifacts for `windows/amd64` alongside the
  existing Linux and macOS builds.

## Later and under consideration

- **Compositions authored as `intent.yaml`.** Collapse the last naming
  distinction between platform intent, component intent, and golden-path
  intent.
- **Affected-set worker.** Serve change detection from the platform rather
  than recomputing it in every CI job. Spec: `specs/orun-affected-worker/`
  (under review, not a current requirement).
- **Additional runners.** Beyond local, Docker, and GitHub Actions.

## How to influence the roadmap

Open a [feature request](https://github.com/sourceplane/orun/issues/new?template=feature_request.yml)
or start a [discussion](https://github.com/sourceplane/orun/discussions).
Larger proposals become specs; see [CONTRIBUTING.md](CONTRIBUTING.md).
