## lab_env — Formally Specified Control Plane with Fault Injection

### Overview

`lab_env` is best understood as a deterministic single-node chaos engineering lab: a miniature reliability control plane for fault injection, conformance validation, and state reconciliation, built as a spec-derived systems engineering exercise that models:

- System health classification (CONFORMANT / DEGRADED / BROKEN / UNPROVISIONED / PROVISIONED / RECOVERING)
- Fault injection and tiered recovery cycles
- Runtime versus recorded state reconciliation
- Strict separation between observation and mutation authority
- Audit-complete mutation execution
- Invariant-driven correctness validation derived from formal semantic specifications

It is designed as a **control-plane correctness model**, not an application. The system's primary claim is behavioral: any valid input sequence produces a predictable, testable, reproducible system state.

> **Execution status:** unit and contract test suite complete; live VM integration is the current phase.

---

### Semantic Foundation

The implementation is derived from three formal semantic model documents. This is the architectural characteristic that separates this project from a well-structured CLI tool.

| Document | Defines | Authority |
|---|---|---|
| `conformance-model.md` | 23-check catalog, severity classifications, validation semantics | Semantic root — defines "correct" |
| `system-state-model.md` | 6-state machine, transition guards, conflict resolution algorithm | State classification derives from conformance |
| `fault-model.md` | Fault schema, mutation rules, postcondition specifications | Faults are operators over the state machine |

The codebase is a mechanical projection of these documents. The authority hierarchy is explicit: conformance-model is the semantic root, state-model derives from it, fault-model derives from both. No implementation detail can redefine behavior owned by a higher-authority document.

**What this means in practice:** every test in the suite references the exact specification section it enforces. A test failure is not just a broken assertion — it is a violated architectural invariant with a named source.

```go
// From detect_test.go — each case names the spec section it enforces:
{
    name: "§4.3 case 1: suite passes but state file records DEGRADED — CONFORMANT wins",
    // system-state-model.md §4.3: conflict resolution case 1
    ...
}
```

This is not test coverage. It is specification enforcement.

---

### Architecture: Authority-Separated Semantic Pipeline

The system is not a layered architecture. It is a one-directional authority pipeline where each layer has exactly one responsibility and cannot access the capabilities of layers above or below it.

```
                    ┌─────────────────────────────────────┐
                    │         System Environment          │
                    └──────────────────┬──────────────────┘
                                       │ observed by
                                       ▼
         ┌─────────────────────────────────────────────────┐
         │              Observer Interface                 │
         │  read-only · no audit · no lock · no mutation   │
         └──────────────────┬──────────────────────────────┘
                            │ feeds
                            ▼
         ┌─────────────────────────────────────────────────┐
         │           Conformance Engine                    │
         │  23 checks · severity classification · ordering │
         │  depends only on Observer                       │
         └──────────────────┬──────────────────────────────┘
                            │ produces SuiteResult
                            ▼
         ┌─────────────────────────────────────────────────┐
         │            State Classification                 │
         │  detection algorithm · conflict resolution      │
         │  reconciles runtime observation + recorded state│
         └──────────────────┬──────────────────────────────┘
                            │ authorizes
                            ▼
         ┌─────────────────────────────────────────────────┐
         │         Executor (Mutation Authority)           │
         │  embeds Observer · all mutations audited        │
         │  advisory lock · atomic state writes            │
         └──────────────────┬──────────────────────────────┘
                            │ mutates
                            ▼
                    ┌────────────────────────────────┐
                    │ System + state.json + audit.log| 
                    └────────────────────────────────┘
```

**The Observer/Executor boundary is the most important design decision in the codebase.** `conformance.Observer` and `executor.Executor` are distinct interfaces. The Executor embeds the Observer (mutation commands can also observe), but the Observer interface carries no mutation methods. Conformance checks receive only an Observer — they are structurally incapable of mutating the system. This is enforced at the type level, not by convention.

```go
// Observer: read-only — 8 methods, no writes
type Observer interface {
    Stat(path string) (fs.FileInfo, error)
    ReadFile(path string) ([]byte, error)
    CheckProcess(name, user string) (ProcessStatus, error)
    CheckPort(addr string) (PortStatus, error)
    CheckEndpoint(url string, skipTLSVerify bool) (EndpointStatus, error)
    ResolveHost(name string) (string, error)
    ServiceActive(unit string) (bool, error)
    ServiceEnabled(unit string) (bool, error)
    RunCommand(cmd string, args ...string) (string, error)
}

// Executor: mutation authority — embeds Observer + adds 9 mutation methods
type Executor interface {
    Observer
    WriteFile(path string, data []byte, mode fs.FileMode, owner, group string) error
    Chmod(path string, mode fs.FileMode) error
    // ...
    RunMutation(cmd string, args ...string) error  // audited privileged operations
}
```

---

### State Machine

Six states. Every transition is defined, every guard is checked, every invalid transition is rejected with a specific error.

```
UNPROVISIONED ──provision──► PROVISIONED
                                  │
                           validate (pass)
                                  │
                                  ▼
                ┌──── CONFORMANT ◄─────────────────────-┐
                │         │                             │
          fault apply   external break              reset (any tier)
                │         │                             │
                ▼         ▼                             │
            DEGRADED    BROKEN ────────────────────────►┤
                │                                       │
              reset ──────────────────────────────────► RECOVERING
```

**Key property:** `lab status` is the only command authorized to reconcile recorded state with observed runtime reality. `lab validate` is strictly observation-only — it records what it sees but cannot update the authoritative state classification. This is enforced in code and tested explicitly.

---

### Conformance Engine

23 checks grouped into five series by what they observe:

| Series | Checks | Observes | Authority |
|---|---|---|---|
| S-series (S-001–S-004) | System state | systemd unit states | Structural — explanatory |
| P-series (P-001–P-004) | Process | running processes, bound ports | Behavioral — semantic |
| E-series (E-001–E-005) | Endpoint | HTTP responses, headers, body | Behavioral — primary semantic authority |
| F-series (F-001–F-007) | Filesystem | file existence, mode bits, DNS | Structural — explanatory |
| L-series (L-001–L-003) | Log | log presence, format, content | Operational — informational |

**Severity classification:** blocking checks (exit 1 when failing) vs degraded checks (exit 0 — F-006, L-001, L-002, L-003). Degraded failures produce a CONFORMANT sub-classification, not a non-conformant result. This distinction is what allows `lab validate` to return exit 0 while still reporting operational drift.

**Dependency ordering:** S-series before P-series before E-series. When S-001 fails (service not running), E-series checks still execute but are marked `dependent` — they failed because of S-001, not independently. Dependent failures are excluded from the failing check count, preserving the diagnostic signal.

---

### Fault Catalog

18 faults covering the full dependency chain: `filesystem → permissions → process → service → socket → proxy → response`.

Each fault defines:

- **FaultDef** (static metadata, fully serializable): ID, layer, domain, reversibility, reset tier, postcondition specification
- **FaultImpl** (adds Apply/Recover functions): mutation operations via Executor only

The postcondition is dual-representation: a behavioral description for humans, plus a machine-verifiable set of conformance check IDs that will fail after Apply. This enables automatic postcondition verification after fault application.

Two faults (F-008 SIGTERM ignored, F-014 zombie accumulation) are non-reversible and require binary rebuild. Both are invisible to `lab validate` while the fault is active — they manifest only at shutdown or over time, not through conformance checks.

---

### Key Guarantees

All are enforced by named tests. Failing any guarantee causes a specific test to fail.

| Guarantee | Enforcing test |
|-----------|----------------|
| No mutation without audit log entry | `TestMutationAuditCompleteness_AllMutationMethodsAreAudited` |
| `lab validate` never updates state classification | `TestValidateCmd_WritesLastValidate_NotState` |
| `lab status` is the only reconciliation point | `TestStatusCmd_ReconcilesBrokenToConformant_WhenRuntimeHealthy` |
| Apply failure never updates state to DEGRADED | `TestFaultApplyCmd_ApplyFailure_DoesNotUpdateState` |
| Interrupt never asserts BROKEN | `TestInterruptPath_DoesNotAssertBroken` |
| At most one active fault at any time | `TestFaultApplyCmd_PreconditionFails_FaultAlreadyActive` |
| Degraded checks never affect exit code | `TestSuiteResult_Classify_DegradedOnly` |
| No mutation through the Observer interface | `TestObserver_DoesNotHaveMutationMethods` |

---

### Command Interface

| Command | Classification | Authority |
|---------|----------------|-----------|
| `lab status` | Reconciliation | Only command that updates authoritative state classification |
| `lab validate [--check ID]` | Observation | Records observations; never updates state |
| `lab fault apply <ID>` | Mutation | CONFORMANT → DEGRADED; requires lock |
| `lab reset [--tier R1\|R2\|R3]` | Mutation + verification | Restores CONFORMANT; always runs post-reset validation |
| `lab provision` | Mutation | Bootstrap; idempotent |
| `lab history` | Read-only | Ring-buffered state transition log |

---

### Testing Architecture

The test suite is organized as a specification enforcement system, not a coverage metric.

```
internal/conformance/runner_test.go    — classification semantics, dependency ordering
internal/state/detect_test.go          — adversarial matrix: all 4 §4.3 conflict cases
internal/state/store_test.go           — atomic write, schema, corruption recovery
internal/executor/audit_test.go        — mutation audit completeness invariant
internal/executor/lock_test.go         — lock contract: acquire, release, stale, live
internal/executor/boundary_test.go     — Observer ≠ Executor interface separation
internal/catalog/catalog_test.go       — catalog completeness, postcondition validity
internal/invariants/invariants_test.go — cross-document invariants (18 faults × 23 checks)
internal/output/golden_test.go         — frozen JSON contract fixtures, nullability
cmd/status_test.go                     — reconciliation authority contract
cmd/validate_test.go                   — observation-only contract
cmd/fault_test.go                      — precondition/atomicity/audit contract
cmd/interrupt_test.go                  — interrupt path: all 8 contract points
```

Each test references the document and section it enforces. A test failure traces to a named invariant, not just a broken assertion.

---
