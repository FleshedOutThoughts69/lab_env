# Testing Plan — Exhaustive Execution Map
## lab-env control plane + service module

---

## Mental model

This is not a testing phase after implementation. It is **progressive system activation under controlled adversarial conditions**. Every phase answers a different question:

- **Phase 0** — Are the tests themselves correctly structured to provide meaningful signal?
- **Phase A** — Does the implementation satisfy its own specification on a real OS?
- **Phase B** — Does the system behave correctly when the OS is real, noisy, and partially failing?
- **Phase C** — Does the system hold its invariants under adversarial input sequences?
- **Phase D** — Is the observable output contract frozen and verifiable?

Phases must be executed in dependency order. Within each phase, items are listed in the order they must be verified.

---

## Completed items (do not re-execute)

These were completed during implementation and are verified by named tests. Listed here to prevent re-work.

| Item | Verifying tests |
|---|---|
| Config drift — no hardcoded paths outside `config.go` | grep audit + `TestArchitecture_*` |
| Status vs validate semantic separation | `TestValidateCmd_WritesLastValidate_NotState`, `TestValidateCmd_DoesNotReconcileState` |
| Conformance runner dependency ordering | `TestRunner_DependentMarking`, `TestCatalog_OrderSPEFL` |
| Executor boundary (mutation monopoly) | `TestObserver_DoesNotHaveMutationMethods`, `boundary_test.go` |
| Fault catalog completeness | `TestAllImpls_Has16Faults`, `TestPostconditions_FailingChecksAreKnownIDs` |
| Cross-document invariants | All `TestInvariant_*` in `internal/invariants/` |
| Interrupt path (contract tests) | All `TestInterruptPath_*` in `cmd/interrupt_test.go` |

---

## Known open issues (resolve before declaring complete)

**H-001 — Status output endpoint code inference**
`cmd/status.go` `buildStatusResult` contains `code := 502 // best guess`. Endpoint codes must be projected from actual check results, not guessed. This blocks Phase D golden fixture expansion. Regression guard: `TestRenderStatus_JSON_EndpointCodesNotGuessed`.

**State file concurrent write race**
`lab status` writes `state.json` without the mutation lock. A concurrent `lab fault apply` also writes under the lock. The atomic write (temp + rename) mitigates torn writes but does not prevent read-modify-write races. Surfaces in Phase C.

---

## Dependency graph

```
Phase 0: Test suite bifurcation
    │
    ▼
Phase A: Unit baseline + H-001 fix
    │
    ▼
Phase B: Live system contract validation
    ├── B1: Interrupt path (real OS signal)
    ├── B2: All 16 faults on real VM        ← parallelizable after B1 green
    └── B3: Status/validate divergence cycle ← parallelizable after B1 green
    │
    ▼
Phase C: Invariant stress testing
    │
    ▼
Phase D: Output and schema freezing
```

---

## Phase 0 — Test suite bifurcation

**Purpose:** prevent mock-green tests from masking live-system failures. Without bifurcation, a full `go test ./...` run on the real VM intermixes unit tests (which pass everywhere) with integration tests (which require a running environment), producing misleading results.

**Exit criterion:** `go test ./...` (no tags) runs all unit tests and skips all integration tests cleanly. `LAB_TEST_MODE=live go test ./... -tags integration` runs only live-system tests.

### 0.1 — Build tag assignment

Apply `//go:build integration` to every test file that requires a running VM, real OS signals, real service processes, or real filesystem paths outside `t.TempDir()`.

| Test file | Tag | Reason |
|---|---|---|
| `internal/conformance/runner_test.go` | unit | stub observer only |
| `internal/state/detect_test.go` | unit | pure functions |
| `internal/state/store_test.go` | unit | uses `t.TempDir()` |
| `internal/state/signal_combinations_test.go` | unit | pure inputs |
| `internal/state/store_edge_cases_test.go` | unit | uses `t.TempDir()` |
| `internal/executor/audit_test.go` | unit | uses `t.TempDir()` |
| `internal/executor/lock_test.go` | unit | uses `t.TempDir()` |
| `internal/executor/lock_stale_system_process_test.go` | unit | uses `/proc` but no service |
| `internal/executor/boundary_test.go` | unit | type-level only |
| `internal/executor/restore_test.go` | unit | recording executor |
| `internal/executor/mutation_failure_test.go` | unit | uses `t.TempDir()` |
| `internal/executor/embed_test.go` | unit | reads embedded bytes only |
| `internal/executor/trace_test.go` | unit | recording struct only |
| `internal/catalog/catalog_test.go` | unit | no OS calls |
| `internal/catalog/content_integrity_test.go` | unit | recording executor |
| `internal/conformance/runner_edge_cases_test.go` | unit | stub observer |
| `internal/conformance/cross_module_test.go` | unit | uses `httptest.NewServer` |
| `internal/invariants/invariants_test.go` | unit | catalog + conformance data |
| `internal/invariants/architecture_test.go` | unit | runs `go list` (no VM) |
| `internal/output/render_test.go` | unit | no OS calls |
| `internal/output/golden_test.go` | unit | reads testdata/ |
| `internal/output/output_quality_test.go` | unit | no OS calls |
| `cmd/status_test.go` | unit | stub observer + `t.TempDir()` |
| `cmd/validate_test.go` | unit | stub observer + `t.TempDir()` |
| `cmd/fault_test.go` | unit | stub executor + `t.TempDir()` |
| `cmd/interrupt_test.go` | unit | `testutil.InterruptableExecutor` |
| `service/*/` all test files | unit | `t.TempDir()` + httptest |

**The following two files do not yet exist and must be written before Phase 0 is complete:**

| File to create | Tag | Content |
|---|---|---|
| `cmd/live_interrupt_test.go` | **integration** | Subprocess-based test: starts `./lab reset` as a child process against the real state path, sends `SIGINT` via `os.Process.Signal`, asserts exit code 4, reads `/var/lib/lab/state.json` for `classification_valid: false`, reads `/var/lib/lab/audit.log` for interrupt entry. See B.1 for the full test sequence. |
| `cmd/live_fault_matrix_test.go` | **integration** | Table-driven test iterating the 14 reversible faults: for each fault, calls `./lab fault apply <ID>`, asserts state=DEGRADED, calls `./lab validate`, asserts failing check IDs match `FaultDef.PostconditionSpec.FailingChecks`, calls `./lab reset`, asserts state=CONFORMANT. |

Both files require `LAB_TEST_MODE=live` to run and use real OS calls — no mock executors.

### 0.2 — TestMain guards

Add `LAB_TEST_MODE` guard to `TestMain` in each integration test package so integration tests skip cleanly when running in unit mode:

```go
func TestMain(m *testing.M) {
    if os.Getenv("LAB_TEST_MODE") != "live" {
        os.Exit(0)
    }
    os.Exit(m.Run())
}
```

### 0.3 — Verify run modes

```bash
# Unit only — must complete with no failures, no skips-as-failures
cd /opt/lab-env && go test ./...
cd /opt/lab-env/service && go test ./...

# Integration only — must require running VM
LAB_TEST_MODE=live go test ./... -tags integration -v

# Race detector — run unit suite with -race
go test -race ./...
```

---

## Phase A — Unit baseline and pre-flight fixes

**Purpose:** establish a green unit test baseline on the real system and resolve H-001 before any golden fixture work.

**Exit criterion:** every test listed below passes. The config grep audit produces no output. H-001 is resolved and its regression guard passes.

### A.1 — Config drift audit

```bash
grep -rn '"/etc/app\|"/var/lib/app\|"/opt/app\|"appuser\|"0\.0\.0\.0:80\|127\.0\.0\.1:8080' \
  internal/ cmd/ \
  | grep -v 'config\.go\|_test\.go\|\.md\|Symptom\|Observable\|MutationDisplay'
```

Expected: empty output. Any match is a hardcoded constant that must be moved to `internal/config/config.go`.

### A.2 — Full unit test suite

Every test below must pass. They are grouped by package for clarity.

#### `internal/state/`
- `TestDetect` — all §4.3 conflict resolution cases by section reference
- `TestDetectReconciliationPriorState` — reconciliation direction
- `TestIsUnknown` — UNKNOWN classification semantics
- `TestDetect_TelemetryWrongSchema` — graceful fallback on schema mismatch
- `TestDetect_RecordedStateVariants` — all 6 recorded state values vs conformant suite
- `TestDetect_ClassificationInvalid_AlwaysRederives` — classification_valid=false forces re-derive
- `TestStore_Write_CreatesFile` — basic write creates state.json
- `TestStore_Write_NoTempFilesLeft` — atomic write leaves no .tmp files
- `TestStore_Write_SetsSpecVersion` — spec_version field populated
- `TestStore_RoundTrip_AllFields` — all fields survive marshal/unmarshal
- `TestStore_ActiveFaultNullWhenNotDegraded` — active_fault is null when state ≠ DEGRADED
- `TestStore_Read_MissingFile_ReturnsNotFound` — ErrStateFileNotFound on absent file
- `TestStore_Read_CorruptJSON_ReturnsCorrupt` — ErrStateFileCorrupt on bad JSON
- `TestStore_Read_InvalidState_ReturnsCorrupt` — ErrStateFileCorrupt on unknown state value
- `TestStore_Read_EmptyFile_ReturnsCorrupt` — 0-byte file → ErrStateFileCorrupt (not NotFound)
- `TestStore_Read_WhitespaceOnly_ReturnsCorrupt` — whitespace-only → ErrStateFileCorrupt
- `TestStore_AppendHistory_RingBuffer` — 51 entries evicts oldest; count stays at 50
- `TestStore_InvalidateClassification` — sets classification_valid=false; state unchanged
- `TestStore_Concurrent_InvalidateAndSave` — race safety; final state is consistent
- `TestStore_Save_ReadOnlyDir_ReturnsError` — error returned; original file unchanged
- `TestFresh_Defaults` — zero-value state file has correct defaults

#### `internal/conformance/`
- `TestSuiteResult_Classify_AllPass` — all checks pass → CONFORMANT
- `TestSuiteResult_Classify_BlockingFails` → NON-CONFORMANT
- `TestSuiteResult_Classify_DegradedOnly` → DEGRADED-CONFORMANT; exit code 0
- `TestSuiteResult_Classify_DependentNotCounted` — dependent failures excluded from count
- `TestSuiteResult_HasFailingCheck` — HasFailingCheck returns correct boolean
- `TestRunner_DependentMarking` — E-checks marked Dependent when S-001 fails
- `TestRunner_AllChecksRunEvenOnFailure` — no early abort on blocking failure
- `TestRunner_LightweightRunChecks` — LightweightRun runs exactly 4 checks
- `TestRunner_RunSingle` — single check by ID
- `TestRunner_RunIDs` — run subset by ID list
- `TestCatalog_Has23Checks` — exactly 23 checks in catalog
- `TestCatalog_UniqueIDs` — all check IDs distinct
- `TestCatalog_AllHaveExecute` — every check has non-nil Execute
- `TestCatalog_SeverityDistribution` — blocking/degraded counts match spec
- `TestCatalog_OrderSPEFL` — checks ordered S→P→E→F→L
- `TestRunner_PanickingCheck_DoesNotHaltSuite` — panic caught; suite continues; failing result recorded
- `TestCatalog_SeverityInvariant_BlockingChecksAreInCorrectSeries` — blocking=S/P/E/F(except F-006); degraded=F-006/L
- `TestCatalog_AllChecks_HandleMissingFilesGracefully` — all Execute functions tolerate missing files without panic
- `TestE001_CheckLogic_MatchesHandlerResponse` — E-001 passes on 200 response
- `TestE002_CheckLogic_FailsOn500` — E-002 fails on 500 response
- `TestE003_CheckLogic_MatchesHandlerResponse` — E-003 passes on exact `{"status":"ok"}` body
- `TestF004DiagnosticPattern_E001PassesE002Fails` — E-001 pass / E-002 fail simultaneously confirmed

#### `internal/executor/`
- `TestAuditEntry_Schema` — all required fields present in entry JSON
- `TestAuditEntryTypes_Values` — 6 distinct entry type constants
- `TestAuditLogger_LogOp_WritesEntry` — executor_op entry written
- `TestAuditLogger_LogTransition_WritesEntry` — state_transition entry written
- `TestAuditLogger_LogInterrupt_WritesEntry` — interrupt entry written
- `TestAuditLogger_AppendOnly` — second write appends; does not overwrite
- `TestAuditLogger_ErrorEntry_DoesNotPanic` — error entry write is safe
- `TestAuditLogger_TimestampPresent` — ts field is valid RFC3339
- `TestMutationAuditCompleteness_AllMutationMethodsAreAudited` — every Executor mutation method produces an audit entry
- `TestAuditEntry_OnMutationFailure` — LogError produces valid error-type audit entry with operation, path, error fields
- `TestLock_Acquire_Succeeds_WhenAbsent` — clean acquisition
- `TestLock_Acquire_Fails_WhenHeldByLiveProcess` — live PID blocks acquisition
- `TestLock_Acquire_Reclaims_StaleLock` — dead PID is reclaimable
- `TestLock_Acquire_Reclaims_MalformedLock` — malformed PID file is reclaimable
- `TestLock_Release_RemovesLockFile` — release deletes lock file
- `TestLock_Release_IsIdempotent` — double release is safe
- `TestLock_AcquireAfterRelease_Succeeds` — re-acquisition after release works
- `TestLock_SecondInstance_Fails_WhileFirstHeld` — concurrent acquisition fails
- `TestErrLockHeld_ContainsPID` — error message includes blocking PID
- `TestLock_StaleLockWithForeignPID_IsReclaimed` — PID 1 (system process) is reclaimable; PID collision does not permanently block
- `TestLock_StaleLockWithCurrentPID_IsReclaimed` — self-PID lock behavior documented
- `TestObserver_DoesNotHaveMutationMethods` — Observer interface has no mutation methods
- `TestExecutor_SatisfiesObserver` — Executor satisfies Observer (embeds it)
- `TestObserver_SatisfiesConformanceObserver` — NewObserver() satisfies conformance.Observer at compile time
- `TestRunCommand_AvailableOnObserver` — RunCommand is accessible on the Observer interface
- `TestRunMutation_RequiresExecutor` — RunMutation is only accessible via Executor, not Observer
- `TestFaultApply_ReceivesExecutor_NotObserver` — fault Apply functions receive Executor, not Observer
- `TestRestoreFile_CanonicalMap_HasCorrectModes` — per-file mode and ownership correct (config.yaml→appuser 0640; app.service→root 0644; nginx.conf→root 0644)
- `TestRunMutation_NonZeroExit_WritesAuditErrorEntry` — failed mutation produces error audit entry
- `TestExecutor_AuditLogger_RequiredForMutations` — nil audit logger rejected by constructor
- `TestEmbeddedFiles_NonEmpty` — all embedded files have non-zero content
- `TestEmbeddedConfigYaml_ContainsRequiredKeys` — embedded config.yaml has addr, app_env, 127.0.0.1:8080
- `TestEmbeddedAppService_ContainsRequiredDirectives` — embedded unit file has ExecStart, User=appuser, RuntimeDirectory=app, StartLimitBurst, TimeoutStopSec, Slice=app.slice
- `TestEmbeddedNginxConf_ContainsUpstreamBlock` — upstream app_backend block present and active; proxy_pass http://app_backend; X-Proxy nginx present
- `TestEmbeddedFiles_ModeAndOwnershipNonEmpty` — mode, owner, group all non-zero for every file
- `TestEmbeddedFiles_NoNullBytes` — no null bytes in embedded file content
- `TestOperationalTrace_FaultApply_SequenceIsCorrect` — 8-step sequence: lock→read→obs→audit→mut→audit→write→unlock
- `TestOperationalTrace_WriteBeforeUnlock` — state written before lock released
- `TestOperationalTrace_AuditBeforeMutation` — audit entry precedes mutation
- `TestOperationalTrace_ReadOnlyCommand_NoLock` — read-only paths never acquire lock

#### `internal/catalog/`
- `TestAllImpls_Has16Faults` — exactly 16 FaultImpl instances (F-011/F-012 are baseline behaviours, not faults)
- `TestAllDefs_Has16Defs` — exactly 16 FaultDef instances
- `TestFaultIDs_AreUnique` — all IDs distinct
- `TestFaultIDs_SequentialWithGap` — catalog IDs match expected list; gap at F-011/F-012 is documented
- `TestFaultIDs_F011AndF012_NotInCatalog` — ImplByID and DefByID return nil for F-011 and F-012
- `TestAllDefs_RequiredFieldsPresent` — all mandatory FaultDef fields populated
- `TestAllImpls_HaveApplyAndRecover` — Apply and Recover non-nil for all faults
- `TestAllFaults_HavePreconditions` — every fault has at least one precondition
- `TestNonBaseline_StandardPrecondition` — all faults require CONFORMANT precondition
- `TestNonReversibleFaults_AreF008AndF014` — exactly F-008 and F-014 are non-reversible
- `TestNonReversibleFaults_RequireConfirmation` — non-reversible require RequiresConfirmation=true
- `TestNonReversibleFaults_HaveR3ResetTier` — non-reversible have ResetTier=R3
- `TestPostconditions_FailingChecksAreKnownIDs` — all FailingChecks are valid conformance IDs
- `TestNonBaseline_NonBaselineHasPostcondition` — every non-baseline fault has a PostconditionSpec
- `TestDefByID_KnownFault` — DefByID returns correct def
- `TestDefByID_UnknownFault` — DefByID returns ErrUnknownFaultID
- `TestImplByID_KnownFault` — ImplByID returns correct impl
- `TestImplByID_UnknownFault` — ImplByID returns ErrUnknownFaultID
- `TestAllDefs_ReturnsCopies` — AllDefs returns copies, not aliases
- `TestFaultRecover_RestoresExactContent` — Recover writes different content than Apply for every reversible fault
- `TestNonReversibleFaults_RecoverReturnsError` — F-008 and F-014 Recover return non-nil error directing to R3
- `TestFaultApply_TargetsOnlyDeclaredFile` — Apply writes to ≤3 files (no overly broad replaceInBytes)

#### `internal/invariants/`
- `TestInvariant_FaultFailingChecks_ExistInCatalog` — FailingChecks IDs exist in conformance catalog
- `TestInvariant_DegradedChecks_AreNonBlocking` — all L-series and F-006 have SeverityDegraded
- `TestInvariant_NoBaselineFaultsInCatalog` — F-011 and F-012 absent from catalog
- `TestInvariant_NonReversible_RequiresR3` — non-reversible faults require R3 reset tier
- `TestInvariant_NonReversible_RequiresConfirmation` — non-reversible faults require explicit confirmation
- `TestInvariant_AllFaults_HaveConformantPrecondition` — all faults require CONFORMANT state precondition
- `TestInvariant_PreconditionChecks_AreValidCheckIDs` — every check ID in PreconditionChecks exists in the conformance catalog
- `TestInvariant_F010_HasPreconditionCheck_P001` — F-010 specifically declares P-001
- `TestInvariant_ConformantState_IsValid` — CONFORMANT is a valid state value
- `TestInvariant_AllStates_AreValid` — all 6 state constants are distinct non-empty strings
- `TestInvariant_ExactlySixStates` — exactly 6 canonical states
- `TestInvariant_Degraded_RequiresActiveFault` — DEGRADED state requires non-nil active fault (invariant I-2)
- `TestInvariant_Conformant_ForbidsActiveFault` — CONFORMANT state forbids active fault (invariant I-2)
- `TestInvariant_16FaultsInCatalog` — catalog has exactly 16 faults
- `TestInvariant_23ChecksInConformanceCatalog` — catalog has exactly 23 checks
- `TestInvariant_ResetTierValues` — all ResetTier values are R1, R2, or R3 (empty no longer valid)
- `TestArchitecture_NoProductionCodeImportsTestingPackage` — production packages do not import testing or testutil
- `TestArchitecture_ConformanceChecks_DoNotImportExecutor` — conformance cannot gain mutation capability
- `TestArchitecture_CatalogFaults_DoNotImportState` — faults cannot bypass audit path
- `TestArchitecture_OutputPackage_DoNotImportConformance` — presentation cannot trigger execution
- `TestArchitecture_ServiceModule_DoesNotImportControlPlane` — service module has no lab-env/lab imports

#### `internal/output/`
- `TestRenderStatus_JSON_Schema` — all required status fields present
- `TestRenderStatus_JSON_WithActiveFault` — active_fault populated correctly
- `TestRenderStatus_JSON_EndpointCodesNotGuessed` — **H-001 regression guard; must pass before Phase D**
- `TestRenderStatus_Unknown` — UNKNOWN state renders without panic
- `TestRenderValidate_JSON_Schema` — validate output schema correct
- `TestRenderFaultList_JSON_Schema` — fault list schema correct
- `TestRenderFaultApply_JSON_Schema` — fault apply schema correct
- `TestRenderFaultApply_Aborted` — aborted apply renders correctly
- `TestRenderer_ErrorGoesToStderr` — error output goes to stderr
- `TestRenderer_RenderGoesToStdout` — data output goes to stdout
- `TestRenderer_QuietSuppressesOutput` — quiet mode suppresses all human-readable output
- `TestFromSuiteResult_Completeness` — all 23 check results appear in validate output
- `TestGolden_Status_Conformant` — CONFORMANT status matches frozen fixture
- `TestGolden_Status_Degraded` — DEGRADED status matches frozen fixture
- `TestGolden_Status_Broken` — BROKEN status matches frozen fixture
- `TestGolden_Validate_Conformant` — all-pass validate matches frozen fixture
- `TestGolden_FaultApply_Success` — successful apply matches frozen fixture
- `TestGolden_FaultInfo_F004` — F-004 info matches frozen fixture
- `TestGolden_StatusResult_NoExtraFields` — no unexpected fields in status output
- `TestGolden_ValidateResult_NoExtraFields` — no unexpected fields in validate output
- `TestGolden_FaultApplyResult_NoExtraFields` — no unexpected fields in fault apply output
- `TestGolden_ActiveFault_NullNotAbsent` — active_fault: null always present; never omitted
- `TestOutput_AllRenderedJSON_IsValidUTF8` — all rendered JSON is valid UTF-8
- `TestOutput_AllRenderedJSON_NoTrailingWhitespace` — no trailing whitespace on any JSON line
- `TestOutput_JSON_IsCompactNotPretty` — output is compact, not indented
- `TestOutput_StatusResult_LastValidateTimestamp_IsRFC3339` — last_validate field is RFC3339
- `TestOutput_JSON_NoDoubleEncoding` — no `\"` sequences from double-encoding

#### `cmd/`
- `TestStatusCmd_ConformantEnvironment_ReturnsConformant`
- `TestStatusCmd_ReconcilesBrokenToConformant_WhenRuntimeHealthy` — status is only reconciliation point
- `TestStatusCmd_DoesNotReconcile_WhenStateMatchesRuntime`
- `TestStatusCmd_UnhealthyRuntime_ReturnsBroken`
- `TestStatusCmd_MissingStateFile_DoesNotCrash`
- `TestStatusCmd_CorruptStateFile_ReturnsUnknownOrDetects`
- `TestStatusCmd_DegradedWithFault_ReturnsDegraded`
- `TestStatusCmd_ClassificationInvalid_ForcesReclassification`
- `TestValidateCmd_WritesLastValidate_NotState` — validate never updates state field
- `TestValidateCmd_FullRun_ExitCode0_WhenOnlyDegradedFail` — exit 0 on degraded-only failures
- `TestValidateCmd_SingleCheck_WritesNothing` — single-check mode has no state side effects
- `TestValidateCmd_UnknownCheckID_ReturnsUsageError`
- `TestValidateCmd_DoesNotReconcileState` — validate never changes state classification
- `TestFaultApplyCmd_UnknownID_RejectsBeforeLock` — catalog lookup before lock acquisition
- `TestFaultApplyCmd_BaselineID_Rejected` — F-011/F-012 return ErrUnknownFaultID
- `TestFaultApplyCmd_PreconditionFails_NotConformant`
- `TestFaultApplyCmd_PreconditionFails_FaultAlreadyActive` — idempotency guard
- `TestFaultApplyCmd_PreconditionCheckFails_F010` — P-001 not satisfied; Apply rejected before mutation (control-plane-contract §4.5 step 5)
- `TestFaultApplyCmd_PreconditionCheckPasses_F010` — P-001 satisfied; Apply proceeds normally
- `TestFaultApplyCmd_ForceBypassesPrecondition`
- `TestFaultApplyCmd_ForceBypassesPreconditionChecks` — --force skips PreconditionChecks guard
- `TestFaultApplyCmd_ApplyFailure_DoesNotUpdateState` — atomicity guarantee
- `TestFaultApplyCmd_Success_UpdatesStateToDegraded`
- `TestFaultApplyCmd_Success_WritesAuditEntry`
- `TestFaultApplyCmd_RequiresConfirmation_WithoutYes_Aborts`
- `TestFaultApplyCmd_HistoryUpdated_OnSuccess`
- `TestFaultList_UsesAllDefs`
- `TestFaultInfo_UsesDefByID`
- `TestFaultInfo_UnknownID_ReturnsError`
- `TestInterruptPath_Reset_FullContract` — all 8 interrupt contract points in sequence
- `TestInterruptPath_BeforeMutation_ExitsCleanly` — pre-mutation interrupt exits 0
- `TestInterruptPath_ClassificationInvalid_ForcesStatusReclassification`
- `TestInterruptPath_DoesNotAssertBroken` — interrupt never asserts BROKEN
- `TestInterruptPath_GracePeriod_CurrentOperationCompletes`
- `TestInterruptPath_AuditEntries_OrderedCorrectly`
- `TestInterruptPath_ExitCode4_Semantics`

#### `service/config/`
- `TestLoad_ValidConfig_Parses`
- `TestLoad_MissingFile_ReturnsError`
- `TestLoad_UnknownKey_ReturnsError` — KnownFields strict mode; typo like "app_envv" fails
- `TestLoad_DefaultAddr_WhenMissing`
- `TestLoad_DefaultAppEnv_WhenEmpty`
- `TestParseBool_AcceptsMultipleTrueValues` — "1", "true", "yes" all activate chaos modes
- `TestLoad_MissingChaosVars_AllDefault` — all chaos modes disabled when vars absent
- `TestSanitizeEnvString_StripsControlChars` — newlines, tabs, null bytes stripped from app_env
- `TestLoad_InvalidLatencyMS_DisablesMode` — non-integer silently disables latency
- `TestLoad_AppEnv_SpacesAreSanitized` — documented behavior for leading/trailing spaces
- `TestLoad_ConfigWithBOM_HandledCorrectly` — BOM produces clear error or correct values
- `TestLoad_DropPercentOutOfRange_Disabled` — negative or >100 values disabled
- `TestChaosConfig_ActiveModes_OrderIsStable` — ActiveModes() order deterministic across calls

#### `service/logging/`
- `TestNew_OpensWithOAppend` — truncation followed by write produces no null bytes
- `TestLogger_ConcurrentWrites_NoInterleavedLines` — 50 goroutines × 20 entries; all lines valid JSON
- `TestLogger_EntryIsCompleteJSON` — each Write is one complete newline-terminated JSON object
- `TestLogger_KeyValuePairs` — extra k/v pairs appear in entry
- `TestLogger_FileMode0640` — log file created with mode 0640
- `TestLogger_SpecialChars_ProperlyEscaped` — quotes, backslashes, unicode produce valid JSON
- `TestLogger_Close_Idempotent` — double Close does not panic
- `TestLogger_WriteAfterClose_DoesNotPanic` — write after Close is safe
- `TestLogger_Levels_ProduceCorrectLevelField` — Info/Warn/Error produce "info"/"warn"/"error"

#### `service/signals/`
- `TestStartupSequence_LoadingBeforeHealthy` — loading→healthy→remove_loading sequence; files never coexist
- `TestShutdownSequence_StatusBeforeHealthyRemoval` — status=ShuttingDown written before healthy removed
- `TestInit_RemovesStaleLoadingFromCrash` — stale loading from previous crash removed and recreated
- `TestInit_RemovesStaleHealthy` — stale healthy from previous crash removed
- `TestAtomicWrite_NoZeroByteTempFile` — no .tmp- files remain after write
- `TestSignalFiles_Mode0644` — all signal files created with mode 0644
- `TestBeginShutdown_WhenHealthyAlreadyRemoved` — idempotent when healthy already absent
- `TestShutdownSequence_RemovesPID` — RemovePID removes the PID file
- `TestSetStatus_ContentIsExactStringPlusNewline` — status file contains exact string + `\n`
- `TestWritePID_ContainsDecimalPIDAndNewline` — PID file is decimal PID + `\n`
- `TestRemoveLoading_IdempotentWhenAbsent` — no error when loading never existed

#### `service/telemetry/`
- `TestSnapshot_Schema_AllFieldsPresent` — exactly 12 JSON fields with correct names
- `TestSnapshot_Schema_FieldTypes` — numerics are numbers; chaos_modes is array not null
- `TestCollector_WritesTelemetryFile` — file exists and parses after first write
- `TestCollector_PanicRecovery` — panic in write function; goroutine continues writing
- `TestCollector_ChaosModesNeverNull` — chaos_modes always `[]` not `null`
- `TestCollector_UptimeSeconds_MonotonicallyIncreasing` — second snapshot > first
- `TestCollector_WrittenWithZeroRequests` — file written before first request; all counters 0
- `TestCollector_MemoryRSSMB_NonZeroWhenRunning` — memory_rss_mb ≥ 1.0 for running process

#### `service/chaos/`
- `TestChaosHandler_Latency_ExemptedForHealth` — /health returns in < latencyMS; / waits ≥ latencyMS
- `TestChaosHandler_Drop_IncrementsRequestsAndErrors` — 100% drop; both counters = 1
- `TestChaosHandler_Drop_BeforeLatency` — 100% drop + 500ms latency; returns instantly
- `TestChaosHandler_ZeroDrop_PassesThrough` — 0% drop; 100 requests, 100 reach base handler
- `TestStartOOM_SyncOnce_GuardsAgainstDuplicateStart` — 10 concurrent calls → 1 goroutine
- `TestChaosHandler_NilCallbacks_NoPanic` — nil reqCounter/errCounter safe
- `TestChaosHandler_ConcurrentRequests_CountersAccurate` — 1000 concurrent requests; counters match
- `TestChaosHandler_Drop100_AllNonHealthRoutesDrop` — /, /slow, others all get 503
- `TestChaosHandler_Latency_ActuallyDelaysRoot` — measured latency ≥ configured latencyMS
- `TestChaosHandler_ZeroLatency_NoDelay` — latencyMS=0 adds < 50ms

#### `service/server/`
- `TestHandleHealth_Returns200_WithOKBody` — exact body `{"status":"ok"}`
- `TestHandleHealth_NeverTouchesStateDir` — /health returns 200 with state dir mode 0000
- `TestHandleRoot_Success_Returns200_WithEnv` — body contains status=ok and env=prod
- `TestHandleRoot_StateWriteFailure_Returns500_WithExactBody` — exact body `{"status":"error","msg":"state write failed"}`
- `TestHandleRoot_StateWriteFailure_E001StillPasses` — simultaneous: health=200, root=500
- `TestHandleSlow_Returns200_After5Seconds` — returns 200 after ≥ 4.8s
- `TestHandleRoot_CountersIncrement` — RequestsTotal increments per request
- `TestHandleRoot_ErrorCounterIncrements` — ErrorsTotal increments on state write failure
- `TestHandleRoot_EmptyAppEnv_ReturnsEmptyStringNotNull` — env="" not null
- `TestHandlers_NoGoServerHeader` — no Server header in responses
- `TestConcurrent_HealthAndRoot_StateWriteFailure` — 20 concurrent pairs; health always 200; root always 500

### A.3 — H-001 fix verification

After fixing `cmd/status.go` `buildStatusResult`:

```bash
go test ./internal/output/... -run TestRenderStatus_JSON_EndpointCodesNotGuessed
# must pass before proceeding to Phase D
```

---

## Phase B — Live system contract validation

**Purpose:** prove the system behaves correctly on a real Ubuntu 22.04 VM with real systemd, real nginx, real OS scheduling, and real signal delivery. Unit tests with mock executors cannot prove this.

**Precondition:** bootstrap complete; `sudo bash /opt/lab-env/scripts/bootstrap.sh` exits 0; `sudo bash /opt/lab-env/scripts/validate.sh` prints CONFORMANT.

**Exit criterion:** all items below produce their specified outcomes on the target VM.

### B.1 — Live interrupt path

This is the highest-value remaining proof. The interrupt path spans real OS signal delivery → real process termination → real state.json invalidation → real audit.log entry → real exit code → real `lab status` reclassification. Every component is individually tested. The composition is not.

**Test sequence — run in order:**

1. `./lab fault apply F-001` — establish DEGRADED state (provides something for reset to do)
2. `./lab reset &; RESET_PID=$!` — begin reset in background
3. Wait for reset process to be alive: `kill -0 $RESET_PID` returns 0
4. `kill -SIGINT $RESET_PID` — send interrupt
5. `wait $RESET_PID; echo $?` — **must print 4**
6. `cat /var/lib/lab/state.json | jq .classification_valid` — **must print false**
7. `grep '"entry_type":"interrupt"' /var/lib/lab/audit.log` — **must find an entry**
8. `./lab status --json | jq .state` — **must NOT print BROKEN** (interrupt ≠ assertion of BROKEN)
9. `./lab status --json | jq .classification_valid` — **must print true** (reclassified from runtime)
10. `./lab reset` — restore to CONFORMANT

**Edge cases — also verify:**

- Interrupt before first mutation: `./lab reset &; kill -SIGINT $!` immediately → exit 0, state unchanged
- Interrupt after all mutations but before state write → exit 4, classification_valid=false
- SIGTERM (not SIGINT) during reset → same behavior as SIGINT (both caught by signal handler)

### B.2 — Live fault execution matrix

Execute all 16 faults against the real VM using the fault-matrix-runbook as the verification checklist. Use `scripts/run-fault-matrix.sh` for the 14 reversible faults; execute F-008 and F-014 manually. F-011 and F-012 are baseline network behaviours documented in `fault-model.md §10` — they are not faults and are not in the catalog; observe them separately per B.2.1.

**Fault breakdown:**
- 14 reversible: F-001–F-007, F-009, F-010, F-013, F-015–F-018
- 2 non-reversible: F-008, F-014 (require `--yes`; R3 recovery)

**For each reversible fault (F-001–F-007, F-009, F-010, F-013, F-015–F-018):**

1. **Pre-flight:** `./lab validate` exits 0; `./lab status --json | jq .state` = "CONFORMANT"
2. **Apply:** `./lab fault apply <ID>` exits 0
3. **State check:** `./lab status --json | jq '{state,active_fault}'` — state="DEGRADED", active_fault="\<ID\>"
4. **Validation check:** `./lab validate` exits 1 for all blocking faults; exits 0 for F-010 (degraded only)
5. **Failing checks:** actual failing check IDs match FaultDef.PostconditionSpec.FailingChecks
6. **Passing checks:** checks listed in PassingChecks all pass (invariant checks remain conformant)
7. **Reset:** `./lab reset` exits 0
8. **Post-reset:** `./lab validate` exits 0; state=CONFORMANT; active_fault=null
9. **History:** `./lab history` shows the apply and reset entries

**Special cases:**

| Fault | Special verification required |
|---|---|
| F-004 | E-001 passes AND E-002 fails simultaneously (health/ready split) |
| F-007 | Both localhost and app.local proxy fail (upstream block change affects all server blocks) |
| F-008 | `lab validate` exits 0 while active; `time sudo systemctl stop app` takes ~90s |
| F-010 | `lab validate` exits 0 (degraded only: log file deleted but process continues with held fd) |
| B-001 | Not a fault; `time curl http://localhost/slow` returns 504 in ~3s; `time curl 127.0.0.1:8080/slow` returns 200 in ~5s (see fault-matrix-runbook.md B-001 section) |
| B-002 | Not a fault; `openssl x509` shows self-signed (Subject = Issuer); E-005 passes with `-k` |
| F-014 | `lab validate` exits 0 while active; `ps aux \| grep Z` shows zombies accumulating |

**Cgroup enforcement prerequisite — verify before running F-008 or F-014:**

```bash
# Verify cgroup v2 is active
stat -f -c %T /sys/fs/cgroup
# must print: cgroup2fs

# Verify MemoryMax is enforced on the app slice
systemctl show app.slice --property=MemoryMax
# must print: MemoryMax=268435456  (256 MiB)

# Verify swap is disabled (swap allows OOM to avoid killing the process)
swapon --show
# must print nothing (no swap active)
# If swap is active: sudo swapoff -a
```

If any of these checks fail, the OOM chaos mode will silently hang rather than kill the process, producing a false negative. Resolve before proceeding.

**For F-008 (manual, non-reversible):**
1. `./lab fault apply F-008 --yes` exits 0
2. `./lab validate` exits 0 (no failing checks while running)
3. `sudo timeout 5 systemctl stop app.service || echo timeout` → must print "timeout"
4. Recover: rebuild binary, `sudo systemctl start app.service`, `./lab reset`, `./lab validate`

**For F-014 (manual, non-reversible):**
1. `./lab fault apply F-014 --yes` exits 0
2. `./lab validate` exits 0 initially
3. `curl http://localhost/` several times; `ps aux | grep -c Z` increases each time
4. Recover: rebuild binary, `sudo systemctl restart app.service`, `./lab reset`, `./lab validate`

### B.2.1 — Scope decision: F-019, F-020, F-021

The Application Runtime Contract v1.0.0 introduced three additional faults not currently in `internal/catalog/faults.go`:

| Fault | Description | Status |
|---|---|---|
| F-019 | Fill `/var/lib/app` loopback volume (block exhaustion) | **Not implemented** — disk-full behavior is tested manually in C.5; add to catalog before declaring complete if desired |
| F-020 | Set `CHAOS_LATENCY_MS=400` in `chaos.env` and restart | **Not implemented** — chaos injection is tested in Section 11 of DEVELOPER-QUICKSTART.md; add to catalog for automated fault-matrix coverage |
| F-021 | Add nftables rule to `LAB-FAULT` chain (network partition) | **Not implemented** — nftables chain existence verified in B.5; partition behavior requires adding this fault |

**Decision required before Phase D:**

These three faults must be either: (a) added to `internal/catalog/faults.go` with full FaultDef and FaultImpl, included in the fault matrix run, and covered by `TestAllImpls_Has16Faults` (updated to 19), or (b) explicitly documented as out-of-scope for this release with a note in `docs/extension-boundary-note.md`.

**If added**, the following tests must be updated: `TestAllImpls_Has16Faults`, `TestAllDefs_Has16Defs`, `TestFaultIDs_SequentialWithGap` (or replaced with a range test), `TestInvariant_16FaultsInCatalog`, and `run-fault-matrix.sh` REVERSIBLE list.

The disk-full test in C.5 is a prerequisite for F-019 regardless of whether it becomes an official catalog fault.

---

Proves that `lab validate` is observation-only and never updates the authoritative state classification.

**Test sequence — run in order:**

1. Verify initial state: `./lab status --json | jq .state` = "CONFORMANT"
2. Run validate: `./lab validate > /dev/null`
3. Verify state unchanged: `cat /var/lib/lab/state.json | jq .state` = "CONFORMANT"
4. Manually break: `sudo chmod 000 /var/lib/app`
5. Run validate: `./lab validate`; must exit 1 (E-002, F-004 fail)
6. Verify state.json unchanged: `cat /var/lib/lab/state.json | jq .state` = **still "CONFORMANT"** — validate did not update it
7. Run status: `./lab status --json | jq .state` = "BROKEN" — status reconciles
8. Verify state.json updated: `cat /var/lib/lab/state.json | jq .state` = "BROKEN"
9. Run validate again: `./lab validate`; must exit 1 (same failures)
10. Verify state.json still BROKEN: validate did not change it even on second run
11. Restore: `sudo chmod 755 /var/lib/app; ./lab reset`
12. Verify CONFORMANT restored: `./lab validate` exits 0

**Also verify the reverse direction:**
- State.json says CONFORMANT; runtime is healthy → `./lab status` = CONFORMANT (no change)
- State.json says BROKEN; runtime is healthy (after manual fix without reset) → `./lab status` = CONFORMANT (reconciliation: runtime truth wins)

### B.4 — Shell conformance suite validation

Verify the shell-level conformance suite (`scripts/validate.sh`) produces correct results and matches the Go CLI output.

**Prerequisite — source-level sync check:** `validate.sh` implements each check's observable command as a bash one-liner. These commands were derived from the `ObservableCommand` field in `internal/conformance/catalog.go`. If a check's observable command is modified in `catalog.go`, `validate.sh` must be updated to match. Before running B.4, verify sync:

```bash
# For each of the 23 check IDs, confirm the bash command in validate.sh
# produces the same pass/fail result as the Go check on the live system.
# If they diverge for any check, update validate.sh to match catalog.go.
grep 'ObservableCommand' /opt/lab-env/internal/conformance/catalog.go | sort
# Compare against each check's bash command in validate.sh
```

**Runtime verification:**

1. `sudo bash /opt/lab-env/scripts/validate.sh` on clean system → exits 0, prints CONFORMANT
2. Apply F-004: `./lab fault apply F-004`
3. `sudo bash /opt/lab-env/scripts/validate.sh` → exits 1 (E-002, F-004 fail), prints NON-CONFORMANT
4. `./lab validate` → exits 1 with same failing checks
5. Both tools must agree on exactly which checks fail — any divergence means validate.sh is out of sync with catalog.go
6. `./lab reset`
7. Both tools exit 0

Repeat steps 2–7 for at least three different faults to exercise S-series, P-series, E-series, F-series, and L-series checks across both tools.

### B.5 — Service signal contract verification

Verify the service's signal files on the real system:

1. `cat /run/app/status` = "Running"
2. `cat /run/app/app.pid` = the PID of the running server process (matches `pgrep -u appuser server`)
3. `ls /run/app/healthy` — file exists
4. `ls /run/app/loading` — file absent (initialization complete)
5. Verify all 12 telemetry fields by name:
   ```bash
   cat /run/app/telemetry.json | jq 'keys'
   # must contain exactly:
   # ["chaos_active","chaos_modes","cpu_percent","disk_usage_percent",
   #  "errors_total","inode_usage_percent","memory_rss_mb","open_fds",
   #  "pid","requests_total","ts","uptime_seconds"]
   cat /run/app/telemetry.json | jq '.memory_rss_mb > 1.0'  # must be true
   cat /run/app/telemetry.json | jq '.pid > 0'               # must be true
   ```
6. `sudo systemctl stop app.service; sleep 0.5; ls /run/app/` — signal files absent (cleaned up)
7. `sudo systemctl start app.service; sleep 2; ls /run/app/` — signal files restored

### B.6 — Logrotate interaction

Verifies the O_APPEND guarantee holds under real logrotate operation.

**Preconditions — verify before running:**
```bash
# logrotate is installed
which logrotate

# Configuration is in place
ls -la /etc/logrotate.d/app

# Logrotate timer is active (so rotation actually happens on schedule)
systemctl is-active logrotate.timer || systemctl is-active cron

# Log directory has correct permissions
stat -c '%U:%G %a' /var/log/app
# must print: appuser:appuser 755
```

**Test sequence:**

1. Write several log lines: `for i in 1 2 3; do curl http://localhost/ > /dev/null; done`
2. Capture pre-rotation last line: `tail -1 /var/log/app/app.log`
3. Force rotation: `sudo logrotate -f /etc/logrotate.d/app`
4. Write another log line: `curl http://localhost/ > /dev/null`
5. Verify current log: `xxd /var/log/app/app.log | head -2` — must show `{` as first byte (no null bytes)
6. Verify last line is valid JSON: `tail -1 /var/log/app/app.log | jq .`
7. Verify `.last_rotate` touched: `ls -la /var/log/app/.last_rotate` — mtime updated
8. Verify `.last_rotate` mode: `stat -c %a /var/log/app/.last_rotate` = 644

---

## Phase C — Invariant stress testing

**Purpose:** break assumptions under real OS scheduling, concurrent operations, and adversarial input sequences. This is not load testing. Every scenario tests a specific named architectural guarantee.

**Precondition:** Phase B complete; all 16 faults verified; environment is CONFORMANT.

**Exit criterion:** all invariant checks hold under every adversarial sequence. No state where DEGRADED and active_fault=null (invariant I-2). No state.json parse errors. No audit log gaps.

### C.1 — State file concurrency

`lab status` writes state.json without the mutation lock. Concurrent mutation commands also write. The atomic write (temp + rename) prevents torn writes but not read-modify-write races.

```bash
# Run status in a tight loop while fault apply executes
./lab fault apply F-004 &
for i in $(seq 1 100); do ./lab status > /dev/null 2>&1; done
wait

# Final state must be consistent (invariant I-2)
./lab status --json | jq '{state, active_fault, classification_valid}'
# If state=DEGRADED, active_fault must be non-null
# If active_fault=null, state must not be DEGRADED
```

**Pass criterion:** `state` and `active_fault` are consistent. No jq parse errors (state.json was never corrupt during the race). Run this sequence 5 times.

**If this fails:** the fix is to have `lab status` acquire the mutation lock before writing state.json, or to use a separate read-advisory lock.

### C.2 — Fault atomicity under real syscall failures

Unit tests mock the executor. On the real system, `chmod` can succeed while `systemctl restart` fails. The multi-step Apply sequences are logically atomic but not physically atomic.

For each multi-step fault (F-002, F-006, F-007, F-013, F-015, F-016, F-017):

1. Record initial state: `./lab status --json > before.json`
2. Remove sudo permissions mid-sequence to force a failure:
   ```bash
   # Temporarily revoke sudo for systemctl to simulate mid-sequence failure
   sudo chmod 000 /etc/sudoers.d/lab-appuser
   ./lab fault apply <ID> ; RESULT=$?
   sudo chmod 0440 /etc/sudoers.d/lab-appuser
   ```
3. Check state: `./lab status --json | jq '{state, active_fault}'`
4. **Pass criterion:** if Apply failed (RESULT≠0), then active_fault must be null. No state where state=DEGRADED and active_fault=null.
5. Restore: `./lab reset` (may require sudo restoration first)

### C.3 — Rapid transition sequences

```bash
# Sequence 1: rapid apply → reset cycles (10 iterations)
for i in $(seq 1 10); do
    ./lab fault apply F-004
    ./lab reset
    ./lab validate || { echo "FAIL at iteration $i"; break; }
    echo "Iteration $i: OK"
done

# Sequence 2: validate during reset
./lab fault apply F-001
./lab reset &
RESET_PID=$!
./lab validate    # may exit 0 or 1 depending on timing; must not crash
wait $RESET_PID
./lab status --json | jq .state   # must be CONFORMANT after reset completes

# Sequence 3: status during fault apply
./lab fault apply F-004 &
APPLY_PID=$!
./lab status      # must not crash; may return CONFORMANT or DEGRADED
wait $APPLY_PID
./lab status --json | jq .state   # must be DEGRADED after apply completes

# Sequence 4: repeated interrupt signals during reset
./lab fault apply F-001
for i in $(seq 1 5); do
    ./lab reset &
    RPID=$!
    sleep 0.1
    kill -SIGINT $RPID 2>/dev/null
    wait $RPID
    ./lab status > /dev/null   # must not crash
done
./lab reset   # clean recovery
./lab validate
```

**Pass criterion:** no crash, no unrecoverable state, `./lab validate` exits 0 after final reset.

### C.4 — Recovery playbook drills

Execute all 9 drills from `docs/recovery-playbook.md` in the order they appear in that document. Use the pass criteria and 7-point verification checklist defined there verbatim. The drills are listed below for reference — consult `recovery-playbook.md` for the exact setup, execution, and verification steps for each:

1. Corrupt state.json while mutation lock is held
2. Missing state.json — control plane starts fresh
3. Stale lock with dead PID
4. Interrupted fault apply — partial mutation
5. Interrupted reset mid-tier
6. Audit log present but state.json missing
7. Partial R2 restore (one canonical file missing)
8. R3 recovery from non-reversible fault (F-008)
9. R3 recovery from non-reversible fault (F-014)

**Pass criterion:** all 9 drills pass the 7-point checklist as defined in `recovery-playbook.md`. The drill numbers above match the document order.

### C.5 — Disk full on /var/lib/app

Verify behavior when the 50 MiB loopback mount fills:

```bash
# Fill the mount (fault F-019 if implemented, or manual)
dd if=/dev/zero of=/var/lib/app/fill bs=1M count=50 2>/dev/null || true

# Verify service behavior
curl http://localhost/health   # must return 200 (health exempt from state dir)
curl http://localhost/         # must return 500 (state write fails)
cat /run/app/status            # must be "Unhealthy"
cat /run/app/telemetry.json | jq .disk_usage_percent  # must be near 100

# Verify control plane detects it
./lab validate   # E-002 must fail; E-001 must pass
./lab status --json | jq .state   # must be DEGRADED or BROKEN

# Restore
rm /var/lib/app/fill
./lab reset
./lab validate
```

### C.6 — Concurrent validate + status + fault apply

```bash
# Three operations running simultaneously
./lab fault apply F-004 &
./lab validate &
./lab status &
wait

# All must complete without crash
# Final state must be consistent
./lab status --json | jq '{state, active_fault}'
./lab reset
./lab validate
```

---

## Phase D — Output and schema freezing

**Precondition:** H-001 is fixed and `TestRenderStatus_JSON_EndpointCodesNotGuessed` passes. Do not run Phase D before H-001 is fixed — incorrect values would be baked into golden fixtures permanently.

**Exit criterion:** `go test ./internal/output/...` fully green with stable golden fixtures; `git diff testdata/golden/` is empty after a fresh `UPDATE_GOLDEN=1` run; all determinism checks pass.

### D.1 — H-001 fix and golden fixture expansion

```bash
# Verify fix is in place
go test ./internal/output/... -run TestRenderStatus_JSON_EndpointCodesNotGuessed

# Regenerate all golden fixtures
UPDATE_GOLDEN=1 go test ./internal/output/...

# Verify no unexpected changes (should only see endpoint code fields updated)
git diff testdata/golden/
```

Expand golden fixture coverage to include scenarios not yet frozen:

| Scenario | Fixture name | Key fields to verify |
|---|---|---|
| Status: CONFORMANT | `status_conformant.json` | state, active_fault=null, classification_valid, endpoint codes (not guessed) |
| Status: DEGRADED with F-004 | `status_degraded.json` | state=DEGRADED, active_fault=F-004 |
| Status: BROKEN | `status_broken.json` | state=BROKEN, active_fault=null |
| Status: UNKNOWN | `status_unknown.json` | state=UNKNOWN, classification_valid=false |
| Validate: all-pass | `validate_conformant.json` | 23 results, all passed=true |
| Validate: blocking failure | `validate_blocking_fail.json` | at least one result with passed=false, blocking=true |
| Validate: degraded-only failure | `validate_degraded_fail.json` | exit code 0 despite failing checks |
| Fault apply: success | `fault_apply_success.json` | state=DEGRADED, audit entry |
| Fault apply: precondition rejected | `fault_apply_rejected.json` | error message, no state change |
| Fault apply: apply failed | `fault_apply_failed.json` | error, active_fault=null (atomicity) |
| Fault apply: baseline rejected | `fault_apply_baseline.json` | specific baseline rejection error |
| Reset: success R2 | `reset_success.json` | state=CONFORMANT |
| History: populated | `history_populated.json` | ring buffer entries present |
| Fault info: F-004 | `fault_info_f004.json` | all FaultDef fields |

### D.2 — Schema drift lock

Every JSON output type must have a no-extra-fields test. Currently covered by existing tests:
- `TestGolden_StatusResult_NoExtraFields`
- `TestGolden_ValidateResult_NoExtraFields`
- `TestGolden_FaultApplyResult_NoExtraFields`

**Required additions — write these tests before Phase D is complete:**

For each uncovered output type, add a test in `internal/output/golden_test.go` following the pattern of the existing no-extra-fields tests. The test marshals a populated result struct to JSON, unmarshals to `map[string]interface{}`, and asserts the key set exactly matches the expected set.

| Output type | Test name to add | Expected top-level keys |
|---|---|---|
| `FaultListResult` | `TestGolden_FaultListResult_NoExtraFields` | `faults` (array) |
| `FaultInfoResult` | `TestGolden_FaultInfoResult_NoExtraFields` | `id`, `layer`, `domain`, `reversible`, `reset_tier`, `requires_confirmation`, `is_baseline`, `preconditions`, `postcondition` |
| `ResetResult` | `TestGolden_ResetResult_NoExtraFields` | `tier`, `state`, `checks_passed`, `checks_failed` |
| `HistoryResult` | `TestGolden_HistoryResult_NoExtraFields` | `entries` (array), each entry: `state`, `ts`, `trigger` |

After adding these tests, run `go test ./internal/output/...` to confirm they pass. If any test fails, either add the unexpected field to `golden-baseline-ledger.md` as a frozen field, or remove it from the output struct.

### D.3 — Output determinism

**Map iteration order:** verify `StatusResult.Services` and `StatusResult.Endpoints` produce stable JSON key ordering across repeated invocations. The renderer must sort map keys or normalize before comparing.

```bash
# Run status 10 times and verify JSON is identical
for i in $(seq 1 10); do ./lab status --json; done | sort -u | wc -l
# Must print 1 (all outputs identical, modulo timestamps)
```

**Timestamp normalization:** verify that the `normalizeJSON` function in `golden_test.go` correctly excludes all timestamp fields (`ts`, `last_validate`, `applied_at`) from golden comparisons. Any timestamp appearing in a fixture must be replaced with a sentinel value.

**Null vs absent:** verify `active_fault` is always present as `null` (not omitted) when no fault is active. `TestGolden_ActiveFault_NullNotAbsent` covers this; verify it still passes after golden expansion.

---

## "Done" definition

The system is complete when every item below is true simultaneously.

**Unit test suite:**
- `go test ./...` (control plane) — fully green, no unexpected failures or skips
- `go test ./...` (service module) — fully green, no unexpected failures or skips
- Do **not** run with `-short`; `TestHandleSlow_Returns200_After5Seconds` is skipped under `-short` and must pass in full mode
- `go test -race ./...` — no race conditions detected. Run on the unit suite only; do not set `CHAOS_OOM_TRIGGER=1` in the environment during race-detector runs as the OOM goroutine will crash the test binary

**Expected skips (these are not failures; document them explicitly):**

| Test | File | Skip condition | Resolution path |
|---|---|---|---|
| `TestRestoreFile_ConfigYaml_OwnershipAndMode` | `internal/executor/restore_test.go` | Requires sudo; run with `LAB_TEST_MODE=live` | Promoted to `cmd/live_fault_matrix_test.go` restore verification in Phase B.2 |
| `TestChaosHandler_Drop100_HealthIsExempted` | `service/chaos/chaos_edge_test.go` | Permanent skip — open design question | **Must be resolved before Done:** decide whether `/health` is exempt from drops. If yes, implement and remove skip. If no, delete the test. The skip must not remain in the suite at declaration of completeness. |
| `TestHandleSlow_Returns200_After5Seconds` | `service/server/server_test.go` | Skips under `-short` | Must pass without `-short`. Do not use `-short` in the Done verification run. |
| `TestCollector_PanicRecovery` | `service/telemetry/telemetry_edge_test.go` | Conditional skip if panic injection fails | Investigate if this skip fires in practice; if the injection logic is unreliable, rewrite using a hook function instead of a call counter. |
| `TestArchitecture_ServiceModule_DoesNotImportControlPlane` | `internal/invariants/architecture_test.go` | Skips if service/ directory not found | Run from repository root; `/opt/lab-env/service/` must exist |
| `TestFaultRecover_RestoresExactContent` (per-fault skipf) | `internal/catalog/content_integrity_test.go` | Skips individual faults where Apply errors without writes | Expected for baseline faults; not a gap |

**Architecture invariants:**
- `TestArchitecture_*` all pass — import boundaries enforced at source level
- Config grep audit produces empty output — no hardcoded constants outside `config.go`
- `TestEmbeddedFiles_*` all pass — embedded config templates are correct and non-empty

**Live system:**
- All 16 faults (or 19 if F-019/F-020/F-021 added) produce observed behavior matching `fault-matrix-runbook.md`
- All 9 drills in `recovery-playbook.md` pass the 7-point checklist
- Interrupt path produces exit 4, classification_valid=false, audit entry, and correct reclassification on real OS
- `scripts/validate.sh` agrees with `./lab validate` on every test scenario
- F-019/F-020/F-021 scope decision documented in `docs/extension-boundary-note.md`

**Output contract:**
- `TestRenderStatus_JSON_EndpointCodesNotGuessed` passes (H-001 resolved)
- All golden fixtures stable under `UPDATE_GOLDEN=1 go test ./internal/output/...`
- No-extra-fields tests for FaultListResult, FaultInfoResult, ResetResult, HistoryResult written and passing
- `TestChaosHandler_Drop100_HealthIsExempted` resolved (implemented or deleted; not a permanent skip)

**Behavioral properties — no exceptions:**
1. No layer interprets another layer's output
2. No command mutates outside its authority
3. No output is derived from guesses or heuristics
4. All state transitions are reproducible from inputs
5. Interrupts behave deterministically on the real OS
6. `lab validate` never updates the authoritative state classification
7. `lab status` is the only command that reconciles recorded state with observed runtime

---

## Highest-risk open items

| Rank | Risk | Phase | Mitigation |
|---|---|---|---|
| 1 | State file concurrent write race (lab status + mutation) | C.1 | Known design tension; test exposes it; fix requires status to acquire lock for writes |
| 2 | Partial mutation under real syscall failures | C.2 | Logical atomicity is not physical atomicity; each multi-step fault must be individually tested |
| 3 | Interrupt path timing (interrupt arrives after reset completes) | B.1 | Use slower fault for reset; verify PID still alive before sending signal |
| 4 | H-001 blocks golden fixture expansion | Phase D precondition | Fix before Phase D; regression guard exists |
| 5 | `cmd/live_interrupt_test.go` and `cmd/live_fault_matrix_test.go` not yet written | Phase 0 | Must be created before Phase 0 is complete; content specified in B.1 and B.2 |
| 6 | `TestChaosHandler_Drop100_HealthIsExempted` permanent skip — design question unresolved | Phase A Done criterion | Decide and implement before declaring complete |
| 7 | F-019/F-020/F-021 scope undecided | B.2.1 | Decision must be documented in extension-boundary-note.md before Phase D |
| 8 | Map iteration non-determinism in status/validate JSON output | D.3 | Round-trip normalization mitigates; verify explicitly with repeated runs |
| 9 | OOM enforcement silently fails without cgroup v2 + no swap | B.2 | Cgroup precondition check gates F-008/F-014 testing |
| 10 | Config drift reintroduction in future fault additions | Ongoing | Architecture tests + grep audit; run before any new fault is added |