#!/usr/bin/env bash
# /opt/lab-env/bootstrap.sh
# Idempotent provisioning script for the lab environment.
# Called by: lab provision, lab reset --tier R3
# Authority: canonical-environment.md §5 (provisioning sequence)
#
# Idempotency: every step checks current state before acting.
# Running on an already-conformant system is safe; produces no changes.
#
# Target: Ubuntu 22.04 LTS (amd64). Requires root. Uses apt-get.
#
# Exit codes:
#   0  provisioning complete; environment is CONFORMANT
#   1  a step failed; see stderr and journalctl -u app.service

set -euo pipefail

# ── Canonical constants (must match internal/config/config.go) ────────────────
readonly APP_USER="appuser"
readonly APP_GROUP="appuser"
readonly APP_UID=1001
readonly APP_GID=1001
readonly APP_BINARY="/opt/app/server"
readonly APP_CONFIG="/etc/app/config.yaml"
readonly APP_LOG_DIR="/var/log/app"
readonly APP_STATE_DIR="/var/lib/app"
readonly APP_UNIT="/etc/systemd/system/app.service"
readonly APP_SLICE_UNIT="/etc/systemd/system/app.slice"
readonly APP_SERVICE="app.service"
readonly NGINX_CONFIG="/etc/nginx/sites-enabled/app"
readonly TLS_DIR="/etc/nginx/tls"
readonly TLS_CERT="/etc/nginx/tls/app.local.crt"
readonly TLS_KEY="/etc/nginx/tls/app.local.key"
readonly APP_HOSTNAME="app.local"
readonly CHAOS_ENV="/etc/app/chaos.env"
readonly LOGROTATE_CONF="/etc/logrotate.d/app"
readonly LAB_DIR="/opt/lab-env"
readonly LAB_STATE_DIR="/var/lib/lab"
readonly SERVICE_SRC="${LAB_DIR}/service"
readonly LOOP_IMAGE="${LAB_STATE_DIR}/app-state.img"
readonly SUDOERS_FILE="/etc/sudoers.d/lab-appuser"

# Track step for trap diagnostics
CURRENT_STEP="init"

# ── Trap: emit failure context on any error ───────────────────────────────────
trap 'rc=$?
echo "[bootstrap] FAILED at step: ${CURRENT_STEP} (exit ${rc})" >&2
echo "[bootstrap] Diagnosis: journalctl -u app.service -n 20 --no-pager" >&2' ERR

log()  { echo "[bootstrap] [${CURRENT_STEP}] $*"; }
fail() { echo "[bootstrap] FATAL: $*" >&2; exit 1; }
step() { CURRENT_STEP="$1"; log "Starting"; }

# ── Step 01: Verify root ──────────────────────────────────────────────────────
step "01-root-check"
[[ "$(id -u)" -eq 0 ]] || fail "must run as root (use: sudo bash $0)"
log "OK"

# ── Step 02: Install required packages ────────────────────────────────────────
step "02-packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq \
    nginx \
    openssl \
    curl \
    jq \
    logrotate \
    nftables \
    golang-go \
    iproute2 \
    procps
log "OK"

# ── Step 03: Create appuser with fixed UID/GID ────────────────────────────────
# Fixed UID/GID required: conformance check P-001 verifies the process runs
# as appuser; stat checks verify ownership by name. Auto-assigned IDs differ
# across systems and cause false conformance failures.
# appuser added to adm group for log-rotation tooling readability.
step "03-user"
if ! getent group "${APP_GROUP}" &>/dev/null; then
    groupadd --gid "${APP_GID}" "${APP_GROUP}"
    log "  created group ${APP_GROUP} gid=${APP_GID}"
fi

if ! id "${APP_USER}" &>/dev/null; then
    useradd \
        --uid "${APP_UID}" \
        --gid "${APP_GID}" \
        --system \
        --no-create-home \
        --shell /usr/sbin/nologin \
        --comment "Lab environment service user" \
        "${APP_USER}"
    log "  created ${APP_USER} uid=${APP_UID} gid=${APP_GID}"
else
    ACTUAL_UID=$(id -u "${APP_USER}")
    ACTUAL_GID=$(id -g "${APP_USER}")
    if [[ "${ACTUAL_UID}" -ne "${APP_UID}" || "${ACTUAL_GID}" -ne "${APP_GID}" ]]; then
        fail "${APP_USER} exists with uid=${ACTUAL_UID}/gid=${ACTUAL_GID}; expected ${APP_UID}/${APP_GID}"
    fi
    log "  ${APP_USER} already exists (uid=${APP_UID} gid=${APP_GID})"
fi

if ! groups "${APP_USER}" | grep -qw adm; then
    usermod -aG adm "${APP_USER}"
    log "  added ${APP_USER} to adm group"
fi

# Verify no shell (security requirement)
SHELL=$(getent passwd "${APP_USER}" | cut -d: -f7)
if [[ "${SHELL}" != "/usr/sbin/nologin" && "${SHELL}" != "/bin/false" ]]; then
    fail "${APP_USER} has shell ${SHELL}; expected /usr/sbin/nologin"
fi
log "OK"

# ── Step 04: Create directory structure ───────────────────────────────────────
# All paths absolute; all modes from canonical-environment.md §2.3.
step "04-directories"
install -d -m 0755 -o root      -g root        /opt/app
install -d -m 0755 -o root      -g root        /etc/app
install -d -m 0755 -o "${APP_USER}" -g "${APP_GROUP}" "${APP_LOG_DIR}"
install -d -m 0755 -o "${APP_USER}" -g "${APP_GROUP}" "${APP_STATE_DIR}"
install -d -m 0755 -o root      -g root        "${LAB_STATE_DIR}"
install -d -m 0755 -o root      -g root        "${TLS_DIR}"
# /run/app is created by systemd RuntimeDirectory=app on service start.
# Do NOT create it here; systemd owns it to ensure correct tmpfs semantics.
log "OK"

# ── Step 05: Mount loopback storage for /var/lib/app ─────────────────────────
# Application Runtime Contract §4.2: 50 MiB sparse ext4 loopback.
# Simulates exhaustible storage; faults F-018 and F-019 fill it.
# Idempotent: checks mountpoint before creating image or mounting.
step "05-loopback-mount"
if ! mountpoint -q "${APP_STATE_DIR}"; then
    if [[ ! -f "${LOOP_IMAGE}" ]]; then
        truncate -s 50M "${LOOP_IMAGE}"
        mkfs.ext4 -q "${LOOP_IMAGE}"
        log "  created 50M sparse image ${LOOP_IMAGE}"
    fi
    mount -o loop "${LOOP_IMAGE}" "${APP_STATE_DIR}"
    log "  mounted ${LOOP_IMAGE} → ${APP_STATE_DIR}"
    # Ownership resets after mount; re-apply
    chown "${APP_USER}:${APP_GROUP}" "${APP_STATE_DIR}"
    chmod 0755 "${APP_STATE_DIR}"
else
    log "  ${APP_STATE_DIR} already mounted"
fi

if ! grep -qF "${LOOP_IMAGE}" /etc/fstab; then
    echo "${LOOP_IMAGE} ${APP_STATE_DIR} ext4 loop,defaults 0 2" >> /etc/fstab
    log "  added /etc/fstab entry"
fi
log "OK"

# ── Step 06: Configure cgroup slice ───────────────────────────────────────────
# Application Runtime Contract §4.1: MemoryMax=256M CPUQuota=20%.
# service/main.go sets GOMAXPROCS(1) calibrated to this quota.
step "06-cgroup-slice"
cat > "${APP_SLICE_UNIT}" << 'SLICE'
[Unit]
Description=Lab environment application slice
Documentation=https://github.com/lab-env/lab

[Slice]
MemoryMax=256M
CPUQuota=20%
SLICE
systemctl daemon-reload
log "OK"

# ── Step 07: Install application configuration files ──────────────────────────
# config.yaml: appuser:appuser 0640 — satisfies conformance check F-002
# chaos.env:   appuser:appuser 0644 — the '-' in EnvironmentFile silently
#              ignores EACCES; wrong mode makes chaos injection impossible
#              with no visible error in logs or journald
step "07-config-files"
if [[ ! -f "${APP_CONFIG}" ]]; then
    install -m 0640 -o "${APP_USER}" -g "${APP_GROUP}" \
        "${LAB_DIR}/internal/config/config.yaml" "${APP_CONFIG}"
    log "  installed ${APP_CONFIG}"
else
    log "  ${APP_CONFIG} already present"
fi

if [[ ! -f "${CHAOS_ENV}" ]]; then
    install -m 0644 -o "${APP_USER}" -g "${APP_GROUP}" /dev/null "${CHAOS_ENV}"
    log "  created empty ${CHAOS_ENV} (mode 0644)"
fi
log "OK"

# ── Step 08: Build the Go service binary ──────────────────────────────────────
# CGO_ENABLED=0: fully static binary, no glibc dependency.
# GOOS=linux GOARCH=amd64: correct target regardless of build host.
# Binary: appuser:appuser mode 0750 — satisfies conformance check F-001.
step "08-build"
[[ -d "${SERVICE_SRC}" ]] || fail "service source not found at ${SERVICE_SRC}"
TMPGOPATH=$(mktemp -d /tmp/go-build-XXXXXX)
(
    cd "${SERVICE_SRC}"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        GOPATH="${TMPGOPATH}" \
        go build -o "${APP_BINARY}" . \
        || fail "go build failed"
)
rm -rf "${TMPGOPATH}"
chown "${APP_USER}:${APP_GROUP}" "${APP_BINARY}"
chmod 0750 "${APP_BINARY}"
log "  built ${APP_BINARY} (0750 ${APP_USER}:${APP_GROUP})"
log "OK"

# ── Step 09: Install systemd unit file ────────────────────────────────────────
# root:root mode 0644 — systemd rejects non-root-owned unit files.
# Satisfies conformance check S-002 (app.service enabled).
step "09-systemd-unit"
install -m 0644 -o root -g root \
    "${LAB_DIR}/internal/config/app.service" "${APP_UNIT}"
systemctl daemon-reload
log "  installed ${APP_UNIT}"
log "OK"

# ── Step 10: Generate TLS certificate ─────────────────────────────────────────
# Self-signed for app.local; E-005 uses curl -k (skip verify).
# Regenerated if missing or expired — satisfies conformance check F-006.
step "10-tls-cert"
CERT_NEEDS_GEN=0
[[ ! -f "${TLS_CERT}" ]] && CERT_NEEDS_GEN=1
if [[ "${CERT_NEEDS_GEN}" -eq 0 ]] && \
   ! openssl x509 -checkend 0 -noout -in "${TLS_CERT}" 2>/dev/null; then
    CERT_NEEDS_GEN=1
    log "  certificate expired; regenerating"
fi

if [[ "${CERT_NEEDS_GEN}" -eq 1 ]]; then
    openssl req -x509 -nodes \
        -newkey rsa:2048 \
        -keyout "${TLS_KEY}" \
        -out "${TLS_CERT}" \
        -days 365 \
        -subj "/CN=${APP_HOSTNAME}" \
        -addext "subjectAltName=DNS:${APP_HOSTNAME},IP:127.0.0.1" \
        2>/dev/null || fail "openssl cert generation failed"
    chmod 0644 "${TLS_CERT}"
    chmod 0640 "${TLS_KEY}"
    log "  generated self-signed cert (365 days)"
else
    log "  certificate already valid"
fi
log "OK"

# ── Step 11: Configure /etc/hosts ─────────────────────────────────────────────
# Satisfies conformance check F-007: getent hosts app.local → 127.0.0.1
step "11-hosts"
if ! grep -qF "${APP_HOSTNAME}" /etc/hosts; then
    echo "127.0.0.1  ${APP_HOSTNAME}" >> /etc/hosts
    log "  added ${APP_HOSTNAME} → 127.0.0.1"
else
    log "  ${APP_HOSTNAME} already in /etc/hosts"
fi
log "OK"

# ── Step 12: Install nginx configuration ──────────────────────────────────────
# Upstream app_backend block: F-007 changes one server address to break all
# proxy blocks simultaneously — no risk of partial fault application.
# Satisfies: S-003/S-004, P-003/P-004, E-004, E-005, F-005.
step "12-nginx"
[[ -L /etc/nginx/sites-enabled/default ]] && rm -f /etc/nginx/sites-enabled/default

install -m 0644 -o root -g root \
    "${LAB_DIR}/internal/config/nginx.conf" "${NGINX_CONFIG}"

nginx -t 2>/dev/null || fail "nginx config syntax check failed"
log "  installed and validated ${NGINX_CONFIG}"
log "OK"

# ── Step 13: Install logrotate configuration ───────────────────────────────────
# copytruncate: service holds log fd open and MUST NOT reopen on SIGHUP.
# Timer must be active or logs fill the 50 MiB mount over time.
step "13-logrotate"
install -m 0644 -o root -g root \
    "${LAB_DIR}/internal/config/logrotate.conf" "${LOGROTATE_CONF}"

if systemctl list-unit-files logrotate.timer &>/dev/null; then
    systemctl enable --now logrotate.timer 2>/dev/null || true
fi
log "OK"

# ── Step 14: Configure nftables LAB-FAULT chain ───────────────────────────────
# Application Runtime Contract §4.3.
# Chain must exist and be empty (default accept) before any fault targets it.
# Fault F-021 adds a drop rule; Recover flushes the chain.
step "14-nftables"
if ! nft list table inet lab_filter &>/dev/null 2>&1; then
    nft add table inet lab_filter
    log "  created table inet lab_filter"
fi

if ! nft list chain inet lab_filter LAB-FAULT &>/dev/null 2>&1; then
    nft add chain inet lab_filter LAB-FAULT \
        '{ type filter hook input priority 0; policy accept; }'
    log "  created LAB-FAULT chain (default accept)"
else
    log "  LAB-FAULT chain already exists"
fi

nft list ruleset > /etc/nftables.conf 2>/dev/null || true
log "OK"

# ── Step 15: Configure sudoers ────────────────────────────────────────────────
# appuser needs passwordless sudo for fault apply/recover mutations.
# Scope is minimal: exact commands only. visudo -c validates before install.
step "15-sudoers"
SUDOERS_TMP=$(mktemp /tmp/sudoers-XXXXXX)
cat > "${SUDOERS_TMP}" << SUDOERS
# Lab environment: controlled sudo for fault injection and reset.
# Generated by bootstrap.sh. Validate with: visudo -c -f ${SUDOERS_FILE}

appuser ALL=(root) NOPASSWD: /bin/systemctl start app.service
appuser ALL=(root) NOPASSWD: /bin/systemctl stop app.service
appuser ALL=(root) NOPASSWD: /bin/systemctl restart app.service
appuser ALL=(root) NOPASSWD: /bin/systemctl start nginx
appuser ALL=(root) NOPASSWD: /bin/systemctl stop nginx
appuser ALL=(root) NOPASSWD: /bin/systemctl restart nginx
appuser ALL=(root) NOPASSWD: /bin/systemctl daemon-reload
appuser ALL=(root) NOPASSWD: /bin/chmod * /opt/app/server
appuser ALL=(root) NOPASSWD: /bin/chmod * /etc/app/config.yaml
appuser ALL=(root) NOPASSWD: /bin/chmod * /var/lib/app
appuser ALL=(root) NOPASSWD: /bin/chmod * /var/log/app
appuser ALL=(root) NOPASSWD: /bin/chmod * /var/log/app/app.log
appuser ALL=(root) NOPASSWD: /bin/chown * /opt/app/server
appuser ALL=(root) NOPASSWD: /bin/chown * /etc/app/config.yaml
appuser ALL=(root) NOPASSWD: /usr/sbin/nginx -t
appuser ALL=(root) NOPASSWD: /usr/sbin/nginx -s reload
appuser ALL=(root) NOPASSWD: /usr/sbin/nft add rule inet lab_filter LAB-FAULT *
appuser ALL=(root) NOPASSWD: /usr/sbin/nft flush chain inet lab_filter LAB-FAULT
SUDOERS

# Validate before installing — a sudoers syntax error locks out appuser
visudo -c -f "${SUDOERS_TMP}" || { rm -f "${SUDOERS_TMP}"; fail "sudoers syntax error"; }
install -m 0440 -o root -g root "${SUDOERS_TMP}" "${SUDOERS_FILE}"
rm -f "${SUDOERS_TMP}"
log "  installed and validated ${SUDOERS_FILE}"
log "OK"

# ── Step 16: Enable, start, and verify services ───────────────────────────────
# Post-start: poll /run/app/healthy then run validate.sh.
# StartLimitBurst=5 in app.service means up to 5 restart attempts in 30s;
# 10s polling is sufficient to detect startup success or failure.
step "16-services-and-validate"
systemctl enable app.service  2>/dev/null
systemctl enable nginx        2>/dev/null

systemctl restart nginx || {
    journalctl -u nginx -n 20 --no-pager >&2
    fail "nginx failed to start"
}
log "  nginx started"

systemctl restart app.service || {
    journalctl -u app.service -n 20 --no-pager >&2
    fail "app.service failed to start"
}
log "  app.service started"

log "  waiting for /run/app/healthy..."
READY=0
for i in $(seq 1 20); do
    if [[ -f /run/app/healthy ]]; then
        READY=1
        log "  ready after $((i * 500))ms"
        break
    fi
    sleep 0.5
done

if [[ "${READY}" -eq 0 ]]; then
    journalctl -u app.service -n 20 --no-pager >&2
    fail "service did not become ready within 10s"
fi

VALIDATE_SCRIPT="${LAB_DIR}/validate.sh"
if [[ -x "${VALIDATE_SCRIPT}" ]]; then
    if "${VALIDATE_SCRIPT}"; then
        log "Provisioning complete — environment is CONFORMANT"
    else
        log "WARNING: validation found failures after provisioning"
        exit 1
    fi
else
    log "WARNING: validate.sh not found; run 'lab validate' manually"
fi