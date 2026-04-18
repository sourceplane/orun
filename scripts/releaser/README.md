# releaser

`releaser` is a standalone-ready Go CLI that packages and optionally publishes tinx provider layouts from an existing `dist/` directory.

## Structure

- `cmd/releaser`: CLI entrypoint
- `internal/cli`: argument parsing
- `internal/config`: provider manifest loading
- `internal/release`: build, package, publish pipeline

## Usage

```bash
go run ./cmd/releaser \
  --provider ../../provider.yaml \
  --dist ../../dist \
  --output ../../oci \
  --ref ghcr.io/sourceplane/arx:<tag>
```

## Quick smoke test

From repository root:

```bash
rm -rf dist oci
mkdir -p dist/linux_amd64 dist/linux_arm64 dist/darwin_amd64 dist/darwin_arm64

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/linux_amd64/entrypoint ./cmd/arx
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o dist/linux_arm64/entrypoint ./cmd/arx
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o dist/darwin_amd64/entrypoint ./cmd/arx
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/darwin_arm64/entrypoint ./cmd/arx

cd scripts/releaser
go run ./cmd/releaser --provider ../../provider.yaml --dist ../../dist --output ../../oci
```

This validates packaging without publishing.

## Pipeline

The GitHub Actions release workflow uses `sourceplane/tinx-release-action` directly. The standalone `releaser` remains useful when you already have a populated `dist/` directory and want to package or publish it manually:

```bash
cd scripts/releaser
go run ./cmd/releaser --provider ../../provider.yaml --dist ../../dist --output ../../oci --ref ghcr.io/<org>/<repo>:<tag>
```

## Build quality

Use the local quality targets:

```bash
make fmt
make vet
make build
```