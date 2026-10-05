# orun-docs-discipline — Implementation Plan

Status: Normative for PD1–PD6. Ordered so the narrative lands first and
every later milestone fills in one pillar under it. Each milestone is one
pull request, merged to `main` and released before the next starts.

Every milestone is **done when**, in addition to its own criteria:

- `cd website && npm ci && npm run docs:build` succeeds. The config throws on
  broken links, duplicate routes, and broken Markdown links.
- No page URL changes (decision 4 in the README).
- Every page touched states only what the code does; a claim the review
  flagged as wrong is fixed in the milestone that touches its page.
- `go test ./...` is green when the milestone touches Go code.
- The release carrying the milestone has notes under
  `website/docs/release-notes/`, a CHANGELOG row, and a sidebar entry.
- `IMPLEMENTATION-STATUS.md` marks the milestone done, with its PR and release.

## PD1 — The narrative: tagline, landing page, information architecture

**Scope**

- `website/docusaurus.config.js`: tagline, meta description, navbar (pillar
  entries instead of Cockpit; Releases links to the newest notes), footer
  columns by pillar.
- `website/docs/intro.mdx`: rewritten. The hero is the tagline; four pillar
  cards (declare, package & evolve, ground agents, verify → plan → execute)
  replace the five (plan, cockpit, execute, catalog, agents); the loop diagram
  gains `validate` and the package and agent sides; the "why this design"
  block that duplicates `principles.md` goes; Orunbase shrinks to one section.
- `website/docs/overview/what-is-orun.md`: rewritten around the four pillars.
  Keep "the problem" and "what orun is not"; regroup "what you get" by pillar.
- `website/docs/principles.md`: reframed. Principles 1–4 stay; add "standards
  travel as versioned code" and "agents read the same intent"; demote the
  cockpit palette principle to a note under determinism of surfaces.
- `website/sidebars.js`: Concepts and CLI regrouped under the four pillars
  (plus "Platform and operations" for tenancy and context discovery, and
  "Cloud client" for the CLI). The duplicate bootstrap-guide entry goes.
  No file moves.
- `website/docs/cli/orun.md`: opens with the tagline and one sentence per
  pillar, the command map grouped by pillar (adds `orun work`), and a typical
  flow that shows the whole loop.
- `README.md`: header, "Why orun" and "What you get" rewritten by pillar; the
  parameter-precedence line corrected (environment, then group, then
  component); the release-notes link points at the newest notes.
- `cmd/orun/commands_root.go`: `Short` and `Long` carry the tagline.
- `website/docs/overview/glossary.md`: sections named by pillar; the broken
  "Formerly 'Orunbase'" line fixed.

**Done when** the landing page, README, site tagline, `orun --help` and
`cli/orun.md` all lead with "Platform discipline as code" and the four
pillars, and no page still calls orun "the intent compiler for platform
engineering" as its definition.

## PD2 — Pillar 1, Declare: structure and standards as intent

**Scope**

- `concepts/intent-model.md`: rewritten as the pillar's hub. It explains what
  goes in `intent.yaml` and `component.yaml` and who owns which, and links to
  each rule page instead of repeating its YAML.
- `concepts/standards.md` (new): the standards hub, organised by question —
  what must a component look like (schemas), how may it run per lane
  (profiles, profile rules), in what order (dependency rules, promotion),
  with which secrets (references only), within what blast radius (contracts,
  `mayAffect`). Each mechanism is labelled **enforced** (fails `validate` or
  `plan`), **recorded** (in the plan, not enforced), or **declared only**
  (parsed, not yet acted on). Intent and profile `policies` and cross-plan
  promotion gates are labelled honestly.
- `concepts/compositions.md`: rewritten as "a golden path written as code";
  the duplicated contract material moves to links into
  `compositions/composition-contract.md`.
- New opening hooks for `trigger-bindings`, `profile-rules`,
  `dependency-rules`, `environment-promotion`, `secrets`,
  `runtime-environment`, and the authoring pages under `compositions/`.
- Fixes: policy enforcement claims (intent-model, compositions,
  intent-presets, `orun validate` docs); promotion gates described as
  enforced (`plan-dag`, `environment-promotion`); one job-id format
  everywhere; the `spec:` wrapper and string-list `promotion.dependsOn` in
  `overview/how-orun-works.md` and `overview/resource-model.md`; one stated
  story for API groups in `resource-model.md`; `compositions.yaml` →
  `composition.yaml`; Title Case titles and duplicate H1s.

**Done when** a reader can answer "what standards can I declare, and which
does orun enforce today?" from `concepts/standards.md` alone, and every
standards page opens with why it matters for platform discipline.

## PD3 — Pillar 2, Package & evolve: standards that travel like code

**Scope**

- `concepts/intent-presets.md`: rewritten as the pillar's flagship. Fix the
  self-contradictory precedence sentence and the merge table's field names
  (`parameterDefaults`, not `defaults`).
- `concepts/stacks.md`: reframed as "how standards travel"; the duplicated
  preset section becomes a pointer.
- `concepts/versioning-and-locking.md` (new): composition versions and
  lifecycle, `orun compositions lock` and `compositions.lock.yaml`, floating
  tags in CI, and `orun new upgrade` three-way merges. Today these exist only
  as fragments.
- `concepts/baselines.md`: reframed as a whole product's structure and
  standards packaged as code, upgradable instead of forked.
- `compositions/writing-compositions.md`: reframed as "encode a standard as a
  package".
- CLI intros: `orun-baseline`, `orun-new`, `orun-publish`, `orun-pack`
  (`stack.yaml` vs `orun.yaml` reconciled with the code's help text),
  `orun-compositions`.
- `examples/bootstrap-a-product-from-a-baseline.md`: an intro tying it to the
  pillar; legacy naming ("Orgs", `oruncloud.workers.dev`) checked against the
  current CLI output.
- `overview/resource-model.md`: `Stack` and `IntentPreset` added to the
  authored kinds.

**Done when** the pillar reads as one path — compositions → Stack → preset →
lock → blueprint → baseline — with each page linking the next, and the
baseline/preset terminology matches decision 2.

## PD4 — Pillar 3, Ground agents: agents that read your standards

**Scope**

- `ai-context/orun-repositories.md`: rewritten as the pillar's anchor. How an
  agent learns a repo's standards (the intent, the catalog, base literacy,
  skills) and what stops it deviating (agent types' tool policy, `mayAffect`,
  TaskContract `affects` and gates, the provenance pen). Linked from the
  landing page.
- `concepts/agent-runtime.md`: new intro and positioning; the body stays;
  the hard-coded model id goes.
- `concepts/task-plane.md`: reframed around "done is derived from evidence".
- `concepts/service-catalog.md`: cross-linked as "what agents read".
- CLI: `orun-mcp` (lead with grounding; document `orun mcp doctor`; drop
  hard-coded tool counts the code computes), `orun-agent`, `orun-skills`,
  `orun-task`, `orun-work` (drop the internal epic reference).
- `context-for-ai/`: field names corrected (`parameters`, not `inputs`;
  `parameterDefaults`, not `defaults`); the pack gains a page on presets and
  on MCP, agent types and task contracts; duplication with
  `ai-context/orun-repositories.md` resolved by making the docs page the
  canonical narrative and the pack the copyable form.
- `skills/software-factory/SKILL.md`: version floor current.
- `agents/*.md`: stale comments about the removed work plane fixed. These
  files are embedded in the binary, so this milestone's release ships them.

**Done when** the pillar answers "how do I make my agents follow our
standards?" end to end, and the copyable pack agrees with the code's field
names.

## PD5 — Pillar 4, Verify → plan → execute: the runner and the record

**Scope**

- `overview/how-orun-works.md`: reframed as the runner's walkthrough, with a
  `validate` beat before compile, and links out to pillars 1–3.
- `concepts/plan-dag.md`, `concepts/execution-model.md` (CI artifacts and
  action caching move to `execute/runners.md` and
  `architecture/github-artifacts.md`), `concepts/workflow-actions.md` (the
  blueprint link fixed).
- `concepts/change-detection.md`: `change-watches.md` merged in, and the
  contradiction about dependents being included resolved against the code;
  `change-watches.md` stays as a pointer.
- `concepts/state-model.md` and `context-discovery.md`: the remote-state
  driver story and the `.orun/` layout made current.
- `execute/runners.md`: the stale `.orun/runs/<id>/state.json` and
  `orun run --resume` claims fixed.
- `cockpit/overview.md`: positioned as the record's inspection surface; the
  stale "State, on disk" section replaced.
- `examples/remote-state-matrix.md`: rewritten for Orunbase and the current
  default backend URL; `examples/review-pull-request.md` expanded into
  "verify intent before merge".
- `start/quick-start.md`: steps relabelled declare → package → verify → plan
  → execute.
- CLI: `orun-run` documents `orun run init`; `orun-validate` and `orun-plan`
  named as the verify and plan steps.

**Done when** the runner's story reads validate → plan → run → record in one
path, and no page in the pillar describes a state layout or flag the binary
no longer has.

## PD6 — Reference sweep and close-out

**Scope**

- Every page under `website/docs/` (release notes excepted) has a
  `description` in its front matter.
- Stale version pins (`v2.58.15`, `v2.58.0`, `v2.66.0`) and internal project
  jargon (`orun-workflows-v3`, `ORUN_TORKFLOW_ENGINE`, epic names) removed
  from user-facing pages.
- CLI accuracy fixes the review found: `orun catalog diff` signature,
  `describe execution` as an alias of `describe run`, `-c/--config-dir`
  wording, `orun-tui-next` intro.
- `architecture/internals.md`: missing packages (`inputglob`, `workfile`).
- `overview/glossary.md`: "baseline" vs "preset" terms per decision 2.
- `website/sidebars.js` header comment and `website/README.md` describe the
  pillar structure.
- The epic is marked complete.

**Done when** a link and front-matter check over `website/docs/` finds no
page without a description and no stale version pin, and every milestone in
`IMPLEMENTATION-STATUS.md` is done with its release.

## Out of scope

- Enforcing intent and profile `policies`, or cross-plan promotion gates.
  PD2 documents them honestly; building enforcement is separate work.
- A docs CI or automatic deploy. Releases do not publish the docs site.
- Moving or renaming doc files (decision 4).
