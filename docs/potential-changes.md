The file structure reveals a fully implemented Go control plane that closely mirrors the specification’s domain model. The good news: the code is **well‑structured, tested, and already embodies many of the decisions you’re about to formalize**. The bad news: exactly because it’s an executable implementation of an ambiguous spec, **several areas will require adjustment** as you resolve contradictions and write the four model documents.

Here’s a package‑by‑package assessment of what’s likely to change.

---

## 1. `internal/catalog/` — **High risk of change**

- `fault.go`, `faults.go`: The code says **18 fault definitions**. The spec’s catalog includes F‑001 through F‑018, but **F‑011 is not a real fault** — it’s baseline behaviour with no mutation. You’ll almost certainly **remove F‑011** (or reclassify it as a pedagogical note, not a catalog entry). That will reduce the count to **17 faults**.
- `catalog_test.go` likely contains tests that assert the count (e.g., “AllDefs length == 18”). Those tests will break and need updating.
- The `FaultDef` metadata schema may need new fields (`IsReversible`, `RequiresConfirmation`, `Preconditions`) that are hinted at in the spec but not fully codified yet. While the code might already have some of these, the formal `fault-model.md` will finalise them, which could lead to schema changes.

**Action:** Wait until you draft `fault-model.md` before touching this package, but expect to delete F‑011 and possibly extend the struct.

---

## 2. `internal/conformance/` — **Medium‑high risk**

- `catalog.go`: **23 checks** are implemented. The spec’s check list (S‑001…L‑003) is incomplete; you’ll need to add checks for:
  - Unit file syntax (e.g., `systemd-analyze verify` or at least file‑exists + parseable)
  - Sudoers entry for `devuser`
  - Manifest drift detection (the `--manifest-check` flag mentioned in §8.4)
- `check.go`: The `Severity` type (blocking/degraded/info) already exists, but the exact mapping of each check to severity hasn’t been formally defined yet. The `conformance-model.md` will lock this down, and you may need to reclassify a few checks. That’s a low‑effort code change but a high‑importance decision.
- `runner_test.go`: Classification tests will need updating once new checks are added or severities change.

**Action:** Draft `conformance-model.md` first, then add the missing checks and adjust severities. The 23‑check count will become ~26, and the golden test data (`validate_conformant.json`) will need regeneration.

---

## 3. `internal/state/` — **Medium risk**

- `detect.go`: The detection algorithm already implements conflict resolution (§4.2, §4.3 of the spec). That’s excellent. However, the spec leaves a gap: **how to reconcile when the manifest (`MANIFEST` file) disagrees with runtime observations**. Drift detection is not yet integrated into the state machine. When you write `system-state-model.md`, you’ll need to decide whether manifest drift is part of state detection or a separate observation. If it’s part of state, `detect.go` will gain a new dimension.
- `state.go`: The six states are already defined. The transition logic (`IsValidTransition`, `CanApplyFault`, etc.) is likely already guarding against double‑faulting. That’s fine; formalising `system-state-model.md` will just confirm what’s there — unless you discover the spec’s contradiction about **degraded → broken when a second fault is attempted**. If that transition is forbidden, `state.go` must enforce it (it probably already does).
- `store.go`: The state file schema is partly defined; `control-plane-contract.md` will pin down its exact JSON fields, ring buffer size, and corruption recovery. Minor adjustments possible.

**Action:** Draft `system-state-model.md` and `control-plane-contract.md` concurrently with the conformance work. The state package is mostly stable; changes will be small, additive, and focused on manifest drift.

---

## 4. `internal/executor/` — **Low risk, but linked to catalog**

- `real.go`, `executor.go`: The concrete implementation of fault application and reset. If the fault catalog changes (removing F‑011, adding new fields like `RequiresConfirmation`), the executor will need to handle those new metadata properties. Possibly no code change other than consuming the updated catalog.
- `audit.go`: Audit log format is probably already implemented; `control-plane-contract.md` will finalise the schema. Likely no change unless the contract demands something significantly different.
- `lock.go`, `lock_test.go`: Already covers advisory locking. That’s solid; the contract doc will just describe it.

**Action:** After `fault-model.md` and `control-plane-contract.md` are stable, review the executor to ensure it respects new fault flags (e.g., requiring confirmation for F‑008, F‑014). Otherwise low‑touch.

---

## 5. `cmd/`, `internal/output/` — **Low risk**

- The CLI commands (`status`, `validate`, `fault`, `reset`, `provision`, `history`) are already built with tests and output contracts. The `control-plane-contract.md` will essentially document what’s already there. The command line flags, exit codes, and JSON schemas are frozen in golden test files (`testdata/golden/`). Any change to the output model would break those golden files, which is a strong disincentive to change. So this area is stable.
- One possible addition: if you decide that `lab validate --manifest-check` is a distinct flag, a new file or modification in `validate.go` may be needed. Minor.

---

## 6. `internal/config/` — **Low risk, but one trigger**

- `config.go`: Centralises paths, ownership, modes. This matches the spec’s §2.3 and §4. Unless you change the filesystem layout (unlikely), this stays stable. The only possible change: if you resolve the state‑directory contradiction by making `/var/lib/app/` writability a hard precondition (Option A from earlier), that might affect the conformance check for that directory, but not the constants.

---

## 7. `internal/invariants/` — **Artifact, will break deliberately**

- `invariants_test.go`: Contains cross‑document rules like “must be exactly 18 faults”, “must be exactly 23 checks”. These tests are **deliberate tripwires**. When you remove F‑011 or add new conformance checks, these tests will fail, signalling that you’ve changed a fundamental invariant. That’s a *good* thing — they force you to update the golden files and the expected counts consistently.

---

## 8. Missing pieces: Service internals & provisioning

The Go module you’ve shown is the **control plane**. The **Go HTTP service** lives in `lab-env/service/` (not shown) and will need its own internal design document (`service-internals.md`). The **provisioning blueprint** (`bootstrap.sh` and the `lab provision` command’s interaction with it) is also outside this file tree. These will be independent workstreams, not directly modifying the above packages.

However, the executor may reference compilation steps for F‑008/F‑014; those will rely on the provisioning blueprint’s specification of build commands.

---

## Summary: What to change and when

| Area                           | Likely changes                                                                 | Trigger Documents                                   |
|--------------------------------|---------------------------------------------------------------------------------|-----------------------------------------------------|
| `internal/catalog/faults.go`   | Remove F‑011, possibly extend `FaultDef` with new fields (`IsReversible`, etc.) | `fault-model.md`                                    |
| `internal/catalog/catalog_test.go` | Update fault count (18→17), add tests for new fields                      | `fault-model.md`                                    |
| `internal/conformance/catalog.go` | Add 3–4 new checks, reclassify some severities                               | `conformance-model.md`                              |
| `internal/state/detect.go`     | Potentially incorporate manifest drift into detection                          | `system-state-model.md`                             |
| `internal/executor/real.go`    | Minor: honour new `RequiresConfirmation` flag, if added                       | `fault-model.md`, `control-plane-contract.md`       |
| `cmd/validate.go`              | Add `--manifest-check` flag (if desired)                                       | `conformance-model.md`, spec §8.4                   |
| `internal/invariants/invariants_test.go` | Expected counts will change; update golden test data                    | All model documents                                 |
| Golden JSON contracts          | Regenerate `validate_conformant.json`, `fault_apply_success.json`, etc.        | Every change that touches output schemas            |

**The most urgent order:**  
1. Draft `fault-model.md` → remove F‑011 from the catalog, update `faults.go`.  
2. Draft `conformance-model.md` → add missing checks, finalise severities.  
3. Draft `system-state-model.md` → integrate manifest drift (or decide to leave it out of scope).  
4. Draft `control-plane-contract.md` → mostly documentation, minimal code change.

The codebase is already an 80% faithful implementation of the eventual specification. Your job now is to **write the documents that make the remaining 20% (the ambiguous edges) explicit**, and then adjust the few places where the code drifted from those formal definitions. The invariant tests will be your safety net.