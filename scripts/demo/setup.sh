#!/usr/bin/env bash
# Builds the workspace the README recording runs in (scripts/demo/record.py):
# a copy of examples/saas-baseline, the baseline readers try the same loop with.
#
# The recording then runs, from <dir>:
#   orun new --blueprint saas-baseline/blueprint.yaml --out acme-shop --set name=acme-shop
#   cd acme-shop; orun plan; orun run <plan id>
#
# usage: scripts/demo/setup.sh <dir>
set -euo pipefail
dir="${1:?usage: setup.sh <dir>}"
src="$(cd "$(dirname "$0")/../../examples/saas-baseline" && pwd)"
rm -rf "$dir"; mkdir -p "$dir"
cp -R "$src" "$dir/saas-baseline"
rm -f "$dir/saas-baseline/README.md"
echo "demo workspace ready in $dir"
