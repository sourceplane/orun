#!/usr/bin/env bash
# run-local-harness.sh
#
# Local remote-state conformance harness for the remote-state-matrix example.
#
# Proves that local CLI sessions exercise the same backend coordination
# semantics as GitHub Actions OIDC runners:
#   - duplicate job claim: only one of two races actually executes
#   - dependency wait: api@dev.smoke polls /runnable until foundation@dev.smoke completes
#   - status: orun status --remote-state shows expected successful jobs
#   - logs: orun logs --remote-state returns non-empty output
#
# Prerequisites:
#   orun auth login                              (human OAuth)
#   orun auth login --device                     (headless / device flow)
#
# Usage:
#   ./run-local-harness.sh
#   ORUN_BACKEND_URL=https://my-backend.example.com ./run-local-harness.sh
#   ORUN_DRY_RUN=1 ./run-local-harness.sh        (print commands, no real calls)
#
# Environment overrides:
#   ORUN_BACKEND_URL   Backend URL (default: https://orun-api.sourceplane.ai)
#   ORUN_EXEC_ID       Pin a specific exec ID (auto-generated if not set)
#   ORUN_DRY_RUN       Set to 1 to print commands without running them
#   ORUN_BIN           Path to orun binary (default: orun on $PATH)

set -euo pipefail

# ── configuration ──────────────────────────────────────────────────────────────
BACKEND_URL="${ORUN_BACKEND_URL:-https://orun-api.sourceplane.ai}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ORUN_BIN="${ORUN_BIN:-orun}"
DRY_RUN="${ORUN_DRY_RUN:-0}"

# ── helpers ────────────────────────────────────────────────────────────────────
info()  { echo "[harness] $*"; }
fail()  { echo "[harness] FAIL: $*" >&2; exit 1; }
check() { echo "[harness] CHECK: $*"; }

orun_cmd() {
  if [ "$DRY_RUN" = "1" ]; then
    echo "[dry-run] ${ORUN_BIN} $*"
    return 0
  fi
  "$ORUN_BIN" "$@"
}

# ── dry-run stubs ──────────────────────────────────────────────────────────────
# In dry-run mode we emit the expected command sequence and exit cleanly so that
# a CI shell validator can confirm the script structure without real credentials.
if [ "$DRY_RUN" = "1" ]; then
  info "DRY-RUN mode — printing command sequence, not executing"
  echo "[dry-run] ${ORUN_BIN} auth status --backend-url ${BACKEND_URL}"
  echo "[dry-run] ${ORUN_BIN} plan --name remote-state-e2e --all"
  echo "[dry-run] ${ORUN_BIN} get plans -o json"
  PLAN_ID="dryrun000000"
  EXEC_ID="local-dryrun-${PLAN_ID}"
  echo "[dry-run] export ORUN_EXEC_ID=${EXEC_ID}"
  echo "[dry-run] export ORUN_BACKEND_URL=${BACKEND_URL}"
  echo "[dry-run] export ORUN_REMOTE_STATE=true"
  echo "[dry-run] ${ORUN_BIN} run ${PLAN_ID} --job foundation@dev.smoke --remote-state --backend-url ${BACKEND_URL} &  (process A)"
  echo "[dry-run] ${ORUN_BIN} run ${PLAN_ID} --job foundation@dev.smoke --remote-state --backend-url ${BACKEND_URL} &  (process B — duplicate)"
  echo "[dry-run] ${ORUN_BIN} run ${PLAN_ID} --job api@dev.smoke        --remote-state --backend-url ${BACKEND_URL} &  (dep-wait)"
  echo "[dry-run] wait"
  echo "[dry-run] assert: duplicate claim — only one process executed the job steps"
  echo "[dry-run] ${ORUN_BIN} status --remote-state --backend-url ${BACKEND_URL} --exec-id ${EXEC_ID} --json"
  echo "[dry-run] assert: foundation@dev.smoke status == success"
  echo "[dry-run] assert: api@dev.smoke        status == success"
  echo "[dry-run] ${ORUN_BIN} logs   --remote-state --backend-url ${BACKEND_URL} --exec-id ${EXEC_ID} --job foundation@dev.smoke"
  echo "[dry-run] assert: logs non-empty"
  echo "[dry-run] PASS"
  exit 0
fi

# ── 1. auth check ──────────────────────────────────────────────────────────────
info "Checking Orun auth status..."
if ! "$ORUN_BIN" auth status --backend-url "$BACKEND_URL" >/dev/null 2>&1; then
  echo "" >&2
  echo "  Not logged in to Orun." >&2
  echo "  Run one of:" >&2
  echo "    orun auth login                         # browser OAuth" >&2
  echo "    orun auth login --device                # device flow (headless)" >&2
  echo "" >&2
  fail "Authentication required. See above."
fi
info "Auth OK."

# ── 2. plan ────────────────────────────────────────────────────────────────────
cd "$SCRIPT_DIR"
info "Compiling plan (remote-state-e2e)..."
orun_cmd plan --name remote-state-e2e --all

PLAN_ID="$("$ORUN_BIN" get plans -o json 2>/dev/null \
  | jq -r '.[] | select(.Name == "remote-state-e2e") | .Checksum' \
  | head -1)"
[ -n "$PLAN_ID" ] || fail "Could not derive plan checksum from 'orun get plans'"
info "Plan ID: ${PLAN_ID}"

# ── 3. exec ID and env ─────────────────────────────────────────────────────────
export ORUN_EXEC_ID="${ORUN_EXEC_ID:-local-$(date +%s)-${PLAN_ID}}"
export ORUN_BACKEND_URL="$BACKEND_URL"
export ORUN_REMOTE_STATE="true"
info "Exec ID: ${ORUN_EXEC_ID}"
info "Backend: ${ORUN_BACKEND_URL}"

# ── 4. launch parallel job processes ──────────────────────────────────────────
TMPDIR_H="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_H"' EXIT

info "Launching job processes..."

# Process A — first claim for foundation@dev.smoke
"$ORUN_BIN" run "$PLAN_ID" \
  --job foundation@dev.smoke \
  --remote-state \
  --backend-url "$BACKEND_URL" \
  > "$TMPDIR_H/foundation-a.log" 2>&1 &
PID_A=$!

# Process B — duplicate claim for the same job (must not re-execute it)
"$ORUN_BIN" run "$PLAN_ID" \
  --job foundation@dev.smoke \
  --remote-state \
  --backend-url "$BACKEND_URL" \
  > "$TMPDIR_H/foundation-b.log" 2>&1 &
PID_B=$!

# Process C — api@dev.smoke depends on foundation@dev.smoke (dep-wait case)
"$ORUN_BIN" run "$PLAN_ID" \
  --job api@dev.smoke \
  --remote-state \
  --backend-url "$BACKEND_URL" \
  > "$TMPDIR_H/api-dev.log" 2>&1 &
PID_C=$!

# ── 5. collect exit codes ──────────────────────────────────────────────────────
EXIT_FAIL=0

wait "$PID_A" || { info "foundation-a exited non-zero ($?)"; cat "$TMPDIR_H/foundation-a.log" >&2; EXIT_FAIL=1; }
wait "$PID_B" || { info "foundation-b exited non-zero ($?)"; cat "$TMPDIR_H/foundation-b.log" >&2; EXIT_FAIL=1; }
wait "$PID_C" || { info "api-dev exited non-zero ($?)"     ; cat "$TMPDIR_H/api-dev.log"     >&2; EXIT_FAIL=1; }

if [ "$EXIT_FAIL" -ne 0 ]; then
  fail "One or more job processes exited with non-zero status (see output above)."
fi
info "All job processes exited 0."

# ── 6. assert: duplicate claim — exactly one runner executed the job ────────────
# Each foundation@dev.smoke execution prints two "=== SMOKE:" lines (validate + apply).
# If both process A and B claimed the job, combined output would contain 4+ "=== SMOKE:" lines.
# One runner claiming means exactly 2.
check "Duplicate claim assertion..."
SMOKE_COUNT="$(cat "$TMPDIR_H/foundation-a.log" "$TMPDIR_H/foundation-b.log" \
  | grep -c "=== SMOKE: foundation" || true)"

if [ "${SMOKE_COUNT:-0}" -gt 2 ]; then
  echo "" >&2
  echo "  Process A output:" >&2; cat "$TMPDIR_H/foundation-a.log" >&2
  echo "  Process B output:" >&2; cat "$TMPDIR_H/foundation-b.log" >&2
  fail "DUPLICATE CLAIM DETECTED: foundation@dev.smoke step output appeared ${SMOKE_COUNT} times (expected ≤2). Both processes executed the job."
fi
info "Duplicate claim check passed (foundation smoke step lines across both processes: ${SMOKE_COUNT})."

# ── 7. assert: api@dev.smoke dependency wait worked ───────────────────────────
# If dep-wait failed because local state was empty, api@dev.smoke would have printed
# a dependency-blocked error and exited 1. We already checked exit codes above.
# Additionally confirm api@dev.smoke actually executed (not just skipped/errored).
check "Dependency wait assertion..."
API_SMOKE="$(grep -c "=== SMOKE: api" "$TMPDIR_H/api-dev.log" || true)"
if [ "${API_SMOKE:-0}" -eq 0 ]; then
  echo "" >&2
  echo "  api-dev output:" >&2; cat "$TMPDIR_H/api-dev.log" >&2
  fail "DEP WAIT FAILED: api@dev.smoke did not execute its steps. It may have blocked or errored instead of waiting for foundation@dev.smoke."
fi
info "Dependency wait check passed (api smoke step lines: ${API_SMOKE})."

# ── 8. verify remote status ────────────────────────────────────────────────────
info "Fetching remote status..."
STATUS_JSON="$("$ORUN_BIN" status \
  --remote-state \
  --backend-url "$BACKEND_URL" \
  --exec-id "$ORUN_EXEC_ID" \
  --json 2>&1)" || fail "orun status --remote-state failed"

[ -n "$STATUS_JSON" ] || fail "orun status --remote-state returned empty output"

for JOB_ID in "foundation@dev.smoke" "api@dev.smoke"; do
  check "Status check for ${JOB_ID}..."
  JOB_STATUS="$(echo "$STATUS_JSON" \
    | jq -r --arg j "$JOB_ID" '.state.jobs[$j].status // empty' 2>/dev/null || true)"
  if [ "$JOB_STATUS" != "success" ] && [ "$JOB_STATUS" != "completed" ]; then
    echo "$STATUS_JSON" | jq . >&2 2>/dev/null || echo "$STATUS_JSON" >&2
    fail "Expected ${JOB_ID} status=success, got: '${JOB_STATUS}'"
  fi
  info "  ${JOB_ID}: ${JOB_STATUS}"
done

# ── 9. retrieve logs ───────────────────────────────────────────────────────────
check "Log retrieval for foundation@dev.smoke..."
REMOTE_LOGS="$("$ORUN_BIN" logs \
  --remote-state \
  --backend-url "$BACKEND_URL" \
  --exec-id "$ORUN_EXEC_ID" \
  --job foundation@dev.smoke 2>&1)" || fail "orun logs --remote-state failed"

if [ -z "$(echo "$REMOTE_LOGS" | tr -d '[:space:]')" ]; then
  fail "Logs for foundation@dev.smoke are empty. Expected step output from the executed job."
fi
info "Log retrieval passed (${#REMOTE_LOGS} bytes)."

# ── done ───────────────────────────────────────────────────────────────────────
echo ""
info "════════════════════════════════════════════════"
info "  LOCAL REMOTE-STATE HARNESS PASSED"
info "════════════════════════════════════════════════"
info ""
info "  Exec ID: ${ORUN_EXEC_ID}"
info "  Backend: ${ORUN_BACKEND_URL}"
info ""
info "  To inspect further:"
info "    orun status --remote-state --backend-url '${ORUN_BACKEND_URL}' --exec-id '${ORUN_EXEC_ID}'"
info "    orun logs   --remote-state --backend-url '${ORUN_BACKEND_URL}' --exec-id '${ORUN_EXEC_ID}' --job foundation@dev.smoke"
