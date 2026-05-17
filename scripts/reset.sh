#!/usr/bin/env bash
# /opt/lab-env/scripts/reset.sh
# Thin shell wrapper that delegates to the lab control plane binary.
# Authority: canonical-environment.md §8.2
# Full reset tier semantics: control-plane-contract.md §4.6
#
# All reset logic lives in the lab binary. This script exists so that
# operators can type a flag-based reset command without knowing which
# tier that flag maps to. Each flag maps directly to a lab reset --tier
# invocation as documented in canonical-environment.md §8.2.
#
# Flag → tier mapping:
#   --config       R2   restore canonical config files + restart services
#   --permissions  R2   restore canonical ownership and mode bits
#   --logs         R1   truncate app.log (preserves file, correct ownership)
#   --state        R1   remove and recreate /var/lib/app/state
#   --full         R3   full reprovision (re-runs bootstrap)
#
# Applying and reversing faults is NOT done through this script:
#   lab fault apply <ID>        # apply a fault
#   lab reset --tier <R1|R2|R3> # reverse a fault
#
# After any reset, verify the environment is conformant:
#   lab validate
#
# Usage:
#   sudo bash /opt/lab-env/scripts/reset.sh --config
#   sudo bash /opt/lab-env/scripts/reset.sh --full
#   lab reset --tier R2   # equivalent to --config / --permissions
#
# Exit codes mirror lab reset exit codes (control-plane-contract.md §3.2):
#   0  reset completed; environment is CONFORMANT
#   1  reset failed; run lab status to determine current state
#   2  usage error

set -uo pipefail

# ── Locate the lab binary ─────────────────────────────────────────────────────
LAB_BIN="${LAB_BIN:-/usr/local/bin/lab}"

if [[ ! -x "${LAB_BIN}" ]]; then
    echo "reset.sh: lab binary not found at ${LAB_BIN}" >&2
    echo "reset.sh: run bootstrap.sh first to build and install the lab binary" >&2
    exit 1
fi

# ── Privilege check ───────────────────────────────────────────────────────────
if [[ "$(id -u)" -ne 0 ]]; then
    echo "reset.sh: must run as root (use: sudo bash $0 $*)" >&2
    exit 1
fi

# ── Argument parsing ──────────────────────────────────────────────────────────
if [[ $# -eq 0 ]]; then
    cat >&2 <<'EOF'
reset.sh: no flag provided

Usage:
  sudo bash reset.sh --config       R2: restore config files and restart services
  sudo bash reset.sh --permissions  R2: restore file ownership and mode bits
  sudo bash reset.sh --logs         R1: truncate app.log to empty
  sudo bash reset.sh --state        R1: remove and recreate /var/lib/app/state
  sudo bash reset.sh --full         R3: full reprovision (re-runs bootstrap)

For granular control use the lab CLI directly:
  lab reset --tier R1
  lab reset --tier R2
  lab reset --tier R3

Applying/reversing faults:
  lab fault apply <ID>
  lab reset --tier <R1|R2|R3>
EOF
    exit 2
fi

FLAG="${1}"

case "${FLAG}" in
    --config|--permissions)
        # Both --config and --permissions are R2 operations.
        # The lab binary restores canonical file contents, ownership, mode bits,
        # then restarts affected services. The two flags are synonyms here
        # because the lab R2 reset covers both operations atomically.
        echo "reset.sh: running R2 reset (restore config files and permissions)" >&2
        exec "${LAB_BIN}" reset --tier R2
        ;;

    --logs)
        # R1: truncate app.log to empty, preserving the file with correct
        # ownership (appuser:appuser 640). The service continues running.
        echo "reset.sh: running R1 reset (truncate app.log)" >&2
        exec "${LAB_BIN}" reset --tier R1
        ;;

    --state)
        # R1: remove and recreate /var/lib/app/state.
        # The service continues running.
        echo "reset.sh: running R1 reset (recreate state file)" >&2
        exec "${LAB_BIN}" reset --tier R1
        ;;

    --full)
        # R3: full reprovision — re-runs bootstrap.sh.
        # Use when the binary has been replaced (F-008, F-014) or the
        # service account has been modified.
        echo "reset.sh: running R3 full reprovision" >&2
        exec "${LAB_BIN}" reset --tier R3
        ;;

    --help|-h)
        cat <<'EOF'
reset.sh — thin wrapper over lab reset

Flags:
  --config       R2  restore canonical config files, restart affected services
  --permissions  R2  restore canonical ownership and mode bits
  --logs         R1  truncate /var/log/app/app.log to empty
  --state        R1  remove and recreate /var/lib/app/state
  --full         R3  full reprovision (re-runs bootstrap)

Equivalent lab CLI commands:
  lab reset --tier R1   (--logs, --state)
  lab reset --tier R2   (--config, --permissions)
  lab reset --tier R3   (--full)

After any reset, verify conformance:
  lab validate
EOF
        exit 0
        ;;

    *)
        echo "reset.sh: unknown flag: ${FLAG}" >&2
        echo "reset.sh: valid flags: --config --permissions --logs --state --full" >&2
        exit 2
        ;;
esac