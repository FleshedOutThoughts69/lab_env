# Conformance Model
## Lab Environment Semantic Model — Layer 1 of 3
## Version 1.0.0

> **Authority:** This document is the semantic root of the lab environment model system. All higher-order system meaning derives from conformance semantics. "State" is a classification of conformance outcomes. "Fault" is a controlled mutation of conformance outcomes. When this document conflicts with any other model document or the environment specification, this document is authoritative.
>
> **Audience:** implementer-primary. This document defines normative rules. Rationale is provided only where a rule would otherwise seem arbitrary.
>
> **Normative language:** MUST, MUST NOT, SHALL — mandatory. SHOULD — strongly preferred. MAY — permitted.

---

## §1 — Purpose

This document defines what it means for the lab environment to be correct. It specifies the invariants the environment must satisfy, the checks that verify those invariants, and the semantics of validation. Every other document in the model system uses this document's definitions as its foundation.

**The three semantic models and their relationship:**

```
conformance-model.md   ← semantic root (this document)
system-state-model.md  ← classification of conformance outcomes
fault-model.md         ← controlled mutation of conformance outcomes
```

No additional semantic model documents exist. Concepts not expressible within these three documents are subordinate concerns belonging in the environment specification or implementation.

---

## §2 — Conformance Layers

The environment can be correct or incorrect along three independent axes. These axes must be kept distinct because they fail independently and carry different authorities.

### 2.1 Behavioral Conformance — Semantic Authority

Behavioral conformance determines whether the system is semantically correct: whether it does what it is supposed to do. A system that is behaviorally conformant is correct by definition, regardless of its structural state.

**Behavioral checks verify:**
- Endpoints respond with the specified status codes and body schemas
- The application process is running as the correct user
- Network ports are bound and accepting connections
- The application handles requests according to its interface contract

**Authority:** behavioral conformance is semantic authority. If behavioral checks pass, the system is correct. Structural drift does not override a passing behavioral check.

### 2.2 Structural Conformance — Explanatory Authority

Structural conformance determines whether the system's configuration matches the canonical specification: whether files exist, own the correct ownership, and have the correct modes. A system can be structurally conformant while behaviorally non-conformant, and vice versa.

**Structural checks verify:**
- Filesystem paths exist with canonical ownership and mode bits
- Service units are enabled and in the expected configuration
- Configuration files are present and syntactically valid
- Certificates are present and not expired

**Authority:** structural conformance is explanatory authority. It answers *why* the system is or is not behaviorally correct. A structural failure explains the cause of a behavioral failure. A structural failure without a behavioral failure indicates the system has compensated for the structural drift.

### 2.3 Operational Conformance — Informational Authority

Operational conformance determines whether long-running maintenance conditions are satisfied: whether logs are rotating correctly, certificates have sufficient remaining validity, and log content indicates healthy operation.

**Operational checks verify:**
- Log files are non-empty and contain valid structured content
- Log files contain expected startup markers
- Certificate validity period has not expired

**Authority:** operational conformance is informational authority. Operational check failures produce a degraded-conformant classification (see §4.3), not a blocking failure. The system can be operationally non-conformant and still be classified as conformant for the purposes of fault injection.

### 2.4 Layer Interaction Rules

The following rules govern how conformance layers interact:

**Rule C-1:** Structural conformance NEVER overrides behavioral conformance outcomes. A passing behavioral check is semantically correct regardless of structural state.

**Rule C-2:** Behavioral conformance defines semantic correctness. Structural conformance defines explanatory correctness. Both are required for a complete conformance picture, but they answer different questions.

**Rule C-3:** Operational check failures are non-blocking. They produce a degraded-conformant classification but do not prevent fault injection.

**Rule C-4:** No check in any layer exists without a named failure meaning. A check that can fail without a defined interpretation violates model completeness (see §5).

---

## §3 — Check Catalog

### 3.1 Check Schema

Every check in the catalog conforms to this schema:

| Field | Type | Description |
|---|---|---|
| **ID** | string | Unique identifier in format `{CATEGORY}-{NNN}` |
| **Category** | enum | `S` (system state), `P` (process), `E` (endpoint), `F` (filesystem), `L` (log) |
| **Layer** | enum | `behavioral`, `structural`, `operational` |
| **Severity** | enum | `blocking` (failure → non-conformant), `degraded` (failure → degraded-conformant) |
| **Assertion** | string | The condition that must be true for the check to pass |
| **Failure meaning** | string | What a failing check means semantically — not the error message, the interpretation |
| **Observable command** | string | The exact command that tests the assertion |
| **Maps to** | []string | State(s), transition(s), or fault(s) this check is evidence for |

The `Maps to` field enforces the bidirectional completeness condition (§5): every check maps to at least one state, transition, or fault in the other model documents.

**Namespace note:** check IDs and fault IDs share the `F-NNN` prefix for the filesystem check series (F-001 through F-007) and the fault catalog (F-001 through F-018). These are distinct namespaces. In `Maps to` fields, identifiers of the form `F-NNN` always refer to **fault catalog entries** from `fault-model.md` §7, never to check IDs. Check IDs are always referenced by their full category-prefixed form (S-NNN, P-NNN, E-NNN, F-NNN, L-NNN) within check-to-check cross-references, which do not appear in this catalog.

### 3.2 System State Checks (S-series)

These checks verify that systemd service units are in the expected lifecycle state. They are structural checks with behavioral implications — a service that is not active cannot serve requests.

| ID | Layer | Severity | Assertion | Failure meaning | Observable command | Maps to |
|---|---|---|---|---|---|---|
| **S-001** | structural | blocking | `app.service` is active | App process is not running | `systemctl is-active app.service --quiet` | BROKEN, F-001, F-003, F-005, F-006, F-009, F-013, F-017 |
| **S-002** | structural | blocking | `app.service` is enabled | App will not start on reboot | `systemctl is-enabled app.service --quiet` | BROKEN |
| **S-003** | structural | blocking | `nginx` is active | Proxy is not running; no traffic reaches app | `systemctl is-active nginx --quiet` | BROKEN, F-015 |
| **S-004** | structural | blocking | `nginx` is enabled | nginx will not start on reboot | `systemctl is-enabled nginx --quiet` | BROKEN |

### 3.3 Process Checks (P-series)

These checks verify that processes are running with the correct identity and are bound to the expected network addresses.

| ID | Layer | Severity | Assertion | Failure meaning | Observable command | Maps to |
|---|---|---|---|---|---|---|
| **P-001** | behavioral | blocking | App process runs as `appuser` | Service is running with wrong identity — security violation | `pgrep -u appuser server > /dev/null` | BROKEN |
| **P-002** | behavioral | blocking | App listens on `127.0.0.1:8080` | App bound to wrong address or port; nginx upstream unreachable | `ss -ltnp \| grep -q '127.0.0.1:8080'` | BROKEN, F-002, F-007, F-016 |
| **P-003** | behavioral | blocking | nginx listens on `0.0.0.0:80` | No HTTP traffic can reach the system | `ss -ltnp \| grep -q '0.0.0.0:80'` | BROKEN, F-015 |
| **P-004** | behavioral | blocking | nginx listens on `0.0.0.0:443` | No HTTPS traffic can reach the system | `ss -ltnp \| grep -q '0.0.0.0:443'` | BROKEN, F-015 |

### 3.4 Endpoint Checks (E-series)

These checks verify the behavioral contract of the HTTP interface. They are the primary behavioral authority.

| ID | Layer | Severity | Assertion | Failure meaning | Observable command | Maps to |
|---|---|---|---|---|---|---|
| **E-001** | behavioral | blocking | `GET /health` returns HTTP 200 | App is not serving health checks; process may be running but not functional | `curl -sf http://localhost/health > /dev/null` | BROKEN, F-001, F-002, F-003, F-005, F-006, F-007, F-009, F-013, F-017 |
| **E-002** | behavioral | blocking | `GET /` returns HTTP 200 | Primary request path is failing | `curl -sf http://localhost/ > /dev/null` | BROKEN, F-004, F-018 |
| **E-003** | behavioral | blocking | `/health` body contains `"status":"ok"` | App is responding but not confirming config loaded | `curl -s http://localhost/health \| jq -e '.status == "ok"' > /dev/null` | BROKEN |
| **E-004** | behavioral | blocking | Response includes `X-Proxy: nginx` header | nginx is not proxying — traffic reaching app directly or response from wrong source | `curl -sI http://localhost/ \| grep -q 'X-Proxy: nginx'` | BROKEN, F-007 |
| **E-005** | behavioral | blocking | `GET https://app.local/health` returns 200 (skip verify) | TLS listener or upstream not functioning | `curl -skf https://app.local/health > /dev/null` | BROKEN, F-015 |

### 3.5 Filesystem Checks (F-series)

These checks verify structural conformance — canonical ownership, modes, and content validity.

| ID | Layer | Severity | Assertion | Failure meaning | Observable command | Maps to |
|---|---|---|---|---|---|---|
| **F-001** | structural | blocking | `/opt/app/server` exists, owned `appuser:appuser`, mode `750` | Binary missing, wrong ownership, or not executable | `stat -c '%U:%G %a' /opt/app/server \| grep -q 'appuser:appuser 750'` | BROKEN, F-005 |
| **F-002** | structural | blocking | `/etc/app/config.yaml` exists, owned `appuser:appuser`, mode `640` | Config missing, wrong ownership, or unreadable by appuser | `stat -c '%U:%G %a' /etc/app/config.yaml \| grep -q 'appuser:appuser 640'` | BROKEN, F-001, F-003 |
| **F-003** | structural | blocking | `/var/log/app/` exists, owned `appuser:appuser`, mode `755` | Log directory missing or wrong permissions | `stat -c '%U:%G %a' /var/log/app \| grep -q 'appuser:appuser 755'` | BROKEN, F-009 |
| **F-004** | structural | blocking | `/var/lib/app/` exists, owned `appuser:appuser`, mode `755` | State directory missing or wrong permissions — `/` will return 500 | `stat -c '%U:%G %a' /var/lib/app \| grep -q 'appuser:appuser 755'` | BROKEN, F-004, F-018 |
| **F-005** | structural | blocking | nginx configuration passes syntax check | nginx config has syntax error; nginx will not reload | `nginx -t 2>/dev/null` | BROKEN, F-015 |
| **F-006** | structural | degraded | TLS certificate exists and has not expired | HTTPS will fail; certificate requires renewal | `openssl x509 -checkend 0 -noout -in /etc/nginx/tls/app.local.crt` | BROKEN, F-012 |
| **F-007** | structural | blocking | `app.local` resolves to `127.0.0.1` | TLS hostname resolution broken; HTTPS problems will be misattributed | `getent hosts app.local \| grep -q '127.0.0.1'` | BROKEN |

### 3.6 Log Checks (L-series)

These checks verify that the application's log output is present and structured correctly.

| ID | Layer | Severity | Assertion | Failure meaning | Observable command | Maps to |
|---|---|---|---|---|---|---|
| **L-001** | operational | degraded | `/var/log/app/app.log` exists and is non-empty | No log output — app may not be logging, or log file was deleted | `test -s /var/log/app/app.log` | BROKEN, F-009, F-010 |
| **L-002** | operational | degraded | Last line of `app.log` is valid JSON | Log is corrupted or format has changed | `tail -1 /var/log/app/app.log \| jq . > /dev/null 2>&1` | BROKEN, F-009 |
| **L-003** | operational | degraded | `app.log` contains a startup entry | App started but startup log was not produced — logging failure | `grep -q '"msg":"server started"' /var/log/app/app.log` | BROKEN, F-009 |

---

## §4 — Validation Semantics

### 4.1 Point-in-Time Snapshot Model

Validation is a point-in-time snapshot, not continuous monitoring. Running `lab validate` produces a result that reflects the system state at the moment the checks execute. The result does not remain valid after the snapshot — system state can change between validations.

**Implication:** conformance is not a persistent property. It is a property of a moment. The state file records the last known conformance result; it does not guarantee current conformance.

### 4.2 Check Ordering and Dependencies

Checks within a category are independent unless explicitly noted. Checks across categories have implied dependencies:

- S-series SHOULD pass before P-series is meaningful. A non-active service will fail all process and endpoint checks — these failures are consequences of the service state check, not independent failures.
- P-series SHOULD pass before E-series is meaningful. A process not listening on the correct port will fail all endpoint checks.
- F-series and L-series are independent of other categories.

**Implication for diagnosis:** when multiple checks fail, examine the highest-authority failures first. An E-series failure with an S-series failure is most likely caused by the service state, not an independent endpoint problem.

### 4.3 Partial Conformance — Degraded-Conformant

A system is **degraded-conformant** when all blocking checks pass and one or more degraded-severity checks fail. The system is classified as operationally degraded but semantically correct.

Degraded-conformant environments MAY be used for fault injection. The degraded condition MUST be noted in the validation output. Degraded-conformant is recorded in the state file as `"conformance": "degraded"` within a `CONFORMANT` state.

Current degraded-severity checks: F-006, L-001, L-002, L-003.

### 4.4 What a Passing Suite Proves and Does Not Prove

**A passing conformance suite proves:**
- All specified invariants hold at the moment of validation
- The system is in a known-good state relative to the canonical specification
- Fault injection will start from a defined, reproducible baseline

**A passing conformance suite does NOT prove:**
- The system will behave correctly under all possible inputs
- The system is free of all bugs or misconfigurations not covered by the check catalog
- The system will remain conformant after the next operation

The check catalog is a finite approximation of correctness, not a proof of correctness.

---

## §5 — Model Completeness Condition

The conformance model is complete when the following bidirectional condition holds:

**Forward direction:** every system state, transition, and fault defined in `system-state-model.md` and `fault-model.md` maps to at least one check in this catalog (via the `Maps to` field of the check schema).

**Exceptions to the forward direction (derived from `fault-model.md` §8):**
- **F-008** (SIGTERM ignored) and **F-014** (zombie accumulation): these faults manifest only at shutdown or over time — no blocking conformance check fails while the app is running normally. They are verified by their `Observable` commands, not by the conformance suite.
- **F-011** (nginx proxy timeout) and **F-012** (TLS certificate not in trust store): these are documented baseline behaviors, not mutations. They are not applied via `lab fault apply` and are therefore not expected to appear in check `Maps to` fields.

**Reverse direction:** every check in this catalog maps to at least one state, transition, or fault in `system-state-model.md` or `fault-model.md` (via the `Maps to` field).

**Completeness violations (prohibited):**
- A check that exists but maps to no state, transition, or fault — it tests something no model document defines
- A state, transition, or fault that has no corresponding check — it has no observable and cannot be verified
- A check whose failure meaning is undefined — it cannot be interpreted

**Completeness maintenance:** when a new fault is added to `fault-model.md`, at least one check in this catalog MUST be updated to include the new fault ID in its `Maps to` field, or a new check MUST be added. When a new check is added to this catalog, it MUST reference at least one state, transition, or fault in the `Maps to` field.

---

## §6 — Authority and References

**This document's authority:** semantic root. No other document may redefine what "conformant" means. Behavioral conformance is semantic authority; structural conformance is explanatory authority.

**Documents that depend on this document:**
- `system-state-model.md` — uses check catalog to define CONFORMANT state and state detection algorithm
- `fault-model.md` — uses check catalog IDs in fault postcondition specifications
- `canonical-environment.md` — uses this document's definitions; the conformance suite in §6 of that document is the executable expression of this catalog

**Version consistency:** this document's version MUST match the version of `system-state-model.md` and `fault-model.md`. The three model documents form a coherent versioned set.