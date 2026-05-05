# Control Plane Contract
## Lab Environment Execution Contract
## Version 1.0.0

> **Document class:** execution contract. This document is subordinate to the three semantic model documents. It introduces no independent semantic behavior and may not redefine behavior established by the Conformance, State, or Fault models. When this document conflicts with any semantic model, the semantic model is authoritative.
>
> **Audience:** implementer-primary. This document defines the behavioral contract for the `lab` CLI. Implementation MUST conform to every normative statement here.
>
> **Scope rule:** if a behavior does not affect determinism, recoverability, or auditability, it does not belong in this document. This rule governs all additions and modifications.
>
> **Normative language:** MUST, MUST NOT, SHALL — mandatory. SHOULD — strongly preferred. MAY — permitted.

---

# §1 — Purpose and Scope

This document is the formal execution contract for the `lab` control plane. It specifies the complete behavioral contract for every command: inputs, preconditions, required outcomes, exit codes, state file effects, and audit obligations. It is the authoritative reference for implementation.

`canonical-environment.md` §13 describes the design intent. This document specifies the execution contract. Where they conflict, this document is authoritative on execution behavior; semantic model documents are authoritative on system semantics.

**This document specifies:**
- CLI behavior under every precondition, including failure conditions
- Exit code semantics for all commands
- Output stream contract (what belongs on stdout vs stderr)
- Global flag behavior and mutual exclusion rules
- State file locking semantics
- Signal handling and interrupt grace periods
- Executor behavioral guarantees
- State file schema and mutation rules
- Audit log schema and obligations
- Error catalog (semantic names, conditions, codes, recovery guidance)

**This document does not specify:**
- What "CONFORMANT" means — that is `conformance-model.md`
- What states exist or how transitions work — that is `system-state-model.md`
- What a fault is or what faults exist — that is `fault-model.md`
- Exact human-readable output phrasing or terminal formatting
- Internal executor implementation details that do not affect guarantees
- Exact method signatures of the executor interface

---

# §2 — Derived Authority

## 2.1 Derived Authority Rule

All behavioral authority in this document is derived from the semantic model stack:

```
conformance-model.md   (semantic root)
system-state-model.md  (state and transition definitions)
fault-model.md         (fault and mutation definitions)
        ↓
control-plane-contract.md  (this document — execution derived from above)
        ↓
canonical-environment.md   (instantiation)
```

No statement in this document may introduce a new system state, redefine an existing state, define a new fault, or redefine what conformance means. Where this document requires knowledge of states, transitions, or faults, it references the appropriate semantic model by section.

**Conflict resolution:** if this document and a semantic model document appear to conflict, the semantic model document is authoritative. This document's execution rules are constrained to be consistent with semantic model definitions.

## 2.2 Scope Boundary

The following are within scope for this document:

| In scope | Authority source |
|---|---|
| Exit code table | This document |
| JSON output schemas | This document |
| State file mutation rules | Derived from state model |
| Command preconditions | Derived from state and fault models |
| Lock semantics | This document |
| Signal handling | This document |
| Executor behavioral guarantees | This document |
| Audit log schema | This document |
| Error catalog | This document |

The following are explicitly out of scope:

| Out of scope | Authoritative source |
|---|---|
| Definition of CONFORMANT | `conformance-model.md` §3 |
| State definitions and invariants | `system-state-model.md` §2 |
| Transition validity rules | `system-state-model.md` §3 |
| State detection algorithm | `system-state-model.md` §4 |
| Fault definitions and catalog | `fault-model.md` §7 |
| Fault postcondition specifications | `fault-model.md` §5 |

## 2.3 Stability Boundaries

The following contract elements are **stable** — they may not change without a version increment:

- Exit codes and their assigned meanings
- JSON output schemas (field names, types, nesting)
- State file schema (field names, types, nesting)
- Audit log schema (field names, types, nesting)
- Command precondition requirements
- Lock file location and locking semantics
- Signal handling grace periods

The following are **implementation-defined** and may change without a version increment:

- Human-readable output phrasing and formatting
- Terminal color schemes and alignment
- Internal executor call ordering (provided guarantees are met)
- Progress indicator behavior
- Error message prose (beyond the semantic minimum defined in §8)

---

# §3 — Global Contract

## 3.1 Invocation Model

The control plane is invoked as:

```
lab <command> [subcommand] [arguments] [flags]
```

The binary is installed at `/usr/local/bin/lab`. All commands are subcommands of the `lab` binary. There are no standalone binaries per command.

Unknown commands MUST return exit code 2 with a usage error on stderr.
Missing required arguments MUST return exit code 2 with a usage error on stderr.

## 3.2 Exit Code Table

The following exit codes are used across all commands. Each code has exactly one semantic meaning; no code is overloaded.

| Code | Name | Meaning |
|---|---|---|
| 0 | `Success` | Operation completed; outcome is as expected |
| 1 | `ExecutionFailed` | Operation attempted but failed; environment may be in an intermediate state |
| 2 | `UsageError` | Invalid arguments, unknown command, or missing required input; operation was not attempted |
| 3 | `PreconditionNotMet` | Operation rejected because current state does not satisfy the required precondition |
| 4 | `InterruptedWithSideEffects` | Operation was interrupted after at least one irreversible side effect occurred; control-plane classification is invalidated and MUST be re-established via `lab status` |
| 5 | `ClassificationFailure` | Current environment state cannot be determined; control-plane certainty is lost |

Exit code 4 does NOT assert that the environment is BROKEN. It asserts that control-plane certainty is lost. The actual environment state is unknown until `lab status` reclassifies it via the state detection precedence defined in `system-state-model.md` §4.

## 3.3 Output Stream Contract

**stdout:** carries command output only. In human mode: formatted results. In JSON mode: a single valid JSON object or array per command invocation. stdout MUST NOT contain diagnostic messages, error messages, warnings, or progress indicators.

**stderr:** carries diagnostic output only. Error messages, warnings, progress indicators, and any non-result output MUST be written to stderr. stderr MUST NOT contain command result data that belongs in stdout.

**The two streams MUST NOT be mixed.** A consumer piping stdout to `jq` MUST receive only parseable JSON (in `--json` mode) or only structured result output (in human mode), with no interleaved diagnostic noise.

**Stability guarantee:** JSON schemas on stdout are stable (version-controlled). Human-readable stdout content is stable at the level of conveying the same information; exact phrasing and formatting are implementation-defined.

## 3.4 Global Flags

The following flags apply to all commands. Command-specific flags are defined in §4.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json` | bool | false | Emit JSON on stdout instead of human-readable output |
| `--quiet` | bool | false | Suppress all non-error output (stdout and stderr). Errors still go to stderr. |
| `--verbose` | bool | false | Emit executor operation trace to stderr in addition to normal output |
| `--yes` | bool | false | Suppress interactive confirmation prompts; treat all as confirmed |

**Mutual exclusions:**
- `--json` and `--quiet` are mutually exclusive. Using both MUST produce exit code 2 with a usage error.
- `--verbose` and `--quiet` are mutually exclusive. Using both MUST produce exit code 2 with a usage error.
- `--yes` is compatible with all flags.

**Inheritance:** global flags MUST be accepted before or after the command name:
```
lab --json status       # valid
lab status --json       # valid
lab --json fault apply F-001  # valid
```

## 3.5 State File Locking

The control plane uses an exclusive advisory lock file at `/var/lib/lab/lab.lock` to serialize state-mutating operations.

**Lock acquisition:** MUST be acquired before any operation that writes to `state.json` or executes system mutations. Lock acquisition is the first step of any mutating command.

**Lock scope:** the lock protects control-plane state mutation authority — specifically, `state.json` writes and the sequence of executor operations that constitute a transition. It does NOT guarantee system-level exclusivity outside control-plane operations. External tools (systemctl, chmod, etc.) may still mutate the system while the lock is held.

**Lock failure:** if the lock cannot be acquired because another `lab` instance holds it, the command MUST fail immediately with exit code 1 and error `ErrLockHeld`. The command MUST NOT wait for the lock.

**Lock release:** the lock MUST be released when the command exits, whether by normal completion or signal handling. Lock files MUST NOT persist after process exit. If the process is killed (SIGKILL, crash), the lock file remains but is stale — subsequent invocations MUST detect a stale lock (process no longer running) and proceed.

**Stale lock detection:** the lock file contains the PID of the holding process. On acquisition failure, the control plane checks whether that PID is still running. If not, the stale lock is removed and acquisition proceeds.

**Read-only commands exempt:** `lab status`, `lab validate`, `lab fault list`, `lab fault info`, and `lab history` do not acquire the lock. They may read `state.json` while a mutating command holds the lock; they will observe a consistent or partially-written state and MUST handle both gracefully.

## 3.6 Signal Handling

The control plane MUST handle SIGINT and SIGTERM during execution.

**Signal receipt behavior:**
1. The control plane marks the signal as received.
2. The current executor operation is allowed to complete normally.
3. No further executor operations are started.
4. If any executor operations were completed before the signal was received (irreversible side effects exist), the control plane records the interruption in the audit log and exits with code 4.
5. If no executor operations were completed before the signal, the control plane exits with code 0 (operation was not started) or 1 (operation started but made no mutations).
6. `state.json` is NOT updated to BROKEN. The control-plane classification is invalidated by setting `state.json` `classification_valid: false`. The `state` field retains its last known value for diagnostic purposes.

**Interrupt grace periods by operation class:**

| Operation class | Grace period | Applies to |
|---|---|---|
| Standard operations | 30 seconds | `fault apply`, `reset --tier R1/R2`, `validate` |
| Long-running operations | 120 seconds | `provision`, `reset --tier R3` |

If the current executor operation has not completed within the grace period, it is abandoned. The control plane records the forced abandonment in the audit log and exits with code 4.

**After interrupt exit code 4:** `lab status` MUST be run to re-establish classification via the state detection precedence defined in `system-state-model.md` §4. The `classification_valid: false` flag in `state.json` causes `lab status` to run the full detection algorithm rather than trusting the cached state.

## 3.7 Concurrent Invocation

If two `lab` invocations are started simultaneously:
- The first to acquire the lock proceeds normally.
- The second fails immediately with exit code 1 and error `ErrLockHeld`.
- The second invocation MUST NOT wait, retry, or queue.

Concurrent read-only invocations (status, validate, history, fault list/info) are permitted and may execute simultaneously without coordination.

---

# §4 — Command Contracts

Each command contract uses the following structure:
- **Synopsis:** usage line
- **Arguments and flags:** types, defaults, validation
- **Preconditions:** state requirements checked before execution
- **Execution contract:** required outcomes and ordering constraints
- **State file effects:** which fields are written, when, under what conditions
- **Exit codes:** complete mapping for this command
- **Audit entries:** what is logged and when
- **Error conditions:** by semantic name (defined in §8)

---

## 4.1 `lab status`

**Synopsis:** `lab status [--json] [--quiet]`

**Arguments and flags:** none beyond global.

**Preconditions:** none. This command MUST execute in any environment state including UNKNOWN.

**Reconciliation authority:** `lab status` is the only command authorized to reconcile observed runtime reality with authoritative control-plane classification. It MUST run the state detection algorithm defined in `system-state-model.md` §4.2 and update `state.json` if the detected state differs from the recorded state.

**Execution contract:**

1. Acquire NO lock (read-path command with reconciliation authority).
2. Read `state.json`. If unreadable or unparseable, proceed with recorded_state = UNKNOWN.
3. Execute lightweight runtime checks (P-001, P-002, S-003, E-001 equivalents) — these are NOT the full conformance suite.
4. Apply the conflict resolution rules from `system-state-model.md` §4.3.
5. If detected state differs from recorded state: update `state.json` with the detected state, record the reconciliation in audit log.
6. If `classification_valid: false` in `state.json` (interrupt recovery): run full detection algorithm; update `classification_valid: true` after completion.
7. Render output.

**State file effects:**
- MAY update `state` field if runtime observation contradicts recorded state
- MAY update `classification_valid` from false to true after interrupt recovery
- MUST update `last_status_at` timestamp
- MUST NOT update `active_fault` (fault state is reconciled, not reset)

**Exit codes:**
- 0: state determined and rendered successfully
- 5: state detection produced UNKNOWN (contradictory evidence; operator must investigate)
- 2: usage error (invalid flags)

**Audit entries:**
- One entry if reconciliation occurred (recorded_state → detected_state)
- No entry for read-without-change

**Error conditions:** `ErrClassificationContradiction` (exit 5)

---

## 4.2 `lab validate [--check <ID>]`

**Synopsis:** `lab validate [--check <ID>] [--json] [--quiet]`

**Arguments and flags:**
- `--check <ID>`: run a single check by ID (e.g., `--check E-001`). Does not update state. Does not write audit entry.

**Preconditions:** none. Validate may run in any state.

**Observation-only rule:** `lab validate` is an observation primitive, not a state mutation primitive. It records observations; it does NOT update the authoritative state classification. Only `lab status` reconciles state. This rule MUST be enforced regardless of whether the validation result would change the classification.

**Execution contract:**

1. Acquire NO lock (observation-only).
2. Execute all conformance suite checks in the order defined in `conformance-model.md` §3 (S-series, P-series, E-series, F-series, L-series).
3. Each check MUST be executed even if a prior check fails (no early abort on blocking check failure — the full picture is required).
4. Exception: if S-001 (app service active) fails, E-series checks SHOULD still be attempted but their failure is expected and noted as a dependent failure.
5. Record the complete result set in `state.json` `last_validate` field.
6. Write one audit entry with the full result set.
7. Render output.

**State file effects:**
- MUST update `last_validate` (timestamp, passed count, total count, per-check results)
- MUST NOT update `state` field
- MUST NOT update `active_fault` field
- MUST NOT update `classification_valid` field

**Exit codes:**
- 0: all blocking checks passed
- 1: at least one blocking check failed
- 2: usage error (unknown check ID for `--check`)

**Audit entries:**
- One entry per full suite run: check count, passing count, failing check IDs
- No entry for `--check <ID>` single-check runs

**Error conditions:** `ErrUnknownCheckID` (exit 2, only for `--check`)

---

## 4.3 `lab fault list`

**Synopsis:** `lab fault list [--json] [--quiet]`

**Arguments and flags:** none beyond global.

**Preconditions:** none.

**Execution contract:** read the embedded fault catalog. Render the fault list. No system interaction. No lock required.

**State file effects:** none.

**Exit codes:**
- 0: always (catalog is always available)
- 2: usage error

**Audit entries:** none.

---

## 4.4 `lab fault info <ID>`

**Synopsis:** `lab fault info <ID> [--json] [--quiet]`

**Arguments and flags:**
- `<ID>`: required. A fault ID from the catalog (e.g., `F-004`).

**Preconditions:** none.

**Execution contract:** look up the fault ID in the embedded catalog. If not found, return `ErrUnknownFaultID`. Otherwise render the full fault entry. No system interaction. No lock required.

**State file effects:** none.

**Exit codes:**
- 0: fault found and rendered
- 1: fault ID not found in catalog (`ErrUnknownFaultID`)
- 2: usage error (missing ID argument)

**Audit entries:** none.

---

## 4.5 `lab fault apply <ID> [--force] [--yes]`

**Synopsis:** `lab fault apply <ID> [--force] [--yes] [--json] [--quiet] [--verbose]`

**Arguments and flags:**
- `<ID>`: required. A fault ID from the fault catalog.
- `--force`: bypass the state precondition guard. Permits application when state is not CONFORMANT. Records `forced: true` in audit and state file. SHOULD produce a prominent warning on stderr.
- `--yes`: suppress confirmation prompt for faults with `RequiresConfirmation: true`.

**Preconditions (checked in order, each failure producing a distinct error):**
1. Fault ID exists in the embedded catalog. Failure: `ErrUnknownFaultID` (exit 2).
2. Current state is CONFORMANT (unless `--force`). Failure: `ErrPreconditionNotMet` (exit 3).
3. No fault is currently active (unless `--force`). Failure: `ErrFaultAlreadyActive` (exit 3).
4. Fault-specific preconditions from the fault catalog entry. Failure: `ErrFaultPreconditionFailed` (exit 3).
5. If `RequiresConfirmation: true` and `--yes` not provided: display `MutationDisplay` and prompt for confirmation. User decline: exit 0 with message "Aborted by user."

**Execution contract:**

1. Acquire mutation lock. Failure: `ErrLockHeld` (exit 1).
2. Re-read `state.json` to confirm preconditions still hold (TOCTOU guard).
3. Execute the fault's `Apply` function through the executor.
4. If `Apply` returns error: do NOT update state file. Write audit entry with failure. Exit 1.
5. If `Apply` succeeds: atomically update `state.json` — transition to DEGRADED, record fault ID, `applied_at` timestamp, `forced` flag.
6. OPTIONAL: run the fault's `FailingChecks` from the postcondition to verify the fault is active. If verification fails, write a warning to stderr but DO NOT revert — the mutation has occurred.
7. Release lock.
8. Render output.

**Atomicity guarantee:** the transition to DEGRADED is recorded only if `Apply` returns without error. Partial mutation without state commit leaves the environment in an unrecorded non-conformant state — this is reported as exit 1 (execution failed, no state record was made) rather than exit 4 (interrupted with side effects), because the control plane never recorded a transition. The environment's actual classification must be established by running `lab status`. This derives from `system-state-model.md` §3.5 (transition failure semantics: failure before state commit does not produce a recorded state change).

**State file effects (on success):**
- `state` → DEGRADED
- `active_fault.id` → fault ID
- `active_fault.applied_at` → current timestamp
- `active_fault.forced` → `--force` flag value
- `last_fault_apply` → summary record

**Exit codes:**
- 0: fault applied successfully (or user aborted confirmation)
- 1: fault application failed (executor error or lock error)
- 2: usage error or unknown fault ID
- 3: precondition not met (state guard, no active fault guard, fault-specific precondition)
- 4: fault application succeeded but interrupted before state recording completed

**Audit entries:**
- One entry per executor operation within Apply
- One state transition entry: CONFORMANT → DEGRADED (or failure record)

**Error conditions:** `ErrUnknownFaultID`, `ErrPreconditionNotMet`, `ErrFaultAlreadyActive`, `ErrFaultPreconditionFailed`, `ErrLockHeld`, `ErrApplyFailed`

---

## 4.6 `lab reset [--tier R1|R2|R3]`

**Synopsis:** `lab reset [--tier R1|R2|R3] [--json] [--quiet] [--verbose]`

**Arguments and flags:**
- `--tier`: optional. `R1`, `R2`, or `R3`. If not provided, the control plane selects the appropriate tier automatically (see tier selection below).

**Preconditions:** none. Reset MUST execute in any state.

**Idempotency:** `lab reset` on a CONFORMANT environment MUST run the conformance suite, confirm all blocking checks pass, and exit 0. It MUST NOT make system modifications if the environment is already conformant. Re-running reset is always safe.

**Tier selection (when `--tier` is not specified):**

| Current state | Active fault `ResetTier` | Selected tier |
|---|---|---|
| DEGRADED | R1 | R1 |
| DEGRADED | R2 | R2 |
| DEGRADED | R3 | R3 |
| BROKEN | any | R2 (minimum safe default) |
| CONFORMANT | none | none (no-op after validation) |
| UNKNOWN | any | R2 (minimum safe default) |

The operator MAY override tier selection with `--tier`. If `--tier R1` is specified for an R2 fault, the control plane MUST warn on stderr that the selected tier is below the fault's recommended tier, but MUST proceed.

**Execution contract:**

1. Acquire mutation lock. Failure: `ErrLockHeld` (exit 1).
2. Read current state and active fault from `state.json`.
3. If fault is active and `IsReversible: true`: call the fault's `Recover` function first, then execute the reset tier operations.
4. If fault is active and `IsReversible: false`: skip `Recover`, execute R3 operations directly.
5. Execute reset tier operations through the executor (ordered — see tier operation requirements below).
6. Run the full conformance suite (equivalent to `lab validate`).
7. If all blocking checks pass: update `state.json` — transition to CONFORMANT, clear `active_fault`, record reset tier and timestamp.
8. If any blocking check fails: update `state.json` — transition to BROKEN, clear `active_fault`, record failed checks.
9. Release lock.
10. Render output.

**Tier operation requirements:**

*R1 — service restart:*
- Restart `app.service` via executor
- Reload nginx via executor
- Ordering: app restart before nginx reload

*R2 — canonical configuration restore + R1:*
- Restore all files listed in `canonical-environment.md` §2.3 to their canonical contents, ownership, and modes (via executor)
- Execute `systemctl daemon-reload` via executor (required after unit file restore)
- Execute R1 operations

*R3 — full reprovision:*
- Execute the bootstrap script via executor
- Bootstrap script includes R2 operations and binary recompilation
- Post-bootstrap: the bootstrap script itself runs the conformance suite; `lab reset` does not run it again

**Post-reset validation:** after R1 and R2 operations, the full conformance suite MUST be run. The reset's exit code reflects the validation result, not just the execution result. A reset that successfully restored all files but still has a failing check exits 1.

**State file effects (on successful validation):**
- `state` → CONFORMANT
- `active_fault` → null
- `last_reset` → tier, timestamp, from-state, fault cleared

**Exit codes:**
- 0: reset completed and conformance suite passes
- 1: reset operations failed or post-reset validation failed
- 2: usage error (unknown tier value)
- 4: reset interrupted after side effects

**Audit entries:**
- One entry per executor operation
- One state transition entry: (prior state) → CONFORMANT or BROKEN

**Error conditions:** `ErrLockHeld`, `ErrResetOperationFailed`, `ErrPostResetValidationFailed`

---

## 4.7 `lab provision`

**Synopsis:** `lab provision [--json] [--quiet] [--verbose]`

**Arguments and flags:** none beyond global.

**Preconditions:** none. Provision MAY run in any state, including UNPROVISIONED.

**Idempotency:** `lab provision` on a CONFORMANT environment MUST converge — recompile and reinstall the binary, restore canonical configs, restart services if config changed. MUST NOT truncate `app.log`. MUST NOT remove `/var/lib/app/state`. MUST NOT clear the audit log. The provision is complete when the conformance suite passes.

**Execution contract:**

1. Acquire mutation lock. Failure: `ErrLockHeld` (exit 1).
2. Execute bootstrap operations through the executor.
3. Bootstrap operations include (in order): package installation, user creation, directory creation, config file installation, binary compilation, binary installation, TLS certificate generation (if absent or expired), nginx configuration, systemd unit installation, service enable and start.
4. Each step is idempotent: if a step finds the target state already matches canonical, it is a no-op.
5. Run the full conformance suite.
6. If all blocking checks pass: update `state.json` — transition to CONFORMANT.
7. If any blocking check fails: update `state.json` — transition to BROKEN.
8. Release lock.
9. Render output.

**State file effects:**
- `state` → CONFORMANT or BROKEN
- `active_fault` → null (provision clears any active fault record)
- `last_provision` → timestamp, result

**Exit codes:**
- 0: provision completed and conformance suite passes
- 1: provision failed or post-provision validation failed
- 4: provision interrupted after side effects (long-running operation, 120-second grace period)

**Audit entries:**
- One entry per executor operation group (packages, users, files, binary, services)
- One state transition entry: (prior state) → CONFORMANT or BROKEN

**Error conditions:** `ErrLockHeld`, `ErrProvisionStepFailed`, `ErrPostProvisionValidationFailed`

---

## 4.8 `lab history [--last <N>]`

**Synopsis:** `lab history [--last <N>] [--json] [--quiet]`

**Arguments and flags:**
- `--last <N>`: show the N most recent entries. Default: 20. Maximum: 50 (the history buffer size).

**Preconditions:** none.

**Execution contract:** read the `history` array from `state.json`. Render the most recent N entries in reverse chronological order (most recent first). No system interaction. No lock required.

**State file effects:** none.

**Exit codes:**
- 0: always (empty history is not an error)
- 2: usage error (invalid `--last` value)

**Audit entries:** none.

**Error conditions:** `ErrStateFileUnreadable` (exit 1 if state.json cannot be read)

---

# §5 — Executor Behavioral Contract

The executor is the single layer through which all system mutations pass. No command implementation may call system mutation primitives directly. This section defines the behavioral guarantees the executor MUST provide.

## 5.1 Required Capabilities

The executor MUST provide the following capabilities. This document defines the required behavior; the exact interface signatures are implementation-defined.

**File write:** write content to a path with specified mode bits. MUST be atomic (temp file + rename on same filesystem). MUST set ownership after write. Failure: path is not modified.

**Mode change:** change mode bits on an existing path. MUST NOT change content or ownership. Failure: path mode is not changed.

**Ownership change:** change owner and group of an existing path. MUST NOT change content or mode bits. Failure: ownership is not changed.

**Canonical restore:** restore a path to its canonical content, ownership, and mode using the canonical content map embedded in the binary. Equivalent to file write with the canonical content. Failure: path is not modified.

**Service action:** execute a systemd operation (start, stop, restart, enable, disable, daemon-reload) on a named unit. MUST wait for the operation to complete (synchronous). Failure: unit state is unchanged.

**nginx reload:** reload nginx configuration. MUST verify config syntax before reloading (`nginx -t`). MUST NOT reload if syntax check fails. Failure: nginx config is unchanged, existing worker processes continue.

**Privileged execution:** execute an arbitrary command with elevated privileges (sudo). MUST be used only for operations not covered by the above capabilities. MUST log the full command and arguments in the audit log before execution.

## 5.2 Audit Obligation

Every executor operation MUST produce an audit log entry before execution completes. The audit log entry MUST be written even if the operation fails. No executor operation may occur without a corresponding audit record.

## 5.3 Ordering Guarantee

Executor operations within a single command execution are strictly ordered. The second operation does not begin until the first completes or fails. There is no concurrent execution of executor operations within a single command.

## 5.4 Failure Semantics

An executor operation failure is non-retrying. If an operation fails, the executor returns the error to the calling command. The calling command is responsible for determining whether the partial state constitutes a BROKEN environment or a recoverable condition. The executor does not perform automatic rollback.

## 5.5 Privilege Model

The executor performs privileged operations via `sudo`. The calling process runs as `devuser`; the existing `devuser` sudoers rule covers all required operations. The executor MUST NOT store credentials, MUST NOT cache sudo authentication tokens beyond the natural sudo cache lifetime, and MUST NOT elevate privilege for operations that do not require it.

---

# §6 — State File Contract

## 6.1 Schema

The state file at `/var/lib/lab/state.json` conforms to the following schema. All fields are present in every write; no field is optional.

```json
{
  "spec_version": "<string — version of this contract document>",
  "state": "<string — one of: UNPROVISIONED, PROVISIONED, CONFORMANT, DEGRADED, BROKEN, RECOVERING>",
  "classification_valid": "<boolean — false if last operation was interrupted>",
  "active_fault": {
    "id": "<string — fault ID, e.g. F-004>",
    "applied_at": "<string — RFC3339 timestamp>",
    "forced": "<boolean>"
  },
  "last_validate": {
    "at": "<string — RFC3339 timestamp>",
    "passed": "<integer>",
    "total": "<integer>",
    "failing_checks": ["<string — check ID>"]
  },
  "last_reset": {
    "at": "<string — RFC3339 timestamp>",
    "tier": "<string — R1, R2, or R3>",
    "from_state": "<string — state before reset>",
    "fault_cleared": "<string — fault ID or null>"
  },
  "last_provision": {
    "at": "<string — RFC3339 timestamp>",
    "result": "<string — CONFORMANT or BROKEN>"
  },
  "last_status_at": "<string — RFC3339 timestamp>",
  "history": [
    {
      "ts": "<string — RFC3339 timestamp>",
      "from": "<string — prior state>",
      "to": "<string — resulting state>",
      "command": "<string — full command invocation>",
      "fault": "<string — fault ID or null>",
      "forced": "<boolean>"
    }
  ]
}
```

**Null values:** when `active_fault` is null (no fault active), the field is present with value `null`. When `last_reset`, `last_provision`, or `last_validate` have never occurred, those fields contain `null`.

**History buffer:** the `history` array is a bounded ring buffer with a maximum of 50 entries. When a new entry is added that would exceed 50 entries, the oldest entry is removed.

## 6.2 Atomic Write Requirement

Every write to `state.json` MUST be atomic. The required method:

1. Write the complete new content to a temporary file in the same directory as `state.json` (same filesystem — ensures rename is atomic).
2. Set the temporary file's mode to `644` and ownership to `root:root`.
3. Rename the temporary file to `state.json` (atomic replacement).

A partial write to `state.json` (crash between write and rename) leaves the temporary file behind; `state.json` retains its prior complete content. Recovery: the next `lab` invocation finds `state.json` intact.

## 6.3 Lock Relationship

Every write to `state.json` MUST be performed while holding the mutation lock (`/var/lib/lab/lab.lock`). `lab status` is the only exception: it MAY update `state.json` (reconciliation) without holding the mutation lock. `lab status` MUST use the atomic write method regardless.

## 6.4 Schema Version Handling

If the `spec_version` in `state.json` does not match the `lab` binary's version:

- Minor version difference: proceed normally; log a warning to stderr.
- Major version difference: refuse to execute mutating commands; exit 1 with error `ErrSchemaVersionMismatch`. Read-only commands (status, history, fault list/info) MAY proceed with a prominent warning.

## 6.5 Corruption Recovery

If `state.json` is missing or cannot be parsed:

- `lab status`: set detected state to UNKNOWN; display error on stderr; prompt operator to run full detection.
- All other commands: set effective state to UNKNOWN before precondition checks. Commands that require a known state will fail their precondition check with `ErrClassificationFailure`.
- Resolution: `lab status` runs the full state detection algorithm and writes a fresh `state.json`.

---

# §7 — Audit Log Contract

## 7.1 Location and Rotation Policy

The audit log is at `/var/lib/lab/audit.log`. It is append-only and MUST NOT be truncated or rotated by any `lab` operation, including R3 reset and `lab provision`. The audit log persists across all reset tiers, including full reprovision.

Ownership: `root:root`, mode `644` — readable by `devuser` without sudo.

## 7.2 Entry Schema

Every audit log entry is a newline-delimited JSON object conforming to the following schema:

```json
{
  "ts": "<string — RFC3339 timestamp with millisecond precision>",
  "entry_type": "<string — see §7.3>",
  "command": "<string — full lab command invocation, e.g. 'lab fault apply F-004'>",
  "fault_id": "<string — active fault ID at time of entry, or null>",
  "op": "<string — executor operation name, or null for non-executor entries>",
  "op_args": "<string — executor operation arguments summary, or null>",
  "exit_code": "<integer — operation exit code, or null for non-executor entries>",
  "duration_ms": "<integer — operation duration in milliseconds>",
  "error": "<string — error message if failed, or null>"
}
```

## 7.3 Entry Types

| `entry_type` | Description | Produced by |
|---|---|---|
| `executor_op` | A single executor capability invocation | Every executor operation |
| `state_transition` | A state machine transition (from → to) | `fault apply`, `reset`, `provision` (on success or failure) |
| `validation_run` | Full conformance suite result | `validate`, post-reset validation |
| `reconciliation` | State reconciliation (observed ≠ recorded) | `status` when state changes |
| `interrupt` | Command interrupted by signal | Signal handler |
| `error` | Command-level error (precondition failure, etc.) | Any command on exit code > 0 |

## 7.4 Ordering Guarantee

Audit log entries within a single command execution are written in the order operations occurred. Each entry is written to the log before the next operation begins. Entries from concurrent read-only commands (status, validate) may interleave with each other but MUST NOT interleave with entries from a mutating command holding the lock.

---

# §8 — Error Catalog

Each error is identified by a semantic name. The error name is the stable identifier; error message prose is implementation-defined beyond the semantic minimum described here.

| Error name | Condition | Exit code | Semantic minimum on stderr | Recovery guidance class |
|---|---|---|---|---|
| `ErrUnknownFaultID` | Fault ID not found in embedded catalog | 2 | "Unknown fault ID: <ID>" | Show `lab fault list` |
| `ErrUnknownCheckID` | Check ID not found in conformance model | 2 | "Unknown check ID: <ID>" | No further action |
| `ErrPreconditionNotMet` | State precondition guard rejected the command | 3 | "Current state is <STATE>; <COMMAND> requires <REQUIRED_STATE>" | Show reset or apply path |
| `ErrFaultAlreadyActive` | `lab fault apply` attempted while fault is active | 3 | "Fault <ID> is currently active" | Show `lab reset` |
| `ErrFaultPreconditionFailed` | Fault-specific precondition not satisfied | 3 | "Fault <ID> precondition not met: <CONDITION>" | Show fault-specific remediation |
| `ErrLockHeld` | Mutation lock is held by another `lab` process | 1 | "Another lab operation is in progress (PID <PID>)" | Wait and retry |
| `ErrApplyFailed` | Fault `Apply` function returned an error | 1 | "Fault application failed: <ERROR>" | Show `lab reset` |
| `ErrResetOperationFailed` | A reset tier operation returned an error | 1 | "Reset operation failed: <OPERATION>: <ERROR>" | Show `lab reset --tier R3` if R2 failed |
| `ErrPostResetValidationFailed` | Post-reset conformance suite has failing checks | 1 | "Reset completed but validation failed: <FAILING_CHECKS>" | Show `lab validate` for detail |
| `ErrProvisionStepFailed` | A provision step returned an error | 1 | "Provision step failed: <STEP>: <ERROR>" | Manual intervention or retry |
| `ErrPostProvisionValidationFailed` | Post-provision conformance suite has failing checks | 1 | "Provision completed but validation failed: <FAILING_CHECKS>" | Show `lab validate` for detail |
| `ErrClassificationFailure` | State cannot be determined; contradictory evidence | 5 | "Environment state cannot be determined: <REASON>" | Run `lab status` for full detection |
| `ErrClassificationContradiction` | `lab status` detection produced contradictory evidence | 5 | "State detection produced contradictory evidence: <DETAILS>" | Manual investigation required |
| `ErrStateFileUnreadable` | `state.json` cannot be read or parsed | 1 | "State file is unreadable or corrupt" | Run `lab status` to regenerate |
| `ErrSchemaVersionMismatch` | `state.json` spec_version differs from binary version | 1 | "State file schema version mismatch: file=<V1> binary=<V2>" | Run `lab provision` to upgrade |

**Error message stability:** the `Semantic minimum on stderr` column defines the required content of the error message. The exact phrasing, punctuation, and surrounding context are implementation-defined. The semantic content (state names, IDs, error descriptions) is stable.

---

# §9 — Conformance with Semantic Models

This section documents which behaviors in this contract are derived from each semantic model and what would constitute a violation of the derivation relationship.

## 9.1 Conformance Model Derivation

**Derived behaviors:**
- §4.2 (validate): the check execution sequence follows `conformance-model.md` §4.2 (check ordering and dependencies)
- §4.2 (validate): the blocking vs degraded severity distinction (§4.3 of validate) derives from `conformance-model.md` §3.1 (check severity field)
- §4.6 (reset): post-reset validation follows the same semantics as `conformance-model.md` §4 (validation semantics)

**Violation conditions:** this contract violates the conformance model derivation if it specifies validation behavior that contradicts `conformance-model.md` §4 (e.g., if this document allowed early abort on first failing check while the conformance model requires full suite execution).

## 9.2 State Model Derivation

**Derived behaviors:**
- §4.1 (status): reconciliation algorithm derives from `system-state-model.md` §4.2 (detection algorithm) and §4.3 (conflict resolution)
- §4.5 (fault apply): precondition state check derives from `system-state-model.md` §3.3 (transition guards)
- §4.6 (reset): tier selection logic and post-reset state derive from `system-state-model.md` §3.3 and §5.4 (liveness)
- §3.6 (signal handling): "classification invalidated, not BROKEN" derives from `system-state-model.md` §2.5 (BROKEN invariants) — an interrupted operation is not BROKEN by definition

**Violation conditions:** this contract violates the state model derivation if it specifies a transition that is forbidden by `system-state-model.md` §3.4, or if it asserts BROKEN for a condition that does not satisfy BROKEN's invariants (at least one blocking check fails, no active fault).

## 9.3 Fault Model Derivation

**Derived behaviors:**
- §4.5 (fault apply): the confirmation behavior derives from `fault-model.md` §3.1 (`RequiresConfirmation` field)
- §4.5 (fault apply): the atomicity guarantee derives from `fault-model.md` §4.2 (Apply atomicity)
- §4.5 (fault apply): postcondition verification uses `fault-model.md` §5.3 (postcondition specification)
- §4.6 (reset): `IsReversible` routing derives from `fault-model.md` §6 (reversibility semantics)

**Violation conditions:** this contract violates the fault model derivation if it permits fault stacking without `--force` (violates the one-fault constraint in `fault-model.md` §2.2), or if it records DEGRADED state without the fault ID (violates `fault-model.md` §2.3 definition).

---

*End of Contract.*
*Contract version: 1.0.0*
*This document introduces no system semantics. All semantic authority resides in the three model documents.*

