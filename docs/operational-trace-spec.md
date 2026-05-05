# Operational Trace Spec
## Version 1.0.0

> Ordered event sequences only. Every event is externally observable
> in audit.log, state.json, or exit code. No implementation names.

**Notation:**
```
[lock]   advisory lock acquired at /var/lib/lab/lab.lock
[read]   state.json read
[obs]    system observation — no audit entry produced
[audit]  entry written to audit.log before next step begins
[mut]    system mutation — always preceded by [audit]
[write]  state.json written atomically (temp + rename)
[unlock] lock released
[exit N] process exits with code N
```

---

## lab status — state matches runtime (no reconciliation)

```
[obs]  ServiceActive("app.service")
[obs]  CheckProcess("server","appuser")
[obs]  CheckPort("127.0.0.1:8080")
[obs]  ServiceActive("nginx")
[obs]  CheckEndpoint("http://localhost/health")
       → detected: CONFORMANT  matches recorded: CONFORMANT
[write] state.json: last_status_at updated only
[exit 0]
```

No audit entry. No state change.

---

## lab status — reconciliation (recorded ≠ runtime)

Example: recorded BROKEN, runtime healthy.

```
[obs]  ServiceActive("app.service")   → true
[obs]  CheckProcess, CheckPort, CheckEndpoint  → all healthy
       → detected: CONFORMANT  recorded: BROKEN  — mismatch
[audit] entry_type: reconciliation  from: BROKEN  to: CONFORMANT
[write] state.json: state=CONFORMANT, classification_valid=true, last_status_at updated
[exit 0]
```

---

## lab status — classification_valid=false (post-interrupt recovery)

```
       → classification_valid=false: cached state untrusted, runtime takes over
[obs]  ServiceActive, CheckProcess, CheckPort, CheckEndpoint
       → detected from runtime (ignores cached state)
[audit] entry_type: reconciliation  from: (prior cached)  to: (detected)
[write] state.json: classification_valid=true, state=(detected)
[exit 0]
```

---

## lab validate — all blocking checks pass

```
       ← no lock acquired
[obs]  S-001, S-002, S-003, S-004
[obs]  P-001, P-002, P-003, P-004
[obs]  E-001, E-002, E-003, E-004, E-005
[obs]  F-001, F-002, F-003, F-004, F-005, F-006, F-007
[obs]  L-001, L-002, L-003
       → classification: CONFORMANT
[audit] entry_type: validation_run  passed:23  total:23  failing:[]
[write] state.json: last_validate updated  ← state field NOT updated
[exit 0]
```

`state` field in state.json is never written by validate.

---

## lab validate — blocking check fails (S-001 down, E-series dependent)

```
       ← no lock acquired
[obs]  S-001  → FAIL
[obs]  S-002, S-003, S-004  (continue — no early abort)
[obs]  P-001, P-002, P-003, P-004
[obs]  E-001 → FAIL (marked dependent: caused by S-001)
[obs]  E-002, E-003, E-004, E-005 → FAIL (all dependent)
[obs]  F-001..F-007, L-001..L-003  (independent — still run)
       → classification: NON-CONFORMANT  failing: [S-001]  (E-series excluded as dependent)
[audit] entry_type: validation_run  passed:N  total:23  failing:[S-001]
[write] state.json: last_validate updated  ← state field NOT updated
[exit 1]
```

---

## lab fault apply — success (example: F-004)

```
       → check: F-004 exists in catalog
       → check: F-004.IsBaselineBehavior=false
[lock] acquire /var/lib/lab/lab.lock
[read] state.json  ← TOCTOU re-read after lock
       → check: state=CONFORMANT
       → check: active_fault=nil
       → check: F-004 fault-specific preconditions
       ← RequiresConfirmation=false: no prompt
[audit] entry_type: executor_op  op: Chmod  args: /var/lib/app 0000
[mut]  Chmod("/var/lib/app", 0000)
[audit] entry_type: state_transition  from: CONFORMANT  to: DEGRADED  fault: F-004
[write] state.json: state=DEGRADED, active_fault={id:F-004, applied_at:...}
[unlock]
[exit 0]
```

If [mut] fails: [write] does not execute. state.json unchanged. Exit 1.

---

## lab fault apply — precondition rejected (state not CONFORMANT)

```
       → check: fault exists
[lock] acquire
[read] state.json  → state=BROKEN
       → check: state=CONFORMANT  FAIL
[audit] entry_type: error  op: ErrPreconditionNotMet
[unlock]
[exit 3]
```

No mutation. No state change.

---

## lab reset — R2 from DEGRADED (reversible fault active, example: F-004)

```
[lock] acquire
[read] state.json  → state=DEGRADED, active_fault=F-004
       ← F-004.IsReversible=true: call Recover first
[audit] entry_type: executor_op  op: Chmod  args: /var/lib/app 0755
[mut]  Recover: Chmod("/var/lib/app", 0755)
       ← R2 tier:
[audit] entry_type: executor_op  op: RestoreFile  args: /etc/app/config.yaml
[mut]  RestoreFile("/etc/app/config.yaml")
[audit] entry_type: executor_op  op: RestoreFile  args: /etc/systemd/system/app.service
[mut]  RestoreFile("/etc/systemd/system/app.service")
[audit] entry_type: executor_op  op: RestoreFile  args: /etc/nginx/sites-enabled/app
[mut]  RestoreFile("/etc/nginx/sites-enabled/app")
[audit] entry_type: executor_op  op: Chmod  args: /opt/app/server 0750
[mut]  Chmod(BinaryPath, 0750)
[audit] entry_type: executor_op  op: Systemctl  args: daemon-reload
[mut]  Systemctl("daemon-reload","")
[audit] entry_type: executor_op  op: Systemctl  args: restart app.service
[mut]  Systemctl("restart","app.service")
[audit] entry_type: executor_op  op: NginxReload
[mut]  NginxReload()
       ← post-reset validation: full 23-check suite
[obs]  S-001..L-003
       → classification: CONFORMANT
[audit] entry_type: validation_run  passed:23  total:23
[audit] entry_type: state_transition  from: DEGRADED  to: CONFORMANT  fault_cleared: F-004
[write] state.json: state=CONFORMANT, active_fault=null, last_reset updated
[unlock]
[exit 0]
```

---

## lab reset — post-validation fails (stays BROKEN)

```
[lock]
[read] state.json
[mut]  ... tier operations ...
[obs]  S-001..L-003  → some blocking checks fail
[audit] entry_type: validation_run  passed:N  total:23  failing:[...]
[audit] entry_type: state_transition  from: (prior)  to: BROKEN
[write] state.json: state=BROKEN, active_fault=null
[unlock]
[exit 1]
```

---

## lab provision

```
[lock] acquire
[audit] entry_type: executor_op  op: RunMutation  args: bash /opt/lab-env/bootstrap.sh
[mut]  RunMutation("bash","/opt/lab-env/bootstrap.sh")
[obs]  S-001..L-003  ← post-provision validation
[audit] entry_type: validation_run
[audit] entry_type: state_transition  from: UNPROVISIONED  to: CONFORMANT (or BROKEN)
[write] state.json: state=CONFORMANT (or BROKEN), last_provision updated
[unlock]
[exit 0 or 1]
```

---

## Interrupt during mutation (signal mid-reset)

```
[lock]
[mut]  ... some operations complete ...
       ← SIGINT received
       ← current operation allowed to complete (grace: 30s standard, 120s for provision/R3)
       ← no further operations started
[audit] entry_type: interrupt  op: (current op name)  grace_period_exceeded: false
       ← classification_valid set to false  ← NOT state=BROKEN
[write] state.json: classification_valid=false  (state field unchanged)
[unlock]
[exit 4]
```

After exit 4: `lab status` reads `classification_valid=false`, re-detects from runtime, writes `classification_valid=true`.

---

## Lock contention

```
       ← attempt lock acquisition
       ← lock file present, PID N is running
[audit] entry_type: error  op: ErrLockHeld  detail: PID N
[exit 1]
```

No state change. No mutations.

---

## Anomaly → Trace Reference

| Observed | Expected trace | Diagnostic |
|---|---|---|
| validate exits 0, status shows BROKEN | Normal — validate never writes state | Run `lab status` |
| fault apply exits 0, state still CONFORMANT | state.json write failed after Apply | `cat state.json \| jq .state`; check audit for ErrApplyFailed |
| status returns stale classification after crash | classification_valid not set false | `cat state.json \| jq .classification_valid` |
| reset exits 0, validate fails | Post-reset validation not running | Check audit for validation_run entry |
| audit entry missing for a mutation | RunCommand used instead of RunMutation | `grep "op" audit.log \| tail -20` |