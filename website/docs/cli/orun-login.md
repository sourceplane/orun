---
title: orun login
description: Store credentials for an OCI registry so orun publish and orun fetch can push and pull composition packages.
---

`orun login` authenticates you to an **OCI registry** (`ghcr.io`, Docker Hub,
an internal Harbor…) so that [`orun publish`](./orun-publish.md),
[`orun fetch`](./orun-fetch.md), and `orun compositions package push` can reach
private repositories.

:::warning This is not the Orunbase login
`orun login` is registry authentication only. To sign in to Orunbase and
link a repository, run [`orun auth login`](./orun-auth.md). The two commands
store different credentials in different places and neither implies the other.
:::

```bash
orun login <registry> [-u USER] [-p PASSWORD | --password-stdin]
```

## `login`

The command shells out to `oras login`, so the [`oras`](https://oras.land)
CLI must be on your `PATH`; without it the command fails with an install
hint. The registry argument is normalised — an `oci://`, `https://`, or
`http://` prefix and a trailing slash are stripped — so `orun login
https://ghcr.io/` and `orun login ghcr.io` mean the same thing.

Prefer `--password-stdin` over `--password`: a password passed as a flag is
visible in shell history and process listings. With neither flag, `oras`
prompts interactively.

| Flag | Meaning |
| --- | --- |
| `--username`, `-u` | Registry username |
| `--password`, `-p` | Registry password (prefer `--password-stdin`) |
| `--password-stdin` | Read the password from stdin |

## Examples

```bash
# Interactive prompt
orun login ghcr.io

# Non-interactive, token piped in
orun login ghcr.io --username acme --password-stdin < token.txt

# From a GitHub Actions job
echo "$GITHUB_TOKEN" | orun login ghcr.io -u "$GITHUB_ACTOR" --password-stdin
```

`orun publish` and `orun fetch` read credentials back from
`~/.docker/config.json` (plaintext `auths` entries only), which is where
`oras login` writes by default. If your Docker config delegates that registry
to a native credential helper, the push or pull falls back to anonymous
access.

## Related

- [`orun auth`](./orun-auth.md) — Orunbase sign-in (`orun auth login`)
- [`orun publish`](./orun-publish.md) — push a composition package
- [`orun fetch`](./orun-fetch.md) — pull a composition package
