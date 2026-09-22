---
title: orun compositions
---

`orun compositions` lists or inspects the composition types resolved for an intent, or from a legacy `--config-dir` fallback, and carries the `pull`, `lock`, and `package` subcommands that manage declared composition sources and their packages.

## Usage

```bash
orun compositions [composition] [-e]
orun compositions list [composition] [-l] [-e]
orun compositions pull
orun compositions lock
orun compositions package build --root DIR --output FILE
orun compositions package push <archive> <oci-ref>
```

The bare command lists every composition; pass a name directly for details:

```bash
orun compositions --intent examples/intent.yaml
orun compositions terraform --intent examples/intent.yaml
```

The alias `composition` is also supported.

| Flag | Meaning |
| --- | --- |
| `--expand-jobs`, `-e` | Show all job steps and details |
| `--intent`, `-i` | Intent file path used to resolve declared composition sources (global) |

The parent command has no `--long` flag; detailed mode lives on `list`.

## `list`

The explicit `list` subcommand adds `--long` for the detailed view. `--expand-jobs` expands every job's steps within that view.

```bash
orun compositions list terraform \
  --intent examples/intent.yaml \
  --long \
  --expand-jobs
```

| Flag | Meaning |
| --- | --- |
| `--long`, `-l` | Show detailed information |
| `--expand-jobs`, `-e` | Show all job steps and details (with `-l`) |

## `pull` and `lock`

Resolve the sources declared under `compositions.sources` in `intent.yaml` into the local cache, and record the resolved digests so plans are reproducible:

```bash
orun compositions pull --intent examples/intent.yaml
orun compositions lock --intent examples/intent.yaml
```

Neither subcommand takes flags of its own beyond the global `--intent` / `--config-dir`.

## `package`

`compositions package` is the explicit two-step form of packaging: build an archive from a package root, then push that archive to a registry reference you spell out in full. It does no inference — every input is a flag or an argument.

### `package build`

Builds a `.tgz` archive from a directory that carries a `stack.yaml` or `orun.yaml` manifest. Both flags are required.

```bash
orun compositions package build --root examples/compositions --output dist/platform-stack.tgz
```

| Flag | Meaning |
| --- | --- |
| `--root` | Composition package root directory (required) |
| `--output`, `-o` | Output `.tgz` archive path (required) |

### `package push`

Pushes an existing archive to an OCI registry. Credentials come from `~/.docker/config.json`; store them with [`orun login`](./orun-login.md).

```bash
orun compositions package push dist/platform-stack.tgz ghcr.io/my-org/my-platform-stack:v1.0.0
```

It takes no flags: the archive path and the full `<registry>/<repository>:<tag>` are positional.

### `package` and the top-level verbs

[`orun pack`](./orun-pack.md) and [`orun publish`](./orun-publish.md) are the one-step, inferring forms of the same operations. `pack` builds the same archive as `package build` but infers the root, name, and version; `publish` streams the package to the registry with no archive on disk and infers the target from `stack.yaml` or the git remote. They are separate commands, not aliases — `orun pack` and `orun publish` are the canonical spelling in these docs, and `compositions package build|push` is for pipelines that need the archive as a durable artifact between the two steps.

## Packaging and publishing stacks

Build a local archive from a Stack directory:

```bash
orun pack --root examples/compositions
orun pack --root examples/compositions --output dist/platform-stack.tgz
```

Stream-publish a Stack directly to an OCI registry (no temp file). The target reference is positional; omit it to infer:

```bash
orun login ghcr.io
orun publish --root examples/compositions
orun publish --root examples/compositions ghcr.io/my-org/my-platform-stack:v1.0.0
orun publish --root examples/compositions --dry-run
```

The `stack.yaml` `registry` block is used to infer the target when the reference is omitted:

```yaml
registry:
  host: ghcr.io
  namespace: my-org
  repository: my-platform-stack
```

See [`orun publish`](./orun-publish.md) for the full inference order and [`orun fetch`](./orun-fetch.md) to unpack a published stack.

## Using a remote stack from a registry

Reference a published Stack directly in `intent.yaml`:

```yaml
compositions:
  sources:
    - name: platform
      kind: oci
      ref: oci://ghcr.io/my-org/my-platform-stack:v1.0.0
```

Lock the resolved digest for reproducible plans:

```bash
orun compositions lock --intent intent.yaml
```

Use this command to confirm which types are available before validating or planning against them. `--config-dir` remains available as a global legacy fallback for folder-shaped compositions.

## Related

- [`orun pack`](./orun-pack.md) — build a package archive without uploading
- [`orun publish`](./orun-publish.md) — package and push in one step
- [`orun fetch`](./orun-fetch.md) — pull and unpack a published package
- [`orun login`](./orun-login.md) — store OCI registry credentials
