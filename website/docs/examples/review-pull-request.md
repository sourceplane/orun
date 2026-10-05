---
title: Verify a pull request
description: Use orun in pull-request review to verify that a change keeps to the platform's standards before it merges - validate the intent, compile the changed plan, check the task contract, and review the plan diff.
---

A pull request is where a standard either holds or quietly slips. This guide shows the
checks to run on every pull request so a change is verified against the platform's
declared standards before it merges, and the reviewer sees exactly what it will do.

## 1. Verify the intent

```bash
orun validate
```

This checks the intent file, the component manifests it discovers, the reserved `ORUN_`
environment prefix, and every profile and dependency rule. It does not load compositions,
so it is fast enough for a pre-commit hook.

## 2. See what the change touches

```bash
orun component --changed --base main --long
orun catalog affected --base main
```

`orun component --changed` gives a merged view of the components the branch changed.
`orun catalog affected` shows the full blast radius: the directly changed components and
everything that depends on them. See
[selection and blast radius](../concepts/change-detection.md#selection-and-blast-radius).

## 3. Compile the plan for the change

```bash
orun plan --changed --base main --output /tmp/pr-plan.json --view dependencies
```

This is the heavier check. Compiling the plan validates every selected component's
parameters against its composition's schema, verifies any pinned source digest, applies
inherited presets, rejects literal values in secret slots, and refuses dependency cycles.
If the plan compiles, the change keeps to every standard orun enforces; see
[standards](../concepts/standards.md).

Plans are deterministic, so compiling the same change on `main` and on the branch and
diffing the two `plan.json` files is a faithful preview of what the change will do: every
rendered step, merged parameter, and edge that differs.

## 4. Check the task contract

If the branch works a task (`orun/<KEY>-<slug>`), hold it to its contract:

```bash
orun task check <KEY> --base main   # the diff stays inside the contract's affects
orun pr check                       # branch name, Orun-Task trailers, one task per PR
```

## Use explicit file lists in CI

If your CI platform already exposes the changed file list, pass it directly:

```bash
orun plan --files apps/web/component.yaml,intent.yaml --output /tmp/pr-plan.json
```

## Wire it into CI

Bind the pull-request event to your development environments in `intent.yaml` so the plan
is shaped by the event that fired it, then run the checks above in the job. A complete
GitHub Actions setup is in [trigger bindings in CI](./trigger-bindings-ci.md).

Use this flow for fast, focused signal on every pull request, and a full plan on merge and
release.
