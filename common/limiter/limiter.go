package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

//go:embed lua/rate_limit.lua
var rateLimitScript string

type RedisLimiter struct {
	client         *redis.Client
	limitScriptSHA string
	// scriptMu serializes script reloads after Redis loses its in-memory
	// script cache (for example after a Redis restart).
	scriptMu sync.Mutex
}

var (
	instance *RedisLimiter
	once     sync.Once
)

func New(ctx context.Context, r *redis.Client) *RedisLimiter {
	once.Do(func() {
		instance = &RedisLimiter{
			client:         r,
		}
		// 预加载脚本；失败时 Allow 仍会尝试重新加载，避免启动时的瞬时
		// Redis 故障把限流器永久置于不可用状态。
		if err := instance.reloadScript(ctx); err != nil {
			common.SysLog(fmt.Sprintf("Failed to load rate limit script: %v", err))
		}
	})

	return instance
}

func (rl *RedisLimiter) reloadScript(ctx context.Context) error {
	rl.scriptMu.Lock()
	defer rl.scriptMu.Unlock()
	sha, err := rl.client.ScriptLoad(ctx, rateLimitScript).Result()
	if err == nil {
		rl.limitScriptSHA = sha
	}
	return err
}

func isNoScriptError(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "NOSCRIPT")
}

func (rl *RedisLimiter) Allow(ctx context.Context, key string, opts ...Option) (bool, error) {
	// 默认配置
	config := &Config{
		Capacity:  10,
		Rate:      1,
		Requested: 1,
	}

	// 应用选项模式
	for _, opt := range opts {
		opt(config)
	}

	// 执行限流
	result, err := rl.client.EvalSha(
		ctx,
		rl.limitScriptSHA,
		[]string{key},
		config.Requested,
		config.Rate,
		config.Capacity,
	).Int()
	if isNoScriptError(err) {
		// Redis does not persist Lua scripts across restart. Reload and retry;
		// another process may have already reloaded it, so tolerate a reload
		// race and use the SHA currently stored on the limiter.
		if reloadErr := rl.reloadScript(ctx); reloadErr != nil {
			return false, fmt.Errorf("rate limit failed: reload script: %w", reloadErr)
		}
		result, err = rl.client.EvalSha(
			ctx,
			rl.limitScriptSHA,
			[]string{key},
			config.Requested,
			config.Rate,
			config.Capacity,
		).Int()
	}

	if err != nil {
		return false, fmt.Errorf("rate limit failed: %w", err)
	}
	return result == 1, nil
}

// Config 配置选项模式
type Config struct {
	Capacity  int64
	Rate      int64
	Requested int64
}

type Option func(*Config)

func WithCapacity(c int64) Option {
	return func(cfg *Config) { cfg.Capacity = c }
}

func WithRate(r int64) Option {
	return func(cfg *Config) { cfg.Rate = r }
}

func WithRequested(n int64) Option {
	return func(cfg *Config) { cfg.Requested = n }
}
