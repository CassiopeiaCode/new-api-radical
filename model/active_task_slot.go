package model

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const (
	defaultActiveTaskGlobalLimit = 1000
	defaultActiveTaskUserLimit   = 50
	defaultActiveTaskLease       = 2 * time.Hour
	defaultActiveTaskWindow      = 30 * time.Second
	maxActiveTaskActivityWindow  = time.Hour
	activeTaskHistoryWindow      = 10 * time.Minute
	activeTaskHistoryThreshold   = 5
	activeTaskSimHashThreshold   = 5
)

var (
	ErrActiveTaskGlobalLimit = errors.New("global active task limit reached")
	ErrActiveTaskUserLimit   = errors.New("user active task limit reached")
	activeTaskSimHashSalt    [16]byte
)

func init() {
	_, _ = rand.Read(activeTaskSimHashSalt[:])
}

// HighActiveTaskRecord is a durable, low-frequency observation snapshot. It
// deliberately does not duplicate task or consumption data: active slots are
// hot process state, while this table is only for administrative history.
type HighActiveTaskRecord struct {
	ID                int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_active_task_history_created_user,priority:1"`
	UserID            int    `json:"user_id" gorm:"index:idx_active_task_history_created_user,priority:2;index"`
	Username          string `json:"username" gorm:"type:varchar(64);index"`
	ActiveSlots       int    `json:"active_slots"`
	GlobalActiveSlots int    `json:"global_active_slots"`
	GlobalLimit       int    `json:"global_limit"`
	UserLimit         int    `json:"user_limit"`
}

func (HighActiveTaskRecord) TableName() string { return "high_active_task_records" }

func ListHighActiveTaskRecords(pageInfo *common.PageInfo, userID int) ([]HighActiveTaskRecord, int64, error) {
	query := DB.Model(&HighActiveTaskRecord{})
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	records := make([]HighActiveTaskRecord, 0)
	if err := query.Order("created_at DESC, id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

// ActiveTaskSlot is one actual in-flight asynchronous task. A similar request
// never reuses another task's capacity. Conversational activity profiles are
// tracked separately and do not consume these slots.
type ActiveTaskSlot struct {
	Token       string
	TaskID      string
	UserID      int
	Username    string
	ModelName   string
	Fingerprint uint64
	AcquiredAt  time.Time
	ExpiresAt   time.Time
}

type activeTaskProfile struct {
	userID      int
	username    string
	fingerprint uint64
	lastSeen    time.Time
	element     *list.Element
}

type ActiveTaskUserCount struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	ActiveSlots int    `json:"active_slots"`
}

type ActiveTaskStats struct {
	GlobalActiveSlots int                   `json:"global_active_slots"`
	GlobalLimit       int                   `json:"global_limit"`
	UserLimit         int                   `json:"user_limit"`
	LeaseSeconds      int64                 `json:"lease_seconds"`
	WindowSeconds     int64                 `json:"window_seconds"`
	ActiveUsers       int                   `json:"active_users"`
	Rank              []ActiveTaskUserCount `json:"rank"`
}

type ActiveTaskSlotManager struct {
	mu          sync.Mutex
	slots       map[string]*ActiveTaskSlot
	taskTokens  map[string]string
	userTokens  map[int]map[string]struct{}
	profiles    map[string]*activeTaskProfile
	profileLRU  *list.List
	globalLimit int
	userLimit   int
	lease       time.Duration
}

const (
	activeTaskRedisGlobalKey  = "active_task_slots:global"
	activeTaskRedisSlotPrefix = "active_task_slot:"
	activeTaskRedisTaskPrefix = "active_task_slot_task:"

	activeTaskActivityGlobalKey  = "active_task_activity:global"
	activeTaskActivityUserPrefix = "active_task_activity:user:"
	activeTaskActivityMetaPrefix = "active_task_activity:meta:"
)

var activeTaskRedisAcquireScript = `
local now = tonumber(ARGV[1])
local expires = tonumber(ARGV[2])
local globalLimit = tonumber(ARGV[3])
local userLimit = tonumber(ARGV[4])
local token = ARGV[5]
local payload = ARGV[6]
local ttl = tonumber(ARGV[7])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
if redis.call('ZCARD', KEYS[1]) >= globalLimit then return 1 end
if redis.call('ZCARD', KEYS[2]) >= userLimit then return 2 end
redis.call('ZADD', KEYS[1], expires, token)
redis.call('ZADD', KEYS[2], expires, token)
redis.call('SET', KEYS[3], payload, 'EX', ttl)
return 0
`

var activeTaskRedisReleaseScript = `
local payload = redis.call('GET', KEYS[2])
if not payload then return 0 end
local separator = string.find(payload, '|', 1, true)
if not separator then return 0 end
local userID = string.sub(payload, 1, separator - 1)
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', 'active_task_slots:user:' .. userID, ARGV[1])
redis.call('DEL', KEYS[2])
return 1
`

var activeTaskActivityRecordScript = `
local globalKey = KEYS[1]
local userKey = KEYS[2]
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local globalLimit = tonumber(ARGV[3])
local userLimit = tonumber(ARGV[4])
local member = ARGV[5]
local payload = ARGV[6]
local ttl = tonumber(ARGV[7])
local metaPrefix = ARGV[8]
local userPrefix = ARGV[9]
local previousMember = ARGV[10]

local function removeMember(value)
  local metadata = redis.call('GET', metaPrefix .. value)
  redis.call('ZREM', globalKey, value)
  if metadata then
    local separator = string.find(metadata, '|', 1, true)
    if separator then
      local userID = string.sub(metadata, 1, separator - 1)
      redis.call('ZREM', userPrefix .. userID, value)
    end
  end
  redis.call('DEL', metaPrefix .. value)
end

local expired = redis.call('ZRANGEBYSCORE', globalKey, '-inf', cutoff)
for _, value in ipairs(expired) do
  removeMember(value)
end

if previousMember ~= '' and previousMember ~= member then
  removeMember(previousMember)
end

if redis.call('ZSCORE', userKey, member) == false and redis.call('ZCARD', userKey) >= userLimit then
  local oldest = redis.call('ZRANGE', userKey, 0, 0)
  if oldest[1] then removeMember(oldest[1]) end
end

if redis.call('ZSCORE', globalKey, member) == false and redis.call('ZCARD', globalKey) >= globalLimit then
  local oldest = redis.call('ZRANGE', globalKey, 0, 0)
  if oldest[1] then removeMember(oldest[1]) end
end

redis.call('ZADD', globalKey, now, member)
redis.call('ZADD', userKey, now, member)
redis.call('SET', metaPrefix .. member, payload, 'EX', ttl)
redis.call('EXPIRE', globalKey, ttl)
redis.call('EXPIRE', userKey, ttl)
return 1
`

func activeTaskGlobalLimit() int {
	value := common.GetEnvOrDefault("ACTIVE_TASK_SLOT_GLOBAL_LIMIT", defaultActiveTaskGlobalLimit)
	if value < 1 {
		return defaultActiveTaskGlobalLimit
	}
	if value > 100000 {
		return 100000
	}
	return value
}

func activeTaskUserLimit() int {
	value := common.GetEnvOrDefault("ACTIVE_TASK_SLOT_USER_LIMIT", defaultActiveTaskUserLimit)
	if value < 1 {
		return defaultActiveTaskUserLimit
	}
	if value > 10000 {
		return 10000
	}
	return value
}

func activeTaskLease() time.Duration {
	seconds := common.GetEnvOrDefault("ACTIVE_TASK_SLOT_LEASE_SECONDS", int(defaultActiveTaskLease.Seconds()))
	if seconds < 60 {
		seconds = int(defaultActiveTaskLease.Seconds())
	}
	if seconds > 7*24*60*60 {
		seconds = 7 * 24 * 60 * 60
	}
	return time.Duration(seconds) * time.Second
}

func newActiveTaskSlotManager(globalLimit, userLimit int, lease time.Duration) *ActiveTaskSlotManager {
	return &ActiveTaskSlotManager{
		slots:       make(map[string]*ActiveTaskSlot),
		taskTokens:  make(map[string]string),
		userTokens:  make(map[int]map[string]struct{}),
		profiles:    make(map[string]*activeTaskProfile),
		profileLRU:  list.New(),
		globalLimit: globalLimit,
		userLimit:   userLimit,
		lease:       lease,
	}
}

var globalActiveTaskSlotManager = newActiveTaskSlotManager(activeTaskGlobalLimit(), activeTaskUserLimit(), activeTaskLease())

func GetActiveTaskSlotManager() *ActiveTaskSlotManager { return globalActiveTaskSlotManager }

func (m *ActiveTaskSlotManager) usesRedis() bool {
	return common.RedisEnabled && common.RDB != nil
}

// Acquire reserves one real task slot. When Redis is configured, its Lua
// transaction is the cross-instance authority for both limits and lease
// recovery. Without Redis, the process-local lock remains a safe single-node
// fallback. In neither mode are stale in-memory reservations retained across a
// restart.
func (m *ActiveTaskSlotManager) Acquire(userID int, username, modelName string, requestBody []byte) (*ActiveTaskSlot, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	token, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return nil, fmt.Errorf("generate active task slot token: %w", err)
	}
	now := time.Now()
	slot := &ActiveTaskSlot{
		Token:       "ats_" + token,
		UserID:      userID,
		Username:    username,
		ModelName:   modelName,
		Fingerprint: activeTaskSimHash(modelName, requestBody),
		AcquiredAt:  now,
		ExpiresAt:   now.Add(m.lease),
	}
	if m.usesRedis() {
		if err := m.acquireRedisSlot(slot); err != nil {
			return nil, err
		}
		return cloneActiveTaskSlot(slot), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(time.Now())
	if len(m.slots) >= m.globalLimit {
		return nil, ErrActiveTaskGlobalLimit
	}
	if len(m.userTokens[userID]) >= m.userLimit {
		return nil, ErrActiveTaskUserLimit
	}
	m.slots[slot.Token] = slot
	if m.userTokens[userID] == nil {
		m.userTokens[userID] = make(map[string]struct{})
	}
	m.userTokens[userID][slot.Token] = struct{}{}
	return cloneActiveTaskSlot(slot), nil
}

// RecordActivity refreshes one short-lived conversational activity profile.
// It is deliberately separate from Acquire/Release: ordinary chat traffic is
// observable here but never consumes asynchronous task capacity.
func (m *ActiveTaskSlotManager) RecordActivity(userID int, username, modelName string, requestBody []byte) error {
	if userID <= 0 {
		return nil
	}
	fingerprint := activeTaskSimHash(modelName, requestBody)
	now := time.Now()
	if m.usesRedis() {
		return m.recordRedisActivity(userID, username, fingerprint, now)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordLocalActivityLocked(userID, username, fingerprint, now)
	return nil
}

func (m *ActiveTaskSlotManager) recordRedisActivity(userID int, username string, fingerprint uint64, now time.Time) error {
	previousMember := m.findRedisActivityMember(userID, fingerprint, now.Add(-maxActiveTaskActivityWindow).Unix())
	member := fmt.Sprintf("%d:%016x", userID, fingerprint)
	payload := strconv.Itoa(userID) + "|" + strings.ReplaceAll(username, "|", " ")
	ttl := int64((maxActiveTaskActivityWindow * 2).Seconds())
	return common.RDB.Eval(
		context.Background(),
		activeTaskActivityRecordScript,
		[]string{activeTaskActivityGlobalKey, activeTaskActivityUserKey(userID)},
		now.Unix(),
		now.Add(-maxActiveTaskActivityWindow).Unix(),
		m.globalLimit,
		m.userLimit,
		member,
		payload,
		ttl,
		activeTaskActivityMetaPrefix,
		activeTaskActivityUserPrefix,
		previousMember,
	).Err()
}

func (m *ActiveTaskSlotManager) findRedisActivityMember(userID int, fingerprint uint64, cutoff int64) string {
	members, err := common.RDB.ZRangeByScore(context.Background(), activeTaskActivityUserKey(userID), &redis.ZRangeBy{
		Min: strconv.FormatInt(cutoff, 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		return ""
	}
	bestMember := ""
	bestDistance := activeTaskSimHashThreshold + 1
	for _, member := range members {
		separator := strings.LastIndexByte(member, ':')
		if separator < 0 || separator == len(member)-1 {
			continue
		}
		candidate, err := strconv.ParseUint(member[separator+1:], 16, 64)
		if err != nil {
			continue
		}
		distance := bits.OnesCount64(candidate ^ fingerprint)
		if distance <= activeTaskSimHashThreshold && distance < bestDistance {
			bestMember = member
			bestDistance = distance
		}
	}
	return bestMember
}

func (m *ActiveTaskSlotManager) acquireRedisSlot(slot *ActiveTaskSlot) error {
	now := time.Now().Unix()
	ttl := int64(m.lease.Seconds())
	payload := strconv.Itoa(slot.UserID) + "|" + slot.Username
	result, err := common.RDB.Eval(
		context.Background(),
		activeTaskRedisAcquireScript,
		[]string{activeTaskRedisGlobalKey, activeTaskRedisUserKey(slot.UserID), activeTaskRedisSlotKey(slot.Token)},
		now,
		slot.ExpiresAt.Unix(),
		m.globalLimit,
		m.userLimit,
		slot.Token,
		payload,
		ttl,
	).Int()
	if err != nil {
		return fmt.Errorf("acquire distributed active task slot: %w", err)
	}
	switch result {
	case 0:
		return nil
	case 1:
		return ErrActiveTaskGlobalLimit
	case 2:
		return ErrActiveTaskUserLimit
	default:
		return fmt.Errorf("unexpected distributed active task slot result: %d", result)
	}
}

func (m *ActiveTaskSlotManager) AttachTaskID(token, taskID string) bool {
	if token == "" || taskID == "" {
		return false
	}
	if m.usesRedis() {
		return m.attachRedisTaskID(token, taskID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(time.Now())
	slot := m.slots[token]
	if slot == nil {
		return false
	}
	if slot.TaskID != "" && slot.TaskID != taskID {
		return false
	}
	if oldToken, exists := m.taskTokens[taskID]; exists && oldToken != token {
		return false
	}
	slot.TaskID = taskID
	m.taskTokens[taskID] = token
	return true
}

func (m *ActiveTaskSlotManager) attachRedisTaskID(token, taskID string) bool {
	ctx := context.Background()
	ttl, err := common.RDB.TTL(ctx, activeTaskRedisSlotKey(token)).Result()
	if err != nil || ttl <= 0 {
		return false
	}
	key := activeTaskRedisTaskKey(taskID)
	created, err := common.RDB.SetNX(ctx, key, token, ttl).Result()
	return err == nil && created
}

// Release is idempotent and is used for submit failures, client cancellation,
// terminal task states, and lease expiry.
func (m *ActiveTaskSlotManager) Release(token string) bool {
	if m.usesRedis() {
		return m.releaseRedisSlot(token)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.releaseLocked(token)
}

func (m *ActiveTaskSlotManager) ReleaseByTaskID(taskID string) bool {
	if m.usesRedis() {
		ctx := context.Background()
		token, err := common.RDB.Get(ctx, activeTaskRedisTaskKey(taskID)).Result()
		if err != nil || token == "" {
			return false
		}
		if !m.releaseRedisSlot(token) {
			return false
		}
		_ = common.RDB.Del(ctx, activeTaskRedisTaskKey(taskID)).Err()
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.releaseLocked(m.taskTokens[taskID])
}

func (m *ActiveTaskSlotManager) releaseRedisSlot(token string) bool {
	if token == "" {
		return false
	}
	result, err := common.RDB.Eval(
		context.Background(),
		activeTaskRedisReleaseScript,
		[]string{activeTaskRedisGlobalKey, activeTaskRedisSlotKey(token)},
		token,
	).Int()
	return err == nil && result == 1
}

func (m *ActiveTaskSlotManager) SweepExpired() int {
	if m.usesRedis() {
		removed, err := common.RDB.ZRemRangeByScore(context.Background(), activeTaskRedisGlobalKey, "-inf", strconv.FormatInt(time.Now().Unix(), 10)).Result()
		if err != nil {
			return 0
		}
		return int(removed)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expireLocked(time.Now())
}

func (m *ActiveTaskSlotManager) expireLocked(now time.Time) int {
	count := 0
	for token, slot := range m.slots {
		if !slot.ExpiresAt.After(now) && m.releaseLocked(token) {
			count++
		}
	}
	return count
}

func (m *ActiveTaskSlotManager) releaseLocked(token string) bool {
	slot := m.slots[token]
	if slot == nil {
		return false
	}
	delete(m.slots, token)
	if slot.TaskID != "" && m.taskTokens[slot.TaskID] == token {
		delete(m.taskTokens, slot.TaskID)
	}
	if tokens := m.userTokens[slot.UserID]; tokens != nil {
		delete(tokens, token)
		if len(tokens) == 0 {
			delete(m.userTokens, slot.UserID)
		}
	}
	return true
}

func (m *ActiveTaskSlotManager) recordLocalActivityLocked(userID int, username string, fingerprint uint64, now time.Time) {
	m.pruneLocalActivityLocked(now.Add(-maxActiveTaskActivityWindow))
	for _, profile := range m.profiles {
		if profile.userID != userID || bits.OnesCount64(profile.fingerprint^fingerprint) > activeTaskSimHashThreshold {
			continue
		}
		profile.username = username
		profile.fingerprint = fingerprint
		profile.lastSeen = now
		m.profileLRU.MoveToBack(profile.element)
		return
	}

	for m.userProfileCountLocked(userID) >= m.userLimit {
		if !m.removeOldestLocalActivityLocked(userID) {
			break
		}
	}
	for len(m.profiles) >= m.globalLimit {
		if !m.removeOldestLocalActivityLocked(0) {
			break
		}
	}

	key := fmt.Sprintf("%d:%016x", userID, fingerprint)
	profile := &activeTaskProfile{userID: userID, username: username, fingerprint: fingerprint, lastSeen: now}
	profile.element = m.profileLRU.PushBack(key)
	m.profiles[key] = profile
}

func (m *ActiveTaskSlotManager) pruneLocalActivityLocked(cutoff time.Time) {
	for element := m.profileLRU.Front(); element != nil; {
		next := element.Next()
		key := element.Value.(string)
		profile := m.profiles[key]
		if profile == nil || profile.lastSeen.Before(cutoff) {
			delete(m.profiles, key)
			m.profileLRU.Remove(element)
		}
		element = next
	}
}

func (m *ActiveTaskSlotManager) removeOldestLocalActivityLocked(userID int) bool {
	for element := m.profileLRU.Front(); element != nil; element = element.Next() {
		key := element.Value.(string)
		profile := m.profiles[key]
		if profile != nil && (userID == 0 || profile.userID == userID) {
			delete(m.profiles, key)
			m.profileLRU.Remove(element)
			return true
		}
	}
	return false
}

func (m *ActiveTaskSlotManager) userProfileCountLocked(userID int) int {
	count := 0
	for _, profile := range m.profiles {
		if profile.userID == userID {
			count++
		}
	}
	return count
}

func (m *ActiveTaskSlotManager) Stats() ActiveTaskStats {
	if m.usesRedis() {
		stats, err := m.redisStats()
		if err == nil {
			return stats
		}
		// Capacity is fail-closed when Redis is enabled; do not quietly report
		// process-local values as a distributed global value after a backend outage.
		return ActiveTaskStats{GlobalLimit: m.globalLimit, UserLimit: m.userLimit, LeaseSeconds: int64(m.lease.Seconds())}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(time.Now())
	rank := m.rankLocked()
	return ActiveTaskStats{GlobalActiveSlots: len(m.slots), GlobalLimit: m.globalLimit, UserLimit: m.userLimit, LeaseSeconds: int64(m.lease.Seconds()), ActiveUsers: len(m.userTokens), Rank: rank}
}

// ActivityStats reports short-lived conversational activity rather than
// asynchronous task capacity. The two concepts share limits/configuration but
// never share reservations or lifecycle state.
func (m *ActiveTaskSlotManager) ActivityStats(window time.Duration) ActiveTaskStats {
	if window <= 0 {
		window = defaultActiveTaskWindow
	}
	if window > maxActiveTaskActivityWindow {
		window = maxActiveTaskActivityWindow
	}
	if m.usesRedis() {
		stats, err := m.redisActivityStats(window)
		if err == nil {
			return stats
		}
		return ActiveTaskStats{GlobalLimit: m.globalLimit, UserLimit: m.userLimit, LeaseSeconds: int64(defaultActiveTaskWindow.Seconds()), WindowSeconds: int64(window.Seconds())}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localActivityStatsLocked(window)
}

func (m *ActiveTaskSlotManager) localActivityStatsLocked(window time.Duration) ActiveTaskStats {
	now := time.Now()
	m.pruneLocalActivityLocked(now.Add(-maxActiveTaskActivityWindow))
	cutoff := now.Add(-window)
	counts := make(map[int]ActiveTaskUserCount)
	total := 0
	for _, profile := range m.profiles {
		if profile.lastSeen.Before(cutoff) {
			continue
		}
		entry := counts[profile.userID]
		entry.UserID = profile.userID
		entry.Username = profile.username
		entry.ActiveSlots++
		counts[profile.userID] = entry
		total++
	}
	return m.buildActivityStats(total, counts, window)
}

func (m *ActiveTaskSlotManager) redisActivityStats(window time.Duration) (ActiveTaskStats, error) {
	ctx := context.Background()
	now := time.Now()
	members, err := common.RDB.ZRangeByScore(ctx, activeTaskActivityGlobalKey, &redis.ZRangeBy{
		Min: strconv.FormatInt(now.Add(-window).Unix(), 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		return ActiveTaskStats{}, err
	}
	counts := make(map[int]ActiveTaskUserCount)
	total := 0
	if len(members) > 0 {
		keys := make([]string, 0, len(members))
		for _, member := range members {
			keys = append(keys, activeTaskActivityMetaPrefix+member)
		}
		values, err := common.RDB.MGet(ctx, keys...).Result()
		if err != nil {
			return ActiveTaskStats{}, err
		}
		for _, value := range values {
			payload, ok := value.(string)
			if !ok {
				continue
			}
			parts := strings.SplitN(payload, "|", 2)
			userID, err := strconv.Atoi(parts[0])
			if err != nil || userID <= 0 {
				continue
			}
			entry := counts[userID]
			entry.UserID = userID
			if len(parts) == 2 {
				entry.Username = parts[1]
			}
			entry.ActiveSlots++
			counts[userID] = entry
			total++
		}
	}
	return m.buildActivityStats(total, counts, window), nil
}

func (m *ActiveTaskSlotManager) buildActivityStats(total int, counts map[int]ActiveTaskUserCount, window time.Duration) ActiveTaskStats {
	rank := make([]ActiveTaskUserCount, 0, len(counts))
	for _, entry := range counts {
		rank = append(rank, entry)
	}
	sort.Slice(rank, func(i, j int) bool {
		if rank[i].ActiveSlots == rank[j].ActiveSlots {
			return rank[i].UserID < rank[j].UserID
		}
		return rank[i].ActiveSlots > rank[j].ActiveSlots
	})
	return ActiveTaskStats{
		GlobalActiveSlots: total,
		GlobalLimit:       m.globalLimit,
		UserLimit:         m.userLimit,
		LeaseSeconds:      int64(defaultActiveTaskWindow.Seconds()),
		WindowSeconds:     int64(window.Seconds()),
		ActiveUsers:       len(rank),
		Rank:              rank,
	}
}

func (m *ActiveTaskSlotManager) redisStats() (ActiveTaskStats, error) {
	ctx := context.Background()
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if err := common.RDB.ZRemRangeByScore(ctx, activeTaskRedisGlobalKey, "-inf", now).Err(); err != nil {
		return ActiveTaskStats{}, err
	}
	tokens, err := common.RDB.ZRange(ctx, activeTaskRedisGlobalKey, 0, -1).Result()
	if err != nil {
		return ActiveTaskStats{}, err
	}
	counts := make(map[int]ActiveTaskUserCount)
	if len(tokens) > 0 {
		keys := make([]string, 0, len(tokens))
		for _, token := range tokens {
			keys = append(keys, activeTaskRedisSlotKey(token))
		}
		values, err := common.RDB.MGet(ctx, keys...).Result()
		if err != nil {
			return ActiveTaskStats{}, err
		}
		for _, value := range values {
			payload, ok := value.(string)
			if !ok {
				continue
			}
			parts := strings.SplitN(payload, "|", 2)
			userID, err := strconv.Atoi(parts[0])
			if err != nil || userID <= 0 {
				continue
			}
			entry := counts[userID]
			entry.UserID = userID
			if len(parts) == 2 {
				entry.Username = parts[1]
			}
			entry.ActiveSlots++
			counts[userID] = entry
		}
	}
	rank := make([]ActiveTaskUserCount, 0, len(counts))
	for _, entry := range counts {
		rank = append(rank, entry)
	}
	sort.Slice(rank, func(i, j int) bool {
		if rank[i].ActiveSlots == rank[j].ActiveSlots {
			return rank[i].UserID < rank[j].UserID
		}
		return rank[i].ActiveSlots > rank[j].ActiveSlots
	})
	return ActiveTaskStats{GlobalActiveSlots: len(tokens), GlobalLimit: m.globalLimit, UserLimit: m.userLimit, LeaseSeconds: int64(m.lease.Seconds()), ActiveUsers: len(rank), Rank: rank}, nil
}

func activeTaskRedisUserKey(userID int) string {
	return "active_task_slots:user:" + strconv.Itoa(userID)
}

func activeTaskActivityUserKey(userID int) string {
	return activeTaskActivityUserPrefix + strconv.Itoa(userID)
}

func activeTaskRedisSlotKey(token string) string { return activeTaskRedisSlotPrefix + token }

func activeTaskRedisTaskKey(taskID string) string { return activeTaskRedisTaskPrefix + taskID }

func (m *ActiveTaskSlotManager) rankLocked() []ActiveTaskUserCount {
	rank := make([]ActiveTaskUserCount, 0, len(m.userTokens))
	for userID, tokens := range m.userTokens {
		username := ""
		for token := range tokens {
			username = m.slots[token].Username
			break
		}
		rank = append(rank, ActiveTaskUserCount{UserID: userID, Username: username, ActiveSlots: len(tokens)})
	}
	sort.Slice(rank, func(i, j int) bool {
		if rank[i].ActiveSlots == rank[j].ActiveSlots {
			return rank[i].UserID < rank[j].UserID
		}
		return rank[i].ActiveSlots > rank[j].ActiveSlots
	})
	return rank
}

func (m *ActiveTaskSlotManager) SnapshotHighActivity() []HighActiveTaskRecord {
	stats := m.ActivityStats(activeTaskHistoryWindow)
	now := common.GetTimestamp()
	records := make([]HighActiveTaskRecord, 0, len(stats.Rank))
	for _, entry := range stats.Rank {
		if entry.ActiveSlots < activeTaskHistoryThreshold {
			continue
		}
		if IsAdmin(entry.UserID) {
			continue
		}
		records = append(records, HighActiveTaskRecord{CreatedAt: now, UserID: entry.UserID, Username: entry.Username, ActiveSlots: entry.ActiveSlots, GlobalActiveSlots: stats.GlobalActiveSlots, GlobalLimit: stats.GlobalLimit, UserLimit: stats.UserLimit})
	}
	return records
}

func PersistHighActiveTaskSnapshot() (int, error) {
	records := GetActiveTaskSlotManager().SnapshotHighActivity()
	if len(records) == 0 {
		return 0, nil
	}
	if err := DB.Create(&records).Error; err != nil {
		return 0, err
	}
	return len(records), nil
}

func cloneActiveTaskSlot(slot *ActiveTaskSlot) *ActiveTaskSlot {
	copy := *slot
	return &copy
}

// activeTaskSimHash preserves the legacy activity-slot meaning: the raw
// request body is split with strings.Fields, every token has equal weight, and
// a process-local random salt prevents retained fingerprints from being useful
// across restarts. modelName is used only when the request body is unavailable.
func activeTaskSimHash(modelName string, requestBody []byte) uint64 {
	text := string(requestBody)
	if text == "" {
		text = modelName
	}
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return 0
	}
	weights := [64]int{}
	for _, token := range tokens {
		h := sha1.New()
		_, _ = h.Write(activeTaskSimHashSalt[:])
		_, _ = h.Write([]byte(token))
		sum := h.Sum(nil)
		value := uint64(sum[0]) |
			uint64(sum[1])<<8 |
			uint64(sum[2])<<16 |
			uint64(sum[3])<<24 |
			uint64(sum[4])<<32 |
			uint64(sum[5])<<40 |
			uint64(sum[6])<<48 |
			uint64(sum[7])<<56
		for bit := 0; bit < 64; bit++ {
			if value&(uint64(1)<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
	}
	var result uint64
	for bit, weight := range weights {
		if weight >= 0 {
			result |= uint64(1) << bit
		}
	}
	return result
}
