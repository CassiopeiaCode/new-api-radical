package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestChannelRateLimiterTotalRequestLimit(t *testing.T) {
	limiter := &channelRateLimiter{local: make(map[int]*channelRateLimitWindowState)}
	config := ChannelRateLimitConfig{RequestRPM: 2}

	if !limiter.localAcquire(101, config) {
		t.Fatal("first request should be accepted")
	}
	if !limiter.localAcquire(101, config) {
		t.Fatal("second request should be accepted")
	}
	if limiter.localAcquire(101, config) {
		t.Fatal("third request should be rejected by total RPM")
	}
	if !limiter.localAcquire(102, config) {
		t.Fatal("different channel must have an independent window")
	}
}

func TestChannelRateLimiterSuccessfulRequestLimit(t *testing.T) {
	limiter := &channelRateLimiter{local: make(map[int]*channelRateLimitWindowState)}
	config := ChannelRateLimitConfig{SuccessfulRPM: 1}

	if !limiter.localAcquire(101, config) {
		t.Fatal("first request should be accepted")
	}
	limiter.localRecordSuccess(101)
	if limiter.localAcquire(101, config) {
		t.Fatal("successful RPM saturation should reject a later request")
	}
}

func TestChannelRateLimitDisabled(t *testing.T) {
	config := ChannelRateLimitConfig{}
	if config.Enabled() {
		t.Fatal("zero values must leave channel limiting disabled")
	}
	parsed := ChannelRateLimitConfigFromSettings(dto.ChannelOtherSettings{})
	if parsed.Enabled() {
		t.Fatal("empty channel settings must not enable channel limiting")
	}
}
