---
title: orun fetch
description: Download and extract a composition package from an OCI registry into a local directory.
---

`orun fetch` pulls a published composition package out of an OCI registry and
unpacks it on disk, so you can read, vendor, or fork a stack without cloning
its source repository. It is the read side of [`orun publish`](./orun-publish.md).

```bash
orun fetch <oci-ref> [-o DIR] [--overwrite]
```

## `fetch`

The reference is a full `<registry>/<repository>:<tag>` (or `@<digest>`); an
`oci://` prefix is accepted and stripped. By default the package lands in a
directory named after the last path segment of the repository —
`ghcr.io/sourceplane/stack-tectonic:0.12.0` unpacks into `./stack-tectonic`.
An existing destination is refused unless you pass `--overwrite`, which removes
it before extracting.

Registry credentials are read from `~/.docker/config.json` (plaintext `auths`
only — native credential helpers are never invoked); without an entry the pull
is anonymous. Use [`orun login`](./orun-login.md) to store one.

| Flag | Meaning |
| --- | --- |
| `--output`, `-o` | Destination directory (defaults to `./<package-name>`) |
| `--overwrite` | Replace the destination directory if it already exists |

## Examples

```bash
# Unpack into ./stack-tectonic
orun fetch ghcr.io/sourceplane/stack-tectonic:0.12.0

# Choose the directory
orun fetch ghcr.io/acme/aws-vpc:v1.4.0 --output ./my-vpc

# Replace a previous fetch
orun fetch ghcr.io/acme/aws-vpc:v1.4.0 --overwrite
```

To consume a published stack from `intent.yaml` without unpacking it, declare
it as an `oci` composition source and let
[`orun compositions pull`](./orun-compositions.md) cache it instead.

## Related

- [`orun publish`](./orun-publish.md) — the write side: package and push
- [`orun login`](./orun-login.md) — store registry credentials
- [`orun compositions`](./orun-compositions.md) — declared sources, `pull`, `lock`, and `package`
