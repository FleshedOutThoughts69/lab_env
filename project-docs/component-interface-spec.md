# Component Interface Specification

## Version 1.1.0

> **Authority:** this document defines the exact interface contracts for the lab environment's public interfaces: the HTTP service API, the application log schema, and the state machine transition map. It is the formal specification that the conformance suite enforces and the CLI parses.
>
> **Companion documents:** `service/CONFORMANCE_CONTRACT.md` (check-to-signal mapping), `conformance-model.md §3` (check catalog), `system-state-model.md §3` (transition model), `fault-model.md §7.2` (fault catalog).

---

## §1 — HTTP Service Protocol

The subject service is a Go HTTP server bound to `127.0.0.1:8080`. All conformance‑relevant traffic is proxied through nginx. Five endpoints are defined. No authentication. No request body is accepted by any endpoint.

### 1.1 Common Response Headers

All responses from the service itself (pre‑nginx):

| Header | Value | Always present |
|---|---|---|
| `Content-Type` | `application/json` | Yes |

All responses via nginx proxy (what conformance checks observe):

| Header | Value | Source | Conformance check |
|---|---|---|---|
| `Content-Type` | `application/json` | Service | — |
| `X-Proxy` | `nginx` | nginx (`add_header X-Proxy nginx always`) | E-004 |
| `X-Real-IP` | Client IP | nginx | — |
| `X-Forwarded-For` | Client IP chain | nginx | — |
| `X-Forwarded-Proto` | `http` or `https` | nginx | — |

### 1.2 `GET /health`

**Purpose:** readiness and liveness probe. The load‑bearing diagnostic endpoint — must survive all state‑directory faults.

**Conformance checks satisfied:** E‑001 (returns 200), E‑003 (body contains `"status":"ok"`), E‑005 (same via HTTPS at `https://app.local/health`).

**Contract constraints:**
- MUST return 200 in any conformant environment
- MUST NOT touch `/var/lib/app` — this is the contract that makes F‑004 and F‑018 diagnosable
- Chaos latency (`CHAOS_LATENCY_MS`) is **exempted** for this path — latency is not injected on `/health` to preserve E‑001 diagnostic integrity
- Chaos drop percent (`CHAOS_DROP_PERCENT`) **does** apply — a dropped `/health` request returns 503

#### Success response

```
HTTP/1.1 200 OK
Content-Type: application/json

{"status":"ok","app_env":"prod","config_loaded":true}
```

| Field | Type | Nullable | Value |
|---|---|---|---|
| `status` | string | no | always `"ok"` |
| `app_env` | string | no | value of `APP_ENV` config field; canonical value `"prod"` |
| `config_loaded` | boolean | no | always `true` while the service is running |

The body is a string literal — it is not produced by `json.Marshal`. It never varies.

#### Error response

This endpoint has no error response. It returns 200 or is unreachable (service crashed). There is no 4xx or 5xx from this handler under any fault in the current catalog.

---

### 1.3 `GET /`

**Purpose:** stateful health check. Touches `/var/lib/app/state` on every request to demonstrate that state directory faults are observable.

**Conformance check satisfied:** E‑002 (returns 200 when healthy).

**Fault targets:** F‑004 (state dir mode 000 → 500), F‑018 (inode exhaustion → 500), F‑019 (block exhaustion → 500).

#### Success response

```
HTTP/1.1 200 OK
Content-Type: application/json

{"status":"ok","path":"/","env":"prod"}
```

| Field | Type | Nullable | Notes |
|---|---|---|---|
| `status` | string | no | always `"ok"` |
| `path` | string | no | always `"/"` |
| `env` | string | no | value of `APP_ENV` config field; canonical value `"prod"`; empty string `""` when F‑006 or F‑017 active; never null |

`env` is always present in success responses. It is never omitted and never null. An empty string is a valid value (produced by F‑006 and F‑017 which clear `APP_ENV`).

#### Error response

```
HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"status":"error","msg":"state write failed"}
```

| Field | Type | Nullable | Notes |
|---|---|---|---|
| `status` | string | no | always `"error"` |
| `msg` | string | no | always `"state write failed"` |

**The `path` and `env` fields are absent in the error response.** The two response shapes are distinguished by `status` value and by the presence/absence of `path`/`env`/`msg`. No other 5xx response is produced by this handler.

**Schema summary — `GET /` response union:**

```json
// Success
{ "status": "ok",    "path": "/", "env": "<string>" }

// Error (F-004, F-018, or F-019 active)
{ "status": "error", "msg": "state write failed" }
```

---

### 1.4 `GET /slow`

**Purpose:** baseline network behaviour demonstration (B‑001). Not part of the blocking conformance suite.

**Contract:** sleeps for exactly 5 seconds, then returns 200. This delay exceeds nginx's `proxy_read_timeout` (~3 seconds), causing a 504 when accessed via nginx. Direct access to `127.0.0.1:8080/slow` succeeds after 5 seconds.

#### Response (direct access only — nginx returns 504 before this arrives)

```
HTTP/1.1 200 OK
Content-Type: application/json

{"status":"ok","path":"/slow","delay_seconds":5}
```

| Field | Type | Nullable | Notes |
|---|---|---|---|
| `status` | string | no | always `"ok"` |
| `path` | string | no | always `"/slow"` |
| `delay_seconds` | integer | no | always `5` |

---

### 1.5 `GET /headers`

**Purpose:** proxy‑header visibility for diagnosing nginx forwarding behaviour.

**Conformance check satisfied:** H‑001 (body contains `Host` field).

**Contract:** echoes the incoming request headers `Host`, `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Real-IP`, and `User-Agent` as a JSON object. If a header is absent, its value is an empty string.

#### Response

```
HTTP/1.1 200 OK
Content-Type: application/json

{"Host":"localhost","X-Forwarded-For":"","X-Forwarded-Proto":"http","X-Real-IP":"127.0.0.1","User-Agent":"curl/8.x.x"}
```

| Field | Type | Nullable | Notes |
|---|---|---|---|
| `Host` | string | no | value of the `Host` request header; empty string if absent |
| `X-Forwarded-For` | string | no | value of the `X-Forwarded-For` request header; empty string if absent |
| `X-Forwarded-Proto` | string | no | value of the `X-Forwarded-Proto` request header; empty string if absent |
| `X-Real-IP` | string | no | value of the `X-Real-IP` request header; empty string if absent |
| `User-Agent` | string | no | value of the `User-Agent` request header; empty string if absent |

---

### 1.6 `GET /reset`

**Purpose:** TCP connection‑reset behaviour for network‑layer diagnostics.

**Conformance check satisfied:** H‑002 (connection reset is delivered to the client).

**Contract:** sets `SO_LINGER` with zero timeout on the TCP connection and closes it immediately. The client receives a TCP RST (connection reset by peer), not a clean FIN. No HTTP response is sent.

#### Response

No HTTP status line, no headers, no body. The connection is terminated at the TCP level.

**Verification:** `curl -v http://localhost/reset 2>&1 | grep -E 'Empty reply|Connection reset'` should show a connection‑reset error.

---

### 1.7 Chaos Middleware Behavior

The chaos middleware wraps the entire mux. It runs before any handler.

| Chaos mode | Effect | Exemptions |
|---|---|---|
| `CHAOS_DROP_PERCENT=N` | N% of requests return 503 immediately | None — applies to all paths including `/health` |
| `CHAOS_LATENCY_MS=N` | N millisecond delay before handler | `/health` is exempt — latency not injected on this path |

When a request is dropped by chaos middleware:

```
HTTP/1.1 503 Service Unavailable
Content-Type: application/json

{"status":"error","msg":"chaos drop"}
```

| Field | Type | Nullable | Notes |
|---|---|---|---|
| `status` | string | no | always `"error"` |
| `msg` | string | no | always `"chaos drop"` |

---

## §2 — Application Log Schema

The service writes newline‑delimited JSON to `/var/log/app/app.log`. Every line is a complete JSON object. No multi‑line entries. No plain‑text lines.

### 2.1 Base Schema

Every log entry contains exactly these three mandatory fields plus zero or more optional fields:

```json
{
  "ts":    "<RFC3339Nano UTC>",
  "level": "<info|warn|error>",
  "msg":   "<string>"
}
```

| Field | Type | Nullable | Format | Notes |
|---|---|---|---|
| `ts` | string | no | RFC3339Nano in UTC | e.g. `"2026-01-15T12:34:56.789012345Z"` — always Z‑suffix |
| `level` | string | no | enum: `"info"`, `"warn"`, `"error"` | no `"debug"` level is used |
| `msg` | string | no | human‑readable message | the conformance check L‑003 matches on `"msg":"server started"` |

Additional fields are key‑value pairs appended after the mandatory fields. All keys are strings. Values may be strings, numbers, or booleans — never objects or arrays.

### 2.2 Known Log Entries (Catalog)

These are all log entries the service is defined to emit. The `msg` value is the exact string used. Entries marked **(aspirational)** are part of the specification but not yet fully verified against the current service implementation.

#### Startup entries

| msg | level | Additional fields | Conformance check | When emitted |
|---|---|---|---|---|
| `"server started"` | info | `addr` (string), `app_env` (string), `chaos_active` (bool) | **L‑003** | Step 6 of startup, before accepting requests |
| `"listening"` **(aspirational)** | info | `addr` (string) | — | Step 9, after socket bound |

**L‑003 critical:** the conformance check matches `"msg":"server started"` as a substring in the log file. The entry must appear before requests are accepted. It is written at startup step 6, before the socket is bound at step 9.

Example `"server started"` entry:
```json
{"ts":"2026-01-15T12:00:00.000000000Z","level":"info","msg":"server started","addr":"127.0.0.1:8080","app_env":"prod","chaos_active":false}
```

#### Chaos entries (when chaos active)

| msg | level | Additional fields | When emitted |
|---|---|---|---|
| `"chaos modes active"` **(aspirational)** | warn | `modes` (string, comma‑joined mode names) | Startup, when any chaos mode is active |
| `"SIGTERM ignored (CHAOS_IGNORE_SIGTERM=1) — process will require SIGKILL"` **(aspirational)** | warn | none | Startup, when F‑008 fault flag is active |
| `"chaos drop"` **(aspirational)** | warn | `path` (string), `drop_percent` (int) | Each dropped request (if logger non‑nil) |

#### Request error entries

| msg | level | Additional fields | Conformance relevance | When emitted |
|---|---|---|---|---|
| `"state write failed"` | error | `path` (string: `/var/lib/app/state`), `error` (string) | E‑002 fails when this is logged | F‑004, F‑018, or F‑019 active, on each `GET /` |

Example `"state write failed"` entry:
```json
{"ts":"2026-01-15T12:00:01.000000000Z","level":"error","msg":"state write failed","path":"/var/lib/app/state","error":"open /var/lib/app/state: permission denied"}
```

#### Shutdown entries **(aspirational)**

| msg | level | Additional fields | When emitted |
|---|---|---|---|
| `"shutdown signal received"` | info | `signal` (string: `"terminated"` or `"interrupt"`) | On SIGTERM or SIGINT |
| `"graceful shutdown started"` | info | `grace_period` (string: duration e.g. `"5s"`) | After signal, before drain |
| `"shutdown complete"` | info | none | After HTTP drain completes |
| `"shutdown did not complete cleanly"` | warn | `error` (string) | If drain context times out |

#### Startup error entries **(aspirational)**

| msg | level | Additional fields | Cause |
|---|---|---|---|
| `"writing PID file"` | error | `error` (string) | `/run/app/app.pid` write failed |
| `"creating healthy marker"` | error | `error` (string) | `/run/app/healthy` write failed |
| `"state directory not writable at startup"` | warn | `path` (string), `error` (string) | Startup probe detected F‑004 early |
| `"HTTP server exited unexpectedly"` | error | `error` (string) | `http.Server.ListenAndServe` returned non‑nil without shutdown signal |

### 2.3 Schema Invariants

These invariants are enforced by the logging package and verified by conformance checks:

1. **Every line is valid JSON** — `json.Marshal` is used for every entry. Plain text is never written to `app.log`. Conformance check L‑002.

2. **Every line ends with exactly one newline** — the logging package appends `\n` to the marshalled JSON. No multi‑line entries.

3. **`ts` is always UTC RFC3339Nano** — `time.Now().UTC().Format(time.RFC3339Nano)`. Never local time. Never RFC3339 (seconds only).

4. **`level` is always one of three values** — `"info"`, `"warn"`, `"error"`. No other values are used.

5. **`msg` is never empty** — all call sites provide a non‑empty message string.

6. **Optional field values are never objects or arrays** — additional fields are flat key‑value pairs only.

7. **File is opened `O_APPEND`** — required for logrotate `copytruncate` compatibility. Without `O_APPEND`, the file offset would point past the truncated end after rotation, producing null bytes.

### 2.4 Forward Compatibility

New optional fields may be added to any log entry without a version increment. Consumers MUST tolerate unknown fields. Existing fields MUST NOT change type or be removed.

The only protected constraint for backward compatibility is: any log entry with `"msg":"server started"` must remain present in the log file after startup. This is the L‑003 conformance check — removing or renaming this entry is a breaking change.

---

## §3 — State Machine Transition Map

### 3.1 State Definitions

| State | Invariants | Permitted operations |
|---|---|---|
| `UNPROVISIONED` | No service, no binary, no config. `active_fault` is null. | `lab provision` only |
| `PROVISIONED` | Bootstrap complete. Conformance not verified. `active_fault` is null. | `lab validate`, `lab provision` |
| `CONFORMANT` | All 25 blocking checks pass. `active_fault` is null. | All commands |
| `DEGRADED` | Exactly one fault active. `active_fault` is non‑null with fault ID. Fault postcondition checks fail. | `lab status`, `lab validate`, `lab reset`, `lab history`, `lab fault apply --force` |
| `BROKEN` | One or more blocking checks fail. `active_fault` is null. No fault explains the failure. | `lab status`, `lab validate`, `lab reset`, `lab history` |
| `RECOVERING` | Reset operation in progress. Transient. | None — concurrent operations rejected |

### 3.2 Complete Transition Table

| From | To | Trigger | Guard | On failure |
|---|---|---|---|---|
| `UNPROVISIONED` | `PROVISIONED` | `lab provision` | VM reachable | `UNPROVISIONED` |
| `PROVISIONED` | `CONFORMANT` | `lab validate` (all blocking pass) | Bootstrap complete | `BROKEN` |
| `PROVISIONED` | `BROKEN` | `lab validate` (any blocking fail) | Bootstrap complete | — |
| `CONFORMANT` | `DEGRADED` | `lab fault apply <ID>` | State is CONFORMANT; no active fault; PreconditionChecks pass | `BROKEN` (partial mutation) |
| `CONFORMANT` | `BROKEN` | External modification | Was CONFORMANT | — |
| `DEGRADED` | `RECOVERING` | `lab reset` | Active fault recorded | `BROKEN` |
| `BROKEN` | `RECOVERING` | `lab reset` | None | `BROKEN` |
| `RECOVERING` | `CONFORMANT` | Reset + validate pass | Operations complete | `BROKEN` |
| `RECOVERING` | `BROKEN` | Reset failure | Operations failed | — |

**Force transitions (bypass guards):**

| From | To | Trigger | Effect |
|---|---|---|---|
| `DEGRADED` | `DEGRADED` | `lab fault apply <ID> --force` | Replaces active fault; no recovery of prior fault |
| `BROKEN` | `DEGRADED` | `lab fault apply <ID> --force` | Fault applied on broken environment; result is ambiguous; `forced: true` recorded |

### 3.3 CONFORMANT → DEGRADED: Fault Trigger Map

Every fault that can trigger a CONFORMANT → DEGRADED transition, with the postcondition state — the conformance check profile that becomes true after Apply succeeds.

| Fault ID | Name | Checks that FAIL after Apply | Checks that continue to PASS | Reset tier |
|---|---|---|---|---|
| F‑001 | Missing config file | S‑001, E‑001, E‑002, E‑003, E‑004, E‑005 | F‑003, F‑007 | R2 |
| F‑002 | Wrong listen port | P‑002, E‑001, E‑002, E‑003, E‑004, E‑005 | S‑001, P‑001, F‑002 | R2 |
| F‑003 | Config unreadable | S‑001, E‑001, E‑002, E‑003, E‑004, E‑005 | F‑002, F‑007 | R2 |
| F‑004 | State dir unwritable | E‑002, F‑004 | S‑001, E‑001, E‑003 | R2 |
| F‑005 | Binary not executable | S‑001, E‑001, E‑002, E‑003, E‑004, E‑005, F‑001 | F‑002 | R2 |
| F‑006 | APP_ENV removed from unit | S‑001, E‑001, E‑002, E‑003, E‑004, E‑005 | F‑002 | R2 |
| F‑007 | nginx wrong upstream | E‑001, E‑002, E‑003, E‑004, E‑005 | S‑001, P‑001, P‑002 | R2 |
| F‑008 | SIGTERM ignored | _(none while running — Apply returns error; no mutation occurs)_ | All 25 pass | R3 |
| F‑009 | Log file unwritable | S‑001, E‑001, E‑002, E‑003, E‑004, E‑005, L‑001, L‑002, L‑003 | F‑002 | R2 |
| F‑010 | Log file deleted while running | L‑001, L‑002, L‑003 _(degraded only)_ | S‑001, P‑001, P‑002, E‑001, E‑002 | R1 |
| F‑013 | Unit file ExecStart broken | S‑001, E‑001, E‑002, E‑003, E‑004, E‑005 | S‑002 | R2 |
| F‑014 | Zombie accumulation | _(none initially — Apply returns error; no mutation occurs)_ | All 25 pass | R3 |
| F‑015 | nginx config syntax error | F‑005 | S‑003, P‑003, P‑004, E‑001, E‑002 | R2 |
| F‑016 | App on all interfaces | P‑002 | S‑001, E‑001, E‑002, E‑003, E‑004 | R2 |
| F‑017 | Empty APP_ENV | S‑001, E‑001, E‑002, E‑003, E‑004, E‑005 | F‑002, F‑001 | R2 |
| F‑018 | Inode exhaustion | E‑002, F‑004 | S‑001, E‑001, E‑003 | R2 |
| F‑019 | Block exhaustion (disk full) | E‑002, F‑004 | S‑001, E‑001, E‑003 | R2 |
| F‑020 | Chaos latency (400ms) | _(none — chaos only; no checks fail)_ | All 25 pass | R2 |
| F‑021 | nftables drop rule | E‑001, E‑002, E‑003, E‑004, E‑005 | S‑001, P‑001, P‑002 | R2 |

**F‑008 and F‑014 notes:** The `Apply` function returns an error because these faults require a binary rebuild with build flags. This is expected — no mutation occurs, and the state remains CONFORMANT. R3 reset is the recovery path for both. When fully implemented with a binary rebuild, F‑008 manifests at shutdown (SIGTERM ignored) and F‑014 manifests over time (zombie accumulation).

**F‑010 note:** `lab validate` exits 0 because L‑series checks are degraded severity. The fault is active and recorded in `state.json`; the environment is DEGRADED but not NON‑CONFORMANT.

**F‑020 note:** `lab validate` exits 0 — no conformance checks fail. The fault is observable via `time curl` (adds ~400ms latency) and telemetry `chaos_active=true`.

**F‑021 note:** The nftables rule is scoped to the external interface (`iif enp0s8`). On a single‑VM lab, all traffic goes through loopback, so the fault may not be visible via `localhost`. The diagnostic pattern is verified by checking the nft chain directly.

### 3.4 Fault Confirmation: --force vs. PreconditionChecks

| Fault | `--yes` required | `PreconditionChecks` | Guard can be bypassed with `--force` |
|---|---|---|---|
| F‑001–F‑007, F‑009, F‑013, F‑015–F‑021 | no | none | N/A |
| F‑010 | no | `[P‑001]` (app must be running) | yes — but fault loses teaching value if bypassed |
| F‑008 | **yes** | none | N/A |
| F‑014 | **yes** | none | N/A |

### 3.5 Diagnostic State Patterns

These are the mappings from conformance check failure sets to active fault candidates. Used by operators when `lab validate` fails without a known active fault.

| Observed failure pattern | Candidate faults | Distinguishing command |
|---|---|---|
| S‑001 + E‑series; F‑002 absent | F‑001 | `ls /etc/app/config.yaml` — file missing |
| S‑001 + E‑series; F‑002 present, mode 000 | F‑003 | `stat /etc/app/config.yaml` — mode 0000 |
| S‑001 + E‑series; F‑002 present, mode OK | F‑006, F‑009, F‑017 | `journalctl -u app.service -n 5` |
| S‑001 + E‑series; F‑001 mode wrong | F‑005 | `ls -la /opt/app/server` — mode 640 |
| E‑002 only; E‑001 passes | F‑004, F‑018, F‑019 | `df -i /var/lib/app` — inodes → F‑018; `df -h /var/lib/app` — blocks → F‑019; else F‑004 |
| E‑series only; P‑002 passes | F‑007, F‑021 | `sudo nft list chain inet lab_filter LAB-FAULT` — if rule present → F‑021; else F‑007 |
| P‑002 + E‑series; S‑001 passes | F‑002 | `ss -ltnp \| grep 9090` |
| F‑005 only | F‑015 | `sudo nginx -t` |
| P‑002 only; E‑series pass | F‑016 | `ss -ltnp \| grep 8080` — shows `0.0.0.0` |
| L‑series only; `lab validate` exits 0 | F‑010 | `lsof +L1 \| grep app.log` |
| None; `lab validate` exits 0 | F‑008, F‑014, F‑020 | Apply error (F‑008/F‑014); `time curl` latency (F‑020) |

---

## §4 — Telemetry Schema

The service writes `/run/app/telemetry.json` every 2 seconds using atomic temp‑file + rename. This file is machine‑readable; it is distinct from `app.log` (diagnostic text for operators).

### 4.1 Schema

```json
{
  "ts":                   "2026-01-15T12:00:00Z",
  "pid":                  1234,
  "uptime_seconds":       120,
  "cpu_percent":          12.3,
  "memory_rss_mb":        45.2,
  "open_fds":             18,
  "disk_usage_percent":   67.0,
  "inode_usage_percent":  12.0,
  "requests_total":       1042,
  "errors_total":         3,
  "chaos_active":         false,
  "chaos_modes":          []
}
```

| Field | Type | Nullable | Notes |
|---|---|---|---|
| `ts` | string | no | RFC3339 UTC (seconds precision, not nanoseconds) |
| `pid` | integer | no | Process ID |
| `uptime_seconds` | integer | no | Seconds since startup |
| `cpu_percent` | float | no | % of one CPU core; 0.0 on first write |
| `memory_rss_mb` | float | no | Resident set size in MiB; 0.0 on read error |
| `open_fds` | integer | no | Open file descriptors; 0 on read error |
| `disk_usage_percent` | float | no | Block usage % for `/var/lib/app`; 0.0 on error |
| `inode_usage_percent` | float | no | Inode usage % for `/var/lib/app`; near 100 when F‑018 active |
| `requests_total` | integer | no | Cumulative since start; includes dropped chaos requests |
| `errors_total` | integer | no | Cumulative 5xx responses + dropped chaos requests |
| `chaos_active` | boolean | no | `true` if any chaos mode is active |
| `chaos_modes` | array of string | no | Active mode names; `[]` (never null) when none active |

**`chaos_modes` values:** `"latency"`, `"drop"`, `"oom"`, `"nosigterm"`.

**`inode_usage_percent` fault signal:** this field approaches 100.0 during F‑018 (inode exhaustion) while `disk_usage_percent` remains low. During F‑019 (block exhaustion), both `inode_usage_percent` and `disk_usage_percent` approach 100.0.

### 4.2 Signal Files

| File | Content | Created | Removed | Purpose |
|---|---|---|---|---|
| `/run/app/app.pid` | Decimal PID + `\n` | Startup step 7 | Shutdown step 4 | P‑005 conformance check |
| `/run/app/healthy` | Empty | Startup step 11 | Shutdown step 2 | Bootstrap readiness gate (step 16) |
| `/run/app/loading` | Empty | Startup step 3 | Startup step 12 | Control plane: RECOVERING evidence |
| `/run/app/status` | One of: `Starting`, `Running`, `Degraded`, `Unhealthy`, `ShuttingDown` | Startup step 2 | Never removed | Control plane: diagnostic context |

**`/run/app/status` vocabulary:**

| Value | Meaning | When set |
|---|---|---|
| `Starting` | Startup sequence in progress | Step 2 |
| `Running` | Fully initialized, all checks passed | Step 13 |
| `Degraded` | Serving with reduced functionality (chaos active) | When chaos mode activates |
| `Unhealthy` | State directory write fails; `/` returns 500 | On F‑004/F‑018/F‑019 write failure |
| `ShuttingDown` | SIGTERM received, drain in progress | Shutdown step 1 |

**These are NOT canonical state names.** They are service‑internal status words. The control plane maps them to canonical states (CONFORMANT, DEGRADED, BROKEN) using the full detection algorithm — not by reading this file alone.

---

*End of Component Interface Specification v1.1.0.*