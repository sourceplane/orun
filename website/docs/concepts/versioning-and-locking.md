---
title: Versioning and locking
description: How standards change on purpose. Stacks and compositions carry versions, a repository pins a source by tag or by an enforced digest, every plan records exactly which digest it used, and products built from blueprints upgrade by three-way merge.
---

Standards that travel like code need what code has: versions, pins, and a way to see what
an upgrade will do before it lands. This page brings together everything orun gives you
for that, from a single golden path to a whole product.

The rule underneath all of it: **a standard changes in a repository when someone changes a
reference in a reviewed commit, and the plan shows the consequence.**

## What carries a version

| Thing | Version lives in | Read by |
|---|---|---|
| A Stack (golden paths and presets) | `metadata.version` in `stack.yaml`, and the tag it is published under | `orun publish`, and every repository that references the tag |
| A composition (one golden path) | `spec.version` and `spec.lifecycle` (`stable`, `beta`, `deprecated`) on the `Composition` | The service catalog |
| A baseline | The pinned `tag` in its registry row | `orun baseline new` |
| A product built from a blueprint | `.orun/provenance.lock` in the product | `orun new upgrade` |

## Pinning a golden path

A repository chooses its golden paths in `intent.yaml`. There are two strengths of pin:

```yaml
compositions:
  sources:
    # A version tag: resolved at plan time. If the tag is moved in the registry,
    # the next plan picks up the new content.
    - name: platform
      kind: oci
      ref: oci://ghcr.io/acme/platform-stack:v1.4.0

    # A digest: enforced. If the reference resolves to anything else,
    # `orun plan` fails with a digest mismatch.
    - name: security
      kind: oci
      ref: oci://ghcr.io/acme/security-stack:v2.0.1
      digest: sha256:4f1c…
```

A `digest:` works for `dir` and `archive` sources too: orun hashes the content and compares.
Use a digest wherever a silent change would be unacceptable, such as for anything that
gates production.

## What every plan records

Whatever the pin strength, every plan records the exact digest each source resolved to,
under `spec.compositionSources` in `plan.json`. Because plans are deterministic, a plan
diff in a pull request shows when a golden path changed underneath a repository, even if
no file in the repository did.

`orun plan` and `orun compositions lock` also write the same record to
`.orun/compositions.lock.yaml`. orun does not read that file back, and `.orun/` is
normally not committed, so treat it as a local record, not a pin. The pin is the
`digest:` in `intent.yaml`.

## Upgrading a standard

An upgrade is a pull request that changes a reference:

1. Change the source's `ref` (and its `digest:`, if pinned) to the new version.
2. Run `orun intent explain` to see which inherited rules changed, if the Stack publishes
   [presets](./intent-presets.md).
3. Run `orun plan` and review the plan diff: every rendered step, default, and edge the new
   version changes is in it.
4. Merge. Repositories that did not change their reference are unaffected.

A platform team retiring a golden path marks its composition `lifecycle: deprecated` in a
new Stack version. The lifecycle is recorded on the composition's catalog entity, so
"which components still ride a deprecated path?" is a [catalog](./service-catalog.md)
query; orun does not print a warning for it.

## Upgrading a product built from a blueprint

[`orun new`](../cli/orun-new.md) writes `.orun/provenance.lock` into everything it places:
the blueprint's digest, each source's digest, a secret-free hash of the inputs, and every
module's mode and target. That lock is what makes a product built from a
[baseline](./baselines.md) upgradable rather than a fork:

```bash
orun new upgrade --out ./my-service                                  # report what would change
orun new upgrade --out ./my-service --apply                          # apply non-conflicting updates
orun new upgrade --out ./my-service --blueprint ../blueprint-v2.yaml --apply
```

`orun new upgrade` re-renders the newer blueprint against the lock and three-way merges it
into the tree. Files the blueprint owns and you did not touch are updated; a file you
edited is reported as a conflict and never overwritten.

## Related

- [Stacks](./stacks.md): packaging and publishing golden paths and presets
- [Intent presets](./intent-presets.md): platform rules inherited with `extends:`
- [Baselines](./baselines.md): a whole product's structure and standards
- [Standards](./standards.md): which pins and rules orun enforces
