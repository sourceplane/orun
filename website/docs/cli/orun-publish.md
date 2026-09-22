---
title: orun publish
description: Package a composition stack and push it to an OCI registry in one step.
---

`orun publish` packages a composition package — a directory carrying a
`stack.yaml` or `orun.yaml` manifest — and pushes it to an OCI registry as a
single artifact, streaming straight from the directory with no intermediate
file. A published stack can then be referenced from any `intent.yaml` as an
`oci` composition source, or unpacked with [`orun fetch`](./orun-fetch.md).

```bash
orun publish [oci-ref] [--root DIR] [--version TAG] [--dry-run] [--keep-archive]
```

## `publish`

The optional argument is the target. It may be omitted, a registry host alone
(`ghcr.io`), or a full `<registry>/<repository>[:tag]`; an `oci://` prefix is
accepted. When the repository is not given, orun infers it:

1. the `registry` block in `stack.yaml` (`host`, `namespace`, `repository`),
   when all three are set;
2. otherwise the local git remote, as `<owner>/<repo>/<package-name>` on
   `ghcr.io` (or on the host you passed).

The tag is the resolved version: `--version` if given, else an exact-match git
tag on `HEAD`, else the manifest's `spec.version`, else `0.1.0-dev+<sha>`. A
tag in the ref itself wins over all of these. Repository paths are lower-cased.

Before uploading, the command prints the package name, version, file count,
target registry, and where the target was inferred from. Credentials are read
from `~/.docker/config.json` — see [`orun login`](./orun-login.md).

| Flag | Meaning |
| --- | --- |
| `--root` | Composition package root (defaults to `./`, then `./examples/compositions`) |
| `--version` | Override the version tag (defaults to git tag, then manifest `spec.version`) |
| `--dry-run` | Resolve the target and build the archive, but skip the upload |
| `--keep-archive` | Also write `<name>-<version>.tgz` to the current directory after a successful publish |

## Examples

```bash
# Infer everything from stack.yaml or the git remote
orun publish

# Name the repository; tag from the version resolution
orun publish ghcr.io/acme/aws-vpc

# Name the full ref
orun publish ghcr.io/acme/aws-vpc:v1.4.0

# See where it would go without pushing
orun publish --root examples/compositions --dry-run
```

A `stack.yaml` that pins its own registry lets the bare form publish to the
right place every time:

```yaml
registry:
  host: ghcr.io
  namespace: my-org
  repository: my-platform-stack
```

## `publish` and `compositions package push`

`orun publish` is the one-step, inferring form.
[`orun compositions package build`](./orun-compositions.md#package) and
`package push <archive> <oci-ref>` are the explicit two-step form: build an
archive with `--root` and `--output`, then push that file to a ref you spell
out in full. They share the archive layout and the registry client but are
separate commands — neither is an alias of the other, and `package push` does
no inference. `orun publish` is the canonical spelling in these docs; reach for
the two-step form when a pipeline needs the archive as a durable artifact
between the build and the push.

## Related

- [`orun pack`](./orun-pack.md) — build the archive without uploading
- [`orun fetch`](./orun-fetch.md) — pull and unpack a published package
- [`orun login`](./orun-login.md) — store registry credentials
- [`orun compositions`](./orun-compositions.md) — declared sources and the `package` subcommands
