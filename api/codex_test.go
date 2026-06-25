package api

import (
	"testing"
	"time"
)

func TestCodexWindowUsesResetAfterSecondsForCountdown(t *testing.T) {
	now := time.Date(2026, 6, 25, 18, 0, 0, 0, time.Local)
	window := CodexRateLimitWindow{
		LimitWindowSeconds: 18000,
		ResetAfterSeconds:  18000,
		ResetAt:            now.Add(9 * time.Hour).Unix(),
	}

	resetAt := codexWindowResetAt(window, now)
	if got, want := resetAt.Sub(now), 5*time.Hour; got != want {
		t.Fatalf("reset countdown = %s, want %s", got, want)
	}

	start := codexWindowStart(window, now)
	if !start.Equal(now) {
		t.Fatalf("window start = %s, want %s", start, now)
	}
}
