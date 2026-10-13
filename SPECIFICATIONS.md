# Project Specification: Self-Healing Homelab Sentinel & Operator TUI (Beelzebub)

## 1. Executive Summary & Objective

Build a lightweight, deterministic self-healing background daemon and companion terminal dashboard (TUI) for a Linux homelab server.

Instead of passive resource monitoring, the system acts as an **Autonomous Site Reliability Engineering (SRE) Cockpit**:

1. Continuously evaluates user-defined health probes in the background.
2. Executes deterministic remediation playbooks when failures occur.
3. Logs incidents and remediation outcomes to an internal SQLite database.
4. Alerts the admin via webhooks (Telegram/Discord/ntfy) when interventions succeed or exhaust retries.
5. Provides an interactive, keyboard-driven TUI when accessing the server via SSH to inspect incident history, force manual checks, or trigger immediate fixes.

---

## 2. Technical Stack & Architecture

- **Language & Runtime:** Go (compiles to a single static binary with negligible CPU and RAM overhead).
- **TUI Framework:** Charmbracelet ecosystem (`bubbletea`, `lipgloss`, `bubbles`).
- **Config Format:** YAML (`sentinels.yaml`).
- **State & Audit Storage:** Local SQLite database via modern pure-Go driver (e.g., `modernc.org/sqlite` for zero-CGO static builds).
- **Execution Modes:**
- `sentinel daemon`: Runs headlessly as a systemd service.
- `sentinel tui` (or default naked binary invocation): Launches the interactive terminal UI.

---

## 3. Core Components

### A. The Declarative Engine (`sentinels.yaml`)

Probes and remediations are defined via human-readable declarative rules.

```yaml
version: "1"
global:
  check_interval_default: "60s"
  notifications:
    provider: "ntfy" # or "discord", "telegram"
    endpoint: "https://ntfy.sh/your-topic-here"
    alert_on: ["healed", "exhausted"] # suppress normal pass checks

sentinels:
  - name: "media-storage-mount"
    interval: "30s"
    timeout: "5s"
    probe:
      type: "shell"
      command: "test -f /mnt/media/.canary"
    remediation:
      max_retries: 3
      backoff: "10s"
      command: "mount -a || systemctl restart autofs"
    on_exhausted: "Failed to remount /mnt/media after 3 attempts."

  - name: "wan-speedtest-gateway"
    interval: "30m"
    timeout: "60s"
    probe:
      type: "threshold"
      # Returns integer or float megabits
      command: "speedtest --format=json | jq '.download.bandwidth / 125000'"
      min_threshold: 50.0 # Alert if bandwidth drops below 50 Mbps
    remediation:
      max_retries: 2
      backoff: "15s"
      command: "systemctl restart wg-quick@wg0" # example network interface reset
    on_exhausted: "WAN speed remained degraded below 50 Mbps."
```

### B. Daemon Runtime & State Machine

1. **Probe Execution:** Runs the `probe.command` in an isolated shell execution context with strict timeouts.
2. **Failure Counter & Backoff:** Tracks consecutive failure counts in SQLite.
3. **Remediation Trigger:** If a probe fails, invoke `remediation.command`.
4. **Resolution / Escalation:**

- If remediation succeeds on check retry: Log resolution, send `healed` notification, reset retry counter to `0`.
- If failure reaches `max_retries`: Set sentinel state to `CRITICAL/EXHAUSTED`, send high-priority alert notification, cease automatic re-runs until reset.

1. **Audit Trail Schema (SQLite):**

- `events`: `id`, `sentinel_name`, `timestamp`, `status` (PASS, REMEDIATING, HEALED, EXHAUSTED), `stdout`, `stderr`, `duration_ms`.

### C. Operator TUI Cockpit

When logging into the machine via SSH, the TUI presents three main visual panels:

1. **Sentinel Fleet Overview:**

- Live list of all defined sentinels, current health state (`HEALTHY`, `DEGRADED`, `CRITICAL`), last check timestamp, and last check duration.

1. **Audit / Incident Log Stream:**

- Real-time tail of recent automated events and shell outputs showing exactly what was fixed in the background.

1. **Interactive Control Bar:**

- `[space]` / `[r]`: Manually force an instant run of the selected probe.
- `[x]`: Manually force run the remediation playbook on demand.
- `[s]`: Trigger on-demand interactive speedtest or diagnostic scripts.
- `[c]`: Clear/reset exhausted state counters.
- `[q]`: Exit cleanly.

---

## 4. Implementation Milestones for Agents

1. **Milestone 1 — Config Parser & Schema Validation:**

- Implement Go structs matching the YAML spec with validation for timeouts, intervals, and required fields.

1. **Milestone 2 — Execution Engine & SQLite Logger:**

- Implement the shell runner, timeout context handling, retry state machine, and SQLite migration/event logging.

1. **Milestone 3 — Notification Dispatcher:**

- Add lightweight HTTP client dispatch for Discord, Telegram, or ntfy webhooks.

1. **Milestone 4 — Bubbletea TUI Cockpit:**

- Build the UI views (Fleet Table, Event History viewport, bottom keybinding bar) reading directly from the SQLite store and IPC/shared engine.

1. **Milestone 5 — Systemd Integration & Packaging:**

- Produce standard `systemd` service unit file (`sentinel.service`) and build script producing a standalone stripped binary.
