# DEVELOPER-QUICKSTART.md
## From fresh VM to passing fault matrix

This document is purely operational. Architecture lives in `docs/`. This document is copy-paste commands only.

---

## Prerequisites

**OS:** Ubuntu 22.04 LTS (amd64). cgroup v2 required for MemoryMax enforcement.

**Verify cgroup v2:**
```bash
stat -f -c %T /sys/fs/cgroup
# must print: cgroup2fs
```

**Go toolchain:** 1.21 or later (required for `math/rand/v2` auto-seed, `log/slog`).
```bash
go version
# go version go1.21.x linux/amd64
```

**Repository must be at `/opt/lab-env`:**
```bash
sudo mkdir -p /opt/lab-env
sudo git clone <repo-url> /opt/lab-env
# or: sudo cp -r . /opt/lab-env
```

---

## 1. Bootstrap (run once on a fresh VM)

```bash
sudo bash /opt/lab-env/scripts/bootstrap.sh
```

**Expected output (abridged):**
```
[bootstrap] [01-root-check] Starting
[bootstrap] [01-root-check] OK
[bootstrap] [02-packages] Starting
[bootstrap] [02-packages] OK
[bootstrap] [03-user] Starting
[bootstrap] [03-user]   created group appuser gid=1001
[bootstrap] [03-user]   created user appuser uid=1001
[bootstrap] [03-user] OK
[bootstrap] [04-directories] Starting
[bootstrap] [04-directories] OK
[bootstrap] [05-loopback-mount] Starting
[bootstrap] [05-loopback-mount]   created 50M sparse image /var/lib/lab/app-state.img
[bootstrap] [05-loopback-mount]   mounted /var/lib/lab/app-state.img → /var/lib/app
[bootstrap] [05-loopback-mount] OK
[bootstrap] [06-cgroup-slice] Starting
[bootstrap] [06-cgroup-slice] OK
[bootstrap] [07-config-files] Starting
[bootstrap] [07-config-files]   installed /etc/app/config.yaml
[bootstrap] [07-config-files]   created empty /etc/app/chaos.env (mode 0644)
[bootstrap] [07-config-files] OK
[bootstrap] [08-build] Starting
[bootstrap] [08-build]   built /opt/app/server (0750 appuser:appuser)
[bootstrap] [08-build] OK
[bootstrap] [09-systemd-unit] Starting
[bootstrap] [09-systemd-unit]   installed /etc/systemd/system/app.service
[bootstrap] [09-systemd-unit] OK
[bootstrap] [10-tls-cert] Starting
[bootstrap] [10-tls-cert]   generated self-signed cert (365 days)
[bootstrap] [10-tls-cert] OK
[bootstrap] [11-hosts] Starting
[bootstrap] [11-hosts]   added app.local → 127.0.0.1
[bootstrap] [11-hosts] OK
[bootstrap] [12-nginx] Starting
[bootstrap] [12-nginx]   installed and validated /etc/nginx/sites-enabled/app
[bootstrap] [12-nginx] OK
[bootstrap] [13-logrotate] Starting
[bootstrap] [13-logrotate] OK
[bootstrap] [14-nftables] Starting
[bootstrap] [14-nftables]   created table inet lab_filter
[bootstrap] [14-nftables]   created LAB-FAULT chain (default accept)
[bootstrap] [14-nftables] OK
[bootstrap] [15-sudoers] Starting
[bootstrap] [15-sudoers]   installed and validated /etc/sudoers.d/lab-appuser
[bootstrap] [15-sudoers] OK
[bootstrap] [16-services-and-validate] Starting
[bootstrap] [16-services-and-validate]   nginx started
[bootstrap] [16-services-and-validate]   app.service started
[bootstrap] [16-services-and-validate]   ready after 1500ms
[bootstrap] [16-services-and-validate] Provisioning complete — environment is CONFORMANT
```

**If bootstrap fails:** check the step name in the error message, then:
```bash
journalctl -u app.service -n 30 --no-pager
```

---

## 2. Build the control plane CLI

The control plane depends on `github.com/spf13/cobra`. Fetch dependencies before building. On a VM with internet access:

```bash
cd /opt/lab-env
go mod download
go build -o lab .
```

**Offline / air-gapped VMs:** pre-populate the Go module cache on a connected machine and copy it, or use a vendor directory:
```bash
# On a connected machine:
cd /opt/lab-env && go mod vendor
# Copy the vendor/ directory to the VM alongside the source.

# Then on the VM (uses local vendor, no network):
go build -mod=vendor -o lab .
```

**Verify:**
```bash
./lab --help
# Usage: lab <command> ...
```

**Optional — add `lab` to PATH for use outside `/opt/lab-env`:**
```bash
sudo cp /opt/lab-env/lab /usr/local/bin/lab
# Now `lab` works from any directory.
# All subsequent quickstart commands use ./lab; substitute lab if using PATH.
```

---

## 3. Verify the environment is CONFORMANT

```bash
./lab validate
```

**Expected output:**
```
PASS [S-001] app.service is active
PASS [S-002] app.service is enabled
PASS [S-003] nginx is active
PASS [S-004] nginx is enabled
PASS [P-001] App process runs as appuser
PASS [P-002] App listens on 127.0.0.1:8080
PASS [P-003] nginx listens on 0.0.0.0:80
PASS [P-004] nginx listens on 0.0.0.0:443
PASS [E-001] GET /health returns HTTP 200
PASS [E-002] GET / returns HTTP 200
PASS [E-003] /health body contains "status":"ok"
PASS [E-004] Response includes X-Proxy: nginx header
PASS [E-005] GET https://app.local/health returns 200 (skip verify)
PASS [F-001] /opt/app/server exists, owned appuser:appuser, mode 750
PASS [F-002] /etc/app/config.yaml exists, owned appuser:appuser, mode 640
PASS [F-003] /var/log/app/ exists, owned appuser:appuser, mode 755
PASS [F-004] /var/lib/app/ exists, owned appuser:appuser, mode 755
PASS [F-005] nginx configuration passes syntax check
PASS [F-006] TLS certificate exists and has not expired
PASS [F-007] app.local resolves to 127.0.0.1
PASS [L-001] /var/log/app/app.log exists and is non-empty
PASS [L-002] Last line of app.log is valid JSON
PASS [L-003] app.log contains startup entry {"msg":"server started"}

=== CONFORMANT: 23/23 checks passed ===
CONFORMANT
```

**Exit code must be 0:**
```bash
echo $?
# 0
```

---

## 4. Component verification

### Service health
```bash
curl http://localhost/health
# {"status":"ok"}

curl http://localhost/
# {"status":"ok","env":"prod"}

curl -sk https://app.local/health
# {"status":"ok"}
```

### F-011 baseline behavior — proxy timeout demo
F-011 is not an applyable fault; it is an observable property of the canonical environment. The nginx `proxy_read_timeout` (3s) is shorter than the `/slow` endpoint response time (5s). Accessing `/slow` via nginx produces a 504; direct access to the service succeeds:

```bash
# Via nginx (times out — nginx cuts connection at 3s)
time curl -s -o /dev/null -w "%{http_code}" http://localhost/slow
# 504  (in ~3 seconds)

# Direct to service (bypasses nginx — waits full 5s)
time curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8080/slow
# 200  (in ~5 seconds)
```

`lab validate` exits 0 while this is active — it is baseline behavior, not a conformance failure.

### Signal files
```bash
cat /run/app/status
# Running

cat /run/app/app.pid
# 12345  (actual PID)

ls -la /run/app/
# healthy, loading (absent), app.pid, status, telemetry.json
```

### Telemetry
```bash
cat /run/app/telemetry.json | jq .
# {
#   "ts": "2026-01-01T12:00:00Z",
#   "pid": 12345,
#   "uptime_seconds": 42,
#   "cpu_percent": 0.1,
#   "memory_rss_mb": 12.3,
#   "open_fds": 8,
#   "disk_usage_percent": 2.1,
#   "inode_usage_percent": 0.4,
#   "requests_total": 3,
#   "errors_total": 0,
#   "chaos_active": false,
#   "chaos_modes": []
# }
```

### systemd status
```bash
systemctl status app.service
# ● app.service - Lab environment subject application
#    Active: active (running)

systemctl status nginx
# ● nginx.service - A high performance web server
#    Active: active (running)
```

### nftables LAB-FAULT chain
```bash
sudo nft list chain inet lab_filter LAB-FAULT
# table inet lab_filter {
#   chain LAB-FAULT {
#     type filter hook input priority filter; policy accept;
#   }
# }
```

### Control plane state
```bash
./lab status --json | jq .
# {
#   "state": "CONFORMANT",
#   "active_fault": null,
#   "classification_valid": true,
#   "last_validate": "2026-01-01T12:00:00Z",
#   "services": {
#     "app": { "active": true, "enabled": true },
#     "nginx": { "active": true, "enabled": true }
#   },
#   "endpoints": {
#     "health": { "reachable": true, "status_code": 200 },
#     "root": { "reachable": true, "status_code": 200 }
#   },
#   "history": []
# }
```

The key fields to check after any operation:
- `state` — current canonical state (CONFORMANT / DEGRADED / BROKEN / UNKNOWN)
- `active_fault` — null when no fault active; fault ID string when DEGRADED
- `classification_valid` — false after an interrupt; run `./lab status` again to re-derive

---

## 5. Test suite execution

### Phase 0 — Unit tests (no VM required, fast)

```bash
# Control plane unit tests
cd /opt/lab-env
go test ./...

# Service unit tests
cd /opt/lab-env/service
go test ./...
```

**Expected:** all tests pass, no failures. Takes ~5-10 seconds.

**Run with race detector:**
```bash
go test -race ./...
```

### Phase A — Pre-flight fixes verification

```bash
# Verify config drift clean (must produce no output)
grep -rn '"/etc/app\|"/var/lib/app\|"/opt/app\|"appuser\|"0\.0\.0\.0:80\|127\.0\.0\.1:8080' \
  /opt/lab-env/internal/ /opt/lab-env/cmd/ \
  | grep -v 'config\.go\|_test\.go\|\.md\|Symptom\|Observable\|MutationDisplay'

# Run shell conformance suite
sudo bash /opt/lab-env/scripts/validate.sh
# must print: CONFORMANT
```

### Phase B — Live system validation

**B1: Interrupt path (requires running service)**
```bash
# Apply a fault that takes longer to reset (F-001 involves service restart)
./lab fault apply F-001

# Begin reset in background
./lab reset &
RESET_PID=$!

# Wait until the reset process is confirmed running before interrupting
# (avoids the race where sleep 0.2 is shorter than the reset itself)
for i in $(seq 1 20); do
    kill -0 $RESET_PID 2>/dev/null && break
    sleep 0.1
done

# Send interrupt — process must still be alive
if kill -0 $RESET_PID 2>/dev/null; then
    kill -SIGINT $RESET_PID
else
    echo "Reset completed before interrupt could be sent — retry with a slower fault"
fi

wait $RESET_PID
echo "Exit code: $?"
# Exit code: 4  (interrupted with side effects)
# Exit code: 0  (completed cleanly before interrupt arrived — retry)

# Verify classification invalidated (only relevant if exit code was 4)
cat /var/lib/lab/state.json | jq '{state, classification_valid}'
# { "state": "...", "classification_valid": false }

# Verify audit entry
grep '"entry_type":"interrupt"' /var/lib/lab/audit.log

# Reclassify from runtime evidence
./lab status
./lab reset    # restore to CONFORMANT if needed
```

**B2: Live fault matrix (see Section 7)**

**B3: Status/validate cycle**
```bash
./lab status --json | jq .state   # CONFORMANT
./lab validate > /dev/null
./lab status --json | jq .state   # still CONFORMANT (validate did not mutate)

# Manual break, then verify validate doesn't reconcile
sudo chmod 000 /var/lib/app
./lab validate > /dev/null        # exits 1, but...
cat /var/lib/lab/state.json | jq .state  # still CONFORMANT (validate cannot update)
./lab status --json | jq .state   # BROKEN (status reconciles)
sudo chmod 755 /var/lib/app
./lab reset
```

### Phase C — Invariant stress

```bash
# State file concurrency: status + fault apply concurrent
./lab fault apply F-004 &
for i in $(seq 1 50); do ./lab status > /dev/null 2>&1; done
wait
./lab status --json | jq '{state, active_fault, classification_valid}'
# state and active_fault must be consistent (invariant I-2)

# Rapid reset cycles
for i in $(seq 1 10); do
    ./lab fault apply F-004
    ./lab reset
    ./lab validate || { echo "FAIL at iteration $i"; break; }
    echo "Iteration $i: OK"
done
```

### Phase D — Golden fixture freeze (after H-001 fix)

**Known issue H-001:** `cmd/status.go` `buildStatusResult` currently has a `code := 502 // best guess` fallback for endpoint status codes. Golden fixtures must not be expanded until this is fixed, because the incorrect value would be baked in as "correct." The regression guard is `TestRenderStatus_JSON_EndpointCodesNotGuessed` — it will fail if the fallback is present. Fix first, then expand fixtures.

```bash
# Verify H-001 is fixed (test must pass)
cd /opt/lab-env
go test ./internal/output/... -run TestRenderStatus_JSON_EndpointCodesNotGuessed
# --- PASS: TestRenderStatus_JSON_EndpointCodesNotGuessed

# Regenerate golden fixtures
UPDATE_GOLDEN=1 go test ./internal/output/...

# Verify no unexpected field changes
git diff testdata/golden/
```

---

## 6. Development edit-test cycle

### Changing the service binary

The service module has one external dependency (`gopkg.in/yaml.v3`). Fetch it before building if not already cached:

```bash
cd /opt/lab-env/service
go mod download          # fetch yaml.v3 if not cached (needs internet or vendor/)
go build -o /opt/app/server .
sudo chown appuser:appuser /opt/app/server
sudo chmod 750 /opt/app/server
sudo systemctl restart app.service
sleep 1
curl http://localhost/health
# {"status":"ok"}
```

### Changing the control plane CLI

```bash
cd /opt/lab-env
go build -o lab .
./lab validate
./lab status
```

### Changing a fault definition

```bash
cd /opt/lab-env
go build -o lab .
./lab fault apply F-004
./lab validate        # E-002 should fail
./lab reset
./lab validate        # all 23 should pass
```

### Changing a conformance check

```bash
cd /opt/lab-env
go build -o lab .
./lab validate --check S-001
./lab validate --check E-002
```

### Rebuilding after ANY source change

```bash
cd /opt/lab-env
go mod download && go build -o lab . && echo "CLI OK"
cd service && go mod download && go build -o /opt/app/server . && echo "Service OK"
sudo chown appuser:appuser /opt/app/server && sudo chmod 750 /opt/app/server
sudo systemctl restart app.service
sleep 1
./lab validate
```

---

## 7. Full fault matrix walkthrough

Run this script from the `/opt/lab-env` directory. It applies every fault, validates the expected failure pattern, resets, and re-validates. Non-reversible faults (F-008, F-014) require `--yes` and an R3 reset (binary rebuild).

```bash
#!/usr/bin/env bash
# /opt/lab-env/scripts/run-fault-matrix.sh
# Runs all reversible faults through apply → validate → reset → validate.
# F-008 and F-014 (non-reversible) are excluded — run manually with care.
set -euo pipefail

# Verify dependencies
command -v jq >/dev/null 2>&1 || { echo "jq is required but not installed. Run bootstrap first."; exit 1; }
command -v ./lab >/dev/null 2>&1 || { echo "./lab not found. Run: go build -o lab . first."; exit 1; }

LAB="./lab"
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

# Non-reversible faults — manual only
echo ""
echo "NOTE: F-008 and F-014 are non-reversible (require R3/binary rebuild)."
echo "      Run manually: ./lab fault apply F-008 --yes"
echo "      Recovery:     cd /opt/lab-env/service && go build -o /opt/app/server ."
echo "                    sudo chown appuser:appuser /opt/app/server && sudo chmod 750 /opt/app/server"
echo "                    sudo systemctl restart app.service && ./lab reset"

[[ "${FAIL}" -eq 0 ]] && exit 0 || exit 1
```

**Run it:**
```bash
chmod +x /opt/lab-env/scripts/run-fault-matrix.sh
sudo /opt/lab-env/scripts/run-fault-matrix.sh
```

### Baseline behaviors (F-011, F-012) — observe without applying

F-011 and F-012 are not faults — they are observable properties of the canonical environment. `lab fault apply` will refuse them. Verify them separately:

**F-011 — nginx proxy timeout shorter than `/slow` response:**
```bash
# Already covered in Section 4 F-011 demo above.
# Quick check: validate exits 0 (not a failure)
./lab validate
# CONFORMANT — F-011 produces no failing checks
```

**F-012 — TLS certificate is self-signed (expected):**
```bash
# Certificate exists and is valid (F-006 check passes)
openssl x509 -noout -text -in /etc/nginx/tls/app.local.crt | grep -E 'Subject:|Issuer:|Not After'
# Subject: CN=app.local
# Issuer: CN=app.local  ← self-signed (Subject = Issuer)
# Not After: <365 days from provisioning>

# E-005 uses -k (skip verify) because the cert is self-signed
curl -sk https://app.local/health
# {"status":"ok"}
```

---

## 8. Common failure modes and triage

> **Note on `./lab validate` vs `validate.sh`:** `./lab validate` runs the Go conformance engine and produces structured output with PASS/FAIL per check. `sudo bash /opt/lab-env/scripts/validate.sh` runs the same checks as bash commands and is used by bootstrap. Both exit 0 for CONFORMANT. The Go CLI output is more detailed; the shell script is simpler for quick manual runs.

| Symptom | Likely cause | Diagnosis |
|---|---|---|
| `go build` fails: `module not found` | Go module cache empty, no internet | `go mod download` first; or `go build -mod=vendor` with vendor/ present |
| `go build` fails: `cannot find package` | Wrong Go version | `go version` (need 1.21+); `go env GOPATH` |
| Service fails to start | `appuser` UID wrong | `id -u appuser` (must be 1001) |
| Service fails to start | Config file missing | `ls -la /etc/app/config.yaml` |
| Service fails to start | Binary not executable | `ls -la /opt/app/server` (must be -rwxr-x---) |
| Service fails to start | Binary wrong ownership | `stat -c '%U:%G' /opt/app/server` (must be appuser:appuser) |
| Service exits after 5 restarts | `StartLimitBurst` hit — persistent fault | `journalctl -u app.service -n 30 --no-pager` |
| All E-series checks fail | nginx not running | `systemctl status nginx` |
| All E-series checks fail | nginx wrong config | `sudo nginx -t` |
| E-004 fails (X-Proxy header missing) | nginx not proxying (direct hit) | `curl -I http://localhost/ \| grep X-Proxy` |
| E-005 fails (HTTPS) | TLS cert missing or expired | `openssl x509 -checkend 0 -in /etc/nginx/tls/app.local.crt` |
| Telemetry missing | `/run/app` not created by systemd | `ls /run/app` — if absent, `systemctl restart app.service` |
| Telemetry missing | Service crashed before first write | `journalctl -u app.service -n 10 --no-pager` |
| `lab status` shows UNKNOWN | classification_valid=false (post-interrupt) | `./lab status` again to reclassify |
| `lab fault apply` hangs | Stale lock file | `cat /var/lib/lab/lab.lock` then `kill -0 <pid>` — if not running, `rm /var/lib/lab/lab.lock` |
| Chaos injection has no effect | chaos.env wrong permissions | `stat /etc/app/chaos.env` (must be 0644) |
| Chaos injection has no effect | Service not restarted after editing chaos.env | `sudo systemctl restart app.service` |
| OOM chaos doesn't kill process | cgroup limit not enforced | `systemctl show app.service \| grep MemoryMax` (must be 268435456) |
| OOM chaos doesn't kill process | Swap enabled | `swapon --show` — if non-empty, `sudo swapoff -a` |
| Log file has null bytes | Logging opened without O_APPEND | `xxd /var/log/app/app.log \| grep ' 0000 '` |
| F-007 Apply has no effect | nginx.conf missing upstream block | `grep upstream /etc/nginx/sites-enabled/app` |
| `lab reset` fails | Active fault not in catalog | `./lab status` — if UNKNOWN, `./lab status` again first |
| Unit tests fail | Missing exported test hooks | Check that `SetDirForTest`, `SetStateTouchPathForTest`, `CanonicalFileEntry` are exported |
| Phase C concurrent test shows inconsistent state | Expected transient race — rerun | Run `./lab status` after; must resolve to consistent state |

---

## 9. R3 reset (non-reversible fault recovery)

Used when F-008 (SIGTERM ignored), F-014 (zombie accumulation), or the environment is severely broken.

### F-008 — SIGTERM ignored (apply + observe + recover)

```bash
# Apply (requires --yes; non-reversible)
./lab fault apply F-008 --yes

# Observe: lab validate exits 0 while fault is active
# The fault is invisible until systemctl stop is attempted
./lab validate
# === CONFORMANT (or DEGRADED): 23 checks pass ===
# F-008 does NOT produce failing checks while the service runs

# Prove the fault: stopping the service should hang ~90 seconds (SIGKILL required)
sudo timeout 5 systemctl stop app.service || echo "SIGTERM ignored — stop timed out as expected"

# Recover (R3 — rebuild required to clear the compiled-in flag)
cd /opt/lab-env/service
go mod download
go build -o /opt/app/server .
sudo chown appuser:appuser /opt/app/server
sudo chmod 750 /opt/app/server
sudo systemctl start app.service
sleep 2
./lab reset
./lab validate
# must print: CONFORMANT
```

### F-014 — zombie accumulation (apply + observe + recover)

```bash
# Apply (requires --yes; non-reversible)
./lab fault apply F-014 --yes

# Observe: zombies accumulate over time as requests complete
# Check zombie count (increases with each / request)
curl http://localhost/ > /dev/null
ps aux | grep -c 'Z'
# count increases after requests

# Recover (R3 — same binary rebuild path as F-008)
cd /opt/lab-env/service
go mod download
go build -o /opt/app/server .
sudo chown appuser:appuser /opt/app/server
sudo chmod 750 /opt/app/server
sudo systemctl restart app.service
sleep 2
./lab reset
./lab validate
# must print: CONFORMANT
```

### General R3 recovery (severely broken environment)

```bash
# Step 1: Rebuild the binary (clears any compiled-in fault flags)
cd /opt/lab-env/service
go mod download
go build -o /opt/app/server .
sudo chown appuser:appuser /opt/app/server
sudo chmod 750 /opt/app/server

# Step 2: Re-run bootstrap to restore all canonical files and restart services
# (Bootstrap is idempotent — safe to run on an existing environment.
#  It will NOT rebuild the binary again since it was just built above.
#  It WILL overwrite config files, reinstall the systemd unit, and restart services.)
sudo bash /opt/lab-env/scripts/bootstrap.sh

# Step 3: Verify
./lab validate
# must print: CONFORMANT
```

---

## 10. Re-running bootstrap on an existing environment

Bootstrap is idempotent. Safe to re-run at any time:

```bash
sudo bash /opt/lab-env/scripts/bootstrap.sh
```

What it will skip (already exists): user creation, fstab entry, TLS cert (if valid), /etc/hosts entry, nftables chain (if present), logrotate timer (if active).

What it will always do: overwrite canonical config files, reinstall systemd unit, restart services, run validate.sh.

---

## 11. Chaos injection workflow

```bash
# 1. Add latency
echo "CHAOS_LATENCY_MS=400" | sudo tee /etc/app/chaos.env
sudo chmod 0644 /etc/app/chaos.env
sudo systemctl restart app.service

# 2. Verify effect
time curl http://localhost/       # should take ~400ms
time curl http://localhost/health # should return immediately (latency exempt)
cat /run/app/telemetry.json | jq '{chaos_active, chaos_modes}'
# { "chaos_active": true, "chaos_modes": ["latency"] }

# 3. Verify via control plane
./lab validate    # L-004 may flag chaos_active

# 4. Clear chaos
echo "" | sudo tee /etc/app/chaos.env
sudo systemctl restart app.service
cat /run/app/telemetry.json | jq '{chaos_active, chaos_modes}'
# { "chaos_active": false, "chaos_modes": [] }
```

**Available chaos variables:**
```bash
CHAOS_LATENCY_MS=400        # add 400ms to every non-/health request
CHAOS_DROP_PERCENT=50       # drop 50% of requests with 503
CHAOS_OOM_TRIGGER=1         # allocate until OOM killed (non-recoverable without rebuild)
CHAOS_IGNORE_SIGTERM=1      # mask SIGTERM (non-recoverable without rebuild)
```

---

## 12. Useful one-liners

```bash
# Watch telemetry live
watch -n 2 'cat /run/app/telemetry.json | jq .'

# Watch service log live
tail -f /var/log/app/app.log | jq .

# Check audit trail
tail -20 /var/lib/lab/audit.log | jq .

# List all faults
./lab fault list

# Get fault details
./lab fault info F-004

# Show state history
./lab history

# Reset to specific tier
./lab reset --tier R1    # config/permissions only
./lab reset --tier R2    # + service files + restart
./lab reset --tier R3    # full rebuild via bootstrap

# Run a single conformance check
./lab validate --check E-001
./lab validate --check F-004

# Force status reclassification after interrupt
./lab status

# Run unit tests for one package
go test ./internal/state/...
go test ./service/logging/...

# Run with verbose output
go test -v ./internal/conformance/...

# Run with race detector
go test -race ./...

# Regenerate golden fixtures (after output format changes)
UPDATE_GOLDEN=1 go test ./internal/output/...
```