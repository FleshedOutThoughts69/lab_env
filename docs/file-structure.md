lab-env/                                     module: github.com/lab-env/lab
│
├── main.go                                  package main  — process adapter (7 lines)
├── app.go                                   package main  — composition root, dispatch
├── go.mod                                   module definition
│
├── cmd/                                     package cmd / cmd_test
│   ├── status.go                            cmd         — lab status command
│   ├── validate.go                          cmd         — lab validate command
│   ├── fault.go                             cmd         — lab fault list/info/apply
│   ├── reset_provision_history.go           cmd         — lab reset/provision/history
│   ├── testhelpers_test.go                  cmd_test    — shared: stubObserver, healthyObs/unhealthyObs
│   ├── status_test.go                       cmd_test    — status reconciliation contract
│   ├── validate_test.go                     cmd_test    — observation-only contract
│   ├── fault_test.go                        cmd_test    — precondition/atomicity contract
│   └── interrupt_test.go                    cmd_test    — interrupt-path cross-layer contract
│
├── internal/
│   │
│   ├── config/
│   │   └── config.go                        config      — all canonical constants (paths, modes, names)
│   │
│   ├── conformance/                         package conformance / conformance_test
│   │   ├── observer.go                      conformance — Observer interface (read-only)
│   │   ├── check.go                         conformance — Check type, Severity, Category, Layer
│   │   ├── result.go                        conformance — SuiteResult, Classification, ExitCode
│   │   ├── catalog.go                       conformance — all 23 check implementations
│   │   ├── runner.go                        conformance — dependency-ordered execution
│   │   └── runner_test.go                   conformance_test — classification, ordering, catalog integrity
│   │
│   ├── state/                               package state / state_test
│   │   ├── state.go                         state       — State type, 6 values, transition helpers
│   │   ├── store.go                         state       — state.json schema, atomic read/write, ring buffer
│   │   ├── detect.go                        state       — detection algorithm (§4.2), conflict resolution (§4.3)
│   │   ├── detect_test.go                   state_test  — adversarial matrix, all §4.3 conflict cases
│   │   └── store_test.go                    state_test  — atomicity, schema, corruption recovery
│   │
│   ├── executor/                            package executor / executor_test
│   │   ├── executor.go                      executor    — Executor interface (embeds Observer + 9 mutations)
│   │   ├── real.go                          executor    — concrete implementation (Observer + Executor)
│   │   ├── audit.go                         executor    — audit log append, 6 entry types
│   │   ├── lock.go                          executor    — advisory lock, stale detection
│   │   ├── audit_test.go                    executor_test — schema, write, completeness assertion
│   │   ├── lock_test.go                     executor_test — acquire/release/stale/live contract
│   │   └── boundary_test.go                 executor_test — Observer≠Executor interface separation
│   │
│   ├── catalog/                             package catalog / catalog_test
│   │   ├── fault.go                         catalog     — FaultDef (metadata), FaultImpl (+ Apply/Recover)
│   │   ├── faults.go                        catalog     — all 18 fault definitions, AllDefs/AllImpls/ByID
│   │   └── catalog_test.go                  catalog_test — completeness, preconditions, metadata contract
│   │
│   ├── output/                              package output / output_test
│   │   ├── model.go                         output      — result types per command, CommandResult
│   │   ├── render.go                        output      — human + JSON renderers
│   │   ├── render_test.go                   output_test — schema, stream separation, H-001 guard
│   │   └── golden_test.go                   output_test — frozen JSON contracts, nullability, no-extra-fields
│   │
│   ├── invariants/                          package invariants / invariants_test
│   │   ├── doc.go                           invariants  — package stub (no production code)
│   │   └── invariants_test.go               invariants_test — cross-document rules (18 faults, 23 checks, etc.)
│   │
│   └── testutil/                            package testutil
│       └── interrupt.go                     testutil    — InterruptableExecutor for interrupt-path tests
│
└── testdata/
    └── golden/
        ├── status_conformant.json           frozen JSON contract — status CONFORMANT
        ├── status_degraded.json             frozen JSON contract — status DEGRADED (with active fault)
        ├── status_broken.json               frozen JSON contract — status BROKEN
        ├── validate_conformant.json         frozen JSON contract — all 23 checks passing
        ├── fault_apply_success.json         frozen JSON contract — successful fault apply
        └── fault_info_f004.json             frozen JSON contract — fault info entry