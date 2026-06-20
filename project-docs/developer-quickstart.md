# DEVELOPER-QUICKSTART.md
## From fresh VM to passing fault matrix

This document is purely operational. Architecture lives in `docs/`. This document is copy-paste commands only.

---

## Prerequisites

**OS:** Ubuntu 22.04 LTS (amd64 or aarch64). cgroup v2 required for MemoryMax enforcement.

**Verify cgroup v2:**
```bash
stat -f -c %T /sys/fs/cgroup
# must print: cgroup2fs
```

**Go toolchain:** 1.22 or later (required for the service module).
```bash
go version
# go version go1.22.x linux/arm64 (or amd64)
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
[bootstrap] [02b-go-version] Starting
[bootstrap] [02b-go-version] Go 1.26.0 OK
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
[bootstrap] [08-build-service] Starting
[bootstrap] [08-build-service]   built /opt/app/server (0750 appuser:appuser)
[bootstrap] [08-build-service] OK
[bootstrap] [08b-build-lab-cli] Starting
[bootstrap] [08b-build-lab-cli]   built /usr/local/bin/lab
[bootstrap] [08b-build-lab-cli] OK
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

**After bootstrap completes:** run `sudo lab provision` to initialise the state file:
```bash
sudo lab provision
```

---

## 2. The `lab` binary is already installed

Bootstrap step 08b builds the control plane CLI and installs it at `/usr/local/bin/lab`. After bootstrap, `sudo lab` works from any directory. No separate build step is needed.

```bash
sudo lab --help
# Usage: lab <command> ...
```

---

## 3. Verify the environment is CONFORMANT

```bash
sudo lab validate
```

**Expected output:**
```
Running conformance suite (23 checks)...

  [PASS] S-001  app.service is active
  [PASS] S-002  app.service is enabled
  [PASS] S-003  nginx is active
  [PASS] S-004  nginx is enabled
  [PASS] P-001  App process runs as appuser
  [PASS] P-002  App listens on 127.0.0.1:8080
  [PASS] P-003  nginx listens on 0.0.0.0:80
  [PASS] P-004  nginx listens on 0.0.0.0:443
  [PASS] E-001  GET /health returns HTTP 200
  [PASS] E-002  GET / returns HTTP 200
  [PASS] E-003  /health body contains "status":"ok"
  [PASS] E-004  Response includes X-Proxy: nginx header
  [PASS] E-005  GET https://app.local/health returns 200 (skip verify)
  [PASS] F-001  /opt/app/server exists, owned appuser:appuser, mode 750
  [PASS] F-002  /etc/app/config.yaml exists, owned appuser:appuser, mode 640
  [PASS] F-003  /var/log/app/ exists, owned appuser:appuser, mode 755
  [PASS] F-004  /var/lib/app/ exists, owned appuser:appuser, mode 755
  [PASS] F-005  nginx configuration passes syntax check
  [PASS] F-006  TLS certificate exists and has not expired
  [PASS] F-007  app.local resolves to 127.0.0.1
  [PASS] L-001  /var/log/app/app.log exists and is non-empty
  [PASS] L-002  Last line of app.log is valid JSON
  [PASS] L-003  app.log contains a startup entry

CONFORMANT  23/23 checks passed
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

### B-001 baseline behavior — proxy timeout demo
B-001 is not an applyable fault; it is an observable property of the canonical environment. The nginx `proxy_read_timeout` (3s) is shorter than the `/slow` endpoint response time (5s). Accessing `/slow` via nginx produces a 504; direct access to the service succeeds:

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
#   "ts": "2026-06-17T12:00:00Z",
#   "pid": 86399,
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
sudo lab status --json | jq .
# {
#   "state": "CONFORMANT",
#   "active_fault": null,
#   "classification_valid": true,
#   ...
# }
```

The key fields to check after any operation:
- `state` — current canonical state (CONFORMANT / DEGRADED / BROKEN / UNKNOWN)
- `active_fault` — null when no fault active; fault ID string when DEGRADED
- `classification_valid` — false after an interrupt; run `sudo lab status` again to re-derive

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
sudo lab fault apply F-001

# Begin reset in background
sudo lab reset &
RESET_PID=$!

# Wait until the reset process is confirmed running before interrupting
for i in $(seq 1 20); do
    kill -0 $RESET_PID 2>/dev/null && break
    sleep 0.1
done

# Send interrupt — process must still be alive
if kill -0 $RESET_PID 2>/dev/null; then
    sudo kill -SIGINT $RESET_PID
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
sudo lab status
sudo lab reset    # restore to CONFORMANT if needed
```

**B2: Live fault matrix (see Section 7)**

**B3: Status/validate cycle**
```bash
sudo lab status --json | jq .state   # CONFORMANT
sudo lab validate > /dev/null
sudo lab status --json | jq .state   # still CONFORMANT (validate did not mutate)

# Manual break, then verify validate doesn't reconcile
sudo chmod 000 /var/lib/app
sudo lab validate > /dev/null        # exits 1, but...
cat /var/lib/lab/state.json | jq .state  # still CONFORMANT (validate cannot update)
sudo lab status --json | jq .state   # BROKEN (status reconciles)
sudo chmod 755 /var/lib/app
sudo lab reset
```

### Phase C — Invariant stress

```bash
# State file concurrency: status + fault apply concurrent
sudo lab fault apply F-004 &
for i in $(seq 1 50); do sudo lab status > /dev/null 2>&1; done
wait
sudo lab status --json | jq '{state, active_fault, classification_valid}'
# state and active_fault must be consistent (invariant I-2)

# Rapid reset cycles
for i in $(seq 1 10); do
    sudo lab fault apply F-004
    sudo lab reset
    sudo lab validate || { echo "FAIL at iteration $i"; break; }
    echo "Iteration $i: OK"
done
```

### Phase D — Golden fixture freeze

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

The service module has one external dependency (`gopkg.in/yaml.v3`). Build and deploy:

```bash
cd /opt/lab-env/service
CGO_ENABLED=0 go build -buildvcs=false -o /tmp/app-server .
sudo mv /tmp/app-server /opt/app/server
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
sudo CGO_ENABLED=0 go build -buildvcs=false -o /usr/local/bin/lab .
sudo lab validate
sudo lab status
```

### Changing a fault definition

```bash
cd /opt/lab-env
sudo go build -o /usr/local/bin/lab .
sudo lab fault apply F-004
sudo lab validate        # E-002 should fail
sudo lab reset
sudo lab validate        # all 23 should pass
```

### Changing a conformance check

```bash
cd /opt/lab-env
sudo go build -o /usr/local/bin/lab .
sudo lab validate --check S-001
sudo lab validate --check E-002
```

### Rebuilding after ANY source change

```bash
cd /opt/lab-env
sudo CGO_ENABLED=0 go build -buildvcs=false -o /usr/local/bin/lab . && echo "CLI OK"
cd service && CGO_ENABLED=0 go build -buildvcs=false -o /tmp/app-server . && sudo mv /tmp/app-server /opt/app/server && echo "Service OK"
sudo chown appuser:appuser /opt/app/server && sudo chmod 750 /opt/app/server
sudo systemctl restart app.service
sleep 1
sudo lab validate
```

---

## 7. Full fault matrix walkthrough

The fault matrix script is installed at `/opt/lab-env/scripts/run-fault-matrix.sh`. It applies every reversible fault, validates the expected failure pattern, resets, and re-validates.

**Run it:**
```bash
sudo bash /opt/lab-env/scripts/run-fault-matrix.sh
```

The script excludes F-008 and F-014 (non-reversible, manual only) and uses the `lab` binary from PATH (installed by bootstrap at `/usr/local/bin/lab`).

---

## 8. Common failure modes and triage

| Symptom | Likely cause | Diagnosis |
|---|---|---|
| `go build` fails: `module not found` | Go module cache empty, no internet | Run `go mod download` first, or use `go build -mod=vendor` |
| `go build` fails: `cannot find package` | Wrong Go version | `go version` (need 1.22+); `go env GOPATH` |
| Service fails to start | `appuser` UID wrong | `id -u appuser` (must be 1001) |
| Service fails to start | Config file missing | `ls -la /etc/app/config.yaml` |
| Service fails to start | Binary not executable | `ls -la /opt/app/server` (must be -rwxr-x---) |
| Service fails to start | Binary wrong ownership | `stat -c '%U:%G' /opt/app/server` (must be appuser:appuser) |
| Service fails to start | Binary wrong architecture | `file /opt/app/server` (must match `uname -m`) |
| Service exits after 5 restarts | `StartLimitBurst` hit — persistent fault | `journalctl -u app.service -n 30 --no-pager`; reset with `sudo systemctl reset-failed app.service` |
| All E-series checks fail | nginx not running | `systemctl status nginx` |
| All E-series checks fail | nginx wrong config | `sudo nginx -t` |
| E-004 fails (X-Proxy header missing) | nginx not proxying (direct hit) | `curl -I http://localhost/ \| grep X-Proxy` |
| E-005 fails (HTTPS) | TLS cert missing or expired | `openssl x509 -checkend 0 -in /etc/nginx/tls/app.local.crt` |
| Telemetry missing | `/run/app` not created by systemd | `ls /run/app` — if absent, `systemctl restart app.service` |
| Telemetry missing | Service crashed before first write | `journalctl -u app.service -n 10 --no-pager` |
| `lab status` shows UNKNOWN | classification_valid=false (post-interrupt) | `sudo lab status` again to reclassify |
| `lab fault apply` hangs | Stale lock file | `cat /var/lib/lab/lab.lock` then `kill -0 <pid>` — if not running, `sudo rm /var/lib/lab/lab.lock` |
| Chaos injection has no effect | chaos.env wrong permissions | `stat /etc/app/chaos.env` (must be 0644) |
| Chaos injection has no effect | Service not restarted after editing chaos.env | `sudo systemctl restart app.service` |
| OOM chaos doesn't kill process | cgroup limit not enforced | `systemctl show app.service \| grep MemoryMax` (must be 268435456) |
| OOM chaos doesn't kill process | Swap enabled | `swapon --show` — if non-empty, `sudo swapoff -a` |
| Log file has null bytes | Logging opened without O_APPEND | `xxd /var/log/app/app.log \| grep ' 0000 '` |
| F-007 Apply has no effect | nginx.conf missing upstream block | `grep upstream /etc/nginx/sites-enabled/app` |
| `lab reset` fails | Active fault not in catalog | `sudo lab status` — if UNKNOWN, `sudo lab status` again first |
| Unit tests fail | Missing exported test hooks | Check that `SetDirForTest`, `SetStateTouchPathForTest`, `CanonicalFileEntry` are exported |
| Phase C concurrent test shows inconsistent state | Expected transient race — rerun | Run `sudo lab status` after; must resolve to consistent state |

---

## 9. R3 reset (non-reversible fault recovery)

Used when F-008 (SIGTERM ignored), F-014 (zombie accumulation), or the environment is severely broken.

> **Current implementation note:** F‑008 and F‑014 Apply return an error immediately (`binary rebuild required — implement in deployment pipeline`). The faults are not applied automatically. To fully test these faults, the service binary must be manually rebuilt with the appropriate ldflags. R3 reset is the recovery path regardless of how the fault was activated.

### General R3 recovery (severely broken environment)

```bash
# Re-run bootstrap — it is idempotent and will restore all canonical files,
# rebuild both binaries, and restart services.
sudo bash /opt/lab-env/scripts/bootstrap.sh

# Verify
sudo lab validate
# must print: CONFORMANT
```

---

## 10. Re-running bootstrap on an existing environment

Bootstrap is idempotent. Safe to re-run at any time:

```bash
sudo bash /opt/lab-env/scripts/bootstrap.sh
```

What it will skip (already exists): user creation, fstab entry, TLS cert (if valid), /etc/hosts entry, nftables chain (if present), logrotate timer (if active).

What it will always do: rebuild both binaries, overwrite canonical config files, reinstall systemd unit, restart services, run validate.sh.

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

# 3. Clear chaos
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
sudo lab fault list

# Get fault details
sudo lab fault info F-004

# Show state history
sudo lab history

# Reset to specific tier
sudo lab reset --tier R1    # service restart only
sudo lab reset --tier R2    # + restore canonical files
sudo lab reset --tier R3    # full reprovision via bootstrap

# Run a single conformance check
sudo lab validate --check E-001
sudo lab validate --check F-004

# Force status reclassification after interrupt
sudo lab status

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