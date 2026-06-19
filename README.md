# lab-env

A deterministic single-node chaos engineering lab: a miniature reliability control plane for fault injection, conformance validation, and state reconciliation — built as a spec-derived systems engineering exercise.

> **Execution status:** all testing phases (0–D) complete and verified on live Ubuntu 22.04 (aarch64) hardware. 47 test files, 199 test functions. 19 faults, 25 conformance checks. Schema drift locked. Golden fixtures stable. See `docs/remaining-features.md` for aspirational roadmap.

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

- **OS:** Ubuntu 22.04 LTS (amd64 / aarch64)
- **Go:** 1.22 or later (service module requires `math/rand/v2`)
- **cgroup v2** (required for MemoryMax enforcement on OOM chaos):
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

# 2. Provision (installs packages, creates service + learner users, builds binaries, starts services)
cd /opt/lab-env
sudo bash scripts/bootstrap.sh

# 3. Initialise state file
sudo lab provision

# 4. Verify
sudo lab validate
# CONFORMANT (25/25 checks pass)

# 5. Apply a fault
sudo lab fault apply F-004
# lab status → DEGRADED (F-004 active: /var/lib/app mode 000)

# 6. Diagnose
sudo lab fault info F-004
curl http://localhost/          # 500 — state dir unwritable
ls -la /var/lib/app             # drwx------ (mode 000)

# 7. Reset
sudo lab reset --tier R2
sudo lab validate               # CONFORMANT
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
         │  25 checks · severity · dependency ordering     │
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
| `conformance-model.md` | 25-check catalog, severity rules, verdict derivation, validation output schema | Semantic root — defines "correct" |
| `system-state-model.md` | 6-state machine, transition guards, detection algorithm, conflict resolution | State classification derives from conformance |
| `fault-model.md` | Fault schema, 19-fault catalog with Apply/Recover specs, reversibility rules | Faults are operators over the state machine |
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
| `lab fault list` | Read-only | List all 19 faults with ID, layer, domain |
| `lab fault info <ID>` | Read-only | Full fault entry: mutation, observable, reset tier |
| `lab fault apply <ID> [--force] [--yes]` | Mutation | CONFORMANT → DEGRADED; requires lock; PreconditionChecks enforced; post-apply verification |
| `lab reset [--tier R1\|R2\|R3]` | Mutation + verification | Restores CONFORMANT; runs post-reset validation; auto-selects tier if omitted |
| `lab provision` | Mutation | Bootstrap; idempotent |
| `lab history [--last N]` | Read-only | Ring-buffered state transition log (default: 20 entries) |

```
Global flags: --json  --quiet  --verbose  --yes
```

---

## Conformance engine

25 checks in six series. Blocking failures → exit 1 (NON-CONFORMANT). Degraded failures → exit 0 (DEGRADED-CONFORMANT).

| Series | Checks | Observes | Severity |
|---|---|---|---|
| S (system state) | S-001–S-004 | systemd unit states | blocking |
| P (process) | P-001–P-004 | running processes, bound ports | blocking |
| E (endpoint) | E-001–E-005 | HTTP responses, headers, body | blocking |
| F (filesystem) | F-001–F-007 | file existence, mode bits, DNS | blocking (F-006: degraded) |
| L (log) | L-001–L-003 | log presence, format, content | degraded |
| H (header / reset) | H-001–H-002 | proxy headers, TCP RST | blocking |

**Dependency ordering:** S-series before P-series before E-series. When S-001 fails, E-series checks run but are marked `dependent` — they failed because of S-001, not independently. Dependent failures are excluded from the failing check count.

---

## Fault catalog

19 faults across the full dependency chain: `filesystem → permissions → process → service → socket → proxy → config → network`. Each fault has a `FaultDef` (static metadata, serializable) and a `FaultImpl` (adds `Apply`/`Recover` via Executor). Postconditions are dual-representation: behavioral description + machine-verifiable failing check IDs.

| Reset tier | Meaning | Faults |
|---|---|---|
| R1 | Service restart only | F-010 |
| R2 | Restore canonical config/permissions + restart | F-001–F-007, F-009, F-013, F-015–F-021 |
| R3 | Full reprovision (binary rebuild required) | F-008, F-014 |

F-008 (SIGTERM ignored) and F-014 (zombie accumulation) are non-reversible and silent to `lab validate` while active — they manifest only at shutdown or over time. Their Apply functions return an error directing the operator to manual binary rebuild with build flags.

F-019 (block exhaustion), F-020 (CHAOS_LATENCY_MS=400), and F-021 (nftables drop rule) extend the catalog into network and storage fault domains.

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
| RunCommand on Observer; RunMutation on Executor only | `TestRunCommand_AvailableOnObserver`, `TestRunMutation_RequiresExecutor` |
| Fault Apply receives Executor, not Observer | `TestFaultApply_ReceivesExecutor_NotObserver` |
| Status endpoint codes are observer‑derived, never guessed | `TestRenderStatus_JSON_EndpointCodesNotGuessed` |

---

## Test suite

The full testing plan (Phases 0–D) has been executed on live Ubuntu 22.04 (aarch64) hardware. The unit test suite is fully green. Integration tests are gated behind `//go:build integration` and run on the VM with `LAB_TEST_MODE=live`.

**47 test files, 199 test functions** across both modules.

```
internal/conformance/runner_test.go        — classification semantics, dependency ordering
internal/conformance/runner_edge_cases_test.go — severity boundary conditions, category invariant
internal/conformance/cross_module_test.go  — cross-module endpoint contract tests
internal/state/detect_test.go              — adversarial matrix: all §4.3 conflict cases
internal/state/detect_combinations_test.go — full detection input space
internal/state/state_test.go               — six-state enumeration, transition guards
internal/state/store_test.go               — atomic write, schema, corruption recovery
internal/state/store_edge_cases_test.go    — edge cases: partial writes, concurrent access
internal/executor/audit_test.go            — mutation audit completeness invariant
internal/executor/lock_test.go             — lock contract: acquire, release, stale, live (unit)
internal/executor/lock_integration_test.go — lock tests requiring /var/lib/lab (integration)
internal/executor/boundary_test.go         — Observer ≠ Executor interface separation (6 tests)
internal/executor/embed_test.go            — embedded canonical file integrity
internal/catalog/catalog_test.go           — catalog completeness (19 faults), postcondition validity
internal/catalog/content_integrity_test.go — Apply/Recover target only declared files
internal/invariants/invariants_test.go     — cross-document invariants (19 faults × 25 checks)
internal/invariants/architecture_test.go   — import boundary enforcement
internal/output/golden_test.go             — frozen JSON contract fixtures + schema drift lock
internal/output/output_quality_test.go     — UTF-8, compactness, no double-encoding
cmd/status_test.go                         — reconciliation authority contract
cmd/validate_test.go                       — observation-only contract
cmd/fault_test.go                          — precondition/PreconditionChecks/atomicity/audit (unit)
cmd/fault_integration_test.go              — fault‑apply tests needing the lock directory (integration)
cmd/interrupt_test.go                      — interrupt path: all 8 contract points (unit)
cmd/interrupt_integration_test.go          — interrupt/status tests needing the lock directory (integration)
cmd/live_interrupt_test.go  (integration)  — real SIGINT/SIGTERM against live lab process
cmd/live_fault_matrix_test.go (integration)— full fault apply→validate→reset cycle on real VM
service/*/                                  — 12 test files covering all service packages
```

```bash
go test ./...                                       # all unit tests (silent – no permission-denied noise)
go test ./internal/invariants/...                   # cross-document invariants
go test -race ./...                                 # with race detector
UPDATE_GOLDEN=1 go test ./internal/output/...       # regenerate golden fixtures

# Integration tests (require live VM and LAB_TEST_MODE=live):
LAB_TEST_MODE=live go test -tags=integration -v -run TestLiveInterrupt ./cmd/...
LAB_TEST_MODE=live go test -tags=integration -v -run TestLiveFaultMatrix ./cmd/...
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
│   ├── fault.go                     # lab fault {list,info,apply} — post-apply verification
│   ├── reset.go                     # lab reset — tiered recovery + reset-failed
│   ├── reset_provision_history.go   # lab provision + lab history
│   ├── generate_spec_index/
│   │   └── main.go                  # spec-index markdown generator
│   ├── status_test.go
│   ├── validate_test.go
│   ├── fault_test.go
│   ├── fault_integration_test.go    # integration: fault apply tests needing lock directory
│   ├── interrupt_test.go
│   ├── interrupt_integration_test.go # integration: interrupt/status tests needing lock directory
│   ├── integration_test.go          # TestMain guard for integration tests
│   ├── live_interrupt_test.go       # integration: real SIGINT/SIGTERM tests
│   ├── live_fault_matrix_test.go    # integration: full fault catalog on real VM
│   └── testhelpers_test.go
├── internal/
│   ├── catalog/
│   │   ├── fault.go                 # FaultDef + FaultImpl types + PreconditionChecks
│   │   └── faults.go                # 19-fault catalog (F-001–F-010, F-013–F-021)
│   ├── conformance/
│   │   ├── catalog.go               # 25-check catalog + CheckByID
│   │   ├── check.go                 # Check type + CheckResult (HTTPStatusCode)
│   │   ├── observer.go              # Observer interface (read-only)
│   │   ├── runner.go                # suite runner: ordering, dependency
│   │   └── result.go                # SuiteResult and Classification
│   ├── executor/
│   │   ├── executor.go              # Executor interface (embeds Observer + 9 mutations)
│   │   ├── real.go                  # real system mutations via sudo
│   │   ├── audit.go                 # append-only audit log writer
│   │   ├── lock.go                  # advisory mutex (/var/lib/lab/lab.lock)
│   │   ├── canonical_files.go       # embedded R2 restore targets
│   │   ├── boundary_test.go         # Observer/Executor boundary tests (6 tests)
│   │   └── lock_integration_test.go # integration: lock tests requiring /var/lib/lab
│   ├── state/
│   │   ├── state.go                 # State type, All(), IsValid(), transition guards
│   │   ├── store.go                 # state.json atomic read/write
│   │   ├── detect.go                # runtime re-detection algorithm
│   │   └── detect_combinations_test.go # full detection input space tests
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
│   │   ├── invariants_test.go       # cross-document invariants
│   │   ├── architecture_test.go     # import boundary enforcement
│   │   ├── spec_index.go            # specification-to-implementation index
│   │   └── spec_index_test.go       # index validation and markdown sync
│   └── testutil/
│       └── interrupt.go             # interrupt test helpers
├── testdata/
│   └── golden/                      # frozen JSON fixtures for output regression
├── service/                         # Go HTTP service (separate module: lab_env/service, Go 1.22)
│   ├── main.go
│   ├── go.mod
│   ├── chaos/                       # chaos middleware (latency, drop, OOM)
│   ├── config/                      # YAML config loader
│   ├── logging/                     # structured JSON logger (O_APPEND)
│   ├── server/                      # HTTP handlers: /health, /, /slow, /headers, /reset
│   ├── signals/                     # PID file, healthy marker, status file
│   └── telemetry/                   # 2-second telemetry writer (12 fields)
├── scripts/
│   ├── bootstrap.sh                 # idempotent 17-step provisioning (appuser + devuser)
│   ├── validate.sh                  # 23-check shell conformance suite
│   ├── reset.sh                     # thin wrapper: exec lab reset --tier <R1|R2|R3>
│   └── run-fault-matrix.sh          # iterates all reversible faults
└── docs/
    ├── testing-plan.md              # Phase 0→A→B→C→D test plan (all phases complete)
    ├── recovery-playbook.md         # 9 hostile-state drills (verified on VM)
    ├── fault-matrix-runbook.md      # diagnostic reference for all 19 faults
    ├── fault-implementation-guide.md # mutation vectors, reversion vectors, side effects
    ├── provisioning-blueprint.md    # idempotency strategy, failure recovery
    ├── operational-trace-spec.md    # ordered event traces for every command
    ├── environment-test-plan.md     # manual verification, mutation boundary, edge cases
    ├── extension-boundary-note.md   # change gates for every extension type
    ├── remaining-features.md        # aspirational roadmap — what's left to build
    └── codebase-reference.md        # formal file-by-file reference
```

---

## The Go service

Minimal Go HTTP service at `service/` (module `lab_env/service`) — the subject of all conformance checks and fault operations.

| Endpoint | Behaviour |
|---|---|
| `GET /health` | Returns `{"status":"ok","app_env":"prod","config_loaded":true}` — never touches state directory |
| `GET /` | Touches `/var/lib/app/state`; returns `{"status":"ok","path":"/","env":"prod"}` or `{"status":"error","msg":"state write failed"}` |
| `GET /slow` | 5-second fixed delay; returns `{"status":"ok","path":"/slow","delay_seconds":5}` (demonstrates B-001 proxy timeout) |
| `GET /headers` | Echoes `Host`, `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Real-IP`, `User-Agent` from the request |
| `GET /reset` | Sets `SO_LINGER` with zero timeout, closes connection — produces TCP RST, no HTTP response |

Runs as `appuser:appuser`, binds to `127.0.0.1:8080`, proxied by nginx on ports 80/443, writes structured JSON logs to `/var/log/app/app.log`. Chaos modes: latency (CHAOS_LATENCY_MS), drop (CHAOS_DROP_PERCENT), OOM (CHAOS_OOM_TRIGGER — verified with cgroup MemoryMax). Telemetry written every 2 seconds to `/run/app/telemetry.json` (12 fields including inode_usage_percent).

---

## Operational documentation

| Document | Contents |
|---|---|
| `DEVELOPER-QUICKSTART.md` | Copy-paste commands from fresh VM to passing fault matrix |
| `docs/testing-plan.md` | Phase 0→A→B→C→D test plan (all phases complete) |
| `docs/recovery-playbook.md` | 9 hostile-state drills with 7-point verification checklist |
| `docs/fault-matrix-runbook.md` | Fault matrix: observable signals, diagnostic lookup table |
| `docs/fault-implementation-guide.md` | Mutation vectors, reversion vectors, side effects for all 19 faults |
| `docs/provisioning-blueprint.md` | Idempotency strategy, failure recovery, canonical artifact specification |
| `docs/operational-trace-spec.md` | Ordered event traces for every command |
| `docs/environment-test-plan.md` | Manual verification checklist, mutation boundary, edge cases |
| `docs/codebase-reference.md` | Formal file-by-file reference for every source file |
| `docs/remaining-features.md` | Aspirational roadmap — features still to be implemented |
| `docs/extension-boundary-note.md` | Change gates: required steps and forbidden shortcuts for every extension type |

---

## Extending the project

Before adding a fault, conformance check, command, executor method, system state, or audit entry type: read `docs/extension-boundary-note.md`. Every extension type has a required-changes checklist and a set of tests that will fail if any step is skipped.