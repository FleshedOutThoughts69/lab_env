# Execution Plan — Integration and Adversarial Validation Phase
## Based on current system state as of architecture completion

---

## Current Reality

**Mental model shift required:**

This is not a testing phase after implementation. It is **progressive system activation under controlled adversarial conditions**. The analogy is closer to OS lab validation or distributed systems failure testing than typical application QA.

**What is complete:**

- Architecture: locked
- Core subsystems: implemented (90%+)
- Unit and contract tests: done
- Cross-layer invariants: done (status/validate separation, conformance ordering, executor boundary, audit completeness, lock contract, catalog completeness)
- Config authority: consolidated in `internal/config/config.go`

**What is actually remaining:**

The bottleneck is no longer construction. It is:

> "Does this system behave correctly when the OS is real, noisy, and partially failing?"

That is a fundamentally different mode than coding.

---

## Completed Items (removed from plan)

The following items appeared in the original plan but are already done. They are listed here so they are not re-executed:

| Item | Status | Evidence |
|---|---|---|
| Config drift sweep | Complete | Last pass cleaned `faults.go` and `reset_provision_history.go`; grep-verified clean |
| Status vs validate semantic separation | Complete | `TestValidateCmd_WritesLastValidate_NotState`, `TestValidateCmd_DoesNotReconcileState` |
| Conformance runner dependency integrity | Complete | `TestRunner_DependentMarking`, `TestRunner_AllChecksRunEvenOnFailure`, `TestCatalog_OrderSPEFL` |
| Cross-document invariant tests | Complete | `internal/invariants/invariants_test.go` |
| Fault catalog completeness | Complete | `TestAllImpls_Has18Faults`, `TestAllDefs_RequiredFieldsPresent` |
| Executor boundary (mutation monopoly) | Complete | `RunMutation` sealed; `TestObserver_DoesNotHaveMutationMethods` |

**Verification before proceeding:** run the full grep audit to confirm config is clean:

```bash
grep -rn '"/etc/app\|"/var/lib/app\|"/opt/app\|"appuser\|"0\.0\.0\.0:80\|127\.0\.0\.1:8080' \
  internal/ cmd/ \
  | grep -v 'config\.go\|_test\.go\|\.md\|Symptom\|Observable\|MutationDisplay'
```

Expected: empty. If not empty, fix before proceeding.

---

## Known Open Issues (carry forward)

These are named, bounded issues that do not block phase execution but must be resolved before the system is declared complete:

**H-001 — Status output inference**
Location: `cmd/status.go`, `buildStatusResult`, line containing `code := 502 // best guess`
Impact: status output endpoint codes are guessed rather than projected from check results
Fix scope: 10–15 lines in one function
Regression guard already exists: `TestRenderStatus_JSON_EndpointCodesNotGuessed`
Fix this before golden fixture expansion. Not before.

**State file concurrent write race**
Location: `lab status` writes `state.json` without the mutation lock; mutating commands also write `state.json` under the lock
Impact: `status` and `fault apply` running concurrently can produce a torn write on the state file. The atomic write (temp + rename) mitigates but does not eliminate this.
This will surface in Phase C adversarial testing. Flag it now.

---

## Global Dependency Graph

The only correct execution order. Items in the same row are parallelizable.

```
┌──────────────────────────────────────────────────┐
│ PHASE 0: Test suite bifurcation (mock vs live)   │
└──────────────────────────┬───────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────┐
│ PHASE A: Pre-flight fixes                        │
│   H-001 fix (status truth)                       │
│   go test ./... green baseline on real system    │
└──────────────────────────┬───────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────┐
│ PHASE B: Live system contract validation         │
│   B1: Live interrupt path (real OS signal)       │
│   B2: Live fault execution matrix (all 18)       │
│   B3: Status/validate/live divergence cycle      │
└──────────────────────────┬───────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────┐
│ PHASE C: Invariant stress testing                │
│   State file concurrency                         │
│   Fault atomicity under real syscall failures    │
│   Rapid transition sequences                     │
│   Corruption injection                           │
└──────────────────────────┬───────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────┐
│ PHASE D: Output and schema freezing              │
│   Golden fixture expansion (post-H-001 fix)      │
│   Schema drift lock                              │
│   Determinism verification                       │
└──────────────────────────────────────────────────┘
```

**Critical path:** Phase 0 → Phase A → Phase B (B1 first) → Phase C → Phase D.
B2 and B3 are parallelizable with B1 after B1 is green.

---

## Phase 0 — Test Suite Bifurcation

**Why first:** without this, mock-green tests can mask live-system failures. Running the full suite against the real VM without bifurcation produces false confidence.

**Exit criterion:** same tests run safely in both modes; integration tests are skipped in mock mode without failing.

### Work

Add build tags to all tests that require a live system:

```go
//go:build integration
```

Add a `LAB_TEST_MODE` environment variable check to `TestMain` in each integration test package:

```go
func TestMain(m *testing.M) {
    if os.Getenv("LAB_TEST_MODE") != "live" {
        // skip all tests in this package for non-live runs
        os.Exit(0)
    }
    os.Exit(m.Run())
}
```

**Tag assignment:**

| Test file | Mode |
|---|---|
| `internal/conformance/runner_test.go` | `unit` — mock observer, no real system |
| `internal/state/detect_test.go` | `unit` — pure functions |
| `internal/state/store_test.go` | `unit` — uses `t.TempDir()` |
| `internal/executor/audit_test.go` | `unit` — uses `t.TempDir()` |
| `internal/executor/lock_test.go` | `unit` — uses `t.TempDir()` |
| `internal/catalog/catalog_test.go` | `unit` |
| `internal/invariants/invariants_test.go` | `unit` |
| `internal/output/render_test.go` | `unit` |
| `internal/output/golden_test.go` | `unit` |
| `cmd/status_test.go` | `unit` — uses stub observer and `t.TempDir()` |
| `cmd/validate_test.go` | `unit` — same |
| `cmd/fault_test.go` | `unit` — same |
| `cmd/interrupt_test.go` | `unit` — uses `testutil.InterruptableExecutor` |
| `cmd/live_interrupt_test.go` | **`integration`** — real OS signal, real state file |
| `cmd/live_fault_matrix_test.go` | **`integration`** — real VM, real commands |

**Run modes:**

```bash
# Unit tests only (CI, fast, no VM required)
go test ./... -tags unit

# Integration tests only (VM required)
LAB_TEST_MODE=live go test ./... -tags integration -v

# Both
LAB_TEST_MODE=live go test ./... -tags 'unit integration' -v
```

**Time: 0.5 days**

---

## Phase A — Pre-flight Fixes

**Exit criterion:** `go test ./... -tags unit` is fully green; H-001 is resolved; config audit passes.

### A1 — H-001 Fix (status truth)

**Location:** `cmd/status.go`, `buildStatusResult`

**Current problem:**

```go
code := 502 // best guess without exact status
```

**Fix:** thread the actual endpoint status codes through from the lightweight check results rather than guessing. The `CheckEndpoint` call in `real.go` returns the actual HTTP status code. The `LightweightRun` results contain `CheckResult` structs with the actual error. The fix is to extract the real code from the `EndpointStatus` stored during the check.

Specifically: add `EndpointCode int` to `CheckResult` or pass endpoint status through the lightweight run result so `buildStatusResult` reads the real code rather than inferring it.

**Regression guard:** `TestRenderStatus_JSON_EndpointCodesNotGuessed` will fail if any guessed value remains.

**Done when:** `TestRenderStatus_JSON_EndpointCodesNotGuessed` passes with real code values, not hardcoded fallbacks.

### A2 — Config audit (verification only)

Run the grep command from the pre-flight section. If empty, Phase A2 is done in 5 minutes. If not empty, fix any remaining leaks.

**Time: 0.5 days total for Phase A**

---

## Phase B — Live System Contract Validation

**Why this is the highest-ROI remaining phase:**

Unit and contract tests prove the components. This phase proves the system. These are not the same thing. A local system under real OS scheduling, real sudo latency, real filesystem timing, and real signal delivery will surface failures that no amount of mock testing can predict.

**Exit criterion for Phase B:** all 18 faults execute correctly on the real VM per the fault matrix runbook; interrupt path produces correct artifacts; status/validate cycle is consistent.

---

### B1 — Live Interrupt Path (highest priority in Phase B)

**Why B1 first:** the interrupt path is the only cross-layer semantic path not yet proven on a real system. It spans: OS signal delivery → app.go signal handler → executor grace period → state.json invalidation → audit log entry → exit code 4 → lab status reclassification. Every component is individually tested. The composition is not.

**What to build:**

`cmd/live_interrupt_test.go` — integration-tagged tests that:

1. Start `lab reset --tier R2` as a subprocess
2. Send `SIGINT` after a short delay using `os.Signal`
3. Verify exit code is 4
4. Read `/var/lib/lab/state.json` and verify `classification_valid: false`
5. Read `/var/lib/lab/audit.log` and verify interrupt entry present
6. Run `lab status` as a subprocess
7. Verify exit code is 0 and state is reclassified

**Test scenarios:**

| Scenario | Signal | Timing | Expected exit |
|---|---|---|---|
| Signal before any mutation | SIGINT | immediate | 0 or 1 (no side effects) |
| Signal after first mutation | SIGINT | 100ms delay | 4 |
| Signal after all mutations, before state write | SIGINT | late in sequence | 4 |
| SIGTERM (graceful) | SIGTERM | 100ms delay | 4 |
| Grace period exceeded | SIGINT | during slow R3 | 4 + grace note in audit |

**Done when:** all five scenarios produce correct artifacts (exit code, state.json, audit.log).

---

### B2 — Live Fault Execution Matrix

**What this is:** execute all 18 faults against the real VM using the fault matrix runbook as the verification checklist. This is not automated — it is an operator-driven execution loop. Each fault:

1. Verify CONFORMANT pre-condition
2. `lab fault apply <ID>`
3. Run verification commands from runbook
4. Verify `lab validate` output matches expected check failures
5. Verify `lab status` shows DEGRADED
6. `lab reset`
7. Verify `lab validate` exits 0

**Automation support:** a test harness script that wraps each fault in a pre/post validation loop and captures all output:

```bash
#!/usr/bin/env bash
# live_fault_matrix.sh
# Usage: ./live_fault_matrix.sh [F-001|F-002|...|all]

run_fault() {
    local id=$1
    echo "=== $id: pre-flight ==="
    lab validate || { echo "FAIL: pre-flight"; exit 1; }

    echo "=== $id: apply ==="
    lab fault apply "$id" --yes 2>&1 | tee /tmp/apply_$id.log

    echo "=== $id: verify ==="
    lab validate 2>&1 | tee /tmp/validate_$id.log
    lab status --json 2>&1 | tee /tmp/status_$id.json

    echo "=== $id: reset ==="
    lab reset 2>&1 | tee /tmp/reset_$id.log

    echo "=== $id: post-reset ==="
    lab validate || { echo "FAIL: post-reset"; exit 1; }

    echo "$id: PASS"
}
```

**Special cases:**

- F-008, F-014: non-reversible, require `--yes`, require R3 reset — run last
- F-011, F-012: baseline — verify from CONFORMANT state without applying
- F-010: verify `lab validate` exits 0 (degraded only)
- F-008, F-014: verify `lab validate` exits 0 while fault is active

**Done when:** all 18 rows in the fault matrix runbook are checked.

---

### B3 — Status/Validate/Live Divergence Cycle

**What this is:** on the real system, run the following cycle and verify consistency at each step:

```bash
lab status --json > before.json
lab validate --json > validate.json
lab status --json > after.json
diff <(jq .state before.json) <(jq .state after.json)  # must be identical
jq .state before.json   # must not change due to validate
```

**Test sequence:**

1. CONFORMANT → validate (should stay CONFORMANT in state.json)
2. Manual break → validate (state.json should stay BROKEN — validate does not reconcile)
3. Manual break → status (state.json should update to BROKEN — status does reconcile)
4. Manual break → validate → status (status must detect BROKEN even after validate ran)

**Done when:** no scenario causes validate to write to the `state` field.

**Time: 1–2 days for Phase B total (B1 heaviest)**

---

## Phase C — Invariant Stress Testing

**Goal:** break assumptions under real OS scheduling and adversarial sequences.

**Key distinction from Phase 4 in the original plan:** this is not load testing. It is *invariant testing under adversarial sequences*. Every scenario tests a specific architectural guarantee.

---

### C1 — State File Concurrency

**The risk:** `lab status` writes `state.json` without the mutation lock. A concurrent `lab fault apply` also writes `state.json` under the lock. The atomic write (temp + rename) provides filesystem-level safety, but the read-modify-write cycle in `status` is not protected.

**Test:**

```bash
# Run status in a tight loop while fault apply executes
lab fault apply F-004 &
for i in $(seq 1 100); do lab status > /dev/null 2>&1; done
wait
lab status --json | jq '{state, active_fault, classification_valid}'
```

**Pass criterion:** final `lab status` output is consistent (state matches active_fault — invariant I-2 holds). No `state.json` parse errors.

**If this fails:** the fix is to have `lab status` acquire a read advisory lock (separate from the mutation lock, or use the mutation lock for status writes as well). This is a known design tension.

---

### C2 — Fault Atomicity Under Real Syscall Failures

**The risk:** unit tests mock the executor. On the real system, `chmod` can succeed while `systemctl restart` fails. The fault Apply sequence is not truly atomic — it is logically atomic under the assumption that all syscalls succeed or fail together.

**Test:** for each multi-step fault (F-002, F-006, F-007, F-013, F-015, F-016, F-017), verify that a mid-sequence failure (simulated by temporarily removing sudo permissions) leaves the system in a recoverable state:

```bash
# Before F-006 apply: revoke sudo for daemon-reload
# Apply should fail mid-sequence
# state.json must not show DEGRADED
# lab status must show BROKEN or CONFORMANT, not DEGRADED with no fault
lab status --json | jq '{state, active_fault}'
# active_fault must be null if Apply failed
```

**Pass criterion:** no state where `state=DEGRADED` and `active_fault=null` (invariant I-2 violation).

---

### C3 — Rapid Transition Sequences

**Scenarios:**

```bash
# Rapid fault apply → reset cycles
for i in $(seq 1 10); do
    lab fault apply F-004
    lab reset
    lab validate || { echo "FAIL at iteration $i"; break; }
done

# Validate during reset
lab fault apply F-001
lab reset &
RESET_PID=$!
lab validate
wait $RESET_PID
lab status --json | jq .state

# Status during fault apply
lab fault apply F-004 &
APPLY_PID=$!
lab status
wait $APPLY_PID
```

**Pass criterion:** no iteration produces an inconsistent state. `lab validate` during reset may return non-zero (expected — environment is mid-reset) but must not crash.

---

### C4 — Corruption Injection (recovery playbook execution)

Execute all 9 drills from `docs/recovery-playbook.md` in sequence. Use the pass criteria in that document verbatim.

**Pass criterion:** all 9 drills pass the 7-point verification checklist.

**Time: 1 day for Phase C**

---

## Phase D — Output and Schema Freezing

**Entry condition:** Phase A (H-001 fix) is complete. Do not run Phase D before H-001 is fixed, because H-001 produces incorrect output that would be baked into fixtures.

**Exit criterion:** `UPDATE_GOLDEN=1 go test ./...` produces stable fixtures; subsequent runs without `UPDATE_GOLDEN=1` pass; no unexpected fields in any JSON output.

---

### D1 — H-001 Regression and Golden Fixture Expansion

After H-001 is fixed, regenerate and expand golden fixtures:

```bash
UPDATE_GOLDEN=1 go test ./internal/output/... -run TestGolden
```

Then expand to cover:

- Status: CONFORMANT, DEGRADED (with fault), BROKEN, UNKNOWN
- Validate: all-pass, blocking-failure, degraded-only-failure
- Fault apply: success, precondition-rejected, apply-failed, baseline-rejected
- Reset: success (R1, R2), post-reset validation failure
- History: populated ring buffer

---

### D2 — Schema Drift Lock

Verify no unexpected fields exist in any JSON output using the no-extra-fields tests already in `internal/output/golden_test.go`. If any test fails, it caught a schema addition — either add the field to the stable schema definition in `golden-baseline-ledger.md` or remove it from the output.

---

### D3 — Output Determinism

**Map iteration:** Go map iteration order is non-deterministic. `StatusResult.Services` and `StatusResult.Endpoints` are `map[string]...` types. Verify that the JSON renderer sorts map keys or that the golden tests normalize before comparing (they already do via round-trip normalization).

**Timestamps:** verify all timestamp fields are excluded from golden comparisons. The `normalizeJSON` function in `golden_test.go` compares after round-trip — but timestamps that are part of the fixture will still be compared. For fixtures that include timestamps, replace with a fixed sentinel value.

**Time: 0.5–1 day for Phase D**

---

## Revised Time Estimates

| Phase | What it is | Estimated time |
|---|---|---|
| Phase 0 | Test bifurcation | 0.5 days |
| Phase A | H-001 fix + config verification | 0.5 days |
| Phase B | Live system validation (interrupt + fault matrix + divergence) | 1–2 days |
| Phase C | Invariant stress testing | 1 day |
| Phase D | Output freezing (post-H-001) | 0.5–1 day |
| **Total** | | **3.5–5 days** |

This is not coding-heavy. It is execution + observation + correction loops.

---

## "Done" Definition

The system is complete when:

1. `go test ./... -tags unit` is fully green
2. `LAB_TEST_MODE=live go test ./... -tags integration` is fully green on the target VM
3. All 18 faults in the fault matrix runbook produce observed behavior matching expected behavior
4. All 9 drills in the recovery playbook pass the 7-point verification checklist
5. `go test ./internal/output/...` is fully green with stable golden fixtures
6. No layer interprets another layer's output
7. No command mutates outside its authority
8. No output is derived from guesses
9. All state transitions are reproducible from inputs
10. Interrupts behave deterministically on the real OS

When all 10 are true: the system is a closed semantic system with deterministic state evolution under adversarial conditions. Any valid input sequence produces a predictable, testable, reproducible system state.

---

## Highest-Risk Open Items (ranked)

| Rank | Risk | Phase it surfaces | Mitigation |
|---|---|---|---|
| 1 | State file concurrent write race (status + mutation) | C1 | Known design tension; fix may require status to acquire lock for writes |
| 2 | Partial mutation under real syscall failures | C2 | Logical atomicity is not physical atomicity; test each multi-step fault |
| 3 | Interrupt path on real OS (end-to-end) | B1 | Highest-value remaining proof; must be live, not mocked |
| 4 | Map iteration non-determinism in JSON output | D3 | Already mitigated by round-trip normalization; verify explicitly |
| 5 | Config drift reintroduction in future fault additions | extension-boundary-note.md | Guardrail document names required tests; grep audit is the detection mechanism |