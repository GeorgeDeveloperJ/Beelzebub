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
	scrollOffset int
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
			Padding(0, 1)
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
		scrollOffset: 0,
		daemonStatus: checkDaemonStatus(),
		statusMsg:    "Ready. Use [j/k/arrows] to scroll, [space/r] to check, [x] to remediate, [c] to clear, [q] to quit.",
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

		events, _ := m.store.GetRecentEvents(6)
		return dataRefreshMsg{
			states:       states,
			events:       events,
			daemonStatus: checkDaemonStatus(),
		}
	}
}

// maxVisibleRows calculates how many sentinel rows fit dynamically in the terminal window.
func (m *Model) maxVisibleRows() int {
	if m.height <= 0 {
		return 10
	}
	// Fixed vertical footprint:
	// Top bar: 2 lines
	// Fleet header & columns: 2 lines
	// Spacing: 1 line
	// Incident header & logs: 1 + 6 = 7 lines
	// Spacing: 1 line
	// Status message: 1 line
	// Controls bar: 3 lines
	// Container borders/padding: 2 lines
	// Centering safety margin: 2 lines
	// Total overhead: ~21 lines
	available := m.height - 21
	if available < 5 {
		return 5
	}
	if available > 25 {
		return 25
	}
	return available
}

// clampScroll keeps the cursor within the current visible scroll window.
func (m *Model) clampScroll() {
	if len(m.sentinels) == 0 {
		m.cursor = 0
		m.scrollOffset = 0
		return
	}
	if m.cursor >= len(m.sentinels) {
		m.cursor = len(m.sentinels) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	visible := m.maxVisibleRows()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	} else if m.cursor >= m.scrollOffset+visible {
		m.scrollOffset = m.cursor - visible + 1
	}

	maxOffset := len(m.sentinels) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// Update processes incoming Bubbletea messages and keyboard events.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()
		return m, nil

	case TickMsg:
		return m, tea.Batch(m.refreshDataCmd(), tickCmd())

	case dataRefreshMsg:
		m.sentinels = msg.states
		m.events = msg.events
		m.daemonStatus = msg.daemonStatus
		m.clampScroll()
		return m, nil

	case ActionMsg:
		m.statusMsg = string(msg)
		return m, m.refreshDataCmd()

	case tea.KeyMsg:
		visible := m.maxVisibleRows()
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.clampScroll()
			}

		case "down", "j":
			if m.cursor < len(m.sentinels)-1 {
				m.cursor++
				m.clampScroll()
			}

		case "pgup":
			m.cursor -= visible
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.clampScroll()

		case "pgdown":
			m.cursor += visible
			if m.cursor >= len(m.sentinels) {
				m.cursor = len(m.sentinels) - 1
			}
			m.clampScroll()

		case "home", "g":
			m.cursor = 0
			m.clampScroll()

		case "end", "G":
			if len(m.sentinels) > 0 {
				m.cursor = len(m.sentinels) - 1
			}
			m.clampScroll()

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
	visible := m.maxVisibleRows()
	totalSentinels := len(m.sentinels)
	headerText := fmt.Sprintf("── SENTINEL FLEET OVERVIEW (%d total) ", totalSentinels)
	if totalSentinels > visible {
		headerText = fmt.Sprintf("── SENTINEL FLEET OVERVIEW [%d-%d of %d] ", m.scrollOffset+1, min(m.scrollOffset+visible, totalSentinels), totalSentinels)
	}
	ruleFiller := strings.Repeat("─", max(2, 75-len(headerText)))
	sb.WriteString(headerStyle.Render(headerText+ruleFiller) + "\n")

	if totalSentinels == 0 {
		sb.WriteString("  No sentinels found in SQLite database.\n")
	} else {
		// Fixed column widths:
		// SENTINEL: 26, HEALTH: 12, RETRIES: 8, LAST CHECK: 18, DURATION: 10
		colHeader := fmt.Sprintf("  %-26s %-12s %-8s %-18s %-10s", "SENTINEL", "HEALTH", "RETRIES", "LAST CHECK", "DURATION")
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#AAAAAA")).Render(colHeader) + "\n")

		end := m.scrollOffset + visible
		if end > totalSentinels {
			end = totalSentinels
		}

		for idx := m.scrollOffset; idx < end; idx++ {
			s := m.sentinels[idx]

			// Raw status text and style
			statusText := "HEALTHY"
			statusStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF66"))

			switch s.Status {
			case store.StatusPending:
				statusText = "PENDING"
				statusStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#AAAAAA"))
			case store.StatusRemediating:
				statusText = "DEGRADED"
				statusStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFA500"))
			case store.StatusExhausted:
				statusText = "CRITICAL"
				statusStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF3333"))
			}

			lastTime := "never"
			if !s.LastCheckedAt.IsZero() {
				lastTime = s.LastCheckedAt.Local().Format("15:04:05")
			}

			sentinelName := s.SentinelName
			if len(sentinelName) > 25 {
				sentinelName = sentinelName[:22] + "..."
			}

			// Format columns with exact widths
			colName := fmt.Sprintf("%-26s", sentinelName)
			colHealth := fmt.Sprintf("%-12s", statusText)
			colRetries := fmt.Sprintf("%-8d", s.ConsecutiveFailures)
			colLastCheck := fmt.Sprintf("%-18s", lastTime)
			colDuration := fmt.Sprintf("%-10s", fmt.Sprintf("%dms", s.LastDurationMs))

			if idx == m.cursor {
				cursorRow := fmt.Sprintf("▶ %s %s %s %s %s", colName, colHealth, colRetries, colLastCheck, colDuration)
				sb.WriteString(selectedStyle.Render(cursorRow) + "\n")
			} else {
				styledHealth := statusStyle.Render(colHealth)
				regularRow := fmt.Sprintf("  %s %s %s %s %s", colName, styledHealth, colRetries, colLastCheck, colDuration)
				sb.WriteString(regularRow + "\n")
			}
		}

		if totalSentinels > visible {
			scrollIndicator := fmt.Sprintf("  ▲▼ [Use j/k or PgUp/PgDn to scroll • %d more]", totalSentinels-visible)
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render(scrollIndicator) + "\n")
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
			if len(out) > 55 {
				out = out[:52] + "..."
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

	// Centering: Center horizontally and vertically within terminal window
	contentHeight := lipgloss.Height(content)
	contentWidth := lipgloss.Width(content)

	vPos := lipgloss.Center
	if m.height <= contentHeight {
		vPos = lipgloss.Top
	}

	hPos := lipgloss.Center
	if m.width <= contentWidth {
		hPos = lipgloss.Left
	}

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, hPos, vPos, content)
	}

	return content
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
