#!/usr/bin/env bash
# dry-run-guard.sh
#
# CI guard for the local remote-state harness.
#
# Verifies that run-local-harness.sh is syntactically valid and that its
# dry-run output contains every required command and assertion marker.
# Does not require Orun credentials, a live backend, or GitHub Actions.
#
# Usage:
#   ./test/dry-run-guard.sh
#   (also invoked by 'go test ./...' via harness_test.go)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HARNESS="${SCRIPT_DIR}/../run-local-harness.sh"

pass() { echo "[guard] PASS: $*"; }
fail() { echo "[guard] FAIL: $*" >&2; exit 1; }

# ── 1. syntax check ────────────────────────────────────────────────────────────
bash -n "$HARNESS" || fail "Bash syntax error in run-local-harness.sh"
pass "Bash syntax check"

# ── 2. dry-run output check ────────────────────────────────────────────────────
DRY_OUTPUT="$(ORUN_DRY_RUN=1 bash "$HARNESS" 2>&1)"

# Required commands
REQUIRED=(
  "auth status"
  "plan --name remote-state-e2e"
  "get plans"
  "foundation@dev.smoke"
  "api@dev.smoke"
  "duplicate"
  "dep-wait"
  "status --remote-state"
  "assert: foundation@dev.smoke status"
  "assert: api@dev.smoke"
  "logs   --remote-state"
  "assert: logs non-empty"
  "PASS"
)

for pattern in "${REQUIRED[@]}"; do
  if ! echo "$DRY_OUTPUT" | grep -qF "$pattern"; then
    echo "" >&2
    echo "  Dry-run output:" >&2
    echo "$DRY_OUTPUT" >&2
    fail "Missing required pattern in dry-run output: '${pattern}'"
  fi
done
pass "Dry-run output contains all required command/assertion markers"

# ── 3. verify harness declares ORUN_EXEC_ID export ────────────────────────────
if ! grep -q "export ORUN_EXEC_ID" "$HARNESS"; then
  fail "Harness does not export ORUN_EXEC_ID"
fi
pass "ORUN_EXEC_ID is exported"

# ── 4. verify harness declares ORUN_REMOTE_STATE export ───────────────────────
if ! grep -q "export ORUN_REMOTE_STATE" "$HARNESS"; then
  fail "Harness does not export ORUN_REMOTE_STATE"
fi
pass "ORUN_REMOTE_STATE is exported"

# ── 5. verify duplicate-claim assertion is present ────────────────────────────
if ! grep -q "DUPLICATE CLAIM" "$HARNESS"; then
  fail "Harness missing DUPLICATE CLAIM assertion"
fi
pass "Duplicate-claim assertion present"

# ── 6. verify dep-wait assertion is present ───────────────────────────────────
if ! grep -q "DEP WAIT" "$HARNESS"; then
  fail "Harness missing DEP WAIT assertion"
fi
pass "Dep-wait assertion present"

# ── 7. verify log-retrieval assertion is present ──────────────────────────────
if ! grep -q "logs.*empty\|empty.*logs\|Logs.*empty\|Empty.*logs" "$HARNESS"; then
  fail "Harness missing log-retrieval assertion"
fi
pass "Log-retrieval assertion present"

echo ""
echo "[guard] ════════════════════════════════════════════"
echo "[guard]   DRY-RUN GUARD PASSED"
echo "[guard] ════════════════════════════════════════════"
