---
title: Installation
---

Install `orun` from source when you want the local CLI, or run it as a packaged provider through `kiox` when you want workspace-pinned execution.

## Prerequisites

- macOS or Linux
- Go 1.25+ for source builds
- Docker only if you plan to use the Docker execution backend
- GitHub Actions only if you plan to rely on `use:` steps inside the GitHub Actions-compatible runner

## Build from this repository

Use this when you are working in the repository and want the local `./orun` binary for examples and development.

```bash
make build
./orun version
./orun --help
```

## Install directly with Go

```bash
go install github.com/sourceplane/orun/cmd/orun@latest
```

Verify the CLI:

```bash
orun version
orun --help
```

## Install a release binary

Replace `<tag>` with the release tag you want to install.

```bash
# macOS arm64
curl -L https://github.com/sourceplane/orun/releases/download/<tag>/orun_<tag>_darwin_arm64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/orun
chmod +x /usr/local/bin/orun

# Linux amd64
curl -L https://github.com/sourceplane/orun/releases/download/<tag>/orun_<tag>_linux_amd64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/orun
chmod +x /usr/local/bin/orun
```

## Run orun through kiox

This path is useful when you want the planner pinned as an OCI-distributed provider inside a reproducible workspace.

```bash
kiox init demo -p ghcr.io/sourceplane/orun:<tag> as orun
kiox --workspace demo -- orun --help
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