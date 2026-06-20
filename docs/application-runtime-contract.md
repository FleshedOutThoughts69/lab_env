**Application Runtime Contract (Data Plane)**
**Version 1.0.0**  
*Part of the Canonical Lab Environment Specification Suite*

---

> **Audience:** implementer‑primary. This document defines the runtime contract for the lab’s Go HTTP service – the “subject” application – within the canonical lab environment. It is a companion to the Canonical Lab Environment Specification v1.0.0 and the semantic model documents (Conformance Model v1.0.0, System State Model v1.0.0, Fault Model v1.0.0).  
>
> **Normative language:** MUST, MUST NOT, SHALL – mandatory. SHOULD – strongly preferred. MAY – permitted.  
>
> **Document set relationship:**  
> - The Canonical Lab Environment Specification defines the overall environment.  
> - This document defines the application’s **in‑process runtime signals**, **resource constraints**, **chaos injection interface**, and **self‑monitoring obligations**.  
> - The Control Plane (defined in the environment spec §12) is the sole authority for determining the canonical state of the environment. The application’s role is to provide accurate, timely evidence; it does not decide the state.  

---

## §1 — Purpose & Scope

The Canonical Lab Environment includes a Go HTTP service (the “subject”) that learners observe and diagnose. To support the curriculum’s diagnostic workflow, the service must continuously expose its own health, resource usage, and any active fault‑injection mode. It must also respect hard resource limits and respond to a standardised chaos‑injection file.

This document specifies:

- The six **canonical environment states** that the system can occupy, and the service’s responsibility to report **internal status indicators** (not final state classifications).  
- The **signal files** the service maintains under `/run/app/` (PID, health, loading, status string, telemetry).  
- The **cgroup resource constraints** (`app.slice`) and the **loopback storage mount** that bound the service’s runtime.  
- The **chaos injection interface** (`/etc/app/chaos.env`) – a set of environment variables that permit controlled fault activation inside the process.  
- The service’s **observability contract**: log paths, rotation, and the `telemetry.json` schema.  

The target reader is both the service implementer (who must satisfy this contract) and the learner (who can consult it to understand the signals they observe).

---

## §2 — State Awareness

The environment can be in exactly one of six **canonical states**, as defined by the Canonical Lab Environment Specification Conformance Model:

| Canonical State | Meaning |
|----------------|---------|
| **UNPROVISIONED** | Bootstrap not run; no service or configuration exists. |
| **PROVISIONED** | Bootstrap completed but conformance not yet validated. |
| **CONFORMANT** | All blocking conformance checks pass. No active fault. Default operational state. |
| **DEGRADED** | A fault from the catalog has been deliberately applied; the environment is intentionally non‑conformant in a specific way. |
| **BROKEN** | One or more blocking checks fail due to unintended modification; no fault is active. |
| **RECOVERING** | A reset operation is in progress; transitional. |

**The application does not determine the canonical state.** That is the exclusive responsibility of the Control Plane, which combines conformance check results, the state file, and runtime signals (including those produced by this contract). However, the application **SHALL** maintain an internal status indicator that reflects its own self‑assessment:

- The file `/run/app/status` SHALL contain a short string indicating the service’s view of its health (see §3.4).  
- The presence/absence of `/run/app/healthy`, `/run/app/loading`, and the value of `chaos_active` in telemetry provide additional evidence to the Control Plane, but they do not replace the canonical classification.

In all documentation and user‑facing output, the canonical state names are those listed above. The application SHALL NOT use state names like “Faulted” or “Fault‑State” in its signals, because those are not canonical states.

---

## §3 — Signal Files

All signal files reside under `/run/app/`, a tmpfs directory owned by `appuser:appuser` with mode `755`. They are ephemeral and do not survive a reboot.

### 3.1 PID File

| Property | Value |
|----------|-------|
| **Path** | `/run/app/app.pid` |
| **Owner** | `appuser:appuser` |
| **Mode** | `644` |
| **Content** | Single line containing the decimal PID of the running service process. |

The service SHALL write its PID to this file immediately after startup, before it begins accepting requests. The file SHALL be unlinked during graceful shutdown. The Control Plane uses this file to verify process identity against the running service.

### 3.2 Healthy Marker

| Property | Value |
|----------|-------|
| **Path** | `/run/app/healthy` |
| **Owner** | `appuser:appuser` |
| **Mode** | `644` |
| **Content** | Empty file; presence alone indicates health. |

The service SHALL create this file once it has successfully:

- Parsed its configuration
- Bound its listening socket
- Opened its log file
- Verified that it can serve the `/health` endpoint correctly

Once created, the file SHALL remain until the service shuts down or enters a non‑functional state. The Control Plane uses the presence of this file as strong evidence that the process is alive and well.

### 3.3 Loading Marker

| Property | Value |
|----------|-------|
| **Path** | `/run/app/loading` |
| **Owner** | `appuser:appuser` |
| **Mode** | `644` |
| **Content** | Empty file; presence indicates initialization in progress. |

The service SHALL create this file at the very beginning of its startup sequence and SHALL delete it immediately after initialization is complete (i.e., just before creating `/run/app/healthy`). During reset operations, the bootstrap or reset script MAY also touch this file to signal that recovery is underway. The Control Plane interprets its presence as the **RECOVERING** state.

### 3.4 Status String

| Property | Value |
|----------|-------|
| **Path** | `/run/app/status` |
| **Owner** | `appuser:appuser` |
| **Mode** | `644` |
| **Content** | A single word reflecting the service’s self‑assessment. Allowed values: `Starting`, `Running`, `Degraded`, `Unhealthy`, `ShuttingDown`. |

The service SHALL update this file atomically (write‑temp‑rename) whenever its internal condition changes. The permitted values are:

- `Starting` – set immediately at startup, before any initialization.
- `Running` – set after all health checks pass; equivalent to `/run/app/healthy` present.
- `Degraded` – set when the service detects a non‑blocking anomaly, such as resource pressure (cgroup throttling) or active chaos injection.
- `Unhealthy` – set when the service detects a condition that prevents it from correctly serving requests (e.g., state directory unwritable, config reloaded with errors). The service may still be alive but will return 5xx errors.
- `ShuttingDown` – set when the service receives SIGTERM and begins graceful shutdown.

The Control Plane MAY read this file for additional diagnostic context, but its state classification is definitive only when combined with other signals.

### 3.5 Telemetry File

| Property | Value |
|----------|-------|
| **Path** | `/run/app/telemetry.json` |
| **Owner** | `appuser:appuser` |
| **Mode** | `644` |
| **Update interval** | Every **2 seconds** (MUST be refreshable). |

**Schema:**

```json
{
  "ts": "2026-01-01T12:00:00Z",
  "pid": 1234,
  "uptime_seconds": 120,
  "cpu_percent": 12.3,
  "memory_rss_mb": 45.2,
  "open_fds": 18,
  "disk_usage_percent": 67,
  "inode_usage_percent": 12,
  "requests_total": 1042,
  "errors_total": 3,
  "chaos_active": false,
  "chaos_modes": []
}
```

- **`cpu_percent`**: Percentage of one CPU core used by the process (0‑100 per core, so can exceed 100 on multi‑core).  
- **`memory_rss_mb`**: Resident set size in mebibytes, as reported by the OS.  
- **`open_fds`**: Number of open file descriptors (should be small, typically < 50).  
- **`disk_usage_percent`**: Percentage of the `/var/lib/app` loopback mount that is in use (block‑level).  
- **`inode_usage_percent`**: Percentage of inode usage on the `/var/lib/app` loopback mount. Distinguishes inode exhaustion (F‑018) from block exhaustion when used together with `disk_usage_percent`.  
- **`requests_total` / `errors_total`**: Cumulative counters since process start. Errors include any 5xx responses and request‑handling panics.  
- **`chaos_active`**: Boolean; `true` if one or more chaos modes are active.  
- **`chaos_modes`**: Array of strings; the names of active chaos variables (e.g., `["latency", "drop"]`).

The telemetry file SHALL be written atomically (write‑temp‑rename) to avoid partial reads. The Control Plane and learners can poll it at any time.

---

## §4 — Resource Constraints

The service operates within hard resource boundaries that are instantiated by the provisioning process and must not be altered by the service itself.

### 4.1 Control Group (cgroup) `app.slice`

The service is placed in the systemd slice `app.slice`. The slice definition is provided by the environment specification and enforces:

- `MemoryMax=256M` – the service process (and any children) cannot exceed 256 MiB of memory. Exceeding this limit results in the OOM killer terminating the process.
- `CPUQuota=20%` – the service is limited to 20% of one CPU core equivalent over a scheduling period. Sustained CPU throttling (as reported by `cpu.stat`) is a symptom that the service MAY use to set its internal status to `Degraded`.

The service SHALL NOT modify cgroup settings. It MAY read its own cgroup statistics (e.g., from `/sys/fs/cgroup/`) to populate telemetry and to detect throttling.

### 4.2 Loopback Storage Mount

The runtime state directory `/var/lib/app` is a loopback mount backed by a sparse file of **50 MiB**. This simulates a small, exhaustible storage volume.

- The mount is created during provisioning and listed in `/etc/fstab`.  
- The service SHALL NOT assume the presence of the mount; it MUST gracefully handle I/O errors (e.g., ENOSPC) when writing to `/var/lib/app/state` or other files in that directory.  
- A full disk (100% block usage) will cause subsequent writes to fail. The service SHALL log these failures and reflect the condition in telemetry (`disk_usage_percent`).  
- The fault catalog includes faults that exploit both block exhaustion (**F‑019**) and inode exhaustion (**F‑018**).

### 4.3 Network Filter Chain

A dedicated `nftables` chain, `LAB-FAULT`, exists to allow controlled injection of network partitions. The provisioning process creates the table and chain:

```bash
nft add table inet lab_filter
nft add chain inet lab_filter LAB-FAULT { type filter hook input priority 0\; }
```

By default, the chain has no rules and does not affect traffic. The fault catalog includes a fault (**F‑021**) that adds a rule to drop traffic to the upstream port, simulating a network partition between nginx and the application. The service itself does not need to interact with nftables; it simply observes that incoming requests cease (or that nginx returns 502).

---

## §5 — Chaos Injection Interface

The service MUST support runtime fault injection controlled by the file `/etc/app/chaos.env`. This file is a simple `KEY=value` format, loaded by systemd via `EnvironmentFile=/etc/app/chaos.env` in the unit file. The service reads its values at startup and MAY re‑read them on SIGHUP.

### 5.1 Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `CHAOS_LATENCY_MS` | integer (ms) | 0 | Adds a fixed extra delay to every incoming request before processing. |
| `CHAOS_DROP_PERCENT` | integer (0‑100) | 0 | Randomly drops the specified percentage of incoming requests (returns 503). |
| `CHAOS_OOM_TRIGGER` | boolean (0/1) | 0 | If `1`, the service will gradually allocate memory until the OOM killer terminates it. Requires confirmation. |
| `CHAOS_IGNORE_SIGTERM` | boolean (0/1) | 0 | If `1`, the service ignores SIGTERM (equivalent to fault F‑008). |

When one or more chaos variables are active (non‑zero / non‑default), the service SHALL:

- Set `chaos_active` to `true` in telemetry.
- Populate `chaos_modes` with short names for the active features: `"latency"`, `"drop"`, `"oom"`, `"nosigterm"`.
- Update its internal status to `Degraded` (unless a more severe condition overrides it).

When `CHAOS_LATENCY_MS` is active, each request handler SHALL sleep for exactly that many milliseconds before dispatching the request. When `CHAOS_DROP_PERCENT` is active, the service SHALL randomly reject that percentage of requests at the earliest possible point (before request‑specific processing) with an HTTP 503 response and a log entry indicating chaos drop.

The `CHAOS_OOM_TRIGGER` and `CHAOS_IGNORE_SIGTERM` modes are irreversible without a full rebuild/reset; they are defined in the fault catalog as complex faults (**F‑008**, **F‑014**) and require explicit confirmation.

### 5.2 Integration with Fault Catalog

Faults that modify `chaos.env` are simple faults (R2). The fault catalog (Fault Model §7) references these variables in its `Apply`/`Recover` steps. For example, fault **F‑020** writes `CHAOS_LATENCY_MS=400` to `chaos.env` and restarts the service; its `Recover` clears the file.

---

## §6 — Observability & Logging

### 6.1 Application Log

The primary request‑level log is `/var/log/app/app.log`. Its contract is defined in the Canonical Lab Environment Specification §3.4 (unbuffered, newline‑delimited JSON). This document does not duplicate that contract, except to note that the service SHALL also log all chaos‑related events (activation, deactivation) and any cgroup or disk errors at `"level":"warn"` or `"level":"error"`.

### 6.2 Log Rotation

Log rotation is handled by the system’s logrotate configuration (`/etc/logrotate.d/app`). The service SHALL NOT reopen its log file on SIGHUP, because `copytruncate` is used. The rotation script additionally touches `/var/log/app/.last_rotate` after each rotation; this timestamp file is available for operational monitoring but is not consumed by the service.

### 6.3 systemd Journal

Startup, shutdown, and critical error messages are emitted to stdout/stderr and captured by journald. The service SHALL prefix all such messages with its `SyslogIdentifier=app`. Learners and the Control Plane can query `journalctl -u app.service` for lifecycle events.

---

## §7 — Relationship to Other Documents

This contract is part of the **Canonical Lab Environment Specification Suite**. It interacts with:

- **Canonical Lab Environment Specification v1.0.0** – defines the environment’s overall architecture, filesystem layout, service unit file, provisioning sequence, and the Control Plane. This document references that one for canonical state names, resource instantiation details, and conformance checks.  
- **Conformance Model v1.0.0** – defines the check catalog that validates this contract’s signals.  
- **System State Model v1.0.0** – defines how the signals in this document are combined with other evidence to determine the canonical state.  
- **Fault Model v1.0.0** – defines faults that manipulate `chaos.env` or nftables chains described here.  

No other document may contradict the signal file locations, chaos variable names, or telemetry schema defined here. When a conflict arises between this document and the environment specification about the **application’s runtime behavior**, this document is authoritative. When a conflict concerns **environment provisioning, conformance checks, or state classification**, the environment specification and model documents are authoritative.

---

## §8 — Current Implementation Status

> **Purpose:** this section documents the delta between the aspirational contract above and the Go service that was built, tested, and verified on live Ubuntu 22.04 (aarch64) hardware as of June 2026.

### 8.1 — Endpoints

| Endpoint | Contract | Implementation | Notes |
|----------|----------|----------------|-------|
| `GET /health` | `{"status":"ok","app_env":"…","config_loaded":true}` | **Identical** | Fully implemented. |
| `GET /` (success) | `{"status":"ok","path":"/"}` | `{"status":"ok","path":"/","env":"<app_env>"}` | Returns both `path` and `env`; contract field `path` is present. |
| `GET /` (failure) | `{"status":"error","msg":"state write failed"}` | **Identical** | Fully implemented. |
| `GET /slow` | 5‑second delay, returns JSON with delay field | **Identical** — returns `{"status":"ok","path":"/slow","delay_seconds":5}` | Fully implemented. |
| `GET /reset` | TCP RST via SO_LINGER | **Identical** — hijacks connection, sets SO_LINGER, closes immediately | Fully implemented and verified. |
| `GET /headers` | Echoes proxy headers | **Identical** — returns `Host`, `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Real-IP`, `User-Agent` | Fully implemented and verified. |

### 8.2 — Telemetry Schema

- The implemented schema includes **12 fields** (the contract lists 11 in §3.5 after the addition of `inode_usage_percent`). The additional field is `inode_usage_percent`, added to distinguish inode exhaustion (F‑018) from block exhaustion.
- `chaos_modes` is always serialised as `[]` (never `null`).

### 8.3 — Chaos Injection

- `CHAOS_LATENCY_MS` and `CHAOS_DROP_PERCENT` are implemented and verified.
- `CHAOS_OOM_TRIGGER` is fully implemented and verified. The OOM goroutine allocates 64 MiB chunks until the cgroup `MemoryMax=256M` limit kills the process. Confirmed via `dmesg` and `journalctl`. The `StartOOMForTest` hook is exported for unit testing and the sync.Once test passes.
- `CHAOS_IGNORE_SIGTERM`: implemented as a **build flag** (`FaultIgnoreSIGTERM`) rather than a runtime environment variable. This aligns with fault F‑008, which requires a binary rebuild. The aspirational contract (§5.1) specifies a runtime env var; the current implementation uses a build flag. See F‑008 in the Fault Model for details.

### 8.4 — Signal Files & Status

- All signal files (`/run/app/status`, `/run/app/healthy`, `/run/app/loading`, `/run/app/app.pid`) are fully implemented and verified.
- Internal status strings match the contract exactly: `Starting`, `Running`, `Degraded`, `Unhealthy`, `ShuttingDown`.
- The `telemetry.json` file is written atomically every 2 seconds.

### 8.5 — Resource Constraints

- cgroup `app.slice` and the 50 MiB loopback mount are provisioned by `bootstrap.sh` and verified during live testing.
- The service correctly handles a full disk (returns 500 on `/`) and full inodes (returns 500 on `/`).

### 8.6 — Logging

- Structured JSON logging with `O_APPEND` is implemented.
- The log file is not reopened on SIGHUP; `copytruncate` logrotate is configured and verified.

---

*End of Application Runtime Contract (Data Plane) v1.0.0.*  
*Aligned with Canonical Lab Environment Specification v1.0.0, Conformance Model v1.0.0, System State Model v1.0.0, Fault Model v1.0.0.*