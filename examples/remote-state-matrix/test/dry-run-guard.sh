#!/usr/bin/env bash
# dry-run-guard.sh
#
# CI guard for the local remote-state harness.
#
# Verifies that run-local-harness.sh is syntactically valid, that its dry-run
# output contains the required command sequence, and that the assertion helpers
# in harness_helpers.sh behave correctly on synthetic inputs.
#
# Does not require Orun credentials, a live backend, or GitHub Actions.
#
# Usage:
#   ./test/dry-run-guard.sh
#   (also invoked by 'go test ./...' via harness_test.go)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HARNESS="${SCRIPT_DIR}/../run-local-harness.sh"
HELPERS="${SCRIPT_DIR}/../harness_helpers.sh"

pass() { echo "[guard] PASS: $*"; }
fail() { echo "[guard] FAIL: $*" >&2; exit 1; }

# ── load assertion helpers ─────────────────────────────────────────────────────
# shellcheck source=../harness_helpers.sh
source "${HELPERS}"

# ── 1. bash syntax checks ──────────────────────────────────────────────────────
bash -n "${HARNESS}"  || fail "Bash syntax error in run-local-harness.sh"
bash -n "${HELPERS}"  || fail "Bash syntax error in harness_helpers.sh"
bash -n "${SCRIPT_DIR}/dry-run-guard.sh" || fail "Bash syntax error in dry-run-guard.sh"
pass "Bash syntax checks"

# ── 2. dry-run command-sequence checks ────────────────────────────────────────
DRY_OUTPUT="$(ORUN_DRY_RUN=1 bash "${HARNESS}" 2>&1)"

# Exactly 2 foundation@dev.smoke run commands (process A and duplicate B)
FOUNDATION_CMDS="$(printf '%s\n' "${DRY_OUTPUT}" | grep -c "foundation@dev.smoke" || true)"
if [ "${FOUNDATION_CMDS}" -lt 2 ]; then
  printf '%s\n' "${DRY_OUTPUT}" >&2
  fail "Dry-run output must contain at least 2 foundation@dev.smoke command lines (got ${FOUNDATION_CMDS})"
fi
pass "Dry-run: at least 2 foundation@dev.smoke commands (${FOUNDATION_CMDS})"

# Exactly 1 api@dev.smoke run command (dep-wait process C)
API_CMDS="$(printf '%s\n' "${DRY_OUTPUT}" | grep -c "api@dev.smoke" || true)"
if [ "${API_CMDS}" -lt 1 ]; then
  printf '%s\n' "${DRY_OUTPUT}" >&2
  fail "Dry-run output must contain at least 1 api@dev.smoke command line (got ${API_CMDS})"
fi
pass "Dry-run: at least 1 api@dev.smoke command (${API_CMDS})"

# Required pattern checks
REQUIRED=(
  "auth status"
  "plan --name remote-state-e2e"
  "get plans"
  "export ORUN_EXEC_ID="
  "process B — duplicate"
  "dep-wait"
  "assert: duplicate claim"
  "assert_exactly_one_duplicate_claimant"
  "status --remote-state"
  "assert: foundation@dev.smoke status == success"
  "assert: api@dev.smoke"
  "logs   --remote-state"
  "assert: logs non-empty"
  "PASS"
)

for pattern in "${REQUIRED[@]}"; do
  if ! printf '%s\n' "${DRY_OUTPUT}" | grep -qF "${pattern}"; then
    printf '%s\n' "${DRY_OUTPUT}" >&2
    fail "Missing required pattern in dry-run output: '${pattern}'"
  fi
done
pass "Dry-run output contains all required command/assertion markers"

# ── 3. assert_exactly_one_duplicate_claimant — unit tests ─────────────────────
TMPDIR_G="$(mktemp -d)"
trap 'rm -rf "${TMPDIR_G}"' EXIT

MARKER="=== SMOKE: foundation"

# Helper: make a log file with N marker lines
make_log() {
  local path="$1" count="$2" i=0
  : > "${path}"
  while [ "${i}" -lt "${count}" ]; do
    echo "${MARKER} validate ===" >> "${path}"
    i=$(( i + 1 ))
  done
}

# PASS: A=2, B=0 (process A executed, B skipped)
make_log "${TMPDIR_G}/a.log" 2
make_log "${TMPDIR_G}/b.log" 0
assert_exactly_one_duplicate_claimant \
  "foundation@dev.smoke" "${MARKER}" 2 \
  "${TMPDIR_G}/a.log" "${TMPDIR_G}/b.log" \
  || fail "duplicate-claim PASS case (A=2 B=0) unexpectedly returned non-zero"
pass "duplicate-claim helper: PASS case (A=2 B=0)"

# PASS: A=0, B=2 (process B executed, A skipped)
make_log "${TMPDIR_G}/a.log" 0
make_log "${TMPDIR_G}/b.log" 2
assert_exactly_one_duplicate_claimant \
  "foundation@dev.smoke" "${MARKER}" 2 \
  "${TMPDIR_G}/a.log" "${TMPDIR_G}/b.log" \
  || fail "duplicate-claim PASS case (A=0 B=2) unexpectedly returned non-zero"
pass "duplicate-claim helper: PASS case (A=0 B=2)"

# FAIL: A=2, B=2 — both executed (duplicate execution detected)
make_log "${TMPDIR_G}/a.log" 2
make_log "${TMPDIR_G}/b.log" 2
if assert_exactly_one_duplicate_claimant \
    "foundation@dev.smoke" "${MARKER}" 2 \
    "${TMPDIR_G}/a.log" "${TMPDIR_G}/b.log" 2>/dev/null; then
  fail "duplicate-claim FAIL case (A=2 B=2) should have returned non-zero but did not"
fi
pass "duplicate-claim helper: FAIL case (A=2 B=2 — both executed)"

# FAIL: A=0, B=0 — neither executed
make_log "${TMPDIR_G}/a.log" 0
make_log "${TMPDIR_G}/b.log" 0
if assert_exactly_one_duplicate_claimant \
    "foundation@dev.smoke" "${MARKER}" 2 \
    "${TMPDIR_G}/a.log" "${TMPDIR_G}/b.log" 2>/dev/null; then
  fail "duplicate-claim FAIL case (A=0 B=0) should have returned non-zero but did not"
fi
pass "duplicate-claim helper: FAIL case (A=0 B=0 — neither executed)"

# FAIL: A=1, B=0 — partial/unexpected count (expected 2)
make_log "${TMPDIR_G}/a.log" 1
make_log "${TMPDIR_G}/b.log" 0
if assert_exactly_one_duplicate_claimant \
    "foundation@dev.smoke" "${MARKER}" 2 \
    "${TMPDIR_G}/a.log" "${TMPDIR_G}/b.log" 2>/dev/null; then
  fail "duplicate-claim FAIL case (A=1 B=0) should have returned non-zero but did not"
fi
pass "duplicate-claim helper: FAIL case (A=1 B=0 — partial count)"

# FAIL: A=3, B=0 — unexpected count (expected 2, got 3)
make_log "${TMPDIR_G}/a.log" 3
make_log "${TMPDIR_G}/b.log" 0
if assert_exactly_one_duplicate_claimant \
    "foundation@dev.smoke" "${MARKER}" 2 \
    "${TMPDIR_G}/a.log" "${TMPDIR_G}/b.log" 2>/dev/null; then
  fail "duplicate-claim FAIL case (A=3 B=0) should have returned non-zero but did not"
fi
pass "duplicate-claim helper: FAIL case (A=3 B=0 — unexpected count)"

# ── 4. assert_jobs_all_succeeded — unit tests ──────────────────────────────────
# PASS: both jobs present and succeeded
JSON_PASS='{"state":{"jobs":{"foundation@dev.smoke":{"status":"success"},"api@dev.smoke":{"status":"success"}}}}'
assert_jobs_all_succeeded "${JSON_PASS}" \
  "foundation@dev.smoke" "api@dev.smoke" \
  || fail "status helper PASS case (all success) unexpectedly returned non-zero"
pass "status helper: PASS case (all success)"

# PASS: status = completed also accepted
JSON_COMPLETED='{"state":{"jobs":{"foundation@dev.smoke":{"status":"completed"},"api@dev.smoke":{"status":"completed"}}}}'
assert_jobs_all_succeeded "${JSON_COMPLETED}" \
  "foundation@dev.smoke" "api@dev.smoke" \
  || fail "status helper PASS case (all completed) unexpectedly returned non-zero"
pass "status helper: PASS case (all completed)"

# FAIL: job missing from JSON
JSON_MISSING='{"state":{"jobs":{"foundation@dev.smoke":{"status":"success"}}}}'
if assert_jobs_all_succeeded "${JSON_MISSING}" \
    "foundation@dev.smoke" "api@dev.smoke" 2>/dev/null; then
  fail "status helper FAIL case (missing job) should have returned non-zero but did not"
fi
pass "status helper: FAIL case (job missing)"

# FAIL: job has status=pending
JSON_PENDING='{"state":{"jobs":{"foundation@dev.smoke":{"status":"success"},"api@dev.smoke":{"status":"pending"}}}}'
if assert_jobs_all_succeeded "${JSON_PENDING}" \
    "foundation@dev.smoke" "api@dev.smoke" 2>/dev/null; then
  fail "status helper FAIL case (pending) should have returned non-zero but did not"
fi
pass "status helper: FAIL case (status=pending)"

# FAIL: job has status=running
JSON_RUNNING='{"state":{"jobs":{"foundation@dev.smoke":{"status":"running"},"api@dev.smoke":{"status":"success"}}}}'
if assert_jobs_all_succeeded "${JSON_RUNNING}" \
    "foundation@dev.smoke" "api@dev.smoke" 2>/dev/null; then
  fail "status helper FAIL case (running) should have returned non-zero but did not"
fi
pass "status helper: FAIL case (status=running)"

# FAIL: job has status=failed
JSON_FAILED='{"state":{"jobs":{"foundation@dev.smoke":{"status":"failed"},"api@dev.smoke":{"status":"success"}}}}'
if assert_jobs_all_succeeded "${JSON_FAILED}" \
    "foundation@dev.smoke" "api@dev.smoke" 2>/dev/null; then
  fail "status helper FAIL case (failed) should have returned non-zero but did not"
fi
pass "status helper: FAIL case (status=failed)"

# FAIL: job has status=blocked
JSON_BLOCKED='{"state":{"jobs":{"foundation@dev.smoke":{"status":"blocked"},"api@dev.smoke":{"status":"success"}}}}'
if assert_jobs_all_succeeded "${JSON_BLOCKED}" \
    "foundation@dev.smoke" "api@dev.smoke" 2>/dev/null; then
  fail "status helper FAIL case (blocked) should have returned non-zero but did not"
fi
pass "status helper: FAIL case (status=blocked)"

# ── 5. structural checks on harness ───────────────────────────────────────────
if ! grep -q "export ORUN_EXEC_ID" "${HARNESS}"; then
  fail "Harness does not export ORUN_EXEC_ID"
fi
pass "ORUN_EXEC_ID is exported"

if ! grep -q "export ORUN_REMOTE_STATE" "${HARNESS}"; then
  fail "Harness does not export ORUN_REMOTE_STATE"
fi
pass "ORUN_REMOTE_STATE is exported"

if ! grep -q "assert_exactly_one_duplicate_claimant" "${HARNESS}"; then
  fail "Harness does not call assert_exactly_one_duplicate_claimant"
fi
pass "assert_exactly_one_duplicate_claimant is called in harness"

if ! grep -q "assert_jobs_all_succeeded" "${HARNESS}"; then
  fail "Harness does not call assert_jobs_all_succeeded"
fi
pass "assert_jobs_all_succeeded is called in harness"

if ! grep -q "HARNESS_PIDS" "${HARNESS}"; then
  fail "Harness does not use HARNESS_PIDS array for background PID tracking"
fi
pass "Background PID tracking (HARNESS_PIDS) present"

if ! grep -q "trap cleanup" "${HARNESS}"; then
  fail "Harness does not set a cleanup trap"
fi
pass "Signal-safe cleanup trap present"

if ! grep -q "INT" "${HARNESS}"; then
  fail "Harness trap does not include INT signal"
fi
pass "INT signal in cleanup trap"

if ! grep -q "TERM" "${HARNESS}"; then
  fail "Harness trap does not include TERM signal"
fi
pass "TERM signal in cleanup trap"

if ! grep -q "command -v jq" "${HARNESS}"; then
  fail "Harness missing jq preflight check"
fi
pass "jq preflight check present"

if ! grep -qE "command -v.*ORUN_BIN|command -v.*orun" "${HARNESS}"; then
  fail "Harness missing orun binary preflight check"
fi
pass "orun binary preflight check present"

if ! grep -q "not linked" "${HARNESS}" || ! grep -q "cloud link" "${HARNESS}"; then
  fail "Harness missing repo-linkage preflight check"
fi
pass "Repo-linkage preflight check present"

# ── done ───────────────────────────────────────────────────────────────────────
echo ""
echo "[guard] ════════════════════════════════════════════"
echo "[guard]   DRY-RUN GUARD PASSED"
echo "[guard] ════════════════════════════════════════════"
