---
title: Installation
description: Install the orun CLI with the install script, a release archive, go install, a source build, or as a pinned kiox provider — on macOS and Linux, amd64 and arm64.
---

`orun` is a single static binary. Releases are published for `linux` and
`darwin` on `amd64` and `arm64`, with a `checksums.txt` next to every archive.
Windows is not yet supported; use WSL.

## Install script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
```

The script detects your OS and architecture, downloads the latest release
from GitHub, and installs `orun` to `~/.local/bin`. Two variables customise
it:

| Variable | Default | Purpose |
|---|---|---|
| `ORUN_VERSION` | `latest` | A specific release tag to install, for example `v2.58.15` |
| `ORUN_INSTALL_DIR` | `~/.local/bin` | Where to put the binary |

```bash
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh \
  | ORUN_VERSION=v2.58.15 ORUN_INSTALL_DIR=/usr/local/bin sh
```

If `~/.local/bin` is not on your `PATH`, the script says so at the end.

## Release archive

Download the archive for your platform from the
[releases page](https://github.com/sourceplane/orun/releases), verify it,
and place the `orun` binary on your `PATH`. Archive names drop the `v` from
the tag:

```bash
VERSION=v2.58.15
OS=$(uname -s | tr '[:upper:]' '[:lower:]')      # linux or darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

curl -fsSLO "https://github.com/sourceplane/orun/releases/download/${VERSION}/orun_${VERSION#v}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "https://github.com/sourceplane/orun/releases/download/${VERSION}/checksums.txt"
shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf "orun_${VERSION#v}_${OS}_${ARCH}.tar.gz" orun
install -m 0755 orun /usr/local/bin/orun
```

## `go install`

With Go 1.25 or later:

```bash
go install github.com/sourceplane/orun/cmd/orun@latest
```

The binary reports `orun version dev` when built this way; pin a tag
(`@v2.58.15`) for a versioned build.

## From source

```bash
git clone https://github.com/sourceplane/orun.git
cd orun
make build
./orun version
```

`make build` runs `go build -o orun ./cmd/orun`. The quick start assumes this
layout.

## As a kiox provider

orun is also published as a [kiox](https://github.com/sourceplane/kiox)
provider at `ghcr.io/sourceplane/orun`. Use this when you want the CLI
pinned per workspace and reproducible across machines and CI:

```bash
kiox init demo
kiox --workspace demo add ghcr.io/sourceplane/orun:v2.58.15 as orun
kiox --workspace demo exec -- orun plan --intent intent.yaml
```

The same image runs under Docker, and `oras pull ghcr.io/sourceplane/orun:v2.58.15`
fetches the raw provider package:

```bash
docker run --rm -v "$PWD":/work -w /work ghcr.io/sourceplane/orun:v2.58.15 plan --intent intent.yaml
```

## Verify

```bash
orun version
orun --help
```

Running `orun` with no arguments on an interactive terminal opens the
cockpit TUI. In CI or a pipe, or with `ORUN_NO_TUI=1`, it prints help
instead.

## Shell completion

```bash
orun completion zsh > "${fpath[1]}/_orun"        # zsh
orun completion bash > /etc/bash_completion.d/orun
orun completion fish > ~/.config/fish/completions/orun.fish
```

## Optional dependencies

| You need | When |
|---|---|
| Docker | Running plans with `--runner docker` |
| `git` and `gh` | Cloud features that touch repositories: `orun auth login` auto-link, the provenance pen, local baseline builds with hooks |
| Node.js 20+ and `pnpm` | Local baseline builds with hooks, and building the docs site |

## Next steps

1. Follow the [quick start](./quick-start.md) to compile and run the example plan.
2. Sign in and [create a workspace and build it from a baseline](../examples/bootstrap-a-product-from-a-baseline.md).
3. Read the [intent model](../concepts/intent-model.md) and [compositions](../concepts/compositions.md) before authoring your own.
