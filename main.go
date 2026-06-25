package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"quota-tui/api"
)

// ─── Messages ────────────────────────────────────────────────────────────────

type tickMsg time.Time

type refreshMsg struct {
	claude api.ClaudeQuota
	codex  api.CodexQuota
}

// ─── Model ───────────────────────────────────────────────────────────────────

type model struct {
	claude      api.ClaudeQuota
	codex       api.CodexQuota
	width       int
	height      int
	lastRefresh time.Time
	nextRefresh time.Time
	refreshing  bool
	now         time.Time
}

func initialModel() model {
	return model{
		width:  100,
		height: 30,
		now:    time.Now(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.Every(time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
		fetchData,
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		// Auto-refresh every 5 minutes to prevent bans
		if !m.refreshing && !m.lastRefresh.IsZero() && m.now.Sub(m.lastRefresh) >= 5*time.Minute {
			m.refreshing = true
			return m, fetchData
		}
		return m, nextTick()

	case refreshMsg:
		m.claude = msg.claude
		m.codex = msg.codex
		m.lastRefresh = time.Now()
		m.nextRefresh = m.lastRefresh.Add(5 * time.Minute)
		m.refreshing = false
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.refreshing {
				m.refreshing = true
				return m, fetchData
			}
		}
	}
	return m, nil
}

func nextTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchData() tea.Msg {
	claudeCh := make(chan api.ClaudeQuota, 1)
	codexCh := make(chan api.CodexQuota, 1)

	go func() { claudeCh <- api.FetchClaudeQuota() }()
	go func() { codexCh <- api.FetchCodexQuota() }()

	return refreshMsg{
		claude: <-claudeCh,
		codex:  <-codexCh,
	}
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m model) View() tea.View {
	content := m.render()
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m model) render() string {
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Main panels
	panelWidth := m.panelWidth()
	claudePanel := m.renderClaudePanel(panelWidth)
	codexPanel := m.renderCodexPanel(panelWidth)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, claudePanel, "  ", codexPanel))
	b.WriteString("\n")

	// Footer
	b.WriteString(m.renderFooter())

	// Center everything
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(b.String())
}

func (m model) panelWidth() int {
	w := (m.width - 6) / 2 // 6 = gap + margins
	if w < 38 {
		w = 38
	}
	if w > 56 {
		w = 56
	}
	return w
}

// ─── Header ──────────────────────────────────────────────────────────────────

func (m model) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E0AAFF")).
		Background(lipgloss.Color("#1A1A2E")).
		Padding(0, 2)

	title := titleStyle.Render("⚡ AI Quota Monitor")

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6C757D")).
		Italic(true)

	clock := timeStyle.Render(m.now.Format("2006-01-02 15:04:05"))

	refreshStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4CC9F0"))

	var refreshInfo string
	if m.refreshing {
		refreshInfo = refreshStyle.Render("⟳ Refreshing...")
	} else if !m.lastRefresh.IsZero() {
		remaining := m.nextRefresh.Sub(m.now)
		if remaining < 0 {
			remaining = 0
		}
		refreshInfo = refreshStyle.Render(fmt.Sprintf("↻ Next refresh in %ds", int(remaining.Seconds())))
	} else {
		refreshInfo = refreshStyle.Render("⟳ Loading...")
	}

	headerWidth := m.width - 4
	if headerWidth < 60 {
		headerWidth = 60
	}

	line := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#2D2D44")).
		Render(strings.Repeat("─", headerWidth))

	header := lipgloss.NewStyle().
		Width(headerWidth).
		Align(lipgloss.Center).
		Render(lipgloss.JoinVertical(lipgloss.Center,
			title,
			lipgloss.JoinHorizontal(lipgloss.Center, clock, "    ", refreshInfo),
			line,
		))

	return header
}

// ─── Claude Panel ────────────────────────────────────────────────────────────

func (m model) renderClaudePanel(width int) string {
	inner := width - 4 // Border padding

	if m.claude.Error != "" && m.lastRefresh.IsZero() && m.claude.FiveHourPercent == 0 {
		return m.renderErrorPanel("🟣 Claude Code", "Pro", m.claude.Error, width, "#9D4EDD")
	}
	if m.claude.Error != "" {
		return m.renderErrorPanel("🟣 Claude Code", "Pro", m.claude.Error, width, "#9D4EDD")
	}

	var parts []string

	// Title bar
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E0AAFF")).
		Width(inner).
		Align(lipgloss.Center)
	titleText := "🟣 Claude Code"
	if m.claude.IsCached {
		titleText = "🟣 Claude Code [⚠️ Rate Limited]"
	}
	parts = append(parts, titleStyle.Render(titleText))

	// Plan badge
	planStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1A1A2E")).
		Background(lipgloss.Color("#9D4EDD")).
		Bold(true).
		Padding(0, 2)
	planBadge := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Render(planStyle.Render("PRO"))
	parts = append(parts, planBadge)

	parts = append(parts, "")

	// 5-hour window
	parts = append(parts, m.renderQuotaBlock(
		"⏱  5-Hour Window",
		m.claude.FiveHourPercent,
		m.claude.FiveHourResetAt,
		m.claude.FiveHourSeverity,
		inner,
	)...)

	if m.claude.WeeklyPercent >= 0 {
		parts = append(parts, "")

		// Weekly window
		parts = append(parts, m.renderQuotaBlock(
			"📅 Weekly Window",
			m.claude.WeeklyPercent,
			m.claude.WeeklyResetAt,
			m.claude.WeeklySeverity,
			inner,
		)...)
	}

	// Extra usage indicator
	if m.claude.ExtraUsage {
		parts = append(parts, "")
		extraStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4CC9F0")).
			Width(inner).
			Align(lipgloss.Center)
		parts = append(parts, extraStyle.Render("💎 Extra Usage Enabled"))
	}

	if m.claude.CostUSD > 0 || m.claude.WeeklyCostUSD > 0 {
		parts = append(parts, "")
		costStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#20C997")).
			Bold(true).
			Width(inner).
			Align(lipgloss.Center)
		parts = append(parts, costStyle.Render(fmt.Sprintf("💰 5h Window: $%.2f  |  Weekly: $%.2f", m.claude.CostUSD, m.claude.WeeklyCostUSD)))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return lipgloss.NewStyle().
		Width(width).
		Height(panelHeight()).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#9D4EDD")).
		Padding(1, 1).
		Render(content)
}

// ─── Codex Panel ─────────────────────────────────────────────────────────────

func (m model) renderCodexPanel(width int) string {
	inner := width - 4

	if m.codex.Error != "" {
		plan := m.codex.PlanType
		if plan == "" {
			plan = "Plus"
		}
		return m.renderErrorPanel("🟢 Codex CLI", strings.Title(plan), m.codex.Error, width, "#06D6A0")
	}

	var parts []string

	// Title bar
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#80FFDB")).
		Width(inner).
		Align(lipgloss.Center)
	parts = append(parts, titleStyle.Render("🟢 Codex CLI"))

	// Plan badge
	planLabel := strings.ToUpper(m.codex.PlanType)
	if planLabel == "" {
		planLabel = "PLUS"
	}
	planStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1A1A2E")).
		Background(lipgloss.Color("#06D6A0")).
		Bold(true).
		Padding(0, 2)
	planBadge := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Render(planStyle.Render(planLabel))
	parts = append(parts, planBadge)

	parts = append(parts, "")

	// Primary window (5h)
	parts = append(parts, m.renderQuotaBlock(
		"⏱  5-Hour Window",
		m.codex.PrimaryPercent,
		m.codex.PrimaryResetAt,
		m.codex.PrimarySeverity,
		inner,
	)...)

	parts = append(parts, "")

	// Secondary window (weekly)
	parts = append(parts, m.renderQuotaBlock(
		"📅 Weekly Window",
		m.codex.SecondaryPercent,
		m.codex.SecondaryResetAt,
		m.codex.SecondarySeverity,
		inner,
	)...)

	// Limit reached warning
	if m.codex.LimitReached {
		parts = append(parts, "")
		warnStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Bold(true).
			Width(inner).
			Align(lipgloss.Center)
		parts = append(parts, warnStyle.Render("⚠ RATE LIMIT REACHED"))
	}

	if m.codex.CostUSD > 0 || m.codex.WeeklyCostUSD > 0 {
		parts = append(parts, "")
		costStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#20C997")).
			Bold(true).
			Width(inner).
			Align(lipgloss.Center)
		parts = append(parts, costStyle.Render(fmt.Sprintf("💰 5h Window: $%.2f  |  Weekly: $%.2f", m.codex.CostUSD, m.codex.WeeklyCostUSD)))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return lipgloss.NewStyle().
		Width(width).
		Height(panelHeight()).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#06D6A0")).
		Padding(1, 1).
		Render(content)
}

// ─── Shared Components ──────────────────────────────────────────────────────

func panelHeight() int {
	return 18
}

func (m model) renderQuotaBlock(label string, percent int, resetAt time.Time, severity string, width int) []string {
	var lines []string

	// Label
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#CED4DA"))
	lines = append(lines, labelStyle.Render(label))

	// Progress bar
	barLine := m.renderProgressBar(percent, severity, width)
	lines = append(lines, barLine)

	// Percentage + Reset countdown
	pctColor := severityColor(severity)

	pctStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(pctColor))

	resetStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6C757D")).
		Italic(true)

	pctStr := pctStyle.Render(fmt.Sprintf("%d%%", percent))

	var resetStr string
	if !resetAt.IsZero() {
		remaining := resetAt.Sub(m.now)
		if remaining < 0 {
			remaining = 0
		}
		resetStr = resetStyle.Render(fmt.Sprintf("Resets in %s", formatDuration(remaining)))
	} else {
		resetStr = resetStyle.Render("Reset time unknown")
	}

	infoLine := lipgloss.NewStyle().
		Width(width).
		Render(pctStr + "  " + severityBadge(severity) + "    " + resetStr)
	lines = append(lines, infoLine)

	return lines
}

func (m model) renderProgressBar(percent int, severity string, width int) string {
	barWidth := width - 2
	if barWidth < 10 {
		barWidth = 10
	}

	filled := int(math.Round(float64(percent) * float64(barWidth) / 100))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	color := severityColor(severity)
	dimColor := severityDimColor(severity)

	filledStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
	emptyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimColor))

	bar := filledStyle.Render(strings.Repeat("━", filled)) +
		emptyStyle.Render(strings.Repeat("━", empty))

	return bar
}

func (m model) renderErrorPanel(title, plan, errMsg string, width int, borderColor string) string {
	inner := width - 4

	var parts []string

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(borderColor)).
		Width(inner).
		Align(lipgloss.Center)
	parts = append(parts, titleStyle.Render(title))

	planStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1A1A2E")).
		Background(lipgloss.Color(borderColor)).
		Bold(true).
		Padding(0, 2)
	planBadge := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Render(planStyle.Render(strings.ToUpper(plan)))
	parts = append(parts, planBadge)

	parts = append(parts, "")

	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF6B6B")).
		Width(inner).
		Align(lipgloss.Center)

	// Truncate error if too long
	displayErr := errMsg
	maxLen := inner * 3
	if len(displayErr) > maxLen {
		displayErr = displayErr[:maxLen-3] + "..."
	}

	parts = append(parts, errStyle.Render("⚠ Error"))
	parts = append(parts, "")

	errDetailStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ADB5BD")).
		Width(inner)
	parts = append(parts, errDetailStyle.Render(displayErr))

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FF6B6B")).
		Padding(1, 1).
		Render(content)
}

// ─── Footer ──────────────────────────────────────────────────────────────────

func (m model) renderFooter() string {
	headerWidth := m.width - 4
	if headerWidth < 60 {
		headerWidth = 60
	}

	line := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#2D2D44")).
		Render(strings.Repeat("─", headerWidth))

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4CC9F0")).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6C757D"))

	keys := lipgloss.JoinHorizontal(lipgloss.Center,
		keyStyle.Render("r"), descStyle.Render(" refresh  "),
		keyStyle.Render("q"), descStyle.Render(" quit"),
	)

	var lastRefresh string
	if !m.lastRefresh.IsZero() {
		lastRefresh = descStyle.Render("Last updated: " + m.lastRefresh.Format("15:04:05"))
	}

	footer := lipgloss.NewStyle().
		Width(headerWidth).
		Align(lipgloss.Center).
		Render(lipgloss.JoinVertical(lipgloss.Center,
			line,
			lipgloss.JoinHorizontal(lipgloss.Center, keys, "    ", lastRefresh),
		))

	return footer
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func severityColor(severity string) string {
	switch severity {
	case "critical":
		return "#FF6B6B"
	case "warning":
		return "#FFD93D"
	default:
		return "#6BCB77"
	}
}

func severityDimColor(severity string) string {
	switch severity {
	case "critical":
		return "#4A2020"
	case "warning":
		return "#4A4A20"
	default:
		return "#204A20"
	}
}

func severityBadge(severity string) string {
	color := severityColor(severity)
	var label string
	switch severity {
	case "critical":
		label = "● CRITICAL"
	case "warning":
		label = "● WARNING"
	default:
		label = "● NORMAL"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true).
		Render(label)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	seconds := int(d.Seconds()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// ─── Main ────────────────────────────────────────────────────────────────────

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
