# Provisioning Blueprint

## Version 1.0.0

> **Authority:** this document defines the idempotency strategy, failure recovery model, and canonical artifact specification for the lab environment provisioning sequence. The executable implementation is `scripts/bootstrap.sh`. When this document and the script conflict, this document is authoritative on intent; the script is authoritative on exact commands.
>
> **Companion documents:** `canonical-environment.md §5` (provisioning contract), `DEVELOPER-QUICKSTART.md` (operational procedure).

---

## §1 — Idempotency Strategy

The bootstrap script is designed to be re-run safely on any system state: fresh VM, partially provisioned, fully conformant, or fault-injected. Every step determines whether its work is already done before acting.

### 1.1 Idempotency Classification

Each step falls into one of three categories:

**Safe-to-repeat:** the operation produces identical results regardless of how many times it runs. No guard check is needed; running twice is harmless.

**Guard-checked:** the step checks a condition before acting. If the condition is already satisfied, the step logs "already present/done" and moves on without making changes.

**Always-overwrites:** the step unconditionally writes a file or configuration on every run. These steps are idempotent in outcome (the result is always the canonical state) but not in mechanism (a write always occurs). Steps in this category are safe to repeat because they write from embedded canonical content — they restore drift, they do not accumulate it.

### 1.2 Step-by-Step Idempotency Table

| Step | Name | Category | Guard condition | Already-done behavior |
|---|---|---|---|---|
| 01 | Root check | Safe-to-repeat | `id -u == 0` | Passes silently or fails with clear message |
| 02 | Install packages | Safe-to-repeat | `apt-get install -y` is idempotent by design | No-ops packages already at correct version |
| 02b | Verify Go version | Guard-checked | Go version ≥ 1.22 | Logs current version; fails if below required minimum |
| 03 | Create appuser | Guard-checked | `getent group appuser` / `id appuser` | Logs existing UID/GID; fails if UID/GID mismatch |
| 04 | Create directories | Safe-to-repeat | `install -d` is idempotent | Mode/ownership re-applied; no error if dir exists |
| 05 | Mount loopback storage | Guard-checked | `mountpoint -q /var/lib/app` | Skips image creation and mount; checks fstab entry separately |
| 06 | Configure cgroup slice | Always-overwrites | None | Slice unit rewritten from canonical content; `daemon-reload` always runs |
| 07 | Install config files | Guard-checked | `[[ ! -f /etc/app/config.yaml ]]` | Skips if file exists; does NOT restore drift |
| 08 | Build Go service binary | Always-overwrites | None | Binary always rebuilt and reinstalled |
| 08b | Build lab CLI binary | Always-overwrites | None | Binary always rebuilt and installed to `/usr/local/bin/lab` |
| 09 | Install systemd unit | Always-overwrites | None | Unit file rewritten from canonical content; `daemon-reload` always runs |
| 10 | Generate TLS certificate | Guard-checked | File exists AND `openssl x509 -checkend 0` passes | Skips if cert exists and is not expired |
| 11 | Configure /etc/hosts | Guard-checked | `grep -qF app.local /etc/hosts` | Skips if entry already present |
| 12 | Install nginx config | Always-overwrites | None | Config rewritten from canonical content; `nginx -t` always validated |
| 13 | Install logrotate config | Always-overwrites | None | Config rewritten from canonical content |
| 14 | Configure nftables | Guard-checked | `nft list table inet lab_filter` / `nft list chain LAB-FAULT` | Creates only missing table/chain; existing rules preserved |
| 15 | Configure sudoers | Always-overwrites | None | Sudoers file rewritten; `visudo -c` always validates before install |
| 16 | Enable, start, verify | Safe-to-repeat | `systemctl enable` is idempotent; `restart` always safe | Services restarted; conformance suite always runs |

### 1.3 The Step 07 Exception

Step 07 (config files) uses a guard check rather than always-overwrite. This is intentional: if a learner has intentionally modified `config.yaml` for a problem exercise, a re-run of bootstrap should not silently undo their work.

**Consequence:** if `config.yaml` has drifted from canonical content and bootstrap is re-run, the drift is preserved. This is the correct behavior for learner workflows. For drift recovery, use `lab reset --tier R2` which explicitly restores canonical content.

**R3 reset behavior differs:** when bootstrap runs as part of `lab reset --tier R3`, all config files are restored to canonical content unconditionally before the bootstrap sequence runs. The R3 path is a full teardown-and-rebuild, not a resume.

---

## §2 — Failure Recovery Strategy

### 2.1 The Resume Strategy

The bootstrap script uses a **resume strategy**, not a cleanup strategy. If the script fails at step 8 of 16, re-running the full script from the beginning is safe because:

1. Steps 1–7 have guard checks that detect their work is already done and skip it
2. Step 8 (binary build) is always-overwrite and will retry cleanly
3. Steps 9–16 follow the same pattern

There is no cleanup phase. The script does not attempt to undo partially completed work. This is the correct choice because partial completion leaves the system in a known-partially-provisioned state that the script can resume from, whereas cleanup would require its own idempotency logic and could introduce new failure modes.

### 2.2 Failure Diagnostics

When any step fails, the ERR trap fires and emits:

```
[bootstrap] FAILED at step: <step-name> (exit <code>)
[bootstrap] Diagnosis: journalctl -u app.service -n 20 --no-pager
```

The `CURRENT_STEP` variable tracks the active step by name (e.g., `08-build`, `16-services-and-validate`) so the failure location is unambiguous.

### 2.3 Step-Specific Failure Modes and Recovery

**Step 03 — appuser UID/GID mismatch**

If `appuser` already exists with a different UID or GID than 1001/1001, the script fails with:
```
[bootstrap] FATAL: appuser exists with uid=1002/gid=1002; expected 1001/1001
```
Recovery: remove the existing user (`userdel -r appuser`) and re-run. Do not change the expected UID/GID — they are canonical values that conformance checks P-001 and filesystem ownership checks depend on.

**Step 05 — loopback mount failure**

If `mount` fails (e.g., loop devices exhausted), the image file exists but `/var/lib/app` is not mounted. On re-run, the image existence check prevents a second `mkfs.ext4`, but `mount` is retried. Check available loop devices with `losetup -l` and free one if needed.

If the image file was partially written (e.g., truncated at 32 MiB instead of 50 MiB), `mkfs.ext4` will have failed. Delete the partial image (`rm /var/lib/lab/app-state.img`) and re-run — the script will recreate it.

**Step 08 — binary build failure**

Go build failures leave no partial artifact at `/opt/app/server` (the build uses a temp `GOPATH` and only installs on success). Re-running re-attempts the build cleanly. Common causes: missing `golang-go` package (step 02 failed), missing source at `/opt/lab-env/service/` (repository not cloned to the expected path), or Go version below 1.22.

**Architecture mismatch:** If the script hardcodes `GOARCH=amd64` but the host is `aarch64`, the kernel will reject the binary with `Exec format error`. The script does not cross-compile; it builds for the host architecture by default. If cross-compilation is needed for a specific target, set `GOARCH` explicitly, but the canonical bootstrap builds directly on the target VM and uses the host’s native architecture.

**Step 10 — TLS certificate generation failure**

`openssl req` failure leaves no partial cert file. Re-run retries generation. Verify `openssl` is installed (step 02) and `/etc/nginx/tls/` directory exists (step 04).

**Step 15 — sudoers validation failure**

A `visudo -c` failure means the generated sudoers content has a syntax error. The temp file is deleted before the script exits — no broken sudoers file is ever installed. This failure requires a code fix, not a re-run. Common issues: wildcards (`*`) are not allowed in sudoers command arguments; colons in `chown appuser:appuser` must be escaped (`appuser\:appuser`).

**Step 16 — service startup failure**

If `app.service` fails to start, journald output is printed to stderr automatically. Common causes: binary not built (step 08 failed silently), config file missing (step 07 guard skipped it and it does not exist), or port 8080 already in use. Fix the underlying cause and re-run.

If the service starts but the `/run/app/healthy` readiness file does not appear within 10 seconds, the service is running but not passing its internal startup check. Inspect logs: `journalctl -u app.service -n 50 --no-pager`.

### 2.4 When Re-running Is Not Sufficient

Three conditions require manual intervention before re-running:

1. **appuser UID/GID conflict** — existing user with wrong IDs (see §2.3 Step 03)
2. **Corrupt loopback image** — partial `mkfs.ext4` (see §2.3 Step 05)
3. **Repository not at `/opt/lab-env`** — the script hardcodes this path; cloning elsewhere produces a build failure with no clear error message

### 2.5 R3 Reset vs. Fresh Provisioning

`lab reset --tier R3` calls `bootstrap.sh` internally but first restores all canonical config files to embedded content. This differs from running `bootstrap.sh` directly on a conformant system:

| Scenario | Config files | Binary | Services |
|---|---|---|---|
| Fresh VM, first run | Installed if absent | Built and installed | Started |
| Re-run on conformant system | Skipped (guard check) | Rebuilt and reinstalled | Restarted |
| `lab reset --tier R3` | Restored to canonical unconditionally | Rebuilt and reinstalled | Restarted |

**Important:** After the very first bootstrap on a fresh VM, `lab provision` must be run (or any command that writes `state.json`, such as `lab status`). The bootstrap does not create `/var/lib/lab/state.json` — that file is the control plane’s recorded state, and it is initialised by the first `lab` command that writes it. Until `state.json` exists, commands like `lab fault apply` will fail with `state file not found`.

---

## §3 — Canonical Artifact Specification

The canonical environment is defined by exact file contents, ownership, and mode bits. The control plane embeds canonical file contents at build time (`internal/executor/canonical_files.go`) — the embedded content is the authoritative source, not the files on disk. This section documents what those artifacts must contain and how to verify them.

### 3.1 Canonical Files Subject to R2 Reset

These files are embedded in the `lab` binary and restored by `lab reset --tier R2` / `exec.RestoreFile(path)`. After any R2 reset, these files must exactly match the embedded content.

| Path | Owner | Mode | Restored by |
|---|---|---|---|
| `/etc/app/config.yaml` | appuser:appuser | 0640 | R2 |
| `/etc/systemd/system/app.service` | root:root | 0644 | R2 |
| `/etc/nginx/sites-enabled/app` | root:root | 0644 | R2 |

### 3.2 Non-Embedded Canonical Files

These files are installed by bootstrap but are not embedded in the binary. They are restored by R3 (full reprovision) only.

| Path | Owner | Mode | Notes |
|---|---|---|---|
| `/etc/logrotate.d/app` | root:root | 0644 | copytruncate required |
| `/etc/nginx/tls/app.local.crt` | root:root | 0644 | Regenerated if missing or expired |
| `/etc/nginx/tls/app.local.key` | root:root | 0640 | Regenerated with cert |
| `/etc/app/chaos.env` | appuser:appuser | 0644 | Empty file; must be 0644 (not 0600) so systemd’s EnvironmentFile can read it |
| `/etc/sudoers.d/lab-appuser` | root:root | 0440 | Validated with visudo -c |

### 3.3 Runtime-Created Artifacts

These files are created at runtime by the service or control plane and are not installed by bootstrap. Their presence is verified by conformance checks but their contents are not embedded.

| Path | Owner | Mode | Created by | Check |
|---|---|---|---|---|
| `/var/log/app/app.log` | appuser:appuser | 0640 | Service on startup | L-001, L-002, L-003 |
| `/var/lib/app/state` | appuser:appuser | — | Service on each `GET /` | E-002 (indirectly) |
| `/var/lib/lab/state.json` | root:root | 0644 | `lab` binary — initialised by `lab provision` or `lab status`, not by bootstrap | All commands |
| `/var/lib/lab/audit.log` | root:root | 0644 | `lab` binary | Append-only |
| `/run/app/healthy` | appuser:appuser | — | Service after startup | Step 16 readiness gate |

### 3.4 Directory Structure

| Path | Owner | Mode | Notes |
|---|---|---|---|
| `/opt/app/` | root:root | 0755 | Binary parent |
| `/etc/app/` | root:root | 0755 | Config parent |
| `/var/log/app/` | appuser:appuser | 0755 | Log directory; F-003 check |
| `/var/lib/app/` | appuser:appuser | 0755 | State dir; ext4 loopback mount |
| `/var/lib/lab/` | root:root | 0755 | Control plane state |
| `/etc/nginx/tls/` | root:root | 0755 | TLS artifacts |
| `/run/app/` | appuser:appuser | — | tmpfs; created by systemd RuntimeDirectory |

### 3.5 Binary Artifact Specification

| Property | Value | How verified |
|---|---|---|
| **Service binary** | | |
| Path | `/opt/app/server` | F-001 conformance check |
| Owner | appuser:appuser | F-001 conformance check |
| Mode | 0750 | F-001 conformance check |
| Build command | `CGO_ENABLED=0 go build -o /opt/app/server .` (builds for host architecture — no cross‑compile flags) | Binary runs on target |
| Process name | `server` | P-001 conformance check (`pgrep -u appuser server`) |
| Bind address | `127.0.0.1:8080` | P-002 conformance check |
| **Control plane binary** | | |
| Path | `/usr/local/bin/lab` | `which lab` |
| Owner | root:root | — |
| Mode | 0755 | — |
| Build command | `CGO_ENABLED=0 go build -o /usr/local/bin/lab .` (from repo root; builds for host architecture) | Binary runs on target |

The binaries are rebuilt from source on every R3 reset. There is no pinned binary checksum because they are always compiled locally from the repository source — a checksum would only be meaningful if the binary were distributed as a pre-built artifact, which it is not. Content integrity is provided by the source code, not the binary hash.

### 3.6 Config File Content Verification

After any R2 reset, config file integrity can be verified by comparing against the embedded canonical content:

```bash
# Verify config.yaml matches canonical content
lab fault info F-001   # shows what canonical config.yaml looks like

# Or directly compare against the embedded content in the binary
# (the lab binary always carries the canonical bytes)
lab reset --tier R2 --dry-run   # shows what would be restored (if implemented)
```

The authoritative canonical content for each file is in `internal/config/`:

```
internal/config/config.yaml          → /etc/app/config.yaml
internal/config/app.service          → /etc/systemd/system/app.service
internal/config/nginx.conf           → /etc/nginx/sites-enabled/app
internal/config/logrotate.conf       → /etc/logrotate.d/app
```

Any drift between the on-disk file and the embedded content constitutes a conformance violation. The control plane detects this indirectly through conformance checks — for example, a modified `config.yaml` that changes the bind address will cause P-002 to fail after a service restart.

### 3.7 Loopback Storage Specification

| Property | Value |
|---|---|
| Image path | `/var/lib/lab/app-state.img` |
| Size | 50 MiB sparse |
| Filesystem | ext4 |
| Mount point | `/var/lib/app/` |
| fstab entry | `<image> /var/lib/app ext4 loop,defaults 0 2` |
| Post-mount ownership | appuser:appuser 0755 |

The loopback mount simulates exhaustible storage. Faults F-018 (inode exhaustion) fill the inode table without filling the data blocks — `df -h` shows space available while `df -i` shows 100% inode usage. This is the canonical diagnostic pattern for distinguishing block exhaustion from inode exhaustion.

---

## §4 — Provisioning Sequence Specification

The authoritative step sequence with guard conditions and idempotency behavior, for reference when reading or modifying `scripts/bootstrap.sh`.

### Step 01 — Root check
**Purpose:** fail fast with a clear message rather than failing later with a cryptic permission error.
**Guard:** `id -u == 0`
**Idempotency:** N/A — guard or fail.

### Step 02 — Install packages
**Purpose:** ensure all required system tools are present.
**Guard:** `apt-get install -y` — package manager handles idempotency.
**Note:** if a package version changes between runs, the newer version is installed. This is intentional.

### Step 02b — Verify Go version
**Purpose:** ensure the installed Go version is at least 1.22 (required by the service module).
**Guard:** `go version` output is parsed and compared against a minimum major/minor version.
**Failure:** if Go is too old, the script fails with a clear message directing to manual installation from `go.dev/dl/`.
**Idempotency:** the check runs every bootstrap; it will fail again if Go has not been upgraded.

### Step 03 — Create appuser
**Purpose:** create the service user with fixed UID 1001 / GID 1001.
**Guard:** `getent group appuser` for the group; `id appuser` for the user.
**Failure condition:** user exists with wrong UID/GID — script fails hard. Do not silently reuse a different UID/GID; conformance checks bind to the username, not the UID, but a UID mismatch indicates the system has a conflicting user that must be resolved manually.

### Step 04 — Create directory structure
**Purpose:** establish all directories with canonical ownership and modes.
**Guard:** `install -d` is inherently idempotent.
**Note:** `/run/app` is NOT created here. It is managed by systemd via `RuntimeDirectory=app` in the unit file, which creates it as a tmpfs on service start and removes it on stop.

### Step 05 — Mount loopback storage
**Purpose:** mount a 50 MiB ext4 image at `/var/lib/app` to provide exhaustible storage for inode/block fault exercises.
**Guard:** `mountpoint -q /var/lib/app` for the mount; `grep -qF image /etc/fstab` for persistence.
**Post-mount:** ownership is re-applied after mount because mounting resets the directory ownership to root.

### Step 06 — Configure cgroup slice
**Purpose:** install `app.slice` unit with `MemoryMax=256M CPUQuota=20%`.
**Guard:** none — always overwrites.
**Rationale:** slice configuration is always restored to canonical values. Drift in the slice definition would affect fault behavior (F-008, F-014 depend on the memory constraint) without triggering any conformance check failure.

### Step 07 — Install config files
**Purpose:** place `config.yaml` and `chaos.env` at their canonical paths.
**Guard:** `[[ ! -f path ]]` — skips if file exists.
**Important:** this step does NOT restore drift. See §1.3 for rationale.

### Step 08 — Build Go service binary
**Purpose:** compile the service from source and install at `/opt/app/server`.
**Guard:** none — always rebuilds.
**Build command:** `CGO_ENABLED=0 go build -o /opt/app/server .` (no cross‑compile flags — builds for the host architecture).
**Rationale:** the binary is always rebuilt to ensure it reflects current source. Drift between the binary and source (e.g., from a partial F-008 recovery) is always corrected.

### Step 08b — Build lab CLI binary
**Purpose:** compile the control plane CLI from the repository root and install at `/usr/local/bin/lab`.
**Guard:** none — always rebuilds.
**Build command:** `CGO_ENABLED=0 go build -o /usr/local/bin/lab .` (from repo root, builds for host architecture).
**Rationale:** like the service binary, the CLI is always rebuilt from source to ensure it reflects the current code. This step was added after discovering that the original bootstrap only built the service binary, leaving `lab` unavailable.

### Step 09 — Install systemd unit
**Purpose:** install `app.service` unit file.
**Guard:** none — always overwrites.
**Rationale:** the unit file is embedded canonical content. Always restoring it ensures F-013 (unit file syntax error) and F-006 (APP_ENV removed) are always cleanly recovered by R3.

### Step 10 — Generate TLS certificate
**Purpose:** generate a self-signed certificate for `app.local`.
**Guard:** file exists AND `openssl x509 -checkend 0` passes (cert not expired).
**Validity:** 365 days. If the cert expires, the next bootstrap run automatically regenerates it.

### Step 11 — Configure /etc/hosts
**Purpose:** add `127.0.0.1 app.local` so E-005 (HTTPS via app.local) and F-007 (DNS resolution) pass.
**Guard:** `grep -qF app.local /etc/hosts`.

### Step 12 — Install nginx configuration
**Purpose:** install the canonical nginx reverse proxy configuration.
**Guard:** none — always overwrites.
**Validation:** `nginx -t` is run after installation; if syntax validation fails, the script exits before any reload.

### Step 13 — Install logrotate configuration
**Purpose:** install `copytruncate` logrotate configuration for `app.log`.
**Guard:** none — always overwrites.

### Step 14 — Configure nftables
**Purpose:** create the `inet lab_filter` table and `LAB-FAULT` chain used by network-layer faults.
**Guard:** `nft list table` / `nft list chain` — creates only what is missing.
**Note:** the chain is created with `policy accept` (default allow). Fault application adds rules to this chain; recovery flushes it. Bootstrap never adds rules.

### Step 15 — Configure sudoers
**Purpose:** grant `appuser` passwordless sudo for the exact commands needed for fault apply/recover.
**Guard:** none — always overwrites.
**Safety:** `visudo -c` validates the generated content before installation. A syntax error in the generated sudoers aborts the script without installing the broken file. The sudoers rules use explicit paths and escaped colons (no wildcards).

### Step 16 — Enable, start, verify
**Purpose:** enable and start both services; poll for service readiness; run the conformance suite.
**Guard:** `systemctl enable` is idempotent; `restart` is always issued.
**Readiness gate:** polls `/run/app/healthy` for up to 10 seconds (20 × 500ms). This file is written by the service after its startup sequence completes.
**Final gate:** `validate.sh` must exit 0. If validation fails, bootstrap exits 1 — provisioning is not considered complete. After a successful bootstrap, `lab provision` should be run to initialise `state.json`.