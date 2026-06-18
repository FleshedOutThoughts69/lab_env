# Remaining Features & Aspirational Gaps
## lab‑env — from current implementation to full specification

This document lists every feature, endpoint, schema field, and behavioural contract that is present in the semantic model documents, environment specification, or application runtime contract but is **not yet implemented** in the verified codebase. Use it as a development roadmap.

---

## 1. Service (the subject application)

### 1.1 Missing endpoints
- **`GET /reset`** – TCP RST via `SO_LINGER` (canonical‑environment §3.3, app runtime contract §3.3). Not implemented.
- **`GET /headers`** – echoes proxy headers (`Host`, `X‑Forwarded‑For`, etc.) (canonical‑environment §3.3, app runtime contract §3.3). Not implemented.

### 1.2 Simplified response bodies
- **`GET /health`** currently returns `{"status":"ok"}`. The spec calls for additional fields: `app_env`, `config_loaded`.  
- **`GET /` (success)** currently returns `{"status":"ok","env":"<APP_ENV>"}`. The spec calls for `"path":"/"`.  
- **`GET /slow`** currently returns `{"status":"ok"}`. The spec calls for `"path":"/slow","delay_seconds":<N>`.

### 1.3 Chaos injection
- **`CHAOS_OOM_TRIGGER`** is not fully implemented. A test hook exists (`StartOOMForTest`) but is not exported. The spec expects the service to allocate memory until killed by the OOM killer.  
- **`CHAOS_IGNORE_SIGTERM`** is implemented as a **build flag** (F‑008), not as a runtime environment variable. The spec describes it as a chaos.env variable that can be toggled without rebuilding.  
- The service does not re‑read `chaos.env` on SIGHUP; chaos changes require a restart.

### 1.4 Telemetry
- The current schema has **12 fields** (including `inode_usage_percent`). The spec lists 10 fields; the extra field is an enhancement. No action needed, but the spec may want to adopt it.
- `chaos_modes` is always `[]` (never `null`), which is correct.

---

## 2. Control plane

### 2.1 Interrupt handling grace period
- **Spec:** the current executor operation is allowed to complete normally; grace periods of 30 s / 120 s.  
- **Implementation:** immediately exits with code 4. No grace period. This is a valid, simpler behaviour, but the aspirational contract includes the grace period.

### 2.2 Post‑apply verification
- **Spec:** after a successful `fault apply`, the control plane MAY run the fault’s `FailingChecks` to verify the fault is active.  
- **Implementation:** not performed. The operator must manually run `lab validate`.

### 2.3 `lab status` lock
- **Spec:** `lab status` may write `state.json` without the mutation lock.  
- **Implementation:** `lab status` acquires the lock before any write (reconciliation, classification‑validity restoration, fresh‑file creation). This is safer than the spec; the spec should be updated to reflect this.

### 2.4 Global `--yes` flag forwarding
- **Fixed** — the global `--yes` flag now reaches `fault apply`. No further work needed.

### 2.5 `reset‑failed` before service restart
- **Fixed** — R1 and R2 reset now call `systemctl reset‑failed` before restarting the app. No further work needed.

---

## 3. Environment provisioning (bootstrap)

### 3.1 Learner account (`devuser`)
- **Spec:** a `devuser` (UID 1000, sudo, shell) is created. The security model and sudoers are written for this user.  
- **Implementation:** only `appuser` is created. No learner account exists. All testing was done as root or a user with passwordless sudo.

### 3.2 Extended package list
- **Spec:** additional learner tools (`tcpdump`, `strace`, `vim`, `htop`, `lsof`, `dnsutils`, etc.).  
- **Implementation:** only the minimum required packages are installed. Adding the full list is a simple bootstrap change.

### 3.3 Endpoints `/reset` and `/headers` (see §1.1) – these also require corresponding conformance checks, which don't exist yet.

---

## 4. Fault catalog

### 4.1 F‑008 / F‑014 Apply behaviour
- **Spec:** Apply rebuilds the binary with the appropriate ldflags and restarts the service.  
- **Implementation:** Apply returns an error immediately. The faults are not applied. Manual binary rebuild is required to exercise them fully.

### 4.2 F‑019 / F‑020 / F‑021
- **F‑019** (block exhaustion), **F‑020** (chaos latency via env), **F‑021** (nftables drop rule) are defined in the Application Runtime Contract and fault model but are not in the catalog. They need `FaultDef` entries, `Apply`/`Recover` functions, and catalog updates (increment fault counts, update invariants).

---

## 5. Conformance suite
- No new checks are required for existing faults (all 23 are implemented).  
- If new endpoints (§1.1) or new faults (§4.2) are added, corresponding checks must be created.

---

## 6. Documentation & testing

### 6.1 Skipped unit tests
Some unit tests are skipped with documented reasons. They can be re‑enabled when the underlying issues are addressed:

| Test | Reason |
|------|--------|
| `TestE001_CheckLogic_MatchesHandlerResponse` | Stub observer URL mismatch |
| `TestE003_CheckLogic_MatchesHandlerResponse` | Same |
| `TestF004DiagnosticPattern_E001PassesE002Fails` | Same |
| `TestRunner_PanickingCheck_DoesNotHaltSuite` | Runner doesn't recover panics |
| `TestCatalog_SeverityInvariant_BlockingChecksAreInCorrectSeries` | Category constants removed |
| `TestStartOOM_SyncOnce_GuardsAgainstDuplicateStart` | OOM test hook not exported |
| `TestSanitizeEnvString_StripsControlChars` | SanitizeEnvString not exported |
| `TestChaosHandler_Drop100_HealthIsExempted` | Design decision pending |
| `TestArchitecture_CatalogFaults_DoNotImportState` | Legitimate import of state.State |
| `TestArchitecture_OutputPackage_DoNotImportConformance` | Legitimate import of conformance.SuiteResult |

### 6.2 Schema drift lock tests (Phase D.2)
- `TestGolden_FaultListResult_NoExtraFields` – not written yet  
- `TestGolden_FaultInfoResult_NoExtraFields` – not written yet  
- `TestGolden_ResetResult_NoExtraFields` – not written yet  
- `TestGolden_HistoryResult_NoExtraFields` – not written yet  

---

## Summary of effort

| Area | Item | Effort |
|------|------|--------|
| Service | `/reset`, `/headers` endpoints | Small (new handlers, conformance checks) |
| Service | Enriched response bodies | Small (add fields to existing handlers) |
| Service | Runtime chaos (OOM, SIGTERM) | Medium (goroutine management, signal masking) |
| Control plane | Interrupt grace period | Medium (refactor signal handler) |
| Control plane | Post‑apply verification | Small (run FailingChecks after Apply) |
| Environment | `devuser` account + tooling | Small (bootstrap step) |
| Faults | F‑019, F‑020, F‑021 | Medium (new catalog entries, Apply/Recover) |
| Faults | F‑008/F‑014 automated Apply | Medium (build in control plane, or document as manual) |
| Tests | Re‑enable skipped tests | Small (fix stubs, export helpers) |
| Tests | Schema drift lock tests | Small (add NoExtraFields tests) |

This document should be committed alongside the specs so anyone picking up the project knows exactly what remains to reach full compliance with the aspirational design.