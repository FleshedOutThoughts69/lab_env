## 1. The Global Architecture (The "Env-Lab" Stack)
The project is split into two distinct but synchronized worlds. The complexity lies in ensuring the **Control Plane** (the tool) correctly interprets the **Data Plane** (the system).

| Layer | Responsibility | Components |
| :--- | :--- | :--- |
| **Control Plane** | The "Referee." Observes and mutates. | Go CLI, State Engine, Fault Catalog. |
| **Data Plane** | The "Field." Where the actual failures happen. | Ubuntu VM, Go Service, Nginx, Systemd. |
| **The Contract** | The "Rulebook." Defines truth. | Canonical Environment Contract v1.0.0. |

---

## 2. Synthesis of Pedagogical Logic
The environment is not just a collection of files; it is a **Signal Generator**. We have distilled three core logical principles that govern how the lab must behave:

*   **The Dependency Chain (Linearity):** Faults are designed to be orthogonal. If the "Proxy" layer is broken, the "Socket" and "Process" layers must remain intact to ensure the student can follow the breadcrumbs.
*   **The Authority Hierarchy:** The `validate.sh` script (Executable Truth) is the ultimate decider. If the system passes validation but contradicts the documentation, the system is correct.
*   **Observability Split:** Information is intentionally siloed. Logs in `journalctl` (lifecycle) are distinct from logs in `app.log` (logic), forcing the student to use the correct tool for the correct layer.

---

## 3. The Implementation Roadmap (The "Gap" Analysis)
We have identified that the Go CLI structure is sound, but the **Subject Material** is currently missing. To bridge this, we must build:

### Phase A: The Data Plane (The Subject)
1.  **The Environment Assets:** "Golden" configuration files for Nginx, Systemd, and Logrotate that the CLI can use to restore state.
2.  **The Bootstrap Engine:** An idempotent bash script that builds the VM from scratch, following a strict 16-step sequence.

### Phase B: The Integration (The Logic)
1.  **Mutation Library:** A library of raw bash commands for the CLI to execute to inject and revert faults.
2.  **State Truth Table:** The logic that allows the CLI to distinguish between an **Intentional Fault** (Problem Set) and an **Unintentional Error** (Broken Lab).

---

## 4. Logical Flow of the "Lab Lifecycle"
This is the order in which the system must move to guarantee a "Conformant" experience:



1.  **Provision:** `bootstrap.sh` builds the layers.
2.  **Validate:** CLI confirms "Conformant" status.
3.  **Mutate:** CLI injects a fault (e.g., F-003: Permissions).
4.  **Observe:** Student uses `strace`, `tail`, or `curl` to find the "Signal."
5.  **Remediate:** Student fixes the file (or uses `lab reset`).
6.  **Re-Validate:** CLI confirms return to "Conformant" status.

---

## 5. Next Action: The Internal Specification
We are now moving into **Component Design**. We will start with **§1: The Go Service Internal Specification**, which will define:
*   The exact byte-level behavior of the `/` endpoint.
*   The JSON schema for the unbuffered logs.
*   The signal-handling logic for `SIGTERM` overrides.

---

Based on the **Canonical Environment Contract v1.0.0** and your provided project structure, there is a significant gap between the **Control Plane** (the Go CLI tool you've mapped out) and the **Data Plane** (the actual system artifacts and the Go service being studied).

The current structure is an excellent "Manager," but the "Managed Assets" are missing. To fulfill the contract, the following components must be added to the `lab-env/` repository.

---

## 1. The Managed Go Service (`service/`)
Section 3 of the contract defines a Go HTTP service that provides the "runtime surface." This is distinct from your CLI tool.
*   **Missing:** `lab-env/service/main.go`. This needs to implement the specific endpoints: `/health`, `/`, `/slow`, `/reset`, and `/headers`.
*   **Missing:** `lab-env/service/faults.go`. To support **F-008** (ignoring SIGTERM), the service needs conditional compilation or a flag-based mechanism to alter its signal handling.
*   **Missing:** `lab-env/service/logging.go`. Implementation of the unbuffered, newline-delimited JSON logging requirement (§3.4).

## 2. Infrastructure as Code & Bootstrap (`provision/` or `scripts/`)
Section 5 explicitly requires a "target-agnostic bootstrap script." 
*   **Missing:** `lab-env/bootstrap.sh`. The idempotent script that performs the 16-step sequence (Package install -> User creation -> Compilation -> Service start).
*   **Missing:** `lab-env/validate.sh`. While you have a `validate.go` in your CLI, the contract (§6) specifically mentions an executable shell script as the authoritative expression of the spec.

## 3. Configuration Templates (`config/`)
The CLI needs source material to "reset" the environment to a conformant state.
*   **Missing:** `lab-env/internal/config/app.service` (systemd unit template).
*   **Missing:** `lab-env/internal/config/config.yaml` (the baseline app config).
*   **Missing:** `lab-env/internal/config/nginx.conf` (the proxy definitions from §4.3).
*   **Missing:** `lab-env/internal/config/logrotate.conf` (the `copytruncate` logic from §4.5).



## 4. Fault Implementation Logic (The "Mutation" scripts)
While `internal/catalog/faults.go` likely contains the Go definitions, the actual **shell-level mutations** (the "Reset action" and "Mutation" columns in §7) need to be accessible.
*   **Implication:** Your `executor/real.go` needs to either embed these shell commands or call out to a set of curated scripts in `lab-env/internal/catalog/scripts/` to ensure the mutations are executed exactly as the contract dictates (e.g., `chmod 000` for F-003).

---

## 5. Revised Structural Recommendation
To bridge the gap between the CLI and the VM environment, I recommend adding these top-level directories:

```text
lab-env/
├── service/                # The Go Service (The "Subject" of the lab)
│   ├── main.go             # HTTP handlers, /slow, /reset logic
│   └── logger.go           # Unbuffered JSON logging implementation
│
├── assets/                 # Canonical configuration source of truth
│   ├── systemd/            # app.service
│   ├── nginx/              # app proxy config
│   └── app/                # baseline config.yaml
│
├── scripts/                # Authoritative bash scripts
│   ├── bootstrap.sh        # The §5 Provisioning Contract implementation
│   └── validate.sh         # The §6 Authoritative Conformance Suite
│
└── ... (your existing Go CLI structure)
```

## Summary of the Gap
Your current structure is the **Lab Software** (the tool the student uses to check status). What is missing is the **Lab Content** (the code that gets broken and the scripts that build the VM). Without the `service/` directory and the `assets/` templates, the `lab` command will have nothing to "validate" or "reset."