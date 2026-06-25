package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeCostsUseRollingWindowStarts(t *testing.T) {
	dir := t.TempDir()
	fiveStart := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	weeklyStart := fiveStart.Add(-24 * time.Hour)

	writeClaudeJSONL(t, dir, "session.jsonl",
		claudeUsageLine("weekly-only", weeklyStart.Add(time.Hour), "claude-sonnet-4-5", 1_000_000, 0, 0, 0, 0, 0),
		claudeUsageLine("both-windows", fiveStart.Add(time.Hour), "claude-sonnet-4-5", 0, 0, 0, 0, 1_000_000, 0),
	)

	fiveCost, weeklyCost := getClaudeCostsFromDir(dir, fiveStart, weeklyStart)
	if fiveCost != 15 {
		t.Fatalf("5h cost = %.2f, want 15.00", fiveCost)
	}
	if weeklyCost != 18 {
		t.Fatalf("weekly cost = %.2f, want 18.00", weeklyCost)
	}
}

func TestClaudeCostsDeduplicateRequestID(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	line := claudeUsageLine("duplicate", start.Add(time.Hour), "claude-sonnet-4-5", 1_000_000, 0, 0, 0, 0, 0)
	writeClaudeJSONL(t, dir, "session.jsonl", line, line)

	fiveCost, weeklyCost := getClaudeCostsFromDir(dir, start, start)
	if fiveCost != 3 {
		t.Fatalf("5h cost = %.2f, want 3.00", fiveCost)
	}
	if weeklyCost != 3 {
		t.Fatalf("weekly cost = %.2f, want 3.00", weeklyCost)
	}
}

func TestClaudeCostUsesPromptCacheRates(t *testing.T) {
	usage := claudeLogUsage{
		InputTokens:              1_000_000,
		CacheCreationInputTokens: 2_000_000,
		CacheReadInputTokens:     1_000_000,
		OutputTokens:             1_000_000,
		CacheCreation: struct {
			Ephemeral1hInputTokens float64 `json:"ephemeral_1h_input_tokens"`
			Ephemeral5mInputTokens float64 `json:"ephemeral_5m_input_tokens"`
		}{
			Ephemeral1hInputTokens: 1_000_000,
			Ephemeral5mInputTokens: 1_000_000,
		},
	}

	cost := calculateClaudeCostUSD("claude-sonnet-4-5", usage)
	if cost != 28.05 {
		t.Fatalf("cost = %.2f, want 28.05", cost)
	}
}

func writeClaudeJSONL(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	path := filepath.Join(dir, name)
	var content string
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func claudeUsageLine(requestID string, ts time.Time, model string, input, cacheCreate, cacheCreate1h, cacheCreate5m, output, cacheRead int) string {
	return `{"type":"assistant","requestId":"` + requestID + `","timestamp":"` + ts.Format(time.RFC3339Nano) + `","message":{"model":"` + model + `","usage":{"input_tokens":` + itoa(input) + `,"cache_creation_input_tokens":` + itoa(cacheCreate) + `,"cache_read_input_tokens":` + itoa(cacheRead) + `,"output_tokens":` + itoa(output) + `,"cache_creation":{"ephemeral_1h_input_tokens":` + itoa(cacheCreate1h) + `,"ephemeral_5m_input_tokens":` + itoa(cacheCreate5m) + `}}}}`
}

func itoa(v int) string {
	switch v {
	case 0:
		return "0"
	case 1_000_000:
		return "1000000"
	case 2_000_000:
		return "2000000"
	default:
		panic("unexpected test integer")
	}
}
