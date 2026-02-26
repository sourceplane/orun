#!/usr/bin/env bash

set -euo pipefail

PROVIDER_PATH="thin.provider.yaml"
DIST_DIR="dist"
OUTPUT_DIR="oci"
PUBLISH_REF=""
BUILD_WITH=""

usage() {
  cat <<'EOF'
Usage:
  scripts/package-thin-provider.sh [options]

Options:
  --provider <path>   Path to provider manifest (default: thin.provider.yaml)
  --build-with <tool> Build before packaging (supported: goreleaser|gorelaser)
  --dist <dir>        GoReleaser dist directory (default: dist)
  --output <dir>      OCI layout output directory (default: oci)
  --ref <oci-ref>     Publish OCI artifact to this reference (optional)
  -h, --help          Show help

Examples:
  scripts/package-thin-provider.sh
  scripts/package-thin-provider.sh --build-with goreleaser
  scripts/package-thin-provider.sh --provider thin.provider.yaml --dist dist --output oci
  scripts/package-thin-provider.sh --ref ghcr.io/org/provider:v1.2.3
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --provider)
      PROVIDER_PATH="$2"
      shift 2
      ;;
    --build-with)
      BUILD_WITH="$2"
      shift 2
      ;;
    --dist)
      DIST_DIR="$2"
      shift 2
      ;;
    --output)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --ref)
      PUBLISH_REF="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ ! -f "$PROVIDER_PATH" ]]; then
  echo "Provider manifest not found: $PROVIDER_PATH" >&2
  exit 1
fi

if [[ -n "$BUILD_WITH" ]]; then
  case "$BUILD_WITH" in
    goreleaser|gorelaser)
      if ! command -v goreleaser >/dev/null 2>&1; then
        echo "goreleaser is required for --build-with $BUILD_WITH but was not found in PATH" >&2
        exit 1
      fi

      GORELEASER_CONFIG=$(ruby -r yaml -e 'data = YAML.load_file(ARGV[0]) || {}; cfg = data.dig("goreleaser", "config"); puts(cfg && !cfg.empty? ? cfg : ".goreleaser.yaml")' "$PROVIDER_PATH")

      if [[ -f ".goreleaser.yaml" ]]; then
        GORELEASER_CONFIG=".goreleaser.yaml"
      fi

      if [[ ! -f "$GORELEASER_CONFIG" ]]; then
        echo "GoReleaser config not found: $GORELEASER_CONFIG" >&2
        exit 1
      fi

      echo "Using GoReleaser config: $GORELEASER_CONFIG"
      goreleaser release --clean --skip=validate --config "$GORELEASER_CONFIG"
      ;;
    *)
      echo "Unsupported --build-with value: $BUILD_WITH" >&2
      echo "Supported values: goreleaser, gorelaser" >&2
      exit 1
      ;;
  esac
fi

if [[ ! -d "$DIST_DIR" ]]; then
  echo "Dist directory not found: $DIST_DIR" >&2
  exit 1
fi

readarray -t PROVIDER_META < <(ruby -r yaml -r json -e '
  provider_path = ARGV[0]
  data = YAML.load_file(provider_path) || {}

  assets_root = data.dig("assets", "root") || "assets"
  provider_file = File.basename(provider_path)
  artifact_type = data.dig("distribution", "artifactType") || "application/vnd.thin.provider.v1"
  core_media_type = data.dig("layers", "core", "mediaType") || "application/vnd.sourceplane.provider.v1"
  assets_media_type = data.dig("layers", "core", "assetsMediaType") || "application/vnd.sourceplane.assets.v1"

  examples_includes = data.dig("layers", "examples", "includes") || []
  examples_dir = if examples_includes.is_a?(Array) && !examples_includes.empty?
    examples_includes.first.sub(%r{/\*\*.*$}, "").sub(%r{/\*$}, "")
  else
    "examples"
  end
  examples_media_type = data.dig("layers", "examples", "mediaType") || "application/vnd.sourceplane.examples.v1"

  binary_layers = (data.dig("layers", "binaries") || {}).values

  platforms = (data["platforms"] || []).map do |platform|
    os = platform["os"]
    arch = platform["arch"]
    binary = platform["binary"]
    plat = "#{os}/#{arch}"
    layer = binary_layers.find { |entry| entry["platform"] == plat }
    media_type = if layer && layer["mediaType"]
      layer["mediaType"]
    else
      "application/vnd.thin.provider.bin.#{os}-#{arch}"
    end

    {
      "os" => os,
      "arch" => arch,
      "binary" => binary,
      "mediaType" => media_type
    }
  end

  puts "provider_file=#{provider_file}"
  puts "assets_root=#{assets_root}"
  puts "artifact_type=#{artifact_type}"
  puts "core_media_type=#{core_media_type}"
  puts "assets_media_type=#{assets_media_type}"
  puts "examples_dir=#{examples_dir}"
  puts "examples_media_type=#{examples_media_type}"
  puts "platform_count=#{platforms.length}"
  platforms.each_with_index do |platform, idx|
    puts "platform_#{idx}_os=#{platform["os"]}"
    puts "platform_#{idx}_arch=#{platform["arch"]}"
    puts "platform_#{idx}_binary=#{platform["binary"]}"
    puts "platform_#{idx}_mediaType=#{platform["mediaType"]}"
  end
' "$PROVIDER_PATH")

for line in "${PROVIDER_META[@]}"; do
  key="${line%%=*}"
  value="${line#*=}"
  case "$key" in
    provider_file) PROVIDER_FILE="$value" ;;
    assets_root) ASSETS_ROOT="$value" ;;
    artifact_type) ARTIFACT_TYPE="$value" ;;
    core_media_type) CORE_MEDIA_TYPE="$value" ;;
    assets_media_type) ASSETS_MEDIA_TYPE="$value" ;;
    examples_dir) EXAMPLES_DIR="$value" ;;
    examples_media_type) EXAMPLES_MEDIA_TYPE="$value" ;;
    platform_count) PLATFORM_COUNT="$value" ;;
    platform_*_os|platform_*_arch|platform_*_binary|platform_*_mediaType)
      eval "$key=\"$value\""
      ;;
  esac
done

if [[ "${PLATFORM_COUNT:-0}" -eq 0 ]]; then
  echo "No platforms declared in $PROVIDER_PATH" >&2
  exit 1
fi

EXE_NAME=$(ruby -r yaml -e 'data = YAML.load_file(ARGV[0]) || {}; puts(data.dig("entrypoint", "executable") || "entrypoint")' "$PROVIDER_PATH")

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

cp "$PROVIDER_PATH" "$OUTPUT_DIR/$PROVIDER_FILE"

ASSETS_REL="${ASSETS_ROOT#./}"
if [[ ! -d "$ASSETS_ROOT" ]]; then
  echo "Assets directory declared in provider not found: $ASSETS_ROOT" >&2
  exit 1
fi
mkdir -p "$OUTPUT_DIR/$(dirname "$ASSETS_REL")"
cp -R "$ASSETS_ROOT" "$OUTPUT_DIR/$ASSETS_REL"

for ((i=0; i<PLATFORM_COUNT; i++)); do
  os_var="platform_${i}_os"
  arch_var="platform_${i}_arch"
  binary_var="platform_${i}_binary"

  os="${!os_var}"
  arch="${!arch_var}"
  binary_path="${!binary_var}"

  target_file="$OUTPUT_DIR/$binary_path"
  target_dir=$(dirname "$target_file")
  mkdir -p "$target_dir"

  archive_path=$(find "$DIST_DIR" -maxdepth 1 -type f -name "*_${os}_${arch}.tar.gz" | sort | head -n 1)
  if [[ -n "$archive_path" ]]; then
    extract_dir=$(mktemp -d)
    tar -xzf "$archive_path" -C "$extract_dir"

    found_binary=$(find "$extract_dir" -type f -name "$EXE_NAME" | head -n 1)
    if [[ -z "$found_binary" ]]; then
      echo "Could not find executable '$EXE_NAME' inside $archive_path" >&2
      rm -rf "$extract_dir"
      exit 1
    fi

    cp "$found_binary" "$target_file"
    chmod +x "$target_file"
    rm -rf "$extract_dir"
    continue
  fi

  built_binary=$(find "$DIST_DIR" -type f -name "$EXE_NAME" | grep -E "(^|/|_)${os}([/_-]|$).*${arch}|${os}[_-]${arch}|${arch}[_-]${os}" | head -n 1 || true)
  if [[ -z "$built_binary" ]]; then
    echo "No archive or built binary found for ${os}/${arch} in $DIST_DIR" >&2
    exit 1
  fi

  cp "$built_binary" "$target_file"
  chmod +x "$target_file"
done

echo "OCI layout built at: $OUTPUT_DIR"

if [[ -n "$PUBLISH_REF" ]]; then
  if ! command -v oras >/dev/null 2>&1; then
    echo "oras is required for publishing but was not found in PATH" >&2
    exit 1
  fi

  push_args=(
    "push"
    "$PUBLISH_REF"
    "--artifact-type" "$ARTIFACT_TYPE"
    "$OUTPUT_DIR/$PROVIDER_FILE:$CORE_MEDIA_TYPE"
    "$OUTPUT_DIR/$ASSETS_REL/:$ASSETS_MEDIA_TYPE"
  )

  for ((i=0; i<PLATFORM_COUNT; i++)); do
    binary_var="platform_${i}_binary"
    media_var="platform_${i}_mediaType"

    binary_path="${!binary_var}"
    media_type="${!media_var}"
    push_args+=("$OUTPUT_DIR/$binary_path:$media_type")
  done

  examples_rel="${EXAMPLES_DIR#./}"
  if [[ -d "$examples_rel" ]] && [[ -n "$(ls -A "$examples_rel")" ]]; then
    push_args+=("$examples_rel/:$EXAMPLES_MEDIA_TYPE")
  fi

  oras "${push_args[@]}"
  echo "✅ OCI artifact published at $PUBLISH_REF"
fi
