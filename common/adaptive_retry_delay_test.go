package common

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveRetryDelay_AdjustAndClamp(t *testing.T) {
	t.Cleanup(func() {
		SetAdaptiveRetryDelayConfig(AdaptiveRetryDelayConfig{Enabled: false, CPUThreshold: 50})
	})

	SetAdaptiveRetryDelayConfig(AdaptiveRetryDelayConfig{Enabled: true, CPUThreshold: 50})

	if got := GetAdaptiveRetryDelay(); got != 0 {
		t.Fatalf("expected initial delay 0, got %s", got)
	}

	AdjustAdaptiveRetryDelay(60) // +10ms
	AdjustAdaptiveRetryDelay(60) // +10ms
	AdjustAdaptiveRetryDelay(60) // +10ms
	if got := GetAdaptiveRetryDelay(); got != 30*time.Millisecond {
		t.Fatalf("expected 30ms, got %s", got)
	}

	AdjustAdaptiveRetryDelay(50) // <= threshold => -10ms
	AdjustAdaptiveRetryDelay(10) // -10ms
	if got := GetAdaptiveRetryDelay(); got != 10*time.Millisecond {
		t.Fatalf("expected 10ms, got %s", got)
	}

	// Clamp to max (1s)
	for i := 0; i < 200; i++ {
		AdjustAdaptiveRetryDelay(99)
	}
	if got := GetAdaptiveRetryDelay(); got != time.Second {
		t.Fatalf("expected 1s clamp, got %s", got)
	}

	// Clamp to min (0)
	for i := 0; i < 200; i++ {
		AdjustAdaptiveRetryDelay(0)
	}
	if got := GetAdaptiveRetryDelay(); got != 0 {
		t.Fatalf("expected 0 clamp, got %s", got)
	}
}

func TestAdaptiveRetryDelay_DisabledResets(t *testing.T) {
	SetAdaptiveRetryDelayConfig(AdaptiveRetryDelayConfig{Enabled: true, CPUThreshold: 50})
	AdjustAdaptiveRetryDelay(99)
	if GetAdaptiveRetryDelay() == 0 {
		t.Fatalf("expected delay > 0 after adjust")
	}

	SetAdaptiveRetryDelayConfig(AdaptiveRetryDelayConfig{Enabled: false, CPUThreshold: 50})
	if got := GetAdaptiveRetryDelay(); got != 0 {
		t.Fatalf("expected delay 0 when disabled, got %s", got)
	}
}

func TestAdaptiveRetryDelay_InvalidThresholdFallback(t *testing.T) {
	SetAdaptiveRetryDelayConfig(AdaptiveRetryDelayConfig{Enabled: true, CPUThreshold: 999})
	cfg := GetAdaptiveRetryDelayConfig()
	if cfg.CPUThreshold != 50 {
		t.Fatalf("expected threshold fallback to 50, got %d", cfg.CPUThreshold)
	}
}

func TestSleepAdaptiveRetryDelay_ContextCancel(t *testing.T) {
	t.Cleanup(func() {
		SetAdaptiveRetryDelayConfig(AdaptiveRetryDelayConfig{Enabled: false, CPUThreshold: 50})
	})
	SetAdaptiveRetryDelayConfig(AdaptiveRetryDelayConfig{Enabled: true, CPUThreshold: 50})
	AdjustAdaptiveRetryDelay(99) // 10ms

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok := SleepAdaptiveRetryDelay(ctx); ok {
		t.Fatalf("expected sleep to abort on canceled context")
	}
}

