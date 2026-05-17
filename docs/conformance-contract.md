# Service Conformance Contract
## lab-env subject application — check-to-signal mapping
## Version 1.0.0

> **Purpose:** Maps every conformance suite check to the exact signal the service
> produces. Use this to verify the service implementation satisfies each check,
> and to understand which service behavior a failing check is actually detecting.
>
> **Authority:** when this document conflicts with the conformance check catalog
> (`conformance-model.md §3`), the check catalog is authoritative. This document
> is a cross-reference, not a redefinition.

---

## How to read this table

| Column | Meaning |
|---|---|   
| Check ID | Conformance suite identifier |
| Assertion | What must be true for the check to pass |
| Service signal | The exact thing the service does to satisfy this check |
| Source | Where in the service the signal is produced |
| Fault that breaks it | Which fault deliberately causes this check to fail |
| Survives fault | Checks that must PASS even when the named fault is active |

---

## S-series — System State Checks
*These checks observe systemd unit state. The service does not produce these signals directly — systemd manages them. Included here to show what the service must not interfere with.*

| Check | Assertion | Service obligation | Breaks on fault |
|---|---|---|---|
| S-001 | `app.service` is active | Service must not crash on startup; must bind successfully; must not exit 0 unexpectedly | F-001 (config missing), F-003 (config unreadable), F-005 (binary not executable), F-006 (APP_ENV missing), F-009 (log unwritable), F-017 (APP_ENV empty) |
| S-002 | `app.service` is enabled | Systemd unit file must be present and valid. Service does not manage this — provisioning does. | F-013 (unit file broken) |
| S-003 | `nginx` is active | Service must respond on `127.0.0.1:8080` so nginx upstream does not fail its own health check | F-007 (nginx wrong upstream — nginx fails, not app) |
| S-004 | `nginx` is enabled | No service obligation. Provisioning concern. | F-013 (unit file broken — this also causes nginx restart failure in some configs) |

---

## P-series — Process Checks

| Check | Assertion | Exact service signal | Source | Breaks on fault |
|---|---|---|---|---|
| P-001 | Process `server` running as `appuser` | Service process exists in process table with name matching `BinaryName = "server"` and user `appuser`. Process name is set by the binary filename at `/opt/app/server`. | `/opt/app/server` binary path | F-005 (chmod 640 — binary not executable, process never starts) |
| P-002 | App listens on `127.0.0.1:8080` | Service calls `net.Listen("tcp", cfg.Server.Addr)` where canonical `Addr = "127.0.0.1:8080"`. Socket is bound before `/run/app/healthy` is created. | `main.go` step 9, `server/server.go` `http.Server.Addr` | F-002 (config addr changed to `127.0.0.1:9090`) |
| P-003 | nginx listens on `0.0.0.0:80` | No service obligation. nginx bind. | nginx config | F-015 (nginx config syntax error — nginx reload fails, old config active, P-003 still passes) |
| P-004 | nginx listens on `0.0.0.0:443` | No service obligation. nginx bind. | nginx config | F-015 (same as P-003) |
| P-005 | `/run/app/app.pid` contains service PID | Service writes `strconv.Itoa(os.Getpid()) + "\n"` to `/run/app/app.pid` atomically (temp+rename) before accepting requests. | `signals/signals.go` `WritePID()`, called at `main.go` step 7 | Any fault that prevents startup (F-001, F-003, F-005) |

---

## E-series — Endpoint Checks
*The most diagnostically sensitive checks. F-004 produces the E-001-passes / E-002-fails split that is the primary diagnostic pattern.*

| Check | Assertion | Exact service signal | Source | Breaks on fault | Survives fault |
|---|---|---|---|---|---|
| E-001 | `GET /health` returns 200 | Handler always returns `HTTP 200` with body `{"status":"ok"}`. Handler **never** touches `/var/lib/app`. | `server/server.go` `handleHealth` | F-001, F-003, F-005, F-006, F-009, F-017 (service crash faults) | **F-004, F-018** — state dir broken but `/health` unaffected |
| E-002 | `GET /` returns 200 | Handler calls `touchStatePath()` → `os.OpenFile("/var/lib/app/state", O_CREATE\|O_WRONLY\|O_TRUNC, 0600)`. On success: `HTTP 200` with `{"status":"ok","env":"<APP_ENV>"}`. On failure: `HTTP 500` with `{"status":"error","msg":"state write failed"}`. | `server/server.go` `handleRoot`, `touchStatePath` | **F-004** (chmod 000 `/var/lib/app`), **F-018** (inode exhaustion) | E-001, E-003 always pass independently |
| E-003 | `/health` body contains `"status":"ok"` | Body is exactly `{"status":"ok"}` — a string literal, not JSON-encoded struct. No variation. | `server/server.go` `handleHealth` | Same as E-001 | Same as E-001 |
| E-004 | Response includes `X-Proxy: nginx` header | **nginx adds this header** via `proxy_set_header X-Proxy nginx` in the nginx config. The service does NOT set this header. If nginx is bypassed (direct access to `127.0.0.1:8080`), this header is absent. | `nginx` — not the service | F-007 (nginx wrong upstream — nginx returns 502, no X-Proxy header) | — |
| E-005 | `GET https://app.local/health` returns 200 | Service responds to `/health` normally. nginx handles TLS termination for `app.local`. The service only needs to respond correctly on `127.0.0.1:8080`. | `server/server.go` `handleHealth` (via nginx TLS proxy) | F-007 (nginx wrong upstream) | F-004, F-018 — same as E-001 |

---

## F-series — Filesystem Checks
*These checks observe file existence, ownership, and mode bits. The service does not create or manage most of these — provisioning does. Exceptions noted.*

| Check | Assertion | Service obligation | Breaks on fault |
|---|---|---|---|
| F-001 | `/opt/app/server` exists, mode 750, owned `appuser:appuser` | Binary must exist at this path. Mode 750 is set by provisioning. Service does not manage its own binary permissions. | F-005 (chmod 640 — mode wrong) |
| F-002 | `/etc/app/config.yaml` exists, mode 640, owned `appuser:appuser` | Service reads this file but does not create or manage it. Provisioning responsibility. | F-001 (rm config), F-003 (chmod 000 config) |
| F-003 | `/var/log/app/` exists, mode 755, owned `appuser:appuser` | Service writes to `/var/log/app/app.log`. It does not create the directory. If the directory is missing, startup fails at step 5. | F-009 (chmod 000 log file) |
| F-004 | `/var/lib/app/` exists, mode 755, owned `appuser:appuser` | Service writes `/var/lib/app/state` on every `GET /`. Also writes `/var/lib/app/.startup-probe` at startup (probe). Does not manage directory permissions. | **F-004** (chmod 000 — this is the fault that breaks this check) |
| F-005 | nginx config passes syntax check | No service obligation. nginx config managed by provisioning. | F-015 (invalid_directive in nginx config) |
| F-006 | TLS certificate exists and has not expired | No service obligation. TLS cert managed by provisioning. | (cert expiry only — no fault in current catalog) |
| F-007 | `app.local` resolves to `127.0.0.1` | No service obligation. `/etc/hosts` entry managed by provisioning. | (no fault in current catalog) |

---

## L-series — Log Checks
*All L-series checks are **degraded severity** — they fail without affecting the exit code. L-003 is the most important: it verifies the startup log entry that the service is required to emit.*

| Check | Assertion | Exact service signal | Source | Breaks on fault |
|---|---|---|---|---|
| L-001 | `/var/log/app/app.log` exists and is non-empty | Service opens the log file at startup (step 5) and immediately writes the startup entry (step 6). The file is non-empty from the first second of operation. | `main.go` step 5 (`os.OpenFile`), step 6 (`logger.Info`) | F-010 (rm app.log while running — file disappears from filesystem; L-001 fails but app continues) |
| L-002 | Last line of `app.log` is valid JSON | Service uses `slog.NewJSONHandler` — every log entry is newline-delimited JSON. No plain-text log lines are ever written. | `main.go` `slog.New(slog.NewJSONHandler(...))` | F-010 (log file deleted — no last line to check) |
| L-003 | `app.log` contains a startup entry | Service emits `logger.Info("server started", ...)` at step 6, before accepting requests. The log line contains `"msg":"server started"` as a top-level JSON field. This is the check that proves the service started and logged correctly. | `main.go` step 6: `logger.Info("server started", "addr", ..., "app_env", ..., "chaos_active", ...)` | F-009 (chmod 000 log file — service cannot open log, startup fails at step 5) |

---

## Signal file summary (checks not in S/P/E/F/L series)

| Signal | Path | Written when | Used by |
|---|---|---|---|
| PID file | `/run/app/app.pid` | Step 7, before accepting requests | P-005 |
| Loading marker | `/run/app/loading` | Step 3 (created), step 12 (removed) | Control plane: RECOVERING state |
| Healthy marker | `/run/app/healthy` | Step 11, after socket bound | Control plane: CONFORMANT evidence |
| Status string | `/run/app/status` | Steps 2, 13, on state changes, shutdown | Control plane: diagnostic context |
| Telemetry | `/run/app/telemetry.json` | Every 2 seconds from step 10 | L-004 (`chaos_active` must be false); operator visibility |

---

## Diagnostic check patterns (fault → failing checks)

These patterns are the primary diagnostic tool. When `lab validate` fails, match the failing check set to identify the active fault.

| Failing checks | Active fault | Distinguishing signal |
|---|---|---|
| S-001 + E-series; F-002 **fails** | F-001 (config deleted) | `ls /etc/app/config.yaml` — absent |
| S-001 + E-series; F-002 passes, mode 000 | F-003 (config unreadable) | `stat /etc/app/config.yaml` — mode 0000 |
| S-001 + E-series; F-002 passes, mode OK | F-006, F-009, or F-017 | `journalctl -u app.service -n 5` |
| S-001 + E-series; F-001 mode wrong | F-005 (binary not executable) | `ls -la /opt/app/server` — mode 640 |
| **E-002 only**; E-001 passes | **F-004** (state dir) or **F-018** (inodes) | `df -i /var/lib/app` — inodes full → F-018; else F-004 |
| E-series only; P-002 passes | F-007 (nginx wrong upstream) | `ss -ltnp \| grep 8080` — app on 8080, nginx misconfigured |
| P-002 + E-series; S-001 passes | F-002 (app wrong port) | `ss -ltnp \| grep 9090` — app on wrong port |
| F-005 only | F-015 (nginx config syntax) | `sudo nginx -t` — syntax error |
| P-002 only; E-series pass | F-016 (app on all interfaces) | `ss -ltnp \| grep 8080` — shows `0.0.0.0` |
| L-series only; `lab validate` exits 0 | F-010 (log deleted) | `lsof +L1 \| grep app.log` — deleted fd held |
| None; `lab validate` exits 0 | F-008 or F-014 | `time systemctl stop app` (F-008) or `ps \| grep Z` (F-014) |

---

## E-001 / E-002 split — the load-bearing design decision

`GET /health` and `GET /` are deliberately asymmetric:

```
GET /health  → never touches /var/lib/app  → survives F-004, F-018
GET /        → always touches /var/lib/app → breaks on F-004, F-018
```

This asymmetry is the mechanism by which F-004 and F-018 produce their diagnostic pattern. If `/health` also touched the state directory, both E-001 and E-002 would fail simultaneously and the pattern would be indistinguishable from a service crash (F-001, F-003, etc.).

The rule is enforced in code by the absence of any `/var/lib/app` reference in `handleHealth`. Any future change that adds state directory access to `/health` breaks this contract.