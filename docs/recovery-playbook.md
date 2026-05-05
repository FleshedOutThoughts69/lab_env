# Recovery Playbook
## Version 1.0.0

> One drill per hostile scenario. Each drill: induction commands, immediate
> observable artifact, expected `lab status`, expected audit evidence,
> recovery commands, pass criterion, failure definition.

---

## Drill 1: Corrupt state.json

**Induce:**
```bash
echo '{"state":"INVALID_JSON' | sudo tee /var/lib/lab/state.json
```

**Immediate artifact:**
```bash
cat /var/lib/lab/state.json    # invalid JSON
```

**Expected `lab status`:**
```
State: UNKNOWN
Exit code: 5
```

**Expected audit evidence:** no new audit entries (status is read-only).

**Recovery:**
```bash
lab status                     # re-detects from runtime; rewrites state.json
lab validate                   # populates last_validate
```

**Pass criterion:** `lab status` exits 0; `cat /var/lib/lab/state.json | jq .classification_valid` returns `true`.

**Failure:** `lab status` panics or produces no output.

---

## Drill 2: Missing state.json

**Induce:**
```bash
sudo rm /var/lib/lab/state.json
```

**Immediate artifact:**
```bash
ls /var/lib/lab/state.json     # no such file
```

**Expected `lab status`:** produces a result (CONFORMANT from runtime, or UNKNOWN). Must not crash.

**Expected audit evidence:** none until recovery.

**Recovery:**
```bash
lab status                     # detects from runtime; creates state.json
```

**Pass criterion:** `state.json` exists with valid content after `lab status`; second call returns same state.

**Failure:** `lab status` panics or writes invalid JSON.

---

## Drill 3: Stale lock (dead PID)

**Induce:**
```bash
echo "99999999" | sudo tee /var/lib/lab/lab.lock
sudo chmod 600 /var/lib/lab/lab.lock
```

**Immediate artifact:**
```bash
cat /var/lib/lab/lab.lock      # 99999999
```

**Expected `lab status`:** runs normally (status does not acquire lock). Exit 0.

**Expected `lab fault apply F-004`:** stale lock detected, reclaimed, fault applies successfully. Exit 0.

**Expected audit evidence:** normal executor_op and state_transition entries for the fault apply.

**Recovery:** automatic — stale lock reclaimed by the first mutating command.

**Pass criterion:** `lab fault apply F-004` succeeds; `lab.lock` is absent after command completes.

**Failure:** `lab fault apply` exits 1 with ErrLockHeld for PID 99999999 (dead process).

---

## Drill 4: Live lock contention

**Induce:**
```bash
lab reset --tier R3 &          # slow operation
sleep 0.5
lab fault apply F-004          # second operation
```

**Immediate artifact (second command):**
```bash
# exits immediately with:
# Error: another lab operation is in progress (PID <N>)
# Exit code: 1
```

**Expected audit evidence:** `entry_type: error, op: ErrLockHeld` in audit.log from second command.

**Recovery:** wait for first command to complete.

**Pass criterion:** second command exits immediately (not waiting); exit code 1; first command completes without interference.

**Failure:** second command waits indefinitely, or second command corrupts first command's operation.

---

## Drill 5: Interrupted fault apply (signal after mutation, before state write)

**Induce:**
```bash
lab fault apply F-001 &
APPLY_PID=$!
sleep 0.1
kill -SIGINT $APPLY_PID
wait $APPLY_PID
echo "Exit: $?"
```

**Immediate artifact:**
```bash
cat /var/lib/lab/state.json | jq '{state, classification_valid, active_fault}'
grep '"entry_type":"interrupt"' /var/lib/lab/audit.log
```

**Expected:** exit 4 (if signal arrived after Apply); `classification_valid: false`; interrupt entry in audit.

**Expected `lab status`:** re-detects from runtime. If runtime is healthy (fault may not have completed): CONFORMANT. If mutation completed: depends on which files changed.

**Recovery:**
```bash
lab status                     # reclassifies
lab reset --tier R2            # if needed
lab validate                   # confirm
```

**Pass criterion:** `lab status` exits 0; `classification_valid` is restored to `true`; `lab validate` exits 0 after recovery.

**Failure:** `state.json` shows DEGRADED with no active fault recorded (invariant I-2 broken), or `lab status` returns UNKNOWN on a healthy system.

---

## Drill 6: Interrupted reset (signal mid-tier)

**Induce:**
```bash
lab fault apply F-004
lab reset &
RESET_PID=$!
sleep 0.2
kill -SIGINT $RESET_PID
wait $RESET_PID
echo "Exit: $?"
```

**Immediate artifact:**
```bash
cat /var/lib/lab/state.json | jq '.classification_valid'   # false
grep '"entry_type":"interrupt"' /var/lib/lab/audit.log     # present
```

**Expected:** exit 4; `classification_valid: false`; system partially reset (some files may be restored, some not).

**Recovery:**
```bash
lab status
lab reset --tier R2            # idempotent — safe to re-run
lab validate
```

**Pass criterion:** `lab reset --tier R2` succeeds on second attempt; `lab validate` exits 0.

**Failure:** `lab reset` on second attempt fails due to state left by interrupted first run (reset is not idempotent).

---

## Drill 7: Partial mutation, no state commit (simulates Apply crash after chmod)

**Induce:**
```bash
sudo chmod 000 /var/lib/app    # apply F-004's mutation manually
# state.json still shows CONFORMANT — no fault recorded
```

**Immediate artifact:**
```bash
cat /var/lib/lab/state.json | jq '.state'         # CONFORMANT
cat /var/lib/lab/state.json | jq '.active_fault'  # null
curl localhost/                                    # 500
```

**Expected `lab status`:**
```
State: BROKEN
(reconciled: CONFORMANT → BROKEN)
```
Conflict resolution case 2 (system-state-model §4.3): suite fails, state records CONFORMANT → BROKEN.

**Expected audit evidence:**
```bash
grep '"entry_type":"reconciliation"' /var/lib/lab/audit.log
# from: CONFORMANT  to: BROKEN
```

**Recovery:**
```bash
lab status                     # writes BROKEN
lab reset --tier R2
lab validate
```

**Pass criterion:** `lab status` detects BROKEN without manual intervention; reconciliation audit entry present; `lab validate` exits 0 after reset.

**Failure:** `lab status` reports CONFORMANT despite E-002 failing.

---

## Drill 8: Audit log present, state.json absent

**Induce:**
```bash
# After running several faults:
sudo rm /var/lib/lab/state.json
```

**Immediate artifact:**
```bash
ls /var/lib/lab/state.json     # absent
wc -l /var/lib/lab/audit.log   # note line count
```

**Expected `lab status`:** detects from runtime; recreates state.json. Exit 0 or 5.

**Recovery:**
```bash
lab status
```

**Pass criterion:** `wc -l /var/lib/lab/audit.log` after recovery shows count ≥ before (appended, not reset); `state.json` recreated with valid content.

**Failure:** audit log truncated or overwritten.

---

## Drill 9: R3 reset on non-reversible fault (F-008)

**Induce:**
```bash
lab fault apply F-008 --yes
lab status                     # DEGRADED
```

**Expected `lab reset --tier R3`:**
1. Recover() called on F-008 → returns error (expected — logged, not fatal)
2. R3 operations execute (bootstrap script)
3. Post-reset validation runs
4. Exit 0

**Immediate artifact:**
```bash
lab reset --tier R3
lab validate
ls -la /opt/app/server         # new binary (modified time updated)
```

**Pass criterion:** `lab reset --tier R3` exits 0; `lab validate` exits 0; binary modification time is newer than fault application time.

**Failure:** R3 exits non-zero due to Recover() failure on F-008 (Recover error must not block R3).

---

## Recovery Verification Checklist

Run after any drill:

```bash
lab status                                        # must exit 0 or 5
cat /var/lib/lab/state.json | jq '.classification_valid'  # must be true
cat /var/lib/lab/state.json | jq '.active_fault'          # must be null
test -f /var/lib/lab/audit.log && echo "audit present"
tail -1 /var/lib/lab/audit.log | jq .                     # last entry valid JSON
lab validate                                      # must exit 0
test ! -f /var/lib/lab/lab.lock && echo "no stale lock"
```

All seven checks must pass.