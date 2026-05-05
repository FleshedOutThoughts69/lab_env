# Extension Boundary Note
## Version 1.0.0

> Change gates only. No architecture commentary.
> For each extension: required changes, required tests, forbidden shortcuts.

---

## Adding a New Fault

**Required changes (all mandatory — missing any is a completeness violation):**

1. `internal/catalog/faults.go` — new `faultFNNN() *FaultImpl`; `Def *FaultDef` all fields; Apply/Recover via Executor only
2. `fault-model.md §7.2` — full catalog entry with FailingChecks and PassingChecks
3. `conformance-model.md §3` — add fault ID to `Maps to` field of every check in FailingChecks
4. `canonical-environment.md §7` — consistent entry (same version, same postcondition)
5. `docs/fault-matrix-runbook.md` — new row in matrix
6. `docs/golden-baseline-ledger.md §IV` — new row in fault table

**Tests that will fail if steps are skipped:**

| Skipped step | Failing test |
|---|---|
| Step 1 count | `TestAllImpls_Has18Faults` |
| Step 1 Def fields | `TestAllDefs_RequiredFieldsPresent` |
| Step 1 Preconditions | `TestAllFaults_HavePreconditions` |
| Step 2 FailingChecks | `TestInvariant_FaultFailingChecks_ExistInCatalog` |
| Step 3 Maps-to | (manual audit — no automated test yet) |

**Forbidden:**
- Apply/Recover calls `RunCommand` for mutations (use `RunMutation`)
- Non-reversible fault without `RequiresConfirmation: true` and `ResetTier: "R3"`
- Baseline behavior fault without `IsBaselineBehavior: true` and empty `ResetTier`
- Fault with no postcondition (except F-008, F-014 exception documented in fault-model §8)

---

## Adding a New Conformance Check

**Required changes:**

1. `internal/conformance/catalog.go` — new `Check` in correct series position; Execute uses Observer only
2. `conformance-model.md §3` — full entry with `Maps to` field
3. `system-state-model.md §6` — update state-to-check mapping table
4. `fault-model.md` — update FailingChecks of any fault whose Apply affects this check
5. `docs/golden-baseline-ledger.md §III` — add check ID to frozen list; update count if changed

**Tests that will fail:**

| Change | Failing test |
|---|---|
| Count not updated | `TestInvariant_23ChecksInConformanceCatalog`, `TestCatalog_Has23Checks` |
| Severity wrong | `TestCatalog_SeverityDistribution` |
| ID not unique | `TestCatalog_UniqueIDs` |
| No Execute func | `TestCatalog_AllHaveExecute` |
| Wrong order | `TestCatalog_OrderSPEFL` |

**Forbidden:**
- Blocking check without a failure meaning
- Check without a `Maps to` entry (orphan check)
- Changing existing check severity without updating golden fixtures

---

## Adding a New Command

**Required changes:**

1. `cmd/<command>.go` — returns `output.CommandResult`; no domain logic
2. `control-plane-contract.md §4` — full contract: preconditions, state effects, exit codes, audit entries
3. `app.go` — dispatch case in `Run()`
4. `cmd/<command>_test.go` — contract tests
5. `docs/operational-trace-spec.md` — event trace

**If mutating (acquires lock):**

- Acquire lock before any guard reads
- Write audit entry for every executor operation via `[audit]` before `[mut]`
- Use `RunMutation` for privileged shell commands, not `RunCommand`
- Write state.json only after all mutations succeed
- Release lock in all exit paths

**If read-only:**

- Must NOT acquire lock
- Must NOT write `state.json` (exception: `lab status` is explicitly authorized to reconcile)
- Must NOT write audit entries (exception: `lab validate` writes `validation_run`)

---

## Adding a New Executor Mutation Method

**Required changes:**

1. `internal/executor/executor.go` — add method to `Executor` interface
2. `internal/executor/real.go` — implement; call `r.audit.LogOp(...)` before return
3. `internal/executor/audit_test.go` — `TestMutationAuditCompleteness_*` must cover new method
4. `internal/executor/boundary_test.go` — update `TestFaultApply_ReceivesExecutor_NotObserver` comment
5. `docs/extension-boundary-note.md` — note the new capability

**Forbidden:**
- Adding a mutation method to `conformance.Observer`
- Calling the new method without an audit entry
- Adding a method that bypasses the lock for state-mutating operations

---

## Changing a Frozen Surface

| Surface | Required steps |
|---|---|
| JSON field name/type | Update `control-plane-contract.md §6`; increment SpecVersion; `UPDATE_GOLDEN=1 go test ./internal/output/...`; update ledger §I |
| Exit code semantics | Do not repurpose. Add new code to `control-plane-contract §3.2` and ledger §II only. |
| State name | Breaking change. Version increment + migration path required. |
| Check ID | Breaking change. Do not renumber existing IDs. |
| Fault ID | Breaking change. Do not renumber existing IDs. New faults use next sequential ID. |
| Audit entry_type | Add only. Do not remove or rename existing values. |

---

## Invariants That Must Hold After Any Change

| Invariant | Spec authority | Enforcing test |
|---|---|---|
| `lab validate` does not update `state` field | control-plane-contract §4.2 | `TestValidateCmd_WritesLastValidate_NotState` |
| `lab status` is the only reconciliation point | control-plane-contract §4.1 | `TestStatusCmd_ReconcilesBrokenToConformant_*` |
| Degraded checks do not affect exit code | conformance-model §4.3 | `TestSuiteResult_Classify_DegradedOnly` |
| At most one active fault | fault-model §2.2 | `TestFaultApplyCmd_PreconditionFails_FaultAlreadyActive` |
| Apply failure does not update state | control-plane-contract §4.5 | `TestFaultApplyCmd_ApplyFailure_DoesNotUpdateState` |
| Interrupt does not assert BROKEN | control-plane-contract §3.6 | `TestInterruptPath_DoesNotAssertBroken` |
| Every mutation produces an audit entry | control-plane-contract §5.2 | `TestMutationAuditCompleteness_*` |
| Audit log never truncated | control-plane-contract §7.1 | `TestAuditLogger_AppendOnly` |
| UNKNOWN is classification failure, not state | system-state-model §4.4 | `TestIsUnknown` |
| No mutation through Observer interface | boundary audit | `TestObserver_DoesNotHaveMutationMethods` |