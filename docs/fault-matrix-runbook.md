# Fault Matrix Runbook
## Version 1.0.0

**Pre-flight:** `sudo lab validate` must exit 0 and `sudo lab status` must show `State: CONFORMANT` before applying any fault. If not: `sudo lab reset --tier R2`.

**Note on sudo:** Most commands require `sudo`. The runbook uses `sudo lab` throughout. If you have passwordless sudo configured (as bootstrap.sh does for the `appuser`), you can omit `sudo`; otherwise prefix all `lab` commands with `sudo`.

**Timing note:** Some faults (F‑002, F‑003, F‑006, F‑009, F‑013, F‑016, F‑017) cause a service restart. The conformance suite may pass if run immediately after apply because the service hasn't finished restarting. For reliable validation, add `sleep 2` between apply and validate. The fault matrix script (`scripts/run-fault-matrix.sh`) already does this.

---

## Matrix

| Fault | Apply command | Mutation | Blocking checks that FAIL | Notable checks that PASS | `lab status` | Reset tier | Post-reset |
|---|---|---|---|---|---|---|---|
| **F-001** | `sudo lab fault apply F-001` | rm `/etc/app/config.yaml` | S-001, E-001, E-002, E-003, E-004, E-005 | F-003, F-007 | DEGRADED | R2 | All 23 pass |
| **F-002** | `sudo lab fault apply F-002` | config.yaml `8080→9090`; restart app | P-002, E-001, E-002, E-003, E-004, E-005 | S-001, P-001, F-002 | DEGRADED | R2 | All 23 pass |
| **F-003** | `sudo lab fault apply F-003` | chmod 000 `/etc/app/config.yaml` | S-001, E-001, E-002, E-003, E-004, E-005 | F-002, F-007 | DEGRADED | R2 | All 23 pass |
| **F-004** | `sudo lab fault apply F-004` | chmod 000 `/var/lib/app/` | E-002, F-004 | **S-001, E-001, E-003** | DEGRADED | R2 | All 23 pass |
| **F-005** | `sudo lab fault apply F-005` | chmod 640 `/opt/app/server` | S-001, E-001, E-002, E-003, E-004, E-005, F-001 | F-002 | DEGRADED | R2 | All 23 pass |
| **F-006** | `sudo lab fault apply F-006` | rm `Environment=APP_ENV=prod` from unit; daemon-reload; restart | S-001, E-001, E-002, E-003, E-004, E-005 | F-002 | DEGRADED | R2 | All 23 pass |
| **F-007** | `sudo lab fault apply F-007` | nginx config `8080→9090`; nginx reload | E-001, E-002, E-003, E-004, E-005 | **S-001, P-001, P-002** | DEGRADED | R2 | All 23 pass |
| **F-008** | `sudo lab --yes fault apply F-008` | Apply returns error (binary rebuild required — implement in deployment pipeline). State remains CONFORMANT. | **None (Apply failed)** | — | CONFORMANT | **R3** | All 23 pass |
| **F-009** | `sudo lab fault apply F-009` | chmod 000 `/var/log/app/app.log` | S-001, E-001, E-002, E-003, E-004, E-005 | F-002 | DEGRADED | R2 | All 23 pass |
| **F-010** | `sudo lab fault apply F-010` | rm `/var/log/app/app.log` (while running) | _(degraded only)_ L-001, L-002, L-003 | **S-001, P-001, P-002, E-001, E-002** | DEGRADED | R1 | All 23 pass |
| **B-001** | _not a fault — observe only_ | baseline network behaviour | None | — | — | — | — |
| **B-002** | _not a fault — observe only_ | baseline network behaviour | None | — | — | — | — |
| **F-013** | `sudo lab fault apply F-013` | `ExecStart=DOESNOTEXIST` in unit; daemon-reload | S-001, E-001, E-002, E-003, E-004, E-005 | **S-002** | DEGRADED | R2 | All 23 pass |
| **F-014** | `sudo lab --yes fault apply F-014` | Apply returns error (binary rebuild required). State remains CONFORMANT. | **None (Apply failed)** | — | CONFORMANT | **R3** | All 23 pass |
| **F-015** | `sudo lab fault apply F-015` | append `invalid_directive on;` to nginx config | **F-005 only** | S-003, P-003, P-004, E-001, E-002 | DEGRADED | R2 | All 23 pass |
| **F-016** | `sudo lab fault apply F-016` | config.yaml `127.0.0.1→0.0.0.0`; restart | **P-002 only** | S-001, E-001, E-002, E-003, E-004 | DEGRADED | R2 | All 23 pass |
| **F-017** | `sudo lab fault apply F-017` | `systemctl set-environment APP_ENV=`; restart | S-001, E-001, E-002, E-003, E-004, E-005 | F-002, F-001 | DEGRADED | R2 | All 23 pass |
| **F-018** | `sudo lab fault apply F-018` | create 100,000 files in `/var/lib/app/file_N` | E-002, F-004 | **S-001, E-001, E-003** | DEGRADED | R2 | All 23 pass |

> **F-008, F-014 note:** The Apply function returns an error because these faults require a binary rebuild with build flags. This is expected. R3 reset is the recovery path for both.
> **F-010 note:** `lab validate` exits **0** for F-010 — L-series failures are degraded severity. All blocking checks pass.

---

## Verification Commands Per Fault

**F-001**
```bash
journalctl -u app.service -n 5     # config-not-found in restart loop
curl localhost/health               # connection refused
```

**F-002**
```bash
ss -ltnp | grep 9090               # app on wrong port
curl 127.0.0.1:9090/health         # 200 direct
curl localhost/health               # 502
```

**F-003**
```bash
stat /etc/app/config.yaml          # mode 0000
journalctl -u app.service -n 5     # permission denied
```

**F-004**
```bash
curl localhost/health               # 200
curl localhost/                     # 500
tail -5 /var/log/app/app.log       # "msg":"state write failed"
```

**F-005**
```bash
ls -la /opt/app/server             # 640 mode
journalctl -u app.service -n 5     # exec permission denied
```

**F-006**
```bash
systemctl show app --property=Environment   # no APP_ENV entry
journalctl -u app.service -n 5             # missing APP_ENV
```

**F-007**
```bash
ss -ltnp | grep 8080               # app still on 8080
curl 127.0.0.1:8080/health         # 200
curl localhost/health               # 502
```

**F-008**
```bash
sudo lab --yes fault apply F-008    # Apply returns error (expected)
sudo lab validate                    # exit 0 (no mutation occurred)
# F-008 requires binary rebuild with FAULT_IGNORE_SIGTERM=true
# To fully test, rebuild the binary with the flag and redeploy.
# R3 reset recovers without the flag.
sudo lab reset --tier R3
```

**F-009**
```bash
stat /var/log/app/app.log          # mode 0000
journalctl -u app.service -n 5     # log permission denied at startup
```

**F-010**
```bash
sudo lab validate                   # exit 0 (degraded only)
ls /var/log/app/                   # no app.log
lsof +L1 | grep app.log            # deleted fd held by app
curl localhost/health               # 200
```

**B-001 (baseline network behaviour)**
```bash
time curl -v http://localhost/slow  # 504 ~3s
time curl 127.0.0.1:8080/slow      # 200 ~5s
```

**B-002 (baseline network behaviour)**
```bash
curl -v https://app.local/health    # SSL certificate error
curl -sk https://app.local/health   # 200
```

**F-013**
```bash
systemctl is-enabled app            # enabled
systemctl is-active app             # failed
```

**F-014**
```bash
sudo lab --yes fault apply F-014    # Apply returns error (expected)
# F-014 requires binary rebuild with FAULT_ZOMBIE_CHILDREN=true
# R3 reset recovers without the flag.
sudo lab reset --tier R3
```

**F-015**
```bash
sudo nginx -t                       # syntax error
curl localhost/health               # 200 (old config active)
```

**F-016**
```bash
ss -ltnp | grep 8080               # 0.0.0.0:8080
curl 127.0.0.1:8080/health         # 200 (no X-Proxy header)
```

**F-017**
```bash
systemctl show app --property=Environment   # APP_ENV= (empty)
cat /etc/systemd/system/app.service         # APP_ENV=prod still in file
```

**F-018**
```bash
df -i /var/lib/app                  # ~100% inodes
df -h /var/lib/app                  # blocks available
touch /var/lib/app/test             # No space left on device
curl localhost/health               # 200
curl localhost/                     # 500
```

---

## Identify Active Fault by Failing Check Pattern

| Failing checks | Candidates | Distinguish with |
|---|---|---|
| S-001 + E-series; F-002 **fails** | F-001 | `ls /etc/app/config.yaml` |
| S-001 + E-series; F-002 passes, mode 000 | F-003 | `stat /etc/app/config.yaml` |
| S-001 + E-series; F-002 passes, mode OK | F-006, F-009, F-017 | `journalctl -u app.service -n 5` |
| S-001 + E-series; F-001 (mode) fails | F-005 | `ls -la /opt/app/server` |
| P-002 + E-series; S-001 passes | F-002 | `ss -ltnp \| grep 9090` |
| E-series only; P-002 passes | F-007 | `ss -ltnp \| grep 8080` confirms app on 8080 |
| E-002 only; E-001 passes | F-004, F-018 | `df -i /var/lib/app` — inodes? → F-018; else F-004 |
| F-005 only | F-015 | `sudo nginx -t` |
| P-002 only | F-016 | `ss -ltnp \| grep 8080` shows `0.0.0.0` |
| L-series only; validate exits 0 | F-010 | `lsof +L1 \| grep app.log` |
| S-001 fails; S-002 passes | F-013 | `systemctl is-active app` → failed |
| None; validate exits 0 | F-008, F-014 | Apply returned error; no mutation occurred |

---

## Post-Matrix

```bash
sudo lab validate
sudo lab status
cat /var/lib/lab/state.json | jq '{state, active_fault, classification_valid}'
```