# lab-env

A deterministic single-node chaos engineering lab: a miniature reliability control plane for fault injection, conformance validation, and state reconciliation — built as a spec-derived systems engineering exercise.

> **Execution status:** unit and contract test suite complete (268 tests across 42 files). Live VM integration is the current phase.

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
- **Go:** 1.21 or later (service module requires 1.22 for `math/rand/v2`)
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

**The Observer/Executor boundary is the most important design decision in the codebase.** `conformance.Observer` and `executor.Executor` are distinct interfaces. The Executor embeds the Observer, but the Observer carries no mutation methods. Conformance checks receive only an Observer — they are structurally incapable of mutating the system.

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
    RunMutation(cmd string, args ...string) error
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

Authority flows top to bottom. Every test references the exact specification section it enforces:

```go
// From detect_test.go:
{
    name: "§4.3 case 1: suite passes but state file records DEGRADED — CONFORMANT wins",
    // system-state-model.md §4.3: conflict resolution case 1
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

`lab status` is the only command authorized to reconcile recorded state with observed runtime reality. `lab validate` is strictly observation-only — it records what it sees but cannot update the authoritative state classification.

---

## Command interface

| Command | Classification | Description |
|---|---|---|
| `lab status` | Reconciliation | Only command that updates authoritative state classification |
| `lab validate [--check ID]` | Observation | Runs conformance suite; records observations; never updates state |
| `lab fault list` | Read-only | List all 16 faults with ID, layer, domain |
| `lab fault info <ID>` | Read-only | Full fault entry: mutation, observable, reset tier |
| `lab fault apply <ID> [--force] [--yes]` | Mutation | CONFORMANT → DEGRADED; requires lock; PreconditionChecks enforced |
| `lab reset [--tier R1\|R2\|R3]` | Mutation + verification | Restores CONFORMANT; runs post-reset validation; auto-selects tier if omitted |
| `lab provision` | Mutation | Bootstrap; idempotent |
| `lab history [--last N]` | Read-only | Ring-buffered state transition log (default: 20 entries) |

```
Global flags: --json  --quiet  --verbose  --yes
```

---

## Conformance engine

23 checks in five series. Blocking failures → exit 1 (NON-CONFORMANT). Degraded failures → exit 0 (DEGRADED-CONFORMANT).

| Series | Checks | Observes | Severity |
|---|---|---|---|
| S (system state) | S-001–S-004 | systemd unit states | blocking |
| P (process) | P-001–P-004 | running processes, bound ports | blocking |
| E (endpoint) | E-001–E-005 | HTTP responses, headers, body | blocking |
| F (filesystem) | F-001–F-007 | file existence, mode bits, DNS | blocking (F-006: degraded) |
| L (log) | L-001–L-003 | log presence, format, content | degraded |

**Dependency ordering:** S-series before P-series before E-series. When S-001 fails, E-series checks run but are marked `dependent` — they failed because of S-001, not independently. Dependent failures are excluded from the failing check count.

---

## Fault catalog

16 faults across the full dependency chain: `filesystem → permissions → process → service → socket → proxy → config`. Each fault has a `FaultDef` (static metadata, serializable) and a `FaultImpl` (adds `Apply`/`Recover` via Executor). Postconditions are dual-representation: behavioral description + machine-verifiable failing check IDs.

| Reset tier | Meaning | Faults |
|---|---|---|
| R1 | Service restart only | F-010 |
| R2 | Restore canonical config/permissions + restart | F-001–F-007, F-009, F-013, F-015–F-018 |
| R3 | Full reprovision (binary rebuild required) | F-008, F-014 |

F-008 (SIGTERM ignored) and F-014 (zombie accumulation) are non-reversible and silent to `lab validate` while active — they manifest only at shutdown or over time.

B-001 (nginx proxy timeout) and B-002 (self-signed TLS certificate) are baseline network behaviours — always present in the conformant environment, documented in `fault-model.md §10`.

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
| All PreconditionCheck IDs resolve to real checks | `TestInvariant_PreconditionChecks_AreValidCheckIDs` |

---

## Test suite

42 test files, 268 test functions across both modules. The suite is a specification enforcement system, not a coverage metric. Each test references the document and section it enforces.

```
internal/conformance/runner_test.go        — classification semantics, dependency ordering
internal/conformance/runner_edge_cases_test.go — severity boundary conditions
internal/state/detect_test.go              — adversarial matrix: all §4.3 conflict cases
internal/state/signal_combinations_test.go — full detection input space
internal/state/state_test.go               — six-state enumeration, transition guards
internal/state/store_test.go               — atomic write, schema, corruption recovery
internal/state/store_edge_cases_test.go    — edge cases: partial writes, concurrent access
internal/executor/audit_test.go            — mutation audit completeness invariant
internal/executor/lock_test.go             — lock contract: acquire, release, stale, live
internal/executor/boundary_test.go         — Observer ≠ Executor interface separation
internal/executor/embed_test.go            — embedded canonical file integrity
internal/catalog/catalog_test.go           — catalog completeness, postcondition validity
internal/catalog/content_integrity_test.go — Apply/Recover target only declared files
internal/invariants/invariants_test.go     — cross-document invariants (16 faults × 23 checks)
internal/invariants/architecture_test.go   — import boundary enforcement
internal/output/golden_test.go             — frozen JSON contract fixtures
internal/output/output_quality_test.go     — UTF-8, compactness, no double-encoding
cmd/status_test.go                         — reconciliation authority contract
cmd/validate_test.go                       — observation-only contract
cmd/fault_test.go                          — precondition/PreconditionChecks/atomicity/audit
cmd/interrupt_test.go                      — interrupt path: all 8 contract points
cmd/live_interrupt_test.go  (integration)  — real SIGINT/SIGTERM against live lab process
cmd/live_fault_matrix_test.go (integration)— full 14-fault apply→validate→reset cycle on real VM
service/*/                                  — 12 test files covering all service packages
```

```bash
go test ./...                                       # all unit tests
go test ./internal/invariants/...                   # cross-document invariants
go test -race ./...                                 # with race detector
UPDATE_GOLDEN=1 go test ./internal/output/...       # regenerate golden fixtures

# Integration tests (require live VM):
LAB_TEST_MODE=live go test -v -run TestLiveInterrupt ./cmd/...
LAB_TEST_MODE=live go test -v -run TestLiveFaultMatrix ./cmd/...
```

---

## Repository layout

```
lab-env/
├── main.go                          # process entry point
├── app.go                           # command dispatch and flag parsing
├── go.mod                           # module: lab_env  (Go 1.21)
├── cmd/
│   ├── status.go                    # lab status — reconciliation authority
│   ├── validate.go                  # lab validate — observation only
│   ├── fault.go                     # lab fault {list,info,apply}
│   ├── reset.go                     # lab reset — tiered recovery
│   ├── reset_provision_history.go   # lab provision + lab history
│   ├── status_test.go
│   ├── validate_test.go
│   ├── fault_test.go
│   ├── interrupt_test.go
│   ├── live_interrupt_test.go       # integration: real SIGINT/SIGTERM tests
│   ├── live_fault_matrix_test.go    # integration: full fault catalog on real VM
│   └── testhelpers_test.go
├── internal/
│   ├── catalog/
│   │   ├── fault.go                 # FaultDef + FaultImpl types + PreconditionChecks
│   │   └── faults.go                # 16-fault catalog (F-001–F-010, F-013–F-018)
│   ├── conformance/
│   │   ├── catalog.go               # 23-check catalog + CheckByID
│   │   ├── check.go                 # Check type + CheckResult (HTTPStatusCode)
│   │   ├── observer.go              # Observer interface (read-only)
│   │   ├── runner.go                # suite runner: ordering, dependency, H-001 fix
│   │   └── result.go                # SuiteResult and Classification
│   ├── executor/
│   │   ├── executor.go              # Executor interface (embeds Observer + 9 mutations)
│   │   ├── real.go                  # real system mutations via sudo
│   │   ├── audit.go                 # append-only audit log writer
│   │   ├── lock.go                  # advisory mutex (/var/lib/lab/lab.lock)
│   │   └── canonical_files.go       # embedded R2 restore targets
│   ├── state/
│   │   ├── state.go                 # State type, All(), IsValid(), transition guards
│   │   ├── store.go                 # state.json atomic read/write
│   │   └── detect.go                # runtime re-detection algorithm
│   ├── output/
│   │   ├── model.go                 # result types (StatusResult, ValidateResult, …)
│   │   └── render.go                # human and JSON renderers
│   ├── config/
│   │   ├── config.go                # canonical paths, modes, service names
│   │   ├── config.yaml              # embedded: /etc/app/config.yaml (R2 restore target)
│   │   ├── app.service              # embedded: systemd unit (R2 restore target)
│   │   ├── nginx.conf               # embedded: nginx site config (R2 restore target)
│   │   └── logrotate.conf           # embedded: logrotate config
│   ├── invariants/
│   │   ├── doc.go
│   │   ├── invariants_test.go       # cross-document invariants
│   │   └── architecture_test.go     # import boundary enforcement
│   └── testutil/
│       └── interrupt.go             # interrupt test helpers
├── testdata/
│   └── golden/                      # 6 frozen JSON fixtures for output regression
├── service/                         # Go HTTP service (separate module: lab_env/service, Go 1.22)
│   ├── main.go
│   ├── go.mod
│   ├── CONFORMANCE_CONTRACT.md
│   ├── chaos/                       # chaos middleware (latency, drop, OOM, SIGTERM)
│   ├── config/                      # YAML config loader
│   ├── logging/                     # structured JSON logger (O_APPEND)
│   ├── server/                      # HTTP handlers: /health, /, /slow
│   ├── signals/                     # PID file, healthy marker, status file
│   └── telemetry/                   # 2-second telemetry writer
├── scripts/
│   ├── bootstrap.sh                 # idempotent 16-step provisioning
│   ├── validate.sh                  # 23-check shell conformance suite
│   ├── reset.sh                     # thin wrapper: exec lab reset --tier <R1|R2|R3>
│   └── run-fault-matrix.sh          # iterates all 14 reversible faults
└── docs/
    ├── component-interface-spec.md  # HTTP protocol, log schema, state machine map
    ├── environment-test-plan.md     # manual verification, mutation boundary, edge cases
    ├── extension-boundary-note.md   # change gates for every extension type
    ├── fault-implementation-guide.md # mutation vectors, reversion vectors, side effects
    ├── fault-matrix-runbook.md      # diagnostic reference for all 16 faults
    ├── golden-baseline-ledger.md    # frozen output surfaces
    ├── operational-trace-spec.md    # 13 ordered event traces
    ├── portfolio-presentation-package.md
    ├── provisioning-blueprint.md    # idempotency strategy, failure recovery
    ├── recovery-playbook.md         # 9 hostile-state drills
    └── testing-plan-revised.md      # Phase 0→A→B→C→D test plan
```

---

## The Go service

Minimal Go HTTP service at `service/` (module `lab_env/service`) — the subject of all conformance checks and fault operations.

| Endpoint | Behaviour |
|---|---|
| `GET /health` | Returns `{"status":"ok"}` — never touches state directory |
| `GET /` | Touches `/var/lib/app/state`; returns `{"status":"ok","env":"prod"}` or `{"status":"error","msg":"state write failed"}` |
| `GET /slow` | 5-second fixed delay (demonstrates B-001 proxy timeout) |

Runs as `appuser:appuser`, binds to `127.0.0.1:8080`, proxied by nginx on ports 80/443, writes structured JSON logs to `/var/log/app/app.log`.

---

## Operational documentation

| Document | Contents |
|---|---|
| `DEVELOPER-QUICKSTART.md` | Copy-paste commands from fresh VM to passing fault matrix |
| `docs/environment-test-plan.md` | Manual verification checklist, fault-by-fault verification, mutation boundary, edge cases |
| `docs/provisioning-blueprint.md` | Idempotency strategy, failure recovery, canonical artifact specification |
| `docs/component-interface-spec.md` | HTTP protocol, log schema, state machine map, telemetry schema |
| `docs/fault-implementation-guide.md` | Mutation vectors, reversion vectors, side effects for all 16 faults |
| `docs/fault-matrix-runbook.md` | Fault matrix: observable signals, diagnostic lookup table |
| `docs/recovery-playbook.md` | 9 hostile-state drills with 7-point verification checklist |
| `docs/operational-trace-spec.md` | 13 ordered event traces for every command |
| `docs/golden-baseline-ledger.md` | Frozen output surfaces, fault table, check table |
| `docs/testing-plan-revised.md` | Phase 0→A→B→C→D test plan |
| `docs/extension-boundary-note.md` | Change gates: required steps and forbidden shortcuts for every extension type |

---

## Extending the project

Before adding a fault, conformance check, command, executor method, system state, or audit entry type: read `docs/extension-boundary-note.md`. Every extension type has a required-changes checklist and a set of tests that will fail if any step is skipped.