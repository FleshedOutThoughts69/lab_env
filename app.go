// Package main provides the process entry point.
// app.go owns dependency composition, argument parsing, and dispatch.
// main.go is process-adapter only.
package main

import (
	"fmt"
	"io"
	"os"

	"lab_env/cmd"
	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// App is the composition root. It wires together all concrete dependencies
// and provides the single entry point for the process.
// main.go calls App.Run; all behavior lives in App and below.
type App struct {
	stdout io.Writer
	stderr io.Writer
}

// NewApp returns an App writing to the given writers.
func NewApp(stdout, stderr io.Writer) *App {
	return &App{stdout: stdout, stderr: stderr}
}

// Run parses args, dispatches to the appropriate command, renders the result,
// and returns the exit code. It does not call os.Exit.
func (a *App) Run(args []string) int {
	// Parse global flags.
	flags, remaining, err := parseGlobalFlags(args)
	if err != nil {
		fmt.Fprintf(a.stderr, "error: %v\n", err)
		fmt.Fprintf(a.stderr, "Run 'lab --help' for usage.\n")
		return 2
	}

	if len(remaining) == 0 || remaining[0] == "--help" || remaining[0] == "-h" {
		a.printUsage()
		return 0
	}

	// Select renderer based on global flags.
	format := output.FormatHuman
	if flags.json {
		format = output.FormatJSON
	}
	renderer := output.NewRenderer(a.stdout, a.stderr, format, flags.quiet)

	// Build shared dependencies.
	obs := executor.NewObserver()
	runner := conformance.NewRunner()
	store := state.NewStore()

	// Dispatch.
	command := remaining[0]
	subArgs := remaining[1:]

	invocation := "lab " + joinArgs(remaining)
	audit := executor.NewAuditLogger(invocation)

	var result output.CommandResult

	switch command {
	case "status":
		c := cmd.NewStatusCmd(obs, runner, store, audit)
		result = c.Run()

	case "validate":
		c := cmd.NewValidateCmd(obs, runner, store, audit)
		if len(subArgs) >= 2 && subArgs[0] == "--check" {
			result = c.RunSingle(subArgs[1])
		} else {
			result = c.Run()
		}

	case "fault":
		if len(subArgs) == 0 {
			fmt.Fprintf(a.stderr, "error: 'lab fault' requires a subcommand: list, info, apply\n")
			return 2
		}
		result = a.dispatchFault(subArgs, obs, runner, store, audit, flags)

	case "reset":
		result = a.dispatchReset(subArgs, obs, runner, store, audit)

	case "provision":
		result = a.dispatchProvision(subArgs, obs, runner, store, audit)

	case "history":
		result = a.dispatchHistory(subArgs, store)

	default:
		fmt.Fprintf(a.stderr, "error: unknown command %q\n", command)
		fmt.Fprintf(a.stderr, "Run 'lab --help' for usage.\n")
		return 2
	}

	// Render result.
	if result.Err != nil {
		renderer.Errorf("%v", result.Err)
	} else if result.Value != nil {
		renderer.Render(result.Value)
	}

	return result.ExitCode
}

// dispatchFault handles the fault subcommand family.
func (a *App) dispatchFault(args []string, obs conformance.Observer, runner *conformance.Runner, store *state.Store, audit *executor.AuditLogger, flags globalFlags) output.CommandResult {
	if len(args) == 0 {
		return output.CommandResult{ExitCode: 2, Err: fmt.Errorf("usage: lab fault [list|info|apply]")}
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "list":
		c := cmd.NewFaultListCmd()
		return c.Run()

	case "info":
		if len(subArgs) == 0 {
			return output.CommandResult{ExitCode: 2, Err: fmt.Errorf("usage: lab fault info <ID>")}
		}
		c := cmd.NewFaultInfoCmd()
		return c.Run(subArgs[0])

	case "apply":
		if len(subArgs) == 0 {
			return output.CommandResult{ExitCode: 2, Err: fmt.Errorf("usage: lab fault apply <ID> [--force] [--yes]")}
		}
		faultID, faultFlags := parseFaultApplyFlags(subArgs)
		exec := executor.NewExecutor(audit)
		c := cmd.NewFaultApplyCmd(obs, runner, exec, store, audit)
		return c.Run(faultID, faultFlags.force, faultFlags.yes)

	default:
		return output.CommandResult{ExitCode: 2, Err: fmt.Errorf("unknown fault subcommand %q: use list, info, or apply", sub)}
	}
}

func (a *App) dispatchReset(args []string, obs conformance.Observer, runner *conformance.Runner, store *state.Store, audit *executor.AuditLogger) output.CommandResult {
	tier := "" // auto-select
	for i, arg := range args {
		if arg == "--tier" && i+1 < len(args) {
			tier = args[i+1]
		}
	}
	exec := executor.NewExecutor(audit)
	c := cmd.NewResetCmd(obs, runner, exec, store, audit)
	return c.Run(tier)
}

func (a *App) dispatchProvision(_ []string, obs conformance.Observer, runner *conformance.Runner, store *state.Store, audit *executor.AuditLogger) output.CommandResult {
	exec := executor.NewExecutor(audit)
	c := cmd.NewProvisionCmd(obs, runner, exec, store, audit)
	return c.Run()
}

func (a *App) dispatchHistory(args []string, store *state.Store) output.CommandResult {
	last := 20
	for i, arg := range args {
		if arg == "--last" && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &last)
		}
	}
	c := cmd.NewHistoryCmd(store)
	return c.Run(last)
}

func (a *App) printUsage() {
	fmt.Fprintln(a.stdout, `lab — Lab Environment Control Plane

Usage:
  lab <command> [flags]

Commands:
  status                    Show current environment state
  validate [--check <ID>]   Run conformance suite (observation only)
  fault list                List all faults in the catalog
  fault info <ID>           Show full fault entry
  fault apply <ID>          Apply a fault (CONFORMANT → DEGRADED)
  reset [--tier R1|R2|R3]   Reset environment to CONFORMANT
  provision                 Provision or re-provision the environment
  history [--last N]        Show state transition history

Global flags:
  --json      Emit JSON output
  --quiet     Suppress all non-error output
  --verbose   Emit executor operation trace
  --yes       Suppress confirmation prompts

Run 'lab fault list' to see available faults.
Run 'lab fault info <ID>' for fault details.`)
}

// globalFlags holds the parsed global flags.
type globalFlags struct {
	json    bool
	quiet   bool
	verbose bool
	yes     bool
}

type faultApplyFlags struct {
	force bool
	yes   bool
}

func parseGlobalFlags(args []string) (globalFlags, []string, error) {
	var flags globalFlags
	var remaining []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			flags.json = true
		case "--quiet":
			flags.quiet = true
		case "--verbose":
			flags.verbose = true
		case "--yes":
			flags.yes = true
		default:
			remaining = append(remaining, args[i])
		}
	}

	if flags.json && flags.quiet {
		return flags, nil, fmt.Errorf("--json and --quiet are mutually exclusive")
	}
	if flags.verbose && flags.quiet {
		return flags, nil, fmt.Errorf("--verbose and --quiet are mutually exclusive")
	}

	return flags, remaining, nil
}

func parseFaultApplyFlags(args []string) (id string, flags faultApplyFlags) {
	for _, arg := range args {
		switch arg {
		case "--force":
			flags.force = true
		case "--yes":
			flags.yes = true
		default:
			if id == "" && len(arg) > 0 && arg[0] != '-' {
				id = arg
			}
		}
	}
	return
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}