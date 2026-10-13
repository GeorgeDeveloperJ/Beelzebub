package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/engine"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TickMsg triggers periodic polling of the SQLite database.
type TickMsg time.Time

// ActionMsg displays asynchronous user action results in the status bar.
type ActionMsg string

// Model represents the Bubbletea TUI state.
type Model struct {
	cfg          *config.Config
	store        *store.Store
	engine       *engine.Engine
	width        int
	height       int
	cursor       int
	sentinels    []store.SentinelState
	events       []store.Event
	statusMsg    string
	daemonStatus string
	err          error
}

// Styles using Lipgloss
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4"))

	borderStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5A5A5A")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#58C1FF"))

	healthyBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF66")).
			SetString("HEALTHY")

	degradedBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFA500")).
			SetString("DEGRADED")

	criticalBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF3333")).
			SetString("CRITICAL")

	pendingBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#AAAAAA")).
			SetString("PENDING")

	keyBindingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00E5FF")).
			Bold(true)

	mainContainerStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)
)

// checkDaemonStatus inspects systemd and running processes to determine daemon liveness.
func checkDaemonStatus() string {
	if err := exec.Command("systemctl", "is-active", "--quiet", "beelzebub.service").Run(); err == nil {
		return "🟢 DAEMON: ACTIVE (systemd)"
	}
	if err := exec.Command("systemctl", "is-active", "--quiet", "sentinel.service").Run(); err == nil {
		return "🟢 DAEMON: ACTIVE (systemd)"
	}
	out, err := exec.Command("pgrep", "-f", "beelzebub daemon").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return "🟢 DAEMON: ACTIVE (process)"
	}
	return "⚪ DAEMON: INACTIVE (standalone)"
}

// New creates a new Bubbletea TUI Model.
func New(cfg *config.Config, s *store.Store, eng *engine.Engine) *Model {
	return &Model{
		cfg:          cfg,
		store:        s,
		engine:       eng,
		cursor:       0,
		daemonStatus: checkDaemonStatus(),
		statusMsg:    "Ready. Use [j/k] to select, [space/r] to check, [x] to remediate, [c] to clear, [q] to quit.",
	}
}

// Init triggers the initial tick for data refresh.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.refreshDataCmd(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

type dataRefreshMsg struct {
	states       []store.SentinelState
	events       []store.Event
	daemonStatus string
}

func (m *Model) refreshDataCmd() tea.Cmd {
	return func() tea.Msg {
		// Read states and recent events
		states, _ := m.store.GetAllSentinelStates()

		// If DB has no states yet, but config has sentinels, seed them so the fleet table is populated
		if len(states) == 0 && m.cfg != nil && len(m.cfg.Sentinels) > 0 {
			for _, sc := range m.cfg.Sentinels {
				_ = m.store.UpdateSentinelState(&store.SentinelState{
					SentinelName:        sc.Name,
					Status:              store.StatusPending,
					ConsecutiveFailures: 0,
					LastOutput:          "Pending initial check",
				})
			}
			states, _ = m.store.GetAllSentinelStates()
		}

		events, _ := m.store.GetRecentEvents(12)
		return dataRefreshMsg{
			states:       states,
			events:       events,
			daemonStatus: checkDaemonStatus(),
		}
	}
}

// Update processes incoming Bubbletea messages and keyboard events.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case TickMsg:
		return m, tea.Batch(m.refreshDataCmd(), tickCmd())

	case dataRefreshMsg:
		m.sentinels = msg.states
		m.events = msg.events
		m.daemonStatus = msg.daemonStatus
		if m.cursor >= len(m.sentinels) && len(m.sentinels) > 0 {
			m.cursor = len(m.sentinels) - 1
		}
		return m, nil

	case ActionMsg:
		m.statusMsg = string(msg)
		return m, m.refreshDataCmd()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.sentinels)-1 {
				m.cursor++
			}

		case " ", "r": // Force manual probe check
			if len(m.sentinels) > 0 && m.cursor < len(m.sentinels) {
				name := m.sentinels[m.cursor].SentinelName
				m.statusMsg = fmt.Sprintf("Running check on %s...", name)
				return m, func() tea.Msg {
					st, err := m.engine.ForceCheck(context.Background(), name)
					if err != nil {
						return ActionMsg(fmt.Sprintf("Check failed: %v", err))
					}
					return ActionMsg(fmt.Sprintf("Check completed for %s: %s (took %dms)", name, st.Status, st.LastDurationMs))
				}
			}

		case "x": // Force manual remediation
			if len(m.sentinels) > 0 && m.cursor < len(m.sentinels) {
				name := m.sentinels[m.cursor].SentinelName
				m.statusMsg = fmt.Sprintf("Running remediation on %s...", name)
				return m, func() tea.Msg {
					res, err := m.engine.ForceRemediation(context.Background(), name)
					if err != nil {
						return ActionMsg(fmt.Sprintf("Remediation error: %v", err))
					}
					return ActionMsg(fmt.Sprintf("Remediation ran for %s: exit %d (took %v)", name, res.ExitCode, res.Duration))
				}
			}

		case "c": // Clear/reset counters
			if len(m.sentinels) > 0 && m.cursor < len(m.sentinels) {
				name := m.sentinels[m.cursor].SentinelName
				_ = m.engine.ResetSentinel(name)
				m.statusMsg = fmt.Sprintf("Cleared counters and reset state for %s", name)
				return m, m.refreshDataCmd()
			}

		case "s": // On-demand diagnostic
			if len(m.sentinels) > 0 && m.cursor < len(m.sentinels) {
				name := m.sentinels[m.cursor].SentinelName
				m.statusMsg = fmt.Sprintf("Triggered instant diagnostics for %s...", name)
				return m, func() tea.Msg {
					st, _ := m.engine.ForceCheck(context.Background(), name)
					out := st.LastOutput
					if len(out) > 50 {
						out = out[:50] + "..."
					}
					return ActionMsg(fmt.Sprintf("Diagnostic result: %s - %s", st.Status, out))
				}
			}
		}
	}

	return m, nil
}

// View renders the terminal dashboard.
func (m *Model) View() string {
	var sb strings.Builder

	// Top Title Bar with Live Daemon Indicator
	title := titleStyle.Render("🛡️  BEELZEBUB — Autonomous SRE Sentinel Cockpit")
	timeStr := time.Now().Format("2006-01-02 15:04:05 MST")
	daemonColor := "#00FF66"
	if strings.Contains(m.daemonStatus, "INACTIVE") {
		daemonColor = "#FFA500"
	}
	daemonBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(daemonColor)).Render(m.daemonStatus)
	topBar := lipgloss.JoinHorizontal(lipgloss.Center, title, "   ", daemonBadge, "   ", lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(timeStr))
	sb.WriteString(topBar + "\n\n")

	// Panel 1: Sentinel Fleet Overview
	sb.WriteString(headerStyle.Render("── SENTINEL FLEET OVERVIEW ────────────────────────────────────────────────") + "\n")
	if len(m.sentinels) == 0 {
		sb.WriteString("  No sentinels found in SQLite database.\n")
	} else {
		// Header row
		colHeader := fmt.Sprintf("  %-25s %-12s %-10s %-20s %-10s", "SENTINEL", "HEALTH", "RETRIES", "LAST CHECK", "DURATION")
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#AAAAAA")).Render(colHeader) + "\n")

		for i, s := range m.sentinels {
			badge := healthyBadge.String()
			if s.Status == store.StatusPending {
				badge = pendingBadge.String()
			} else if s.Status == store.StatusRemediating {
				badge = degradedBadge.String()
			} else if s.Status == store.StatusExhausted {
				badge = criticalBadge.String()
			}

			lastTime := "never"
			if !s.LastCheckedAt.IsZero() {
				lastTime = s.LastCheckedAt.Local().Format("15:04:05")
			}

			durStr := fmt.Sprintf("%dms", s.LastDurationMs)
			retriesStr := fmt.Sprintf("%d", s.ConsecutiveFailures)

			row := fmt.Sprintf("  %-25s %-12s %-10s %-20s %-10s", s.SentinelName, badge, retriesStr, lastTime, durStr)

			if i == m.cursor {
				sb.WriteString(selectedStyle.Render(row) + "\n")
			} else {
				sb.WriteString(row + "\n")
			}
		}
	}
	sb.WriteString("\n")

	// Panel 2: Incident & Audit Log Stream
	sb.WriteString(headerStyle.Render("── RECENT INCIDENT & AUDIT STREAM ─────────────────────────────────────────") + "\n")
	if len(m.events) == 0 {
		sb.WriteString("  No audit events recorded yet.\n")
	} else {
		for _, e := range m.events {
			statusColor := "#00FF66"
			if e.Status == store.StatusRemediating {
				statusColor = "#FFA500"
			} else if e.Status == store.StatusExhausted {
				statusColor = "#FF3333"
			} else if e.Status == store.StatusHealed {
				statusColor = "#00E5FF"
			}

			tag := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(statusColor)).Render(fmt.Sprintf("[%-11s]", e.Status))
			tStr := e.Timestamp.Local().Format("15:04:05")
			out := e.Stdout
			if out == "" && e.Stderr != "" {
				out = e.Stderr
			}
			if len(out) > 60 {
				out = out[:57] + "..."
			}

			logLine := fmt.Sprintf("  %s %s %-20s %s (%dms)", tStr, tag, e.SentinelName, out, e.DurationMs)
			sb.WriteString(logLine + "\n")
		}
	}
	sb.WriteString("\n")

	// Status Message
	if m.statusMsg != "" {
		sb.WriteString("  " + statusBarStyle.Render(m.statusMsg) + "\n\n")
	}

	// Panel 3: Interactive Keybinding Control Bar
	controls := "  [space/r] Force Check  •  [x] Run Remediation  •  [s] Diagnostic  •  [c] Reset Counters  •  [q] Quit"
	sb.WriteString(borderStyle.Render(keyBindingStyle.Render(controls)))

	content := mainContainerStyle.Render(sb.String())

	// Centering: If terminal dimensions are received, center in screen
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	return content
}
