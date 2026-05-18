# Codebase Reference

## Version 1.0.0

> **Scope:** a formal, file-by-file reference for every source file in the project. Each entry covers the file's single responsibility, its public API, its invariants, its position in the dependency graph, and design decisions embedded in the code that would otherwise require reading the source to understand.
>
> This document is descriptive, not normative. For contracts see the semantic model documents. For extension procedures see `docs/extension-boundary-note.md`.

---

## Control plane — module `lab_env`

---

## `main.go`

**Responsibility:** process adapter. The only file that calls `os.Exit`.

**Invariant:** exactly three statements — construct, run, exit. No business logic, no flag parsing, no output formatting. If any code in `main.go` would require its own unit test, it does not belong in `main.go`.

**Why it matters:** keeping `main.go` at three lines means the entire control plane is testable through `App.Run(args)` without spawning a subprocess. All `cmd/` tests construct command structs directly; none need a real binary.

---

## `app.go`

**Responsibility:** composition root and command dispatcher. Owns all concrete dependency wiring and routes `os.Args` to the correct command handler.

**Key types:**

`App{stdout, stderr}` — the only value constructed in `main.go`. `Run(args []string) int` is the process entry point; it never calls `os.Exit`.

`globalFlags{json, quiet, verbose, yes}` — parsed before the command token. `--json` and `--quiet` are mutually exclusive; `--verbose` and `--quiet` are mutually exclusive. Both checked in `parseGlobalFlags` — a conflict returns exit 2 before any command runs.

`faultApplyFlags{force, yes}` — parsed from fault subcommand args by `parseFaultApplyFlags`.

**`Run` execution sequence:**
1. `parseGlobalFlags(args)` — separates global flags from command token and subargs
2. If no command or `--help` — prints usage, returns 0
3. Selects `output.Format` (human/JSON) and constructs `output.Renderer`
4. Constructs shared dependencies: `executor.NewObserver()`, `conformance.NewRunner()`, `state.NewStore()`
5. Constructs `executor.NewAuditLogger(invocation)` with the full human-readable invocation string (e.g. `"lab fault apply F-004"`)
6. Routes via `switch command` to one of six dispatch paths
7. Renders `output.CommandResult` — data to `stdout`, errors to `stderr`
8. Returns the exit code

**Dispatch methods:** `dispatchFault`, `dispatchReset`, `dispatchProvision`, `dispatchHistory` — each constructs the command-specific executor and delegates to `cmd/`. No business logic lives here.

**Why no cobra:** the entire dispatch is a `switch` over a string. Cobra adds dependency weight and reflection-based flag binding with no gain in correctness at this complexity level.

---

## `cmd/` — command handlers

One type per `lab` subcommand. Each is a pure orchestrator: it calls into `internal/` packages but contains no business logic of its own. All dependencies are injected via constructors, making every command independently testable with stubs.

---

## `cmd/status.go`

**Responsibility:** implement `lab status` — the only command authorized to update the authoritative state classification.

**Type:** `StatusCmd{obs, runner, store, audit}`

**`Run()` sequence:**
1. `store.Read()` — read `state.json`; errors tolerated (corrupt file produces degraded result, not crash)
2. `runner.LightweightRun(obs)` — run 4 checks (P-001, S-003, P-002, E-001)
3. `state.Detect(input)` — apply detection algorithm; produces `DetectionResult{Detected, Reconciled, PriorState}`
4. If `Reconciled` — write new state to `state.json`; append `reconciliation` audit entry
5. `buildStatusResult(sf, detected, lw, readErr)` — assemble `output.StatusResult`

**`buildStatusResult` — endpoint code population (H-001 fix):** E-001 and E-002 case branches read `res.HTTPStatusCode` (populated from the check's `EndpointCode *int` closure). Zero means transport failure (connection refused), not an HTTP error. Previously E-001 guessed 502 and E-002 was absent from the map entirely. Both `http://localhost/health` and `http://localhost/` now appear in `StatusResult.Endpoints` with their actual observed codes.

**Critical invariant:** `StatusCmd` is the only command that calls `store.Write` with a modified `state` field. Enforced by `TestStatusCmd_IsOnlyReconciliationPoint`.

---

## `cmd/validate.go`

**Responsibility:** implement `lab validate` — observation-only conformance check execution.

**Type:** `ValidateCmd{obs, runner, store, audit}`

**`Run()` sequence:**
1. `runner.Run(obs)` — full 23-check suite
2. `recordObservation(sr)` — reads `state.json`, updates `last_validate` only, writes back; non-fatal if unreadable
3. Appends `validation_run` audit entry
4. Returns `output.ValidateResult`; exit 0 (conformant/degraded) or 1 (non-conformant)

**`RunSingle(checkID string)`:** runs one check by ID. Does not update `state.json`. Does not write an audit entry.

**Observation-only rule:** `recordObservation` explicitly does not touch `sf.State`. Annotated `// Intentional: we do NOT update sf.State here.` Enforced by `TestValidateCmd_WritesLastValidate_NotState`.

---

## `cmd/fault.go`

**Responsibility:** implement `lab fault list`, `lab fault info`, and `lab fault apply`.

**Types:**
- `FaultListCmd` — no executor dependency; uses `catalog.AllDefs()` only
- `FaultInfoCmd` — no executor dependency; uses `catalog.DefByID()` only
- `FaultApplyCmd{obs, runner, exec, store, audit, lock}` — full mutation authority

**`FaultApplyCmd.Run(id, force, yes)` — 6-step precondition sequence:**

| Step | Guard | Bypass | Error | Exit |
|---|---|---|---|---|
| 1 | Fault ID exists | — | `ErrUnknownFaultID` | 2 |
| 2 | State is CONFORMANT | `--force` | `ErrPreconditionNotMet` | 3 |
| 3 | No active fault | `--force` | `ErrFaultAlreadyActive` | 3 |
| 4 | `Preconditions []State` satisfied | `--force` | `ErrFaultPreconditionFailed` | 3 |
| 5 | `PreconditionChecks []string` satisfied | `--force` | `ErrFaultPreconditionFailed` | 3 |
| 6 | `RequiresConfirmation` prompt | `--yes` | Aborted by user | 0 |

Step 5 resolves each ID via `conformance.CheckByID(checkID)` then calls `check.Execute(obs)`. An unknown check ID returns internal error (exit 2) — catalog integrity violation, not an operator error.

**After preconditions:** acquires lock → TOCTOU re-read → `fault.Apply(exec)` → writes DEGRADED state with `active_fault` record → appends history → writes `state_transition` audit entry.

**Atomicity invariant:** if `Apply(exec)` returns an error, `state.json` is not updated. Enforced by `TestFaultApplyCmd_ApplyFailure_DoesNotUpdateState`.

---

## `cmd/reset.go`

**Responsibility:** implement `lab reset` — tiered environment recovery.

**Type:** `ResetCmd{obs, runner, exec, store, audit, lock}`

**`Run(tier string)` sequence:**
1. Acquires lock
2. Reads state file (defaults to BROKEN if unreadable)
3. If `tier == ""` — `selectTier(sf)` for auto-selection
4. Early return if already CONFORMANT with no active fault and validation passes
5. `executeTier(tier, sf)` — tier-specific operations including Recover
6. Runs full conformance suite post-reset
7. Updates state file: new state, clears `active_fault`, sets `last_reset` and `last_validate`
8. Appends history entry and `state_transition` audit entry

**`selectTier(sf)`:** returns `sf.ActiveFault.ID`'s declared `ResetTier` via `catalog.ByID`, or `"R2"` as the safe default for BROKEN/unknown state.

**`executeTier(tier, sf)`:** calls `fault.Recover(exec)` for reversible active faults before tier operations, then:
- **R1:** `Systemctl("restart", app)` + `NginxReload()`
- **R2:** `RestoreFile` for each path in `cfg.R2RestoreFiles` → `Chmod` for each mode in `cfg.R2RestoreModes` → `daemon-reload` → `restart` → `NginxReload()`
- **R3:** `RunMutation("bash", cfg.BootstrapScript)` — full reprovision

Post-reset validation always runs; failure produces exit 1 with both `Value` and `Err` populated.

---

## `cmd/reset_provision_history.go`

**Responsibility:** implement `lab provision` and `lab history`. (Reset was extracted to `reset.go`.)

**`ProvisionCmd.Run()`:** acquires lock → `exec.RunMutation("bash", cfg.BootstrapScript)` → runs validation → writes CONFORMANT or BROKEN to a fresh `state.File` → writes `ProvisionRecord`.

**`HistoryCmd.Run(last int)`:** reads `state.json` → slices to last N entries → reverses to return most recent first. No lock; read-only.

---

## `cmd/testhelpers_test.go`

**Responsibility:** shared test infrastructure for all cmd tests. Never imported by production code.

- `stubObserver` — implements `conformance.Observer` with configurable per-check responses
- `healthyObs()` — stub where every check returns a passing value
- `newTrackingExecutor()` — records all mutation calls; used to verify atomicity and Apply side effects
- `mockFileInfo` — implements `fs.FileInfo` for Stat-based tests

---

## `cmd/live_interrupt_test.go`

**Responsibility:** integration tests for the interrupt contract against a real VM.

**Build tag:** `//go:build integration`

**`TestLiveInterrupt_Reset_ExitsCode4`** — 9-step sequence: apply F-001 → start `lab reset` as subprocess → send `SIGINT` → assert exit 4 → assert `classification_valid: false` in `state.json` → assert `entry_type: "interrupt"` in `audit.log` → assert `lab status` does not assert BROKEN → assert `lab status` reclassifies. Deferred cleanup restores environment.

**`TestLiveInterrupt_BeforeMutation_ExitsCleanly`** — SIGINT immediately after process start: exit 0 or 4 acceptable; exit 1 must not occur.

**`TestLiveInterrupt_SIGTERM_SameBehaviorAsSIGINT`** — SIGTERM produces exit 4.

---

## `cmd/live_fault_matrix_test.go`

**Responsibility:** integration tests for the full fault catalog on a real VM.

**Build tag:** `//go:build integration`

**`TestLiveFaultMatrix_AllReversible`** — table-driven over all 14 reversible faults: pre-flight → apply → assert DEGRADED with correct `active_fault` → compare `lab validate --json` failing checks against `FaultDef.PostconditionSpec.FailingChecks` → reset at declared tier → assert CONFORMANT. Cleanup deferred.

**`TestLiveFaultMatrix_F010_PreconditionCheckEnforced`** — stops app service, verifies F-010 returns exit 3.

**`TestLiveFaultMatrix_F018_InodeExhaustion`** — verifies `/health` → 200, `/` → 500, `df -i` near 100%.

---

## `internal/catalog/fault.go`

**Responsibility:** define the two-type fault model.

**`FaultDef`** — static, JSON-serializable. Key fields:

| Field | Type | Notes |
|---|---|---|
| `ID` | `string` | F-NNN identifier |
| `IsReversible` | `bool` | False only for F-008, F-014 |
| `ResetTier` | `string` | R1, R2, or R3; drives `ResetCmd.selectTier` |
| `Preconditions` | `[]state.State` | States required before Apply; standard: `[CONFORMANT]` |
| `PreconditionChecks` | `[]string` | Check IDs run as live observations; non-empty only for F-010 (`[P-001]`) |
| `RequiresConfirmation` | `bool` | True for F-008/F-014 |
| `Postcondition` | `PostconditionSpec` | `FailingChecks []string` for machine verification; empty for F-008/F-014 |

**`FaultImpl`** — pairs `*FaultDef` with `Apply` and `Recover` functions of type `func(executor.Executor) error`. Uses a pointer to `FaultDef` (not embedding) to allow composite literal syntax in fault constructors.

**Why the two-type split:** `lab fault list` and `lab fault info` need only `FaultDef` — no executor dependency. Only mutation commands need `FaultImpl`.

---

## `internal/catalog/faults.go`

**Responsibility:** implement all 16 fault constructors and provide the catalog lookup API.

**Catalog:** F-001–F-010, F-013–F-018. F-011 and F-012 are intentionally absent — baseline network behaviours (`fault-model.md §10`). `TestInvariant_NoBaselineFaultsInCatalog` enforces absence.

**Lookup API:** `AllImpls()`, `AllDefs()`, `All()` (alias), `ImplByID(id)`, `DefByID(id)`, `ByID(id)` (alias for ImplByID).

**Apply/Recover patterns:**

| Pattern | Faults | Apply mechanism | Recover mechanism |
|---|---|---|---|
| Single chmod | F-003, F-004, F-005, F-009 | `exec.Chmod(path, badMode)` | `exec.Chmod(path, canonicalMode)` |
| File delete | F-001 | `exec.Remove(path)` | `exec.RestoreFile` + restart |
| Config content replace | F-002, F-016 | ReadFile → replace → WriteFile → restart | `exec.RestoreFile` → restart |
| Unit file modify | F-006, F-013 | Modify file → daemon-reload | RestoreFile → daemon-reload → restart |
| Nginx config modify | F-007, F-015 | Modify file → NginxReload | RestoreFile → NginxReload |
| Env var override | F-017 | `systemctl set-environment APP_ENV=` → restart | `systemctl unset-environment APP_ENV` → restart |
| Binary rebuild | F-008, F-014 | `go build -ldflags "..."` → restart | Returns error directing to R3 |
| File delete (open FD) | F-010 | `exec.Remove(log path)` [P-001 required] | `exec.Systemctl("restart", app)` |
| Mass file create | F-018 | `for i in ...; do touch ...; done` via RunMutation | `rm -f /var/lib/app/file_*` via RunMutation |

---

## `internal/conformance/observer.go`

**Responsibility:** define the `Observer` interface — the read-only observation boundary.

**8 methods:**

| Method | Returns | Purpose |
|---|---|---|
| `Stat(path)` | `(fs.FileInfo, error)` | File metadata |
| `ReadFile(path)` | `([]byte, error)` | File contents |
| `CheckProcess(name, user)` | `(ProcessStatus, error)` | Process existence |
| `CheckPort(addr)` | `(PortStatus, error)` | TCP listen socket |
| `CheckEndpoint(url, skipTLS)` | `(EndpointStatus, error)` | HTTP GET; `StatusCode` carries real HTTP code |
| `ResolveHost(name)` | `(string, error)` | DNS A record |
| `ServiceActive(unit)` | `(bool, error)` | systemctl is-active |
| `ServiceEnabled(unit)` | `(bool, error)` | systemctl is-enabled |

**Design invariant:** `Observer` is defined in `conformance`, not `executor`. This means `internal/conformance` has zero dependency on `internal/executor`. Conformance checks are structurally incapable of calling mutation methods. The dependency flows one way: `executor` embeds `conformance.Observer` — not the reverse.

---

## `internal/conformance/check.go`

**Responsibility:** define `Check` (check definition) and `CheckResult` (execution outcome).

**`Check` key fields:**

| Field | Type | Notes |
|---|---|---|
| `ID` | `string` | S-001, P-002, E-003, F-004, L-001 |
| `Category` | `Category` | S/P/E/F/L — determines execution order |
| `Severity` | `Severity` | Blocking (exit 1) or Degraded (exit 0) |
| `Execute` | `func(Observer) error` | nil = pass; non-nil error = fail |
| `EndpointCode` | `*int` | E-series only: pointer written by Execute with actual HTTP status code |

**`CheckResult` key fields:**

| Field | Type | Notes |
|---|---|---|
| `Passed` | `bool` | True if Execute returned nil |
| `Dependent` | `bool` | True for E-series failures caused by S-001 failure |
| `HTTPStatusCode` | `int` | Actual HTTP code for E-series; 0 for non-endpoint or transport failure; H-001 fix |

---

## `internal/conformance/catalog.go`

**Responsibility:** implement all 23 check definitions and provide `CheckByID`.

**`Catalog() []*Check`** — all 23 checks in S→P→E→F→L order. Every `Execute` uses only `Observer`. Path constants from `internal/config`.

**`CheckByID(id string) *Check`** — iterates Catalog; nil if not found. Used by `cmd/fault.go` to resolve `PreconditionChecks` entries.

**E-001 and E-002 — H-001 fix pattern:** constructed via IIFE to capture a per-check `code` variable shared between the Execute closure (writer) and `EndpointCode` pointer (reader):

```go
func() *Check {
    code := 0
    return &Check{
        EndpointCode: &code,
        Execute: func(o Observer) error {
            ep, _ := o.CheckEndpoint(...)
            code = ep.StatusCode   // write into shared var
            if ep.StatusCode != 200 { return fmt.Errorf(...) }
            return nil
        },
    }
}()
```

**Series authority and severity:**

| Series | Checks | Observes | Severity |
|---|---|---|---|
| S | S-001–S-004 | systemd unit states | Blocking |
| P | P-001–P-004 | Process, ports | Blocking |
| E | E-001–E-005 | HTTP responses, headers, TLS | Blocking |
| F | F-001–F-007 | Files, modes, DNS | Blocking (F-006: Degraded) |
| L | L-001–L-003 | Log file, JSON, startup entry | Degraded |

---

## `internal/conformance/runner.go`

**Responsibility:** execute checks in dependency order and provide three run variants.

**`Run(o Observer) *SuiteResult`** — full 23-check run, S→P→E→F→L. E-series pre-marked `Dependent = true` if S-001 failed (they still execute).

**`runOne(check, o, s001Passed) CheckResult`** — executes one check; copies `*check.EndpointCode` into `res.HTTPStatusCode` after Execute (H-001 propagation point).

**`LightweightRun(o Observer) *SuiteResult`** — runs P-001, S-003, P-002, E-001 only. Used by `lab status` to avoid full-suite overhead on every invocation.

**`RunIDs(ids, o) []*CheckResult`** — runs named checks in catalog order. Used for postcondition verification and `lab validate --check <ID>`.

---

## `internal/conformance/cross_module_test.go`

**Responsibility:** verify that the conformance check logic and the service's HTTP handler output are mutually consistent. Catches mismatches that pass in each package's own unit tests but fail in integration.

**Why this file exists:** conformance checks and service handlers are tested in separate packages. A subtle divergence — for example, E-003 asserting `{"status":"ok"}` while the handler produces `{"status": "ok"}` with an added space — would never be caught by either package alone. This file wires actual check `Execute` functions against actual handler responses via `httptest.Server`.

**Tests:**
- `TestE003_CheckLogic_MatchesHandlerResponse` — E-003 body assertion against the exact handler response string; any discrepancy between check assertion and handler output fails here
- `TestE001_CheckLogic_MatchesHandlerResponse` — E-001 passes on a 200 `/health` response
- `TestE002_CheckLogic_FailsOn500` — E-002 fails on a 500 `/` response; confirms the check correctly detects the F-004 diagnostic pattern
- `TestF004DiagnosticPattern_E001PassesE002Fails` — the complete F-004 pattern verified end-to-end: `/health` → 200 (E-001 passes), `/` → 500 (E-002 fails) simultaneously

---

## `internal/conformance/result.go`

**Responsibility:** define `SuiteResult` and `Classification` with derivation rules.

**`SuiteResult`** — `Results []CheckResult`, `Passed int`, `Total int`, `FailingBlockingIDs []string` (excludes dependent failures), `Classification`, `At time.Time`.

**Classification derivation:**
- All pass → `ClassConformant`
- All blocking pass, degraded failures present → `ClassDegradedConformant`
- Any blocking fails → `ClassNonConformant`

**`IsConformant()`** — true for conformant and degraded-conformant. `ExitCode()` — 0 for both; 1 for non-conformant.

---

## `internal/state/state.go`

**Responsibility:** define the `State` type and transition guard methods.

**Six states:**

| State | Entry condition |
|---|---|
| `UNPROVISIONED` | Bootstrap not run |
| `PROVISIONED` | Bootstrap complete; conformance unverified |
| `CONFORMANT` | All blocking checks pass; no active fault |
| `DEGRADED` | Exactly one fault active; `active_fault` non-null |
| `BROKEN` | Blocking checks fail; no active fault explaining it |
| `RECOVERING` | Reset in progress; transitional |

**Guard methods:**

| Method | Logic |
|---|---|
| `CanApplyFault(force)` | Without force: CONFORMANT only. With force: all except RECOVERING |
| `CanReset()` | Always true |
| `RequiresActiveFault()` | Only DEGRADED (invariant I-2, system-state-model §5.2) |
| `ForbidsActiveFault()` | All states except DEGRADED; strict complement |
| `All()` | All six in definition order |
| `IsValid(s)` | True for the six defined values only |

---

## `internal/state/store.go`

**Responsibility:** persist and retrieve `state.json`. Define the complete state file schema.

**`File` schema:**

| Field | JSON key | Notes |
|---|---|---|
| `SpecVersion string` | `spec_version` | `"1.0.0"` |
| `State State` | `state` | Authoritative classification |
| `ClassificationValid bool` | `classification_valid` | False after interrupt; forces re-detection |
| `ActiveFault *ActiveFault` | `active_fault` | Non-null only when State == DEGRADED |
| `LastValidate *ValidateRecord` | `last_validate` | Most recent `lab validate` summary |
| `LastReset *ResetRecord` | `last_reset` | Most recent `lab reset` record |
| `LastProvision *ProvisionRecord` | `last_provision` | Most recent `lab provision` record |
| `LastStatusAt *time.Time` | `last_status_at` | Most recent `lab status` timestamp |
| `History []HistoryEntry` | `history` | Ring buffer; cap 50 |

**`Write(f *File) error`** — atomic temp+rename: same-directory temp → write → `Sync()` → close → `Chmod(0644)` → `Rename`. Temp file cleaned up on any failure path.

**`Read() (*File, error)`** — `ErrStateFileNotFound` if absent; `ErrStateFileCorrupt` for empty, whitespace-only, invalid JSON, or unrecognized state value.

**`InvalidateClassification(f *File) error`** — sets `ClassificationValid = false` and writes. Called by interrupt handler to signal the recorded state may be stale.

**`AppendHistory(entry, f)`** — appends and truncates front to 50. Mutates `f` in place; caller writes.

---

## `internal/state/detect.go`

**Responsibility:** implement the state detection algorithm (system-state-model §4.2) and conflict resolution rules (§4.3).

**`Detector{obs, store}`** — `Detect(lw *SuiteResult, sf *File) DetectionResult`

**Algorithm:**
1. If `ClassificationValid == false` — ignore recorded state; re-derive entirely from `lw`
2. Evaluate lightweight suite results
3. If all pass: active fault recorded → DEGRADED; no fault → CONFORMANT (reconcile if prior was BROKEN/DEGRADED)
4. If any fail: fault recorded and failure consistent with postcondition → DEGRADED; else → BROKEN (clear active fault)

**Conflict resolution:**

| Case | Suite | Recorded | Resolution | Rationale |
|---|---|---|---|---|
| 1 | passes | DEGRADED | CONFORMANT | Fault cleared outside control plane |
| 2 | fails | CONFORMANT | BROKEN | Unexpected failure |
| 3 | passes | BROKEN | CONFORMANT | Recovery without control plane |
| 4 | contradictory | any | Runtime wins | Runtime is highest authority |

---

## `internal/executor/executor.go`

**Responsibility:** define the `Executor` interface — mutation authority boundary.

**9 mutation methods** (all audited) in addition to the 8 inherited `Observer` methods:

| Method | Action |
|---|---|
| `WriteFile(path, data, mode, owner, group)` | Atomic temp+rename write |
| `Chmod(path, mode)` | Mode bits via sudo |
| `Chown(path, owner, group)` | Ownership via sudo |
| `Remove(path)` | File deletion via sudo |
| `MkdirAll(path, mode, owner, group)` | Directory creation via sudo |
| `Systemctl(action, unit)` | systemctl via sudo |
| `NginxReload()` | `nginx -t && nginx -s reload` via sudo |
| `RestoreFile(path)` | WriteFile from embedded canonical content |
| `RunMutation(cmd, args...)` | Arbitrary audited privileged command |

**`RunMutation` vs `Observer.RunCommand`:** `RunMutation` = audited mutation path. `RunCommand` = unaudited read-only path. Fault Apply/Recover must use `RunMutation` for mutations. Enforced by `TestFaultApply_ReceivesExecutor_NotObserver`.

---

## `internal/executor/real.go`

**Responsibility:** concrete `Executor` and `Observer` implementations against the Ubuntu VM.

**`NewObserver()`** — `&Real{}` with no audit logger; read-only operations only.

**`NewExecutor(audit *AuditLogger)`** — `&Real{audit: audit, canonMap: buildCanonicalMap()}`. Every mutation method calls `audit.LogOp(...)` before executing.

**`CheckEndpoint`** — HTTP GET with 10-second timeout. `tls.Config{InsecureSkipVerify: skipTLS}` for HTTPS. Returns `EndpointStatus{StatusCode: resp.StatusCode}`. On transport error: `StatusCode: 0`.

**`atomicWrite`** — same-directory temp → write data → `Sync()` → close → rename. `fsync` before rename prevents silent data loss on crash.

**`runSudo`** — `sudo -n` (non-interactive; fails immediately if password required rather than hanging). Captures stdout+stderr; returns combined output on error for diagnostics.

---

## `internal/executor/canonical_files.go`

**Responsibility:** embed R2 restore targets at build time; implement `RestoreFile`.

**Embedded:** `app.service`, `config.yaml`, `nginx.conf` via `go:embed`.

**`init()`** — populates `canonicalFiles map[string]canonicalFileEntry{content, mode, owner, group}`. Modes and ownership come from `internal/config` constants.

**`RestoreFile(path)`** — lookup in map → `WriteFile` with canonical content, mode, ownership. Returns distinct error for unknown paths.

**Why embedded:** canonical bytes must survive on-disk corruption. If a fault deletes or corrupts a config file, the binary still has the correct content. `go:embed` makes this a build-time guarantee.

---

## `internal/executor/audit.go`

**Responsibility:** append-only audit log (control-plane-contract §7).

**`AuditEntry` — six `entry_type` values:**
1. `executor_op` — before each mutation; `op`, `op_args`, `exit_code`, `duration_ms`
2. `state_transition` — CONFORMANT→DEGRADED etc.; `fault_id` when relevant
3. `validation_run` — `lab validate` results
4. `reconciliation` — `lab status` reconciliation
5. `interrupt` — SIGINT/SIGTERM during mutation
6. `error` — command/executor errors

**`append` method** — `O_APPEND|O_CREATE|O_WRONLY` open → marshal → write → close. Separate open/close per entry; no persistent file handle.

**Ordering invariant:** `LogOp` is called before the mutation begins. If the process is killed mid-mutation, the audit log shows the attempted operation with no corresponding success entry — the gap is itself diagnostic signal.

---

## `internal/executor/lock.go`

**Responsibility:** advisory mutation lock at `config.LockPath` (`/var/lib/lab/lab.lock`).

**`Acquire()` algorithm:**
1. `O_CREATE|O_EXCL|O_WRONLY` — atomic create; succeeds only if file absent
2. On `EEXIST`: read PID → `processRunning(pid)` via `kill -0`
3. Dead or invalid PID → `os.Remove` → recurse (reclaim stale)
4. Live PID → `ErrLockHeld{HolderPID}`

**`processRunning(pid)`** — `proc.Signal(syscall.Signal(0))`. `syscall.EPERM` (process exists but owned by different user) also returns true.

**PID 1 handling:** PID 1 is always live; a lock file with PID 1 is treated as stale and reclaimed. Prevents a PID-collision edge case from permanently blocking operations.

**Why advisory:** serializes `lab` command mutations; does not prevent direct system modifications outside the control plane.

---

## `internal/output/model.go`

**Responsibility:** define one result type per command and the `CommandResult` wrapper.

**`CommandResult{Value any, ExitCode int, Err error}`** — universal return type. Both `Value` and `Err` may be non-nil on partial failure (reset completed but post-reset validation failed).

**Result types:**

| Type | Command | Notable field |
|---|---|---|
| `StatusResult` | `lab status` | `Endpoints map[string]int` — actual HTTP codes, 0 = unreachable |
| `ValidateResult` | `lab validate` | `CheckResultItem.Dependent` preserved |
| `FaultListResult` | `lab fault list` | `FaultSummary` condensed view |
| `FaultInfoResult` | `lab fault info` | Full `FaultDef` flattened |
| `FaultApplyResult` | `lab fault apply` | `Applied bool`, `Aborted bool`, `FromState`, `ToState` |
| `ResetResult` | `lab reset` | `Tier`, `FaultCleared`, `Suite`, `DurationMs` |
| `ProvisionResult` | `lab provision` | `ToState`, `Suite`, `DurationMs` |
| `HistoryResult` | `lab history` | `Entries []HistoryItem` (reverse-chronological) |

---

## `internal/output/render.go`

**Responsibility:** render command results for human and JSON output.

**`Renderer{stdout, stderr, format, quiet}`** — `Render(value)` dispatches on concrete result type. `Errorf` writes to `stderr` always (not suppressed by quiet).

**Stream discipline:** data → `stdout`; diagnostic messages → `stderr`. Enables `lab status --json | jq .state` without interference.

**JSON format:** `json.NewEncoder.SetIndent("", "  ")`. Golden fixtures are the authoritative expected output. `UPDATE_GOLDEN=1 go test ./internal/output/...` regenerates after schema changes.

**Human format:** implementation-defined; not a frozen surface.

---

## `internal/output/output_quality_test.go`

**Responsibility:** byte-level quality tests for all rendered JSON output. Guards against formatting regressions that would break machine consumers silently — the golden tests verify shape and values, but not byte-level properties.

**Tests:**
- `TestOutput_AllRenderedJSON_IsValidUTF8` — all result types rendered to JSON contain only valid UTF-8 sequences; invalid UTF-8 in a JSON string is technically invalid JSON and rejected by strict parsers
- `TestOutput_AllRenderedJSON_NoTrailingWhitespace` — no line in any rendered output has trailing spaces or tabs; prevents strict diff-based golden fixture comparison failures
- `TestOutput_JSON_IsCompactNotPretty` — JSON output has no indentation; compact format is the standard for machine-readable output
- `TestOutput_StatusResult_LastValidateTimestamp_IsRFC3339` — the `last_validate` field round-trips as a valid RFC3339 string; a non-RFC3339 format would break dashboard parsers
- `TestOutput_JSON_NoDoubleEncoding` — output does not contain `\"` escape sequences indicating a pre-marshaled string was marshaled again

---

## `internal/config/config.go`

**Responsibility:** single source of truth for all canonical environment constants. No path string, user string, mode constant, or service name appears in any other file.

**Constant groups:** Paths (`StatePath`, `AuditPath`, `LockPath`, `BinaryPath`, `ConfigPath`, `LogPath`, `UnitFilePath`, `NginxConfigPath`, `TLSCertPath`, `BootstrapScript`), Services (`AppServiceName`, `NginxServiceName`, `AppLocalHostname`), Ownership (`AppUser`, `AppGroup`, `AppUID=1001`, `AppGID=1001`), Modes (`ModeBinary=0750`, `ModeConfig=0640`, `ModeLogDir=0755`, `ModeUnitFile=0644`), R2 restore targets (`R2RestoreFiles []string`, `R2RestoreModes []ModeEntry`).

---

## `internal/config/app.service`

**Responsibility:** canonical systemd unit file. Embedded; R2 restore target.

**Fault-relevant directives:**

| Directive | Fault |
|---|---|
| `ExecStart=/opt/app/server` | F-013 replaces path with `/opt/app/DOESNOTEXIST` |
| `Environment=APP_ENV=prod` | F-006 removes this line; F-017 overrides at manager level |
| `TimeoutStopSec=10` | F-008: SIGTERM masked → systemd waits 10s then SIGKILL |
| `RuntimeDirectory=app` | systemd creates `/run/app/` as tmpfs on start |
| `Restart=on-failure` / `StartLimitBurst=5` | Service restarts up to 5 times in 30s after crash |

---

## `internal/config/config.yaml`

**Responsibility:** canonical app config. Embedded; R2 restore target.

```yaml
server:
  addr: 127.0.0.1:8080    # F-002 → 9090; F-016 → 0.0.0.0:8080
app_env: prod               # F-006 / F-017 empty this
```

---

## `internal/config/nginx.conf`

**Responsibility:** canonical nginx reverse proxy config. Embedded; R2 restore target.

**Fault-relevant sections:**

| Section | Fault |
|---|---|
| `upstream app_backend { server 127.0.0.1:8080; }` | F-007 changes to `9090`; all proxy blocks break |
| `add_header X-Proxy nginx always` | Required by E-004 |
| `proxy_read_timeout 3s` | Shorter than `/slow` 5s → B-001 baseline (504 via proxy) |
| F-015 append target | `invalid_directive on;` appended → nginx -t fails → F-005 check fails |

---

## `internal/config/logrotate.conf`

**Responsibility:** canonical logrotate config. NOT in `R2RestoreFiles`; provisioning-only; R3 restores.

`copytruncate` required because the service uses `O_APPEND` and has no SIGHUP handler. Without it, rotate would leave the service writing to a deleted inode (L-series failures). `lastaction` re-applies ownership after each rotation.

---

## `internal/invariants/doc.go`

Package stub. No production code. Exists to declare the package.

---

## `internal/invariants/spec_index_test.go`

**Responsibility:** verify the integrity of the reverse index declared in `spec_index.go`. The 14 tests collectively guarantee that the index is accurate, complete, structurally sound, synchronized with the markdown document, and immune to silent drift.

**Tests and their specific guarantees:**
- `TestSpecIndex_AllReferencedFilesExist` — every path in `ImplFiles` and `TestFiles` resolves on disk; catches renames and deletions
- `TestSpecIndex_AllSectionsHaveAtLeastOneFile` — no entry has zero files without an explicit `TestOnly` or `CrossRef` marker
- `TestSpecIndex_NoDuplicateSections` — no `(Doc, Section)` pair appears twice
- `TestSpecIndex_AllFiveDocumentsCovered` — every document in `DocOrder` has at least one entry; derived from `DocOrder` (not a separate list) so adding a sixth document automatically requires new entries
- `TestSpecIndex_NoRelativePathPrefixes` — all paths are module-root-relative without `./` or `../`
- `TestSpecIndex_CrossRefEntriesHaveNoImplFiles` — cross-reference sections carry no implementation files
- `TestSpecIndex_TestOnlyEntriesHaveNoImplFiles` — test-only sections carry no production implementation files
- `TestSpecIndex_DocumentsExistOnDisk` — the five spec document filenames resolve on disk; catches a renamed document before any other test fires
- `TestSpecIndex_MarkerGuardsExist` — both `BEGIN GENERATED` and `END GENERATED` HTML comments are present and ordered in `codebase-reference.md`
- `TestSpecIndex_MarkdownIsUpToDate` — the committed markdown section matches `GenerateMarkdown()` output exactly; guard-comment extraction is immune to `##` headings inside the section
- `TestSpecIndex_GeneratedMarkdownStructure` — every table row has exactly 5 columns; no adjacent pipes (empty cell)
- `TestSpecIndex_NoUndocumentedSections` — every `§`-prefixed top-level heading in each spec document has a corresponding `SpecIndex` entry; fails hard if a document is not found on disk; minimum-headings guard prevents vacuous pass
- `TestSpecIndex_EntriesFollowDocumentOrder` — `SpecIndex` entries for each document appear in the same order as sections appear in the document; fails hard if document not found
- `TestSpecIndex_ConstraintPathsExist` — file paths embedded in `Constraints` strings are verified on disk

---

## `internal/invariants/generate_spec_index.go`

**Responsibility:** regenerate the `Specification → Implementation Index` section of `docs/codebase-reference.md` from the `SpecIndex` declared in `spec_index.go`. Build tag: `//go:build ignore` — not compiled into the test binary.

**Usage:**
```bash
go run internal/invariants/generate_spec_index.go
# or via go:generate directive in spec_index.go:
go generate ./internal/invariants/
```

**Replacement logic:** finds the `BEGIN GENERATED` and `END GENERATED` HTML comment guards in `codebase-reference.md` and replaces only the content between them. Content before the `BEGIN` guard and after the `END` guard is preserved unchanged. Falls back to finding the old `## Specification → Implementation Index` heading if guards are not yet present (first-generation case).

**CI integration:** the file's header includes the exact CI recipe to prevent stale markdown from being committed:
```bash
go generate ./internal/invariants/
git diff --exit-code docs/codebase-reference.md || \
  (echo "codebase-reference.md is out of sync; run go generate" && exit 1)
```

---

## `internal/invariants/invariants_test.go`

**Responsibility:** enforce cross-document architectural invariants spanning package boundaries.

**Key invariants and why they must be cross-package:**

| Invariant | Requires |
|---|---|
| Fault `FailingChecks` exist in conformance catalog | `catalog` + `conformance` |
| Degraded checks are non-blocking | `catalog` + `conformance` |
| F-011/F-012 absent | catalog count + ID list |
| Non-reversible faults require R3 + confirmation | Both fields of `FaultDef` |
| All `PreconditionChecks` resolve to real check IDs | `catalog.AllDefs` + `conformance.CheckByID` |
| F-010 declares `PreconditionChecks: [P-001]` | Pinned fault-specific invariant |
| Exactly 16 faults, 23 checks, 6 states | Pinned counts across three packages |

Each test names the spec document and section it enforces in its comment.

---

## `internal/invariants/architecture_test.go`

**Responsibility:** enforce import boundaries via `go list` dependency inspection.

**Enforced rules:**

| Rule | Test |
|---|---|
| No production code imports `testing` | `TestInvariant_NoProductionImportsTesting` |
| No production code imports `testutil` | `TestInvariant_NoProductionImportsTestutil` |
| `conformance` does not import `executor` | `TestInvariant_ConformanceDoesNotImportExecutor` |
| `catalog` does not import `state` | `TestInvariant_CatalogDoesNotImportState` |
| `output` does not import `conformance` | `TestInvariant_OutputDoesNotImportConformance` |
| Service does not import control plane | `TestInvariant_ServiceDoesNotImportControlPlane` |

---

## `internal/testutil/interrupt.go`

**Responsibility:** `InterruptableExecutor` — fires `context.CancelFunc` after N mutation calls. Used by `cmd/interrupt_test.go` to prove the interrupt-path contract without real OS signals.

Must never be imported by production code. Enforced by `architecture_test.go`.

---

## `testdata/golden/`

Six frozen JSON fixtures for output regression testing. `UPDATE_GOLDEN=1 go test ./internal/output/...` regenerates after schema changes.

| File | Scenario | Notable invariant |
|---|---|---|
| `status_conformant.json` | CONFORMANT, no fault | `active_fault` is explicit `null`, not omitted |
| `status_degraded.json` | DEGRADED, F-004 active | All `ActiveFault` fields present |
| `status_broken.json` | BROKEN, unreachable | Endpoint code is 0, not 502 (H-001) |
| `validate_conformant.json` | 23 checks pass | `classification == "CONFORMANT"` |
| `fault_apply_success.json` | F-004 applied | `from_state: "CONFORMANT"`, `to_state: "DEGRADED"` |
| `fault_info_f004.json` | `lab fault info F-004` | All `FaultInfoResult` fields present |

---

## `scripts/bootstrap.sh`

**Responsibility:** idempotent 16-step provisioning of the canonical Ubuntu 22.04 environment. Called by `lab provision` and R3 reset via `exec.RunMutation("bash", cfg.BootstrapScript)`. Can also be run directly: `sudo bash /opt/lab-env/scripts/bootstrap.sh`.

**Resume strategy:** every step checks current state before acting. Re-running after a failure at any step is safe — completed steps detect their work is done and skip. The ERR trap emits `[bootstrap] FAILED at step: <name> (exit <rc>)` and a journalctl diagnosis command on any failure.

**16 steps in order:**

| Step | Name | Idempotency mechanism |
|---|---|---|
| 01 | `root-check` | Guard: `id -u == 0`; fails immediately if not root |
| 02 | `packages` | `apt-get install -y` — idempotent by design |
| 03 | `user` | Guard: `getent group appuser` and `id appuser`; fatal on UID/GID mismatch with 1001/1001 |
| 04 | `directories` | `install -d` — idempotent |
| 05 | `loopback-mount` | Guard: `mountpoint -q /var/lib/app`; creates 50 MiB sparse ext4 image if absent |
| 06 | `cgroup-slice` | Always overwrites `app.slice` unit; `daemon-reload` always runs |
| 07 | `config-files` | Guard: `[[ ! -f path ]]`; does NOT restore drift — intentional (only installs if absent) |
| 08 | `build` | Always rebuilds from source via `go build` to a temp path then renames |
| 09 | `systemd-unit` | Always overwrites; `daemon-reload` always runs |
| 10 | `tls-cert` | Guard: file exists AND `openssl x509 -checkend 0` passes (not expired) |
| 11 | `hosts` | Guard: `grep -qF app.local /etc/hosts` |
| 12 | `nginx` | Always overwrites; `nginx -t` validates before reload |
| 13 | `logrotate` | Always overwrites |
| 14 | `nftables` | Guard: `nft list table`/`nft list chain` before creating LAB-FAULT chain |
| 15 | `sudoers` | Always overwrites; `visudo -c` validates before install |
| 16 | `services-and-validate` | `systemctl enable` + restart; polls `/run/app/healthy`; runs `lab validate` as final gate |

**Step 07 idempotency note:** config files are installed only if absent. This is intentional — re-running bootstrap after a learner has modified a config file does not clobber their changes. The R2 reset (via `lab reset --tier R2`) is the correct mechanism for restoring canonical config content.

**Step 10 TLS note:** the certificate is regenerated if absent or expired. The subject is `app.local`; the SAN covers both `app.local` and `127.0.0.1`. A 365-day validity is used; after one year `lab reset --tier R3` regenerates the cert.

---

## `scripts/reset.sh`

**Responsibility:** thin shell wrapper over `lab reset --tier`. Provides a flag-based interface for operators who prefer named flags over explicit tier arguments. Authority: `canonical-environment.md §8.2`.

**Flag → tier mapping:**

| Flag | Tier | Operation |
|---|---|---|
| `--config` | R2 | Restore canonical config files + restart services |
| `--permissions` | R2 | Restore canonical ownership and mode bits |
| `--logs` | R1 | Truncate `app.log` to empty; service continues running |
| `--state` | R1 | Remove and recreate `/var/lib/app/state`; service continues running |
| `--full` | R3 | Full reprovision — re-runs `bootstrap.sh` |

**Implementation:** uses `exec "${LAB_BIN}" reset --tier <X>` to replace the shell process with the lab binary. The lab binary's exit code, stdout, and stderr pass through directly — no wrapping logic. Pre-conditions: binary exists and is executable; caller is root. Both are checked before `exec` with clear error messages.

**What this script does not do:** fault apply and fault recovery are not routed through `reset.sh`. The comments explicitly state: `lab fault apply <ID>` to apply a fault; `lab reset --tier <R>` to reverse it. This prevents the script from becoming a catch-all mutation interface.

**Exit codes:** mirror `lab reset` exit codes exactly (control-plane-contract §3.2): 0 = CONFORMANT after reset; 1 = reset failed; 2 = usage error (bad flag).

---

## `scripts/validate.sh`

**Responsibility:** full 23-check shell conformance suite. An independent shell implementation of every conformance check — the same checks the Go `lab validate` command runs, expressed as bash commands for use without the compiled binary.

**Architecture:** each check calls the `check(ID severity assertion command...)` function. The function executes the command, records pass/fail, accumulates counts by severity. Summary is printed to `stderr`; the verdict (`CONFORMANT` / `DEGRADED-CONFORMANT` / `NON-CONFORMANT`) is printed to `stdout`. Exit 0 for conformant/degraded; exit 1 for non-conformant.

**Relationship to `catalog.go`:** the shell commands used here must match the `ObservableCommand` fields in `internal/conformance/catalog.go`. These are two independent implementations of the same specification — any divergence is a documentation bug, not a feature.

**Side-effect-free:** never modifies system state. Safe to run concurrently with `lab status` or `lab fault apply`.

**Primary use cases:** pre-binary-build conformance check during bootstrap step 16; manual verification from the `environment-test-plan.md` checklist; quick operator confirmation without compiling Go.

---

## `scripts/run-fault-matrix.sh`

**Responsibility:** iterate all 14 reversible faults through the full apply → validate → reset → validate cycle in sequence and report pass/fail for each.

**Per-fault sequence:** pre-flight `lab validate` → `lab fault apply <ID> --yes` → `lab validate` (expect non-conformant) → `lab status` (log state) → `lab reset` → `lab validate` (expect conformant). Reports `PASS` or `FAIL` per fault; exits 1 if any fault fails.

**Excluded faults:** F-008 and F-014 are excluded — they are non-reversible and require binary rebuild (R3). The script header explains this and directs to `lab reset --tier R3` for manual recovery.

**Recovery note:** previously used `./lab reset` without `--tier`, which would auto-select R2 for the non-reversible faults. Corrected to `lab reset --tier R3` to match the actual recovery requirement.

---

## Service module — `lab_env/service`

---

## `service/main.go`

**Responsibility:** 16-step startup and 4-step graceful shutdown.

**Startup steps (ordered; order matters):**

| Step | Action | Why ordered |
|---|---|---|
| 1 | `GOMAXPROCS(1)` | Aligns with cgroup CPU quota |
| 2 | Parse config | Config required for all subsequent steps |
| 3 | Write `status = "Starting"` | Before any work begins |
| 4 | Write `loading` marker | Bootstrap polls to detect startup |
| 5 | Open log with `O_APPEND` | Log ready before any messages |
| 6 | Emit `"server started"` log entry | Must precede socket bind (L-003 conformance) |
| 7 | Write `app.pid` | P-005 conformance check |
| 8–10 | Init telemetry, chaos, signal handlers | Dependencies before bind |
| 11 | Write `healthy` | Bootstrap step 16 polls for this |
| 12 | Write `status = "Running"` | After healthy written |
| 13 | Remove `loading` | Startup complete |
| 14–15 | Start telemetry + OOM goroutines | Background after bind |
| 16 | `ListenAndServe` | Blocks until shutdown |

**Shutdown steps:** write `ShuttingDown` → remove `healthy` → drain → remove PID.

**Ordering invariant:** `ShuttingDown` written before `healthy` removed — prevents monitoring from seeing `healthy absent + status unknown` which would look like an unexpected crash.

**Build-time fault flags:** `FaultIgnoreSIGTERM` and `FaultZombieChildren` are package-level `string` variables injected via `-ldflags "-X main.FaultIgnoreSIGTERM=true"`. Baked into the binary at build time; no runtime env var check.

---

## `service/go.mod`

```
module lab_env/service
go 1.22
require gopkg.in/yaml.v3 v3.0.1
```

Go 1.22 required for `math/rand/v2`. Single external dependency: `yaml.v3` for strict-parsing YAML config.

---

## `service/CONFORMANCE_CONTRACT.md`

Handler-to-check mapping; fault target table; E-001/E-002 split rationale; chaos latency `/health` exemption rationale. Read this before modifying any endpoint behavior.

---

## `service/config/config.go`

**Responsibility:** load and validate `/etc/app/config.yaml`.

**`Load(path)`** — reads → YAML decode with `KnownFields(true)` (unknown keys = startup failure) → defaults → sanitize.

**`sanitizeEnvString(s)`** — strips control characters and newlines from `app_env`. Prevents log injection.

**`parseBool(s)`** — `"1"`, `"true"`, `"yes"` (case-insensitive) = true. Used for chaos env vars.

**`Config.Chaos`** — `LatencyMS`, `DropPercent`, `OOMTrigger`, `IgnoreSIGTERM`. Read once at startup; no SIGHUP.

---

## `service/logging/logging.go`

**Responsibility:** structured JSON logger writing to `/var/log/app/app.log`.

**Format:** newline-delimited JSON. `{"ts":"<RFC3339Nano UTC>","level":"<info|warn|error>","msg":"<string>"[,key:value...]}`.

**`O_APPEND` invariant:** opened with `O_APPEND` in constructor. After logrotate `copytruncate` truncates the file, the kernel seeks to end before each write — no null-byte padding at the old offset.

**`sync.Mutex`** per entry — single `Write` syscall (data + newline). No interleaved output from concurrent goroutines.

---

## `service/logging/logging_edge_test.go`

**Responsibility:** edge case tests for the logger not covered by `logging_test.go`.

**Tests:**
- `TestLogger_SpecialChars_ProperlyEscaped` — log messages containing JSON special characters (`"`, `\`, newlines, tabs, unicode) are properly escaped; an unescaped `"` would produce invalid JSON and fail L-002
- `TestLogger_Close_Idempotent` — `Close()` can be called multiple times without error or panic
- `TestLogger_WriteAfterClose_NoPanic` — writing after `Close()` does not panic; returns an error but does not crash the service
- `TestLogger_LevelField_CorrectForAllMethods` — `Info`, `Warn`, and `Error` each produce the correct `level` field value in the output

---

## `service/server/server.go`

**Responsibility:** HTTP handlers for three defined endpoints.

**`GET /health`:**
- Returns `{"status":"ok"}`, HTTP 200, unconditionally
- Must not access `/var/lib/app` — structural contract enabling F-004/F-018 diagnosis
- Chaos latency exempt; chaos drop applies

**`GET /` — two distinct response shapes:**
- Success: `{"status":"ok","env":"<app_env>"}` — `env` always present (may be empty string)
- Failure: `{"status":"error","msg":"state write failed"}`, HTTP 500 — `env` absent (not null, absent)

**`GET /slow`:** `time.Sleep(5s)` → `{"status":"ok"}`. B-001: nginx `proxy_read_timeout 3s` < 5s → 504 via proxy; 200 direct.

---

## `service/signals/signals.go`

**Responsibility:** manage `/run/app/` signal files via atomic temp+rename.

**Shutdown ordering invariant:** `status = "ShuttingDown"` written before `healthy` removed. Prevents false crash signal from monitoring.

**`Init()`** — removes stale `loading` and `healthy` from previous crash before startup proceeds. Prevents bootstrap step 16 from completing on a stale `healthy` marker.

---

## `service/signals/signals_edge_test.go`

**Responsibility:** edge case tests for signal file management not covered by `signals_test.go`.

**Tests:**
- `TestBeginShutdown_WhenHealthyAlreadyRemoved` — `BeginShutdown` does not error when `healthy` is already absent (external cleanup scripts may remove it before the service's own shutdown runs); `status=ShuttingDown` still written
- `TestShutdownSequence_RemovesPID` — `RemovePID` removes the PID file
- `TestStatusFile_ExactStringAndNewline` — status file contains the exact status string followed by exactly one newline; no extra whitespace
- `TestPIDFile_DecimalAndNewline` — PID file contains the decimal PID followed by exactly one newline
- `TestRemoveLoading_Idempotent` — `RemoveLoading` called when `loading` is already absent returns nil; no error

---

## `service/telemetry/telemetry.go`

**Responsibility:** write `/run/app/telemetry.json` every 2 seconds with 12 metrics.

**Key metrics:**

| Field | F-018 signal |
|---|---|
| `inode_usage_percent` | Approaches 100% under F-018; `disk_usage_percent` remains low |
| `disk_usage_percent` | Low under F-018 (blocks not exhausted) |

All 12 fields must be present in every snapshot. `chaos_modes` is never null (empty array when none active).

**Panic recovery:** `defer recover()` per tick. Non-atomic `callCount` race fixed to `atomic.Int32`. Goroutine never exits on panic.

---

## `service/telemetry/telemetry_test.go`

**Responsibility:** verify the telemetry snapshot schema exactness, inode percentage calculation, and panic recovery. High ROI: the control plane parses `telemetry.json` to detect chaos state and resource pressure; a wrong JSON tag silently produces zero in the parsed field.

**Tests:**
- `TestSnapshot_Schema_AllFieldsPresent` — a marshaled `Snapshot` contains exactly the 12 required JSON field names with correct tags; catches tag typos (`cpu_pct` vs `cpu_percent`) before integration
- `TestSnapshot_NumericFields_AreNotStrings` — numeric fields marshal as JSON numbers, not strings
- `TestSnapshot_ChaosModes_NeverNull` — `chaos_modes` marshals as `[]` not `null` when empty; a `null` array would require nil-checking in all consumers
- `TestCollector_WritesFile` — the collector goroutine writes `telemetry.json` within 3 seconds of start
- `TestCollector_PanicRecovery` — a panic injected into the first collection tick is recovered; the goroutine continues and produces subsequent writes; uses `atomic.Int32` for `callCount` to prevent the data race fixed in this session

---

## `service/telemetry/telemetry_edge_test.go`

**Responsibility:** edge case tests for the telemetry collector's runtime behavior.

**Tests:**
- `TestCollector_UptimeSeconds_MonotonicallyIncreasing` — consecutive snapshots have strictly increasing `uptime_seconds`; a clock bug or incorrect start time would produce non-monotonic values
- `TestCollector_WrittenWithZeroRequests` — `telemetry.json` is written before any requests arrive; the control plane uses file presence as a liveness signal, so the file must exist immediately after startup regardless of traffic
- `TestCollector_MemoryRSSMB_NonZeroWhenRunning` — `memory_rss_mb` is non-zero for a running process; a `/proc/self/status` parse bug would produce 0.0, masking OOM conditions

---

## `service/chaos/chaos.go`

**Responsibility:** HTTP middleware that injects failures before dispatching to real handlers.

**Per-request order:**
1. Drop check (applies to all routes including `/health`)
2. Latency injection (exempt on `/health`)
3. Dispatch to `next.ServeHTTP`

**Drop on `/health`** — documented decision: drops return a distinct `{"status":"error","msg":"chaos drop"}` body distinguishable from genuine service failure. E-001 correctly fails under 100% drop (expected behavior for an active fault).

**Latency exempt on `/health`** — a delayed `/health` would make E-001 indistinguishable from a crashed service, destroying diagnostic value.

**`StartOOM()`** — `sync.Once`; allocates 64MiB chunks until cgroup `MemoryMax=256M` kills the process. Requires cgroup v2, no swap. Multiple calls safe.

---

## `service/chaos/chaos_test.go`

**Responsibility:** verify the chaos middleware's core behavioral contracts.

**Tests:**
- `TestChaosHandler_Latency_ExemptedForHealth` — `/health` is not delayed regardless of `latencyMS` (preserves E-001 diagnostic integrity); `/` is delayed by at least `latencyMS`
- `TestChaosHandler_Drop_IncrementsErrorCount` — drops increment both `requests_total` and `errors_total` via counter callbacks
- `TestChaosHandler_Drop_BeforeLatency` — drop check runs before latency injection; 100% drop + 100ms latency returns immediately without sleeping
- `TestChaosHandler_ZeroDrop_AlwaysPassesThrough` — `dropPercent=0` never drops any request
- `TestStartOOM_SyncOnce` — 10 concurrent `StartOOM` calls produce exactly one goroutine
- `TestChaosHandler_NilCallbacks_NoPanic` — nil counter callbacks handled safely
- `TestChaosHandler_ConcurrentRequests_CounterAccuracy` — 1000 concurrent requests produce accurate counter values

---

## `service/chaos/chaos_edge_test.go`

**Responsibility:** edge case tests documenting the resolved design decision on drop behavior for `/health`.

**Tests:**
- `TestChaosHandler_Drop100_HealthIsDropped` — at 100% drop rate, `/health` returns 503 with `{"status":"error","msg":"chaos drop"}` (documented decision: drops apply to all routes including `/health`; body is distinct from genuine service failure so E-001 failure is correctly attributable to the chaos fault)
- `TestChaosHandler_Drop100_AllNonHealthRoutesDrop` — `/`, `/slow`, `/api/anything` all return 503 at 100% drop
- `TestChaosHandler_Latency_ActuallyDelaysRoot` — positive case: `GET /` is delayed by at least `latencyMS`
- `TestChaosHandler_ZeroLatency_NoDelay` — `latencyMS=0` causes no measurable delay (< 50ms)

---

<!-- BEGIN GENERATED: Specification → Implementation Index -->
## Specification → Implementation Index

> **Source of truth:** `internal/invariants/spec_index.go` — the Go data structure that backs this table.
> Every file reference is verified by `TestSpecIndex_AllReferencedFilesExist` on every test run.
> The markdown in this section is kept in sync by `TestSpecIndex_MarkdownIsUpToDate`.
>
> **Integrity guarantee:** the CI pipeline runs `TestSpecIndex*` on every push. A passing build means all mappings are verified.
>
> **To update:** edit `internal/invariants/spec_index.go`, then run:
> ```
> go generate ./internal/invariants/
> ```
> **Notation:** `→ (test-only)` = enforced by tests only · `→ (cross-reference)` = points to other documents · `constraints:` = layered enforcement

---

### `conformance-model.md`

| Section | Title | Primary implementation | Enforcing tests | Notes |
|---|---|---|---|---|
| §3 | Check Catalog | `internal/conformance/check.go` · `internal/conformance/catalog.go` | `runner_test.go` · `catalog_test.go` | §3.1 defines the Check schema; §3.2–§3.7 are the catalog entries themselves |
| §4 | Validation Semantics | `internal/conformance/result.go` · `internal/conformance/runner.go` | `runner_test.go` · `runner_edge_cases_test.go` | §4.1 verdict rules → result.go; §4.4 ordering + dependent marking → runner.go; §4.7 output schema → output/model.go |
| §4.7 | Validation Output Schema | `internal/output/model.go` · `internal/output/render.go` | `golden_test.go` · `render_test.go` | §4.7 is the output subset; the full in-memory representation is SuiteResult in result.go |
| §5 | Model Completeness Condition | → (test-only) | `invariants_test.go` | Bidirectional completeness (FailingChecks ↔ fault catalog) enforced by TestInvariant_FaultFailingChecks_ExistInCatalog |

---

### `system-state-model.md`

| Section | Title | Primary implementation | Enforcing tests | Notes |
|---|---|---|---|---|
| §2 | State Definitions | `internal/state/state.go` | `state_test.go` | Six constants, invariant methods (RequiresActiveFault, ForbidsActiveFault, CanApplyFault, CanReset) all in state.go |
| §3 | Transition Model | `cmd/fault.go` · `cmd/reset.go` · `internal/state/store.go` · `internal/state/state.go` | `fault_test.go` · `interrupt_test.go` · `state_test.go` | Logical atomicity (§3.1) → store.go; guard checks (§3.4) → state.go; transitions themselves → cmd/fault.go + cmd/reset.go |
| §4 | State Detection | `internal/state/detect.go` · `cmd/status.go` | `detect_test.go` · `signal_combinations_test.go` · `status_test.go` | §4.1 authority precedence + §4.2 algorithm + §4.3 four conflict cases all in detect.go; §4.4 UNKNOWN → exit 5 in status.go |
| §5 | Constraint Graph | `internal/state/state.go` · `internal/state/store.go` | `state_test.go` · `store_test.go` · `invariants_test.go` | I-2 (active_fault constraint) → state.go; I-1 (classification_valid) + I-3 (history cap) → store.go |

---

### `fault-model.md`

| Section | Title | Primary implementation | Enforcing tests | Notes |
|---|---|---|---|---|
| §3 | Fault Schema | `internal/catalog/fault.go` | `catalog_test.go` · `content_integrity_test.go` | §3.1 FaultDef fields → fault.go; §3.2 PostconditionSpec → fault.go; FaultImpl (Apply/Recover) also in fault.go |
| §4 | Mutation Rules | `cmd/fault.go` · `internal/catalog/faults.go` · `internal/executor/executor.go` | `fault_test.go` · `boundary_test.go` · `trace_test.go` | §4.1 executor requirement enforced structurally by type system; §4.2 atomicity contract → cmd/fault.go (ApplyFailure test) |
| §5 | Pre/Post Conditions | `cmd/fault.go` · `internal/conformance/catalog.go` | `fault_test.go` · `invariants_test.go` | §5.1 standard precondition (steps 2-4) → fault.go; §5.2 PreconditionChecks (step 5) → fault.go + catalog.go; §5.3 postcondition → catalog/fault.go |
| §6 | Reversibility Semantics | `internal/catalog/faults.go` · `cmd/reset.go` | `catalog_test.go` · `content_integrity_test.go` |  |
| §7 | Fault Catalog | `internal/catalog/faults.go` | `catalog_test.go` · `content_integrity_test.go` · `invariants_test.go` | §7.2 is the canonical catalog; each fault constructor is the mechanical projection of the corresponding §7.2 entry |
| §10 | Baseline Network Behaviours | → (test-only) | `invariants_test.go` | B-001/B-002 are absent from the Go catalog by design; TestInvariant_NoBaselineFaultsInCatalog enforces absence |

---

### `control-plane-contract.md`

| Section | Title | Primary implementation | Enforcing tests | Notes |
|---|---|---|---|---|
| §3 | Global Contract | `app.go` · `internal/output/render.go` · `internal/executor/lock.go` | `lock_test.go` · `lock_stale_system_process_test.go` · `render_test.go` | §3.2 exit code table is distributed across all cmd/ files; §3.6 signal handling is the interrupt path (see §4.5) |
| §4.1 | lab status | `cmd/status.go` | `status_test.go` | Reconciliation authority; only command that updates state classification; LightweightRun + Detect |
| §4.2 | lab validate | `cmd/validate.go` | `validate_test.go` | Observation-only; updates last_validate; must not update state field |
| §4.3 | lab fault list | `cmd/fault.go` | `fault_test.go` | FaultListCmd; AllDefs() only; no executor dependency |
| §4.4 | lab fault info | `cmd/fault.go` | `fault_test.go` | FaultInfoCmd; DefByID() only; no executor dependency |
| §4.5 | lab fault apply | `cmd/fault.go` | `fault_test.go` · `interrupt_test.go` · `live_fault_matrix_test.go` | 6-step precondition sequence; PreconditionChecks (step 5); atomicity; audit; interrupt path (§3.6) handled here |
| §4.6 | lab reset | `cmd/reset.go` | `live_fault_matrix_test.go` | R1/R2/R3 tiers; auto-select from fault ResetTier; post-reset validation always runs |
| §4.7 | lab provision | `cmd/reset_provision_history.go` | — | Delegates to bootstrap.sh via RunMutation; idempotent |
| §4.8 | lab history | `cmd/reset_provision_history.go` | — | Ring buffer read; reverse chronological; read-only; no lock |
| §5 | Executor Behavioral Contract | `internal/executor/executor.go` · `internal/executor/real.go` | `audit_test.go` · `boundary_test.go` · `trace_test.go` · `embed_test.go` · `restore_test.go` | §5.1 capabilities → executor.go interface; §5.2 audit obligation → audit.go; §5.3 ordering → trace_test.go; §5.5 privilege → real.go (runSudo) |
| §6 | State File Contract | `internal/state/store.go` | `store_test.go` · `store_edge_cases_test.go` | §6.1 schema → store.go File struct; §6.2 atomic write → Store.Write; §6.3 lock relationship → lock.go; §6.5 corruption recovery → Store.Read ErrStateFileCorrupt |
| §7 | Audit Log Contract | `internal/executor/audit.go` | `audit_test.go` · `mutation_failure_test.go` | §7.2 entry schema + §7.3 entry types + §7.4 ordering guarantee all in audit.go |
| §8 | Error Catalog | `cmd/fault.go` · `internal/executor/lock.go` | `fault_test.go` | Named error strings (ErrUnknownFaultID, ErrFaultAlreadyActive, ErrLockHeld, etc.) live in the files that return them |
| §9 | Conformance with Semantic Models | → (cross-reference) | — | Cross-reference section only; points to conformance-model, system-state-model, and fault-model. No independent implementation. |

---

### `canonical-environment.md`

| Section | Title | Primary implementation | Enforcing tests | Notes |
|---|---|---|---|---|
| §2 | Canonical Environment Contract | constraints: constants (internal/config/config.go) + provisioning (scripts/bootstrap.sh) + verification (conformance checks) | `embed_test.go` | §2.2 users + §2.3 filesystem layout → config.go constants; §2.4 baseline service state → checked by conformance suite |
| §3 | Go Service Interface Contract | `service/main.go` · `service/server/server.go` · `service/logging/logging.go` · `service/signals/signals.go` | `server_test.go` · `server_edge_test.go` · `signals_test.go` · `logging_test.go` | §3.1 startup contract → main.go; §3.2 process model → main.go (GOMAXPROCS, build flags); §3.3 endpoints → server.go; §3.4 logging → logging.go; §3.5 signals → main.go; §3.6 log file → logging.go |
| §4 | Canonical Artifact Contents | constraints: embedded content (internal/config/*) + parser enforcement (service/config/config.go) + R2 restore (internal/executor/canonical_files.go) | `embed_test.go` · `config_test.go` · `config_edge_test.go` |  |
| §5 | Provisioning Contract | constraints: script (scripts/bootstrap.sh) + idempotency strategy (docs/provisioning-blueprint.md) + final gate (lab validate) | `live_fault_matrix_test.go` | §5.4 idempotency contract → each step's guard condition in bootstrap.sh |
| §8 | State Control | `cmd/reset.go` · `scripts/reset.sh` | `live_fault_matrix_test.go` | §8.1 reset tiers → cmd/reset.go executeTier; §8.2 reset.sh contract → scripts/reset.sh |

<!-- END GENERATED: Specification → Implementation Index -->