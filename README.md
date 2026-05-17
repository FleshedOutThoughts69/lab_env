# lab-env

A deterministic single-node chaos engineering lab: a miniature reliability control plane for fault injection, conformance validation, and state reconciliation — built as a spec-derived systems engineering exercise.

> **Execution status:** unit and contract test suite complete (262 tests across 40 files). Live VM integration is the current phase.

---

## What this is

`lab-env` models:

- System health classification across six defined states (UNPROVISIONED / PROVISIONED / CONFORMANT / DEGRADED / BROKEN / RECOVERING)
- Fault injection and tiered recovery cycles (R1 / R2 / R3)
- Runtime vs. recorded state reconciliation with named conflict resolution rules
- Strict separation between observation and mutation authority, enforced at the type level
- Audit-complete mutation execution — every mutation produces an audit log entry before it executes
- Invariant-driven correctness validation derived from four formal semantic model documents

The system's primary claim is behavioral: any valid input sequence produces a predictable, testable, reproducible environment state. The implementation is a mechanical projection of the semantic models. Every test references the exact specification section it enforces.

---

## Requirements

- **OS:** Ubuntu 22.04 LTS (amd64)
- **Go:** 1.21 or later
- **cgroup v2** (required for MemoryMax enforcement on F-008/F-014):
  ```bash
  stat -f -c %T /sys/fs/cgroup   # must print: cgroup2fs
  ```
- **Root access:** bootstrap and fault operations require `sudo`
- **Repository location:** `/opt/lab-env` (expected by bootstrap and the systemd unit file)

---

## Quick start

```bash
# 1. Clone
sudo mkdir -p /opt/lab-env
sudo git clone <repo-url> /opt/lab-env

# 2. Provision (installs packages, creates service user, builds binary, starts services)
cd /opt/lab-env
sudo bash scripts/bootstrap.sh

# 3. Verify
lab validate
# CONFORMANT (23/23 checks pass)

# 4. Apply a fault
lab fault apply F-004
# lab status → DEGRADED (F-004 active: /var/lib/app mode 000)

# 5. Diagnose
lab fault info F-004
curl http://localhost/          # 500 — state dir unwritable
ls -la /var/lib/app             # drwx------ (mode 000)

# 6. Reset
lab reset --tier R2
lab validate                    # CONFORMANT
```

---

## Architecture

The system is a one-directional authority pipeline. Each layer has exactly one responsibility and cannot access the capabilities of layers above or below it.

```
                    ┌──────────────────────────────────────┐
                    │          System Environment          │
                    └──────────────────┬───────────────────┘
                                       │ observed by
                                       ▼
         ┌─────────────────────────────────────────────────┐
         │              Observer Interface                 │
         │  read-only · no audit · no lock · no mutation   │
         └──────────────────┬──────────────────────────────┘
                            │ feeds
                            ▼
         ┌─────────────────────────────────────────────────┐
         │            Conformance Engine                   │
         │  23 checks · severity · dependency ordering     │
         │  depends only on Observer                       │
         └──────────────────┬──────────────────────────────┘
                            │ produces SuiteResult
                            ▼
         ┌─────────────────────────────────────────────────┐
         │           State Classification                  │
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
                    ┌───────────────────────────────────┐
                    │  System + state.json + audit.log  │
                    └───────────────────────────────────┘
```

**The Observer/Executor boundary is the most important design decision in the codebase.** `conformance.Observer` and `executor.Executor` are distinct interfaces. The Executor embeds the Observer (mutation commands can also observe), but the Observer interface carries no mutation methods. Conformance checks receive only an Observer — they are structurally incapable of mutating the system.

```go
// conformance.Observer — read-only, 8 methods, no writes
type Observer interface {
    Stat(path string) (fs.FileInfo, error)
    ReadFile(path string) ([]byte, error)
    CheckProcess(name, user string) (ProcessStatus, error)
    CheckPort(addr string) (PortStatus, error)
    CheckEndpoint(url string, skipTLSVerify bool) (EndpointStatus, error)
    ResolveHost(name string) (string, error)
    ServiceActive(unit string) (bool, error)
    ServiceEnabled(unit string) (bool, error)
}

// executor.Executor — mutation authority, embeds Observer + 9 mutation methods
type Executor interface {
    conformance.Observer
    WriteFile(path string, data []byte, mode fs.FileMode, owner, group string) error
    Chmod(path string, mode fs.FileMode) error
    Chown(path, owner, group string) error
    Remove(path string) error
    MkdirAll(path string, mode fs.FileMode, owner, group string) error
    Systemctl(action, unit string) error
    NginxReload() error
    RestoreFile(path string) error
    RunMutation(cmd string, args ...string) error  // audited privileged shell operations
}
```

---

## Semantic foundation

The implementation derives from four formal semantic model documents. This is what separates the project from a well-structured CLI tool.

| Document | Defines | Authority |
|---|---|---|
| `conformance-model.md` | 23-check catalog, severity rules, verdict derivation, validation output schema | Semantic root — defines "correct" |
| `system-state-model.md` | 6-state machine, transition guards, detection algorithm, conflict resolution | State classification derives from conformance |
| `fault-model.md` | Fault schema, 16-fault catalog with Apply/Recover specs, reversibility rules | Faults are operators over the state machine |
| `control-plane-contract.md` | `lab` CLI command contracts, state file schema, audit log format, exit codes | Implementation contract |
| `canonical-environment.md` | Instantiation layer: paths, users, modes, Ubuntu-specific details | Subordinate to all four semantic models |

Authority flows top to bottom. When documents conflict, the higher document wins. Every test references the exact specification section it enforces — a test failure is a violated architectural invariant with a named source:

```go
// From detect_test.go:
{
    name: "§4.3 case 1: suite passes but state file records DEGRADED — CONFORMANT wins",
    // system-state-model.md §4.3: conflict resolution case 1
    // Resolution: CONFORMANT wins; fault was likely cleared outside control plane.
    ...
}
```

---

## State machine

Six states. Every transition is defined, every guard is checked.

```
UNPROVISIONED ──provision──► PROVISIONED
                                  │
                           validate (pass)
                                  │
                                  ▼
                         CONFORMANT ◄──────────────────────┐
                         │       │                         │
                   fault apply  external break         reset (any tier)
                         │       │                         │
                         ▼       ▼                         │
                     DEGRADED   BROKEN ───────────────────►┤
                         │                                 │
                       reset ──────────────────────────► RECOVERING
```

**Key property:** `lab status` is the only command authorized to reconcile recorded state with observed runtime reality. `lab validate` is strictly observation-only — it records what it sees but cannot update the authoritative state classification. This boundary is enforced in code and tested explicitly.

---

## Command interface

| Command | Classification | Description |
|---|---|---|
| `lab status` | Reconciliation | Only command that updates authoritative state classification |
| `lab validate [--check ID]` | Observation | Runs conformance suite; records observations; never updates state |
| `lab fault list` | Read-only | List all 16 faults with ID, layer, domain |
| `lab fault info <ID>` | Read-only | Full fault entry: mutation, observable, reset tier |
| `lab fault apply <ID> [--force] [--yes]` | Mutation | CONFORMANT → DEGRADED; requires lock; PreconditionChecks enforced |
| `lab reset [--tier R1\|R2\|R3]` | Mutation + verification | Restores CONFORMANT; runs post-reset validation |
| `lab provision` | Mutation | Bootstrap; idempotent |
| `lab history [--last N]` | Read-only | Ring-buffered state transition log (default: 20 entries) |

```
Global flags:
  --json      Emit structured JSON output
  --quiet     Suppress non-error output
  --verbose   Emit executor operation trace
  --yes       Suppress confirmation prompts
```

---

## Conformance engine

23 checks in five series. Blocking check failures produce exit 1 (NON-CONFORMANT). Degraded check failures produce exit 0 with a DEGRADED-CONFORMANT sub-classification.

| Series | Checks | Observes | Severity |
|---|---|---|---|
| S (system state) | S-001–S-004 | systemd unit states | blocking |
| P (process) | P-001–P-004 | running processes, bound ports | blocking |
| E (endpoint) | E-001–E-005 | HTTP responses, headers, body | blocking |
| F (filesystem) | F-001–F-007 | file existence, mode bits, DNS | blocking (F-006: degraded) |
| L (log) | L-001–L-003 | log presence, format, content | degraded |

**Dependency ordering:** S-series before P-series before E-series. When S-001 fails (service not running), E-series checks still execute but are marked `dependent` — they failed because of S-001, not independently. Dependent failures are excluded from the failing check count, preserving the diagnostic signal.

---

## Fault catalog

16 faults covering the full dependency chain: `filesystem → permissions → process → service → socket → proxy → config`. Each fault defines a `FaultDef` (static metadata, fully serializable) and a `FaultImpl` (adds `Apply`/`Recover` functions via Executor). The postcondition is dual-representation: a behavioral description and a machine-verifiable set of conformance check IDs that will fail after Apply.

| Reset tier | Meaning | Faults |
|---|---|---|
| R1 | Service restart only | F-010 |
| R2 | Restore canonical config/permissions + restart | F-001–F-007, F-009, F-013, F-015–F-018 |
| R3 | Full reprovision (binary rebuild required) | F-008, F-014 |

F-008 (SIGTERM ignored) and F-014 (zombie accumulation) are non-reversible and invisible to `lab validate` while active — they manifest only at shutdown or over time, not through conformance checks.

B-001 (nginx proxy timeout shorter than `/slow` response) and B-002 (self-signed TLS certificate not in trust store) are baseline network behaviours — always present in the conformant environment, not faults.

```bash
lab fault list             # all 16 faults with domain
lab fault info F-010       # full entry: mutation, observable, reset tier
```

---

## Key guarantees

All enforced by named tests. Any violation causes a specific test to fail.

| Guarantee | Enforcing test |
|---|---|
| No mutation without audit log entry | `TestMutationAuditCompleteness_AllMutationMethodsAreAudited` |
| `lab validate` never updates state classification | `TestValidateCmd_WritesLastValidate_NotState` |
| `lab status` is the only reconciliation point | `TestStatusCmd_ReconcilesBrokenToConformant_WhenRuntimeHealthy` |
| Apply failure never updates state to DEGRADED | `TestFaultApplyCmd_ApplyFailure_DoesNotUpdateState` |
| Interrupt never asserts BROKEN | `TestInterruptPath_DoesNotAssertBroken` |
| At most one active fault at any time | `TestFaultApplyCmd_PreconditionFails_FaultAlreadyActive` |
| Degraded checks never affect exit code | `TestSuiteResult_Classify_DegradedOnly` |
| No mutation through Observer interface | `TestObserver_DoesNotHaveMutationMethods` |
| State set contains exactly six defined states | `TestState_ValidStates_ContainsAll` |

---

## Test suite

40 test files, 262 test functions. The suite is a specification enforcement system, not a coverage metric. Each test references the document and section it enforces — a failure traces to a named invariant, not just a broken assertion.

```
internal/conformance/runner_test.go        — classification semantics, dependency ordering
internal/state/detect_test.go              — adversarial matrix: all §4.3 conflict cases
internal/state/signal_combinations_test.go — full detection input space
internal/state/state_test.go               — six-state enumeration, transition guards
internal/state/store_test.go               — atomic write, schema, corruption recovery
internal/executor/audit_test.go            — mutation audit completeness invariant
internal/executor/lock_test.go             — lock contract: acquire, release, stale, live
internal/executor/boundary_test.go         — Observer ≠ Executor interface separation
internal/catalog/catalog_test.go           — catalog completeness, postcondition validity
internal/catalog/content_integrity_test.go — Apply/Recover target only declared files
internal/invariants/invariants_test.go     — cross-document invariants (16 faults × 23 checks)
internal/output/output_quality_test.go     — UTF-8, compactness, no double-encoding
cmd/status_test.go                         — reconciliation authority contract
cmd/validate_test.go                       — observation-only contract
cmd/fault_test.go                          — precondition/PreconditionChecks/atomicity/audit
cmd/interrupt_test.go                      — interrupt path: all 8 contract points
```

```bash
go test ./...                                      # all tests
go test ./internal/invariants/...                  # cross-document invariants only
go test -race ./...                                # with race detector
UPDATE_GOLDEN=1 go test ./internal/output/...      # regenerate golden fixtures
```

---

## Repository layout

```
lab-env/
├── main.go              # process entry point
├── app.go               # command dispatch and flag parsing
├── go.mod               # module: github.com/lab-env/lab
├── cmd/                 # one file per command: status, validate, fault, reset, history
├── internal/
│   ├── catalog/         # 16-fault catalog (FaultDef + FaultImpl)
│   ├── conformance/     # 23-check suite + Observer interface
│   ├── executor/        # Executor interface, mutation layer, audit log, lock
│   ├── state/           # six-state machine, state.json persistence, detection
│   ├── output/          # result types and human/JSON renderers
│   └── invariants/      # cross-document architecture invariant tests
├── testdata/golden/     # golden JSON fixtures for output regression
├── service/             # Go HTTP service (separate module: github.com/lab-env/service)
├── scripts/
│   ├── bootstrap.sh     # idempotent provisioning (16 steps)
│   ├── validate.sh      # thin wrapper: exec lab validate
│   ├── reset.sh         # thin wrapper: exec lab reset --tier <R1|R2|R3>
│   └── run-fault-matrix.sh  # iterates all 14 reversible faults
└── docs/                # operational documentation
```

---

## The Go service

A minimal Go HTTP service at `service/` (module `github.com/lab-env/service`) — the subject of all conformance checks and fault operations.

| Endpoint | Behaviour |
|---|---|
| `GET /health` | Returns `{"status":"ok","version":"..."}` |
| `GET /` | Touches `/var/lib/app/state`; returns request metadata as JSON |
| `GET /slow` | 5-second fixed delay (demonstrates B-001 proxy timeout) |

Runs as `appuser:appuser`, binds to `127.0.0.1:8080`, proxied by nginx on ports 80/443, writes structured JSON logs to `/var/log/app/app.log`.

---

## Operational documentation

| Document | Contents |
|---|---|
| `DEVELOPER-QUICKSTART.md` | Copy-paste commands from fresh VM to passing fault matrix |
| `docs/provisioning-blueprint.md` | Idempotency strategy, failure recovery, canonical artifact specification |
| `docs/fault-matrix-runbook.md` | Fault matrix: observable signals, diagnostic lookup table |
| `docs/recovery-playbook.md` | 9 hostile-state drills with 7-point verification checklist |
| `docs/operational-trace-spec.md` | 13 ordered event traces for every command |
| `docs/golden-baseline-ledger.md` | Frozen output surfaces, fault table, check table |
| `docs/testing-plan-revised.md` | Phase 0→A→B→C→D test plan |
| `docs/extension-boundary-note.md` | Change gates: required steps and forbidden shortcuts for every extension type |
| `docs/portfolio-presentation-package.md` | Five-demo presentation package |

---

## Extending the project

Before adding a fault, conformance check, command, executor method, system state, or audit entry type: read `docs/extension-boundary-note.md`. Every extension type has a required-changes checklist and a set of tests that will fail if any step is skipped.