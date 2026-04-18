---
title: Installation
---

Install `arx` from source when you want the local CLI, or run it as a packaged provider through `tinx` when you want workspace-pinned execution.

## Prerequisites

- macOS or Linux
- Go 1.25+ for source builds
- Docker only if you plan to use the Docker execution backend
- GitHub Actions only if you plan to rely on `use:` steps inside the GitHub Actions-compatible runner

## Build from this repository

Use this when you are working in the repository and want the local `./arx` binary for examples and development.

```bash
make build
./arx version
./arx --help
```

The build also emits deprecated `./ciz` and `./liteci` aliases for compatibility with older workflows.

## Install directly with Go

```bash
go install github.com/sourceplane/arx/cmd/arx@latest
```

Verify the CLI:

```bash
arx version
arx --help
```

## Install a release binary

Replace `<tag>` with the release tag you want to install.

```bash
# macOS arm64
curl -L https://github.com/sourceplane/arx/releases/download/<tag>/arx_<tag>_darwin_arm64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/arx
chmod +x /usr/local/bin/arx

# Linux amd64
curl -L https://github.com/sourceplane/arx/releases/download/<tag>/arx_<tag>_linux_amd64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/arx
chmod +x /usr/local/bin/arx
```

## Run arx through tinx

This path is useful when you want the planner pinned as an OCI-distributed provider inside a reproducible workspace.

```bash
tinx init demo -p ghcr.io/sourceplane/arx:<tag> as arx
tinx --workspace demo -- arx --help
```

The legacy aliases are still valid if your workspace expects `ciz` or `lite-ci`:

```bash
tinx init demo -p ghcr.io/sourceplane/arx:<tag> as ciz
tinx init demo -p ghcr.io/sourceplane/arx:<tag> as lite-ci
```

## Build the docs site locally

The documentation site lives in `website/` and uses Docusaurus.

```bash
cd website
npm install
npm run docs:start
```

## Next steps

1. Follow the [quick start](./quick-start.md) to compile and preview the example plan.
2. Read [intent model](../concepts/intent-model.md) and [compositions](../concepts/compositions.md) before authoring your own contracts.