#!/usr/bin/env bash
# run-local-harness.sh
#
# Local remote-state conformance harness for the remote-state-matrix example.
#
# Proves that local CLI sessions exercise the same backend coordination
# semantics as GitHub Actions OIDC runners:
#   - duplicate job claim: exactly one of two races executes the job body
#   - dependency wait: api@dev.smoke polls /runnable until foundation@dev.smoke completes
#   - status: orun status --remote-state shows expected successful jobs
#   - logs: orun logs --remote-state returns non-empty output
#
# Prerequisites:
#   orun auth login                              (human OAuth)
#   orun auth login --device                     (headless / device flow)
#   orun cloud link                              (link current repo namespace)
#
# Usage:
#   ./run-local-harness.sh
#   ORUN_BACKEND_URL=https://my-backend.example.com ./run-local-harness.sh
#   ORUN_DRY_RUN=1 ./run-local-harness.sh        (print commands only — no real calls)
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

# ── assertion helpers ──────────────────────────────────────────────────────────
# shellcheck source=harness_helpers.sh
source "${SCRIPT_DIR}/harness_helpers.sh"

# ── helpers ────────────────────────────────────────────────────────────────────
info()  { echo "[harness] $*"; }
fail()  { echo "[harness] FAIL: $*" >&2; exit 1; }
check() { echo "[harness] CHECK: $*"; }

# ── signal-safe cleanup ────────────────────────────────────────────────────────
HARNESS_PIDS=()
TMPDIR_H=""

cleanup() {
  if [ "${#HARNESS_PIDS[@]}" -gt 0 ]; then
    for pid in "${HARNESS_PIDS[@]}"; do
      kill "${pid}" 2>/dev/null || true
    done
    wait "${HARNESS_PIDS[@]}" 2>/dev/null || true
  fi
  if [ -n "${TMPDIR_H}" ]; then
    rm -rf "${TMPDIR_H}"
  fi
}
trap cleanup EXIT INT TERM

# ── dry-run mode ───────────────────────────────────────────────────────────────
# Prints the full intended command sequence without making real backend calls.
# Used by CI dry-run guards to verify script structure without credentials.
# NOTE: dry-run verifies command construction only, NOT live backend behavior.
#       It does not prove duplicate claim, dep-wait, status, or logs against a
#       real backend. Run without ORUN_DRY_RUN after orun auth login + cloud link
#       for live conformance.
if [ "${DRY_RUN}" = "1" ]; then
  info "DRY-RUN mode — printing command sequence, not executing"
  info "(Dry-run proves command construction only, not live backend coordination)"
  echo "[dry-run] preflight: command -v ${ORUN_BIN}"
  echo "[dry-run] preflight: command -v jq"
  echo "[dry-run] preflight: ${ORUN_BIN} auth status --backend-url ${BACKEND_URL}"
  echo "[dry-run] preflight: repo linkage check (orun auth status | grep '(linked)')"
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
  echo "[dry-run] assert: duplicate claim — assert_exactly_one_duplicate_claimant foundation@dev.smoke '=== SMOKE: foundation' 2 foundation-a.log foundation-b.log"
  echo "[dry-run] assert: dep-wait — api@dev.smoke smoke markers > 0"
  echo "[dry-run] ${ORUN_BIN} status --remote-state --backend-url ${BACKEND_URL} --exec-id ${EXEC_ID} --json"
  echo "[dry-run] assert: foundation@dev.smoke status == success"
  echo "[dry-run] assert: api@dev.smoke        status == success"
  echo "[dry-run] ${ORUN_BIN} logs   --remote-state --backend-url ${BACKEND_URL} --exec-id ${EXEC_ID} --job foundation@dev.smoke"
  echo "[dry-run] assert: logs non-empty"
  echo "[dry-run] PASS"
  exit 0
fi

# ── 1. preflight checks ────────────────────────────────────────────────────────
info "Running preflight checks..."

# 1a. orun binary
if ! command -v "${ORUN_BIN}" >/dev/null 2>&1; then
  echo "" >&2
  echo "  '${ORUN_BIN}' not found on PATH." >&2
  echo "" >&2
  echo "  Install options:" >&2
  echo "    go install github.com/sourceplane/orun/cmd/orun@latest" >&2
  echo "    Or build from source: go build -o orun ./cmd/orun" >&2
  echo "    Then add the binary to your PATH, or set ORUN_BIN=/path/to/orun." >&2
  fail "orun binary not found. See above."
fi

# 1b. jq
if ! command -v jq >/dev/null 2>&1; then
  echo "" >&2
  echo "  'jq' not found on PATH." >&2
  echo "  Install: https://stedolan.github.io/jq/download/" >&2
  echo "    macOS:  brew install jq" >&2
  echo "    Linux:  apt-get install jq  /  dnf install jq" >&2
  fail "jq not found. See above."
fi

# 1c. auth
AUTH_STATUS_OUT=""
if ! AUTH_STATUS_OUT="$("${ORUN_BIN}" auth status --backend-url "${BACKEND_URL}" 2>&1)"; then
  echo "" >&2
  echo "  Not logged in to Orun." >&2
  echo "" >&2
  echo "  Run one of:" >&2
  echo "    orun auth login                         # browser OAuth" >&2
  echo "    orun auth login --device                # device flow (headless)" >&2
  echo "" >&2
  echo "  Then re-run this harness." >&2
  fail "Authentication required. See above."
fi

# Check for expired token inside auth status output
if echo "${AUTH_STATUS_OUT}" | grep -q "(expired)"; then
  echo "" >&2
  echo "  Orun access token has expired." >&2
  echo "  Run: orun auth logout && orun auth login" >&2
  fail "Access token expired. See above."
fi
info "Auth OK."

# 1d. repo linkage
# orun run --remote-state requires a linked repo namespace so POST /v1/runs
# can include the namespaceId. Check this before starting concurrent runners.
if echo "${AUTH_STATUS_OUT}" | grep -q "(not linked)"; then
  echo "" >&2
  echo "  The current Git remote is not linked to your Orun account." >&2
  echo "" >&2
  echo "  Fix options:" >&2
  echo "    1. Run: orun cloud link --backend-url ${BACKEND_URL}" >&2
  echo "       (requires the repo to already be linked in the Orun backend)" >&2
  echo "    2. If the repo is not yet linked on the backend, visit the Orun" >&2
  echo "       dashboard and link it there first, then re-run orun cloud link." >&2
  fail "Repo linkage required for remote-state runs. See above."
fi

if ! echo "${AUTH_STATUS_OUT}" | grep -q "Current Git remote:"; then
  echo "" >&2
  echo "  Could not determine the current Git remote from 'orun auth status'." >&2
  echo "" >&2
  echo "  Ensure you are running from within a Git repository with a GitHub remote." >&2
  echo "  Then run: orun cloud link --backend-url ${BACKEND_URL}" >&2
  fail "Repo linkage required for remote-state runs. See above."
fi
info "Repo linkage OK."

# ── 2. plan ────────────────────────────────────────────────────────────────────
cd "${SCRIPT_DIR}"
info "Compiling plan (remote-state-e2e)..."
"${ORUN_BIN}" plan --name remote-state-e2e --all

PLAN_ID="$("${ORUN_BIN}" get plans -o json 2>/dev/null \
  | jq -r '.[] | select(.Name == "remote-state-e2e") | .Checksum' \
  | head -1)"
[ -n "${PLAN_ID}" ] || fail "Could not derive plan checksum from 'orun get plans'"
info "Plan ID: ${PLAN_ID}"

# ── 3. exec ID and env ─────────────────────────────────────────────────────────
export ORUN_EXEC_ID="${ORUN_EXEC_ID:-local-$(date +%s)-${PLAN_ID}}"
export ORUN_BACKEND_URL="${BACKEND_URL}"
export ORUN_REMOTE_STATE="true"
info "Exec ID: ${ORUN_EXEC_ID}"
info "Backend: ${ORUN_BACKEND_URL}"

# ── 4. temp dir ────────────────────────────────────────────────────────────────
TMPDIR_H="$(mktemp -d)"
# cleanup() above handles deletion on exit/signal

# ── 5. launch parallel job processes ──────────────────────────────────────────
info "Launching job processes..."

# Process A — first claimant for foundation@dev.smoke
"${ORUN_BIN}" run "${PLAN_ID}" \
  --job foundation@dev.smoke \
  --remote-state \
  --backend-url "${BACKEND_URL}" \
  > "${TMPDIR_H}/foundation-a.log" 2>&1 &
PID_A=$!
HARNESS_PIDS+=("${PID_A}")

# Process B — duplicate claimant for the same job (must not re-execute the body)
"${ORUN_BIN}" run "${PLAN_ID}" \
  --job foundation@dev.smoke \
  --remote-state \
  --backend-url "${BACKEND_URL}" \
  > "${TMPDIR_H}/foundation-b.log" 2>&1 &
PID_B=$!
HARNESS_PIDS+=("${PID_B}")

# Process C — api@dev.smoke depends on foundation@dev.smoke (dep-wait case)
"${ORUN_BIN}" run "${PLAN_ID}" \
  --job api@dev.smoke \
  --remote-state \
  --backend-url "${BACKEND_URL}" \
  > "${TMPDIR_H}/api-dev.log" 2>&1 &
PID_C=$!
HARNESS_PIDS+=("${PID_C}")

# ── 6. collect exit codes ──────────────────────────────────────────────────────
EXIT_FAIL=0

wait "${PID_A}" || { info "foundation-a exited non-zero ($?)"; cat "${TMPDIR_H}/foundation-a.log" >&2; EXIT_FAIL=1; }
wait "${PID_B}" || { info "foundation-b exited non-zero ($?)"; cat "${TMPDIR_H}/foundation-b.log" >&2; EXIT_FAIL=1; }
wait "${PID_C}" || { info "api-dev exited non-zero ($?)"     ; cat "${TMPDIR_H}/api-dev.log"     >&2; EXIT_FAIL=1; }

# Background processes have finished; remove from PID list so cleanup() doesn't
# attempt to kill already-exited processes.
HARNESS_PIDS=()

if [ "${EXIT_FAIL}" -ne 0 ]; then
  fail "One or more job processes exited with non-zero status (see output above)."
fi
info "All job processes exited 0."

# ── 7. assert: duplicate claim ─────────────────────────────────────────────────
# Each foundation@dev.smoke execution prints exactly 2 "=== SMOKE: foundation"
# lines (one per step: validate + apply). The assertion requires exactly one
# claimant log to contain 2 matching lines and the other to contain 0.
# Any other combination (both >0, neither >0, unexpected count) is a failure.
check "Duplicate claim assertion (foundation@dev.smoke)..."
assert_exactly_one_duplicate_claimant \
  "foundation@dev.smoke" \
  "=== SMOKE: foundation" \
  2 \
  "${TMPDIR_H}/foundation-a.log" \
  "${TMPDIR_H}/foundation-b.log" \
  || fail "DUPLICATE CLAIM DETECTED: see above for details."

# ── 8. assert: api@dev.smoke dependency wait worked ───────────────────────────
# If dep-wait failed (empty local state rather than polling backend /runnable),
# api@dev.smoke would have exited 1. That is already caught above.
# Additionally confirm the steps actually executed.
check "Dependency wait assertion (api@dev.smoke)..."
API_SMOKE="$(grep -c "=== SMOKE: api" "${TMPDIR_H}/api-dev.log" || true)"
if [ "${API_SMOKE:-0}" -eq 0 ]; then
  echo "" >&2
  echo "  api-dev output:" >&2; cat "${TMPDIR_H}/api-dev.log" >&2
  fail "DEP WAIT FAILED: api@dev.smoke did not execute its steps. It may have blocked on empty local state instead of polling backend /runnable."
fi
info "Dependency wait check passed (api smoke step lines: ${API_SMOKE})."

# ── 9. verify remote status ────────────────────────────────────────────────────
check "Fetching remote status..."
STATUS_JSON=""
STATUS_JSON="$("${ORUN_BIN}" status \
  --remote-state \
  --backend-url "${BACKEND_URL}" \
  --exec-id "${ORUN_EXEC_ID}" \
  --json 2>&1)" || fail "orun status --remote-state failed"

[ -n "${STATUS_JSON}" ] || fail "orun status --remote-state returned empty output"

check "Asserting expected job statuses..."
assert_jobs_all_succeeded "${STATUS_JSON}" \
  "foundation@dev.smoke" \
  "api@dev.smoke" \
  || fail "Status assertion failed: see above for details."

# ── 10. retrieve logs ──────────────────────────────────────────────────────────
check "Log retrieval for foundation@dev.smoke..."
REMOTE_LOGS=""
REMOTE_LOGS="$("${ORUN_BIN}" logs \
  --remote-state \
  --backend-url "${BACKEND_URL}" \
  --exec-id "${ORUN_EXEC_ID}" \
  --job foundation@dev.smoke 2>&1)" || fail "orun logs --remote-state failed"

if [ -z "$(printf '%s' "${REMOTE_LOGS}" | tr -d '[:space:]')" ]; then
  fail "Logs for foundation@dev.smoke are empty. The backend may not have received log uploads."
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
