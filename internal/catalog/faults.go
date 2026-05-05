package catalog

import (
	"fmt"
	"io/fs"
	"strings"

	cfg "lab_env/lab/internal/config"
	"lab_env/lab/internal/executor"
	"lab_env/lab/internal/state"
)

// AllImpls returns the complete fault catalog as FaultImpl (with Apply/Recover).
// Used by commands that need to execute faults: lab fault apply, lab reset.
func AllImpls() []*FaultImpl {
	return []*FaultImpl{
		faultF001(), faultF002(), faultF003(), faultF004(),
		faultF005(), faultF006(), faultF007(), faultF008(),
		faultF009(), faultF010(), faultF011(), faultF012(),
		faultF013(), faultF014(), faultF015(), faultF016(),
		faultF017(), faultF018(),
	}
}

// AllDefs returns the complete fault catalog as FaultDef (metadata only).
// Used by commands that only display fault information: lab fault list, lab fault info.
func AllDefs() []*FaultDef {
	impls := AllImpls()
	defs := make([]*FaultDef, len(impls))
	for i, impl := range impls {
		def := impl.FaultDef // copy
		defs[i] = &def
	}
	return defs
}

// All returns the complete catalog as FaultImpl.
// Retained for backward compatibility with callers expecting All().
func All() []*FaultImpl {
	return AllImpls()
}

// ImplByID returns the FaultImpl with the given ID, or nil if not found.
// Used by lab fault apply and lab reset.
func ImplByID(id string) *FaultImpl {
	for _, f := range AllImpls() {
		if f.Def.ID == id {
			return f
		}
	}
	return nil
}

// ByID returns the FaultImpl with the given ID.
// Retained for backward compatibility.
func ByID(id string) *FaultImpl {
	return ImplByID(id)
}

// DefByID returns the FaultDef with the given ID, or nil if not found.
// Used by lab fault info (no executor dependency required).
func DefByID(id string) *FaultDef {
	f := ImplByID(id)
	if f == nil {
		return nil
	}
	def := f.FaultDef
	return &def
}

// newImpl is a constructor helper that builds a FaultImpl from a FaultDef
// plus Apply and Recover functions. This avoids the Go composite literal
// restriction on embedded structs (promoted fields cannot be used in
// composite literals alongside the outer struct's own fields).
func newImpl(
	def FaultDef,
	apply func(executor.Executor) error,
	recover func(executor.Executor) error,
) *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		FaultDef: def,
		},
		Apply:    apply,
		Recover:  recover,
	}
}

// errNonReversible is returned by Recover for faults that require R3 reset.
func errNonReversible(id string) error {
	return fmt.Errorf("fault %s requires R3 reset (full reprovision) — run: lab reset --tier R3", id)
}

func faultF001() *FaultImpl {
	return &FaultImpl{
		ID:                   "F-001",
		Layer:                "filesystem",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App cannot start because config is missing. Restart loop with connection refused.",
			FailingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004", "E-005"},
			PassingChecks: []string{"F-003", "F-007"},
		},
		Symptom:             "Service enters restart loop; curl localhost/health fails (connection refused)",
		AuthoritativeSignal: "journald — journalctl -u app.service",
		Observable:          "journalctl -u app.service -n 20 shows repeated start failures with config-not-found error; curl localhost/health → connection refused",
		MutationDisplay:     "sudo rm /etc/app/config.yaml",
		ResetAction:         "sudo cp lab-env/config/config.yaml /etc/app/config.yaml && sudo chown appuser:appuser /etc/app/config.yaml && sudo chmod 640 /etc/app/config.yaml && sudo systemctl restart app",
		Apply: func(exec executor.Executor) error {
			return exec.Remove(cfg.ConfigPath)
		},
		Recover: func(exec executor.Executor) error {
			return exec.RestoreFile(cfg.ConfigPath)
		},
	}
}

func faultF002() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-002",
		Layer:                "config/socket",
		Domain:               []string{"linux", "networking"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App is running and healthy from its own perspective (reachable directly on 9090) but unreachable via nginx (which expects 8080).",
			FailingChecks: []string{"P-002", "E-001", "E-002", "E-003", "E-004", "E-005"},
			PassingChecks: []string{"S-001", "P-001", "F-002"},
		},
		Symptom:             "nginx returns 502; app process is running and believes it is healthy",
		AuthoritativeSignal: "ss -ltnp + nginx error log",
		Observable:          "ss -ltnp | grep 9090 shows app on wrong port; curl -I localhost → 502 with X-Proxy: nginx; curl 127.0.0.1:9090/health → 200 (direct)",
		MutationDisplay:     "Change server.addr in /etc/app/config.yaml from 127.0.0.1:8080 to 127.0.0.1:9090; sudo systemctl restart app",
		ResetAction:         "Restore canonical config.yaml, restart app",
		},
		Apply: func(exec executor.Executor) error {
			data, err := exec.ReadFile(cfg.ConfigPath)
			if err != nil {
				return fmt.Errorf("reading config: %w", err)
			}
			// Simple string replacement of the bind address.
			// The app service will restart with the new config.
			modified := replaceInBytes(data, cfg.AppBindAddr, "127.0.0.1:9090")
			if err := exec.WriteFile(cfg.ConfigPath, modified, cfg.ModeConfig, cfg.ServiceUser, cfg.ServiceGroup); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			return exec.Systemctl("restart", "app")
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.RestoreFile(cfg.ConfigPath); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF003() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-003",
		Layer:                "permissions",
		Domain:               []string{"linux", "security"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App cannot read config (permission denied). Same observable symptoms as F-001 but structural check F-002 mode bits distinguish the cause.",
			FailingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004", "E-005"},
			PassingChecks: []string{"F-002", "F-007"},
		},
		Symptom:             "Service enters restart loop; journald shows permission denied reading config",
		AuthoritativeSignal: "journald",
		Observable:          "journalctl -u app.service -n 10 shows permission denied error; stat /etc/app/config.yaml shows mode 000",
		MutationDisplay:     "sudo chmod 000 /etc/app/config.yaml",
		ResetAction:         "sudo chmod 640 /etc/app/config.yaml && sudo systemctl restart app",
		},
		Apply: func(exec executor.Executor) error {
			return exec.Chmod(cfg.ConfigPath, 0000)
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.Chmod(cfg.ConfigPath, cfg.ModeConfig); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF004() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-004",
		Layer:                "permissions",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "/health returns 200 (service is alive); / returns 500 (state write fails). Health/ready split is the diagnostic signal.",
			FailingChecks: []string{"E-002", "F-004"},
			PassingChecks: []string{"S-001", "E-001", "E-003"},
		},
		Symptom:             "/health returns 200; / returns 500; app continues running",
		AuthoritativeSignal: "app.log",
		Observable:          `curl localhost/health → 200; curl localhost/ → 500; tail -5 /var/log/app/app.log shows "level":"error","msg":"state write failed"`,
		MutationDisplay:     "sudo chmod 000 /var/lib/app/",
		ResetAction:         "sudo chmod 755 /var/lib/app/",
		},
		Apply: func(exec executor.Executor) error {
			return exec.Chmod(cfg.StateDir, 0000)
		},
		Recover: func(exec executor.Executor) error {
			return exec.Chmod(cfg.StateDir, cfg.ModeStateDir)
		},
	}
}

func faultF005() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-005",
		Layer:                "permissions",
		Domain:               []string{"linux"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "systemd cannot execute the binary (execute bit removed). Restart loop with exec failure.",
			FailingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004", "E-005", "F-001"},
			PassingChecks: []string{"F-002", "F-007"},
		},
		Symptom:             "Service fails to start; systemctl status app shows exec failure",
		AuthoritativeSignal: "journald",
		Observable:          "journalctl -u app.service -n 5 shows exec format error or permission denied; ls -la /opt/app/server shows 640",
		MutationDisplay:     "sudo chmod 640 /opt/app/server",
		ResetAction:         "sudo chmod 750 /opt/app/server && sudo systemctl restart app",
		},
		Apply: func(exec executor.Executor) error {
			return exec.Chmod(cfg.BinaryPath, 0640)
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.Chmod(cfg.BinaryPath, cfg.ModeBinary); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF006() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-006",
		Layer:                "service",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App startup validation fails on the environment variable check. journald message explicitly identifies APP_ENV as missing.",
			FailingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004", "E-005"},
			PassingChecks: []string{"F-002"},
		},
		Symptom:             "Service fails to start; journald shows missing APP_ENV error",
		AuthoritativeSignal: "journald",
		Observable:          "journalctl -u app.service -n 10 shows missing APP_ENV error; systemctl show app --property=Environment shows no APP_ENV",
		MutationDisplay:     "Remove Environment=APP_ENV=prod line from /etc/systemd/system/app.service; sudo systemctl daemon-reload && sudo systemctl restart app",
		ResetAction:         "Restore canonical unit file; sudo systemctl daemon-reload && sudo systemctl restart app",
		},
		Apply: func(exec executor.Executor) error {
			data, err := exec.ReadFile(cfg.UnitFilePath)
			if err != nil {
				return fmt.Errorf("reading unit file: %w", err)
			}
			modified := replaceInBytes(data, "Environment=APP_ENV=prod\n", "")
			if err := exec.WriteFile(cfg.UnitFilePath, modified, cfg.ModeUnitFile, cfg.RootUser, cfg.RootGroup); err != nil {
				return err
			}
			if err := exec.Systemctl("daemon-reload", ""); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.RestoreFile(cfg.UnitFilePath); err != nil {
				return err
			}
			if err := exec.Systemctl("daemon-reload", ""); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF007() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-007",
		Layer:                "proxy/config",
		Domain:               []string{"linux", "networking"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App is healthy on 8080. nginx is misconfigured to upstream port 9090. Distinguishable from F-002 because P-002 passes (app on correct port).",
			FailingChecks: []string{"E-001", "E-002", "E-003", "E-004", "E-005"},
			PassingChecks: []string{"S-001", "P-001", "P-002"},
		},
		Symptom:             "nginx returns 502; app is running correctly on 8080",
		AuthoritativeSignal: "nginx error log + ss -ltnp",
		Observable:          "ss -ltnp | grep 8080 shows app running correctly; curl -I localhost → 502; curl 127.0.0.1:8080/health → 200 (direct)",
		MutationDisplay:     "Change server 127.0.0.1:8080 to server 127.0.0.1:9090 in /etc/nginx/sites-enabled/app; sudo nginx -s reload",
		ResetAction:         "Restore canonical nginx config; sudo nginx -s reload",
		},
		Apply: func(exec executor.Executor) error {
			data, err := exec.ReadFile(cfg.NginxConfigPath)
			if err != nil {
				return fmt.Errorf("reading nginx config: %w", err)
			}
			modified := replaceInBytes(data, "server "+cfg.AppBindAddr, "server 127.0.0.1:9090")
			if err := exec.WriteFile(cfg.NginxConfigPath, modified, cfg.ModeNginxConfig, cfg.RootUser, cfg.RootGroup); err != nil {
				return err
			}
			return exec.NginxReload()
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.RestoreFile(cfg.NginxConfigPath); err != nil {
				return err
			}
			return exec.NginxReload()
		},
	}
}

func faultF008() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-008",
		Layer:                "process",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: true,  // binary rebuild required
		IsReversible:         false, // R3 reset required
		ResetTier:            "R3",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App appears healthy to all endpoint checks during fault. The fault only manifests at shutdown — systemctl stop hangs 90 seconds.",
			FailingChecks: []string{}, // no blocking checks fail while running
			PassingChecks: []string{"S-001", "E-001", "E-002"},
		},
		Symptom:             "systemctl stop app hangs 90 seconds (SIGTERM ignored); app serves requests normally during wait",
		AuthoritativeSignal: "systemctl status app showing stop-sigterm → stop-sigkill transition",
		Observable:          "time sudo systemctl stop app takes ~90 seconds; journalctl shows Sent signal SIGTERM followed by Sent signal SIGKILL 90s later",
		MutationDisplay:     "Rebuild binary with FAULT_IGNORE_SIGTERM=true flag enabled; redeploy. Requires binary rebuild — confirmation required.",
		ResetAction:         "Rebuild binary without fault flag; redeploy. Run: lab reset --tier R3",
		},
		Apply: func(exec executor.Executor) error {
			// Complex fault: requires binary rebuild.
			// Apply is only reached after RequiresConfirmation prompt.
			return fmt.Errorf("F-008 Apply: binary rebuild required — implement in deployment pipeline")
		},
		Recover: func(_ executor.Executor) error {
			return errNonReversible("F-008")
		},
	}
}

func faultF009() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-009",
		Layer:                "permissions",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App cannot open its log file at startup (mode 000). Startup fails before binding. Distinguishable from F-001/F-003 — config exists and is readable.",
			FailingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004", "E-005", "L-001", "L-002", "L-003", "F-003"},
			PassingChecks: []string{"F-002"},
		},
		Symptom:             "Service fails to start; journald shows log file permission denied",
		AuthoritativeSignal: "journald",
		Observable:          "journalctl -u app.service -n 5 shows log file permission denied; stat /var/log/app/app.log shows mode 000",
		MutationDisplay:     "sudo chmod 000 /var/log/app/app.log",
		ResetAction:         "sudo chmod 640 /var/log/app/app.log && sudo chown appuser:appuser /var/log/app/app.log && sudo systemctl restart app",
		},
		Apply: func(exec executor.Executor) error {
			return exec.Chmod(cfg.LogPath, 0000)
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.Chmod(cfg.LogPath, cfg.ModeLogFile); err != nil {
				return err
			}
			if err := exec.Chown(cfg.LogPath, cfg.ServiceUser, cfg.ServiceGroup); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF010() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-010",
		Layer:                "filesystem",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R1",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App is alive and serving requests. Log file is unlinked (link count 0) but inode held open by the app process. New log entries are written to the unlinked inode and are not accessible on the filesystem.",
			FailingChecks: []string{"L-001", "L-002", "L-003"},
			PassingChecks: []string{"S-001", "P-001", "P-002", "E-001", "E-002"},
		},
		Symptom:             "app.log does not exist on disk; app continues running; disk space held by open fd",
		AuthoritativeSignal: "lsof -p $(pgrep server)",
		Observable:          "ls /var/log/app/ shows no app.log; lsof +L1 shows app process holding deleted file descriptor; curl localhost/health → 200",
		MutationDisplay:     "sudo rm /var/log/app/app.log (while service is running)",
		ResetAction:         "sudo systemctl restart app (recreates the log file on startup)",
		},
		Apply: func(exec executor.Executor) error {
			return exec.Remove(cfg.LogPath)
		},
		Recover: func(exec executor.Executor) error {
			// R1: service restart recreates the log file.
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF011() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		FaultDef: FaultDef{
			ID:                   "F-011",
			Layer:                "network",
			Domain:               []string{"networking"},
			RequiresConfirmation: false,
			IsReversible:         false,
			IsBaselineBehavior:   true,
			ResetTier:            "",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "Baseline behavior: GET /slow via nginx returns 504 after ~3 seconds. Direct access to 127.0.0.1:8080/slow returns 200 after 5 seconds.",
			FailingChecks: []string{}, // no conformance checks fail — this is baseline behavior
			PassingChecks: []string{},
		},
		Symptom:             "/slow returns 504 after ~3s (nginx proxy_read_timeout 3s < /slow delay 5s)",
		AuthoritativeSignal: "curl response code + timing",
		Observable:          "time curl -v http://localhost/slow → 504 in ~3s with X-Proxy: nginx; time curl 127.0.0.1:8080/slow → 200 in ~5s",
		MutationDisplay:     "No mutation — this is baseline nginx behavior (proxy_read_timeout 3s < /slow default delay 5s). Not applied via lab fault apply.",
		ResetAction:         "N/A — baseline behavior",
		},
		Apply: func(_ executor.Executor) error {
			return fmt.Errorf("F-011 is a baseline behavior entry and cannot be applied via lab fault apply")
		},
		Recover: func(_ executor.Executor) error {
			return fmt.Errorf("F-011 is a baseline behavior entry and has no recover function")
		},
	}
}

func faultF012() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-012",
		Layer:                "network",
		Domain:               []string{"networking", "security"},
		RequiresConfirmation: false,
		IsReversible:         false,
		IsBaselineBehavior:   true,
		ResetTier:            "",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "Baseline behavior: TLS connection succeeds (handshake completes) but certificate verification fails without -k. The cert is present and valid; it lacks a trusted CA chain.",
			FailingChecks: []string{}, // E-005 uses -k (skip verify) and passes
			PassingChecks: []string{},
		},
		Symptom:             "curl https://app.local/health fails with TLS error (cert not in trust store)",
		AuthoritativeSignal: "curl TLS error output",
		Observable:          "curl -v https://app.local/health → SSL certificate problem: self-signed certificate; curl -sk https://app.local/health succeeds (skip verify)",
		MutationDisplay:     "No mutation — the self-signed certificate is not in the system trust store at baseline. Not applied via lab fault apply.",
		ResetAction:         "Trust installation (problem-specific): sudo cp /etc/nginx/tls/app.local.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates",
		},
		Apply: func(_ executor.Executor) error {
			return fmt.Errorf("F-012 is a baseline behavior entry and cannot be applied via lab fault apply")
		},
		Recover: func(_ executor.Executor) error {
			return fmt.Errorf("F-012 is a baseline behavior entry and has no recover function")
		},
	}
}

func faultF013() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-013",
		Layer:                "service",
		Domain:               []string{"linux"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "Service is enabled (desired state) but not active (runtime state). Demonstrates the critical distinction between systemd desired state and runtime state.",
			FailingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004", "E-005"},
			PassingChecks: []string{"S-002"}, // enabled/failed asymmetry is the fault's diagnostic property
		},
		Symptom:             "systemctl is-enabled app → enabled; systemctl is-active app → failed; service will not start",
		AuthoritativeSignal: "journald + systemctl status app",
		Observable:          "systemctl status app shows failed state and exec error; systemctl is-enabled app → enabled (desired ≠ actual)",
		MutationDisplay:     "Replace ExecStart=/opt/app/server with ExecStart=/opt/app/DOESNOTEXIST in unit file; sudo systemctl daemon-reload",
		ResetAction:         "Restore canonical unit file; sudo systemctl daemon-reload && sudo systemctl start app",
		},
		Apply: func(exec executor.Executor) error {
			data, err := exec.ReadFile(cfg.UnitFilePath)
			if err != nil {
				return fmt.Errorf("reading unit file: %w", err)
			}
			modified := replaceInBytes(data, "ExecStart=/opt/app/server", "ExecStart=/opt/app/DOESNOTEXIST")
			if err := exec.WriteFile(cfg.UnitFilePath, modified, cfg.ModeUnitFile, cfg.RootUser, cfg.RootGroup); err != nil {
				return err
			}
			return exec.Systemctl("daemon-reload", "")
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.RestoreFile(cfg.UnitFilePath); err != nil {
				return err
			}
			if err := exec.Systemctl("daemon-reload", ""); err != nil {
				return err
			}
			return exec.Systemctl("start", "app")
		},
	}
}

func faultF014() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-014",
		Layer:                "process",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: true,  // binary rebuild required
		IsReversible:         false, // R3 reset required
		ResetTier:            "R3",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App serves all endpoints correctly. Zombie accumulation is a resource leak (PID table slots) not an immediate behavioral failure. All endpoint checks pass.",
			FailingChecks: []string{}, // no checks fail initially
			PassingChecks: []string{},
		},
		Symptom:             "ps aux shows growing count of Z-state processes parented to the app",
		AuthoritativeSignal: "ps -eo pid,ppid,stat,comm | grep Z",
		Observable:          "Zombie count increases with each / request; pstree -p $(pgrep server) shows zombie children",
		MutationDisplay:     "Rebuild binary with FAULT_ZOMBIE_CHILDREN=true flag enabled; redeploy. Requires binary rebuild — confirmation required.",
		ResetAction:         "Rebuild binary without fault flag; redeploy. Run: lab reset --tier R3",
		},
		Apply: func(_ executor.Executor) error {
			return fmt.Errorf("F-014 Apply: binary rebuild required — implement in deployment pipeline")
		},
		Recover: func(_ executor.Executor) error {
			return errNonReversible("F-014")
		},
	}
}

func faultF015() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-015",
		Layer:                "proxy",
		Domain:               []string{"linux", "networking"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "Endpoints continue to work (old config persists). Only F-005 (nginx config syntax) fails. Demonstrates nginx's atomic reload: failed reload does not break existing service.",
			FailingChecks: []string{"F-005"},
			PassingChecks: []string{"S-003", "P-003", "P-004", "E-001", "E-002"},
		},
		Symptom:             "nginx reload fails; existing nginx worker processes continue with old config",
		AuthoritativeSignal: "nginx -t output",
		Observable:          "sudo nginx -t shows configuration error; sudo nginx -s reload returns error; curl localhost/health → 200 (old config still active)",
		MutationDisplay:     "Add invalid directive 'invalid_directive on;' to /etc/nginx/sites-enabled/app; attempt sudo nginx -s reload",
		ResetAction:         "Restore canonical nginx config; sudo nginx -s reload",
		},
		Apply: func(exec executor.Executor) error {
			data, err := exec.ReadFile(cfg.NginxConfigPath)
			if err != nil {
				return fmt.Errorf("reading nginx config: %w", err)
			}
			badConfig := append(data, []byte("\ninvalid_directive on;\n")...)
			if err := exec.WriteFile(cfg.NginxConfigPath, badConfig, cfg.ModeNginxConfig, cfg.RootUser, cfg.RootGroup); err != nil {
				return err
			}
			// Attempt reload — nginx -t will fail, nginx -s reload will fail.
			// The Apply succeeds because the file was written (mutation occurred).
			// The conformance check F-005 will now fail.
			exec.NginxReload() //nolint:errcheck — expected to fail
			return nil
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.RestoreFile(cfg.NginxConfigPath); err != nil {
				return err
			}
			return exec.NginxReload()
		},
	}
}

func faultF016() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-016",
		Layer:                "socket/config",
		Domain:               []string{"linux", "networking", "security"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "All nginx-proxied endpoints continue to work. App is additionally exposed directly on all interfaces. nginx proxying is not broken — it is bypassed as an option. P-002 check for 127.0.0.1:8080 fails (app is on 0.0.0.0:8080 instead).",
			FailingChecks: []string{"P-002"},
			PassingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004"},
		},
		Symptom:             "App is accessible directly on port 8080 from any interface, bypassing nginx",
		AuthoritativeSignal: "ss -ltnp",
		Observable:          "ss -ltnp | grep 8080 shows 0.0.0.0:8080 instead of 127.0.0.1:8080; direct access bypasses nginx (X-Proxy: nginx header absent)",
		MutationDisplay:     "Change server.addr in /etc/app/config.yaml from 127.0.0.1:8080 to 0.0.0.0:8080; sudo systemctl restart app",
		ResetAction:         "Restore canonical config.yaml; sudo systemctl restart app",
		},
		Apply: func(exec executor.Executor) error {
			data, err := exec.ReadFile(cfg.ConfigPath)
			if err != nil {
				return fmt.Errorf("reading config: %w", err)
			}
			modified := replaceInBytes(data, "127.0.0.1:8080", "0.0.0.0:8080")
			if err := exec.WriteFile(cfg.ConfigPath, modified, cfg.ModeConfig, cfg.ServiceUser, cfg.ServiceGroup); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.RestoreFile(cfg.ConfigPath); err != nil {
				return err
			}
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF017() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-017",
		Layer:                "service",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "Service does not start. journald error message distinguishes from F-006 — in F-006, the unit file lacks the directive; in F-017, the directive is present but overridden by the system-level environment.",
			FailingChecks: []string{"S-001", "E-001", "E-002", "E-003", "E-004", "E-005"},
			PassingChecks: []string{"F-002", "F-001"},
		},
		Symptom:             "Service fails to start; journald shows APP_ENV is empty or missing error",
		AuthoritativeSignal: "journald",
		Observable:          "journalctl -u app.service -n 5 shows empty APP_ENV error; systemctl show app --property=Environment shows APP_ENV= (empty)",
		MutationDisplay:     "sudo systemctl set-environment APP_ENV=; sudo systemctl restart app",
		ResetAction:         "sudo systemctl unset-environment APP_ENV && sudo systemctl restart app",
		},
		Apply: func(exec executor.Executor) error {
			// Set APP_ENV to empty string at the systemd manager level,
			// overriding the unit file value.
			if err := exec.RunMutation("systemctl", "set-environment", "APP_ENV="); err != nil {
				return fmt.Errorf("setting empty APP_ENV: %w", err)
			}
			return exec.Systemctl("restart", "app")
		},
		Recover: func(exec executor.Executor) error {
			if err := exec.RunMutation("systemctl", "unset-environment", "APP_ENV"); err != nil {
				return fmt.Errorf("unsetting APP_ENV: %w", err)
			}
			return exec.Systemctl("restart", "app")
		},
	}
}

func faultF018() *FaultImpl {
	return &FaultImpl{
		Def: &FaultDef{
		ID:                   "F-018",
		Layer:                "filesystem",
		Domain:               []string{"linux", "os"},
		RequiresConfirmation: false,
		IsReversible:         true,
		ResetTier:            "R2",
		Preconditions:        []state.State{state.StateConformant},
		Postcondition: PostconditionSpec{
			Behavioral:    "App is running but / fails (cannot create the state file touch). /health continues to return 200. Demonstrates the inode/block distinction.",
			FailingChecks: []string{"E-002", "F-004"},
			PassingChecks: []string{"S-001", "E-001", "E-003"},
		},
		Symptom:             "Filesystem reports inode exhaustion; new files cannot be created despite available disk blocks",
		AuthoritativeSignal: "df -i",
		Observable:          "df -i /var/lib/app shows inode usage near 100%; touch /var/lib/app/test → No space left on device despite df -h showing available blocks; app / endpoint returns 500",
		MutationDisplay:     "for i in $(seq 1 100000); do sudo touch /var/lib/app/file_$i; done",
		ResetAction:         "sudo rm /var/lib/app/file_*",
		},
		Apply: func(exec executor.Executor) error {
			// Create 100,000 empty files to exhaust inodes.
			for i := 1; i <= 100000; i++ {
				path := fmt.Sprintf("%s/file_%d", cfg.StateDir, i)
				if err := exec.WriteFile(path, []byte{}, fs.FileMode(0644), cfg.ServiceUser, cfg.ServiceGroup); err != nil {
					return fmt.Errorf("creating inode exhaustion file %d: %w", i, err)
				}
			}
			return nil
		},
		Recover: func(exec executor.Executor) error {
			// Remove all created files via glob expansion.
			return exec.RunMutation("sh", "-c", "rm -f /var/lib/app/file_*")
		},
	}
}

// replaceInBytes replaces the first occurrence of old with new in data.
func replaceInBytes(data []byte, old, new string) []byte {
	return []byte(strings.Replace(string(data), old, new, 1))
}