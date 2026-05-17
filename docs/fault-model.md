# Fault Model
## Lab Environment Semantic Model — Layer 3 of 3
## Version 1.0.0

> **Authority:** This document defines what a fault is, the rules governing mutation and recovery, and the complete fault catalog. It is the single authoritative source for fault semantics. It depends on `conformance-model.md` for check IDs used in postconditions and on `system-state-model.md` for state preconditions and postconditions. When this document conflicts with `canonical-environment.md` §7, this document is authoritative on fault semantics; `canonical-environment.md` §7 is authoritative on instantiation details (exact file paths, commands) specific to the Ubuntu 22.04 environment.
>
> **Catalog consistency:** the fault catalog in this document and in `canonical-environment.md` §7 MUST remain consistent. Both MUST carry the same version identifier. When a fault is added, modified, or removed, both documents MUST be updated in the same change.
>
> **Audience:** implementer-primary. Normative rules first; rationale where ambiguity would otherwise exist.
>
> **Normative language:** MUST, MUST NOT, SHALL — mandatory. SHOULD — strongly preferred. MAY — permitted.

---

## §1 — Purpose

This document defines the fault system for the lab environment. A fault is a deterministic, documented mutation operator over the environment state machine. This document specifies what faults are, what rules govern their application and recovery, and the complete catalog of faults available in the lab.

**Position in the model hierarchy:**

```
conformance-model.md   ← check IDs used in postconditions
system-state-model.md  ← state preconditions and postconditions
fault-model.md         ← this document (mutation operators)
```

Faults are defined in terms of the other two model documents. A fault's precondition references a state from the state model. A fault's postcondition references check IDs from the conformance model. Fault semantics cannot be understood without the other two documents.

---

## §2 — What a Fault Is

### 2.1 Definition

A fault is a **deterministic, documented, bounded mutation** that transitions the lab environment from `CONFORMANT` to a specific `DEGRADED` state by introducing exactly one failure cause into the system.

Each component of this definition is normative:

**Deterministic:** the same fault applied to a conformant environment MUST produce the same observable outcome every time. Non-deterministic mutations (race conditions, timing-dependent failures) are not faults in this model.

**Documented:** every fault has a complete entry in the catalog (§7). An undocumented mutation is not a fault — it produces `BROKEN`, not `DEGRADED`.

**Bounded:** a fault affects exactly one layer of the dependency chain (`filesystem → permissions → process → service → socket → proxy → response`). Cross-layer mutations that affect multiple independent system behaviors are not single faults.

**Exactly one failure cause:** the one-fault constraint (§2.2) ensures that the environment in `DEGRADED` state has one and only one non-conformant condition. This is required for the diagnostic model to function.

### 2.2 The One-Fault Constraint

At most one fault MAY be active at any time. This is an architectural constraint, not a technical limitation.

**Formal justification:** a lab fault is a pedagogical instrument. Its purpose is to produce one isolatable failure cause that can be traced through one causal chain to one diagnosis. Multiple simultaneous faults produce overlapping symptoms in different layers of the dependency chain. The learner cannot attribute a symptom to a specific cause when multiple causes are present. The causal chain from symptom to root cause is the learning objective; multiple simultaneous faults destroy the chain's uniqueness.

**Enforcement:** the control plane enforces this constraint by rejecting `lab fault apply` when state is `DEGRADED`. The `--force` flag bypasses this enforcement and records `forced: true` in the audit log. Forced application is permitted for advanced use cases but the resulting state is classified as ambiguous in the audit record.

### 2.3 Fault vs Broken Environment

The distinction between a fault (`DEGRADED` state) and a broken environment (`BROKEN` state) is whether the non-conformance was deliberately applied through the control plane with a recorded fault ID.

| Property | DEGRADED (fault active) | BROKEN (no fault) |
|---|---|---|
| Non-conformance origin | Deliberate, documented | Unintended or unknown |
| Fault ID recorded | Yes | No |
| Symptoms are predictable | Yes (per fault postcondition) | No |
| Recovery path | Fault `Recover` function + reset tier | Reset tier only |
| Diagnostic intent | Learner diagnoses known fault | Environment needs repair |

### 2.4 Fault Categories

Faults are classified by the layer of the dependency chain they affect and the domain of the practice problem set that uses them.

**Layer categories:**
- `filesystem` — file creation, deletion, or content modification
- `permissions` — ownership or mode bit changes
- `config` — configuration file content changes
- `service` — systemd unit file changes
- `process` — application behavior changes (binary rebuild required)
- `socket` — network binding changes
- `proxy` — nginx configuration changes
- `network` — network-level behaviors (baseline behaviors, not mutations)

**Domain tags:** `linux`, `networking`, `security`, `os` — indicates which practice problem set uses the fault. A fault may belong to multiple domains.

---

## §3 — Fault Schema

### 3.1 Required Fields

Every fault in the catalog MUST define all of the following fields:

| Field | Type | Description |
|---|---|---|
| **ID** | string | Unique identifier: `F-NNN` |
| **Layer** | string | Layer of the dependency chain affected (see §2.4) |
| **Domain** | []string | Practice problem domains that use this fault |
| **RequiresConfirmation** | bool | `true` if the mutation requires operator confirmation before executing. Determined by fault properties, not inferred from complexity. |
| **IsReversible** | bool | `true` if `Recover` returns the system to pre-Apply state. `false` for binary rebuild faults. |
| **ResetTier** | enum | `R1`, `R2`, or `R3` — the reset tier required to restore CONFORMANT |
| **Preconditions** | []State | States the environment MUST be in for Apply to execute. Standard: `[CONFORMANT]`. |
| **PreconditionChecks** | []string | Optional. Additional conformance check IDs (from `conformance-model.md` §3) that MUST pass immediately before Apply executes. Used when the standard state precondition is insufficient — for example, when the fault requires a specific process to be running. Checked after the state precondition. Empty for most faults. |
| **Apply** | func(Executor) error | The mutation function. MUST route all system operations through the Executor. The catalog entry for each fault specifies the exact sequence of Executor calls. Returns nil on success; non-nil error if any step fails, in which case the state file MUST NOT be updated (§4.2). |
| **Recover** | func(Executor) error | The recovery function. MUST be idempotent. The catalog entry for each fault specifies the exact sequence of Executor calls that undo Apply. For non-reversible faults, MUST return an error directing the operator to R3 reset and MUST NOT attempt any system mutation. The error message is implementation-defined; it MUST convey that R3 reset is required (see §4.4). |
| **Postcondition** | PostconditionSpec | See §3.2 |
| **Symptom** | string | Human-readable description of observable behavior after fault application |
| **AuthoritativeSignal** | string | Which observability source (journald, app.log, ss, curl) is the primary evidence source |
| **Observable** | string | Exact command(s) and expected output that confirm the fault is active |
| **MutationDisplay** | string | Human-readable description of the mutation shown to the operator before Apply executes |

**`severity` is not a fault field.** Severity is derived from the conformance model — it is a property of the checks that fail, not of the fault that causes them to fail. The postcondition (§3.2) identifies which checks fail; the conformance model assigns severity to those checks.

### 3.2 Postcondition Specification

A fault's postcondition defines the exact conformance state after the fault is applied. It has two complementary representations:

**Behavioral postcondition (human meaning):** a description of how the system behaves differently after the fault. Written in terms of observable system behavior, not implementation details.

**Conformance postcondition (machine-verifiable):** the exact set of conformance check IDs (from `conformance-model.md` §3) that:
- **Fail** after the fault is applied (the failing set)
- **Continue to pass** despite the fault (the invariant set — checks that the fault does not affect)

```go
type PostconditionSpec struct {
    Behavioral  string   // human description
    FailingChecks []string // check IDs that fail after Apply; empty slice for F-008 and F-014
    PassingChecks []string // notable checks that continue to pass
    // All checks not listed in either set are unaffected and continue
    // to pass unless they depend on a listed failing check
}
```

**Catalog presentation:** the catalog (§7.2) represents the `PostconditionSpec` as three separate labelled rows (`Behavioral postcondition`, `Failing checks`, `Invariant checks`) for human readability. These rows map directly to the struct fields above. Machine parsing must treat `Failing checks: []` as an empty `FailingChecks` slice, not a missing field. Descriptive text in the `Failing checks` row is a schema violation — the field MUST contain only check IDs or be empty.

**The passing checks field is as important as the failing checks field.** It specifies which invariants the fault does NOT break — this is the diagnostic isolation property. For example, F-004 (state directory unwritable) fails E-002 (`/` returns 500) but E-001 (`/health` returns 200) continues to pass. The continued passing of E-001 is not incidental — it is the fault's primary diagnostic feature, demonstrating the health/ready split.

### 3.3 Observability Contract

Every fault MUST satisfy the observability contract:

**At least one of the following MUST be true after fault application:**
1. At least one conformance check in `conformance-model.md` §3 fails (visible in `lab validate`)
2. At least one structured log entry in `app.log` indicates the fault condition (visible in `tail app.log`)
3. Observable network behavior changes (visible in `curl`, `ss`, or `tcpdump`)

**Faults that produce no observable signal are not valid faults** — they cannot be diagnosed and cannot be verified as active. Every fault in the catalog MUST have at least one `Observable` command that confirms the fault is active.

**State-altering observation:** F-008 (SIGTERM ignored) is the single fault whose observable command changes the system state (`time sudo systemctl stop app` stops the service). State-altering observation is explicitly permitted for F-008 under the following conditions:

1. The fault is otherwise unobservable by non-destructive means — no conformance check fails, no log entry differs, no network behavior changes while the service is running
2. The observable command is the authoritative and expected diagnostic technique for this fault class (shutdown timing is how SIGTERM handling is verified in any production system)
3. The operator is informed that the observation stops the service and must restart it afterward

This does not open a general exception. All other faults MUST have non-destructive observable commands. F-008 is the specific exception and its status as such is documented here as the authoritative statement.

---

## §4 — Mutation Rules

### 4.1 The Executor Requirement

All system mutations performed by fault `Apply` and `Recover` functions MUST route through the Executor interface (defined in `canonical-environment.md` §13.5). No fault function MAY call `os/exec`, `syscall`, or any other system interface directly.

**Rationale:** the Executor is the single layer through which all system mutations pass. It provides audit logging, consistent error handling, and the ability to trace every mutation the control plane makes. Bypassing it produces unaudited mutations that cannot be attributed to a fault ID in the audit log.

### 4.2 Logical Atomicity of Apply

A fault is considered applied only when:
1. The `Apply` function completes without error, AND
2. The state file is updated to `DEGRADED` with the fault ID recorded

**If the `Apply` function fails:** the state file MUST NOT be updated. The environment MAY be in a partially mutated state depending on how far `Apply` progressed before failing. Run `lab status` to determine the current classification via the state detection algorithm (`system-state-model.md` §4.2). The operator must run `lab reset` to restore canonical state.

**If `Apply` succeeds but state file write fails:** the runtime state may be degraded but the control plane does not know which fault caused it. The control plane MUST classify this as `BROKEN` and report the state recording failure. Running `lab validate` will establish the actual conformance state.

This is logical atomicity, not syscall atomicity. The guarantee is about the consistency of the control plane's knowledge, not about filesystem transaction semantics.

### 4.3 Simple Mutations

Simple mutations are file, permission, configuration, and service-state changes that can be reversed by the `Recover` function without rebuilding the binary.

**Characteristics of simple mutations:**
- Mutation is a single operation or a small sequence of operations
- `Recover` function restores the exact pre-Apply state
- `IsReversible: true`
- `ResetTier` is R1 or R2

**What `Apply` MUST NOT do for simple mutations:**
- Modify files outside the canonical environment paths (see `canonical-environment.md` §2.3)
- Create new system users or groups
- Install or remove packages
- Modify kernel parameters

### 4.4 Complex Mutations

Complex mutations require rebuilding the Go service binary with different behavior (fault injection hooks). They cannot be reversed without a full reprovision.

**Characteristics of complex mutations:**
- Mutation changes application behavior, not just configuration or file state
- Requires recompiling the binary with a fault flag enabled
- `IsReversible: false`
- `ResetTier: R3`
- `RequiresConfirmation: true`

**Current complex faults:** F-008 (SIGTERM ignored), F-014 (zombie children accumulation).

**`Recover` function behavior for complex faults:** the `Recover` function MUST return an error directing the operator to R3 reset. The exact error message is implementation-defined; it MUST convey that R3 reset (full reprovision) is required and MUST NOT attempt partial recovery or binary restoration.

---

## §5 — Pre/Post Conditions

### 5.1 Standard Precondition

The standard precondition for all faults is: **current state MUST be `CONFORMANT`**.

This precondition is checked by the control plane before calling `Apply`. If the current state is not `CONFORMANT`, `lab fault apply` MUST refuse with an error message that:
1. States the current state
2. Explains why the fault cannot be applied in that state
3. Provides the command to reach CONFORMANT state

### 5.2 Fault-Specific Preconditions

Some faults have additional preconditions beyond state. These are declared in the fault's **`PreconditionChecks`** field (defined in §3.1) as a list of conformance check IDs that MUST pass immediately before `Apply` executes. This is distinct from the **`Preconditions`** field, which holds state values — `PreconditionChecks` holds check IDs from the conformance catalog that are run as live observations.

`PreconditionChecks` is empty for most faults. It is used when the standard state precondition (CONFORMANT) is insufficient to guarantee the fault will behave correctly. For example:

- F-010 (log file deleted while running): `PreconditionChecks: [P-001]` — the app process must be running so that the deleted inode is held open. If the process is not running, the fault's teaching value is lost: no open file descriptor means the delete is simply a missing file, not a deleted-but-held inode.
- F-018 (inode exhaustion): `PreconditionChecks: []` — the `df -i` check is not a formal precondition. The `Apply` function will fail naturally if inodes are already exhausted; no pre-check is required.

Fault-specific `PreconditionChecks` are evaluated after the state precondition. Both MUST pass for `Apply` to execute.

### 5.3 Postcondition Specification

The postcondition specifies the exact conformance state after successful `Apply`. It has two components:

**Failing checks:** the conformance check IDs (from `conformance-model.md` §3) that will fail after the fault is applied. This is the machine-verifiable postcondition.

**Invariant checks:** notable conformance check IDs that continue to pass. These are specified when a check's continued passing is diagnostically significant — i.e., when a learner might expect it to fail but it does not.

**Verification:** after `Apply` completes, the control plane MAY verify the postcondition by running the checks in the `FailingChecks` list and confirming they fail. This is optional but provides a correctness guarantee that the fault was applied as specified.

### 5.4 Which Checks Fail by Fault Class

The following table summarizes which conformance check classes are affected by each fault layer:

| Fault layer | Typically fails | Typically invariant |
|---|---|---|
| `filesystem` (missing file) | S-001 (if config/binary), E-001, E-002 | Depends on which file |
| `permissions` (wrong mode) | F-series check for the path | E-001 may be invariant if only /health |
| `config` (wrong bind port) | P-002, E-001, E-002, E-004 | Process still running |
| `service` (broken unit) | S-001, E-001, E-002 | Filesystem checks |
| `process` (binary behavior) | E-002 (for /), or S-001 (for shutdown) | E-001 may be invariant |
| `socket` (wrong bind) | P-002, E-001, E-002 | App process running |
| `proxy` (nginx config) | E-001, E-004, E-005 | App direct checks |

---

## §6 — Reversibility Semantics

### 6.1 Definition of Reversibility

A fault is reversible if and only if calling `Recover` after `Apply` returns the system to a state that is **conformance-identical** to the pre-Apply state:

1. The conformance suite passes (all blocking checks pass)
2. The state file shows no active fault
3. No trace of the mutation remains in canonical files, permissions, or service state

Reversibility is a property of the fault's `Recover` function, not of the reset system. A reversible fault can be undone without running the full reset tier — `Recover` is sufficient. The reset tier (`ResetTier` field) is the fallback when `Recover` is not used or fails.

### 6.2 Reversible Faults (R1/R2)

Reversible faults MUST have `IsReversible: true` and a `Recover` function that satisfies:

**Idempotency:** calling `Recover` twice on an already-recovered system MUST NOT produce errors or unintended mutations. Idempotency enables safe retry on failure.

**Completeness:** `Recover` MUST restore all canonical values that `Apply` mutated. If `Apply` changes a file's mode and content, `Recover` MUST restore both mode and content.

**Non-side-effect:** `Recover` MUST NOT make changes beyond reversing the `Apply` mutation. It is not a general reset — it is a targeted undo.

### 6.3 Non-Reversible Faults (R3)

Non-reversible faults (`IsReversible: false`) require R3 reset (full reprovision) because the binary has been replaced. The original binary cannot be restored by `Recover` — it requires rebuilding from source.

**`Recover` for non-reversible faults:** MUST return an error directing the operator to R3 reset. MUST NOT attempt to restore the binary or any other system state. The error message is the recovery instruction.

### 6.4 Reset Tier as Reversibility Classification

The `ResetTier` field classifies the recovery depth required:

| Tier | Operations | Fault characteristics |
|---|---|---|
| R1 | Service restart only | Mutation is in process state only; no files changed |
| R2 | Restore canonical files + restart | Mutation changed files, permissions, or config |
| R3 | Full reprovision (bootstrap script) | Binary replaced; file-level recovery is insufficient |

The reset tier is the fallback recovery path used by `lab reset`. It is independent of (but consistent with) the `Recover` function. An operator may use `Recover` for targeted undo or `lab reset --tier <tier>` for full recovery.

---

## §7 — Fault Catalog

### 7.1 Catalog Completeness Condition

The catalog is complete when:

**Forward direction:** every fault maps to at least one conformance check ID in `FailingChecks`, with two permitted exceptions: F-008 and F-014, which manifest only at shutdown or over time and have empty `FailingChecks` by design (documented in §3.3 and §4.4).

**Reverse direction:** every conformance check in `conformance-model.md` §3 that has a non-empty `Maps to` field references at least one fault in this catalog.

**Observability:** every fault has at least one `Observable` command that produces distinguishable output when the fault is active vs conformant. For F-008, the observable command is state-altering (documented exception in §3.3).

**Apply/Recover:** every fault has a fully specified `Apply` and `Recover` step sequence in its catalog entry. For non-reversible faults (F-008, F-014), `Recover` is specified as returning an error directing to R3 reset.

**Recovery:** every fault has a defined `ResetTier` (`R1`, `R2`, or `R3`). Baseline behaviors (§10 appendix) are not faults and do not have `ResetTier` values.

### 7.2 Catalog Entries

---

**F-001 — Missing configuration file**

| Field | Value |
|---|---|
| Layer | `filesystem` |
| Domain | `linux`, `os` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | `sudo rm /etc/app/config.yaml` |
| Symptom | Service enters restart loop. `/health` fails (connection refused). nginx returns 502. |
| AuthoritativeSignal | journald — `journalctl -u app.service` |
| Observable | `journalctl -u app.service -n 20` shows repeated start failures with config-not-found error; `curl localhost/health` → connection refused |
| **Behavioral postcondition** | App cannot start because config is missing. Restart loop produces repeated journald entries. No endpoint is reachable. |
| **Failing checks** | S-001, E-001, E-002, E-003, E-004, E-005 |
| **Invariant checks** | F-003 (log dir still exists), F-007 (DNS still resolves) |
| **Apply** | `exec.Remove("/etc/app/config.yaml")` |
| **Recover** | `exec.RestoreFile("/etc/app/config.yaml")` — restores from embedded canonical bytes with mode 0640, owner appuser:appuser |

---

**F-002 — Wrong bind port in config**

| Field | Value |
|---|---|
| Layer | `config`, `socket` |
| Domain | `linux`, `networking` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Change `server.addr` in `/etc/app/config.yaml` from `127.0.0.1:8080` to `127.0.0.1:9090`; `sudo systemctl restart app` |
| Symptom | nginx returns 502. App process is running and believes it is healthy. `/health` fails because nginx cannot reach the app. |
| AuthoritativeSignal | `ss -ltnp` + nginx error log |
| Observable | `ss -ltnp \| grep 9090` shows app on wrong port; `curl -I localhost` → 502 with `X-Proxy: nginx`; `curl 127.0.0.1:9090/health` → 200 (direct) |
| **Behavioral postcondition** | App is running and healthy from its own perspective (reachable directly on 9090) but unreachable via nginx (which expects 8080). The health/proxy split is observable. |
| **Failing checks** | P-002, E-001, E-002, E-003, E-004, E-005 |
| **Invariant checks** | S-001 (app is active), P-001 (running as appuser), F-002 (config exists) |
| **Apply** | 1. `exec.ReadFile("/etc/app/config.yaml")` → replace `127.0.0.1:8080` with `127.0.0.1:9090` → `exec.WriteFile("/etc/app/config.yaml", modified, 0640, "appuser", "appuser")`; 2. `exec.Systemctl("restart", "app.service")` |
| **Recover** | `exec.RestoreFile("/etc/app/config.yaml")` → `exec.Systemctl("restart", "app.service")` |

---

**F-003 — Config file unreadable by appuser**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux`, `security` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | `sudo chmod 000 /etc/app/config.yaml` |
| Symptom | Service enters restart loop. journald shows permission denied reading config. |
| AuthoritativeSignal | journald |
| Observable | `journalctl -u app.service -n 10` shows permission denied error; `stat /etc/app/config.yaml` shows mode 000; `curl localhost/health` → connection refused |
| **Behavioral postcondition** | App cannot read config (permission denied). Same observable symptoms as F-001 (restart loop, connection refused) but the structural check F-002 mode bits distinguish the cause. |
| **Failing checks** | S-001, E-001, E-002, E-003, E-004, E-005 |
| **Invariant checks** | F-002 (config file exists — distinguishes from F-001), F-007 |
| **Apply** | `exec.Chmod("/etc/app/config.yaml", 0000)` |
| **Recover** | `exec.Chmod("/etc/app/config.yaml", 0640)` |

---

**F-004 — State directory unwritable by appuser**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux`, `os` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | `sudo chmod 000 /var/lib/app/` |
| Symptom | `/health` returns 200. `/` returns 500. App continues running. The health/ready split is the diagnostic signal. |
| AuthoritativeSignal | app.log |
| Observable | `curl localhost/health` → 200; `curl localhost/` → 500; `tail -5 /var/log/app/app.log` shows `"level":"error","msg":"state write failed"` |
| **Behavioral postcondition** | App is alive and config is loaded (health endpoint passes) but the state write on `/` fails (permission denied on `/var/lib/app/`). Demonstrates that `/health` and `/` have independent failure modes. |
| **Failing checks** | E-002, F-004 (state dir mode) |
| **Invariant checks** | S-001, E-001, E-003 — E-001 passing while E-002 fails is the primary diagnostic signal |
| **Apply** | `exec.Chmod("/var/lib/app", 0000)` |
| **Recover** | `exec.Chmod("/var/lib/app", 0755)` |

---

**F-005 — Binary not executable**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | `sudo chmod 640 /opt/app/server` |
| Symptom | Service fails to start. `systemctl status app` shows exec failure. |
| AuthoritativeSignal | journald |
| Observable | `journalctl -u app.service -n 5` shows exec format error or permission denied; `ls -la /opt/app/server` shows 640 |
| **Behavioral postcondition** | systemd cannot execute the binary (execute bit removed). Restart loop with exec failure. Identical symptom to F-001 and F-003 at the endpoint level, but F-001 structural check distinguishes — binary exists with wrong mode. |
| **Failing checks** | S-001, E-001, E-002, E-003, E-004, E-005, F-001 (mode bits) |
| **Invariant checks** | F-002 (config still exists and readable), F-007 |
| **Apply** | `exec.Chmod("/opt/app/server", 0640)` |
| **Recover** | `exec.Chmod("/opt/app/server", 0750)` |

---

**F-006 — APP\_ENV removed from unit file**

| Field | Value |
|---|---|
| Layer | `service` |
| Domain | `linux`, `os` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Remove `Environment=APP_ENV=prod` line from `/etc/systemd/system/app.service`; `sudo systemctl daemon-reload && sudo systemctl restart app` |
| Symptom | Service fails to start. journald shows "missing APP\_ENV" error. Structurally different from F-001/F-003 — config exists and is readable, but the required environment variable is absent. |
| AuthoritativeSignal | journald |
| Observable | `journalctl -u app.service -n 10` shows missing APP\_ENV error; `systemctl show app --property=Environment` shows no APP\_ENV |
| **Behavioral postcondition** | App startup validation fails on the environment variable check, not the config file check. journald message explicitly identifies APP\_ENV as missing. |
| **Failing checks** | S-001, E-001, E-002, E-003, E-004, E-005 |
| **Invariant checks** | F-002 (config exists and readable — distinguishes from F-001/F-003) |
| **Apply** | 1. `exec.ReadFile("/etc/systemd/system/app.service")` → remove line containing `Environment=APP_ENV=prod` → `exec.WriteFile("/etc/systemd/system/app.service", modified, 0644, "root", "root")`; 2. `exec.Systemctl("daemon-reload", "")`; 3. `exec.Systemctl("restart", "app.service")` |
| **Recover** | `exec.RestoreFile("/etc/systemd/system/app.service")` → `exec.Systemctl("daemon-reload", "")` → `exec.Systemctl("restart", "app.service")` |

---

**F-007 — nginx pointing to wrong upstream port**

| Field | Value |
|---|---|
| Layer | `proxy`, `config` |
| Domain | `linux`, `networking` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Change `server 127.0.0.1:8080` to `server 127.0.0.1:9090` in `/etc/nginx/sites-enabled/app`; `sudo nginx -s reload` |
| Symptom | nginx returns 502. App is running correctly on 8080. The fault is in the proxy configuration, not the app. |
| AuthoritativeSignal | nginx error log + `ss -ltnp` |
| Observable | `ss -ltnp \| grep 8080` shows app running correctly; `curl -I localhost` → 502; nginx error log shows "connection refused" on 9090; `curl 127.0.0.1:8080/health` → 200 (direct) |
| **Behavioral postcondition** | App is healthy on its actual port. nginx is misconfigured to upstream the wrong port. Distinguishable from F-002 (wrong app port) because here the app is on 8080 but nginx expects 9090. |
| **Failing checks** | E-001, E-002, E-003, E-004, E-005 |
| **Invariant checks** | S-001, P-001, P-002 (app listening on 8080) — P-002 passing distinguishes from F-002 |
| **Apply** | 1. `exec.ReadFile("/etc/nginx/sites-enabled/app")` → replace `server 127.0.0.1:8080;` with `server 127.0.0.1:9090;` inside the `upstream app_backend` block → `exec.WriteFile("/etc/nginx/sites-enabled/app", modified, 0644, "root", "root")`; 2. `exec.NginxReload()` |
| **Recover** | `exec.RestoreFile("/etc/nginx/sites-enabled/app")` → `exec.NginxReload()` |

---

**F-008 — SIGTERM ignored (unclean shutdown)**

| Field | Value |
|---|---|
| Layer | `process` |
| Domain | `linux`, `os` |
| RequiresConfirmation | **true** |
| IsReversible | **false** |
| ResetTier | R3 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Rebuild binary with `FAULT_IGNORE_SIGTERM=true` flag enabled; redeploy |
| Symptom | `sudo systemctl stop app` hangs for 90 seconds before systemd sends SIGKILL. App is running correctly during this period. |
| AuthoritativeSignal | `systemctl status app` showing stop-sigterm → stop-sigkill transition |
| Observable | `time sudo systemctl stop app` takes ~90 seconds; `journalctl -u app.service` shows `Sent signal SIGTERM` followed by `Sent signal SIGKILL` 90 seconds later; app serves requests normally during the wait |
| **Behavioral postcondition** | App appears healthy to all endpoint checks during the fault. The fault only manifests at shutdown — `sudo systemctl stop app` takes ~90 seconds because SIGTERM is ignored and systemd must wait for the KillTimeout before sending SIGKILL. All conformance checks pass while the app is running. The fault is silent to `lab validate`. |
| **Failing checks** | [] |
| **Invariant checks** | All 23 checks pass while app is running |
| **Apply** | `exec.RunMutation("go", "build", "-ldflags", "-X main.FaultIgnoreSIGTERM=true", "-o", "/opt/app/server", "./service")` → `exec.Chown("/opt/app/server", "appuser", "appuser")` → `exec.Chmod("/opt/app/server", 0750)` → `exec.Systemctl("restart", "app.service")` |
| **Recover** | Returns error: `"R3 reset required: run lab reset --tier R3 to rebuild the binary without the fault flag"`. MUST NOT attempt any system mutation. |

---

**F-009 — Log file unwritable**

| Field | Value |
|---|---|
| Layer | `permissions` |
| Domain | `linux`, `os` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | `sudo chmod 000 /var/log/app/app.log` |
| Symptom | Service fails to start. journald shows log file open failure. |
| AuthoritativeSignal | journald |
| Observable | `journalctl -u app.service -n 5` shows log file permission denied; `stat /var/log/app/app.log` shows mode 000 |
| **Behavioral postcondition** | App cannot open its log file at startup (mode 000). Startup fails before binding. Distinguishable from F-001/F-003 via structural check — config exists and is readable. |
| **Failing checks** | S-001, E-001, E-002, E-003, E-004, E-005, L-001, L-002, L-003, F-003 (log file mode) |
| **Invariant checks** | F-002 (config exists) |
| **Apply** | `exec.Chmod("/var/log/app/app.log", 0000)` |
| **Recover** | `exec.Chmod("/var/log/app/app.log", 0640)` |

---

**F-010 — Log file deleted while running**

| Field | Value |
|---|---|
| Layer | `filesystem` |
| Domain | `linux`, `os` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R1 |
| Preconditions | [CONFORMANT] |
| PreconditionChecks | [P-001] — app process must be running; the fault's teaching value depends on the inode being held open by a live process |
| MutationDisplay | `sudo rm /var/log/app/app.log` (while service is running) |
| Symptom | Service continues running and serving requests. `app.log` does not exist on disk. Disk space held by the file is not freed until restart. New request logs are lost. |
| AuthoritativeSignal | `lsof -p $(pgrep server)` |
| Observable | `ls /var/log/app/` shows no app.log; `lsof +L1` shows app process holding a deleted file descriptor; `curl localhost/health` → 200 (app still running); disk space unreleased |
| **Behavioral postcondition** | App is alive and serving requests. Log file is unlinked (link count 0) but the inode is held open by the app process. New log entries are written to the unlinked inode and are not accessible on the filesystem. |
| **Failing checks** | L-001, L-002, L-003 |
| **Invariant checks** | S-001, P-001, P-002, E-001, E-002 — app continues serving normally |
| **Apply** | `exec.Remove("/var/log/app/app.log")` — must be called while the service is running (enforced by PreconditionChecks: [P-001]) so the inode is held open |
| **Recover** | `exec.Systemctl("restart", "app.service")` — restarting causes the service to re-open the log file, recreating the inode. R1 tier sufficient. |

---

**F-013 — Unit file syntax error (service enabled but broken)**

| Field | Value |
|---|---|
| Layer | `service` |
| Domain | `linux` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Replace `ExecStart=/opt/app/server` with `ExecStart=/opt/app/DOESNOTEXIST` in unit file; `sudo systemctl daemon-reload` |
| Symptom | `systemctl is-enabled app` → enabled. `systemctl is-active app` → failed. Service will not start. The enabled/active asymmetry is the diagnostic signal. |
| AuthoritativeSignal | journald + `systemctl status app` |
| Observable | `systemctl status app` shows failed state and exec error; `journalctl -u app.service -n 10` shows the specific exec failure; `systemctl is-enabled app` → enabled (demonstrating desired ≠ actual state) |
| **Behavioral postcondition** | Service is enabled (desired state) but not active (runtime state). Demonstrates the critical distinction between systemd desired state and runtime state. |
| **Failing checks** | S-001, E-001, E-002, E-003, E-004, E-005 |
| **Invariant checks** | S-002 (enabled) — the enabled/failed asymmetry is the fault's diagnostic property |
| **Apply** | 1. `exec.ReadFile("/etc/systemd/system/app.service")` → replace `ExecStart=/opt/app/server` with `ExecStart=/opt/app/DOESNOTEXIST` → `exec.WriteFile("/etc/systemd/system/app.service", modified, 0644, "root", "root")`; 2. `exec.Systemctl("daemon-reload", "")` |
| **Recover** | `exec.RestoreFile("/etc/systemd/system/app.service")` → `exec.Systemctl("daemon-reload", "")` → `exec.Systemctl("restart", "app.service")` |

---

**F-014 — Zombie process accumulation**

| Field | Value |
|---|---|
| Layer | `process` |
| Domain | `linux`, `os` |
| RequiresConfirmation | **true** |
| IsReversible | **false** |
| ResetTier | R3 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Rebuild binary with `FAULT_ZOMBIE_CHILDREN=true` flag; redeploy. App forks child processes without calling `wait()`. |
| Symptom | `ps aux` shows growing count of Z-state processes parented to the app. App continues serving requests. |
| AuthoritativeSignal | `ps -eo pid,ppid,stat,comm \| grep Z` |
| Observable | Zombie count increases with each `/` request; `pstree -p $(pgrep server)` shows zombie children; `ps aux \| grep -c ' Z '` grows over time |
| **Behavioral postcondition** | App serves all endpoints correctly. Zombie accumulation is a resource leak (PID table slots) that grows with each `/` request. The fault is silent to `lab validate` until PID table exhaustion — at which point P-001 will fail, but this condition is not expected to be reached during normal lab exercises. |
| **Failing checks** | [] |
| **Invariant checks** | All 23 checks pass while PID table is not exhausted |
| **Apply** | `exec.RunMutation("go", "build", "-ldflags", "-X main.FaultZombieChildren=true", "-o", "/opt/app/server", "./service")` → `exec.Chown("/opt/app/server", "appuser", "appuser")` → `exec.Chmod("/opt/app/server", 0750)` → `exec.Systemctl("restart", "app.service")` |
| **Recover** | Returns error: `"R3 reset required: run lab reset --tier R3 to rebuild the binary without the fault flag"`. MUST NOT attempt any system mutation. |

---

**F-015 — nginx configuration syntax error**

| Field | Value |
|---|---|
| Layer | `proxy` |
| Domain | `linux`, `networking` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Add invalid directive `invalid_directive on;` to `/etc/nginx/sites-enabled/app`; attempt `sudo nginx -s reload` |
| Symptom | nginx reload fails. Existing nginx worker processes continue with old config. New config is not applied. The old config continues working — nginx does not crash on failed reload. |
| AuthoritativeSignal | `nginx -t` output |
| Observable | `sudo nginx -t` shows configuration error; `sudo nginx -s reload` returns error; `curl localhost/health` → 200 (old config still active) |
| **Behavioral postcondition** | Endpoints continue to work (old config persists). Only the filesystem check F-005 (nginx config validity) fails. Demonstrates nginx's atomic reload: failed reload does not break existing service. |
| **Failing checks** | F-005 (nginx config syntax) |
| **Invariant checks** | S-003, P-003, P-004, E-001, E-002 — all continue working with old config |
| **Apply** | 1. `exec.ReadFile("/etc/nginx/sites-enabled/app")` → append `\ninvalid_directive on;` → `exec.WriteFile("/etc/nginx/sites-enabled/app", modified, 0644, "root", "root")`; 2. attempt `exec.NginxReload()` — this will fail (expected; fault is in the config, not the reload attempt) |
| **Recover** | `exec.RestoreFile("/etc/nginx/sites-enabled/app")` → `exec.NginxReload()` |

---

**F-016 — App binding on all interfaces**

| Field | Value |
|---|---|
| Layer | `socket`, `config` |
| Domain | `linux`, `networking`, `security` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | Change `server.addr` in `/etc/app/config.yaml` from `127.0.0.1:8080` to `0.0.0.0:8080`; `sudo systemctl restart app` |
| Symptom | App is accessible directly on port 8080 from any interface, bypassing nginx and all proxy-layer controls (headers, timeouts, TLS). |
| AuthoritativeSignal | `ss -ltnp` |
| Observable | `ss -ltnp \| grep 8080` shows `0.0.0.0:8080` instead of `127.0.0.1:8080`; direct access bypasses nginx (`X-Proxy: nginx` header absent on direct requests) |
| **Behavioral postcondition** | All nginx-proxied endpoints continue to work. App is additionally exposed directly on all interfaces. nginx proxying is not broken — it is bypassed as an option. P-002 check for `127.0.0.1:8080` fails (app is on `0.0.0.0:8080` instead). |
| **Failing checks** | P-002 (listening on wrong address) |
| **Invariant checks** | S-001, E-001, E-002, E-003, E-004 (nginx still proxying) |
| **Apply** | 1. `exec.ReadFile("/etc/app/config.yaml")` → replace `127.0.0.1:8080` with `0.0.0.0:8080` → `exec.WriteFile("/etc/app/config.yaml", modified, 0640, "appuser", "appuser")`; 2. `exec.Systemctl("restart", "app.service")` |
| **Recover** | `exec.RestoreFile("/etc/app/config.yaml")` → `exec.Systemctl("restart", "app.service")` |

---

**F-017 — Empty APP\_ENV environment variable**

| Field | Value |
|---|---|
| Layer | `service` |
| Domain | `linux`, `os` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| MutationDisplay | `sudo systemctl set-environment APP_ENV=`; `sudo systemctl restart app` (sets APP\_ENV to empty string, overriding unit file value) |
| Symptom | Service fails to start. journald shows "APP\_ENV is empty or missing" error. Structurally identical to F-006 from endpoint perspective, but mechanism differs — unit file is intact, the system-level environment override is the cause. |
| AuthoritativeSignal | journald |
| Observable | `journalctl -u app.service -n 5` shows empty APP\_ENV error; `systemctl show app --property=Environment` shows `APP_ENV=` (empty) overriding unit file value |
| **Behavioral postcondition** | Service does not start. journald error message is the distinguishing signal from F-006 — in F-006, the unit file lacks the directive; in F-017, the directive is present but overridden. |
| **Failing checks** | S-001, E-001, E-002, E-003, E-004, E-005 |
| **Invariant checks** | F-002 (config exists), F-001 (binary exists) |
| **Apply** | 1. `exec.RunMutation("systemctl", "set-environment", "APP_ENV=")` — sets APP_ENV to empty string at the systemd manager level, overriding the unit file value; 2. `exec.Systemctl("restart", "app.service")` |
| **Recover** | 1. `exec.RunMutation("systemctl", "unset-environment", "APP_ENV")` — removes the manager-level override, restoring the unit file value; 2. `exec.Systemctl("restart", "app.service")` |

---

**F-018 — Inode exhaustion**

| Field | Value |
|---|---|
| Layer | `filesystem` |
| Domain | `linux`, `os` |
| RequiresConfirmation | false |
| IsReversible | true |
| ResetTier | R2 |
| Preconditions | [CONFORMANT] |
| PreconditionChecks | [] — no additional check preconditions; Apply fails naturally if inodes are already exhausted |
| Symptom | Filesystem reports inode exhaustion. New files cannot be created despite available data blocks. `df -h` shows available space; `df -i` shows 100% inode usage. |
| AuthoritativeSignal | `df -i` |
| Observable | `df -i /var/lib/app` shows inode usage near 100%; `touch /var/lib/app/test` → "No space left on device" despite `df -h` showing available blocks; app `/` endpoint returns 500 (cannot write state file) |
| **Behavioral postcondition** | App is running but `/` fails (cannot create the state file touch). `/health` continues to return 200. Demonstrates the inode/block distinction: "disk full" can mean block exhaustion or inode exhaustion, and `df -h` vs `df -i` is the diagnostic that distinguishes them. |
| **Failing checks** | E-002 (/ returns 500), F-004 (state dir write fails) |
| **Invariant checks** | S-001, E-001, E-003 — app is alive and config is loaded |
| **Apply** | `exec.RunMutation("bash", "-c", "for i in $(seq 1 100000); do touch /var/lib/app/file_$i; done")` |
| **Recover** | `exec.RunMutation("bash", "-c", "rm -f /var/lib/app/file_*")` — removes all fault-created files; idempotent (no error if files already absent) |

---

## §8 — Model Completeness Condition

The fault model is complete when:

**Forward direction:** every fault in the catalog maps to at least one conformance check ID in `FailingChecks`. Permitted exceptions: F-008 and F-014 (empty `FailingChecks` by design — see §7.1). Baseline behaviors in §10 are not faults and are not subject to this condition.

**Reverse direction:** every conformance check in `conformance-model.md` §3 that references a fault ID in its `Maps to` field has a corresponding entry in this catalog.

**Observability:** every fault has at least one `Observable` command that produces distinguishable output when the fault is active.

**Apply/Recover specification:** every fault catalog entry has complete `Apply` and `Recover` step sequences. Non-reversible faults have `Recover` returning an error directing to R3. No fault leaves the environment in an unrecoverable state.

**ResetTier validity:** every fault's `ResetTier` is one of `R1`, `R2`, `R3`. No other values are permitted.

---

---

## §9 — Authority and References

**This document's authority:** mutation truth. Defines what faults are, how they work, and what they do. The complete fault catalog is the authoritative definition of all faults.

**Depends on:**
- `conformance-model.md` §3 — check IDs used in postcondition `FailingChecks` and `InvariantChecks` fields
- `system-state-model.md` §2 — state names used in fault `Preconditions`

**Relationship to `canonical-environment.md`:** the fault catalog in `canonical-environment.md` §7 MUST remain consistent with this document. This document is authoritative on fault semantics; `canonical-environment.md` is authoritative on environment-specific instantiation details (exact file paths, Ubuntu-specific commands). When the two differ on semantic meaning, this document wins. When they differ on path details, `canonical-environment.md` wins.

**Version consistency:** this document's version MUST match `conformance-model.md` and `system-state-model.md`. The three documents form a coherent versioned set.

---

## §10 — Appendix: Baseline Network Behaviours

These entries document observable properties of the canonical conformant environment that are intentional design decisions, not faults. They cannot be applied via `lab fault apply` because they are not mutations — they are always present. They are documented here because they are used as teaching scenarios in networking problem sets and are referenced from conformance check `Maps to` fields.

**Baseline entries are not faults.** They do not meet the fault definition (§2.1): they introduce no failure cause, do not transition the state to `DEGRADED`, and have no `Apply` or `Recover` functions. The `ResetTier` concept does not apply.

---

**B-001 — nginx proxy timeout shorter than app processing time**

| Field | Value |
|---|---|
| Layer | `network` |
| Domain | `networking` |
| Observable | `time curl -v http://localhost/slow` → 504 in ~3s; `time curl 127.0.0.1:8080/slow` → 200 in ~5s |
| Conformance impact | No checks fail. `lab validate` exits 0. This is intended baseline behavior. |
| Teaching scenario | Demonstrates that proxy timeouts are independent of app response time. nginx `proxy_read_timeout 3s` in `canonical-environment.md` is the canonical value. |
| Note | Previously cataloged as F-011. Removed from fault catalog because it violates the fault definition (§2.1): no mutation, no failure cause, no DEGRADED state. |

---

**B-002 — TLS certificate not in system trust store**

| Field | Value |
|---|---|
| Layer | `network` |
| Domain | `networking`, `security` |
| Observable | `curl -v https://app.local/health` → `SSL certificate problem: self-signed certificate`; `curl -sk https://app.local/health` → 200 |
| Conformance impact | No checks fail. E-005 uses `-k` (skip verify) and passes. F-006 (cert valid) passes. This is an intended baseline condition. |
| Teaching scenario | Demonstrates the distinction between TLS handshake success and certificate verification. Trust installation: `sudo cp /etc/nginx/tls/app.local.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates` |
| Note | Previously cataloged as F-012. Removed from fault catalog for the same reason as B-001. |