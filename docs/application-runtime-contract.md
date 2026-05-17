**Application Runtime Contract (Data Plane)**
**Version 1.0.0**  
*Part of the Canonical Lab Environment Specification Suite*

---

> **Audience:** implementer‑primary. This document defines the runtime contract for the lab’s Go HTTP service – the “subject” application – within the canonical lab environment. It is a companion to the Canonical Lab Environment Specification v1.5.0 and the semantic model documents (Conformance Model v1.2.0, System State Model v1.3.0, Fault Model v1.2.0).  
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
| **FAULTED** | A fault from the catalog has been deliberately applied; the environment is intentionally non‑conformant in a specific way. |
| **BROKEN** | One or more blocking checks fail due to unintended modification; no fault is active. |
| **RECOVERING** | A reset operation is in progress; transitional. |

**The application does not determine the canonical state.** That is the exclusive responsibility of the Control Plane, which combines conformance check results, the state file, and runtime signals (including those produced by this contract). However, the application **SHALL** maintain an internal status indicator that reflects its own self‑assessment:

- The file `/run/app/status` SHALL contain a short string indicating the service’s view of its health (see §3.4).  
- The presence/absence of `/run/app/healthy`, `/run/app/loading`, and the value of `chaos_active` in telemetry provide additional evidence to the Control Plane, but they do not replace the canonical classification.

In all documentation and user‑facing output, the canonical state names are those listed above. The application SHALL NOT use state names like “Degraded” or “Fault‑State” in its signals, because those are not canonical states.

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

The service SHALL write its PID to this file immediately after startup, before it begins accepting requests. The file SHALL be unlinked during graceful shutdown. The conformance suite uses this file to verify that the process table PID matches the expected service process (check **P‑005**).

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
- **`requests_total` / `errors_total`**: Cumulative counters since process start. Errors include any 5xx responses and request‑handling panics.  
- **`chaos_active`**: Boolean; `true` if one or more chaos modes are active.  
- **`chaos_modes`**: Array of strings; the names of active chaos variables (e.g., `["latency", "drop"]`).

The telemetry file SHALL be written atomically (write‑temp‑rename) to avoid partial reads. The Control Plane and learners can poll it at any time. The conformance check **L‑004** verifies that `chaos_active` is `false` in the absence of a fault.

---

## §4 — Resource Constraints

The service operates within hard resource boundaries that are instantiated by the provisioning process and must not be altered by the service itself.

### 4.1 Control Group (cgroup) `app.slice`

The service is placed in the systemd slice `app.slice`. The slice definition is provided by the environment specification and enforces:

- `MemoryMax=256M` – the service process (and any children) cannot exceed 256 MiB of memory. Exceeding this limit results in the OOM killer terminating the process, which triggers the **BROKEN** state.
- `CPUQuota=20%` – the service is limited to 20% of one CPU core equivalent over a scheduling period. Sustained CPU throttling (as reported by `cpu.stat`) is a symptom that the service MAY use to set its internal status to `Degraded`.

The service SHALL NOT modify cgroup settings. It MAY read its own cgroup statistics (e.g., from `/sys/fs/cgroup/`) to populate telemetry and to detect throttling.

### 4.2 Loopback Storage Mount

The runtime state directory `/var/lib/app` is a loopback mount backed by a sparse file of **50 MiB**. This simulates a small, exhaustible storage volume.

- The mount is created during provisioning and listed in `/etc/fstab`.  
- The service SHALL NOT assume the presence of the mount; it MUST gracefully handle I/O errors (e.g., ENOSPC) when writing to `/var/lib/app/state` or other files in that directory.  
- A full disk (100% block usage) will cause subsequent writes to fail. The service SHALL log these failures and reflect the condition in telemetry (`disk_usage_percent`).  
- The fault catalog includes a fault (**F‑019**) that deliberately fills this volume.

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

- **Canonical Lab Environment Specification v1.5.0** – defines the environment’s overall architecture, filesystem layout, service unit file, provisioning sequence, and the Control Plane. This document references that one for canonical state names, resource instantiation details, and conformance checks (e.g., S‑007, F‑008, F‑009, P‑005, L‑004).  
- **Conformance Model v1.2.0** – defines the check catalog that validates this contract’s signals.  
- **System State Model v1.3.0** – defines how the signals in this document are combined with other evidence to determine the canonical state.  
- **Fault Model v1.2.0** – defines faults that manipulate `chaos.env` or nftables chains described here.  

No other document may contradict the signal file locations, chaos variable names, or telemetry schema defined here. When a conflict arises between this document and the environment specification about the **application’s runtime behavior**, this document is authoritative. When a conflict concerns **environment provisioning, conformance checks, or state classification**, the environment specification and model documents are authoritative.

---

*End of Application Runtime Contract (Data Plane) v1.0.0.*  
*Aligned with Canonical Lab Environment Specification v1.5.0, Conformance Model v1.2.0, System State Model v1.3.0, Fault Model v1.2.0.*


Yes, this specification is the contract for the Go service (the “Subject App” at `/opt/app/server`). My earlier analysis listed gaps across the entire data plane (platform, control plane, and the service itself). Let me now refocus strictly on **what the Go binary needs to implement** — and what’s missing from the spec to actually build it.

---

### What the Go Service Must Do (and What’s Undefined)

#### 1. State machine internals
The spec says the service must “reflect one of the six canonical states” in `telemetry.json`, but never defines:

- **How** the service detects its own state:
  - **Degraded**: Read cgroup stats? Which files (`cpu.stat`, `memory.events`)? How does a user-space process read its own cgroup without hardcoding the path? (usually `/sys/fs/cgroup/…`, but cgroup v1 vs v2 matters)
  - **Latency >500ms**: Latency of *what* exactly? (internal request processing? socket round-trip? self‑ping?) The service must be instrumented to measure it.
  - **Broken**: The service can’t report its own crash; that’s external. But for a *running* service to declare “Broken” (e.g., out-of-memory condition detected *before* OOM kill), what internal signal? (e.g., allocation failure? `ENOSPC` on the data volume?)
  - **Fault-State**: The spec says `chaos.env` is active and `LAB-FAULT` chain drops packets. Should the service *detect* that? If so, how? (periodically attempt outbound connections? read `/etc/app/chaos.env` directly?)

- **Transition rules**:
  - Under what conditions does the service move from **Conformant → Degraded**? “cgroup telemetry showing 90%+ resource utilisation” – 90% of *what* (MemoryMax, CPUQuota, or both)? Over what sliding window?
  - What clears a fault? Is there a hysteresis to avoid flapping?

#### 2. Telemetry contract
`/run/app/telemetry.json` is described as “high‑fidelity metrics & error logs” but the schema is absent. Without a fixed JSON structure, the grading/control plane can’t parse it. The service needs to know:

- Required fields: e.g., `state`, `uptime`, `memory_usage_bytes`, `cpu_throttled_seconds`, `open_fds`, `last_error`, `conformance_suite_passed` (boolean)
- Data types and units
- Frequency of updates (every second? on change?)

#### 3. Chaos contract
`/etc/app/chaos.env` “triggers Fault-State via environment variables”. The service must read this, but the exact variables and their semantics are missing. For example:

- `CHAOS_MODE=latency` – inject 500ms delay on every socket response?
- `CHAOS_MODE=partition` – simulate network drop by refusing all connections?
- `CHAOS_MODE=corrupt_config` – make the service behave as if config is invalid?

Without this contract, the service can’t implement fault injection.

#### 4. Conformance Suite
**Conformant** requires “Conformance Suite passes.” If the suite is run *against* the service’s socket, the service must expose specific endpoints or behaviours. The spec doesn’t define:

- What the suite checks (e.g., `GET /health` returns 200, response matches schema, file permissions are correct?)
- Whether the service runs a self‑test internally and reports the result in telemetry.

#### 5. Recovery / reset
The service enters **Recovering** when `/run/app/loading` exists and `reset-state.sh` is executing. What does the service do during recovery? Does it:

- Accept no traffic?
- Re‑initialise its internal state?
- Rerun integrity checks?
- Eventually remove `/run/app/loading` and transition to **Conformant**?

The service’s internal recovery logic is completely unspecified.

#### 6. Miscellaneous operational details
- **PID file**: “PID file missing” is evidence for Broken, but no path is given. The service must write one — where? `/run/app/app.pid` is a reasonable guess, but must be defined.
- **Socket protocol**: The socket `/run/app/app.sock` is the “primary interface”. What protocol? (HTTP over Unix socket? gRPC? raw JSON stream?) The service can’t be written without knowing what to speak.
- **`/run/app/status` string**: Must exactly match canonical state names? Is it a plain text file containing only “Degraded”? The service needs a precise format.

---

### Distinction: what’s the service’s job vs. the platform’s
Items like the systemd unit file, nftables rules, cgroup creation, and loopback mount are **platform responsibilities**, not service code. However, the service’s behaviour *depends* on them (e.g., it can only detect OOM if the cgroup is properly set up; it can’t self‑declare **Fault-State** unless the rules drop *its* traffic). The spec should still define them, because you can’t test the Go service in isolation without a matching environment.

**Bottom line:** Even when zooming in on just the Go service, critical specifications — state transition rules, telemetry schema, chaos variable contract, recovery behaviour, and the socket protocol — are missing. I’d recommend filling those in first, as they are the minimum needed for the developer to start coding the subject app.

Excellent, this is a solid consolidation of constraints. I’ll focus on the few things that, if unaddressed, will cause the most pain downstream — high ROI feedback.

---

### 1. `/run/app/status` strings don’t match the canonical states

You listed `Starting`, `Running`, `Degraded`, `Unhealthy`, `ShuttingDown`.  
The Data Plane Spec says `/run/app/status` must be a *“Direct State Mirror”* of the six canonical states: **Conformant, Degraded, Non‑Conformant, Fault‑State, Broken, Recovering**.

If the control plane parser expects `Conformant` but gets `Running`, grading breaks. At best you need a mapping layer, but that’s a brittle hidden contract.

**Recommendation:** Use canonical state names in the status file. Map internal phases (like startup) to the closest canonical state: e.g., `Starting` → `Recovering`, `Running` → `Conformant`, `Unhealthy` → `Broken`. Add a comment in the code that the value must match the spec exactly.

---

### 2. Telemetry schema must be locked down now

The conformance suite and the control plane will read `telemetry.json` every 2 seconds. Without a concrete schema you’ll have silent mismatches.

The constraints already imply several fields: `chaos_active`, `chaos_modes`, plus process metrics. You need exactly 10 fields, so define them all before coding. Missing ones likely include: `state` (canonical), `uptime`, `disk_usage_percent`, `cpu_percent`, `memory_rss_mb`, `open_fds`, `last_error`. The order matters for JSON if the schema is positional? Probably an object, but field names must be exact.

**High‑impact action:** Write the JSON schema as a struct with tags and share it with the control‑plane team now. That one‑time investment prevents weeks of integration head‑scratching.

---

### 3. What happens when `/var/lib/app/state` write fails?

The `/` handler must touch `/var/lib/app/state`. If the state directory is broken (F‑004), that write will fail. The spec is silent on the response code. You need `/health` to return 200 and `/` to *not* return 200 to produce the diagnostic pattern. Likely return `503 Service Unavailable` and log the error. Without a clear contract, someone will inadvertently return `200` and break the fault signature.

**Decide now:** On write error, return `503` (or `500`) and set status to `Degraded` in the current request cycle. Also ensure `/health` **never** touches that path.

---

### 4. OOM chaos implementation is dangerous if not isolated

`CHAOS_OOM_TRIGGER` should force an out‑of‑memory kill. If you allocate inside the same goroutine that serves requests, you risk deadlocking the whole server before the OOM reaper acts — making the failure nondeterministic.

**Safe pattern:** Spawn a separate goroutine that gradually allocates slices and doesn’t return. Mark `chaos_active = true, chaos_modes = ["oom"]`, and let the OS kill the process. The service will disappear abruptly, which is exactly what the lab expects. Document that this chaos mode will **terminate the process**, so logs will stop mid‑stream.

---

### 5. Signal‑file lifecycles need a tiny state machine comment

You have `/run/app/loading`, `/run/app/healthy`, `/run/app/status`. Creation and removal must be choreographed carefully:

- `/run/app/loading` created **before** any other setup, removed **after** all init + self‑test passes
- `/run/app/healthy` created only when `status = Conformant` (or `Running` if you keep your mapping)
- On shutdown: remove `/run/app/healthy`, write `status = ShuttingDown` (or `Recovering`?), then remove PID.

This is easy to get wrong in a hurried shutdown sequence. Adding a small table in a comment (e.g., in `signals.go`) that lists the exact order of file writes/removals will save you from subtle race conditions with the control plane polling.

---

### Answers to your questions

**Q1 – `/slow` endpoint:** Hardcoded is absolutely right. A fixed 5‑second sleep makes the teaching demo predictable and removes unnecessary configuration surface. I’d hardcode it directly in the handler with a `time.Sleep(5 * time.Second)`.

**Q2 – SIGHUP config reload:** Skip it. Startup‑only chaos environment reading is simpler, avoids the split‑semantics headache (don’t reopen logs, but reload chaos), and aligns with the fact that the fault runbook (F‑020) restarts the service anyway. Don’t build a feature you don’t need to demonstrate.

---

These five items are the highest‑leverage fixes before you write a single line. Everything else in your package design looks sound. Once the telemetry schema and status strings are final, you’ll have a stress‑free build.