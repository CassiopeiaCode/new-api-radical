package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const channelRateLimitWindow = time.Minute

// ChannelRateLimitConfig protects a channel independently of user rate limits.
// Both values are rolling one-minute limits; a non-positive value disables the
// corresponding dimension.
type ChannelRateLimitConfig struct {
	RequestRPM    int
	SuccessfulRPM int
}

func ChannelRateLimitConfigFromSettings(settings dto.ChannelOtherSettings) ChannelRateLimitConfig {
	return ChannelRateLimitConfig{
		RequestRPM:    settings.ChannelRequestRateLimitCount,
		SuccessfulRPM: settings.ChannelRequestRateLimitSuccessCount,
	}
}

func (c ChannelRateLimitConfig) Enabled() bool {
	return c.RequestRPM > 0 || c.SuccessfulRPM > 0
}

type channelRateLimitWindowState struct {
	total   []time.Time
	success []time.Time
}

type channelRateLimiter struct {
	mu     sync.Mutex
	local  map[int]*channelRateLimitWindowState
	serial atomic.Uint64
}

var globalChannelRateLimiter = &channelRateLimiter{local: make(map[int]*channelRateLimitWindowState)}

func pruneChannelRateLimitSamples(samples []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-channelRateLimitWindow)
	firstValid := 0
	for firstValid < len(samples) && !samples[firstValid].After(cutoff) {
		firstValid++
	}
	return samples[firstValid:]
}

// IsChannelRateLimited reports whether a channel is already saturated. It does
// not write a request sample, so callers may safely use it while choosing a
// candidate. TryAcquireChannelRequest performs the atomic reservation directly
// before the request is sent upstream.
func IsChannelRateLimited(ctx context.Context, channelID int, config ChannelRateLimitConfig) (bool, error) {
	if channelID <= 0 || !config.Enabled() {
		return false, nil
	}
	if common.RedisEnabled {
		return globalChannelRateLimiter.redisSaturated(ctx, channelID, config)
	}
	return globalChannelRateLimiter.localSaturated(channelID, config), nil
}

// TryAcquireChannelRequest reserves one total-request sample. A rejected
// reservation represents a local routing decision, not an upstream attempt;
// callers must select another channel and must not record a perf_metrics sample.
func TryAcquireChannelRequest(ctx context.Context, channelID int, config ChannelRateLimitConfig) (bool, error) {
	if channelID <= 0 || !config.Enabled() {
		return true, nil
	}
	if common.RedisEnabled {
		return globalChannelRateLimiter.redisAcquire(ctx, channelID, config)
	}
	return globalChannelRateLimiter.localAcquire(channelID, config), nil
}

// RecordChannelRequestSuccess records a successful upstream attempt. It must be
// called only after the upstream relay has succeeded; locally skipped channels
// never reach this function.
func RecordChannelRequestSuccess(ctx context.Context, channelID int, config ChannelRateLimitConfig) error {
	if channelID <= 0 || config.SuccessfulRPM <= 0 {
		return nil
	}
	if common.RedisEnabled {
		return globalChannelRateLimiter.redisRecordSuccess(ctx, channelID, config)
	}
	globalChannelRateLimiter.localRecordSuccess(channelID)
	return nil
}

// RecordChannelRequestSuccessForRelay is the single completion hook used by
// quota settlement paths. Audio and text relays both settle through those paths,
// so a successful Whisper response with zero completion tokens is still counted
// as successful without any token- or response-size special case.
func RecordChannelRequestSuccessForRelay(ctx context.Context, relayInfo *relaycommon.RelayInfo) error {
	if relayInfo == nil || relayInfo.ChannelMeta == nil {
		return nil
	}
	return RecordChannelRequestSuccess(ctx, relayInfo.ChannelMeta.ChannelId,
		ChannelRateLimitConfigFromSettings(relayInfo.ChannelMeta.ChannelOtherSettings))
}

// TryAcquireChannelRequestForRelay reserves the selected channel immediately
// before an upstream relay starts. A false result is a local selection skip and
// must never be turned into a perf_metrics failure sample.
func TryAcquireChannelRequestForRelay(ctx context.Context, relayInfo *relaycommon.RelayInfo) (bool, error) {
	if relayInfo == nil || relayInfo.ChannelMeta == nil {
		return true, nil
	}
	return TryAcquireChannelRequest(ctx, relayInfo.ChannelMeta.ChannelId,
		ChannelRateLimitConfigFromSettings(relayInfo.ChannelMeta.ChannelOtherSettings))
}

func channelRateLimitConfigFromContext(c *gin.Context) ChannelRateLimitConfig {
	if c == nil {
		return ChannelRateLimitConfig{}
	}
	settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	if !ok {
		return ChannelRateLimitConfig{}
	}
	return ChannelRateLimitConfigFromSettings(settings)
}

// TryAcquireChannelRequestForContext always uses the channel settings currently
// installed in the Gin context. Relay retries replace those settings whenever a
// new channel is selected, unlike RelayInfo.ChannelMeta which is constructed at
// request initialization.
func TryAcquireChannelRequestForContext(c *gin.Context, channelID int) (bool, error) {
	if c == nil {
		return true, nil
	}
	return TryAcquireChannelRequest(c.Request.Context(), channelID, channelRateLimitConfigFromContext(c))
}

func RecordChannelRequestSuccessForContext(c *gin.Context, channelID int) error {
	if c == nil {
		return nil
	}
	return RecordChannelRequestSuccess(c.Request.Context(), channelID, channelRateLimitConfigFromContext(c))
}

func (l *channelRateLimiter) localSaturated(channelID int, config ChannelRateLimitConfig) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.local[channelID]
	if state == nil {
		return false
	}
	now := time.Now()
	state.total = pruneChannelRateLimitSamples(state.total, now)
	state.success = pruneChannelRateLimitSamples(state.success, now)
	return (config.RequestRPM > 0 && len(state.total) >= config.RequestRPM) ||
		(config.SuccessfulRPM > 0 && len(state.success) >= config.SuccessfulRPM)
}

func (l *channelRateLimiter) localAcquire(channelID int, config ChannelRateLimitConfig) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	state := l.local[channelID]
	if state == nil {
		state = &channelRateLimitWindowState{}
		l.local[channelID] = state
	}
	state.total = pruneChannelRateLimitSamples(state.total, now)
	state.success = pruneChannelRateLimitSamples(state.success, now)
	if (config.RequestRPM > 0 && len(state.total) >= config.RequestRPM) ||
		(config.SuccessfulRPM > 0 && len(state.success) >= config.SuccessfulRPM) {
		return false
	}
	state.total = append(state.total, now)
	return true
}

func (l *channelRateLimiter) localRecordSuccess(channelID int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	state := l.local[channelID]
	if state == nil {
		state = &channelRateLimitWindowState{}
		l.local[channelID] = state
	}
	state.success = append(pruneChannelRateLimitSamples(state.success, now), now)
}

func (l *channelRateLimiter) redisKey(channelID int, kind string) string {
	return fmt.Sprintf("new-api:channel-rpm:v1:%s:%d", kind, channelID)
}

func (l *channelRateLimiter) redisMember(now time.Time) string {
	return fmt.Sprintf("%d-%d", now.UnixNano(), l.serial.Add(1))
}

const channelRateLimitSaturatedLua = `
local cutoff = tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', cutoff)
local total = redis.call('ZCARD', KEYS[1])
local success = redis.call('ZCARD', KEYS[2])
if (tonumber(ARGV[2]) > 0 and total >= tonumber(ARGV[2])) or (tonumber(ARGV[3]) > 0 and success >= tonumber(ARGV[3])) then return 1 end
return 0`

const channelRateLimitAcquireLua = `
local cutoff = tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', cutoff)
local total = redis.call('ZCARD', KEYS[1])
local success = redis.call('ZCARD', KEYS[2])
if (tonumber(ARGV[2]) > 0 and total >= tonumber(ARGV[2])) or (tonumber(ARGV[3]) > 0 and success >= tonumber(ARGV[3])) then return 0 end
redis.call('ZADD', KEYS[1], ARGV[4], ARGV[5])
redis.call('EXPIRE', KEYS[1], 120)
redis.call('EXPIRE', KEYS[2], 120)
return 1`

const channelRateLimitSuccessLua = `
local cutoff = tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff)
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[3])
redis.call('EXPIRE', KEYS[1], 120)
return 1`

func (l *channelRateLimiter) redisSaturated(ctx context.Context, channelID int, config ChannelRateLimitConfig) (bool, error) {
	now := time.Now()
	result, err := common.RDB.Eval(ctx, channelRateLimitSaturatedLua,
		[]string{l.redisKey(channelID, "total"), l.redisKey(channelID, "success")},
		now.Add(-channelRateLimitWindow).UnixMilli(), config.RequestRPM, config.SuccessfulRPM).Int()
	return result == 1, err
}

func (l *channelRateLimiter) redisAcquire(ctx context.Context, channelID int, config ChannelRateLimitConfig) (bool, error) {
	now := time.Now()
	result, err := common.RDB.Eval(ctx, channelRateLimitAcquireLua,
		[]string{l.redisKey(channelID, "total"), l.redisKey(channelID, "success")},
		now.Add(-channelRateLimitWindow).UnixMilli(), config.RequestRPM, config.SuccessfulRPM, now.UnixMilli(), l.redisMember(now)).Int()
	return result == 1, err
}

func (l *channelRateLimiter) redisRecordSuccess(ctx context.Context, channelID int, config ChannelRateLimitConfig) error {
	now := time.Now()
	_, err := common.RDB.Eval(ctx, channelRateLimitSuccessLua,
		[]string{l.redisKey(channelID, "success")},
		now.Add(-channelRateLimitWindow).UnixMilli(), now.UnixMilli(), l.redisMember(now)).Result()
	return err
}
