---
title: Run with GitHub Actions compatibility
---

The repository example includes packaged compositions that use GitHub Actions `use:` steps for tool setup. A small dependency-free smoke path is the Terraform-backed `network-foundation` component.

## Compile the example plan

```bash
orun plan \
  --intent examples/intent.yaml \
  --component network-foundation \
  --env development \
  --output /tmp/orun-github-actions-plan.json
```

The example intent declares its packaged composition source directly, so no extra composition path flag is required.

## Run the plan

```bash
orun run \
  --plan /tmp/orun-github-actions-plan.json \
  --workdir examples
```

Because the plan contains a `use:` step, `orun run` auto-selects the `github-actions` backend unless you explicitly override it.

## Force the backend explicitly

```bash
orun run \
  --plan /tmp/orun-github-actions-plan.json \
  --workdir examples \
  --gha
```

Use the explicit flag when you want the command line itself to document that the plan requires GitHub Actions semantics.

## Trigger-aware CI planning

When your intent file declares trigger bindings, use `--from-ci` to let orun automatically scope the plan based on the GitHub event:

```bash
orun plan \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH" \
  --output plan.json

orun run \
  --plan plan.json \
  --runner github-actions
```

This replaces manual `--changed --base --head` flags — the trigger binding's `plan.scope` and event paths handle everything. See [trigger-aware CI](./trigger-bindings-ci.md) for a complete workflow example.
