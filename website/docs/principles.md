---
title: Design principles
description: The five principles that shape every choice in orun — from compiler stages to the cockpit palette.
---

orun was designed by working backwards from one question: *what would platform engineering
look like if planning, observation, and execution all spoke the same language?*

These principles are the answer. They are load-bearing — every concept, every command,
every pixel of the cockpit traces back to one of them.

## 1. Intent and execution are different layers

The most common platform-engineering anti-pattern is collapsing **what should happen**
into **how it happens**. Helm values get tangled with kubectl invocations, Terraform vars
get tangled with CI workflows, environment policy gets tangled with shell scripts.

orun draws a hard line:

| Layer | Lives in | Owned by |
|---|---|---|
| **Intent** — desired state, policy, environment matrix | `intent.yaml`, `component.yaml` | Platform & app teams |
| **Contract** — typed execution recipes per component type | `Composition` packages | Platform team |
| **Plan** — fully-resolved DAG of jobs and steps | `plan.json` | Generated, never edited |
| **Execution** — running the plan against a backend | Runner adapter | Runtime |

Each layer has a stable schema. Each layer can be reviewed independently. A platform team
can evolve compositions without touching app intent; an app team can change inputs without
re-reading runner code.

## 2. The plan is the audit artifact

`plan.json` is not a debugging aid. It is the **artifact of record** — what was decided,
based on what inputs, at what revision.

- Every implicit default becomes explicit.
- Every policy merge is visible.
- Every dependency edge is named.
- Every composition source is pinned by digest in `compositions.lock.yaml`.

This means:

- You **diff plans** in pull requests instead of guessing what a YAML change will do.
- You **archive plans** as deployment records — every run came from a plan you can replay.
- You can **execute the same plan on a different runner** without recompiling.

If a behavior isn't visible in the plan, it's a bug.

## 3. Determinism over cleverness

Identical inputs produce **byte-for-byte identical** plans. The compiler is a pure
function of `(intent, components, locked composition digests, trigger context)`.

Concretely:

- Maps are serialized in sorted key order.
- Job IDs are derived from `component@environment.short` — no random suffixes.
- Step ordering is stable across compiler runs.
- Floating tags in composition sources fail the lock; only digests are accepted in CI.

Cleverness — auto-inferred dependencies, magic environment selection, implicit retries —
is rejected when it threatens determinism. Where heuristics are unavoidable, they live
behind an explicit flag.

## 4. Policy at compile time

Group policies and domain constraints are **enforced when the plan is built**, not when
it runs. A non-compliant intent fails fast with a structured error, not a half-deployed
environment.

This puts the right pressure in the right place:

- **Authors** see policy violations during `orun validate`.
- **Reviewers** see policy in the plan diff, not in runtime logs.
- **Operators** never have to roll back a non-compliant deploy that slipped past a
  permissive runner.

Profile rules and dependency rules ([profile-rules](/concepts/profile-rules),
[dependency-rules](/concepts/dependency-rules)) let policy *adapt to the trigger* without
escaping the compile-time boundary — a PR can run plan-only with parallel jobs, a release
can run apply with enforced ordering, both from the same intent.

## 5. One design language across every surface

The cockpit — the violet wedge `▲`, the status glyphs `✓ ✗ ◐ ○ ↷`, the tree connectors,
the progress bar — is **not styling**. It is a shared vocabulary that lets you move from
`orun status` in a CI log, to `orun status --watch` on your terminal, to `orun tui` in a
control room without re-learning what success looks like.

The implementation enforces it:

```text
internal/cockpit/style   ← single source of truth (Go constants, zero deps)
       │
       ├──▶ internal/ui          (ANSI escapes for the CLI)
       └──▶ internal/tui/theme   (lipgloss.AdaptiveColor for the TUI)
```

This documentation site uses the same tokens. The violet (`#7c3aed` / `#a78bfa`), the
glyphs, the brand wedge — all the same constants. Reskinning the entire ecosystem is one
file.

---

## What follows from these principles

A few things you'll notice as you go deeper:

- **No "smart" defaults.** Every input that affects the plan is declarable; defaults
  exist, but they're documented and visible in the rendered plan.
- **No runtime mutation of the DAG.** Triggers shape the DAG at compile time; the
  executor consumes it as-is.
- **No surface-specific output formats.** The same view-model produces the CLI frame,
  the TUI panes, and (Phase 4) the JSON surface.
- **No undocumented state.** `.orun/` is the only place runtime state lives, and its
  schema is part of the public contract.

When you're authoring a new composition, a new runner, or a new surface, hold the change
up to these five principles. If it fights them, it probably belongs somewhere else.

Next: read [intent model](/concepts/intent-model), then [compositions](/concepts/compositions),
then [plan DAG](/concepts/plan-dag) in that order — they map directly onto principles 1, 1,
and 2.
