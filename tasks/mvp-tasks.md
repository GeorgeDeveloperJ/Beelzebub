# Beelzebub MVP Task Tracker

## Specification Summary
Self-Healing Homelab Sentinel & Operator TUI (Beelzebub).
Single static Go binary (`sentinel daemon`, `sentinel tui`), Charmbracelet ecosystem (`bubbletea`, `lipgloss`), SQLite event auditing (`modernc.org/sqlite`), and webhook alert dispatcher.

## Milestones Status

### [x] Milestone 1: Config Parser & Schema Validation
- Go struct models for `sentinels.yaml` in `pkg/config/config.go`.
- Custom `Duration` parser for human strings (`30s`, `15m`, `1h`).
- Semantic invariant validator in `pkg/config/validate.go`.
- Table-driven unit test matrix in `pkg/config/config_test.go`.

### [x] Milestone 2: Execution Engine & SQLite Logger
- Pure-Go SQLite event logger & state store in `pkg/store/store.go`.
- Shell runner with timeout context & threshold evaluation in `pkg/runner/runner.go`.
- Autonomous background scheduler & remediation state machine in `pkg/engine/engine.go`.
- Unit test suites for store, runner, and engine.

### [x] Milestone 3: Webhook Notification Dispatcher
- HTTP client dispatcher supporting `ntfy`, `discord`, and `telegram` in `pkg/notifier/notifier.go`.
- Dispatches on `healed` and `exhausted` states with event suppression.
- Unit test suite with mock HTTP server in `pkg/notifier/notifier_test.go`.

### [x] Milestone 4: Bubbletea Operator TUI Cockpit
- Interactive Charmbracelet TUI in `pkg/tui/tui.go`.
- Panel 1: Sentinel Fleet Overview with color-coded health badges.
- Panel 2: Real-time incident audit log stream.
- Panel 3: Keybinding control bar (`[space/r]` force check, `[x]` remediate, `[s]` diagnostic, `[c]` reset counters, `[q]` quit).

### [x] Milestone 5: Systemd Packaging & Static Compilation
- CLI entrypoint supporting `beelzebub daemon` and `beelzebub tui` in `cmd/beelzebub/main.go`.
- POSIX signal trapping (`SIGINT`, `SIGTERM`, `SIGHUP`) for clean daemon shutdown.
- Systemd service unit in `packaging/beelzebub.service`.
- Standalone stripped static build script in `scripts/build.sh` producing `dist/beelzebub`.
- Automated installer in `install.sh` managing `beelzebub.service`.
- End-to-end reality check verification.
