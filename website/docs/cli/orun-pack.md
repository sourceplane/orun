---
title: orun pack
description: Build a composition package archive locally without uploading it to a registry.
---

`orun pack` builds the `.tgz` archive of a composition package — a directory
carrying a `stack.yaml` or `orun.yaml` manifest — and leaves it on disk. It is
the offline half of [`orun publish`](./orun-publish.md): the same archive
layout, no registry involved. Use it to inspect what would ship, attach the
archive to a release, or push it later with `orun compositions package push`.

```bash
orun pack [--root DIR] [-o FILE]
```

## `pack`

The package root defaults to the current directory when it holds a
`stack.yaml` or `orun.yaml`, then to `./examples/compositions`; pass `--root`
to point anywhere else. The package name comes from the manifest's
`metadata.name`, and the version is resolved the same way `publish` resolves
it — an exact-match git tag on `HEAD`, then the manifest's `spec.version`, then
a `0.1.0-dev+<sha>` placeholder. Both feed the default archive name.

The command prints the package name, version, file count, and archive path,
then `✓ packed`.

| Flag | Meaning |
| --- | --- |
| `--root` | Composition package root (defaults to `./`) |
| `--output`, `-o` | Output archive path (defaults to `./<name>-<version>.tgz`) |

## Examples

```bash
# Archive the package in the current directory
orun pack

# Archive a stack that lives elsewhere, to a chosen path
orun pack --root examples/compositions --output dist/platform-stack.tgz

# Push the archive later, explicitly
orun compositions package push dist/platform-stack.tgz ghcr.io/my-org/platform-stack:v1.0.0
```

## `pack` and `compositions package build`

`orun pack` and [`orun compositions package build`](./orun-compositions.md#package)
produce the same archive through the same code path. They are two separate
commands, not aliases of one another: `pack` infers the root, name, and
version for you, while `package build` requires an explicit `--root` and
`--output` and does no inference. `orun pack` is the spelling the rest of
these docs use.

## Related

- [`orun publish`](./orun-publish.md) — package and push in one step
- [`orun compositions`](./orun-compositions.md) — `package build` and `package push`
- [`orun fetch`](./orun-fetch.md) — unpack a published package
