package common

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// AdaptiveRetryDelayConfig controls dynamic retry delay tuning based on system CPU usage.
// It is intentionally env-driven (not option/db-driven) to keep rollout simple.
type AdaptiveRetryDelayConfig struct {
	Enabled      bool
	CPUThreshold int // 0-100 (%)
}

const (
	adaptiveRetryDelayStep = 10 * time.Millisecond
	adaptiveRetryDelayMax  = 1 * time.Second
)

var adaptiveRetryDelayConfig atomic.Value // AdaptiveRetryDelayConfig
var adaptiveRetryDelayNS atomic.Int64     // current delay in nanoseconds

func init() {
	adaptiveRetryDelayConfig.Store(AdaptiveRetryDelayConfig{
		Enabled:      false,
		CPUThreshold: 50,
	})
	adaptiveRetryDelayNS.Store(0)
}

func GetAdaptiveRetryDelayConfig() AdaptiveRetryDelayConfig {
	return adaptiveRetryDelayConfig.Load().(AdaptiveRetryDelayConfig)
}

func SetAdaptiveRetryDelayConfig(config AdaptiveRetryDelayConfig) {
	if config.CPUThreshold < 0 || config.CPUThreshold > 100 {
		SysError(fmt.Sprintf("invalid RETRY_DELAY_CPU_THRESHOLD=%d, fallback to 50", config.CPUThreshold))
		config.CPUThreshold = 50
	}
	adaptiveRetryDelayConfig.Store(config)
	if !config.Enabled {
		adaptiveRetryDelayNS.Store(0)
	}
}

// GetAdaptiveRetryDelay returns the current delay to apply between retries.
func GetAdaptiveRetryDelay() time.Duration {
	config := GetAdaptiveRetryDelayConfig()
	if !config.Enabled {
		return 0
	}
	ns := adaptiveRetryDelayNS.Load()
	if ns <= 0 {
		return 0
	}
	return time.Duration(ns)
}

// AdjustAdaptiveRetryDelay should be called when system CPU usage is sampled.
// Rule:
//   - cpuUsage > threshold: delay += 10ms
//   - cpuUsage <= threshold: delay -= 10ms
//   - clamp delay to [0, 1s]
func AdjustAdaptiveRetryDelay(cpuUsage float64) {
	config := GetAdaptiveRetryDelayConfig()
	if !config.Enabled {
		return
	}

	stepNS := int64(adaptiveRetryDelayStep)
	maxNS := int64(adaptiveRetryDelayMax)

	current := adaptiveRetryDelayNS.Load()
	if cpuUsage > float64(config.CPUThreshold) {
		current += stepNS
	} else {
		current -= stepNS
	}

	if current < 0 {
		current = 0
	} else if current > maxNS {
		current = maxNS
	}
	adaptiveRetryDelayNS.Store(current)
}

// SleepAdaptiveRetryDelay sleeps for current delay between retries.
// It returns false if ctx is canceled while waiting.
func SleepAdaptiveRetryDelay(ctx context.Context) bool {
	delay := GetAdaptiveRetryDelay()
	if delay <= 0 {
		return true
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

