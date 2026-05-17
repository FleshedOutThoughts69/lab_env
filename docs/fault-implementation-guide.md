# Fault Implementation Guide

## Version 1.0.0

> **Authority:** this document describes the exact mutation and reversion mechanics for every fault in the catalog, including shell-equivalent commands, transient side effects during apply and recover, timing characteristics, and monitoring signals. The formal specification is `fault-model.md §7.2`. This document is the operator-facing implementation view.
>
> **Companion documents:** `docs/fault-matrix-runbook.md` (diagnostic reference), `fault-model.md §7.2` (formal spec with full postcondition definitions).

---

## How to Read This Document

Each fault entry has five sections:

**Mutation vector** — what the control plane does to the system when `lab fault apply <ID>` succeeds. Expressed as both executor calls (what the Go code does) and shell-equivalent commands (what you could reproduce manually).

**Reversion vector** — what the control plane does when `lab reset --tier <R>` runs. Same dual representation.

**Apply side effects** — transient system behavior during the apply window, before the state settles. These are not conformance failures; they are the transition from CONFORMANT to DEGRADED.

**Recover side effects** — transient system behavior during the recovery window, before the system returns to CONFORMANT.

**Monitoring signals** — what to watch in real time while the fault is active or during apply/recover.

---

## Service Restart Timing Reference

Many faults trigger a `systemctl restart app.service`. The following timing applies across all such faults:

| Phase | Duration | Notes |
|---|---|---|
| Stop (normal) | ≤ 10s | `TimeoutStopSec=10`; service has 5s shutdown grace period |
| Stop (F-008 active) | ~90s | SIGTERM ignored; systemd waits full `TimeoutStopSec` then sends SIGKILL |
| Start | ≤ 1s | Service opens log, binds port, writes `/run/app/healthy` |
| Restart loop (crash faults) | 2s between attempts | `RestartSec=2`; max 5 attempts in 30s before `start-limit-hit` |

For faults that cause immediate startup failure (F-001, F-003, F-005, F-006, F-009, F-013, F-017): the service will attempt to restart up to 5 times in 30 seconds before systemd marks it `start-limit-hit` and stops retrying. At that point `systemctl is-active app.service` returns `failed` and S-001 fails. This is expected and correct — the fault is active and the conformance check is doing its job.

---

## F-001 — Missing configuration file

### Mutation vector

```
Executor: exec.Remove("/etc/app/config.yaml")

Shell equivalent:
  sudo rm /etc/app/config.yaml
```

Apply is a single atomic file removal. No service restart is triggered by Apply itself — the service crashes on its next startup attempt because it cannot find the config file.

### Reversion vector

```
Executor: exec.RestoreFile("/etc/app/config.yaml")
  → writes embedded canonical bytes, mode 0640, owner appuser:appuser

Shell equivalent:
  sudo cp /opt/lab-env/internal/config/config.yaml /etc/app/config.yaml
  sudo chown appuser:appuser /etc/app/config.yaml
  sudo chmod 0640 /etc/app/config.yaml
  sudo systemctl restart app.service
```

### Apply side effects

The service crashes immediately after Apply because the config file is gone. Systemd restarts it every 2 seconds. After 5 failed starts in 30 seconds it enters `start-limit-hit` and stops retrying. During this window you will see rapid restart entries in journald.

### Recover side effects

R2 reset restores the config file first, then restarts the service. The service starts cleanly on the first attempt. No restart loop.

### Monitoring signals

```bash
journalctl -u app.service -f        # watch restart loop
lab status                           # DEGRADED during fault
lab status                           # CONFORMANT after reset
```

---

## F-002 — Wrong listen port in config

### Mutation vector

```
Executor:
  1. exec.ReadFile("/etc/app/config.yaml")
     → replace "127.0.0.1:8080" with "127.0.0.1:9090"
  2. exec.WriteFile("/etc/app/config.yaml", modified, 0640, "appuser", "appuser")
  3. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo sed -i 's/127.0.0.1:8080/127.0.0.1:9090/' /etc/app/config.yaml
  sudo systemctl restart app.service
```

### Reversion vector

```
Executor:
  1. exec.RestoreFile("/etc/app/config.yaml")
  2. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo cp /opt/lab-env/internal/config/config.yaml /etc/app/config.yaml
  sudo chown appuser:appuser /etc/app/config.yaml
  sudo chmod 0640 /etc/app/config.yaml
  sudo systemctl restart app.service
```

### Apply side effects

Service is down for ~1 second during restart. After restart it binds to `127.0.0.1:9090` instead of `8080`. nginx upstream still points to `8080` so all proxied requests fail with 502. Direct access to `9090` works.

### Recover side effects

Service is down for ~1 second during restart. After restart it rebinds to `8080`.

### Monitoring signals

```bash
ss -ltnp | grep '8080\|9090'        # watch port migration
curl -s 127.0.0.1:9090/health       # 200 while fault active
curl -s localhost/health             # 502 while fault active (nginx → 8080)
```

---

## F-003 — Config file unreadable

### Mutation vector

```
Executor: exec.Chmod("/etc/app/config.yaml", 0000)

Shell equivalent:
  sudo chmod 0000 /etc/app/config.yaml
```

### Reversion vector

```
Executor: exec.Chmod("/etc/app/config.yaml", 0640)

Shell equivalent:
  sudo chmod 0640 /etc/app/config.yaml
  sudo systemctl restart app.service
```

### Apply side effects

Same restart loop as F-001. The service cannot read the config (permission denied rather than file not found). The distinction is visible in the journald log message and in `stat /etc/app/config.yaml` — the file exists but mode is `0000`.

### Recover side effects

Mode restored; service restarts cleanly.

### Monitoring signals

```bash
stat /etc/app/config.yaml            # mode 0000 while fault active
journalctl -u app.service -n 5       # "permission denied" vs "no such file"
```

---

## F-004 — State directory unwritable

### Mutation vector

```
Executor: exec.Chmod("/var/lib/app", 0000)

Shell equivalent:
  sudo chmod 0000 /var/lib/app
```

### Reversion vector

```
Executor: exec.Chmod("/var/lib/app", 0755)

Shell equivalent:
  sudo chmod 0755 /var/lib/app
```

### Apply side effects

No service restart. The service continues running with `/var/lib/app` inaccessible. Every `GET /` request fails with 500 because the service tries to touch `/var/lib/app/state` and gets permission denied. `GET /health` continues returning 200 because it does not touch the state directory.

### Recover side effects

Mode restored immediately. No restart needed. The next `GET /` request succeeds.

### Monitoring signals

```bash
curl localhost/health                 # 200 — service alive
curl localhost/                       # 500 — state write fails
tail -5 /var/log/app/app.log         # "msg":"state write failed"
ls -la /var/lib/app                  # drwx------ while fault active
```

---

## F-005 — Binary not executable

### Mutation vector

```
Executor: exec.Chmod("/opt/app/server", 0640)

Shell equivalent:
  sudo chmod 0640 /opt/app/server
```

### Reversion vector

```
Executor: exec.Chmod("/opt/app/server", 0750)

Shell equivalent:
  sudo chmod 0750 /opt/app/server
  sudo systemctl restart app.service
```

### Apply side effects

Apply removes execute permission from the binary while the service is running. The currently running process is unaffected — Linux keeps the binary mapped in memory. The fault manifests when systemd next tries to start the process (after any stop/restart). The service enters the restart loop immediately after Apply because Apply does not restart the service — the running process stays up but any restart will fail with "exec permission denied."

**Important:** the service remains up and serving requests immediately after Apply. The conformance checks S-001 and E-series do not fail until the service is restarted (which may happen naturally or via `lab fault apply` triggering a restart for a different reason). The fault is fully active — if the service crashes or is restarted for any reason, it cannot come back up.

### Recover side effects

Mode restored; service restarts cleanly on next attempt.

### Monitoring signals

```bash
ls -la /opt/app/server               # -rw-r----- while fault active
systemctl restart app.service        # to trigger the failure manually
journalctl -u app.service -n 5       # "exec format error" or "permission denied"
```

---

## F-006 — APP_ENV removed from unit file

### Mutation vector

```
Executor:
  1. exec.ReadFile("/etc/systemd/system/app.service")
     → remove line containing "Environment=APP_ENV=prod"
  2. exec.WriteFile("/etc/systemd/system/app.service", modified, 0644, "root", "root")
  3. exec.Systemctl("daemon-reload", "")
  4. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo sed -i '/Environment=APP_ENV=prod/d' /etc/systemd/system/app.service
  sudo systemctl daemon-reload
  sudo systemctl restart app.service
```

### Reversion vector

```
Executor:
  1. exec.RestoreFile("/etc/systemd/system/app.service")
  2. exec.Systemctl("daemon-reload", "")
  3. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo cp /opt/lab-env/internal/config/app.service \
    /etc/systemd/system/app.service
  sudo systemctl daemon-reload
  sudo systemctl restart app.service
```

### Apply side effects

`daemon-reload` briefly interrupts systemd's unit file watch (< 100ms). Service is down for ~1 second during restart. After restart `APP_ENV` is missing from the process environment — the service starts without it and its behavior depends on `sanitizeEnvString("")` returning empty string. The `/` endpoint returns `"env":""` in the response body.

**Distinguishing F-006 from F-017:** both produce an empty or missing `APP_ENV`. The distinction is in the mechanism: F-006 removes the `Environment=` line from the unit file (file is modified on disk); F-017 uses `systemctl set-environment` to override at the manager level (unit file is unchanged).

```bash
# F-006: line is gone from the file
grep APP_ENV /etc/systemd/system/app.service   # no output

# F-017: line is still in the file, overridden at manager level
grep APP_ENV /etc/systemd/system/app.service   # Environment=APP_ENV=prod
systemctl show app --property=Environment      # APP_ENV= (empty, override active)
```

### Recover side effects

Unit file restored; `daemon-reload` runs; service restarts cleanly with `APP_ENV=prod`.

### Monitoring signals

```bash
systemctl show app --property=Environment      # no APP_ENV= entry
grep APP_ENV /etc/systemd/system/app.service   # no output while fault active
curl -s localhost/ | jq .env                   # "" while fault active
```

---

## F-007 — nginx pointing to wrong upstream port

### Mutation vector

```
Executor:
  1. exec.ReadFile("/etc/nginx/sites-enabled/app")
     → replace "server 127.0.0.1:8080;" with "server 127.0.0.1:9090;"
       inside the upstream app_backend block
  2. exec.WriteFile("/etc/nginx/sites-enabled/app", modified, 0644, "root", "root")
  3. exec.NginxReload()
     → runs: nginx -t && nginx -s reload

Shell equivalent:
  sudo sed -i 's/server 127.0.0.1:8080;/server 127.0.0.1:9090;/' \
    /etc/nginx/sites-enabled/app
  sudo nginx -t && sudo nginx -s reload
```

### Reversion vector

```
Executor:
  1. exec.RestoreFile("/etc/nginx/sites-enabled/app")
  2. exec.NginxReload()

Shell equivalent:
  sudo cp /opt/lab-env/internal/config/nginx.conf \
    /etc/nginx/sites-enabled/app
  sudo nginx -t && sudo nginx -s reload
```

### Apply side effects

nginx reload is graceful — in-flight requests complete before workers restart with new config. The reload window is < 500ms. After reload, all proxy requests fail with 502 because nginx forwards to `9090` but the app is on `8080`. Direct access to `127.0.0.1:8080` continues to work. The upstream block is the single change point; all server blocks share it, so all virtual hosts are affected simultaneously.

### Recover side effects

nginx reload restores upstream. Same graceful reload window.

### Monitoring signals

```bash
cat /etc/nginx/sites-enabled/app | grep server  # upstream server 127.0.0.1:9090
curl 127.0.0.1:8080/health                       # 200 — app fine
curl localhost/health                            # 502 — proxy broken
```

---

## F-008 — SIGTERM ignored (non-reversible)

### Mutation vector

```
Executor:
  1. exec.RunMutation("go", "build",
       "-ldflags", "-X main.FaultIgnoreSIGTERM=true",
       "-o", "/opt/app/server",
       "./service")
     [working directory: /opt/lab-env/service]
  2. exec.Chown("/opt/app/server", "appuser", "appuser")
  3. exec.Chmod("/opt/app/server", 0750)
  4. exec.Systemctl("restart", "app.service")

Shell equivalent:
  cd /opt/lab-env/service
  sudo CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-X main.FaultIgnoreSIGTERM=true" \
    -o /opt/app/server .
  sudo chown appuser:appuser /opt/app/server
  sudo chmod 0750 /opt/app/server
  sudo systemctl restart app.service
```

**Go toolchain required.** Apply invokes `go build` and will fail if the Go toolchain is not installed or the source is not present at `/opt/lab-env/service`. Build time is typically 15–30 seconds on the lab VM.

**`--yes` flag required.** This fault requires operator confirmation: `lab fault apply F-008 --yes`.

### Reversion vector

```
Recover returns error immediately:
  "R3 reset required: run lab reset --tier R3 to rebuild the binary without the fault flag"
  No mutation is attempted.

To recover:
  lab reset --tier R3
  → re-runs bootstrap.sh
  → rebuilds binary WITHOUT the fault flag
  → restores all canonical files
  → restarts services
  → runs lab validate
```

### Apply side effects

**Build phase (15–30 seconds):** the service continues running with the old binary during the build. The system remains in CONFORMANT state. No conformance checks fail.

**Restart phase (~1 second):** service stops and restarts with the new binary.

**Post-apply:** `lab validate` exits 0. All 23 checks pass. The fault is entirely silent during normal operation — it only manifests at shutdown.

**Observable proof of fault:**

```bash
time sudo systemctl stop app    # ~90 seconds (SIGKILL at TimeoutStopSec)
sudo systemctl start app        # restart after observation
```

The `TimeoutStopSec=10` in `app.service` means systemd sends SIGTERM, waits 10 seconds, then sends SIGKILL. With F-008 active, SIGTERM is masked — the process ignores it and systemd escalates to SIGKILL after the timeout. The ~90 second figure from the runbook assumes an earlier version; the current `TimeoutStopSec=10` means the timeout is ~10 seconds, not 90. The runbook figure should be read as "significantly longer than normal stop time."

### Recover side effects

R3 reset runs bootstrap.sh in full. This involves:
- Restoring all R2-embedded config files
- Rebuilding the binary (~15-30 seconds)
- Restarting services
- Running the conformance suite

Total recovery time: 1–3 minutes depending on build speed.

### Monitoring signals

```bash
lab validate                          # exit 0 — fault is silent
time sudo systemctl stop app          # > 10s — SIGKILL escalation
journalctl -u app.service -n 5        # "Killed" entry from SIGKILL
```

---

## F-009 — Log file unwritable

### Mutation vector

```
Executor: exec.Chmod("/var/log/app/app.log", 0000)

Shell equivalent:
  sudo chmod 0000 /var/log/app/app.log
```

### Reversion vector

```
Executor: exec.Chmod("/var/log/app/app.log", 0640)

Shell equivalent:
  sudo chmod 0640 /var/log/app/app.log
  sudo systemctl restart app.service
```

### Apply side effects

The currently running service has the log file open and writes will immediately fail with permission denied. The service's logging package handles write errors by falling back to stderr (journald). The service crashes because it cannot write to the log file — it enters the restart loop. On each restart attempt it tries to open the log file, fails again, and crashes again.

### Recover side effects

Mode restored; service restarts cleanly and reopens the log file successfully.

### Monitoring signals

```bash
stat /var/log/app/app.log             # mode 0000
journalctl -u app.service -f          # restart loop entries
```

---

## F-010 — Log file deleted while running

### Mutation vector

```
Executor: exec.Remove("/var/log/app/app.log")
  [P-001 PreconditionCheck enforced: app must be running]

Shell equivalent:
  sudo rm /var/log/app/app.log
  # Must be executed while app.service is active
```

**PreconditionCheck:** `lab fault apply F-010` enforces that P-001 (app process running) passes before executing Apply. If the service is stopped, the apply is rejected. This is a `--force`-bypassable precondition.

### Reversion vector

```
Executor: exec.Systemctl("restart", "app.service")
  → service opens /var/log/app/app.log on startup, recreating the inode

Shell equivalent:
  sudo systemctl restart app.service
```

### Apply side effects

No service crash. The running process holds an open file descriptor to the deleted inode. The file is unlinked from the directory but still exists on disk. The service continues writing to the deleted inode — those bytes are visible via `lsof` but not via `ls`. The log directory appears empty.

`lab validate` exits 0 (degraded-conformant) — L-series checks fail (no `app.log` on disk) but they are degraded severity. All blocking checks pass.

### Recover side effects

Service is down for ~1 second during restart. On startup, the service's logging package opens `/var/log/app/app.log` with `O_CREATE|O_APPEND` — a new inode is created. Any writes to the deleted inode during the fault are lost.

### Monitoring signals

```bash
ls /var/log/app/                      # empty — file unlinked
lsof +L1 | grep app.log              # deleted fd still held by process
lsof -p $(pgrep server) | grep log   # inode with link count 0
lab validate                          # exit 0 (degraded-conformant)
df -h /var/log/app                   # space still consumed by deleted inode
```

---

## F-013 — Unit file syntax error

### Mutation vector

```
Executor:
  1. exec.ReadFile("/etc/systemd/system/app.service")
     → replace "ExecStart=/opt/app/server" with "ExecStart=/opt/app/DOESNOTEXIST"
  2. exec.WriteFile("/etc/systemd/system/app.service", modified, 0644, "root", "root")
  3. exec.Systemctl("daemon-reload", "")

Shell equivalent:
  sudo sed -i 's|ExecStart=/opt/app/server|ExecStart=/opt/app/DOESNOTEXIST|' \
    /etc/systemd/system/app.service
  sudo systemctl daemon-reload
```

**No service restart in Apply.** The currently running service continues running with the old binary. The fault manifests on the next stop/start cycle. The unit file references a nonexistent binary — systemd will report `failed` when it tries to execute it.

### Reversion vector

```
Executor:
  1. exec.RestoreFile("/etc/systemd/system/app.service")
  2. exec.Systemctl("daemon-reload", "")
  3. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo cp /opt/lab-env/internal/config/app.service \
    /etc/systemd/system/app.service
  sudo systemctl daemon-reload
  sudo systemctl restart app.service
```

### Apply side effects

Service continues running after Apply because Apply does not restart it. The fault becomes observable only after the service stops (naturally or manually). The diagnostic property of this fault is the `enabled/failed` asymmetry: `systemctl is-enabled app` returns `enabled` while `systemctl is-active app` returns `failed`.

To demonstrate the fault immediately after Apply:

```bash
sudo systemctl restart app.service   # triggers the failure
systemctl is-enabled app             # enabled
systemctl is-active app              # failed
```

### Recover side effects

Unit file restored; `daemon-reload` reloads correct binary path; service restarts cleanly.

### Monitoring signals

```bash
systemctl is-enabled app              # enabled (S-002 passes)
systemctl is-active app               # failed (S-001 fails)
systemctl status app                  # ExecStart=/opt/app/DOESNOTEXIST
```

---

## F-014 — Zombie process accumulation (non-reversible)

### Mutation vector

```
Executor:
  1. exec.RunMutation("go", "build",
       "-ldflags", "-X main.FaultZombieChildren=true",
       "-o", "/opt/app/server",
       "./service")
     [working directory: /opt/lab-env/service]
  2. exec.Chown("/opt/app/server", "appuser", "appuser")
  3. exec.Chmod("/opt/app/server", 0750)
  4. exec.Systemctl("restart", "app.service")

Shell equivalent:
  cd /opt/lab-env/service
  sudo CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-X main.FaultZombieChildren=true" \
    -o /opt/app/server .
  sudo chown appuser:appuser /opt/app/server
  sudo chmod 0750 /opt/app/server
  sudo systemctl restart app.service
```

Same toolchain requirements as F-008. **`--yes` flag required.**

### Reversion vector

Same as F-008 — R3 reset only.

```
lab reset --tier R3
```

### Apply side effects

Same build/restart window as F-008 (15–30 second build, ~1 second restart). Post-apply, `lab validate` exits 0. The fault is silent immediately after apply.

Zombies accumulate gradually: each `GET /` request spawns a child process that the parent intentionally does not `wait()` on. The child exits but remains in the process table as a zombie until the parent is killed. Zombies consume PID table slots but no CPU or memory.

**Accumulation rate:** approximately one zombie per `GET /` request. The 50-entry PID table constraint is not exhausted in normal operation — the table is not 50 entries wide; it is limited by the system PID limit (typically 32768). The fault's teaching value is observable at any zombie count.

### Recover side effects

Same as F-008 — R3 full reprovision, 1–3 minutes.

### Monitoring signals

```bash
lab validate                          # exit 0
for i in $(seq 1 20); do curl -s localhost/ > /dev/null; done
ps aux | grep -c ' Z '               # count zombie processes
ps aux | grep ' Z '                  # list zombie processes
```

---

## F-015 — nginx configuration syntax error

### Mutation vector

```
Executor:
  1. exec.ReadFile("/etc/nginx/sites-enabled/app")
     → append "\ninvalid_directive on;" to end of file
  2. exec.WriteFile("/etc/nginx/sites-enabled/app", modified, 0644, "root", "root")
  3. exec.NginxReload()   — this fails (expected)

Shell equivalent:
  echo "invalid_directive on;" | sudo tee -a /etc/nginx/sites-enabled/app
  sudo nginx -t            # fails — this is the observable proof
```

**The NginxReload failure is expected and not an Apply error.** The executor's `NginxReload` runs `nginx -t` before attempting `nginx -s reload`. The syntax check fails, so no reload occurs. nginx continues running with the last known-good configuration. The fault is in the config file on disk; the running nginx instance is unaffected.

### Reversion vector

```
Executor:
  1. exec.RestoreFile("/etc/nginx/sites-enabled/app")
  2. exec.NginxReload()    — this succeeds after restore

Shell equivalent:
  sudo cp /opt/lab-env/internal/config/nginx.conf \
    /etc/nginx/sites-enabled/app
  sudo nginx -t && sudo nginx -s reload
```

### Apply side effects

nginx continues serving requests from its in-memory configuration. The config file on disk is invalid, but nginx has not reloaded it. All E-series endpoint checks pass. Only F-005 (nginx config syntax check: `nginx -t`) fails.

### Recover side effects

Config file restored; nginx reloads successfully.

### Monitoring signals

```bash
sudo nginx -t                         # syntax error message
curl localhost/health                 # 200 — nginx still serving
tail -3 /etc/nginx/sites-enabled/app  # invalid_directive on;
```

---

## F-016 — App binding on all interfaces

### Mutation vector

```
Executor:
  1. exec.ReadFile("/etc/app/config.yaml")
     → replace "127.0.0.1:8080" with "0.0.0.0:8080"
  2. exec.WriteFile("/etc/app/config.yaml", modified, 0640, "appuser", "appuser")
  3. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo sed -i 's/127.0.0.1:8080/0.0.0.0:8080/' /etc/app/config.yaml
  sudo systemctl restart app.service
```

### Reversion vector

```
Executor:
  1. exec.RestoreFile("/etc/app/config.yaml")
  2. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo cp /opt/lab-env/internal/config/config.yaml /etc/app/config.yaml
  sudo chown appuser:appuser /etc/app/config.yaml
  sudo chmod 0640 /etc/app/config.yaml
  sudo systemctl restart app.service
```

### Apply side effects

Service is down for ~1 second during restart. After restart it binds to `0.0.0.0:8080` instead of `127.0.0.1:8080`. nginx upstream still points to `127.0.0.1:8080` which still resolves correctly — so E-series checks continue to pass via nginx. Only P-002 fails because it checks for `127.0.0.1:8080` specifically:

```bash
ss -ltnp | grep -q '127.0.0.1:8080'   # fails — socket is 0.0.0.0:8080
ss -ltnp | grep '8080'                 # shows 0.0.0.0:8080
```

Direct access without nginx (`curl 127.0.0.1:8080/health`) still works because `0.0.0.0` includes `127.0.0.1`.

### Recover side effects

Service restarts binding to `127.0.0.1:8080`.

### Monitoring signals

```bash
ss -ltnp | grep 8080                  # 0.0.0.0:8080 while fault active
curl 127.0.0.1:8080/health            # 200 — direct access still works
curl localhost/health                  # 200 — nginx proxy still works
```

---

## F-017 — Empty APP_ENV environment variable

### Mutation vector

```
Executor:
  1. exec.RunMutation("systemctl", "set-environment", "APP_ENV=")
     → sets APP_ENV="" at the systemd manager level, overriding the unit file
  2. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo systemctl set-environment APP_ENV=
  sudo systemctl restart app.service
```

**Manager-level override:** `systemctl set-environment` writes to the systemd manager's in-memory environment table, not to the unit file. The unit file still contains `Environment=APP_ENV=prod`. The override survives daemon-reload but is cleared on systemd restart.

### Reversion vector

```
Executor:
  1. exec.RunMutation("systemctl", "unset-environment", "APP_ENV")
     → removes the manager-level override; unit file value takes effect
  2. exec.Systemctl("restart", "app.service")

Shell equivalent:
  sudo systemctl unset-environment APP_ENV
  sudo systemctl restart app.service
```

**`unset-environment` vs `set-environment APP_ENV=prod`:** unset removes the override entirely, restoring the unit file value. Setting it back to `prod` would also work but leaves a redundant manager-level entry.

### Apply side effects

Service is down for ~1 second during restart. After restart `APP_ENV` is empty. The service's sanitization logic strips it to empty string. The `/` endpoint returns `"env":""`.

### Recover side effects

Manager override cleared; service restarts with `APP_ENV=prod` from the unit file.

### Monitoring signals

```bash
systemctl show app --property=Environment  # APP_ENV= (empty override)
grep APP_ENV /etc/systemd/system/app.service  # Environment=APP_ENV=prod (unchanged)
curl -s localhost/ | jq .env              # "" while fault active
```

---

## F-018 — Inode exhaustion

### Mutation vector

```
Executor: exec.RunMutation("bash", "-c",
  "for i in $(seq 1 100000); do touch /var/lib/app/file_$i; done")

Shell equivalent:
  sudo bash -c 'for i in $(seq 1 100000); do touch /var/lib/app/file_$i; done'
```

**Duration:** creating 100,000 files takes approximately 10–30 seconds depending on the VM's I/O speed. The service continues running during Apply. No service restart.

### Reversion vector

```
Executor: exec.RunMutation("bash", "-c", "rm -f /var/lib/app/file_*")
  → removes all fault-created files; idempotent

Shell equivalent:
  sudo rm -f /var/lib/app/file_*
```

**Duration:** deletion takes approximately 5–15 seconds for 100,000 files.

### Apply side effects

During the file creation loop (10–30 seconds), inode usage climbs toward 100%. Once the inode table is full, `GET /` requests start failing with 500 — the service cannot create `/var/lib/app/state`. `GET /health` continues returning 200 throughout because it does not touch the filesystem.

**Key diagnostic property:** `df -h /var/lib/app` shows available block space; `df -i /var/lib/app` shows 100% inode usage. This is the canonical demonstration that "disk full" errors have two independent causes.

### Recover side effects

During file deletion (5–15 seconds), inode usage drops. Once the table has free entries again, `GET /` requests succeed without any service restart. Recovery is visible in real time.

### Monitoring signals

```bash
watch -n1 'df -i /var/lib/app'          # watch inode usage during apply
df -i /var/lib/app                       # 100% after apply
df -h /var/lib/app                       # blocks still available
touch /var/lib/app/test                  # "No space left on device"
curl localhost/health                    # 200 — service alive
curl localhost/                          # 500 — state write fails
ls /var/lib/app/file_* | wc -l          # 100000 files while fault active
```

---

## B-001 and B-002 — Baseline Network Behaviours

These are not faults. No apply or recover operation exists. They are observable properties of the canonical conformant environment.

**B-001 — nginx proxy timeout shorter than `/slow` response:**

```bash
time curl -v http://localhost/slow     # 504 after ~3s (proxy_read_timeout 3s)
time curl 127.0.0.1:8080/slow         # 200 after ~5s (app delay)
```

The nginx `proxy_read_timeout 3s` is shorter than the `/slow` handler's 5-second delay. nginx times out and returns 504 before the app responds. Direct access bypasses nginx and gets the 200.

**B-002 — Self-signed TLS certificate:**

```bash
curl -v https://app.local/health       # SSL certificate error (not trusted)
curl -sk https://app.local/health      # 200 (skip verify)
openssl s_client -connect app.local:443 2>/dev/null | grep "subject\|issuer"
# subject and issuer are identical — self-signed
```

The certificate is valid (E-005 passes with `-k`; F-006 passes). The trust store does not contain it.