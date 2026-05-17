#!/usr/bin/env bash
# /opt/lab-env/validate.sh
# Shell-level conformance suite for the lab environment.
# Authoritative expression of the conformance spec in shell.
#
# Authority: conformance-model.md §3 (check catalog)
# Check commands derived verbatim from internal/conformance/catalog.go
# ObservableCommand fields — the two must stay in sync.
#
# Severity:
#   blocking  → failure causes exit 1 (NON-CONFORMANT)
#   degraded  → failure is reported but exit remains 0 (DEGRADED-CONFORMANT)
#
# Exit codes:
#   0  all blocking checks pass (CONFORMANT or DEGRADED-CONFORMANT)
#   1  at least one blocking check fails (NON-CONFORMANT)
#
# Output: one line per check to stderr; summary to stdout.
# Side effects: NONE. This script never modifies system state.
# Safe to run concurrently with lab status / lab fault apply.
#
# Usage:
#   sudo bash /opt/lab-env/validate.sh
#   # or via control plane:
#   lab validate

set -uo pipefail
# Note: no -e; we capture individual check failures without aborting the suite.

# ── Privilege check ───────────────────────────────────────────────────────────
# Some checks require root: nginx -t, openssl, stat on protected files.
# Running as root avoids false permission-denied failures masking real issues.
if [[ "$(id -u)" -ne 0 ]]; then
    echo "WARNING: not running as root; some checks may fail due to permissions" >&2
    echo "Recommend: sudo bash $0" >&2
fi

# ── Check runner infrastructure ───────────────────────────────────────────────
PASS=0
FAIL=0
DEGRADED_FAIL=0
FAILED_IDS=()

# check ID severity "assertion" command...
# severity: blocking | degraded
check() {
    local id="$1"
    local severity="$2"
    local assertion="$3"
    shift 3

    if "$@" >/dev/null 2>&1; then
        printf "  PASS  [%-6s] %s\n" "${id}" "${assertion}" >&2
        (( PASS++ )) || true
    else
        printf "  FAIL  [%-6s] %s\n" "${id}" "${assertion}" >&2
        FAILED_IDS+=("${id}")
        if [[ "${severity}" == "blocking" ]]; then
            (( FAIL++ )) || true
        else
            printf "         (degraded — does not affect exit code)\n" >&2
            (( DEGRADED_FAIL++ )) || true
        fi
    fi
}

echo "=== Conformance suite ===" >&2

# ── S-series: System state checks ────────────────────────────────────────────
# Observes: systemd unit states.
# S-series provides structural/explanatory context for E-series failures.
# When S-001 fails, E-series checks are run but marked Dependent in the
# control plane — they failed because S-001 failed, not independently.
echo "--- S: System state ---" >&2

check S-001 blocking \
    "app.service is active" \
    systemctl is-active app.service --quiet

check S-002 blocking \
    "app.service is enabled" \
    systemctl is-enabled app.service --quiet

check S-003 blocking \
    "nginx is active" \
    systemctl is-active nginx --quiet

check S-004 blocking \
    "nginx is enabled" \
    systemctl is-enabled nginx --quiet

# ── P-series: Process checks ──────────────────────────────────────────────────
echo "--- P: Process ---" >&2

check P-001 blocking \
    "App process runs as appuser" \
    bash -c "pgrep -u appuser server > /dev/null"

check P-002 blocking \
    "App listens on 127.0.0.1:8080" \
    bash -c "ss -ltnp | grep -q '127.0.0.1:8080'"

check P-003 blocking \
    "nginx listens on 0.0.0.0:80" \
    bash -c "ss -ltnp | grep -q '0.0.0.0:80'"

check P-004 blocking \
    "nginx listens on 0.0.0.0:443" \
    bash -c "ss -ltnp | grep -q '0.0.0.0:443'"

# ── E-series: Endpoint checks ─────────────────────────────────────────────────
# These are the primary semantic authority checks.
# E-001 passes / E-002 fails = F-004 or F-018 diagnostic pattern.
# E-004 is satisfied by nginx (adds X-Proxy header); service does NOT set it.
echo "--- E: Endpoint ---" >&2

check E-001 blocking \
    "GET /health returns HTTP 200" \
    bash -c "curl -sf http://localhost/health > /dev/null"

check E-002 blocking \
    "GET / returns HTTP 200" \
    bash -c "curl -sf http://localhost/ > /dev/null"

check E-003 blocking \
    '/health body contains "status":"ok"' \
    bash -c "curl -s http://localhost/health | jq -e '.status == \"ok\"' > /dev/null"

check E-004 blocking \
    "Response includes X-Proxy: nginx header" \
    bash -c "curl -sI http://localhost/ | grep -q 'X-Proxy: nginx'"

check E-005 blocking \
    "GET https://app.local/health returns 200 (skip verify)" \
    bash -c "curl -skf https://app.local/health > /dev/null"

# ── F-series: Filesystem checks ───────────────────────────────────────────────
# These observe file existence, ownership, and mode bits.
# Note: F-series check IDs share the F-NNN prefix with fault catalog IDs.
# They are separate namespaces: conformance checks vs fault definitions.
echo "--- F: Filesystem ---" >&2

check F-001 blocking \
    "/opt/app/server exists, owned appuser:appuser, mode 750" \
    bash -c "stat -c '%U:%G %a' /opt/app/server | grep -q 'appuser:appuser 750'"

check F-002 blocking \
    "/etc/app/config.yaml exists, owned appuser:appuser, mode 640" \
    bash -c "stat -c '%U:%G %a' /etc/app/config.yaml | grep -q 'appuser:appuser 640'"

check F-003 blocking \
    "/var/log/app/ exists, owned appuser:appuser, mode 755" \
    bash -c "stat -c '%U:%G %a' /var/log/app | grep -q 'appuser:appuser 755'"

check F-004 blocking \
    "/var/lib/app/ exists, owned appuser:appuser, mode 755" \
    bash -c "stat -c '%U:%G %a' /var/lib/app | grep -q 'appuser:appuser 755'"

check F-005 blocking \
    "nginx configuration passes syntax check" \
    nginx -t

check F-006 degraded \
    "TLS certificate exists and has not expired" \
    openssl x509 -checkend 0 -noout -in /etc/nginx/tls/app.local.crt

check F-007 blocking \
    "app.local resolves to 127.0.0.1" \
    bash -c "getent hosts app.local | grep -q '127.0.0.1'"

# ── L-series: Log checks ──────────────────────────────────────────────────────
# All L-series checks are degraded severity:
# they fail without affecting exit code (exit 0 = DEGRADED-CONFORMANT).
echo "--- L: Log ---" >&2

check L-001 degraded \
    "/var/log/app/app.log exists and is non-empty" \
    test -s /var/log/app/app.log

check L-002 degraded \
    "Last line of app.log is valid JSON" \
    bash -c "tail -1 /var/log/app/app.log | jq . > /dev/null 2>&1"

check L-003 degraded \
    'app.log contains startup entry {"msg":"server started"}' \
    bash -c "grep -q '\"msg\":\"server started\"' /var/log/app/app.log"

# ── Summary ───────────────────────────────────────────────────────────────────
echo "" >&2
TOTAL=$(( PASS + FAIL + DEGRADED_FAIL ))

if [[ "${FAIL}" -eq 0 && "${DEGRADED_FAIL}" -eq 0 ]]; then
    echo "=== CONFORMANT: ${PASS}/${TOTAL} checks passed ===" >&2
    echo "CONFORMANT"
    exit 0
elif [[ "${FAIL}" -eq 0 ]]; then
    echo "=== DEGRADED-CONFORMANT: ${PASS}/${TOTAL} passed, ${DEGRADED_FAIL} degraded failure(s) ===" >&2
    for id in "${FAILED_IDS[@]}"; do
        echo "  degraded: ${id}" >&2
    done
    echo "DEGRADED"
    exit 0
else
    echo "=== NON-CONFORMANT: ${FAIL} blocking failure(s), ${DEGRADED_FAIL} degraded failure(s) ===" >&2
    for id in "${FAILED_IDS[@]}"; do
        echo "  failed: ${id}" >&2
    done
    echo "NON-CONFORMANT"
    exit 1
fi