# Spec: orun-docs-discipline (PD — platform discipline as code)

**orun's documentation is re-framed around one line: platform discipline as
code.** orun's declarative language lets a platform team write its structure
and its standards as intent, package them as baselines, and transfer and
evolve them like any other code. The same intent is what agents read, so they
work inside the standards instead of guessing at them. And a runner verifies
the intent, compiles a plan, and executes it.

Today the docs tell a different story. The landing page, the README, the
binary's own `--help`, and the overview pages describe orun as "the intent
compiler for platform engineering" with a cockpit and a runtime, and spread
seven different taglines across the site. Standards, packaging, and agents
appear as features in a list rather than as the point.

## Status

| Field | Value |
|-------|-------|
| Status | **In progress.** PD1–PD6 below; progress in [IMPLEMENTATION-STATUS.md](./IMPLEMENTATION-STATUS.md) |
| Cluster | **PD**, milestones **PD1–PD6** |
| Owner(s) | `website/` (docs, sidebar, site config), `README.md`, `cmd/orun/commands_root.go` (root help text), `context-for-ai/`, `skills/`, `agents/` (comments only) |
| Target branch | `main` |
| Release cadence | One release per milestone. A milestone merges to `main`, then `release-oci` cuts the next version; its release notes say what the milestone changed. |
| Docs deploy | Manual (`wrangler pages deploy`, see `website/docs/contributing/deploying-docs.md`). This epic validates every milestone with `npm run docs:build`, which fails on broken links; publishing the site is left to a maintainer with Cloudflare access. |

## The four pillars

Every page belongs to one of four pillars, and the site's navigation follows them.

| Pillar | The promise | Mechanisms |
|---|---|---|
| **1 · Declare** | Your platform's structure and standards are written down as intent, not tribal knowledge. | `intent.yaml`, `component.yaml`, compositions (schemas, job templates, profiles), groups and environments, trigger bindings, profile and dependency rules, promotion, secret references |
| **2 · Package & evolve** | Standards travel and change like code: versioned, pinned, reviewed, upgraded. | Stacks (compositions published as OCI artifacts), intent presets (`extends:`), `compositions.lock.yaml`, blueprints and `orun new`, baselines and `orun baseline` |
| **3 · Ground agents** | Agents read the same intent, so they don't deviate from it. | `orun mcp`, `orun skills`, `orun agent` (base literacy, agent types, `mayAffect`), the task plane and TaskContracts, the provenance pen, `context-for-ai/` |
| **4 · Verify → plan → execute** | A runner checks the intent, compiles a deterministic plan, and runs it anywhere, leaving a record. | `orun validate`, `orun plan`, `orun run` (shell, Docker, GitHub Actions), change detection, state, catalog, cockpit |

The cloud client (`auth`, `workspace`, `cloud`, `secrets`, `integrations`,
`policy`, `backend`) supports all four and is documented as such.

## Decisions locked

1. **The tagline is "Platform discipline as code."** It replaces "the intent
   compiler for platform engineering" everywhere: the site tagline and meta
   description, the landing page, the README, `orun --help`, and
   `cli/orun.md`. "Compiler" survives as a description of how pillar 4 works,
   not as what orun is.
2. **"Baseline" keeps its meaning in the code.** A baseline is a packaged
   product: a blueprint plus the Stacks and presets it pulls, rebuilt for a
   new owner and upgraded by three-way merge. Stacks and presets are the
   standards layers that travel inside a baseline or on their own. Presets are
   never called baselines.
3. **The docs claim only what the code enforces.** The review found intent
   `policies` (groups, environments, presets) and profile `policies` are
   parsed, merged, and attached to instances, but nothing in the planner or
   runner reads them; cross-plan promotion gates are recorded, not enforced.
   PD2 labels each standard *enforced*, *recorded*, or *declared only*.
   Building enforcement is out of scope for this epic.
4. **URLs stay stable.** Pages move between sidebar categories, but file paths,
   and so URLs, do not change. The one merge (`change-watches` into
   `change-detection`) keeps the old page as a short pointer.
5. **The cockpit is part of pillar 4, not a pillar of its own.** It is the
   surface for inspecting the record.

## Documents

- [implementation-plan.md](./implementation-plan.md): the milestones, each with scope and "done when".
- [review-findings.md](./review-findings.md): the page-by-page review this plan is built on, including the factual errors each milestone fixes.
- [IMPLEMENTATION-STATUS.md](./IMPLEMENTATION-STATUS.md): progress, one row per milestone.
