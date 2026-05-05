# Canonical Lab Environment
## Specification v1.0.0

> **Document authority:** This document defines the canonical environment contract for the Linux, Networking, OS Fundamentals, and Security practice problem sets. When this document and any other source conflict, this document is authoritative. When runtime observation and this document conflict, the environment is non-conformant.
>
> **Companion repository:** `lab-env/` — contains the Go service implementation, bootstrap script, conformance suite, fault injection scripts, and reset scripts. This document defines what those artifacts must do; the repository implements it.
>
> **Normative language:** MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT — mandatory. SHOULD, RECOMMENDED — strongly preferred. MAY, OPTIONAL — permitted but not required.

---

## Authority Hierarchy

When sources disagree, resolve by priority (highest first):

1. **Conformance suite** — the executable expression of this spec (`lab-env/validate.sh`). If the suite passes, the environment is conformant regardless of other observations.
2. **Canonical artifact contents** — the exact file contents defined in §4 of this document.
3. **Spec text** — the normative statements in this document.
4. **Runtime observation** — what tools show about the running system.
5. **Learner modifications** — anything the learner has changed. Learner modifications do not alter the spec; they produce a non-conformant or intentionally mutated environment.

---

## Conformance Model

An environment is in exactly one of four states:

**Conformant:** all conformance suite checks pass. This is the baseline state. Every practice problem begins from this state unless the problem explicitly states a fault is active.

**Degraded-conformant:** one or more non-critical checks fail (e.g., log rotation not yet triggered) but all behavioral checks pass. Acceptable for most problems.

**Non-conformant:** one or more behavioral checks fail due to build error, provisioning failure, or unintended learner modification. The environment MUST be reset before continuing.

**Fault-state:** a fault from the fault catalog (§7) has been deliberately applied. The environment is intentionally non-conformant in a specific, documented way. The active fault ID MUST be recorded. Reset returns the environment to conformant.

---

## Non-Goals

This environment intentionally excludes:

- Container runtimes (Docker, containerd, podman)
- Container orchestration (Kubernetes, Nomad)
- External DNS (all name resolution is local)
- Authentication systems (no OAuth, LDAP, PAM beyond defaults)
- Multi-host networking (single VM only)
- Service mesh
- CDN or external load balancing
- Persistent message queues
- Distributed tracing
- Databases (no PostgreSQL, MySQL, Redis)
- SELinux (Ubuntu ships AppArmor; see §9)

Adding any of these to the environment produces undefined behavior for the practice problems.

---

---

# §1 — Environment Overview

The canonical environment is a single Ubuntu 22.04 LTS virtual machine running a small Go HTTP service behind an nginx reverse proxy, managed by systemd, operated by a non-privileged service account.

**The pedagogical property this environment serves:**

Every failure mode in the Linux, Networking, OS, and Security curriculum manifests as a break somewhere in a single dependency chain:

```
filesystem → permissions → process → service → socket → proxy → response
```

The environment is designed so that exactly one layer can be broken at a time, the break is observable through the tools taught in the curriculum, and the break can be reset to baseline without rebuilding the entire environment.

**The service is deliberately small.** Its purpose is not to demonstrate software engineering. Its purpose is to provide a realistic runtime surface that generates meaningful signals — process state, file descriptor behavior, system calls, network connections, log output — that learners can observe and reason about. The entire Go service source SHOULD fit in one focused reading session (~400–600 lines).

**The nginx proxy exists for two reasons:** it separates the network-facing layer from the application layer (producing the `502 Bad Gateway` signal that distinguishes "nginx alive, app dead" from "nginx dead"), and it provides a TLS termination point for the networking problem set.

---

# §2 — Canonical Environment Contract

## 2.1 Platform

- **Base OS:** Ubuntu Server 22.04.4 LTS (Jammy Jellyfish)
- **Architecture:** x86-64
- **Minimum resources:** 1 vCPU, 1GB RAM, 10GB disk
- **Provisioning:** target-agnostic bootstrap script on a fresh Ubuntu 22.04 installation. The bootstrap MUST be idempotent — running it twice on the same system MUST converge to the canonical baseline without error.

## 2.2 Users

| User | UID | Groups | Shell | Purpose |
|---|---|---|---|---|
| `devuser` | 1000 | `devuser`, `sudo` | `/bin/bash` | Learner account — has sudo, performs all observations and mutations |
| `appuser` | 1001 | `appuser` | `/usr/sbin/nologin` | Service account — owns and runs the Go service, no interactive login |

**`devuser` sudo scope:** full sudo via `/etc/sudoers.d/devuser`. The learner MAY use `sudo` for any system operation. This is intentional — the environment teaches system observation and diagnosis, not privilege restriction of the learner.

**`appuser` restrictions:** no login shell, no sudo, no home directory with sensitive material, no membership in any group beyond its own.

## 2.3 Filesystem Layout

| Path | Owner | Mode | Purpose |
|---|---|---|---|
| `/opt/app/server` | `appuser:appuser` | `750` | Compiled Go binary |
| `/etc/app/` | `appuser:appuser` | `755` | Config directory |
| `/etc/app/config.yaml` | `appuser:appuser` | `640` | App configuration |
| `/var/log/app/` | `appuser:appuser` | `755` | Log directory |
| `/var/log/app/app.log` | `appuser:appuser` | `640` | Structured JSON log |
| `/var/lib/app/` | `appuser:appuser` | `755` | Runtime state directory |
| `/var/lib/app/state` | `appuser:appuser` | `644` | Runtime state file — touched on every `/` request |
| `/etc/systemd/system/app.service` | `root:root` | `644` | systemd unit file |
| `/etc/nginx/sites-enabled/app` | `root:root` | `644` | nginx proxy config |
| `/etc/nginx/tls/app.local.crt` | `root:root` | `644` | Self-signed TLS certificate |
| `/etc/nginx/tls/app.local.key` | `root:root` | `640` | TLS private key |
| `/etc/logrotate.d/app` | `root:root` | `644` | Log rotation config |

All paths MUST exist with exactly the ownership and mode specified. Deviation is non-conformant.

## 2.4 Baseline Service State (Known-Good)

The following MUST be true in a conformant environment:

- `systemctl is-active app` → `active`
- `systemctl is-enabled app` → `enabled`
- `systemctl is-active nginx` → `active`
- `systemctl is-enabled nginx` → `enabled`
- `curl -sf http://localhost/health` → HTTP 200
- `curl -sf http://localhost/` → HTTP 200
- `ss -ltnp | grep '127.0.0.1:8080'` → present (app listening)
- `ss -ltnp | grep '0.0.0.0:80'` → present (nginx listening)
- `ss -ltnp | grep '0.0.0.0:443'` → present (nginx TLS listening)
- `/var/log/app/app.log` → exists, is valid newline-delimited JSON, contains at least one startup log entry

## 2.5 Observability Split

The environment has two distinct log sources. Using the wrong source for a given failure class produces incomplete or misleading information.

| Symptom class | Authoritative source | Tool |
|---|---|---|
| Service fails to start | systemd journal | `journalctl -u app.service` |
| Service exits unexpectedly | systemd journal | `journalctl -u app.service` |
| Unit misconfiguration | systemd journal | `journalctl -u app.service` |
| Restart loop | systemd journal | `journalctl -u app.service -f` |
| Request failure (4xx, 5xx) | app log | `tail -f /var/log/app/app.log` |
| Runtime dependency error | app log | `tail -f /var/log/app/app.log` |
| Missing config at runtime | app log | `tail -f /var/log/app/app.log` |
| nginx proxy failure | nginx error log | `journalctl -u nginx` |
| Full picture | both | both sources together |

**The observability split is a pedagogical property, not a convenience.** Many problems are designed so that exactly one source shows the relevant signal and the other shows nothing. Using both by default bypasses the diagnostic discipline the problems are designed to build.

## 2.6 Source of Truth by Layer

| Layer | Authoritative source |
|---|---|
| Desired service state | Unit file + `systemctl is-enabled` |
| Runtime process state | Process table (`ps`, `pgrep`) |
| App configuration | `/etc/app/config.yaml` |
| Proxy configuration | `/etc/nginx/sites-enabled/app` |
| App behavior logs | `/var/log/app/app.log` |
| Service lifecycle logs | `journalctl -u app.service` |
| Network exposure | `ss -ltnp` |
| File access rules | Ownership + mode bits (`ls -la`, `stat`) |
| TLS certificate | `openssl x509 -noout -text -in /etc/nginx/tls/app.local.crt` |

---

# §3 — Go Service Interface Contract

The Go service is a single HTTP server process. This section defines its contract — what it requires, what it provides, and how it fails. The companion repository (`lab-env/service/`) contains the implementation. The implementation MUST conform to every requirement in this section.

## 3.1 Startup Contract

**Preconditions (REQUIRED at startup):**

1. `/etc/app/config.yaml` MUST exist and be readable by the process UID.
2. `/etc/app/config.yaml` MUST be valid YAML conforming to the schema in §4.2.
3. Environment variable `APP_ENV` MUST be set and non-empty.
4. `/var/lib/app/` MUST exist and be writable by the process UID.

**On precondition failure:**

- Missing or unreadable config: the process MUST log a structured error to stdout (captured by journald), MUST exit with code 1, MUST NOT attempt to bind.
- Invalid config YAML: same as above.
- Missing `APP_ENV`: same as above.
- Unwritable `/var/lib/app/`: the process MAY start successfully. The failure is deferred to the first `/` request.

**Startup log (REQUIRED):**

The process MUST emit the following structured log line to stdout on successful startup:

```json
{"ts":"<RFC3339>","level":"info","msg":"server started","addr":"127.0.0.1:8080","app_env":"<APP_ENV value>","config":"/etc/app/config.yaml"}
```

## 3.2 Process Model

- The service MUST bind `127.0.0.1:8080` (loopback only — not accessible from outside the host without going through nginx).
- The service MUST run as the user specified in the systemd unit (`appuser`).
- The service MUST be a single process (no forking, no child processes).
- Default server timeouts:
  - Read timeout: 10 seconds
  - Write timeout: 30 seconds
  - Idle timeout: 60 seconds
- Maximum header size: 1MB

## 3.3 Endpoint Contracts

### `GET /health`

**Purpose:** liveness check. Confirms the process is alive and its configuration is loaded.

**Does NOT:** perform I/O to `/var/lib/app/state`. Does NOT validate runtime dependencies. A 200 from `/health` while `/` returns 500 is a designed and expected state.

**Response (success):**
- Status: 200
- Content-Type: `application/json`
- Body:
```json
{"status":"ok","app_env":"<APP_ENV value>","config_loaded":true}
```

**Response (failure):** this endpoint MUST NOT return non-200 in normal operation. If the process is running and the config is loaded, this endpoint returns 200.

---

### `GET /`

**Purpose:** primary request handler. Performs request-path work including writing a timestamp to `/var/lib/app/state`.

**Behavior:**
1. Attempt to open `/var/lib/app/state` for writing (O_WRONLY|O_CREATE|O_TRUNC).
2. Write the current UTC timestamp as a single line.
3. Close the file.
4. Return 200 on success.

**On state file failure:**
- Log a structured error to `app.log` with `"level":"error"` and `"msg":"state write failed"`.
- Return HTTP 500.
- DO NOT crash — the process continues serving `/health` and other endpoints.

**Response (success):**
- Status: 200
- Content-Type: `application/json`
- Body:
```json
{"status":"ok","path":"/"}
```

**Response (failure):**
- Status: 500
- Content-Type: `application/json`
- Body:
```json
{"status":"error","msg":"state write failed"}
```

---

### `GET /slow`

**Purpose:** controlled latency endpoint for timeout and proxy behavior problems.

**Behavior:**
- Sleep for `SLOW_DELAY_SECONDS` seconds (default: 5; configurable via environment variable).
- The sleep MUST occur before writing response headers — nginx will time out before receiving any response bytes.
- After the sleep, return 200.

**Response (if not timed out by proxy):**
- Status: 200
- Body: `{"status":"ok","path":"/slow","delay_seconds":<N>}`

---

### `GET /reset`

**Purpose:** abrupt connection reset for TCP behavior problems.

**Behavior:**
- Set `SO_LINGER` with `l_onoff=1, l_linger=0` on the connection socket.
- Close the connection immediately.
- Result: the client receives a TCP RST (connection reset), not a FIN (clean close).
- NO HTTP response is sent.

---

### `GET /headers`

**Purpose:** header visibility for proxy and request-path problems.

**Response:**
- Status: 200
- Content-Type: `application/json`
- Body: a JSON object containing the following headers from the incoming request, with their exact values as received by the app (after nginx forwarding):
  - `Host`
  - `X-Forwarded-For`
  - `X-Forwarded-Proto`
  - `X-Real-IP`
  - `User-Agent`

```json
{
  "Host": "<value or empty string if absent>",
  "X-Forwarded-For": "<value or empty string if absent>",
  "X-Forwarded-Proto": "<value or empty string if absent>",
  "X-Real-IP": "<value or empty string if absent>",
  "User-Agent": "<value or empty string if absent>"
}
```

## 3.4 Request Logging

Every request MUST produce exactly one log line in `/var/log/app/app.log` after the response is sent. Log lines MUST be newline-delimited JSON in this exact schema:

```json
{
  "ts": "<RFC3339 timestamp>",
  "level": "info",
  "msg": "request complete",
  "path": "<request path>",
  "method": "<HTTP method>",
  "status": <HTTP status code as integer>,
  "duration_ms": <response duration in milliseconds as integer>,
  "app_env": "<APP_ENV value>"
}
```

Error log lines use `"level":"error"` and include an `"error"` field with the error string.

Logs MUST be written to the file at `/var/log/app/app.log` directly (not to stdout). The logger MUST be unbuffered — each log line MUST be written immediately, not held in a userspace buffer.

## 3.5 Signal Handling

**SIGTERM:** the process MUST handle SIGTERM gracefully.
1. Stop accepting new connections.
2. Wait for in-flight requests to complete (drain), up to a 10-second grace period.
3. Log `{"ts":"...","level":"info","msg":"server shutdown","reason":"SIGTERM"}` to stdout.
4. Exit with code 0.

**SIGKILL:** the process CANNOT catch SIGKILL. This is handled by the OS.

**Fault F-008** (see §7) disables SIGTERM handling — the process ignores SIGTERM and requires SIGKILL.

## 3.6 Log File Behavior

The service opens `/var/log/app/app.log` with `O_WRONLY|O_CREATE|O_APPEND` at startup. It does NOT reopen the file on SIGHUP (log rotation is handled by logrotate's copytruncate directive — see §4.5).

If the log file is not writable at startup, the process MUST log an error to stdout and exit with code 1.

---

# §4 — Canonical Artifact Contents

These are the exact contents of configuration and operational files. Any deviation is non-conformant.

## 4.1 systemd Unit File

**Path:** `/etc/systemd/system/app.service`

```ini
[Unit]
Description=Lab Go HTTP Service
After=network.target
Wants=network.target

[Service]
Type=simple
User=appuser
Group=appuser
WorkingDirectory=/opt/app
ExecStart=/opt/app/server
Environment=APP_ENV=prod
Restart=on-failure
RestartSec=2s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=app

# Resource limits
LimitNOFILE=65536

# Security hardening (intentionally minimal — not the subject of study)
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

**Pedagogical properties of this unit:**
- `Restart=on-failure` + `RestartSec=2s` produces an observable restart loop when the app exits non-zero.
- `StandardOutput=journal` routes stdout (startup logs, shutdown logs) to journald — not to `app.log`.
- `APP_ENV=prod` is the required environment variable — removing it produces a startup failure.
- The absence of `TimeoutStopSec` means systemd uses the default 90-second SIGTERM→SIGKILL sequence.

## 4.2 App Configuration Schema

**Path:** `/etc/app/config.yaml`

**Canonical contents (baseline):**

```yaml
# Lab app configuration
# Version: 1.0

server:
  addr: "127.0.0.1:8080"
  read_timeout_seconds: 10
  write_timeout_seconds: 30
  idle_timeout_seconds: 60

log:
  path: "/var/log/app/app.log"

state:
  path: "/var/lib/app/state"
```

**Required fields:** `server.addr`, `log.path`, `state.path`. Missing any required field causes startup failure (exit code 1).

**`server.addr` is a fault injection surface** (Fault F-002): changing it to `127.0.0.1:9090` causes the app to bind a different port, making nginx's upstream unreachable and producing 502.

## 4.3 nginx Configuration

**Path:** `/etc/nginx/sites-enabled/app`

```nginx
# Upstream definition
upstream app_backend {
    server 127.0.0.1:8080;
    keepalive 32;
}

# HTTP server
server {
    listen 0.0.0.0:80;
    server_name _;

    # Proxy timeouts — deliberately shorter than app's /slow delay
    proxy_connect_timeout 2s;
    proxy_read_timeout 3s;
    proxy_send_timeout 10s;

    # Forwarded headers
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # Identifying header on all responses
    add_header X-Proxy nginx always;

    location / {
        proxy_pass http://app_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
}

# HTTPS server
server {
    listen 0.0.0.0:443 ssl;
    server_name app.local;

    ssl_certificate     /etc/nginx/tls/app.local.crt;
    ssl_certificate_key /etc/nginx/tls/app.local.key;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    proxy_connect_timeout 2s;
    proxy_read_timeout 3s;
    proxy_send_timeout 10s;

    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;

    add_header X-Proxy nginx always;

    location / {
        proxy_pass http://app_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
}
```

**Pedagogical properties:**
- `proxy_read_timeout 3s` is deliberately shorter than `/slow`'s default 5-second delay — produces a deterministic 504 on `/slow`.
- `proxy_connect_timeout 2s` produces 502 when the upstream port is not listening.
- `X-Proxy: nginx` allows learners to distinguish nginx responses from app responses.
- The TLS server uses a self-signed cert not in the system trust store — `curl https://app.local` fails without `-k` or explicit CA, which is the designed behavior for TLS problems.

## 4.4 TLS Certificate Generation

The self-signed certificate MUST be generated during provisioning using exactly this command:

```bash
openssl req -x509 -newkey rsa:2048 -keyout /etc/nginx/tls/app.local.key \
    -out /etc/nginx/tls/app.local.crt \
    -days 365 -nodes \
    -subj "/CN=app.local" \
    -addext "subjectAltName=DNS:app.local"
```

The certificate:
- MUST have CN: `app.local`
- MUST have SAN: `DNS:app.local`
- MUST NOT be in the system trust store at baseline (learners install it explicitly when required by a problem)
- MUST be valid for 365 days from provisioning date

**`/etc/hosts` entry (required):**

```
127.0.0.1 app.local
```

This line MUST be present in `/etc/hosts`. DNS resolution of `app.local` MUST resolve to `127.0.0.1` via this mechanism, not via any external DNS.

## 4.5 Logrotate Configuration

**Path:** `/etc/logrotate.d/app`

```
/var/log/app/app.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
    create 0640 appuser appuser
}
```

**`copytruncate` is required** because the app opens the log file at startup and holds the file descriptor open. Standard logrotate (rename + create) would cause the app to continue writing to the renamed file. `copytruncate` copies the log and truncates the original in place, keeping the app's file descriptor valid without requiring a restart or SIGHUP.

## 4.6 `/etc/sudoers.d/devuser`

```
devuser ALL=(ALL:ALL) ALL
```

---

# §5 — Provisioning Contract

## 5.1 Requirements

The bootstrap script (`lab-env/bootstrap.sh`) MUST:

1. Be idempotent — running it twice on the same system MUST produce the same result as running it once.
2. Run without interactive prompts.
3. Complete successfully on a fresh Ubuntu 22.04 LTS installation with internet access.
4. Leave the environment in conformant state (all conformance suite checks pass) upon completion.
5. Print a summary of actions taken.
6. Exit non-zero if any step fails.

## 5.2 Package Dependencies

The following packages MUST be installed:

```
nginx
curl
jq
net-tools
iproute2
tcpdump
openssl
dnsutils
netcat-openbsd
strace
lsof
htop
vim
git
golang-go
```

The Go toolchain MUST be present for binary compilation during provisioning. The learner does NOT need to compile the service — the bootstrap script compiles and installs the binary.

## 5.3 Bootstrap Sequence

The bootstrap MUST execute in this order:

1. Update package index
2. Install package dependencies
3. Create `appuser` (UID 1001, no shell, no home)
4. Create `devuser` (UID 1000, sudo)
5. Create directory structure with correct ownership and modes
6. Copy and render configuration files from `lab-env/config/`
7. Compile Go service (`cd lab-env/service && go build -o /opt/app/server .`)
8. Set binary ownership and permissions
9. Generate TLS certificate
10. Install nginx configuration
11. Add `app.local` to `/etc/hosts`
12. Install logrotate configuration
13. Install sudoers entry
14. Enable and start `app.service`
15. Enable and start `nginx`
16. Run conformance suite — exit non-zero if any check fails

## 5.4 Idempotency Contract

Re-running the bootstrap script on a conformant environment MUST:
- Recompile and reinstall the binary (safe — service is restarted after)
- Regenerate TLS certificate only if it is missing or expired
- Restore any configuration file that differs from canonical contents
- Restart services only if configuration changed
- NOT delete or modify `app.log` contents
- NOT delete `/var/lib/app/state`
- Pass the conformance suite upon completion

---

# §6 — Conformance Suite

The conformance suite is the executable expression of this specification. A conformant environment MUST pass all checks. The suite is at `lab-env/validate.sh`.

## 6.1 Conformance Checks

### System state checks

```bash
# S-001: app service is active
systemctl is-active app.service --quiet
# S-002: app service is enabled
systemctl is-enabled app.service --quiet
# S-003: nginx service is active
systemctl is-active nginx --quiet
# S-004: nginx service is enabled
systemctl is-enabled nginx --quiet
```

### Process checks

```bash
# P-001: app process is running as appuser
pgrep -u appuser server > /dev/null
# P-002: app is listening on 127.0.0.1:8080
ss -ltnp | grep -q '127.0.0.1:8080'
# P-003: nginx is listening on 0.0.0.0:80
ss -ltnp | grep -q '0.0.0.0:80'
# P-004: nginx is listening on 0.0.0.0:443
ss -ltnp | grep -q '0.0.0.0:443'
```

### Endpoint checks

```bash
# E-001: /health returns 200
curl -sf http://localhost/health > /dev/null
# E-002: / returns 200
curl -sf http://localhost/ > /dev/null
# E-003: /health body contains status:ok
curl -s http://localhost/health | jq -e '.status == "ok"' > /dev/null
# E-004: X-Proxy header is present (nginx is proxying)
curl -sI http://localhost/ | grep -q 'X-Proxy: nginx'
# E-005: HTTPS endpoint responds (self-signed cert, skip verify)
curl -skf https://app.local/health > /dev/null
```

### Filesystem checks

```bash
# F-001: binary exists with correct mode
test -x /opt/app/server
stat -c '%U:%G %a' /opt/app/server | grep -q 'appuser:appuser 750'
# F-002: config exists with correct mode
stat -c '%U:%G %a' /etc/app/config.yaml | grep -q 'appuser:appuser 640'
# F-003: log directory exists with correct mode
stat -c '%U:%G %a' /var/log/app | grep -q 'appuser:appuser 755'
# F-004: state directory exists with correct mode
stat -c '%U:%G %a' /var/lib/app | grep -q 'appuser:appuser 755'
# F-005: nginx config is valid
nginx -t 2>/dev/null
# F-006: TLS cert exists and is not expired
openssl x509 -checkend 0 -noout -in /etc/nginx/tls/app.local.crt
# F-007: app.local resolves to 127.0.0.1
getent hosts app.local | grep -q '127.0.0.1'
```

### Log checks

```bash
# L-001: app.log exists and is non-empty
test -s /var/log/app/app.log
# L-002: app.log contains valid JSON (last line)
tail -1 /var/log/app/app.log | jq . > /dev/null 2>&1
# L-003: app.log contains a startup entry
grep -q '"msg":"server started"' /var/log/app/app.log
```

## 6.2 Conformance Suite Output Format

The suite MUST output one line per check:

```
[PASS] S-001: app service is active
[PASS] S-002: app service is enabled
[FAIL] E-001: /health returns 200
...
CONFORMANCE RESULT: 20/23 checks passed. Environment is NON-CONFORMANT.
```

**Exit codes:** exit 0 if all blocking-severity checks pass (degraded-severity check failures — F-006, L-001, L-002, L-003 — do not affect the exit code and produce a degraded-conformant classification). Exit 1 if any blocking-severity check fails. Authoritative severity classifications: `conformance-model.md` §3.1. Degraded-conformant semantics: `conformance-model.md` §4.3.

---

# §7 — Fault Injection Catalog

Faults are the operational backbone of the incident lab problems. Each fault is a deterministic, documented mutation of the environment that produces a specific, observable failure while leaving all other layers intact.

## 7.1 Fault Entry Schema

| Field | Description |
|---|---|
| **ID** | Unique fault identifier (F-NNN) |
| **Layer** | Which layer is mutated: `filesystem`, `permissions`, `process`, `service`, `socket`, `proxy`, `config`, `network` |
| **Domain** | Which problem set uses this fault: `linux`, `networking`, `security`, `os`, `multi` |
| **Mutation** | The exact change made to produce the fault |
| **Symptom** | What the learner observes |
| **Authoritative signal** | The source of truth that confirms the fault |
| **Observable** | The specific tool and output that shows the fault |
| **Reset tier** | R1, R2, or R3 (see §8) |
| **Reset action** | The exact command that restores the canonical state |

## 7.2 Fault Catalog

---

**F-001 — Missing configuration file**

| Field | Value |
|---|---|
| Layer | `filesystem` |
| Domain | `linux`, `os` |
| Mutation | `sudo rm /etc/app/config.yaml` |
| Symptom | Service enters restart loop; `curl localhost/health` fails (connection refused) |
| Authoritative signal | journald |
| Observable | `journalctl -u app.service -n 20` shows repeated start failures with config error |
| Reset tier | R2 |
| Reset action | `sudo cp lab-env/config/config.yaml /etc/app/config.yaml && sudo chown appuser:appuser /etc/app/config.yaml && sudo chmod 640 /etc/app/config.yaml && sudo systemctl restart app` |

---

**F-002 — Wrong bind port in config**

| Field | Value |
|---|---|
| Layer | `config`, `socket` |
| Domain | `linux`, `networking` |
| Mutation | Change `server.addr` in `/etc/app/config.yaml` from `127.0.0.1:8080` to `127.0.0.1:9090`, then `sudo systemctl restart app` |
| Symptom | `curl localhost/` returns 502; app process is running and healthy from its own perspective |
| Authoritative signal | `ss -ltnp` (shows 9090, not 8080) + nginx error log |
| Observable | `ss -ltnp | grep 9090` shows app on wrong port; `curl -I localhost` shows `502 Bad Gateway` with `X-Proxy: nginx` |
| Reset tier | R2 |
| Reset action | Restore canonical config.yaml, restart app |

---

**F-003 — Config file unreadable by appuser**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux`, `security` |
| Mutation | `sudo chmod 000 /etc/app/config.yaml` |
| Symptom | Service enters restart loop; permission denied reading config |
| Authoritative signal | journald |
| Observable | `journalctl -u app.service -n 10` shows permission denied error; `ls -la /etc/app/config.yaml` shows 000 |
| Reset tier | R2 |
| Reset action | `sudo chmod 640 /etc/app/config.yaml && sudo systemctl restart app` |

---

**F-004 — State directory unwritable by appuser**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux`, `os` |
| Mutation | `sudo chmod 000 /var/lib/app/` |
| Symptom | `curl localhost/health` → 200 (intentionally); `curl localhost/` → 500 |
| Authoritative signal | app.log |
| Observable | `tail -5 /var/log/app/app.log` shows `"level":"error","msg":"state write failed"`; `/health` continues returning 200 |
| Reset tier | R2 |
| Reset action | `sudo chmod 755 /var/lib/app/` |

---

**F-005 — Binary not executable**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux` |
| Mutation | `sudo chmod 640 /opt/app/server` |
| Symptom | Service fails to start; `systemctl status app` shows start failure |
| Authoritative signal | journald |
| Observable | `journalctl -u app.service -n 5` shows exec failure; `ls -la /opt/app/server` shows 640 |
| Reset tier | R2 |
| Reset action | `sudo chmod 750 /opt/app/server && sudo systemctl restart app` |

---

**F-006 — APP\_ENV environment variable removed from unit**

| Field | Value |
|---|---|
| Layer | `service` |
| Domain | `linux`, `os` |
| Mutation | Remove or comment out `Environment=APP_ENV=prod` from the unit file, then `sudo systemctl daemon-reload && sudo systemctl restart app` |
| Symptom | Service fails to start with "missing APP\_ENV" error |
| Authoritative signal | journald |
| Observable | `journalctl -u app.service -n 10` shows the missing env var error |
| Reset tier | R2 |
| Reset action | Restore canonical unit file, `sudo systemctl daemon-reload && sudo systemctl restart app` |

---

**F-007 — nginx pointing to wrong upstream port**

| Field | Value |
|---|---|
| Layer | `proxy`, `config` |
| Domain | `linux`, `networking` |
| Mutation | Change `server 127.0.0.1:8080` to `server 127.0.0.1:9090` in `/etc/nginx/sites-enabled/app`, then `sudo nginx -s reload` |
| Symptom | `curl localhost/` returns 502; app is running correctly on 8080 |
| Authoritative signal | nginx error log + `ss -ltnp` |
| Observable | `ss -ltnp | grep 8080` shows app running; `curl -I localhost` returns 502; nginx error log shows connection refused on 9090 |
| Reset tier | R2 |
| Reset action | Restore canonical nginx config, `sudo nginx -s reload` |

---

**F-008 — SIGTERM ignored (unclean shutdown)**

| Field | Value |
|---|---|
| Layer | `process` |
| Domain | `linux`, `os` |
| Mutation | Rebuild app with SIGTERM handler disabled (fault flag in `lab-env/service/faults.go`: `FAULT_IGNORE_SIGTERM=true`), redeploy |
| Symptom | `sudo systemctl stop app` hangs for 90 seconds before SIGKILL |
| Authoritative signal | `systemctl status app` showing `stop-sigterm → stop-sigkill` transition |
| Observable | `systemctl stop app` does not return promptly; after 90s, journald shows `Killed` |
| Reset tier | R3 |
| Reset action | Rebuild without fault flag, redeploy |

---

**F-009 — Log file unwritable**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux`, `os` |
| Mutation | `sudo chmod 000 /var/log/app/app.log` |
| Symptom | Service fails to start (cannot open log file); journald shows error |
| Authoritative signal | journald |
| Observable | `journalctl -u app.service -n 5` shows log open failure |
| Reset tier | R2 |
| Reset action | `sudo chmod 640 /var/log/app/app.log && sudo chown appuser:appuser /var/log/app/app.log && sudo systemctl restart app` |

---

**F-010 — Log file deleted while running**

| Field | Value |
|---|---|
| Layer | `filesystem` |
| Domain | `linux`, `os` |
| Mutation | `sudo rm /var/log/app/app.log` while the service is running |
| Symptom | Service continues running; new requests return 200; `app.log` does not exist on disk; disk space from old log is not freed until restart |
| Authoritative signal | `lsof -p $(pgrep server) | grep app.log` |
| Observable | `ls /var/log/app/` shows no app.log; `lsof +L1` shows the deleted file still held open by the app process; disk space not reclaimed |
| Reset tier | R1 |
| Reset action | `sudo systemctl restart app` — recreates the log file on startup |

---

**F-011 — nginx proxy\_read\_timeout shorter than app processing (504)**

| Field | Value |
|---|---|
| Layer | `proxy`, `network` |
| Domain | `networking` |
| Mutation | None — this is baseline behavior. `GET /slow` naturally exceeds nginx's 3-second `proxy_read_timeout`. |
| Symptom | `curl http://localhost/slow` returns 504 after approximately 3 seconds |
| Authoritative signal | curl response code + timing |
| Observable | `curl -v http://localhost/slow` shows `504 Gateway Time-out` with `X-Proxy: nginx`; `time curl http://localhost/slow` shows ~3 second duration |
| Reset tier | N/A — this is intended baseline behavior, not a fault |
| Reset action | N/A |

---

**F-012 — TLS certificate not in trust store**

| Field | Value |
|---|---|
| Layer | `network` |
| Domain | `networking`, `security` |
| Mutation | None — this is baseline behavior. |
| Symptom | `curl https://app.local/health` fails with TLS error |
| Authoritative signal | curl TLS error output |
| Observable | `curl -v https://app.local/health` shows `SSL certificate problem: self-signed certificate`; `curl -sk https://app.local/health` succeeds (skip verify) |
| Reset tier | N/A — intended baseline behavior |
| Reset action | Problem-specific: `sudo cp /etc/nginx/tls/app.local.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates` to install; reverse to uninstall |

---

**F-013 — Service enabled but unit file broken (startup failure on boot)**

| Field | Value |
|---|---|
| Layer | `service` |
| Domain | `linux` |
| Mutation | Add a syntax error to the unit file (`ExecStart=/opt/app/DOESNOTEXIST`), `sudo systemctl daemon-reload` |
| Symptom | `systemctl is-enabled app` → `enabled`; `systemctl is-active app` → `failed`; service will not start |
| Authoritative signal | journald + systemctl status |
| Observable | `systemctl status app` shows failed state and the exec error; `journalctl -u app.service -n 10` shows the exec failure |
| Reset tier | R2 |
| Reset action | Restore canonical unit file, `sudo systemctl daemon-reload && sudo systemctl start app` |

---

**F-014 — Zombie process accumulation**

| Field | Value |
|---|---|
| Layer | `process` |
| Domain | `linux`, `os` |
| Mutation | Rebuild app with fault flag `FAULT_ZOMBIE_CHILDREN=true` — app forks child processes and does not call `wait()`. |
| Symptom | `ps aux` shows growing count of `Z` state (zombie) processes parented to the app |
| Authoritative signal | `ps -eo pid,ppid,stat,comm | grep Z` |
| Observable | Zombie count increases with each `/` request; `pstree -p $(pgrep server)` shows zombie children |
| Reset tier | R3 |
| Reset action | Rebuild without fault flag, redeploy |

---

**F-015 — nginx configuration syntax error (nginx fails to reload)**

| Field | Value |
|---|---|
| Layer | `proxy` |
| Domain | `linux`, `networking` |
| Mutation | Add an invalid directive to `/etc/nginx/sites-enabled/app`, then attempt `sudo nginx -s reload` |
| Symptom | nginx reload fails; existing nginx worker processes continue serving with the old config; new config is not applied |
| Authoritative signal | nginx -t output |
| Observable | `sudo nginx -t` shows configuration error; `sudo nginx -s reload` returns error; the old config continues to work |
| Reset tier | R2 |
| Reset action | Restore canonical nginx config, `sudo nginx -s reload` |

---

**F-016 — App binding on all interfaces instead of loopback**

| Field | Value |
|---|---|
| Layer | `socket`, `security` |
| Domain | `linux`, `networking`, `security` |
| Mutation | Change `server.addr` in config.yaml from `127.0.0.1:8080` to `0.0.0.0:8080`, restart app |
| Symptom | App is accessible directly on port 8080 from external hosts, bypassing nginx and all proxy-level controls |
| Authoritative signal | `ss -ltnp` |
| Observable | `ss -ltnp | grep 8080` shows `0.0.0.0:8080` instead of `127.0.0.1:8080`; `curl http://<external-ip>:8080/health` succeeds (bypassing nginx) |
| Reset tier | R2 |
| Reset action | Restore canonical config.yaml, restart app |

---

**F-017 — Missing APP\_ENV at runtime (different from F-006)**

| Field | Value |
|---|---|
| Layer | `service` |
| Domain | `linux`, `os` |
| Mutation | `sudo systemctl set-environment APP_ENV=` (set to empty), `sudo systemctl restart app` |
| Symptom | Service fails to start due to empty APP\_ENV; journald shows the specific validation error |
| Authoritative signal | journald |
| Observable | `journalctl -u app.service -n 5` shows empty APP\_ENV error |
| Reset tier | R2 |
| Reset action | `sudo systemctl unset-environment APP_ENV && sudo systemctl restart app` (unit file Environment directive takes effect again) |

---

**F-018 — Disk full (inode exhaustion variant)**

| Field | Value |
|---|---|
| Layer | `filesystem` |
| Domain | `linux`, `os` |
| Mutation | `for i in $(seq 1 100000); do sudo touch /var/lib/app/file_$i; done` — creates 100,000 small files |
| Symptom | Filesystem reports inode exhaustion; new files cannot be created despite available disk blocks |
| Authoritative signal | `df -i` |
| Observable | `df -i /var/lib/app` shows 100% inode usage; `touch /var/lib/app/test` fails with "No space left on device" despite `df -h` showing available blocks |
| Reset tier | R2 |
| Reset action | `sudo rm /var/lib/app/file_*` |

---

# §8 — State Control

## 8.1 Reset Tiers

**R1 — Service restart:**
Restores runtime state without modifying any files. Use when the fault is in process state only.

```bash
sudo systemctl restart app
sudo systemctl restart nginx    # only if nginx was mutated
```

**R2 — Configuration restore:**
Restores canonical file contents and restarts affected services. Use for permission, configuration, and filesystem faults.

```bash
# Restore all canonical config files:
sudo lab-env/reset.sh --config
# Then restart affected services:
sudo systemctl daemon-reload
sudo systemctl restart app
sudo nginx -s reload
```

**R3 — Full reprovision:**
Re-runs the bootstrap script. Use when the binary has been replaced, the service account modified, or the environment has drifted beyond R2 recovery.

```bash
sudo lab-env/bootstrap.sh
```

**R4 — Snapshot rollback:**
If the VM has a baseline snapshot, roll back to it. This is the most complete reset — every change, including those not tracked by R1–R3, is reversed. This SHOULD be done before each new Phase E incident lab problem.

## 8.2 Reset Script Contract

`lab-env/reset.sh` MUST accept the following flags:

| Flag | Action |
|---|---|
| `--config` | Restore all canonical config files to spec-defined contents |
| `--permissions` | Restore all canonical ownership and mode bits |
| `--logs` | Truncate `app.log` to empty (preserves file with correct ownership) |
| `--state` | Remove and recreate `/var/lib/app/state` |
| `--full` | All of the above, then `systemctl daemon-reload`, restart app, reload nginx |
| `--fault <ID>` | Apply the named fault from the catalog |
| `--unfault <ID>` | Reverse the named fault (equivalent to appropriate tier reset for that fault) |

After any reset, the conformance suite MUST be run to verify the environment is conformant.

## 8.3 Snapshot Strategy

A baseline snapshot MUST be taken after successful provisioning and before any learner interaction. Snapshot naming convention:

```
lab-env-baseline-v1.0.0-<YYYY-MM-DD>
```

Additional snapshots SHOULD be taken at the end of each phase:

```
lab-env-post-phase-A-<YYYY-MM-DD>
lab-env-post-phase-B-<YYYY-MM-DD>
...
```

The baseline snapshot is the authoritative rollback target. Phase snapshots enable resuming from a specific point without replaying all prior problems.

## 8.4 Environment Versioning

**Spec version:** defined in this document's title (`v1.0.0`).

**Environment manifest:** `lab-env/MANIFEST` contains SHA256 checksums of all canonical files:

```
sha256sum /opt/app/server > lab-env/MANIFEST
sha256sum /etc/app/config.yaml >> lab-env/MANIFEST
sha256sum /etc/systemd/system/app.service >> lab-env/MANIFEST
sha256sum /etc/nginx/sites-enabled/app >> lab-env/MANIFEST
sha256sum /etc/nginx/tls/app.local.crt >> lab-env/MANIFEST
```

**Drift detection:**

```bash
# Check for drift from canonical state:
lab-env/validate.sh --manifest-check
```

Any difference between the running environment and the MANIFEST is logged as drift. Drift from non-fault causes is non-conformant.

---

# §9 — Security Boundaries

## 9.1 Learner Permissions

`devuser` has full sudo access. This is intentional. The security boundary in this environment is not between the learner and the system — it is between `appuser` and the system.

| Action | devuser | appuser |
|---|---|---|
| Read canonical config files | Yes (via sudo) | Yes (file owner) |
| Modify canonical config files | Yes (via sudo) | No (would require own write permission, which is not the intent) |
| Restart services | Yes (via sudo systemctl) | No |
| Read journald logs | Yes | No |
| Read app.log | Yes (via sudo or group) | Yes (file owner) |
| Execute the binary | No (mode 750, appuser only) | Yes |
| Write to `/var/lib/app/` | Yes (via sudo) | Yes (directory owner) |
| Edit unit files | Yes (via sudo) | No |

## 9.2 AppArmor

Ubuntu 22.04 ships with AppArmor enabled. The lab environment does NOT define an AppArmor profile for the app service. AppArmor is not in scope for the practice problems. If AppArmor interferes with lab behavior, it SHOULD be set to complain mode for the app:

```bash
sudo aa-complain /opt/app/server 2>/dev/null || true
```

The bootstrap script performs this step if AppArmor is active.

## 9.3 Trust Model

The environment models a single-machine production system where:

- `appuser` is the service identity with least privilege.
- `devuser` is the operator identity with administrative access.
- `root` is the system identity (accessed via sudo from devuser).
- `nginx` runs as `www-data` (Ubuntu default).

Security practice problems use this model to demonstrate trust boundary failures — the service account (`appuser`) is the subject of least-privilege analysis.

---

# §10 — Networking Extensions

These extensions are active in the baseline environment and are used specifically by the Networking and Security problem sets.

## 10.1 Additional App Endpoints

See §3.3 for full endpoint contracts. Summary:

| Endpoint | Purpose | Networking problem |
|---|---|---|
| `GET /slow` | Exceeds nginx proxy_read_timeout → 504 | Proxy timeout behavior |
| `GET /reset` | TCP RST without HTTP response | TCP connection reset |
| `GET /headers` | Returns received headers | Proxy header propagation |

## 10.2 Tools Available in the Environment

The following tools MUST be installed and available to `devuser`:

| Layer | Tool | Purpose |
|---|---|---|
| Name resolution | `dig`, `getent`, `resolvectl` | DNS query and resolution |
| TCP | `ss`, `nc` (netcat-openbsd), `tcpdump` | Connection state, raw TCP |
| TLS | `openssl s_client` | TLS handshake inspection |
| HTTP | `curl -v`, `curl --resolve` | Full request/response inspection |
| Process | `strace`, `lsof`, `pgrep`, `ps` | Syscall and fd inspection |
| System | `vmstat`, `iostat`, `free`, `df`, `stat` | System resource state |

## 10.3 Authoritative Tools by Layer

| Layer | Authoritative tool | What it proves |
|---|---|---|
| Name | `getent hosts`, `dig @127.0.0.53`, `resolvectl` | What address a name resolves to, in which context |
| TCP | `ss -ltnp`, `nc`, `tcpdump` | Whether a TCP connection can be established |
| TLS | `openssl s_client -connect host:port` | Whether the handshake completes and which cert is presented |
| HTTP | `curl -v` | Whether an HTTP response was received and at which status |
| Response | app.log body | Whether the application processed the request correctly |

## 10.4 nginx Timeout Behavior Reference

| Scenario | nginx directive | nginx response | Upstream behavior |
|---|---|---|---|
| App not listening on 8080 | `proxy_connect_timeout 2s` | 502 Bad Gateway | Connection refused |
| App listening but not responding | `proxy_read_timeout 3s` | 504 Gateway Timeout | Request hung |
| App closes connection mid-response | — | 502 Bad Gateway | Upstream closed connection |
| App returns non-2xx | — | Passes through status | Normal proxy behavior |

---

# §11 — Non-Goals

The following are explicitly outside the scope of this environment. Adding any of these produces undefined behavior for the practice problems.

| Item | Rationale for exclusion |
|---|---|
| Container runtimes | Adds a virtualization layer that obscures the OS concepts under study |
| Kubernetes / orchestration | Multi-component complexity incompatible with single-chain failure model |
| External DNS | All name resolution MUST be local and deterministic |
| Authentication systems | Auth failures add a layer above the infrastructure layer under study |
| Multi-host networking | All networking is loopback + host-only — no external routing |
| Service mesh | Sidecar complexity obscures the direct TCP/HTTP behavior under study |
| CDN / external load balancing | External infrastructure cannot be made deterministic for fault injection |
| Databases | DB dependency would expand the failure surface beyond the curriculum scope |
| Distributed tracing | Requires external collector infrastructure |
| SELinux | Ubuntu ships AppArmor; mixing MAC systems is out of scope |
| Persistent message queues | No async processing in scope |
| Go runtime upgrades beyond 1.21 | Binary must be compiled against the spec'd Go version |

---

# §13 — Lab Control Plane

> **Instantiation note:** This section describes how the lab control plane is instantiated in this environment. Semantic authority for all state, transition, fault, and conformance definitions resides in the model documents. This section references those definitions and specifies the implementation target. It does not redefine any semantic model content.

The `lab` CLI is a Go binary compiled during provisioning and installed at `/usr/local/bin/lab`. It is the single interface for all environment lifecycle operations: provisioning, validation, fault injection, reset, and state inspection. No other tool or script SHOULD be used to mutate the canonical environment — all mutations MUST route through `lab` or the underlying scripts it calls.

**Authoritative specification documents:**

| Concern | Authoritative document |
|---|---|
| State definitions, invariants, transitions | `system-state-model.md` |
| Conformance check catalog, validation semantics | `conformance-model.md` |
| Fault definitions, mutation rules, fault catalog | `fault-model.md` |
| CLI command contracts, exit codes, schemas | `control-plane-contract.md` |

---

## 13.1 — State Machine (reference)

The environment occupies one of six states at all times. Authoritative definitions: `system-state-model.md` §2.

```
UNPROVISIONED ──provision──► PROVISIONED ──validate──► CONFORMANT
                                                            │    ▲
                                                    fault apply  reset
                                                            │    │
                                                            ▼    │
                                                         DEGRADED
                                                            │
                                                    (unexpected break)
                                                            │
                                                            ▼
                                                          BROKEN
                                                            │
                                                          reset
                                                            ▼
                                                        RECOVERING ──► CONFORMANT
```

States in brief (full invariants and transition rules: `system-state-model.md` §2–§3):

- **UNPROVISIONED** — no canonical files, users, or services present
- **PROVISIONED** — bootstrap completed, conformance not yet verified
- **CONFORMANT** — all blocking conformance checks pass; no active fault. Definition: `conformance-model.md` §3, `system-state-model.md` §2.3
- **DEGRADED** — exactly one catalog fault deliberately applied; active fault ID recorded
- **BROKEN** — one or more blocking checks fail; no active fault; recoverable
- **RECOVERING** — reset operation in progress (transitional)

Transition guards, forbidden transitions, and failure semantics: `system-state-model.md` §3.

State detection precedence and conflict resolution: `system-state-model.md` §4.

---

## 13.2 — Transition Model (reference)

Every `lab` command is a named transition or observation over the state machine. Transitions are synchronous — the binary does not return until the transition completes and the resulting state is recorded. Transition failure semantics: `system-state-model.md` §3.5.

**`lab status`** is the canonical reconciliation point — the only command authorized to reconcile observed runtime reality with recorded control-plane state. See `control-plane-contract.md` §4.1.

**`lab validate`** is an observation primitive — it records conformance observations but does NOT update the authoritative state classification. State reconciliation occurs only via `lab status`. See `control-plane-contract.md` §4.2.

Subsystems participating in transitions: systemd, nginx, filesystem, Go binary, conformance suite, state file. Detailed executor contract: `control-plane-contract.md` §5.

---

## 13.3 — Command Surface

Eight commands. Full behavioral contracts — preconditions, exit codes, state file effects, audit obligations — in `control-plane-contract.md` §4.

| Command | Type | Transition |
|---|---|---|
| `lab status` | reconciliation | none (reads + reconciles) |
| `lab validate` | observation | none (records observations only) |
| `lab fault list` | read-only | none |
| `lab fault info <ID>` | read-only | none |
| `lab fault apply <ID>` | mutating | CONFORMANT → DEGRADED |
| `lab reset [--tier R1\|R2\|R3]` | mutating | any → CONFORMANT |
| `lab provision` | mutating | UNPROVISIONED → CONFORMANT |
| `lab history` | read-only | none |

**Idempotency:** `status`, `validate`, `reset`, `provision`, `history`, `fault list`, `fault info` are idempotent. `fault apply` is not — stateful mutation; guard prevents double-apply.

---

## 13.4 — Fault Execution Model (reference)

Faults are typed operators. Full schema, mutation rules, reversibility semantics, and precondition/postcondition specifications: `fault-model.md` §2–§6.

Key instantiation properties for this environment:
- All mutations route through the executor — no direct system calls from fault functions
- Faults with `IsReversible: false` (F-008, F-014) require binary rebuild and `ResetTier: R3`
- `RequiresConfirmation: true` for F-008 and F-014; false for all others
- Standard precondition for all faults: state MUST be CONFORMANT

---

## 13.5 — Executor Contract (reference)

The executor is the single mutation layer. Full behavioral contract, audit obligations, and privilege model: `control-plane-contract.md` §5.

Key properties:
- Every executor operation produces an audit log entry at `/var/lib/lab/audit.log` before execution completes
- The audit log is append-only and survives all reset tiers including R3
- All privileged operations use `devuser`'s existing sudo grant — no credential storage

---

## 13.6 — State Memory Model (reference)

Control-plane memory at `/var/lib/lab/state.json`. Full schema, atomic write requirement, lock relationship, and corruption recovery: `control-plane-contract.md` §6.

Key properties:
- `state.json` ownership: `root:root`, mode `644`
- Atomic writes via temp file + rename (see §12.14 for the Go atomic write pattern)
- `active_fault` is non-null if and only if state is DEGRADED
- `classification_valid: false` when control-plane certainty is lost (interrupt recovery)
- History is a bounded ring buffer; see `control-plane-contract.md` §6.1 for the buffer limit

---

## 13.7 — Output Model (reference)

All commands produce output through a structured internal model rendered to human-readable or JSON format. Full output schemas: `control-plane-contract.md` §4 (per-command JSON schemas).

Key properties:
- `--json` flag: emit JSON to stdout; human output is default
- stdout carries command output only; stderr carries diagnostics only — never mixed
- JSON schemas are stable (version-controlled); human-readable formatting is implementation-defined

---

## 13.8 — Repository Layout

The companion repository (`lab-env/`) MUST conform to this layout. The `lab` binary is the primary artifact of `cmd/lab/`.

```
lab-env/
├── cmd/
│   └── lab/
│       ├── main.go              # cobra command tree, flag definitions
│       ├── status.go            # lab status command
│       ├── validate.go          # lab validate command
│       ├── fault.go             # lab fault {list,info,apply} commands
│       ├── reset.go             # lab reset command
│       ├── provision.go         # lab provision command
│       ├── history.go           # lab history command
│       ├── go.mod
│       ├── go.sum
│       └── internal/
│           ├── state/
│           │   ├── machine.go   # State type, transition guards, valid transitions
│           │   └── store.go     # state.json atomic read/write
│           ├── executor/
│           │   └── executor.go  # all system mutations, audit log
│           ├── catalog/
│           │   └── faults.go    # embedded fault definitions ([]Fault)
│           └── output/
│               ├── model.go     # result types (StatusResult, ValidateResult, etc.)
│               └── renderer.go  # human and JSON renderers
├── service/                     # Go HTTP service (separate from CLI)
├── config/                      # canonical config file templates
│   ├── app.service
│   ├── nginx-app.conf
│   ├── config.yaml
│   └── logrotate-app
├── bootstrap.sh                 # provisions the environment; calls lab provision
├── validate.sh                  # thin wrapper: exec lab validate "$@"
└── reset.sh                     # thin wrapper: exec lab reset "$@"
```

The shell scripts (`bootstrap.sh`, `validate.sh`, `reset.sh`) are thin wrappers that delegate to the `lab` binary. Once the binary exists, all operations SHOULD use `lab` directly.

**Build:** the bootstrap script compiles the `lab` binary during provisioning:

```bash
cd /opt/lab-env/cmd/lab && go build -o /usr/local/bin/lab .
```

The binary is owned by `root:root`, mode `755`, installed at `/usr/local/bin/lab`.

---

*End of Specification.*
*Spec version: v1.0.0*
*A conformant environment is one where all blocking conformance checks pass. See `conformance-model.md` §3 for the authoritative check catalog.*