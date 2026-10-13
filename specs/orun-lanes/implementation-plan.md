# Implementation Plan — orun-lanes

Milestones LN0–LN7. Each is one reviewable pull request that leaves `orun`
releasable, and none changes behaviour for a repository that declares no lanes.
The order: **decide → declare → partition → automate → execute → surface on
GitHub → document and release → adopt in Cirrus.**

---

## LN0 — the design

**Scope.** This epic: README, design, this plan, the status file.
**Done when.** The epic is merged and every later milestone can point at a
section of design.md.

---

## LN1 — declare: the schema, validated, inert

**Scope.**
- `execution.lanes` in the intent model (design §3.1): `mode`, `maxLanes`,
  `concurrency`, `onCycle`, `groups`, `rules`.
- `spec.lane` on components, in `component.yaml` and inline intent components
  (§3.2).
- Validation at load:
  - unknown mode, a reserved or malformed group name, `spec.lane` naming an
    undeclared group;
  - a `rules[].when.triggerRef` missing from `automation.triggerBindings`;
  - `maxLanes` < 1 or `concurrency` < 1 when set.
- The intent JSON schema accepts the new keys.

**Not yet.** Nothing reads the configuration; plans are unchanged.
**Done when.** Valid configurations load and invalid ones fail with messages
naming the field; every existing test and golden plan is unchanged.

---

## LN2 — partition: explicit modes in the plan

**Scope.**
- Resolution (§3.3): rules on the active trigger, then annotations, then mode.
- Lane assignment for `job`, `component`, `env`, groups and `isolated`, under
  the invariants in §4 (environment boundary, `runsOn` boundary, `isolated`).
- `lanes[]` on the plan and `lane` on each job, with ids, names and order per
  §5.
- Cycle detection over enforced edges (§7): split by level with a warning, or
  reject under `onCycle: reject` / `--strict-lanes`.
- `orun plan --view lanes` (§10), and the one-line lane summary.
- Absence is invisible: no configuration → no `lanes` key (§4.6), proven by
  the existing golden plans staying byte-identical.

**Done when.** Each mode yields the documented partition on fixture intents;
a cycle fixture is split (and rejected under strict); plans without lanes are
byte-identical.

---

## LN3 — automate: `mode: auto`

**Scope.** Design §6:
- the small-plan pass-through;
- the (environment, `runsOn`) partition and the proportional budget;
- chain clusters, cut by level when oversized;
- longest-first packing with deterministic tie-breaks;
- merge repair for auto lanes in a cycle;
- auto names (§5).

**Done when.**
- A 49-job fixture shaped like Cirrus `04-workers` yields at most `maxLanes`
  lanes, with each migration→worker chain inside one lane.
- A 6-job plan is laid out as `job` mode.
- Repeated runs are byte-identical.

---

## LN4 — execute: `orun run --lane`

**Scope.** Design §8:
- `--lane` selection, falling back to `--job` on a lane-less plan; mutually
  exclusive with `--job`;
- the lane's concurrency;
- outside-lane upstreams awaited through the remote claim loop without holding
  a slot;
- the local-state refusal naming the lane to run first;
- `--retry` with `--lane`.

**Done when.**
- A two-lane plan with a cross-lane edge completes with remote state (the
  conformance backend), and refuses clearly with local state.
- `--retry --lane` re-runs only the lane's failed jobs.
- `--job` behaviour is unchanged.

---

## LN5 — surface: lane matrix, per-job checks, grouped logs

**Scope.** Design §9:
- `lane-matrix` and `lane-count` from `--github-output`;
- per-job check runs from multi-job lanes (best effort, `ORUN_CHECK_RUNS=0`);
- per-job `::group::` output when concurrency > 1;
- the lane summary in `$GITHUB_STEP_SUMMARY`.

**Done when.**
- `lane-matrix` on a lane-less plan equals one entry per job.
- Against a fake GitHub API, a 3-job lane creates three check runs named by
  `checkName` and completes them with the right conclusions.
- A posting failure only warns.

---

## LN6 — document and release

**Scope.**
- Concept page `website/docs/concepts/lanes.md`, and the intent and component
  reference.
- The CI recipe (the workflow above), and `orun plan --view lanes` and
  `orun run --lane` in the CLI reference.
- Release notes for the release that ships it, and the CHANGELOG row; then
  cut the release with `release-oci`.

**Done when.** The docs build, the release is published, and the release notes
name what is backward compatible and what needs `checks: write`.

---

## LN7 — adopt: Cirrus on `mode: auto`

**Scope (sourceplane/cirrus).**
- `intent.yaml`: `execution.lanes: { mode: auto, maxLanes: 16 }` with a
  `rules` entry keeping pull requests per job.
- `web-console-next`: `spec.lane: isolated`.
- `ci.yml`:
  - fan out over `lane-matrix`, run `orun run --lane`;
  - pin the released orun on both the `plan` and `run` jobs;
  - add `checks: write`.

**Done when.** The baseline's contract tests pass, and a product built from
the next baseline converges `04-workers` in a few lanes instead of 49 jobs.
