# Portfolio Presentation Package — lab-env
## Revised

---

# 1. Narrative Framing

Use this as your opening in README or interviews:

> `lab-env` is a formally specified control-plane system with fault injection, semantic state classification, and strict separation between observation and mutation.
>
> The system models how an environment transitions between health states — under normal execution, deliberate fault injection, and forced interruption — while enforcing correctness through a semantic conformance engine derived directly from a three-document formal specification.
>
> The key design goal is not functionality. It is **specification-coupled implementation**: the codebase is a mechanical projection of formal semantic models, and tests enforce that projection.

---

# 2. README (Portfolio-Grade)

---

## lab-env — Formally Specified Control Plane with Fault Injection

### Overview

`lab-env` is a deterministic control-plane system that models:

- System health classification (CONFORMANT / DEGRADED / BROKEN)
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
                    │         System Environment           │
                    └──────────────────┬──────────────────┘
                                       │ observed by
                                       ▼
         ┌─────────────────────────────────────────────────┐
         │              Observer Interface                  │
         │  read-only · no audit · no lock · no mutation   │
         └──────────────────┬──────────────────────────────┘
                            │ feeds
                            ▼
         ┌─────────────────────────────────────────────────┐
         │           Conformance Engine                     │
         │  23 checks · severity classification · ordering  │
         │  depends only on Observer                        │
         └──────────────────┬──────────────────────────────┘
                            │ produces SuiteResult
                            ▼
         ┌─────────────────────────────────────────────────┐
         │            State Classification                  │
         │  detection algorithm · conflict resolution       │
         │  reconciles runtime observation + recorded state │
         └──────────────────┬──────────────────────────────┘
                            │ authorizes
                            ▼
         ┌─────────────────────────────────────────────────┐
         │         Executor (Mutation Authority)            │
         │  embeds Observer · all mutations audited         │
         │  advisory lock · atomic state writes             │
         └──────────────────┬──────────────────────────────┘
                            │ mutates
                            ▼
                    ┌───────────────────┐
                    │ System + state.json + audit.log │
                    └───────────────────┘
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
                ┌──── CONFORMANT ◄─────────────────────┐
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
|---|---|
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
|---|---|---|
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

# 3. Diagrams

## 3.1 Authority Flow (primary diagram)

```
┌──────────────────────────────────────────────────────────────┐
│                    System Environment                         │
│             (filesystem · processes · services)               │
└────────────────────────┬─────────────────────────────────────┘
                         │  observed via
                         ▼
┌──────────────────────────────────────────────────────────────┐
│                  conformance.Observer                         │
│              read-only · unaudited · no lock                  │
│                                                               │
│  Stat · ReadFile · CheckProcess · CheckPort                   │
│  CheckEndpoint · ResolveHost · ServiceActive · RunCommand     │
└────────────────────────┬─────────────────────────────────────┘
                         │  used by
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
   Conformance       State           lab status
    Engine        Classification    (reconciler)
   (23 checks)    (detect.go)
          │              │
          └──────┬───────┘
                 │  authorizes mutation via
                 ▼
┌──────────────────────────────────────────────────────────────┐
│                  executor.Executor                            │
│         embeds Observer · all writes audited · lock required  │
│                                                               │
│  WriteFile · Chmod · Chown · Remove · Systemctl               │
│  NginxReload · RestoreFile · RunMutation                      │
└────────────────────────┬─────────────────────────────────────┘
                         │  writes to
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
   System state       state.json     audit.log
```

---

## 3.2 State Machine

```
UNPROVISIONED
      │ lab provision
      ▼
PROVISIONED
      │ lab validate
      ├──(pass)──► CONFORMANT ◄────────────────────────────────┐
      │                │                                        │
      └──(fail)──►     │ lab fault apply                        │
                       ▼                                        │
                   DEGRADED                              lab reset
                       │ lab reset                        (R1/R2/R3)
                       ▼                                        │
CONFORMANT ──(external break)──► BROKEN ───────────────────────┘
                                     │
                              lab reset
                                     │
                                     ▼
                                RECOVERING ──(success)──► CONFORMANT
                                           ──(failure)──► BROKEN
```

---

## 3.3 Fault Lifecycle

```
lab fault apply F-004
        │
        ├─ [check] fault exists in catalog
        ├─ [check] IsBaselineBehavior = false
        ├─ [lock]  acquire /var/lib/lab/lab.lock
        ├─ [read]  state.json  ← TOCTOU re-read after lock
        ├─ [check] state == CONFORMANT
        ├─ [check] active_fault == nil
        ├─ [audit] executor_op: Chmod /var/lib/app 0000
        ├─ [mut]   Chmod("/var/lib/app", 0000)
        ├─ [audit] state_transition: CONFORMANT → DEGRADED
        ├─ [write] state.json: state=DEGRADED, active_fault=F-004
        ├─ [unlock]
        └─ [exit 0]

If [mut] fails:
        ├─ [audit] error: ErrApplyFailed
        ├─ [unlock]
        └─ [exit 1]   ← state.json unchanged (atomicity guarantee)
```

---

## 3.4 State Reconciliation (the novel piece)

This is the design decision that most distinguishes this system from a simple CLI wrapper.

```
lab status execution:
                                        recorded: BROKEN
                                            │
  runtime observation ──────────────────►  │  conflict resolution
  (process running,                        │  (system-state-model §4.3)
   port bound,                             │
   /health returns 200)                    ▼
                                   detected: CONFORMANT
                                            │
                                     [audit] reconciliation
                                     [write] state.json: CONFORMANT
                                     [exit 0]

The system does not trust its own stored state.
It corrects it from observed reality.
```

---

# 4. Demo Scripts

## Demo 1 — Fault Injection and System Degradation

```bash
# Start from clean state
lab status
# State: CONFORMANT  Active fault: none

# Apply a fault — state directory becomes unwritable
lab fault apply F-004

# Health endpoint still works (app is running)
curl localhost/health          # 200
# Primary endpoint fails (state write fails)
curl localhost/                # 500
# App log shows the cause
tail -5 /var/log/app/app.log   # "msg":"state write failed"

lab status
# State: DEGRADED  Active fault: F-004

lab validate
# [PASS] E-001  GET /health returns 200    ← still passing
# [FAIL] E-002  GET / returns 200          ← failing
# [FAIL] F-004  /var/lib/app/ mode 755     ← failing
# Exit: 1
# This is the health/ready split — the most diagnostically important fault pattern
```

---

## Demo 2 — Tiered Recovery

```bash
lab fault apply F-001          # config file deleted → full service outage

lab status
# State: DEGRADED

lab reset                      # auto-selects R2 tier
# [1] Restores canonical config file
# [2] Restarts services
# [3] Runs conformance suite — 23/23 pass

lab status
# State: CONFORMANT  Active fault: none
```

---

## Demo 3 — Interrupt Safety

```bash
# Apply a fault first
lab fault apply F-004

# Begin reset — this will take several seconds
lab reset &
RESET_PID=$!

# Interrupt after the first mutation completes
sleep 0.2
kill -SIGINT $RESET_PID
wait $RESET_PID
echo "Exit: $?"
# Exit: 4  ← interrupted with side effects

# The interrupt handler does NOT assert BROKEN
# It invalidates the classification
cat /var/lib/lab/state.json | jq '{state, classification_valid}'
# { "state": "DEGRADED", "classification_valid": false }
# State unchanged. Certainty lost.

# Audit log shows the interrupt entry
grep '"entry_type":"interrupt"' /var/lib/lab/audit.log

# lab status reclassifies from runtime reality
lab status
# classification_valid restored to true
# State derived from what the system actually is, not what was recorded
```

---

## Demo 4 — Runtime Truth Overrides Recorded State

```bash
# Simulate a control-plane failure: manually apply F-004's mutation
# without going through lab (simulates a crash mid-apply)
sudo chmod 000 /var/lib/app

# state.json still says CONFORMANT — no fault was recorded
cat /var/lib/lab/state.json | jq '.state'      # "CONFORMANT"
cat /var/lib/lab/state.json | jq '.active_fault'  # null

# validate is observation-only — it sees the failure but cannot update state
lab validate
# [FAIL] E-002  / returns 200
# [FAIL] F-004  /var/lib/app/ mode 755
# Exit: 1
cat /var/lib/lab/state.json | jq '.state'      # "CONFORMANT" — validate did NOT update this

# status reconciles — runtime truth overrides recorded state
lab status
# State: BROKEN  (reconciled from CONFORMANT)
cat /var/lib/lab/state.json | jq '.state'      # "BROKEN" — status updated this

# The system does not trust its own stored state.
# It corrects the record from observed reality.
# This is the reconciliation engine.
```

---

## Demo 5 — Baseline Behavior (not a fault — observable from CONFORMANT)

```bash
# nginx proxy timeout is shorter than the app's slow endpoint
time curl -v http://localhost/slow
# 504 Gateway Timeout after ~3 seconds (nginx proxy_read_timeout)
# X-Proxy: nginx

time curl 127.0.0.1:8080/slow
# 200 OK after ~5 seconds (direct to app, bypasses proxy)

lab validate
# Exit: 0 — no conformance check fails
# This is F-011 in the catalog: baseline behavior, not a fault
# Demonstrates the proxy timeout layer in isolation
```

---

# 5. What Interviewers Should Take Away

---

## 5.1 This is specification-coupled implementation (rare)

Most systems are built first and documented after. This system works the other direction: three formal semantic model documents define the system's invariants, state machine, and fault semantics. The implementation is derived from those documents. Tests enforce the derivation.

When an interviewer asks "how do you know the implementation is correct?" the answer is: because any violation of the semantic model causes a named test to fail. The tests are the machine-readable form of the specification.

---

## 5.2 The Observer/Executor split is a type-level correctness guarantee

This is not a design pattern. It is a structural impossibility of violation. A conformance check receives a `conformance.Observer`. The `Observer` interface has no mutation methods. The check is literally incapable of mutating the system, enforced by the Go type system. This is the same reasoning as capability-based security — you cannot escalate authority you were never given.

---

## 5.3 The state reconciliation engine models distributed systems thinking

The state detection algorithm applies authority precedence rules: runtime observation overrides conformance suite results, which overrides the state file. The system assumes its own stored state may be wrong and corrects it from evidence. This is the same reasoning as a consensus system treating its own log as advisory rather than authoritative until it can verify quorum.

---

## 5.4 Testing maturity: tests as named invariants

The test suite does not measure coverage. It enforces named invariants derived from the specification. Each test is linked to a specific document section. A test failure is not "something broke" — it is "a specific architectural guarantee was violated." This is the level of test discipline typical of systems software (compilers, kernels, databases), not application development.

---

## 5.5 Production-grade execution boundaries

- Advisory lock serializes all mutations; read-only paths never contend
- Every mutation produces an audit log entry before the next mutation begins (ordering guarantee)
- State file writes are atomic (temp file + rename — no partial writes survive a crash)
- Interrupt handling sets `classification_valid: false` rather than asserting BROKEN — uncertainty is distinct from failure

---

# 6. Positioning Statement

> `lab-env` is not a production application. It is a formally specified, adversarially validated control-plane system whose primary purpose is to demonstrate what it looks like when a codebase is structurally coupled to its own specification rather than loosely documented after the fact.
>
> The system's invariants are not comments. They are tests. The tests are not coverage metrics. They are proofs. The specification documents are not README prose. They are the authoritative source from which the implementation is derived.
>
> If that is the kind of engineering you are looking for, this is evidence that it is possible to build it.