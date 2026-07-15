package model

import (
	"container/list"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
)

const (
	defaultActiveTaskGlobalLimit = 1000
	defaultActiveTaskUserLimit   = 50
	defaultActiveTaskLease       = 2 * time.Hour
	defaultActiveTaskWindow      = 30 * time.Second
	activeTaskProfileLimit       = 4096
	activeTaskProfileUserLimit   = 128
)

var (
	ErrActiveTaskGlobalLimit = errors.New("global active task limit reached")
	ErrActiveTaskUserLimit   = errors.New("user active task limit reached")
)

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

// ActiveTaskUsage is intentionally sourced from the existing quota_data
// aggregate table. This keeps historic token accounting intact and avoids a
// migration of the high-volume log tables.
type ActiveTaskUsage struct {
	ModelName    string `json:"model_name"`
	TokenUsed    int64  `json:"token_used"`
	RequestCount int64  `json:"request_count"`
}

func GetUserRecentTokenUsage(userID int, since int64, limit int) ([]ActiveTaskUsage, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	usage := make([]ActiveTaskUsage, 0)
	err := DB.Table("quota_data").
		Select("model_name, SUM(token_used) AS token_used, SUM(count) AS request_count").
		Where("user_id = ? AND created_at >= ?", userID, since).
		Group("model_name").
		Order("token_used DESC, model_name ASC").
		Limit(limit).
		Scan(&usage).Error
	return usage, err
}

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
// never reuses another task's capacity; SimHash/LRU are used solely to retain a
// bounded activity profile for diagnostics and grouping.
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

// Acquire reserves one real task slot. Limits are checked while holding the
// same lock as insertion, so a failed request cannot over-admit under local
// concurrency. State is intentionally rebuilt empty after a restart; this is
// safe recovery rather than retaining an unreleaseable in-memory reservation.
func (m *ActiveTaskSlotManager) Acquire(userID int, username, modelName string, requestBody []byte) (*ActiveTaskSlot, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
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
	m.slots[slot.Token] = slot
	if m.userTokens[userID] == nil {
		m.userTokens[userID] = make(map[string]struct{})
	}
	m.userTokens[userID][slot.Token] = struct{}{}
	m.recordProfileLocked(slot)
	return cloneActiveTaskSlot(slot), nil
}

func (m *ActiveTaskSlotManager) AttachTaskID(token, taskID string) bool {
	if token == "" || taskID == "" {
		return false
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

// Release is idempotent and is used for submit failures, client cancellation,
// terminal task states, and lease expiry.
func (m *ActiveTaskSlotManager) Release(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.releaseLocked(token)
}

func (m *ActiveTaskSlotManager) ReleaseByTaskID(taskID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.releaseLocked(m.taskTokens[taskID])
}

func (m *ActiveTaskSlotManager) SweepExpired() int {
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

func (m *ActiveTaskSlotManager) recordProfileLocked(slot *ActiveTaskSlot) {
	key := fmt.Sprintf("%d:%016x", slot.UserID, slot.Fingerprint>>16) // first 48 SimHash bits: coarse grouping level
	if profile := m.profiles[key]; profile != nil {
		profile.username = slot.Username
		profile.fingerprint = slot.Fingerprint
		profile.lastSeen = slot.AcquiredAt
		m.profileLRU.MoveToBack(profile.element)
		return
	}
	profile := &activeTaskProfile{userID: slot.UserID, username: slot.Username, fingerprint: slot.Fingerprint, lastSeen: slot.AcquiredAt}
	profile.element = m.profileLRU.PushBack(key)
	m.profiles[key] = profile
	for len(m.profiles) > activeTaskProfileLimit || m.userProfileCountLocked(slot.UserID) > activeTaskProfileUserLimit {
		front := m.profileLRU.Front()
		if front == nil {
			break
		}
		oldKey := front.Value.(string)
		delete(m.profiles, oldKey)
		m.profileLRU.Remove(front)
	}
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(time.Now())
	rank := m.rankLocked()
	return ActiveTaskStats{GlobalActiveSlots: len(m.slots), GlobalLimit: m.globalLimit, UserLimit: m.userLimit, LeaseSeconds: int64(m.lease.Seconds()), ActiveUsers: len(m.userTokens), Rank: rank}
}

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
	stats := m.Stats()
	now := common.GetTimestamp()
	records := make([]HighActiveTaskRecord, 0, len(stats.Rank))
	for _, entry := range stats.Rank {
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

// activeTaskSimHash is a true 64-bit SimHash. It tokenizes a bounded request
// prefix, hashes each feature independently, and votes per bit. The retained
// profile key uses the high 48 bits as a coarse level; callers retain the full
// hash for future finer-grained comparisons without persisting request content.
func activeTaskSimHash(modelName string, requestBody []byte) uint64 {
	const maxBytes = 64 * 1024
	if len(requestBody) > maxBytes {
		requestBody = requestBody[:maxBytes]
	}
	text := modelName + " " + string(requestBody)
	weights := [64]int{}
	feature := strings.Builder{}
	addFeature := func() {
		if feature.Len() == 0 {
			return
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(feature.String()))
		value := h.Sum64()
		for bit := 0; bit < 64; bit++ {
			if value&(uint64(1)<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
		feature.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' {
			feature.WriteRune(r)
			if feature.Len() >= 64 {
				addFeature()
			}
		} else {
			addFeature()
		}
	}
	addFeature()
	var result uint64
	for bit, weight := range weights {
		if weight >= 0 {
			result |= uint64(1) << bit
		}
	}
	return result
}
