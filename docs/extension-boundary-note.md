# Extension Boundary Note
## Version 1.0.0

> Change gates only. No architecture commentary.
> For each extension: required changes, required tests, forbidden shortcuts.

---

## Adding a New Fault

**Required changes (all mandatory — missing any is a completeness violation):**

1. `internal/catalog/faults.go` — new `faultFNNN() *FaultImpl`; `Def *FaultDef` all fields; Apply/Recover via Executor only
2. `internal/catalog/catalog_test.go` — increment expected count in `TestAllImpls_Has16Faults` and `TestAllDefs_Has16Defs`; add new fault ID to the expected list in `TestFaultIDs_SequentialWithGap`
3. `internal/invariants/invariants_test.go` — increment expected count in `TestInvariant_16FaultsInCatalog`
4. `fault-model.md §7.2` — full catalog entry with FailingChecks and PassingChecks
5. `conformance-model.md §3` — add fault ID to `Maps to` field of every check in FailingChecks
6. `canonical-environment.md §7` — consistent entry (same version, same postcondition)
7. `docs/fault-matrix-runbook.md` — new row in matrix
8. `docs/golden-baseline-ledger.md §IV` — new row in fault table

**Tests that will fail if steps are skipped:**

| Skipped step | Failing test |
|---|---|
| Step 1 (no faultFNNN) | `TestAllImpls_Has16Faults`, `TestAllDefs_Has16Defs` |
| Step 2 count constants | `TestAllImpls_Has16Faults`, `TestAllDefs_Has16Defs`, `TestInvariant_16FaultsInCatalog` |
| Step 2 ID list | `TestFaultIDs_SequentialWithGap` |
| Step 1 Def fields | `TestAllDefs_RequiredFieldsPresent` |
| Step 1 Preconditions | `TestAllFaults_HavePreconditions` |
| Step 1 PreconditionChecks validity | `TestInvariant_PreconditionChecks_AreValidCheckIDs` |
| Step 4 FailingChecks | `TestInvariant_FaultFailingChecks_ExistInCatalog` |
| Step 5 Maps-to | (manual audit — no automated test yet) |

**Forbidden:**
- Apply/Recover calls `RunCommand` for mutations (use `RunMutation`)
- Non-reversible fault without `RequiresConfirmation: true` and `ResetTier: "R3"`
- Fault with `PreconditionChecks` containing an ID not in the conformance catalog
- Fault with no postcondition (except F-008, F-014 exception documented in fault-model §8)

---

## Adding a New Conformance Check

**Required changes:**

1. `internal/conformance/catalog.go` — new `Check` in correct series position; Execute uses Observer only
2. `internal/conformance/runner_test.go` and `internal/invariants/invariants_test.go` — increment expected count in `TestCatalog_Has23Checks` and `TestInvariant_23ChecksInConformanceCatalog`
3. `conformance-model.md §3` — full check entry including ID, layer, category, severity, assertion, failure meaning, observable command, and `Maps to` field. The `Maps to` field update alone is insufficient; the check's complete specification must be present before the implementation is committed.
4. `system-state-model.md §6` — update state-to-check mapping table
5. `fault-model.md` — update FailingChecks of any fault whose Apply affects this check
6. `docs/golden-baseline-ledger.md §III` — add check ID to frozen list; update count if changed
7. `testdata/golden/` — regenerate golden fixtures: `UPDATE_GOLDEN=1 go test ./internal/output/...`

**Tests that will fail:**

| Change | Failing test |
|---|---|
| Count not updated (step 2) | `TestInvariant_23ChecksInConformanceCatalog`, `TestCatalog_Has23Checks` |
| Severity wrong | `TestCatalog_SeverityDistribution` |
| ID not unique | `TestCatalog_UniqueIDs` |
| No Execute func | `TestCatalog_AllHaveExecute` |
| Wrong order | `TestCatalog_OrderSPEFL` |
| Golden fixtures stale (step 7) | `TestOutput_AllRenderedJSON_*`, `TestSuiteResult_*` |

**Forbidden:**
- Blocking check without a failure meaning
- Check without a `Maps to` entry in `conformance-model.md §3` (orphan check)
- Changing existing check severity without regenerating golden fixtures (`UPDATE_GOLDEN=1 go test ./internal/output/...`)

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

**Forbidden:**
- Acquiring a mutation lock in a read-only command
- Using `RunCommand` for any privileged shell operation (use `RunMutation`)
- Writing `state.json` in a read-only command (beyond the `lab status` exception)
- Mutating the environment without first acquiring the lock and writing a pre-mutation audit entry
- Defining state effects not declared in `control-plane-contract.md §4`
- Returning exit code 0 on an error condition (all errors produce non-zero exit codes — see ledger §II)

---

## Adding a New Executor Mutation Method

**Required changes:**

1. `internal/executor/executor.go` — add method to `Executor` interface
2. `internal/executor/real.go` — implement; call `r.audit.LogOp(...)` before return
3. `internal/executor/audit_test.go` — add `TestAuditLogger_<TypeName>_*` test verifying the new entry is emitted with correct fields under the triggering condition; update `TestMutationAuditCompleteness_AllMutationMethodsAreAudited` to include the new method if it adds a mutation path
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
| Check severity | Run `UPDATE_GOLDEN=1 go test ./internal/output/...` to regenerate all golden fixtures. Update `conformance-model.md §3.1` severity field. Verify `TestCatalog_SeverityDistribution` still reflects intended distribution. |
| Fault ID | Breaking change. Do not renumber existing IDs. New faults use next sequential ID. |
| Audit entry_type | Add only. Do not remove or rename existing values. See "Adding a New Audit Entry Type" below. |

**Regenerating golden fixtures:** any change to JSON output structure or check severity requires regenerating the golden fixtures in `testdata/golden/`. Run:

```bash
UPDATE_GOLDEN=1 go test ./internal/output/...
```

Commit the regenerated files. A PR that changes output structure without regenerating golden fixtures will fail `TestOutput_AllRenderedJSON_*`.

---

## Adding a New Invariant

An invariant is a cross-cutting rule that must hold after every change. Adding one requires both a specification location and an enforcing test.

**Required changes:**

1. `internal/invariants/invariants_test.go` — new `TestInvariant_<Name>` function; comment must name the spec authority (document and section)
2. Spec authority document — add normative language in the appropriate section. Invariants without a spec citation are ungoverned opinions, not enforced rules.
3. `docs/extension-boundary-note.md` — add a row to the "Invariants That Must Hold After Any Change" table below
4. Audit all existing tests to confirm the new invariant is not already violated by an existing passing test

**Tests that will fail if step 1 is skipped:**

The invariant itself will not be enforced. No test failure prevents the gap — this is a process requirement, not an automated one.

**Forbidden:**
- Invariant test with no spec authority citation in the comment
- Adding a row to the invariants table without a corresponding `TestInvariant_*` test
- Invariant that duplicates the assertion of an existing test (consolidate instead)

---

## Adding a New System State

States are part of the frozen state name surface. A new state requires coordinated changes across four documents and the Go type.

**Required changes:**

1. `internal/state/state.go` — add new `State` constant; update `ValidStates` set
2. `system-state-model.md §2` — full state definition: invariants, entry conditions, permitted operations, exit conditions
3. `system-state-model.md §3` — add valid transitions into and out of the new state to the transition table
4. `system-state-model.md §4.2` — update the detection algorithm if the new state is reachable via runtime observation
5. `control-plane-contract.md §4` — update every command whose execution contract is affected by the new state (preconditions, state effects, exit codes)
6. `canonical-environment.md` — update the "Environment State Model" section preamble
7. `internal/state/store.go` — update the `state` enum in the JSON schema comment if present
8. `docs/golden-baseline-ledger.md` — update any state-enumeration references

**Tests that will fail:**

| Change | Failing test |
|---|---|
| `All()` not updated | `TestState_ValidStates_ContainsAll` |
| `IsValid` inconsistent | `TestState_IsValid_AcceptsAllSixStates` |
| `CanApplyFault` wrong | `TestState_Transitions_CanApplyFault` |
| `CanReset` wrong | `TestState_Transitions_CanReset` |
| `RequiresActiveFault` wrong | `TestState_Transitions_RequiresActiveFault` |
| `ForbidsActiveFault` wrong | `TestState_Transitions_ForbidsActiveFault` |
| Requires/Forbids not complementary | `TestState_Transitions_RequiresForbids_AreComplementary` |
| Detection algorithm gap | `TestDetect_RecordedStateVariants`, `TestDetect_ClassificationInvalid_AlwaysRederives` |

**Forbidden:**
- New state without full invariant definition in `system-state-model.md §2`
- New state without defined transitions in `system-state-model.md §3`
- Reusing an existing state name with different semantics

---

## Adding a New Audit Entry Type

Audit entry types are part of the frozen `entry_type` surface. New types may be added; existing types must never be removed or renamed.

**Required changes:**

1. `internal/executor/audit.go` — add new `entry_type` constant string; add `LogXxx(...)` method on `AuditLogger` if a new structured method is needed
2. `control-plane-contract.md §7.2` — add the new entry type to the audit event catalog with: type name, triggering event, mandatory fields, which command(s) emit it
3. `internal/executor/audit_test.go` — add `TestAuditLogger_<TypeName>_*` test verifying the new entry is emitted with correct fields under the triggering condition
4. `docs/operational-trace-spec.md` — update any affected event traces that should include the new entry type

**Tests that will fail if steps are skipped:**

| Skipped step | Failing test |
|---|---|
| Step 3 (no test) | No automated failure — process requirement |
| Entry missing mandatory fields | `TestAuditEntry_<TypeName>_HasRequiredFields` (new test from step 3) |

**Forbidden:**
- Removing or renaming an existing `entry_type` value (breaking change to consumers parsing audit logs)
- Emitting a new entry type without a `control-plane-contract.md §7.2` catalog entry
- Emitting audit entries via `fmt.Fprintf` to the log file directly (use the `AuditLogger` methods)

---

## Invariants That Must Hold After Any Change

| Invariant | Spec authority | Enforcing test |
|---|---|---|
| `lab validate` does not update `state` field | control-plane-contract §4.2 | `TestValidateCmd_WritesLastValidate_NotState` |
| `lab status` is the only reconciliation point | control-plane-contract §4.1 | `TestStatusCmd_ReconcilesBrokenToConformant_*` |
| Degraded checks do not affect exit code | conformance-model §4.5 | `TestSuiteResult_Classify_DegradedOnly` |
| At most one active fault | fault-model §2.2 | `TestFaultApplyCmd_PreconditionFails_FaultAlreadyActive` |
| Apply failure does not update state | control-plane-contract §4.5 | `TestFaultApplyCmd_ApplyFailure_DoesNotUpdateState` |
| Interrupt does not assert BROKEN | control-plane-contract §3.6 | `TestInterruptPath_DoesNotAssertBroken` |
| Every mutation produces an audit entry | control-plane-contract §5.2 | `TestMutationAuditCompleteness_AllMutationMethodsAreAudited` |
| Audit log never truncated | control-plane-contract §7.1 | `TestAuditLogger_AppendOnly` |
| UNKNOWN is classification failure, not state | system-state-model §4.4 | `TestIsUnknown` |
| State set contains exactly six defined states | system-state-model §2 | `TestState_ValidStates_ContainsAll` |
| No mutation through Observer interface | boundary audit | `TestObserver_DoesNotHaveMutationMethods` |