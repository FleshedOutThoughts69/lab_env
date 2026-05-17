# Golden Baseline Ledger
## Version 1.0.0

> Contract index only. Lists what is frozen, what is unfrozen, and why.
> Consult before modifying any output, schema, or fixture file.

---

## I. Frozen JSON Output Schemas

### StatusResult (`lab status --json`)

| Field | Type | Stable | Notes |
|---|---|---|---|
| `state` | string | ✓ | one of 6 state values |
| `active_fault` | object\|null | ✓ | **explicit null** when no fault — never absent |
| `active_fault.id` | string | ✓ | |
| `active_fault.applied_at` | RFC3339 | ✗ | timestamp — not frozen |
| `services` | object | ✓ | keys are service names |
| `services.<name>.active` | bool | ✓ | |
| `services.<name>.pid` | int | ✗ | not frozen |
| `ports` | array | ✓ | shape frozen, values not |
| `ports[].addr` | string | ✓ | |
| `ports[].owner` | string | ✓ | |
| `endpoints` | object | ✓ | keys are URLs |
| `endpoints.<url>` | int | ✓ | HTTP status code |
| `last_validate` | object\|null | ✓ | null when never run |
| `last_validate.passed` | int | ✓ | |
| `last_validate.total` | int | ✓ | |
| `last_validate.at` | RFC3339 | ✗ | not frozen |
| `last_reset` | object\|null | ✓ | null when never run |
| `last_reset.tier` | string | ✓ | |
| `last_reset.at` | RFC3339 | ✗ | not frozen |
| `reconciled` | bool | ✓ | omitted when false |
| `unknown` | bool | ✓ | omitted when false |

Fixtures: `testdata/golden/status_conformant.json`, `status_degraded.json`, `status_broken.json`

---

### ValidateResult (`lab validate --json`)

| Field | Type | Stable | Notes |
|---|---|---|---|
| `at` | RFC3339 | ✗ | not frozen |
| `checks` | array | ✓ | 23 entries |
| `checks[].id` | string | ✓ | check ID (S-NNN etc) |
| `checks[].assertion` | string | ✓ | |
| `checks[].passed` | bool | ✓ | |
| `checks[].severity` | string | ✓ | "blocking"\|"degraded" |
| `checks[].dependent` | bool | ✓ | omitted when false |
| `checks[].error` | string | ✓ | omitted when passed |
| `passed` | int | ✓ | |
| `total` | int | ✓ | always 23 |
| `classification` | string | ✓ | "CONFORMANT"\|"CONFORMANT (degraded)"\|"NON-CONFORMANT" |
| `failing_checks` | []string\|null | ✓ | explicit null when empty |

Fixture: `testdata/golden/validate_conformant.json`

---

### FaultApplyResult (`lab fault apply --json`)

| Field | Type | Stable |
|---|---|---|
| `fault_id` | string | ✓ |
| `applied` | bool | ✓ |
| `from_state` | string | ✓ |
| `to_state` | string | ✓ |
| `forced` | bool | ✓ |
| `aborted` | bool | ✓ (omitted when false) |
| `abort_reason` | string | ✓ (omitted when not aborted) |

Fixture: `testdata/golden/fault_apply_success.json`

---

### FaultInfoResult (`lab fault info --json`)

| Field | Type | Stable |
|---|---|---|
| `id` | string | ✓ |
| `layer` | string | ✓ |
| `domain` | []string | ✓ |
| `reset_tier` | string | ✓ |
| `requires_confirmation` | bool | ✓ |
| `is_reversible` | bool | ✓ |
| `mutation` | string | ✗ (prose) |
| `symptom` | string | ✗ (prose) |
| `authoritative_signal` | string | ✗ (prose) |
| `observable` | string | ✗ (prose) |
| `reset_action` | string | ✗ (prose) |

Fixture: `testdata/golden/fault_info_f004.json`
Note: prose fields (`mutation`, `symptom`, etc.) are present in the schema but their values are not frozen.

---

### AuditEntry (individual entry schema — the log as a whole is never frozen)

| Field | Type | Stable |
|---|---|---|
| `ts` | RFC3339+ms | ✗ |
| `entry_type` | string | ✓ — one of 6 values |
| `command` | string | ✓ |
| `fault_id` | string | ✓ (omitted when null) |
| `op` | string | ✓ (omitted when null) |
| `op_args` | string | ✗ (prose) |
| `exit_code` | int\|null | ✓ |
| `duration_ms` | int | ✗ |
| `error` | string | ✗ (prose, omitted on success) |

Frozen entry_type values: `executor_op`, `state_transition`, `validation_run`, `reconciliation`, `interrupt`, `error`

---

## II. Frozen Exit Codes

| Code | Meaning | Frozen |
|---|---|---|
| 0 | Success | ✓ |
| 1 | ExecutionFailed | ✓ |
| 2 | UsageError | ✓ |
| 3 | PreconditionNotMet | ✓ |
| 4 | InterruptedWithSideEffects | ✓ |
| 5 | ClassificationFailure | ✓ |

---

## III. Frozen Conformance Catalog

**Count:** 23 checks. Any addition requires spec + test update.

**Frozen check IDs:**
S-001, S-002, S-003, S-004, P-001, P-002, P-003, P-004,
E-001, E-002, E-003, E-004, E-005,
F-001, F-002, F-003, F-004, F-005, F-006, F-007,
L-001, L-002, L-003

**Frozen severity assignments:**

| Severity | Checks |
|---|---|
| degraded (exit 0 when failing) | F-006, L-001, L-002, L-003 |
| blocking (exit 1 when failing) | all others |

**Not frozen:** assertion text, observable commands, failure meaning prose.

---

## IV. Frozen Fault Catalog

**Count:** 16 faults. F-011 and F-012 are baseline network behaviours (fault-model.md §10) and are not in the fault catalog.

| ID | Reversible | Reset tier | Requires confirmation |
|---|---|---|---|
| F-001 | ✓ | R2 | no |
| F-002 | ✓ | R2 | no |
| F-003 | ✓ | R2 | no |
| F-004 | ✓ | R2 | no |
| F-005 | ✓ | R2 | no |
| F-006 | ✓ | R2 | no |
| F-007 | ✓ | R2 | no |
| F-008 | ✗ | R3 | **yes** |
| F-009 | ✓ | R2 | no |
| F-010 | ✓ | R1 | no |
| F-013 | ✓ | R2 | no |
| F-014 | ✗ | R3 | **yes** |
| F-015 | ✓ | R2 | no |
| F-016 | ✓ | R2 | no |
| F-017 | ✓ | R2 | no |
| F-018 | ✓ | R2 | no |

**Not frozen:** symptom prose, observable prose, mutation display prose.

**Note:** F-011 and F-012 previously appeared in this table as baseline entries. They have been reclassified as baseline network behaviours and removed from the fault catalog. See fault-model.md §10 (Appendix: Baseline Network Behaviours).

---

## V. Frozen State Machine

**Frozen:** 6 state names, valid transitions (system-state-model §3.3), invariants I-1 through I-4 (§5.2).

**State names:** UNPROVISIONED, PROVISIONED, CONFORMANT, DEGRADED, BROKEN, RECOVERING

**Not frozen:** state.json history ring buffer size (defined in control-plane-contract §6.1 as the single authority).

---

## VI. Explicitly Unfrozen

Never add assertions on these:

| Item | Reason |
|---|---|
| Timestamps (`ts`, `at`, `applied_at`) | Non-deterministic |
| `duration_ms` | Non-deterministic |
| `pid` values | Non-deterministic |
| `last_status_at` in state.json | Changes on every status call |
| Human-readable CLI output prose | Implementation-defined (control-plane-contract §2.3) |
| Terminal colors, column alignment | Cosmetic |
| Exact error message text | Semantic minimum frozen, full prose not |
| Audit log total line count | Grows with usage |
| Internal executor call ordering | Provided guarantees met; order is not frozen |
| History ring buffer size | Owned by control-plane-contract §6.1 |

---

## VII. Fixture Update Protocol

**Unexpected failure:** determine whether the change is intentional before running `UPDATE_GOLDEN=1`. If unintentional, fix the code — the test caught a regression.

**Intentional schema change:**
1. Update schema in `control-plane-contract.md`
2. Increment spec version if breaking
3. Run `UPDATE_GOLDEN=1 go test ./internal/output/...`
4. Update the relevant rows in this ledger
5. Verify `go test ./...` passes