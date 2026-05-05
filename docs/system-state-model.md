# System State Model
## Lab Environment Semantic Model — Layer 2 of 3
## Version 1.0.0

> **Authority:** This document defines the finite set of states the lab environment can occupy, the valid and forbidden transitions between them, and the algorithm for determining current state. It depends on `conformance-model.md` for the definition of CONFORMANT and is depended on by `fault-model.md` for fault precondition and postcondition specifications. When this document conflicts with `fault-model.md` or `canonical-environment.md`, this document is authoritative. When this document conflicts with `conformance-model.md`, `conformance-model.md` is authoritative.
>
> **Audience:** implementer-primary. Normative rules first; rationale where ambiguity would otherwise exist.
>
> **Normative language:** MUST, MUST NOT, SHALL — mandatory. SHOULD — strongly preferred. MAY — permitted.

---

## §1 — Purpose

This document defines the state machine governing the lab environment. Every operation the `lab` control plane performs is a transition in this machine. The state machine provides the formal basis for:

- Determining which operations are permitted at any given moment
- Defining what the `lab` tool's commands actually do (transition operators, not features)
- Specifying what recovery means in each non-conformant state

**Dependency relationship:**

```
conformance-model.md  →  system-state-model.md  →  fault-model.md
(defines CONFORMANT)     (this document)             (uses state preconditions)
```

CONFORMANT is not defined in this document. It is defined in `conformance-model.md` §3 and referenced here. This document classifies conformance outcomes into states; it does not define what conformance means.

---

## §2 — State Definitions

The environment is always in exactly one of six states. States are mutually exclusive and collectively exhaustive over all reachable environment conditions.

### 2.1 UNPROVISIONED

The VM exists but the bootstrap process has not been run. No canonical files, users, services, or the `lab` binary itself may be present.

**Invariants:**
- `systemctl is-active app` returns `inactive` or `not-found`
- `/opt/app/server` does not exist

**Valid transitions out:** `PROVISIONED` (via `lab provision`)
**Valid transitions in:** none (initial state only)
**Permitted operations:** `lab provision` only

### 2.2 PROVISIONED

The bootstrap script has completed but conformance has not been verified. The environment may be conformant, but this has not been established. This state is transitional.

**Invariants:**
- The bootstrap script exited 0
- No guarantee on conformance suite result

**Valid transitions out:** `CONFORMANT` (if conformance suite passes), `BROKEN` (if conformance suite fails)
**Valid transitions in:** `UNPROVISIONED` (via provision)
**Permitted operations:** `lab validate`, `lab status`, `lab history`
**Note:** `lab provision` automatically runs validation after bootstrap. The environment should not remain in PROVISIONED longer than the time it takes validation to complete.

### 2.3 CONFORMANT

All blocking conformance checks pass. The environment matches the canonical specification in both behavioral and structural dimensions. This is the required precondition for fault injection and the target state for all reset operations.

**Invariants (all MUST hold simultaneously):**
- All S-series checks pass
- All P-series checks pass
- All E-series checks pass
- All F-series blocking checks pass
- `state.json` `active_fault` is null
- Formally: `lab validate` exits 0 AND no active fault recorded

**Valid transitions out:** `DEGRADED` (via `lab fault apply`), `BROKEN` (if external change breaks conformance)
**Valid transitions in:** `PROVISIONED` (first validation), `RECOVERING` (after successful reset)
**Permitted operations:** all operations

**Degraded-conformant sub-classification:** a CONFORMANT environment with one or more degraded-severity checks failing is classified as conformant with a degraded note. This sub-classification does not change permitted operations. See `conformance-model.md` §4.3.

### 2.4 DEGRADED

A fault from the catalog has been deliberately applied. The environment is intentionally non-conformant in a specific, documented way. Exactly one fault is active.

**Invariants (all MUST hold simultaneously):**
- Exactly one fault ID is recorded in `state.json` `active_fault`
- The `active_fault` ID exists in the fault catalog
- The fault's postcondition holds: the conformance checks specified in the fault's postcondition are failing
- `applied_at` timestamp is recorded in `state.json`

**Valid transitions out:** `RECOVERING` (via `lab reset`)
**Valid transitions in:** `CONFORMANT` (via `lab fault apply`)
**Permitted operations:** `lab status`, `lab validate`, `lab reset`, `lab history`, `lab fault info`
**Forbidden operations:** `lab fault apply` without `--force`. Rationale: stacking faults produces overlapping symptoms that violate the one-fault-one-chain-one-diagnosis model. The fault is a pedagogical instrument; multiple simultaneous faults make the causal chain ambiguous and undiagnosable.

### 2.5 BROKEN

One or more blocking conformance checks fail due to unintended modification, build failure, provisioning error, or manual intervention. No fault is active — this non-conformance was not deliberately applied through the control plane.

**Invariants:**
- At least one blocking conformance check fails
- `state.json` `active_fault` is null
- The environment is recoverable through a defined reset tier

**Distinction from DEGRADED:** DEGRADED has a recorded fault ID and a documented fault postcondition. BROKEN has no fault ID. The non-conformance in BROKEN is either unknown in origin or known to be unintended. This distinction determines the recovery path — DEGRADED recovery uses the fault's `Recover` function; BROKEN recovery uses the appropriate reset tier without a fault-specific recovery step.

**Valid transitions out:** `RECOVERING` (via `lab reset`)
**Valid transitions in:** `CONFORMANT` or `DEGRADED` (via external modification detected by `lab validate`)
**Permitted operations:** `lab status`, `lab validate`, `lab reset`, `lab history`
**Forbidden operations:** `lab fault apply`. Rationale: applying a fault on a broken environment produces a state where fault symptoms and pre-existing breakage are indistinguishable. Recovery path is also undefined.

### 2.6 RECOVERING

A reset operation is in progress. This state is transitional — the environment is actively being modified to return to CONFORMANT.

**Invariants:**
- A reset operation is executing
- The environment is in an intermediate modification state
- Duration is bounded (seconds to minutes depending on reset tier)

**Valid transitions out:** `CONFORMANT` (reset succeeds and validation passes), `BROKEN` (reset fails)
**Valid transitions in:** `DEGRADED` (via `lab reset`), `BROKEN` (via `lab reset`)
**Permitted operations:** none (the control plane is executing; concurrent operations are rejected)

---

## §3 — Transition Model

### 3.1 What a Transition Is

A transition is an atomic operation that:

1. Verifies the precondition (current state matches the required from-state)
2. Executes mutations through the executor (no direct system calls)
3. Records the new state in `state.json` atomically with mutation completion
4. Runs post-transition validation where specified
5. Returns the resulting state

**Logical atomicity:** transitions are logically atomic, not syscall-atomic. The guarantee is: a fault is considered applied only when mutation succeeds AND state recording commits. Partial mutation without state commit MUST NOT result in a DEGRADED state record. If mutation succeeds but state recording fails, the system is BROKEN (non-conformant, no fault record).

**Synchronous execution:** the `lab` tool does not return to the prompt until the transition is complete and the resulting state is recorded. There is no background execution, no eventual consistency.

### 3.2 Transition Schema

Each transition is defined by the following fields:

| Field | Description |
|---|---|
| **From** | Required current state |
| **To** | Resulting state on success |
| **Trigger** | The `lab` command or event that initiates the transition |
| **Precondition** | Condition that MUST hold before the transition executes |
| **Postcondition** | Condition that MUST hold after successful transition |
| **On failure** | Resulting state if the transition fails mid-execution |

### 3.3 Complete Transition Table

| From | To | Trigger | Precondition | Postcondition | On failure |
|---|---|---|---|---|---|
| UNPROVISIONED | PROVISIONED | `lab provision` | VM is reachable; internet access available | Bootstrap script exited 0 | UNPROVISIONED (no change) |
| PROVISIONED | CONFORMANT | `lab validate` (pass) | Bootstrap completed | All blocking checks pass | BROKEN |
| PROVISIONED | BROKEN | `lab validate` (fail) | Bootstrap completed | At least one blocking check fails | — |
| CONFORMANT | DEGRADED | `lab fault apply <ID>` | State is CONFORMANT; no active fault | Fault postcondition holds; fault ID recorded | BROKEN |
| CONFORMANT | BROKEN | External modification | Was CONFORMANT | At least one blocking check now fails | — |
| DEGRADED | RECOVERING | `lab reset` | Active fault recorded | Reset tier operations executing | BROKEN |
| BROKEN | RECOVERING | `lab reset` | None | Reset tier operations executing | BROKEN |
| RECOVERING | CONFORMANT | Reset success + validate pass | Reset operations complete | All blocking checks pass; active fault null | BROKEN |
| RECOVERING | BROKEN | Reset failure | Reset operations failed | At least one blocking check fails | — |

**Force transition (special case):**

| From | To | Trigger | Precondition | Note |
|---|---|---|---|---|
| DEGRADED | DEGRADED | `lab fault apply <ID> --force` | None | Replaces active fault; prior fault cleared without recovery. Records `forced: true`. |
| BROKEN | DEGRADED | `lab fault apply <ID> --force` | None | Fault applied on broken environment. Result is ambiguous. Records `forced: true`. |

### 3.4 Forbidden Transitions

The following transitions are explicitly forbidden. Attempting them without `--force` MUST result in an error with the rationale displayed.

| From | Attempted To | Rationale |
|---|---|---|
| DEGRADED | DEGRADED | Stacking faults produces overlapping symptoms that violate the one-fault-one-chain model |
| BROKEN | DEGRADED | Fault symptoms and pre-existing breakage are indistinguishable; recovery path undefined |
| UNPROVISIONED | CONFORMANT | Cannot validate without provisioning |
| UNPROVISIONED | DEGRADED | Cannot inject faults into an unprovisioned environment |
| RECOVERING | any | Concurrent transitions are forbidden while recovery is in progress |

### 3.5 Transition Failure Semantics

When a transition fails mid-execution, the resulting state is determined by how far execution progressed:

**Failure before any mutation:** the system remains in the prior state. No state file update occurs.

**Failure after partial mutation:** the system is BROKEN. The state file MUST be updated to BROKEN with a note identifying the failed transition. The partial mutation is not automatically reversed — the operator must run `lab reset` to restore canonical state.

**State recording failure (mutation succeeded but state.json write failed):** the system's runtime state may be DEGRADED (if a fault was applied) but the control plane does not know this. The control plane MUST classify this as BROKEN and report that state recording failed. Running `lab validate` will resolve the discrepancy by observing runtime state.

---

## §4 — State Detection

### 4.1 Authority Precedence for State Determination

When the current state must be determined (e.g., on `lab status`), the following precedence applies. Higher-numbered sources override lower-numbered sources when they conflict:

1. **CLI cached state** — lowest authority. The result of the prior `lab` command stored in process memory. Stale by definition after any external modification.

2. **State file (`/var/lib/lab/state.json`)** — records the last known state transition. May be stale if the system was modified outside the control plane.

3. **Conformance suite result** — the result of running `lab validate`. Reflects current runtime state at the moment of execution. Overrides the state file.

4. **Runtime observation (process/network/endpoints)** — highest authority. What the system is actually doing right now. Overrides all other sources.

**Precedence in practice:** `lab status` performs lightweight runtime checks (process running, ports bound, one endpoint check) and compares against the state file. If they agree, the state file classification is used. If they disagree, the runtime observation wins and the state file is updated.

### 4.2 Detection Algorithm

`lab status` determines current state using the following steps:

```
1. Read state.json → recorded_state, active_fault
2. Check: is app process running as appuser? (P-001 equivalent)
3. Check: is app listening on 127.0.0.1:8080? (P-002 equivalent)
4. Check: is nginx active? (S-003 equivalent)
5. Check: does GET /health return 200? (E-001 equivalent)

If steps 2-5 all pass:
  If active_fault is non-null:
    → Report DEGRADED (runtime consistent with state file)
  Else:
    → Report CONFORMANT (runtime indicates healthy)
    → If recorded_state was BROKEN or DEGRADED: update state file

If any of steps 2-5 fail:
  If active_fault is non-null AND failure is consistent with fault postcondition:
    → Report DEGRADED (expected failure for active fault)
  Else:
    → Report BROKEN (unexpected failure; active_fault cleared if inconsistent)
    → Update state file to BROKEN

If state.json cannot be read or parsed:
  → Report state as UNKNOWN (classification failure, not a system state)
  → Prompt: run lab validate to establish state
```

### 4.3 Conflict Resolution

**Case: conformance suite passes but state file records DEGRADED**
Resolution: CONFORMANT wins. The fault was likely cleared by an operation outside the control plane. Update state file to CONFORMANT, clear `active_fault`. Log the discrepancy in audit.

**Case: conformance suite fails but state file records CONFORMANT**
Resolution: BROKEN. The environment was modified outside the control plane. Update state file to BROKEN.

**Case: conformance suite shows fault-consistent failures, state file records no fault**
Resolution: BROKEN. The failure pattern matches a known fault postcondition but no fault was applied through the control plane. This indicates manual mutation that mimics a fault. Classify as BROKEN, not DEGRADED.

**Case: multiple blocking checks fail in a pattern inconsistent with any known fault**
Resolution: BROKEN. Log the failing check IDs for diagnosis.

### 4.4 UNKNOWN — Classification Failure

UNKNOWN is not a system state. It is a control-plane classification failure that occurs when the detection algorithm cannot determine which state the environment is in due to contradictory or insufficient evidence.

**Conditions that produce UNKNOWN:**
- `state.json` cannot be read or parsed
- Runtime checks produce contradictory results (e.g., process running but port not bound, endpoint succeeding but process check failing)
- The environment is in the middle of a transition when `lab status` runs (rare)

**Response to UNKNOWN:**
- `lab status` reports `STATE: UNKNOWN` with a description of the contradictory evidence
- No state file update is made
- The operator is prompted to run `lab validate` (full suite) to establish state
- `lab fault apply` is forbidden in UNKNOWN state

**UNKNOWN is not reachable by design:** every valid operational transition produces a defined resulting state. UNKNOWN is only reachable through external modification that produces contradictory runtime evidence or through state file corruption.

---

## §5 — Constraint Graph

### 5.1 Valid Transition Graph

```
UNPROVISIONED
    │
    provision
    │
    ▼
PROVISIONED ──validate(fail)──► BROKEN
    │                               │
validate(pass)                    reset
    │                               │
    ▼                               ▼
CONFORMANT ◄────────────────── RECOVERING ◄── DEGRADED
    │                               │              │
fault apply                    (success)       reset
    │                               │
    ▼                               ▼
DEGRADED                        CONFORMANT
    │
(external break)
    │
    ▼
BROKEN (from any state, via external modification)
```

### 5.2 Invariant Preservation Across Transitions

The following invariants MUST hold across all transitions:

**I-1 (Active fault uniqueness):** at most one fault ID is recorded in `state.json` at any time. `DEGRADED` state requires exactly one; all other states require zero.

**I-2 (State-fault consistency):** if `active_fault` is non-null, state MUST be `DEGRADED`. If state is `DEGRADED`, `active_fault` MUST be non-null.

**I-3 (Conformance-state consistency):** if all blocking conformance checks pass and `active_fault` is null, state MUST NOT be `BROKEN`.

**I-4 (Transition completeness):** every transition updates `state.json` as its final step. No transition completes without a state file update.

### 5.3 Reachability

Every valid operational state is reachable through defined transitions from UNPROVISIONED:

- UNPROVISIONED → PROVISIONED: via `lab provision`
- PROVISIONED → CONFORMANT: via `lab validate` (pass)
- PROVISIONED → BROKEN: via `lab validate` (fail)
- CONFORMANT → DEGRADED: via `lab fault apply`
- CONFORMANT → BROKEN: via external modification
- DEGRADED → RECOVERING → CONFORMANT: via `lab reset`
- BROKEN → RECOVERING → CONFORMANT: via `lab reset`

UNKNOWN is not reachable through any defined transition. It arises only from control-plane classification failure.

### 5.4 Liveness

Every non-CONFORMANT state has a defined path back to CONFORMANT:

- DEGRADED → `lab reset` → RECOVERING → CONFORMANT
- BROKEN → `lab reset [--tier R2 or R3]` → RECOVERING → CONFORMANT
- PROVISIONED → `lab validate` → CONFORMANT (if checks pass)
- UNPROVISIONED → `lab provision` → PROVISIONED → `lab validate` → CONFORMANT
- RECOVERING → (automatic after reset) → CONFORMANT

No state is a dead end. Recovery is always possible through a defined path.

---

## §6 — Model Completeness Condition

The state model is complete when:

**Forward direction:** every state defined in this document maps to at least one conformance check in `conformance-model.md` via the `Maps to` fields of those checks.

**Reverse direction:** every conformance check in `conformance-model.md` maps to at least one state in this document.

**Current state-to-check mapping:**

| State | Conformance check evidence |
|---|---|
| CONFORMANT | All S/P/E/F blocking checks pass; L checks may degrade |
| DEGRADED | S-001, P-002, E-001, E-002, or F-series checks fail per fault postcondition |
| BROKEN | Any blocking check fails without matching fault postcondition |
| PROVISIONED | S-001, S-002 may be failing; no endpoint checks expected to pass |
| UNPROVISIONED | F-001 fails (binary absent); S-series checks fail |
| RECOVERING | Transitional; checks in intermediate states |

**Completeness violations (prohibited):**
- A state with no conformance check evidence (unverifiable state)
- A conformance check that maps to no state (orphan check)

---

## §7 — Authority and References

**This document's authority:** system behavior truth. Defines valid states and transitions. Overrides `fault-model.md` and `canonical-environment.md` on matters of state classification and transition rules.

**Depends on:** `conformance-model.md` for the definition of CONFORMANT (§3 of that document) and check IDs used in the detection algorithm.

**Depended on by:** `fault-model.md` for fault precondition states (CONFORMANT required) and fault postcondition states (DEGRADED produced).

**Relationship to `canonical-environment.md`:** the environment specification instantiates this model. The `lab` CLI in §13 of that document is the executable implementation of this state machine. The conformance suite in §6 of that document is the executable expression of the conformance model.

**Version consistency:** this document's version MUST match `conformance-model.md` and `fault-model.md`. The three documents form a coherent versioned set.