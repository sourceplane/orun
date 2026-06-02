#!/usr/bin/env bash
# check-object-model.sh — lint/grep gate for the orun-object-model rewrite.
#
# Enforces the hard constraints from specs/orun-object-model/claude-goals.md §3
# at the source level. The checks are forward-looking: each scans a target
# directory only if it exists, so the gate passes trivially in early milestones
# (M0) and becomes meaningful as the object-model packages land (M1+).
#
# Rules enforced:
#   1. No time.Now() in object-model production code (use an injected clock).
#   2. No json.Marshal of records in the higher object-model packages
#      (records MUST go through nodes.CanonicalEncode).
#   3. No raw "objects/" path literals outside internal/objectstore.
#   4. No new internal/state imports inside object-model packages (the legacy
#      module must not leak into the new model; deleted entirely at M12).
set -euo pipefail

fail=0
note() { echo "❌ $*"; fail=1; }

# Object-model package set.
OM_DIRS="internal/objectstore internal/nodes internal/nodewriter internal/objplan internal/execseal internal/objindex internal/objremote internal/workingview"

# 1. time.Now() ban (production files only).
for d in $OM_DIRS; do
  [ -d "$d" ] || continue
  if grep -RnE 'time\.Now\(\)' --include='*.go' "$d" | grep -v '_test\.go'; then
    note "time.Now() in $d — inject a clock.Clock instead"
  fi
done

# 2. json.Marshal ban in the higher packages (records use nodes.CanonicalEncode).
for d in internal/nodewriter internal/objindex internal/objremote internal/workingview; do
  [ -d "$d" ] || continue
  if grep -RnE 'json\.Marshal\(' --include='*.go' "$d" | grep -v '_test\.go'; then
    note "json.Marshal in $d — encode records via nodes.CanonicalEncode"
  fi
done

# 3. Raw object-path literals outside the object store.
if grep -RnE '"objects/' --include='*.go' internal cmd 2>/dev/null \
    | grep -v 'internal/objectstore' | grep -v '_test\.go'; then
  note "raw \"objects/\" path literal outside internal/objectstore"
fi

# 4. internal/state must not leak into object-model packages.
for d in $OM_DIRS; do
  [ -d "$d" ] || continue
  if grep -RnE '"github.com/sourceplane/orun/internal/state"' --include='*.go' "$d"; then
    note "internal/state imported by $d — object-model packages must not depend on legacy state"
  fi
done

if [ "$fail" -eq 0 ]; then
  echo "✅ object-model lint gate passed"
fi
exit "$fail"
