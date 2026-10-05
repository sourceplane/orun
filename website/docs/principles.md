---
title: Design principles
description: The principles behind platform discipline as code, from why standards must be declarations to why the plan is the audit artifact and why agents read the same intent as everyone else.
---

orun was designed by working backwards from one question: *what would it take for a
platform's standards to be followed every time, by every team and every agent, without
someone having to remember them?*

The answer is that a standard has to be something the platform can **read**: written in a
declarative language, versioned like code, shared with every tool that acts on the
repository, and checked before anything runs. These principles are how orun gets there.
Every concept and command traces back to one of them.

## 1. A standard is a declaration, not a document

If a rule lives in a wiki, a review checklist, or a copied CI file, it is a
recommendation. It drifts the first time someone is in a hurry, and nothing notices.

In orun, the platform's structure and standards are typed documents in the repository:

- **Structure**: which components exist, where they are discovered, which environments
  they ship to, and what depends on what (`intent.yaml`, `component.yaml`).
- **Standards**: how each kind of component is built and what it must look like
  (compositions: schemas, job templates, profiles), which profile runs in which lane
  (profile rules), what waits for what (dependency rules, promotion), and how secrets are
  referenced.

A rule that is not declared does not exist as far as orun is concerned. That is the point:
the declaration is the standard.

## 2. Intent and execution are different layers

The most common platform anti-pattern is collapsing **what should happen** into **how it
happens**. Helm values get tangled with kubectl invocations, Terraform variables with CI
workflows, environment rules with shell scripts.

orun draws a hard line:

| Layer | Lives in | Owned by |
|---|---|---|
| **Intent**: desired state, environment matrix, rules | `intent.yaml`, `component.yaml` | Platform and app teams |
| **Contract**: typed execution recipes per component type | Composition packages | Platform team |
| **Plan**: the fully resolved DAG of jobs and steps | `plan.json` | Generated, never edited |
| **Execution**: running the plan against a backend | Runner adapter | Runtime |

Each layer has a stable schema and can be reviewed on its own. A platform team can evolve
a golden path without touching application intent; an application team can change its
parameters without reading runner code.

## 3. Standards travel as versioned code

A standard that cannot move between repositories gets copied, and a copy is a fork. orun
packages standards the way software packages libraries:

- Golden paths are published as versioned OCI **Stacks** and pulled by reference.
- Platform rules are published as **intent presets** and inherited with `extends:`; the
  repository's own intent always wins, and `orun intent explain` shows where each field
  came from.
- `orun compositions lock` pins every resolved source by digest, so a standard changes in
  a repository only when someone changes the pin, in a reviewed commit.
- A **baseline** packages a whole product's structure and standards. `orun new upgrade`
  three-way merges a newer release into a product built from it, so improvements flow
  forward instead of each copy aging on its own.

Upgrading a standard is a version bump whose consequence you can read as a plan diff.

## 4. Agents read the same intent

Coding agents amplify whatever a repository makes easy. If the standards are implicit,
an agent learns them by imitating the nearest example, which is how drift scales.

In orun, an agent is grounded in the same declarations everyone else uses:

- `orun mcp serve` exposes the catalog, runs, skills, and task plane derived from the
  repository's intent, so an agent can ask "what is this component and what depends on
  it?" instead of guessing.
- Every agent type extends a versioned base literacy, and its tools are allowed, gated,
  or denied by a policy written in the agent type itself.
- A `TaskContract` declares the paths a change may affect and the gates it must pass.
  `orun task check` and `orun pr check` hold a change to it, and status is derived from
  evidence, never asserted.

The agent is a client of the platform's truth, not a second source of it.

## 5. The plan is the audit artifact

`plan.json` is not a debugging aid. It is the **artifact of record**: what was decided,
based on what inputs, at what revision.

- Every implicit default becomes explicit.
- Every dependency edge is named.
- Every composition source is pinned by digest in `compositions.lock.yaml`.

This means:

- You **diff plans** in pull requests instead of guessing what a YAML change will do.
- You **archive plans** as deployment records; every run came from a plan you can replay.
- You can **execute the same plan on a different runner** without recompiling.

If a behaviour is not visible in the plan, it is a bug.

## 6. Determinism over cleverness

Identical inputs produce **byte-for-byte identical** plans. The compiler is a pure
function of the intent, the components, the locked composition digests, and the trigger
context.

Concretely:

- Maps are serialised in sorted key order.
- Job IDs are derived from the component, environment, and job name
  (`component.environment.job`), with no random suffixes.
- Job and step order are stable across compiler runs.
- The plan carries no wall-clock timestamp.

Cleverness such as auto-inferred dependencies, magic environment selection, or implicit
retries is rejected when it threatens determinism. Where a heuristic is unavoidable, it
lives behind an explicit flag.

## 7. Check before you run

Whatever can be checked before execution is checked before execution, so a violation
fails in review, not halfway through a deploy:

- `orun validate` checks the intent and its profile and dependency rules.
- `orun plan` checks every component's parameters against its composition's schema,
  rejects a literal value in a secret slot, and refuses a dependency cycle.
- Profile and dependency rules let behaviour *adapt to the trigger* without escaping the
  compile step: a pull request can run plan-only with parallel jobs and a release can run
  apply with enforced ordering, both from the same intent.

Not every declared rule is enforced yet. Group and environment `policies`, and the
`policies` block on execution profiles, are carried through to each component instance
but are not yet checked by the planner or the runner; cross-plan promotion gates are
recorded in the plan as evidence to check, not enforced. The concept pages say which is
which.

## 8. One vocabulary across every surface

The status glyphs `✓ ✗ ◐ ○ ↷`, the tree connectors, and the progress bar are a shared
vocabulary, so you can move from `orun status` in a CI log to `orun tui` in a terminal
without relearning what success looks like. The tokens live in one package,
`internal/cockpit/style`, and this documentation site uses the same palette.

---

## What follows from these principles

A few things you will notice as you go deeper:

- **No unwritten rules.** Every input that affects the plan is declarable; defaults exist,
  but they are documented and visible in the rendered plan.
- **No silent upgrades.** A standard changes in a repository when its pin changes.
- **No runtime mutation of the DAG.** Triggers shape the DAG at compile time; the executor
  consumes it as is.
- **No undocumented state.** `.orun/` is the only place runtime state lives, and its
  schema is part of the public contract.

When you author a new composition, preset, runner, or agent type, hold the change up to
these principles. If it fights them, it probably belongs somewhere else.

Next: read the [intent model](/concepts/intent-model), then
[compositions](/concepts/compositions), [Stacks](/concepts/stacks), and the
[plan DAG](/concepts/plan-dag).
