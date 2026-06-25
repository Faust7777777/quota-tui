package main

import (
	"strings"
	"testing"
	"time"

	"quota-tui/api"
)

func TestTickSchedulesNextTick(t *testing.T) {
	m := initialModel()
	m.lastRefresh = time.Now()
	m.nextRefresh = m.lastRefresh.Add(5 * time.Minute)

	_, cmd := m.Update(tickMsg(m.lastRefresh.Add(time.Second)))
	if cmd == nil {
		t.Fatal("expected ordinary tick to schedule the next tick")
	}
}

func TestPanelsRenderAtEqualHeight(t *testing.T) {
	m := initialModel()
	m.now = time.Date(2026, 6, 25, 18, 0, 0, 0, time.Local)
	m.claude = testClaudeQuota()
	m.codex = testCodexQuota()
	m.codex.LimitReached = true

	claudePanel := m.renderClaudePanel(56)
	codexPanel := m.renderCodexPanel(56)

	if got, want := visibleLineCount(claudePanel), visibleLineCount(codexPanel); got != want {
		t.Fatalf("panel heights differ: claude=%d codex=%d", got, want)
	}
}

func testClaudeQuota() api.ClaudeQuota {
	now := time.Date(2026, 6, 25, 18, 0, 0, 0, time.Local)
	return api.ClaudeQuota{
		FiveHourPercent:  100,
		FiveHourResetAt:  now.Add(36 * time.Minute),
		WeeklyPercent:    56,
		WeeklyResetAt:    now.Add(10*time.Hour + 36*time.Minute),
		FiveHourSeverity: "critical",
		WeeklySeverity:   "normal",
		CostUSD:          22.47,
		WeeklyCostUSD:    93.49,
	}
}

func testCodexQuota() api.CodexQuota {
	now := time.Date(2026, 6, 25, 18, 0, 0, 0, time.Local)
	return api.CodexQuota{
		PlanType:          "plus",
		PrimaryPercent:    100,
		PrimaryResetAt:    now.Add(3*time.Hour + 6*time.Minute),
		SecondaryPercent:  16,
		SecondaryResetAt:  now.Add(166*time.Hour + 6*time.Minute),
		PrimarySeverity:   "critical",
		SecondarySeverity: "normal",
		CostUSD:           17.02,
		WeeklyCostUSD:     17.02,
	}
}

func visibleLineCount(s string) int {
	return len(strings.Split(s, "\n"))
}
