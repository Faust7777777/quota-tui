package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// CodexAuth represents the ~/.codex/auth.json structure.
type CodexAuth struct {
	Tokens struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

// CodexUsageResponse is the API response from /backend-api/wham/usage.
type CodexUsageResponse struct {
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		Allowed       bool `json:"allowed"`
		LimitReached  bool `json:"limit_reached"`
		PrimaryWindow struct {
			UsedPercent        float64 `json:"used_percent"`
			LimitWindowSeconds int     `json:"limit_window_seconds"`
			ResetAfterSeconds  int     `json:"reset_after_seconds"`
			ResetAt            int64   `json:"reset_at"`
		} `json:"primary_window"`
		SecondaryWindow struct {
			UsedPercent        float64 `json:"used_percent"`
			LimitWindowSeconds int     `json:"limit_window_seconds"`
			ResetAfterSeconds  int     `json:"reset_after_seconds"`
			ResetAt            int64   `json:"reset_at"`
		} `json:"secondary_window"`
	} `json:"rate_limit"`
}

// CodexQuota holds the processed quota data for Codex CLI.
type CodexQuota struct {
	PlanType          string
	PrimaryPercent    int
	PrimaryResetAt    time.Time
	SecondaryPercent  int
	SecondaryResetAt  time.Time
	LimitReached      bool
	PrimarySeverity   string
	SecondarySeverity string
	Error             string
	CostUSD           float64
	WeeklyCostUSD     float64
}

type tokenUsage struct {
	Input  float64
	Cached float64
	Output float64
}

func parseTokenCount(line []byte) (tokenUsage, time.Time, bool) {
	var event struct {
		Timestamp interface{} `json:"timestamp"`
		Time      interface{} `json:"time"`
		CreatedAt interface{} `json:"created_at"`
		Type      string      `json:"type"`
		Payload   struct {
			Type string `json:"type"`
			Info struct {
				TotalTokenUsage struct {
					InputTokens       float64 `json:"input_tokens"`
					CachedInputTokens float64 `json:"cached_input_tokens"`
					OutputTokens      float64 `json:"output_tokens"`
				} `json:"total_token_usage"`
			} `json:"info"`
		} `json:"payload"`
		Info struct {
			TotalTokenUsage struct {
				InputTokens       float64 `json:"input_tokens"`
				CachedInputTokens float64 `json:"cached_input_tokens"`
				OutputTokens      float64 `json:"output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	}

	if err := json.Unmarshal(line, &event); err != nil {
		return tokenUsage{}, time.Time{}, false
	}

	isTokenCount := event.Type == "token_count" || event.Payload.Type == "token_count"
	if !isTokenCount {
		return tokenUsage{}, time.Time{}, false
	}

	var tsStr string
	if s, ok := event.Timestamp.(string); ok {
		tsStr = s
	} else if s, ok := event.Time.(string); ok {
		tsStr = s
	} else if s, ok := event.CreatedAt.(string); ok {
		tsStr = s
	} else if f, ok := event.Timestamp.(float64); ok {
		ts := time.Unix(int64(f), 0)
		tu := event.Info.TotalTokenUsage
		if event.Payload.Type == "token_count" {
			tu = event.Payload.Info.TotalTokenUsage
		}
		return tokenUsage{Input: tu.InputTokens, Cached: tu.CachedInputTokens, Output: tu.OutputTokens}, ts, true
	} else {
		return tokenUsage{}, time.Time{}, false
	}

	ts, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, tsStr)
		if err != nil {
			return tokenUsage{}, time.Time{}, false
		}
	}

	tu := event.Info.TotalTokenUsage
	if event.Payload.Type == "token_count" {
		tu = event.Payload.Info.TotalTokenUsage
	}
	return tokenUsage{Input: tu.InputTokens, Cached: tu.CachedInputTokens, Output: tu.OutputTokens}, ts, true
}

func getCodexCost(usage CodexUsageResponse) (float64, float64) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0
	}
	sessionsDir := filepath.Join(home, ".codex", "sessions")

	primaryStart := time.Unix(int64(usage.RateLimit.PrimaryWindow.ResetAt)-int64(usage.RateLimit.PrimaryWindow.LimitWindowSeconds), 0)
	secondaryStart := time.Unix(int64(usage.RateLimit.SecondaryWindow.ResetAt)-int64(usage.RateLimit.SecondaryWindow.LimitWindowSeconds), 0)

	earliest := primaryStart
	if secondaryStart.Before(earliest) {
		earliest = secondaryStart
	}

	lastTotals := make(map[string]tokenUsage)
	primaryTotals := tokenUsage{}
	secondaryTotals := tokenUsage{}

	filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".jsonl" {
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

		sessKey := filepath.Base(path)
		scanner := bufio.NewScanner(file)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			u, ts, ok := parseTokenCount(line)
			if !ok {
				continue
			}

			prev := lastTotals[sessKey]

			if !ts.Before(primaryStart) {
				dIn := u.Input - prev.Input
				if dIn < 0 {
					dIn = 0
				}
				dCa := u.Cached - prev.Cached
				if dCa < 0 {
					dCa = 0
				}
				dOu := u.Output - prev.Output
				if dOu < 0 {
					dOu = 0
				}

				primaryTotals.Input += dIn
				primaryTotals.Cached += dCa
				primaryTotals.Output += dOu
			}

			if !ts.Before(secondaryStart) {
				dIn := u.Input - prev.Input
				if dIn < 0 {
					dIn = 0
				}
				dCa := u.Cached - prev.Cached
				if dCa < 0 {
					dCa = 0
				}
				dOu := u.Output - prev.Output
				if dOu < 0 {
					dOu = 0
				}

				secondaryTotals.Input += dIn
				secondaryTotals.Cached += dCa
				secondaryTotals.Output += dOu
			}

			lastTotals[sessKey] = u
		}
		return nil
	})

	calcCost := func(t tokenUsage) float64 {
		uncachedInput := t.Input - t.Cached
		if uncachedInput < 0 {
			uncachedInput = 0
		}
		credits := (uncachedInput*125 + t.Cached*12.5 + t.Output*750) / 1000000
		return credits * 0.04
	}

	return calcCost(primaryTotals), calcCost(secondaryTotals)
}

// readCodexAuth reads the Codex CLI auth tokens from auth.json.
func readCodexAuth() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("cannot find home dir: %w", err)
	}
	authPath := filepath.Join(home, ".codex", "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		return "", "", fmt.Errorf("cannot read %s: %w", authPath, err)
	}
	var auth CodexAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return "", "", fmt.Errorf("cannot parse auth: %w", err)
	}
	if auth.Tokens.AccessToken == "" {
		return "", "", fmt.Errorf("access_token is empty in %s", authPath)
	}
	return auth.Tokens.AccessToken, auth.Tokens.AccountID, nil
}

// FetchCodexQuota fetches quota usage from the OpenAI/ChatGPT API.
func FetchCodexQuota() CodexQuota {
	token, accountID, err := readCodexAuth()
	if err != nil {
		return CodexQuota{Error: err.Error()}
	}

	client, err := newUsageHTTPClient()
	if err != nil {
		return CodexQuota{Error: err.Error()}
	}

	req, err := http.NewRequest("GET", "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return CodexQuota{Error: fmt.Sprintf("request error: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return CodexQuota{Error: fmt.Sprintf("API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return CodexQuota{Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CodexQuota{Error: fmt.Sprintf("read body error: %v", err)}
	}

	var usage CodexUsageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return CodexQuota{Error: fmt.Sprintf("parse error: %v", err)}
	}

	today, weekly := getCodexCost(usage)

	quota := CodexQuota{
		PlanType:         usage.PlanType,
		PrimaryPercent:   int(usage.RateLimit.PrimaryWindow.UsedPercent),
		SecondaryPercent: int(usage.RateLimit.SecondaryWindow.UsedPercent),
		LimitReached:     usage.RateLimit.LimitReached,
		CostUSD:          today,
		WeeklyCostUSD:    weekly,
	}

	// Convert unix timestamps to time.Time
	if usage.RateLimit.PrimaryWindow.ResetAt > 0 {
		quota.PrimaryResetAt = time.Unix(usage.RateLimit.PrimaryWindow.ResetAt, 0)
	}
	if usage.RateLimit.SecondaryWindow.ResetAt > 0 {
		quota.SecondaryResetAt = time.Unix(usage.RateLimit.SecondaryWindow.ResetAt, 0)
	}

	quota.PrimarySeverity = severityFromPercent(quota.PrimaryPercent)
	quota.SecondarySeverity = severityFromPercent(quota.SecondaryPercent)

	return quota
}
