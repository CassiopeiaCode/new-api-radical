package model

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha1"
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
	defaultActiveTaskWindow      = 30 * time.Second
	maxActiveTaskWindow          = time.Hour
	activeTaskHistoryWindow      = 10 * time.Minute
	activeTaskHistoryThreshold   = 5
	activeTaskSimHashThreshold   = 5
)

var activeTaskSimHashSalt [16]byte

func init() {
	_, _ = rand.Read(activeTaskSimHashSalt[:])
}

// HighActiveTaskRecord is the low-frequency ten-minute abnormal-activity
// snapshot. Request bodies and SimHash values are never persisted.
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

type ActiveTaskUserCount struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	ActiveSlots int    `json:"active_slots"`
}

type ActiveTaskStats struct {
	GlobalActiveSlots int                   `json:"global_active_slots"`
	GlobalLimit       int                   `json:"global_limit"`
	UserLimit         int                   `json:"user_limit"`
	WindowSeconds     int64                 `json:"window_seconds"`
	ActiveUsers       int                   `json:"active_users"`
	Rank              []ActiveTaskUserCount `json:"rank"`
}

type activeTaskProfile struct {
	userID      int
	username    string
	fingerprint uint64
	lastSeen    time.Time
	element     *list.Element
}

// ActiveTaskSlotManager retains only the legacy temporary SimHash activity
// slots used by the admin activity view and ten-minute abnormal snapshots.
type ActiveTaskSlotManager struct {
	mu          sync.Mutex
	profiles    map[string]*activeTaskProfile
	profileLRU  *list.List
	globalLimit int
	userLimit   int
}

const (
	activeTaskRedisGlobalKey  = "active_task_activity:global"
	activeTaskRedisUserPrefix = "active_task_activity:user:"
	activeTaskRedisMetaPrefix = "active_task_activity:meta:"
)

var activeTaskRecordScript = `
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

func newActiveTaskSlotManager(globalLimit, userLimit int) *ActiveTaskSlotManager {
	return &ActiveTaskSlotManager{
		profiles:    make(map[string]*activeTaskProfile),
		profileLRU:  list.New(),
		globalLimit: globalLimit,
		userLimit:   userLimit,
	}
}

var globalActiveTaskSlotManager = newActiveTaskSlotManager(activeTaskGlobalLimit(), activeTaskUserLimit())

func GetActiveTaskSlotManager() *ActiveTaskSlotManager { return globalActiveTaskSlotManager }

func (m *ActiveTaskSlotManager) usesRedis() bool {
	return common.RedisEnabled && common.RDB != nil
}

// RecordActivity applies the legacy temporary-slot algorithm to one chat
// request. The raw request is used only to calculate SimHash and is discarded.
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
	previousMember := m.findRedisActivityMember(userID, fingerprint, now.Add(-maxActiveTaskWindow).Unix())
	member := fmt.Sprintf("%d:%016x", userID, fingerprint)
	payload := strconv.Itoa(userID) + "|" + strings.ReplaceAll(username, "|", " ")
	ttl := int64((maxActiveTaskWindow * 2).Seconds())
	return common.RDB.Eval(
		context.Background(),
		activeTaskRecordScript,
		[]string{activeTaskRedisGlobalKey, activeTaskRedisUserKey(userID)},
		now.Unix(),
		now.Add(-maxActiveTaskWindow).Unix(),
		m.globalLimit,
		m.userLimit,
		member,
		payload,
		ttl,
		activeTaskRedisMetaPrefix,
		activeTaskRedisUserPrefix,
		previousMember,
	).Err()
}

func (m *ActiveTaskSlotManager) findRedisActivityMember(userID int, fingerprint uint64, cutoff int64) string {
	members, err := common.RDB.ZRangeByScore(context.Background(), activeTaskRedisUserKey(userID), &redis.ZRangeBy{
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

func (m *ActiveTaskSlotManager) recordLocalActivityLocked(userID int, username string, fingerprint uint64, now time.Time) {
	m.pruneLocalActivityLocked(now.Add(-maxActiveTaskWindow))
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

func (m *ActiveTaskSlotManager) ActivityStats(window time.Duration) ActiveTaskStats {
	if window <= 0 {
		window = defaultActiveTaskWindow
	}
	if window > maxActiveTaskWindow {
		window = maxActiveTaskWindow
	}
	if m.usesRedis() {
		stats, err := m.redisActivityStats(window)
		if err == nil {
			return stats
		}
		return ActiveTaskStats{GlobalLimit: m.globalLimit, UserLimit: m.userLimit, WindowSeconds: int64(window.Seconds())}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localActivityStatsLocked(window)
}

func (m *ActiveTaskSlotManager) localActivityStatsLocked(window time.Duration) ActiveTaskStats {
	now := time.Now()
	m.pruneLocalActivityLocked(now.Add(-maxActiveTaskWindow))
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
	members, err := common.RDB.ZRangeByScore(ctx, activeTaskRedisGlobalKey, &redis.ZRangeBy{
		Min: strconv.FormatInt(time.Now().Add(-window).Unix(), 10),
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
			keys = append(keys, activeTaskRedisMetaPrefix+member)
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
		WindowSeconds:     int64(window.Seconds()),
		ActiveUsers:       len(rank),
		Rank:              rank,
	}
}

func activeTaskRedisUserKey(userID int) string {
	return activeTaskRedisUserPrefix + strconv.Itoa(userID)
}

func (m *ActiveTaskSlotManager) SnapshotHighActivity() []HighActiveTaskRecord {
	stats := m.ActivityStats(activeTaskHistoryWindow)
	now := common.GetTimestamp()
	records := make([]HighActiveTaskRecord, 0, len(stats.Rank))
	for _, entry := range stats.Rank {
		if entry.ActiveSlots < activeTaskHistoryThreshold || IsAdmin(entry.UserID) {
			continue
		}
		records = append(records, HighActiveTaskRecord{
			CreatedAt:         now,
			UserID:            entry.UserID,
			Username:          entry.Username,
			ActiveSlots:       entry.ActiveSlots,
			GlobalActiveSlots: stats.GlobalActiveSlots,
			GlobalLimit:       stats.GlobalLimit,
			UserLimit:         stats.UserLimit,
		})
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

// activeTaskSimHash preserves the legacy algorithm: raw request text,
// strings.Fields tokenization, equal token weights, and Hamming matching.
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
