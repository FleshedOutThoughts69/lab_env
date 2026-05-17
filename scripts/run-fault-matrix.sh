#!/usr/bin/env bash
# /opt/lab-env/scripts/run-fault-matrix.sh
# Runs all reversible faults through apply → validate → reset → validate.
# F-008 and F-014 (non-reversible) are excluded — run manually with care.
set -euo pipefail

# Verify dependencies
command -v jq >/dev/null 2>&1 || { echo "jq is required but not installed. Run bootstrap first."; exit 1; }
command -v ./lab >/dev/null 2>&1 || { echo "./lab not found. Run: go build -o lab . first."; exit 1; }

LAB="${LAB:-./lab}"
PASS=0
FAIL=0

run_fault() {
    local id="$1"
    echo ""
    echo "════════════════════════════════════════"
    echo "  $id"
    echo "════════════════════════════════════════"

    echo "  [pre-flight]"
    ${LAB} validate > /dev/null || { echo "  FAIL: pre-flight failed"; (( FAIL++ )); return; }

    echo "  [apply]"
    ${LAB} fault apply "${id}" --yes 2>&1 | tail -3

    echo "  [validate — expect failure]"
    if ${LAB} validate > /dev/null; then
        echo "  WARN: validate passed after apply (check fault definition)"
    else
        echo "  OK: validate correctly shows failures"
    fi

    echo "  [status]"
    ${LAB} status --json | jq -r '"  state=\(.state) fault=\(.active_fault // "none")"'

    echo "  [reset]"
    ${LAB} reset 2>&1 | tail -3

    echo "  [post-reset validate]"
    if ${LAB} validate > /dev/null; then
        echo "  PASS: environment is CONFORMANT after reset"
        (( PASS++ ))
    else
        echo "  FAIL: environment not conformant after reset"
        ${LAB} validate
        (( FAIL++ ))
    fi
}

cd /opt/lab-env

# Reversible faults — safe to run in sequence
REVERSIBLE="F-001 F-002 F-003 F-004 F-005 F-006 F-007 F-009 F-010
            F-013 F-015 F-016 F-017 F-018"

for fault in ${REVERSIBLE}; do
    run_fault "${fault}"
done

echo ""
echo "════════════════════════════════════════"
echo "  Results: ${PASS} passed, ${FAIL} failed"
echo "════════════════════════════════════════"

echo ""
echo "NOTE: F-008 and F-014 are non-reversible (require R3/binary rebuild)."
echo "      Run manually: ./lab fault apply F-008 --yes"
echo "      Recovery:     cd /opt/lab-env/service && go build -o /opt/app/server ."
echo "                    sudo chown appuser:appuser /opt/app/server && sudo chmod 750 /opt/app/server"
echo "                    sudo systemctl restart app.service && ./lab reset"

[[ "${FAIL}" -eq 0 ]] && exit 0 || exit 1