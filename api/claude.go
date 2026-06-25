package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeCredentials represents the ~/.claude/.credentials.json structure.
type ClaudeCredentials struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// ClaudeUsageResponse is the API response from /api/oauth/usage.
type ClaudeUsageResponse struct {
	FiveHour struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day"`
	Limits []struct {
		Kind     string  `json:"kind"`
		Group    string  `json:"group"`
		Percent  float64 `json:"percent"`
		Severity string  `json:"severity"`
		ResetsAt string  `json:"resets_at"`
		IsActive bool    `json:"is_active"`
	} `json:"limits"`
	ExtraUsage struct {
		IsEnabled bool `json:"is_enabled"`
	} `json:"extra_usage"`
}

// ClaudeQuota holds the processed quota data for Claude.
type ClaudeQuota struct {
	FiveHourPercent  int
	FiveHourResetAt  time.Time
	WeeklyPercent    int
	WeeklyResetAt    time.Time
	FiveHourSeverity string
	WeeklySeverity   string
	ExtraUsage       bool
	Error            string
	CostUSD          float64
	WeeklyCostUSD    float64
	IsCached         bool
}

var lastClaudeQuota *ClaudeQuota

type claudeLogUsage struct {
	InputTokens              float64 `json:"input_tokens"`
	CacheCreationInputTokens float64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     float64 `json:"cache_read_input_tokens"`
	OutputTokens             float64 `json:"output_tokens"`
	ServiceTier              string  `json:"service_tier"`
	Speed                    string  `json:"speed"`
	InferenceGeo             string  `json:"inference_geo"`
	CacheCreation            struct {
		Ephemeral1hInputTokens float64 `json:"ephemeral_1h_input_tokens"`
		Ephemeral5mInputTokens float64 `json:"ephemeral_5m_input_tokens"`
	} `json:"cache_creation"`
}

type claudePrice struct {
	Input     float64
	Cache5m   float64
	Cache1h   float64
	CacheRead float64
	Output    float64
}

func getClaudeCosts(fiveHourResetAt, weeklyResetAt time.Time) (float64, float64) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0
	}
	now := time.Now()
	fiveStart := now.Add(-5 * time.Hour)
	if !fiveHourResetAt.IsZero() {
		fiveStart = fiveHourResetAt.Add(-5 * time.Hour)
	}
	weeklyStart := now.Add(-7 * 24 * time.Hour)
	if !weeklyResetAt.IsZero() {
		weeklyStart = weeklyResetAt.Add(-7 * 24 * time.Hour)
	}
	return getClaudeCostsFromDir(filepath.Join(home, ".claude", "projects"), fiveStart, weeklyStart)
}

func getClaudeCostsFromDir(root string, fiveStart, weeklyStart time.Time) (float64, float64) {
	earliest := fiveStart
	if weeklyStart.Before(earliest) {
		earliest = weeklyStart
	}

	var fiveCost float64
	var weeklyCost float64
	seen := make(map[string]bool)

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if info.ModTime().Add(24 * time.Hour).Before(earliest) {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			eventID, ts, model, usage, ok := parseClaudeLogLine(scanner.Bytes())
			if !ok {
				continue
			}
			if eventID == "" {
				eventID = fmt.Sprintf("%s:%d", path, lineNo)
			}
			if seen[eventID] {
				continue
			}
			seen[eventID] = true

			cost := calculateClaudeCostUSD(model, usage)
			if !ts.Before(fiveStart) {
				fiveCost += cost
			}
			if !ts.Before(weeklyStart) {
				weeklyCost += cost
			}
		}
		return nil
	})

	return roundUSD(fiveCost), roundUSD(weeklyCost)
}

func parseClaudeLogLine(line []byte) (string, time.Time, string, claudeLogUsage, bool) {
	var event struct {
		Type      string `json:"type"`
		RequestID string `json:"requestId"`
		UUID      string `json:"uuid"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			Model string         `json:"model"`
			Usage claudeLogUsage `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return "", time.Time{}, "", claudeLogUsage{}, false
	}
	if event.Type != "assistant" || event.Timestamp == "" || event.Message.Model == "" {
		return "", time.Time{}, "", claudeLogUsage{}, false
	}
	usage := event.Message.Usage
	if usage.InputTokens == 0 && usage.CacheCreationInputTokens == 0 && usage.CacheReadInputTokens == 0 && usage.OutputTokens == 0 {
		return "", time.Time{}, "", claudeLogUsage{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			return "", time.Time{}, "", claudeLogUsage{}, false
		}
	}
	eventID := event.RequestID
	if eventID == "" {
		eventID = event.UUID
	}
	return eventID, ts, event.Message.Model, usage, true
}

func calculateClaudeCostUSD(model string, usage claudeLogUsage) float64 {
	price := priceForClaudeModel(model, usage)
	cache5m := usage.CacheCreation.Ephemeral5mInputTokens
	cache1h := usage.CacheCreation.Ephemeral1hInputTokens
	if cache5m == 0 && cache1h == 0 && usage.CacheCreationInputTokens > 0 {
		cache5m = usage.CacheCreationInputTokens
	}
	cost := (usage.InputTokens*price.Input +
		cache5m*price.Cache5m +
		cache1h*price.Cache1h +
		usage.CacheReadInputTokens*price.CacheRead +
		usage.OutputTokens*price.Output) / 1_000_000
	return roundUSD(cost)
}

func priceForClaudeModel(model string, usage claudeLogUsage) claudePrice {
	m := strings.ToLower(model)
	speed := strings.ToLower(usage.Speed)
	var price claudePrice

	switch {
	case strings.Contains(m, "fable-5") || strings.Contains(m, "mythos-5"):
		price = claudePrice{Input: 10, Cache5m: 12.5, Cache1h: 20, CacheRead: 1, Output: 50}
	case strings.Contains(m, "opus-4-8"):
		if speed == "fast" {
			price = claudePrice{Input: 10, Cache5m: 12.5, Cache1h: 20, CacheRead: 1, Output: 50}
		} else {
			price = claudePrice{Input: 5, Cache5m: 6.25, Cache1h: 10, CacheRead: 0.5, Output: 25}
		}
	case strings.Contains(m, "opus-4-7") || strings.Contains(m, "opus-4-6"):
		if speed == "fast" {
			price = claudePrice{Input: 30, Cache5m: 37.5, Cache1h: 60, CacheRead: 3, Output: 150}
		} else {
			price = claudePrice{Input: 5, Cache5m: 6.25, Cache1h: 10, CacheRead: 0.5, Output: 25}
		}
	case strings.Contains(m, "opus-4-5"):
		price = claudePrice{Input: 5, Cache5m: 6.25, Cache1h: 10, CacheRead: 0.5, Output: 25}
	case strings.Contains(m, "opus-4-1") || strings.Contains(m, "opus-4"):
		price = claudePrice{Input: 15, Cache5m: 18.75, Cache1h: 30, CacheRead: 1.5, Output: 75}
	case strings.Contains(m, "sonnet-4"):
		price = claudePrice{Input: 3, Cache5m: 3.75, Cache1h: 6, CacheRead: 0.3, Output: 15}
	case strings.Contains(m, "haiku-4-5"):
		price = claudePrice{Input: 1, Cache5m: 1.25, Cache1h: 2, CacheRead: 0.1, Output: 5}
	case strings.Contains(m, "haiku-3-5"):
		price = claudePrice{Input: 0.8, Cache5m: 1, Cache1h: 1.6, CacheRead: 0.08, Output: 4}
	default:
		price = claudePrice{Input: 3, Cache5m: 3.75, Cache1h: 6, CacheRead: 0.3, Output: 15}
	}

	if strings.EqualFold(usage.InferenceGeo, "us") && supportsClaudeGeoPricing(m) {
		price.Input *= 1.1
		price.Cache5m *= 1.1
		price.Cache1h *= 1.1
		price.CacheRead *= 1.1
		price.Output *= 1.1
	}
	return price
}

func supportsClaudeGeoPricing(model string) bool {
	return strings.Contains(model, "fable-5") ||
		strings.Contains(model, "mythos-5") ||
		strings.Contains(model, "opus-4-8") ||
		strings.Contains(model, "opus-4-7") ||
		strings.Contains(model, "opus-4-6") ||
		strings.Contains(model, "opus-4-5") ||
		strings.Contains(model, "sonnet-4-6") ||
		strings.Contains(model, "sonnet-4-5") ||
		strings.Contains(model, "haiku-4-5")
}

func roundUSD(v float64) float64 {
	return math.Round(v*10_000) / 10_000
}

// readClaudeToken reads the Claude OAuth token from credentials file.
func readClaudeToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home dir: %w", err)
	}
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", credPath, err)
	}
	var creds ClaudeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("cannot parse credentials: %w", err)
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("access token is empty in %s", credPath)
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}

// FetchClaudeQuota fetches quota usage from the Anthropic API.
func FetchClaudeQuota() ClaudeQuota {
	token, err := readClaudeToken()
	if err != nil {
		return ClaudeQuota{Error: err.Error()}
	}

	client, err := newUsageHTTPClient()
	if err != nil {
		return ClaudeQuota{Error: err.Error()}
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return ClaudeQuota{Error: fmt.Sprintf("request error: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-code/0.2.29")

	resp, err := client.Do(req)
	if err != nil {
		return ClaudeQuota{Error: fmt.Sprintf("API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		if lastClaudeQuota != nil {
			cached := *lastClaudeQuota
			cached.IsCached = true
			cached.Error = ""
			return cached
		} else {
			fiveCost, weeklyCost := getClaudeCosts(time.Time{}, time.Time{})
			return ClaudeQuota{
				FiveHourPercent:  100,
				FiveHourSeverity: "critical",
				WeeklyPercent:    -1,
				WeeklySeverity:   "normal",
				IsCached:         true,
				CostUSD:          fiveCost,
				WeeklyCostUSD:    weeklyCost,
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ClaudeQuota{Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ClaudeQuota{Error: fmt.Sprintf("read body error: %v", err)}
	}

	var usage ClaudeUsageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return ClaudeQuota{Error: fmt.Sprintf("parse error: %v", err)}
	}

	quota := ClaudeQuota{
		FiveHourPercent: int(usage.FiveHour.Utilization),
		WeeklyPercent:   int(usage.SevenDay.Utilization),
		ExtraUsage:      usage.ExtraUsage.IsEnabled,
	}

	// Parse reset times
	if t, err := time.Parse(time.RFC3339, usage.FiveHour.ResetsAt); err == nil {
		quota.FiveHourResetAt = t
	}
	if t, err := time.Parse(time.RFC3339, usage.SevenDay.ResetsAt); err == nil {
		quota.WeeklyResetAt = t
	}

	quota.CostUSD, quota.WeeklyCostUSD = getClaudeCosts(quota.FiveHourResetAt, quota.WeeklyResetAt)

	// Extract severity from limits
	quota.FiveHourSeverity = severityFromPercent(quota.FiveHourPercent)
	quota.WeeklySeverity = severityFromPercent(quota.WeeklyPercent)
	for _, limit := range usage.Limits {
		if limit.Group == "session" {
			quota.FiveHourSeverity = limit.Severity
		}
		if limit.Group == "weekly" {
			quota.WeeklySeverity = limit.Severity
		}
	}

	lastClaudeQuota = &quota

	return quota
}

func severityFromPercent(pct int) string {
	switch {
	case pct >= 80:
		return "critical"
	case pct >= 50:
		return "warning"
	default:
		return "normal"
	}
}
