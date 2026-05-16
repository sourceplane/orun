#!/usr/bin/env sh
set -eu

if (set -o pipefail) >/dev/null 2>&1; then
  set -o pipefail
fi

REPO="${ORUN_REPO:-sourceplane/orun}"
BIN_NAME="${ORUN_BIN_NAME:-orun}"
INSTALL_DIR="${ORUN_INSTALL_DIR:-${HOME}/.local/bin}"
REQUESTED_VERSION="${ORUN_VERSION:-latest}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 1
  }
}

need_cmd curl
need_cmd tar

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  darwin|linux) ;;
  *)
    echo "error: unsupported OS: $OS (supported: darwin, linux)" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "error: unsupported architecture: $ARCH (supported: amd64, arm64)" >&2
    exit 1
    ;;
esac

resolve_version() {
  if [ "$REQUESTED_VERSION" != "latest" ]; then
    echo "$REQUESTED_VERSION"
    return
  fi

  url="https://github.com/${REPO}/releases/latest"
  tag="$(curl -fsSI "$url" | grep -i "^location:" | sed 's|.*/tag/||' | tr -d '\r\n')"

  if [ -z "$tag" ]; then
    echo "error: could not resolve latest release tag from ${url}" >&2
    exit 1
  fi

  echo "$tag"
}

VERSION="$(resolve_version)"
ASSET_VERSION="${VERSION#v}"
ARCHIVE="${BIN_NAME}_${ASSET_VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

echo "Installing ${BIN_NAME} ${VERSION} for ${OS}/${ARCH}"
echo "Download: ${URL}"

curl -fL "$URL" -o "$TMP_DIR/$ARCHIVE"

tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR"

if [ ! -f "$TMP_DIR/$BIN_NAME" ]; then
  echo "error: archive did not contain expected binary: ${BIN_NAME}" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMP_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"

echo "Installed to: $INSTALL_DIR/$BIN_NAME"
if ! command -v "$BIN_NAME" >/dev/null 2>&1; then
  echo "Note: add $INSTALL_DIR to your PATH"
fi

echo "Run: $BIN_NAME --help"
