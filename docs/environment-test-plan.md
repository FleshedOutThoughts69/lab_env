# Environment Test Plan

## Version 1.0.0

> **Scope:** this document covers testing of the lab environment as a product — the provisioned Ubuntu VM, the running services, and the control plane as an integrated system. Unit tests (which test the Go source in isolation with mock executors) are covered in `docs/testing-plan-revised.md`. This document covers what cannot be tested without a real OS: the provisioned file system, real systemd, real signal delivery, real nginx, and the boundary between what the environment tolerates and what breaks it.
>
> **Companion documents:** `docs/testing-plan-revised.md` (unit/contract/integration test plan), `docs/recovery-playbook.md` (hostile-state drills), `docs/fault-implementation-guide.md` (fault mechanics).

---

## §1 — Quick Verification Checklist

A 30-minute end-to-end check for a freshly provisioned VM. Run this in order after `bootstrap.sh` completes and after any major change to the environment. Each step has an expected outcome on the right — any deviation is a failure.

### 1.1 Baseline conformance

```bash
lab validate
```
**Expected:** `CONFORMANT` on stdout; exit 0; all 23 checks pass.

```bash
lab status --json | jq '{state, active_fault, classification_valid}'
```
**Expected:** `{"state":"CONFORMANT","active_fault":null,"classification_valid":true}`

---

### 1.2 One fault from each layer

Apply one representative fault from each layer, verify it, then reset. These five checks cover the full dependency chain.

**Filesystem layer — F-001 (missing config file)**

```bash
lab fault apply F-001
curl -s localhost/health                        # connection refused
lab status --json | jq .state                  # "DEGRADED"
lab validate | grep 'FAIL\|S-001'              # S-001 fails
lab reset --tier R2
lab validate                                    # CONFORMANT
```

**Permissions layer — F-004 (state dir unwritable)**

```bash
lab fault apply F-004
curl -s localhost/health                        # 200
curl -s localhost/                              # 500
lab validate | grep 'E-002'                    # E-002 fails; E-001 passes
lab reset --tier R2
lab validate                                    # CONFORMANT
```

**Proxy layer — F-007 (nginx wrong upstream)**

```bash
lab fault apply F-007
curl -s localhost/health                        # 502
curl -s 127.0.0.1:8080/health                  # 200 — app alive, proxy broken
lab validate | grep 'E-001'                    # E-series fails; P-002 passes
lab reset --tier R2
lab validate                                    # CONFORMANT
```

**Log layer — F-010 (log file deleted while running)**

```bash
lab fault apply F-010
lab validate                                    # exit 0 (degraded only)
ls /var/log/app/                               # empty — no app.log
lsof +L1 | grep app.log                       # deleted fd held by process
lab reset --tier R1
lab validate                                    # CONFORMANT
```

**Service config layer — F-002 (wrong listen port)**

```bash
lab fault apply F-002
ss -ltnp | grep 9090                           # app on wrong port
curl -s 127.0.0.1:9090/health                  # 200 direct
curl -s localhost/health                        # 502 via nginx
lab reset --tier R2
lab validate                                    # CONFORMANT
```

---

### 1.3 Non-reversible fault cycle (F-008)

```bash
lab fault apply F-008 --yes
lab validate                                    # exit 0 — silent fault
sudo timeout 12 systemctl stop app.service \
  || echo "TIMEOUT — SIGTERM ignored as expected"
sudo systemctl start app.service
lab reset --tier R3                             # full reprovision
lab validate                                    # CONFORMANT
```

---

### 1.4 State file and audit log integrity

```bash
cat /var/lib/lab/state.json | jq .            # valid JSON; no parse error
cat /var/lib/lab/audit.log | tail -5          # recent entries; valid JSON lines
lab history --last 10                          # shows recent transitions
```

---

### 1.5 Baseline network behaviours

```bash
# B-001: proxy timeout shorter than app response
time curl -v http://localhost/slow             # 504 in ~3 seconds
time curl 127.0.0.1:8080/slow                 # 200 in ~5 seconds

# B-002: self-signed TLS
curl -sk https://app.local/health             # 200 (skip verify)
curl -s  https://app.local/health             # SSL error (cert not trusted)
```

---

## §2 — Fault-by-Fault Manual Verification

Quick one-liner verification for each fault. Apply the fault, run the verification command, observe the expected output, then reset.

| Fault | Apply | Verification command | Expected output | Reset tier |
|---|---|---|---|---|
| F-001 | `lab fault apply F-001` | `journalctl -u app.service -n 3 --no-pager` | config-not-found in restart loop | R2 |
| F-002 | `lab fault apply F-002` | `ss -ltnp \| grep 9090` | app listening on wrong port | R2 |
| F-003 | `lab fault apply F-003` | `stat -c '%a' /etc/app/config.yaml` | `0` (mode 0000) | R2 |
| F-004 | `lab fault apply F-004` | `curl -s localhost/ \| jq .status` | `"error"` | R2 |
| F-005 | `lab fault apply F-005` | `stat -c '%a' /opt/app/server` | `640` | R2 |
| F-006 | `lab fault apply F-006` | `systemctl show app --property=Environment` | no `APP_ENV` entry | R2 |
| F-007 | `lab fault apply F-007` | `curl -so /dev/null -w '%{http_code}' localhost/health` | `502` | R2 |
| F-008 | `lab fault apply F-008 --yes` | `lab validate; echo $?` | `0` — fault silent while running | R3 |
| F-009 | `lab fault apply F-009` | `stat -c '%a' /var/log/app/app.log` | `0` (mode 0000) | R2 |
| F-010 | `lab fault apply F-010` | `ls /var/log/app/ \| wc -l` | `0` — no app.log on disk | R1 |
| F-013 | `lab fault apply F-013` | `systemctl is-active app` | `failed` | R2 |
| F-014 | `lab fault apply F-014 --yes` | `for i in $(seq 1 10); do curl -s localhost/ > /dev/null; done; ps aux \| grep -c ' Z '` | non-zero zombie count | R3 |
| F-015 | `lab fault apply F-015` | `sudo nginx -t 2>&1 \| grep emerg` | syntax error message | R2 |
| F-016 | `lab fault apply F-016` | `ss -ltnp \| grep '0.0.0.0:8080'` | app bound to all interfaces | R2 |
| F-017 | `lab fault apply F-017` | `curl -s localhost/ \| jq .env` | `""` (empty string) | R2 |
| F-018 | `lab fault apply F-018` | `df -i /var/lib/app \| awk 'NR==2{print $5}'` | `100%` | R2 |

**Reset command for all R2 faults:** `lab reset --tier R2`
**Reset command for R1 fault (F-010):** `lab reset --tier R1`
**Reset command for R3 faults (F-008, F-014):** `lab reset --tier R3`

---

## §3 — Supported Mutation Boundary

The environment is designed for learner-driven investigation. A learner with `sudo` can modify anything on the system. This section defines what the environment is resilient to, what silently breaks it, and what requires a reset to recover from.

### 3.1 Classification of External Mutations

**Resilient:** the environment detects or recovers from this automatically.

**Recoverable:** `lab reset --tier R1|R2|R3` restores full conformance. No manual steps.

**Breaks reset:** the mutation prevents `lab reset` from working correctly. Manual intervention required before reset.

**Unsupported:** the mutation is outside the designed scope. The environment is not expected to survive it.

---

### 3.2 System Package Operations

| Operation | Classification | Behavior | Recovery |
|---|---|---|---|
| `apt-get update` | **Resilient** | Package index update only; no service changes | None needed |
| `apt-get upgrade` (nginx minor version) | **Recoverable** | nginx config syntax is stable across minor versions; site config unchanged; R2 reset restores if needed | `lab reset --tier R2` |
| `apt-get upgrade` (nginx major version) | **Unsupported** | nginx 1.x → nginx 1.x with breaking directive changes could invalidate F-005 check or the site config | Re-run bootstrap.sh |
| `apt-get upgrade` (golang-go) | **Recoverable** | Go toolchain upgrade does not affect the installed binary; only matters on next binary rebuild (R3 reset) | Acceptable; R3 uses whatever `go` is installed |
| `apt-get install <new-package>` | **Resilient** | Adds packages; does not touch lab paths | None needed |
| `apt-get remove nginx` | **Breaks reset** | Removes nginx; R2 reset cannot restore service to running; re-run bootstrap | Re-run `bootstrap.sh` |
| `apt-get remove golang-go` | **Breaks R3** | R3 reset calls `go build`; will fail without Go toolchain | `sudo apt-get install golang-go` then retry `lab reset --tier R3` |
| `apt-get autoremove` | **Recoverable** | May remove unused packages; rarely touches lab dependencies | `lab validate` to check; re-run bootstrap if needed |

**Note on nginx upgrades:** the lab config uses only stable nginx directives (`proxy_pass`, `proxy_set_header`, `add_header`, `ssl_certificate`, `proxy_read_timeout`). These have been stable across nginx 1.14–1.25. An upgrade within Ubuntu 22.04's package repository is safe. Replacing nginx with a different web server (e.g., caddy, apache) is unsupported.

---

### 3.3 Service and Process Operations

| Operation | Classification | Behavior | Recovery |
|---|---|---|---|
| `sudo systemctl restart app.service` | **Resilient** | Restarts the service; conformance restored on next validate | None needed |
| `sudo systemctl stop app.service` | **Recoverable** | Service stops; S-001 and E-series fail; `lab reset --tier R1` or manual restart | `lab reset --tier R1` |
| `sudo systemctl disable app.service` | **Recoverable** | S-002 fails; service does not start on boot; R2 reset re-enables | `lab reset --tier R2` |
| `sudo systemctl mask app.service` | **Breaks reset** | R1 and R2 resets cannot start a masked unit; must unmask first | `sudo systemctl unmask app.service` then `lab reset` |
| `sudo kill -9 $(pgrep server)` | **Resilient** | systemd restarts via `Restart=on-failure`; back up within 2s | None needed (auto-recovery) |
| `sudo reboot` | **Recoverable** | All services auto-start; loopback mount auto-mounts via fstab; state.json persists | `lab status` after boot; likely CONFORMANT |
| `sudo poweroff` + cold start | **Recoverable** | Same as reboot | Same as reboot |
| `sudo kill -9 1` (PID 1) | **Unsupported** | Kernel panic or hard reboot | Re-provision if filesystem corrupt |

---

### 3.4 Filesystem Operations

| Operation | Classification | Behavior | Recovery |
|---|---|---|---|
| Edit `/etc/app/config.yaml` | **Recoverable** | Conforms until service restarts; restores with R2 | `lab reset --tier R2` |
| Edit `/etc/systemd/system/app.service` | **Recoverable** | Takes effect on daemon-reload + restart; R2 reset restores | `lab reset --tier R2` |
| Edit `/etc/nginx/sites-enabled/app` | **Recoverable** | Takes effect on nginx reload; R2 reset restores | `lab reset --tier R2` |
| Edit `/etc/nginx/nginx.conf` (global) | **Unsupported** | The global config is not managed by lab. If broken, nginx fails to start entirely. R2 reset only restores the site config, not the global config. | Manual restore from `/etc/nginx/nginx.conf.bak` or `apt-get reinstall nginx` |
| `sudo rm /opt/app/server` | **Recoverable** | Service enters restart loop; R3 rebuilds binary | `lab reset --tier R3` |
| `sudo chmod 000 /var/lib/lab/` | **Breaks reset** | `lab` cannot read or write state.json; all commands fail | `sudo chmod 755 /var/lib/lab` |
| `sudo rm /var/lib/lab/state.json` | **Recoverable** | Control plane starts fresh; detects CONFORMANT from runtime | `lab status` re-establishes state |
| `sudo rm /var/lib/lab/audit.log` | **Recoverable** | Audit log gap; new entries append to recreated file | None needed |
| Modify `/etc/hosts` (remove app.local) | **Recoverable** | F-007 check fails; R2 reset does NOT restore /etc/hosts (it is not embedded) | `echo '127.0.0.1  app.local' | sudo tee -a /etc/hosts` |
| Delete TLS cert (`/etc/nginx/tls/app.local.crt`) | **Recoverable** | E-005 fails; R3 or manual cert regeneration restores | `lab reset --tier R3` or run step 10 of bootstrap manually |
| `sudo umount /var/lib/app` | **Breaks reset** | R1/R2 resets write to the unmounted directory; files created there are lost; remount and re-run bootstrap | `sudo mount /var/lib/app` then `lab validate` |

---

### 3.5 User and Permission Operations

| Operation | Classification | Behavior | Recovery |
|---|---|---|---|
| `sudo passwd appuser` (set password) | **Unsupported** | appuser should have no password; setting one does not break the service but violates the security model | `sudo passwd -d appuser` |
| `sudo usermod -s /bin/bash appuser` (give shell) | **Unsupported** | Violates security boundary; service still runs but conformance model relies on no-login constraint | `sudo usermod -s /usr/sbin/nologin appuser` |
| `sudo userdel appuser` | **Breaks reset** | systemd cannot start service as deleted user; bootstrap fails at step 03 UID conflict check | Re-run bootstrap.sh (will recreate user) |
| `sudo userdel -r appuser` (remove home) | **Recoverable** | Same as above; bootstrap recreates the user cleanly | Re-run bootstrap.sh |
| `sudo usermod -u 1002 appuser` (change UID) | **Breaks reset** | Bootstrap step 03 fails: "appuser exists with uid=1002; expected 1001" | `sudo usermod -u 1001 appuser` |
| `sudo chmod -R 000 /etc/sudoers.d/` | **Breaks reset** | sudo stops working entirely; cannot run lab commands with elevated privileges | Physical/VM console access to restore; `sudo chmod 0440 /etc/sudoers.d/lab-appuser` |

---

### 3.6 Network and Firewall Operations

| Operation | Classification | Behavior | Recovery |
|---|---|---|---|
| `sudo ufw enable` | **Breaks conformance** | Blocks ports 80/443; E-series checks fail; ufw is not the lab firewall model | `sudo ufw disable` |
| `sudo nft flush ruleset` | **Recoverable** | Removes the LAB-FAULT chain; nftables faults (F-021 if implemented) stop working; R2 reset rebuilds the chain via bootstrap step 14 — but R2 does NOT run bootstrap; only R3 does | `sudo bash /opt/lab-env/scripts/bootstrap.sh` to recreate nftables config |
| `sudo nft add rule inet lab_filter LAB-FAULT drop` | **Recoverable** | Network drops; flush the chain to recover | `sudo nft flush chain inet lab_filter LAB-FAULT` |
| Change VM network interface | **Unsupported** | nginx listens on `0.0.0.0:80`; as long as the interface has an IP, conformance holds. Conformance does not test external reachability. | None needed for conformance |

---

### 3.7 Time and Clock Operations

| Operation | Classification | Behavior | Recovery |
|---|---|---|---|
| `sudo timedatectl set-time` | **Resilient** | Timestamps in state.json and audit.log will be skewed but no conformance check verifies time correctness | None needed |
| Time jump forward > 365 days | **Breaks TLS cert** | F-006 check fails: `openssl x509 -checkend 0` fails on expired cert; R3 reset regenerates cert | `lab reset --tier R3` |
| `sudo systemctl stop systemd-timesyncd` | **Resilient** | NTP sync stops; timestamps drift; no conformance impact | None needed |

---

### 3.8 cgroup and Resource Operations

| Operation | Classification | Behavior | Recovery |
|---|---|---|---|
| `sudo systemctl edit app.slice` (change MemoryMax) | **Recoverable** | Affects F-008/F-014 behavior (OOM enforcement); R3 reset re-installs the slice unit | `lab reset --tier R3` |
| `sudo swapoff -a` | **Resilient** | Required for OOM enforcement; no conformance impact | None needed |
| `sudo swapon -a` | **Breaks F-008/F-014** | Swap allows OOM to be avoided; `CHAOS_OOM_TRIGGER` will hang instead of killing the process | `sudo swapoff -a` |
| `sudo cgroupfs-mount` / cgroup v1 changes | **Unsupported** | The environment requires cgroup v2 (`cgroup2fs`); switching to v1 breaks MemoryMax enforcement | Reinstall or reconfigure OS |

---

### 3.9 Operations That Require Full Re-provisioning

The following operations corrupt the environment in ways that `lab reset --tier R3` cannot recover from. They require re-running `bootstrap.sh` from scratch (or restoring from a VM snapshot):

- Removing or corrupting the repository at `/opt/lab-env/` — R3 reset calls bootstrap.sh which calls `go build` from this path
- Replacing nginx with a different web server
- Upgrading Ubuntu major version (22.04 → 24.04)
- Removing the Go toolchain AND running R3 (can break binary rebuild)
- Deleting or unmounting the loopback image at `/var/lib/lab/app-state.img` while also clearing the fstab entry
- Mounting something else at `/var/lib/app` that prevents the loopback from remounting

**Recommendation:** take a VM snapshot after successful bootstrap and before any learner session. Restoring the snapshot is always available as a last resort and takes 10–30 seconds vs. 2–5 minutes for a full re-provision.

---

## §4 — Edge Case Catalogue

Specific scenarios that are not covered by the fault catalog but are known to produce unexpected behavior.

### 4.1 nginx global config modified

**Scenario:** learner edits `/etc/nginx/nginx.conf` (the global nginx config, not the lab site config at `/etc/nginx/sites-enabled/app`).

**Effect:** if the global config syntax is broken, `nginx -t` fails and `nginx -s reload` refuses to run. This means nginx itself cannot reload. The lab's F-015 fault (nginx config syntax error in the site config) relies on `nginx -t` failing — but if the global config is also broken, F-015 recovery may fail because `exec.NginxReload()` calls `nginx -t` and will return an error even after restoring the site config.

**Detection:** `sudo nginx -t 2>&1 | grep 'nginx.conf'` — error in nginx.conf rather than sites-enabled/app.

**Recovery:** `sudo nginx -t` to identify the broken file; restore `/etc/nginx/nginx.conf` manually or via `sudo apt-get install --reinstall nginx-common`.

---

### 4.2 Service started with wrong binary (stale R3 recovery)

**Scenario:** R3 reset is interrupted mid-build (e.g., SIGKILL during `go build`). The old binary at `/opt/app/server` survives (build writes to a temp path first) but service is restarted pointing at the pre-fault binary.

**Effect:** `lab status` shows CONFORMANT. `lab validate` exits 0. The fault is no longer active. No action needed — the environment is in a safe state. The stale binary was the pre-fault binary, not the faulty one.

**Detection:** not needed — the safe state is correct.

---

### 4.3 appuser home directory created accidentally

**Scenario:** learner runs `sudo mkhomedir_helper appuser` or a package creates `/home/appuser`.

**Effect:** no conformance impact — no check verifies that `appuser` has no home directory. The security model notes `appuser` should have no home directory, but this is not enforced by the conformance suite.

**Recovery:** `sudo rm -rf /home/appuser` if desired; no reset needed.

---

### 4.4 Loopback mount not remounted after reboot

**Scenario:** fstab entry is present but `mount -o loop` fails on boot (e.g., `loop` kernel module not loaded).

**Effect:** `/var/lib/app` shows as an empty directory owned by root (the pre-mount state). F-004 check fails (wrong ownership). `GET /` returns 500.

**Detection:** `mountpoint -q /var/lib/app && echo mounted || echo not mounted`

**Recovery:** `sudo mount /var/lib/app` (uses fstab entry); then `sudo chown appuser:appuser /var/lib/app && sudo chmod 755 /var/lib/app`; then `sudo systemctl restart app.service`.

---

### 4.5 Port 8080 already in use at provisioning

**Scenario:** another process is listening on `127.0.0.1:8080` or `0.0.0.0:8080` when the service starts.

**Effect:** `app.service` fails to start (bind error); enters restart loop; S-001 and E-series checks fail; `BROKEN` state.

**Detection:** `ss -ltnp | grep 8080 | grep -v server` — shows non-app process on 8080.

**Recovery:** stop the conflicting process; `sudo systemctl restart app.service`.

---

### 4.6 State.json written with wrong permissions

**Scenario:** learner or other process creates `/var/lib/lab/state.json` with mode 0600 owned by root.

**Effect:** `lab` binary runs as `devuser` and reads state.json as root via sudo. Actually — `lab` does not require sudo to read state.json (it's mode 0644 normally, readable by all). If mode is changed to 0600 owned by root, `lab status` returns an error reading state.json.

**Detection:** `stat -c '%U:%G %a' /var/lib/lab/state.json` — should be `root:root 644`.

**Recovery:** `sudo chmod 644 /var/lib/lab/state.json`.

---

### 4.7 Concurrent `lab` commands without --force

**Scenario:** learner runs `lab fault apply F-004 &` and then immediately `lab fault apply F-001` without `--force`.

**Effect:** the second command is rejected with `ErrFaultAlreadyActive` or blocked on the mutex lock. No double-fault state is possible without `--force`. The lock ensures at most one mutating command runs at a time.

**Detection:** second command exits 3 with `ErrFaultAlreadyActive`.

**Recovery:** none needed — the second command was cleanly rejected.

---

### 4.8 `apt upgrade` changes nginx behavior

**Scenario:** `apt upgrade` updates nginx from 1.18 to 1.24 (both available in Ubuntu 22.04 repositories).

**Effect:** nginx minor version upgrades are backward-compatible for the directives used in the lab config. All directives (`proxy_pass`, `proxy_set_header`, `add_header`, `proxy_read_timeout`, `ssl_certificate`) have been stable since nginx 1.9. The `proxy_read_timeout 3s` used for B-001 baseline behavior is a long-standing directive.

**Verification after upgrade:**
```bash
nginx -t                    # config still valid
lab validate                # all checks still pass
time curl http://localhost/slow  # still 504 at ~3s (B-001 still works)
```

**Recovery:** if anything breaks, `lab reset --tier R2` restores the site config; `sudo apt-get install --reinstall nginx` restores default nginx config files.

---

## §5 — Environment Integrity Verification

A full integrity check to run after any significant change (package upgrade, learner session, suspected corruption). More thorough than §1.

```bash
#!/usr/bin/env bash
# environment-integrity-check.sh
# Full integrity verification — run after significant changes.
# Expected outcome: all checks print OK; final line is PASS.

FAIL=0
check() { local label="$1"; shift; "$@" > /dev/null 2>&1 && echo "OK  $label" || { echo "FAIL $label"; FAIL=1; }; }

echo "=== System state ==="
check "app.service active"       systemctl is-active app.service --quiet
check "app.service enabled"      systemctl is-enabled app.service --quiet
check "nginx active"             systemctl is-active nginx --quiet
check "nginx enabled"            systemctl is-enabled nginx --quiet

echo "=== Process state ==="
check "appuser running server"   pgrep -u appuser server
check "app on 127.0.0.1:8080"   bash -c "ss -ltnp | grep -q '127.0.0.1:8080'"
check "nginx on :80"             bash -c "ss -ltnp | grep -q '0.0.0.0:80'"
check "nginx on :443"            bash -c "ss -ltnp | grep -q '0.0.0.0:443'"

echo "=== Endpoints ==="
check "GET /health → 200"        curl -sf http://localhost/health
check "GET / → 200"              curl -sf http://localhost/
check "GET /health body ok"      bash -c "curl -s localhost/health | jq -e '.status==\"ok\"'"
check "X-Proxy: nginx header"    bash -c "curl -sI localhost/ | grep -q 'X-Proxy: nginx'"
check "HTTPS app.local → 200"   curl -skf https://app.local/health

echo "=== Filesystem ==="
check "binary mode 750"          bash -c "stat -c '%a' /opt/app/server | grep -q 750"
check "config exists mode 640"   bash -c "stat -c '%a' /etc/app/config.yaml | grep -q 640"
check "log dir mode 755"         bash -c "stat -c '%a' /var/log/app | grep -q 755"
check "state dir mode 755"       bash -c "stat -c '%a' /var/lib/app | grep -q 755"
check "nginx config syntax"      sudo nginx -t

echo "=== Control plane ==="
check "state.json readable"      jq . /var/lib/lab/state.json
check "state is CONFORMANT"      bash -c "jq -e '.state==\"CONFORMANT\"' /var/lib/lab/state.json"
check "no active fault"          bash -c "jq -e '.active_fault==null' /var/lib/lab/state.json"
check "loopback mounted"         mountpoint -q /var/lib/app
check "app.local in /etc/hosts"  grep -q app.local /etc/hosts
check "lab validate exits 0"     lab validate

echo ""
[[ $FAIL -eq 0 ]] && echo "PASS — environment is conformant" || echo "FAIL — $FAIL check(s) failed"
exit $FAIL
```

---

## §6 — Pre-Learner-Session Checklist

Run before handing the VM to a learner. Takes ~2 minutes.

```bash
# 1. Verify CONFORMANT
lab validate || { echo "Not conformant — run lab reset --tier R2"; exit 1; }

# 2. No active fault
lab status --json | jq -e '.active_fault == null' \
  || { echo "Fault still active — run lab reset"; exit 1; }

# 3. Audit log clear (optional — create a clean start marker)
echo "--- session start $(date -u +%Y-%m-%dT%H:%M:%SZ) ---" \
  | sudo tee -a /var/lib/lab/audit.log > /dev/null

# 4. Snapshot (if VM supports it — strongly recommended)
# VBoxManage snapshot <vm> take "pre-session-$(date +%Y%m%d)"

echo "Environment ready."
```