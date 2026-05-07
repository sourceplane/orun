#!/usr/bin/env bash
# harness_helpers.sh
#
# Pure assertion helpers for the remote-state-matrix local harness.
# Sourced by run-local-harness.sh and test/dry-run-guard.sh.
# Must not execute side effects on source — only define functions.

# assert_exactly_one_duplicate_claimant JOB_ID MARKER EXPECTED_COUNT LOG_A LOG_B
#
# Asserts that exactly one of the two claimant processes executed the job body:
#   - exactly one log contains EXPECTED_COUNT lines matching MARKER
#   - the other log contains zero matching lines
#
# PASS on:  count_a == expected && count_b == 0
#       OR  count_a == 0        && count_b == expected
#
# FAIL on all other cases:
#   - both logs contain matching lines (duplicate execution)
#   - neither log contains matching lines (job did not execute at all)
#   - partial/unexpected count in either log
#
# Prints both logs on failure.
assert_exactly_one_duplicate_claimant() {
  local job_id="$1" marker="$2" expected="$3" log_a="$4" log_b="$5"

  local count_a count_b
  # grep -c prints "0" on no matches and exits 1; use || true to suppress the
  # non-zero exit without appending a second "0" via || echo 0.
  count_a="$(grep -c "${marker}" "${log_a}" 2>/dev/null || true)"
  count_b="$(grep -c "${marker}" "${log_b}" 2>/dev/null || true)"

  local executor_label executor_count skip_count

  if   [ "${count_a}" -eq "${expected}" ] && [ "${count_b}" -eq 0 ]; then
    executor_label="process A"; executor_count="${count_a}"; skip_count="${count_b}"
  elif [ "${count_b}" -eq "${expected}" ] && [ "${count_a}" -eq 0 ]; then
    executor_label="process B"; executor_count="${count_b}"; skip_count="${count_a}"
  else
    echo "" >&2
    echo "  DUPLICATE CLAIM ASSERTION FAILED for job: ${job_id}" >&2
    echo "" >&2
    echo "  Expected: exactly one claimant log with ${expected} '${marker}' line(s)" >&2
    echo "            and the other claimant log with 0 matching lines." >&2
    echo "" >&2
    echo "  Got: process A = ${count_a} line(s), process B = ${count_b} line(s)" >&2
    if   [ "${count_a}" -gt 0 ] && [ "${count_b}" -gt 0 ]; then
      echo "  Diagnosis: BOTH processes executed the job body." >&2
    elif [ "${count_a}" -eq 0 ] && [ "${count_b}" -eq 0 ]; then
      echo "  Diagnosis: NEITHER process executed the job body." >&2
    else
      echo "  Diagnosis: unexpected marker count (expected ${expected}, got A=${count_a} B=${count_b})." >&2
    fi
    echo "" >&2
    echo "  Process A log (${log_a}):" >&2
    cat "${log_a}" >&2 2>/dev/null || echo "  (empty or missing)" >&2
    echo "" >&2
    echo "  Process B log (${log_b}):" >&2
    cat "${log_b}" >&2 2>/dev/null || echo "  (empty or missing)" >&2
    return 1
  fi

  echo "[harness] Duplicate claim OK for ${job_id}: ${executor_label} executed" \
    "(${executor_count} marker line(s)), other process had 0."
  return 0
}

# assert_jobs_all_succeeded STATUS_JSON JOB_ID [JOB_ID ...]
#
# Asserts that every listed job ID has status "success" or "completed" in the
# JSON produced by `orun status --remote-state --json`.
#
# Fails if:
#   - any job has a status other than success/completed (pending, running, failed, blocked, ...)
#   - any job is missing from the status JSON (.state.jobs key is absent)
#
# Prints the full status JSON on failure.
assert_jobs_all_succeeded() {
  local status_json="$1"; shift
  local all_ok=0

  for job_id in "$@"; do
    local job_status
    job_status="$(printf '%s' "${status_json}" \
      | jq -r --arg j "${job_id}" '.state.jobs[$j].status // empty' 2>/dev/null || true)"

    if [ "${job_status}" = "success" ] || [ "${job_status}" = "completed" ]; then
      echo "[harness]   ${job_id}: ${job_status}"
    else
      if [ -z "${job_status}" ]; then
        echo "" >&2
        echo "  STATUS ASSERTION FAILED: job '${job_id}' is missing from status JSON." >&2
      else
        echo "" >&2
        echo "  STATUS ASSERTION FAILED: job '${job_id}' has status '${job_status}'" \
          "(expected success or completed)." >&2
      fi
      all_ok=1
    fi
  done

  if [ "${all_ok}" -ne 0 ]; then
    echo "" >&2
    echo "  Full status JSON:" >&2
    printf '%s' "${status_json}" | jq . >&2 2>/dev/null || printf '%s\n' "${status_json}" >&2
    return 1
  fi

  return 0
}
