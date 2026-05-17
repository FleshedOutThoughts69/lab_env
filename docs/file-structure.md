97 files. Printing the updated structure:

---

```
lab-env/                                    module: github.com/lab-env/lab
│                                           the control plane — observes, classifies,
│                                           mutates, and audits the subject environment
│
├── main.go                                 process adapter — constructs App,
│                                           calls os.Exit(app.Run(os.Args[1:]))
│
├── app.go                                  composition root and command dispatcher;
│                                           wires all concrete dependencies, routes to
│                                           command handlers, renders results
│
├── go.mod                                  module: github.com/lab-env/lab
│                                           single external dependency: cobra
│
├── scripts/
│   ├── bootstrap.sh                        idempotent 16-step provisioning script;
│   │                                       called by lab provision and R3 reset;
│   │                                       creates appuser uid=1001, loopback mount,
│   │                                       cgroup slice, TLS cert, nftables LAB-FAULT
│   │                                       chain, sudoers entry, installs all canonical
│   │                                       config files, enables services; validates
│   │                                       with visudo -c before writing sudoers
│   │
│   └── validate.sh                         shell-level conformance suite; all 23 checks
│                                           implemented as bash commands matching the
│                                           ObservableCommand fields in catalog.go;
│                                           exit 0 = CONFORMANT or DEGRADED-CONFORMANT,
│                                           exit 1 = NON-CONFORMANT; side-effect-free
│
├── cmd/                                    package cmd — command handlers (orchestration only)
│   │
│   ├── status.go                           lab status — canonical reconciliation point;
│   │                                       runs LightweightRun + state.Detect; only
│   │                                       command authorized to update authoritative
│   │                                       state classification
│   │
│   ├── validate.go                         lab validate — observation primitive; runs full
│   │                                       conformance suite; records last_validate;
│   │                                       MUST NOT update state field (observation-only)
│   │
│   ├── fault.go                            lab fault list/info/apply — list/info use
│   │                                       AllDefs() (no executor); apply uses ImplByID()
│   │                                       with 5-step precondition sequence, TOCTOU
│   │                                       re-read after lock, atomicity guarantee
│   │
│   ├── reset_provision_history.go          lab reset / lab provision / lab history;
│   │                                       reset selects tier from fault metadata;
│   │                                       provision delegates to bootstrap.sh via
│   │                                       RunMutation; history reads ring buffer
│   │
│   ├── testhelpers_test.go                 [test] shared stubs for all cmd tests:
│   │                                       stubObserver, healthyObs(), unhealthyObs(),
│   │                                       mockFileInfo
│   │
│   ├── status_test.go                      [test] reconciliation authority contract:
│   │                                       only command that reconciles state, corrupt/
│   │                                       missing file handling, classification_valid
│   │                                       recovery, DEGRADED with active fault
│   │
│   ├── validate_test.go                    [test] observation-only contract: never writes
│   │                                       state field, single-check writes nothing,
│   │                                       exit code from blocking checks only
│   │
│   ├── fault_test.go                       [test] apply contract: unknown ID rejected
│   │                                       before lock, precondition failures, baseline
│   │                                       rejection, Apply failure atomicity, history
│   │                                       updated on success
│   │
│   └── interrupt_test.go                   [test] full interrupt-path contract (8 points):
│                                           signal → grace period → classification
│                                           invalidation → audit entry → exit 4 →
│                                           status reclassification
│
├── internal/
│   │
│   ├── config/                             single source of truth for all canonical
│   │   │                                   environment constants and embedded templates
│   │   │
│   │   ├── config.go                       [Go] all canonical constants: file paths,
│   │   │                                   permission modes, ownership strings, service
│   │   │                                   names, network addresses, R2 reset targets
│   │   │
│   │   ├── app.service                     [template] canonical systemd unit; embedded
│   │   │                                   by canonical_files.go; defines RuntimeDirectory=app,
│   │   │                                   StartLimitBurst=5, StartLimitInterval=30s,
│   │   │                                   TimeoutStopSec=10, Slice=app.slice,
│   │   │                                   StandardOutput/Error=journal; chaos.env
│   │   │                                   EnvironmentFile with CRITICAL permission note
│   │   │
│   │   ├── config.yaml                     [template] canonical app config; embedded and
│   │   │                                   restored during R2 reset; KnownFields strict
│   │   │                                   parsing; F-002 changes server.addr
│   │   │
│   │   ├── nginx.conf                      [template] canonical nginx reverse proxy;
│   │   │                                   embedded and restored during R2 reset;
│   │   │                                   upstream app_backend block — F-007 changes
│   │   │                                   one server address to break all proxy blocks;
│   │   │                                   X-Proxy: nginx header (E-004);
│   │   │                                   proxy_read_timeout 3s (F-011 demo);
│   │   │                                   localhost default_server (E-001–E-004)
│   │   │
│   │   └── logrotate.conf                  [template] canonical logrotate config;
│   │                                       NOT in R2RestoreFiles (provisioning-only);
│   │                                       copytruncate with O_APPEND coupling documented;
│   │                                       lastaction explicitly chowns .last_rotate 0644
│   │
│   ├── conformance/                        package conformance — semantic root;
│   │   │                                   depends only on Observer
│   │   │
│   │   ├── observer.go                     Observer interface: 8 read-only methods;
│   │   │                                   no lock, no audit, no mutation authority
│   │   │
│   │   ├── check.go                        Check type, Severity, Category, Layer,
│   │   │                                   CheckResult
│   │   │
│   │   ├── result.go                       SuiteResult, Classification, Classify(),
│   │   │                                   ExitCode() — degraded failures return exit 0
│   │   │
│   │   ├── catalog.go                      all 23 check implementations; Execute functions
│   │   │                                   use only Observer; paths from internal/config
│   │   │
│   │   ├── runner.go                       dependency-ordered S→P→E→F→L; E-series
│   │   │                                   marked Dependent on S-001 failure;
│   │   │                                   LightweightRun() (4 checks) for lab status;
│   │   │                                   RunIDs() for postcondition verification
│   │   │
│   │   ├── runner_test.go                  [test] classification semantics, degraded-only
│   │   │                                   exit 0, dependent suppression, no-early-abort,
│   │   │                                   catalog completeness (23 checks, ordering)
│   │   │
│   │   ├── runner_edge_cases_test.go       [test] panic in check recovered and runner
│   │   │                                   continues; severity invariant (blocking=S/P/E,
│   │   │                                   degraded=F-006/L); all checks handle missing
│   │   │                                   files without panic
│   │   │
│   │   └── cross_module_test.go            [test] conformance check logic vs actual HTTP
│   │                                       responses via httptest.Server; E-003 vs exact
│   │                                       handler body; E-002 fails on 500; F-004
│   │                                       diagnostic pattern (E-001 pass, E-002 fail)
│   │                                       verified end-to-end
│   │
│   ├── state/                              package state — classification engine
│   │   │                                   + persistence layer
│   │   │
│   │   ├── state.go                        State type (6 values); transition helpers:
│   │   │                                   CanApplyFault(), CanReset(),
│   │   │                                   RequiresActiveFault(), ForbidsActiveFault()
│   │   │
│   │   ├── store.go                        state.json schema; atomic read/write
│   │   │                                   (temp+rename); ring buffer history (50);
│   │   │                                   InvalidateClassification() for interrupt
│   │   │
│   │   ├── detect.go                       detection algorithm (system-state-model §4.2);
│   │   │                                   pure functions over DetectInput; all 4
│   │   │                                   conflict resolution cases (§4.3)
│   │   │
│   │   ├── detect_test.go                  [test] adversarial matrix — all 4 §4.3 cases
│   │   │                                   named by section; optimistic DEGRADED trust
│   │   │                                   documented
│   │   │
│   │   ├── signal_combinations_test.go     [test] all recorded state values vs conformant
│   │   │                                   suite; classification_valid=false always
│   │   │                                   re-derives; fault explaining non-conformant
│   │   │                                   results → DEGRADED not BROKEN
│   │   │
│   │   ├── store_test.go                   [test] atomic write, schema round-trip,
│   │   │                                   corruption recovery, ring buffer limit,
│   │   │                                   InvalidateClassification state/certainty
│   │   │
│   │   └── store_edge_cases_test.go        [test] 0-byte file → ErrStateFileCorrupt;
│   │                                       whitespace-only file → ErrStateFileCorrupt;
│   │                                       concurrent InvalidateClassification + SaveState
│   │                                       race safety; read-only dir Save returns error
│   │                                       without corrupting original
│   │
│   ├── executor/                           package executor — mutation authority boundary
│   │   │
│   │   ├── executor.go                     Executor interface: embeds Observer + 9
│   │   │                                   mutation methods; RunMutation = audited
│   │   │                                   privileged path; Observer.RunCommand = unaudited
│   │   │
│   │   ├── real.go                         concrete implementation against Ubuntu VM;
│   │   │                                   NewObserver() for read-only; NewExecutor(audit)
│   │   │                                   for mutations; atomicWrite with fsync;
│   │   │                                   all sudo via runSudo(); MkdirAll before write
│   │   │
│   │   ├── canonical_files.go              go:embed directives for app.service,
│   │   │                                   config.yaml, nginx.conf; init() populates
│   │   │                                   canonicalFiles map with content + per-file
│   │   │                                   mode/ownership from config constants;
│   │   │                                   RestoreFile uses this during R2 reset
│   │   │
│   │   ├── audit.go                        AuditLogger: appends JSON entries before each
│   │   │                                   operation; 6 entry types (executor_op,
│   │   │                                   state_transition, validation_run,
│   │   │                                   reconciliation, interrupt, error);
│   │   │                                   append-only, never truncated
│   │   │
│   │   ├── lock.go                         advisory mutation lock at config.LockPath;
│   │   │                                   stale detection via kill -0; serializes all
│   │   │                                   state mutations; read-only commands never lock
│   │   │
│   │   ├── audit_test.go                   [test] schema, 6 entry types distinct,
│   │   │                                   append-only, mutation audit completeness
│   │   │
│   │   ├── boundary_test.go                [test] Observer≠Executor; RunMutation
│   │   │                                   unavailable through Observer interface
│   │   │
│   │   ├── lock_test.go                    [test] acquire/fail/reclaim stale/reclaim
│   │   │                                   malformed/release/idempotent/re-acquire/
│   │   │                                   contention (9 tests)
│   │   │
│   │   ├── lock_stale_system_process_test.go [test] lock with PID 1 (always-live system
│   │   │                                   process) must be reclaimable — PID collision
│   │   │                                   with system process must not permanently block
│   │   │                                   lab operations; self-PID lock documented
│   │   │
│   │   ├── embed_test.go                   [test] all embedded files non-empty, contain
│   │   │                                   required keys/directives; mode/ownership
│   │   │                                   non-zero; no null bytes; nginx upstream block
│   │   │                                   present and active (F-007 Apply precondition)
│   │   │
│   │   ├── restore_test.go                 [test] RestoreFile sets canonical mode and
│   │   │                                   ownership per-file (not uniform default);
│   │   │                                   audit error entry written on mutation failure
│   │   │
│   │   ├── mutation_failure_test.go        [test] LogError produces valid JSON audit
│   │   │                                   entry with entry_type="error", operation,
│   │   │                                   path, error fields; nil audit logger
│   │   │                                   refused by constructor
│   │   │
│   │   └── trace_test.go                   [test] operational trace sequence invariants:
│   │                                       write before unlock; audit before mutation;
│   │                                       read-only commands never lock; fault apply
│   │                                       8-step sequence matches spec
│   │
│   ├── catalog/                            package catalog — fault definitions
│   │   │
│   │   ├── fault.go                        FaultDef (pure metadata, JSON-serializable);
│   │   │                                   FaultImpl (adds Apply/Recover functions);
│   │                                       IsBaselineBehavior explicit bool
│   │   │
│   │   ├── faults.go                       all 18 fault definitions (F-001–F-018);
│   │   │                                   AllDefs/AllImpls/DefByID/ImplByID;
│   │   │                                   nginx faults use upstream app_backend block;
│   │   │                                   F-008/F-014 non-reversible; F-011/F-012
│   │   │                                   baseline behavior
│   │   │
│   │   ├── catalog_test.go                 [test] 18 faults, sequential IDs, required
│   │   │                                   fields, Apply/Recover present, preconditions,
│   │   │                                   baseline explicit, non-reversible require R3,
│   │   │                                   postcondition check IDs valid, AllDefs copies
│   │   │
│   │   └── content_integrity_test.go       [test] Recover restores different content
│   │                                       than Apply wrote; non-reversible Recover
│   │                                       returns error; Apply targets ≤3 files
│   │                                       (no overly broad replaceInBytes)
│   │
│   ├── output/                             package output — presentation only; no logic
│   │   │
│   │   ├── model.go                        one result type per command; CommandResult
│   │   │                                   wraps any result with exit code;
│   │   │                                   FromSuiteResult() converts conformance results
│   │   │
│   │   ├── render.go                       human and JSON renderers; stdout = data,
│   │   │                                   stderr = diagnostics; quiet mode support
│   │   │
│   │   ├── render_test.go                  [test] JSON schema, stream separation,
│   │   │                                   H-001 regression guard, quiet suppression
│   │   │
│   │   ├── golden_test.go                  [test] frozen JSON contract fixtures;
│   │   │                                   UPDATE_GOLDEN=1 to regenerate; no-extra-fields
│   │   │                                   guards; active_fault explicit null contract
│   │   │
│   │   └── output_quality_test.go          [test] all rendered JSON is valid UTF-8;
│   │                                       no trailing whitespace on any line; output
│   │                                       is compact not pretty-printed; last_validate
│   │                                       is RFC3339; no double JSON encoding
│   │
│   ├── invariants/                         package invariants — cross-document invariants
│   │   │
│   │   ├── doc.go                          package stub; no production code
│   │   │
│   │   ├── invariants_test.go              [test] cross-document rules: fault
│   │   │                                   FailingChecks exist in catalog, degraded
│   │   │                                   checks non-blocking, baseline not applyable,
│   │   │                                   non-reversible require R3, 6 states,
│   │   │                                   18 faults × 23 checks count invariants
│   │   │
│   │   └── architecture_test.go            [test] static import boundary enforcement via
│   │                                       go list; no production code imports testing
│   │                                       or testutil; conformance does not import
│   │                                       executor; catalog does not import state;
│   │                                       output does not import conformance; service
│   │                                       module does not import control plane
│   │
│   └── testutil/                           package testutil — test infrastructure only
│       │                                   (never imported by production code)
│       │
│       └── interrupt.go                    InterruptableExecutor: fires cancel() after
│                                           N mutations; used by cmd/interrupt_test.go
│                                           to prove interrupt-path contract
│
├── testdata/
│   └── golden/                             frozen JSON contract fixtures
│       ├── status_conformant.json
│       ├── status_degraded.json
│       ├── status_broken.json
│       ├── validate_conformant.json
│       ├── fault_apply_success.json
│       └── fault_info_f004.json
│
└── docs/
    ├── fault-matrix-runbook.md             operator truth table: one row per fault,
    │                                       failing/passing checks, verification commands,
    │                                       diagnostic lookup table
    │
    ├── operational-trace-spec.md           13 ordered event traces with
    │                                       [lock][obs][audit][mut][write] notation;
    │                                       anomaly detection table
    │
    ├── recovery-playbook.md                9 hostile-state drills; 7-point checklist
    │
    ├── golden-baseline-ledger.md           frozen field index; stable vs unstable;
    │                                       update protocol
    │
    ├── extension-boundary-note.md          change gates for adding fault/check/command/
    │                                       audit type/state; required changes, failing
    │                                       tests, forbidden shortcuts
    │
    ├── testing-plan-revised.md             Phase 0→A→B→C→D; 3.5–5 day estimate;
    │                                       5 ranked risks; entry/exit criteria
    │
    └── portfolio-presentation-package.md   README, authority flow diagram, 5 demos,
                                            interviewer takeaways, positioning statement

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

service/                                    module: github.com/lab-env/service
│                                           the subject application — observed, broken,
│                                           and recovered by the control plane;
│                                           no imports from github.com/lab-env/lab
│
├── main.go                                 GOMAXPROCS(1) for cgroup alignment;
│                                           15-step startup; 4-step shutdown;
│                                           CHAOS_IGNORE_SIGTERM branches signal handler;
│                                           second-signal reset after first consumed;
│                                           OOM started after first telemetry write;
│                                           http.ErrServerClosed filtered
│
├── go.mod                                  module: github.com/lab-env/service
│                                           single dependency: gopkg.in/yaml.v3
│
├── CONFORMANCE_CONTRACT.md                 maps every conformance check to the exact
│                                           service signal; handler-to-check table;
│                                           fault targets; diagnostic patterns;
│                                           E-001/E-002 split design rationale;
│                                           chaos latency /health exemption documented
│
├── config/
│   ├── config.go                           KnownFields strict YAML (rejects unknown
│   │                                       keys); parseBool() accepts "1"/"true"/"yes";
│   │                                       sanitizeEnvString() strips control chars;
│   │                                       chaos vars read once at startup; no SIGHUP
│   │
│   ├── config_test.go                      [test] valid parse, missing file error,
│   │                                       unknown key rejected, defaults, parseBool
│   │                                       all true values, missing chaos vars default,
│   │                                       sanitize strips control chars, invalid
│   │                                       latency disabled
│   │
│   └── config_edge_test.go                 [test] spaces in app_env behavior documented;
│                                           BOM handled or rejected clearly; drop percent
│                                           out-of-range disabled; ActiveModes() order
│                                           stable across calls
│
├── logging/
│   ├── logging.go                          O_APPEND guaranteed in constructor (prevents
│   │                                       null-byte corruption after copytruncate);
│   │                                       sync.Mutex per entry (no interleaving);
│   │                                       single Write syscall per line (unbuffered);
│   │                                       variadic k/v pairs; mode 0640
│   │
│   ├── logging_test.go                     [test] O_APPEND survives truncation (no null
│   │                                       bytes); 50 concurrent goroutines, no
│   │                                       interleaved lines; single complete JSON entry
│   │                                       per Write; k/v pairs in output; mode 0640
│   │
│   └── logging_edge_test.go                [test] special chars escaped; Close()
│                                           idempotent; write after Close no panic;
│                                           Info/Warn/Error produce correct level field
│
├── server/
│   ├── server.go                           conformance contract comments on every
│   │                                       handler; GET /health never touches
│   │                                       /var/lib/app; GET / touches state dir —
│   │                                       500 + "state write failed" on failure;
│   │                                       GET /slow hardcoded 5s; chaos latency
│   │                                       exempted on /health
│   │
│   ├── server_test.go                      [test] /health 200 + exact body; /health
│   │                                       survives unwritable state dir; / success
│   │                                       200 + env field; / failure 500 + exact body;
│   │                                       E-001 passes / E-002 fails simultaneously;
│   │                                       /slow 200 after 5s; counters increment
│   │
│   └── server_edge_test.go                 [test] empty app_env → "" not null; no Server
│                                           header; concurrent /health + / during state
│                                           failure — health always 200
│
├── signals/
│   ├── signals.go                          manages /run/app/ signal files; atomic
│   │                                       temp+rename with chmod 0644 before rename;
│   │                                       10-step startup / 4-step shutdown; BeginShutdown:
│   │                                       status=ShuttingDown THEN remove healthy
│   │                                       (prevents false crash signal); Init() removes
│   │                                       stale loading and healthy from previous crash
│   │
│   ├── signals_test.go                     [test] loading → healthy → remove loading
│   │                                       never coexist; shutdown status before healthy
│   │                                       removal; Init removes stale loading; Init
│   │                                       removes stale healthy; no .tmp- files;
│   │                                       all files mode 0644
│   │
│   └── signals_edge_test.go                [test] BeginShutdown when healthy already
│                                           absent; RemovePID removes file; status file
│                                           exact string + newline; PID file decimal +
│                                           newline; RemoveLoading idempotent when absent
│
├── telemetry/
│   ├── telemetry.go                        writes /run/app/telemetry.json every 2s;
│   │                                       12-field Snapshot (ts, pid, uptime_seconds,
│   │                                       cpu_percent, memory_rss_mb, open_fds,
│   │                                       disk_usage_percent, inode_usage_percent,
│   │                                       requests_total, errors_total, chaos_active,
│   │                                       chaos_modes); two-sample CPU delta via
│   │                                       atomic.Value; inode_usage_percent (F-018);
│   │                                       panic recovery keeps goroutine alive
│   │
│   ├── telemetry_test.go                   [test] exactly 12 fields with correct JSON
│   │                                       tags; numeric types not strings; chaos_modes
│   │                                       never null; file written; panic recovered
│   │                                       and goroutine continues
│   │
│   └── telemetry_edge_test.go              [test] uptime_seconds monotonically increasing;
│                                           telemetry written with zero requests (liveness
│                                           signal before first request); memory_rss_mb
│                                           non-zero for running process
│
└── chaos/
    ├── chaos.go                            drop before latency; latency exempted on
    │                                       /health; drop increments both requests_total
    │                                       and errors_total via callbacks; StartOOM()
    │                                       sync.Once; OOM goroutine allocates 64MiB
    │                                       chunks; depends on MemoryMax=256M cgroup
    │
    ├── chaos_test.go                       [test] /health not delayed (200 < latencyMS);
    │                                       / is delayed (elapsed >= latencyMS); drop
    │                                       increments both counters; drop before latency
    │                                       (100ms+100% drop returns fast); zero drop
    │                                       always passes through; StartOOM sync.Once
    │                                       (10 concurrent calls → 1 goroutine); nil
    │                                       callbacks no panic; 1000 concurrent requests
    │                                       counter accuracy
    │
    └── chaos_edge_test.go                  [test] 100% drop all non-health routes get
                                            503; measured latency on / >= expected;
                                            zero latency no measurable delay; /health
                                            drop exemption documented as open question
```

**Totals:** 97 files — 40 Go source, 39 Go test, 2 shell scripts, 4 config templates, 6 JSON fixtures, 7 operational docs, 2 module files, 1 contract markdown, 1 package stub.

**Test file count by package:**

| Package | Test files | Tests |
|---|---|---|
| `cmd/` | 5 (incl. testhelpers) | 27 |
| `internal/catalog/` | 2 | 24 |
| `internal/conformance/` | 3 | 18 |
| `internal/executor/` | 7 | 31 |
| `internal/invariants/` | 2 | 18 |
| `internal/output/` | 3 | 16 |
| `internal/state/` | 4 | 18 |
| `service/chaos/` | 2 | 11 |
| `service/config/` | 2 | 12 |
| `service/logging/` | 2 | 8 |
| `service/server/` | 2 | 10 |
| `service/signals/` | 2 | 9 |
| `service/telemetry/` | 2 | 8 |
| **Total** | **39** | **~210** |




The documentation I need is not a high‑level architecture deck — it’s an **execution‑order playbook** that turns “I have a fresh VM” into “I am running the fault matrix and all tests pass.”

---

## What the documentation should cover

### 1. Prerequisites & assumptions
- Base OS version (e.g., Ubuntu 22.04, specific kernel if cgroup v2 matters)
- Required packages before bootstrap (`apt-get install …` or a list for manual install)
- Go toolchain version (1.22+ for `go:embed` and `math/rand` auto‑seed, etc.)
- Expected filesystem layout if pre‑created

### 2. Step‑by‑step environment setup
This is the most important section. It must be a linear, numbered list that someone can copy‑paste:

1. Clone repository
2. Run bootstrap script (with expected output)
3. Verify the service is running (`systemctl status app`)
4. Verify nginx is proxying (`curl http://localhost/`)
5. Run `lab validate` and confirm all 23 checks pass
6. Apply a fault, validate, reset, validate again

Each step should show **the exact command** and **the expected output**.

### 3. Component‑by‑component testing instructions
For each major component, a self‑contained section that explains:

| Component | How to test in isolation | What success looks like |
|-----------|--------------------------|-------------------------|
| Service binary | `go run ./service` or `curl http://localhost:8080/health` | `{"status":"ok"}` |
| Control plane CLI | `go build && ./lab status` | JSON output with CONFORMANT |
| Signal files | `cat /run/app/status` | `Running` |
| Telemetry | `cat /run/app/telemetry.json \| jq .` | 12‑field JSON |
| Chaos injection | Set `CHAOS_LATENCY_MS=100` in chaos.env, restart service, curl `/slow` | Increased latency |
| nftables | `sudo nft list chain lab_filter LAB-FAULT` | Empty chain with accept policy |
| Log rotation | `logrotate -f /etc/logrotate.d/app && cat /var/log/app/app.log` | No null bytes |

### 4. Test suite execution order
Your test plan (in `testing-plan-revised.md`) already exists, but the onboarding doc should translate it into exact commands:

- **Phase 0 (unit tests):** `cd lab-env && go test ./...` and `cd ../service && go test ./...`
- **Phase B (live system):** Start the lab environment, then run integration tests
- **Phase C (invariant stress):** The specific commands that test cross‑package invariants
- **Phase D (golden freeze):** How to run `UPDATE_GOLDEN=1 go test ./internal/output`

### 5. Common failure modes & triage
This section pays for itself the first time something breaks. For each symptom, list the likely cause:

| Symptom | Likely cause | Check |
|---------|-------------|-------|
| Service fails to start | `appuser` UID mismatch | `id -u appuser` |
| `lab validate` all E‑checks fail | nginx not running or wrong config | `systemctl status nginx && nginx -t` |
| Telemetry file empty or missing | `/run/app` wrong permissions | `stat /run/app` |
| Fault apply hangs | Stale lock file | `ls -la /var/lock/lab-mutation.lock` |
| Log file has null bytes | Logging not using O_APPEND | Check `service/logging/logging.go` |
| OOM chaos doesn't kill process | cgroup memory limit not enforced | `systemctl show app | grep MemoryMax` |

### 6. Development workflow (edit‑test cycle)
Explain how to make a change to one component and verify it quickly:

- **Changing the service:** `go build -o /opt/app/server ./service && systemctl restart app && curl http://localhost/health`
- **Changing the control plane:** `go build -o lab && ./lab validate`
- **Changing a fault definition:** Build, then `./lab fault apply F‑004 && ./lab validate && ./lab reset`
- **Changing a conformance check:** Build, then `./lab validate --check S‑001`

### 7. Full fault‑matrix walkthrough as a script
The documentation should include a copy‑paste‑able script that runs the entire fault matrix:

```bash
#!/bin/bash
set -e
for fault in F-001 F-002 F-004 F-007 F-008 F-016 F-018; do
  echo "=== Applying $fault ==="
  ./lab fault apply "$fault"
  ./lab validate
  ./lab reset
  ./lab validate
done
echo "All faults applied and recovered successfully"
```

This is the fastest way to confirm the whole system works.

---

## Format recommendation

Write this as a single `DEVELOPER-QUICKSTART.md` in the repository root. Every instruction must be a copy‑paste‑able command. No narrative, no architecture theory — those live in the existing docs. This document is purely operational.

The high ROI is that this document will be used **every single time** someone sets up the lab, which means it gets battle‑tested and stays accurate, unlike most documentation that rots on the wiki.