/*
Copyright (C) 2025 QuantumNous

活跃任务槽管理器
- 全局上限 1000 槽
- 单用户上限 50 槽
- 每个槽存储：用户ID、时间戳、多级哈希（8, 64, 512, 4096 长度各16字节）
- 继承逻辑：先在同用户槽中匹配，匹配不到则 LRU 淘汰
*/

package model

import (
	"crypto/rand"
	"crypto/sha1"
	"math/bits"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// 全局槽上限
	MaxGlobalSlots = 1000
	// 单用户槽上限
	MaxUserSlots = 50
	// 活跃时间窗口（秒）
	ActiveWindowSeconds = 30
	// SimHash 海明距离阈值（<= 视为同一活跃任务槽）
	SimHashThreshold = 5
)

var simhashTokenSalt [16]byte

func init() {
	// 每次进程启动生成随机盐：让 SimHash 的 token 哈希在不同进程间不可直接对比
	// 目的：降低被外部推断/复现指纹的可行性（同时会导致跨重启的活跃槽“继承”不连续，这是预期行为）
	_, _ = rand.Read(simhashTokenSalt[:])
}

// TaskSlot 单个任务槽
type TaskSlot struct {
	UserID    int
	Username  string
	UpdatedAt int64  // Unix 秒
	SimHash   uint64 // 基于原始请求体/数据的 SimHash 指纹
}

// ActiveTaskSlotManager 活跃任务槽管理器
type ActiveTaskSlotManager struct {
	mu          sync.RWMutex
	slots       []*TaskSlot           // 所有槽
	userSlotIdx map[int][]int         // 用户ID -> 槽索引列表
	lruOrder    []int                 // LRU 顺序（索引列表，最近使用的在后面）
}

var (
	activeTaskManager     *ActiveTaskSlotManager
	activeTaskManagerOnce sync.Once
)

// GetActiveTaskSlotManager 获取单例管理器
func GetActiveTaskSlotManager() *ActiveTaskSlotManager {
	activeTaskManagerOnce.Do(func() {
		activeTaskManager = &ActiveTaskSlotManager{
			slots:       make([]*TaskSlot, 0, MaxGlobalSlots),
			userSlotIdx: make(map[int][]int),
			lruOrder:    make([]int, 0, MaxGlobalSlots),
		}
	})
	return activeTaskManager
}

// simhash64 计算文本的 64-bit SimHash
// - 不做清洗/归一化：直接使用传入 data（通常是原始请求体）
// - 特征：strings.Fields 分词 token
// - 权重：每个 token 计 1（重复 token 会多次计入）
func simhash64(data string) uint64 {
	tokens := strings.Fields(data)
	if len(tokens) == 0 {
		return 0
	}

	var v [64]int
	for _, tok := range tokens {
		h := tokenHash64(tok)
		for i := 0; i < 64; i++ {
			if (h>>i)&1 == 1 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}

	var out uint64
	for i := 0; i < 64; i++ {
		if v[i] >= 0 {
			out |= 1 << i
		}
	}
	return out
}

func tokenHash64(token string) uint64 {
	h := sha1.New()
	_, _ = h.Write(simhashTokenSalt[:])
	_, _ = h.Write([]byte(token))
	sum := h.Sum(nil)
	// little-endian: match the sandbox script behavior
	return uint64(sum[0]) |
		uint64(sum[1])<<8 |
		uint64(sum[2])<<16 |
		uint64(sum[3])<<24 |
		uint64(sum[4])<<32 |
		uint64(sum[5])<<40 |
		uint64(sum[6])<<48 |
		uint64(sum[7])<<56
}

func hamming64(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// RecordTask 记录一次任务请求
// data: 用于计算 SimHash 的原始数据（默认是原始请求体；取不到时退化为 modelName）
func (m *ActiveTaskSlotManager) RecordTask(userID int, username string, data string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().Unix()
	newHash := simhash64(data)

	// 1. 在该用户的槽中查找可继承的槽（SimHash 距离 <= 阈值）
	userSlots := m.userSlotIdx[userID]
	for _, idx := range userSlots {
		slot := m.slots[idx]
		if hamming64(slot.SimHash, newHash) <= SimHashThreshold {
			// 找到匹配：更新时间，并用新指纹覆盖旧指纹
			slot.UpdatedAt = now
			slot.SimHash = newHash
			slot.Username = username
			m.moveToLRUEnd(idx)
			return
		}
	}

	// 2. 没有匹配，需要分配新槽
	// 检查用户槽数是否已满
	if len(userSlots) >= MaxUserSlots {
		// 淘汰该用户最旧的槽
		oldestIdx := m.findOldestUserSlot(userID)
		if oldestIdx >= 0 {
			m.reuseSlot(oldestIdx, userID, username, now, newHash)
			return
		}
	}

	// 检查全局槽数是否已满
	if len(m.slots) >= MaxGlobalSlots {
		// LRU 淘汰全局最旧的槽
		if len(m.lruOrder) > 0 {
			oldestIdx := m.lruOrder[0]
			m.reuseSlot(oldestIdx, userID, username, now, newHash)
			return
		}
	}

	// 3. 分配新槽
	newSlot := &TaskSlot{
		UserID:    userID,
		Username:  username,
		UpdatedAt: now,
		SimHash:   newHash,
	}
	newIdx := len(m.slots)
	m.slots = append(m.slots, newSlot)
	m.userSlotIdx[userID] = append(m.userSlotIdx[userID], newIdx)
	m.lruOrder = append(m.lruOrder, newIdx)
}

// reuseSlot 复用一个槽
func (m *ActiveTaskSlotManager) reuseSlot(idx int, newUserID int, username string, now int64, newHash uint64) {
	oldSlot := m.slots[idx]
	oldUserID := oldSlot.UserID

	// 从旧用户的索引中移除
	if oldUserID != newUserID {
		m.removeFromUserSlotIdx(oldUserID, idx)
		m.userSlotIdx[newUserID] = append(m.userSlotIdx[newUserID], idx)
	}

	// 更新槽数据
	oldSlot.UserID = newUserID
	oldSlot.Username = username
	oldSlot.UpdatedAt = now
	oldSlot.SimHash = newHash

	m.moveToLRUEnd(idx)
}

// removeFromUserSlotIdx 从用户槽索引中移除
func (m *ActiveTaskSlotManager) removeFromUserSlotIdx(userID int, idx int) {
	slots := m.userSlotIdx[userID]
	for i, v := range slots {
		if v == idx {
			m.userSlotIdx[userID] = append(slots[:i], slots[i+1:]...)
			break
		}
	}
	if len(m.userSlotIdx[userID]) == 0 {
		delete(m.userSlotIdx, userID)
	}
}

// findOldestUserSlot 找到用户最旧的槽
func (m *ActiveTaskSlotManager) findOldestUserSlot(userID int) int {
	userSlots := m.userSlotIdx[userID]
	if len(userSlots) == 0 {
		return -1
	}

	oldestIdx := userSlots[0]
	oldestTime := m.slots[oldestIdx].UpdatedAt
	for _, idx := range userSlots[1:] {
		if m.slots[idx].UpdatedAt < oldestTime {
			oldestIdx = idx
			oldestTime = m.slots[idx].UpdatedAt
		}
	}
	return oldestIdx
}

// moveToLRUEnd 将槽移动到 LRU 末尾（最近使用）
func (m *ActiveTaskSlotManager) moveToLRUEnd(idx int) {
	for i, v := range m.lruOrder {
		if v == idx {
			m.lruOrder = append(m.lruOrder[:i], m.lruOrder[i+1:]...)
			break
		}
	}
	m.lruOrder = append(m.lruOrder, idx)
}

// UserActiveTaskCount 用户活跃任务统计
type UserActiveTaskCount struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	ActiveSlots int    `json:"active_slots"`
}

// GetActiveTaskRank 获取指定时间窗口内的活跃任务排名
// windowSeconds: 时间窗口（秒），默认30秒
func (m *ActiveTaskSlotManager) GetActiveTaskRank(windowSeconds int64) []UserActiveTaskCount {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if windowSeconds <= 0 {
		windowSeconds = ActiveWindowSeconds
	}

	now := time.Now().Unix()
	cutoff := now - windowSeconds

	// 统计每个用户的活跃槽数
	userCounts := make(map[int]*UserActiveTaskCount)
	for _, slot := range m.slots {
		if slot.UpdatedAt >= cutoff {
			if _, exists := userCounts[slot.UserID]; !exists {
				userCounts[slot.UserID] = &UserActiveTaskCount{
					UserID:   slot.UserID,
					Username: slot.Username,
				}
			}
			userCounts[slot.UserID].ActiveSlots++
		}
	}

	// 转换为切片并排序
	result := make([]UserActiveTaskCount, 0, len(userCounts))
	for _, v := range userCounts {
		result = append(result, *v)
	}

	// 按活跃槽数降序排序
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].ActiveSlots > result[i].ActiveSlots {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// GetStats 获取管理器统计信息
func (m *ActiveTaskSlotManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now().Unix()
	activeCount := 0
	for _, slot := range m.slots {
		if slot.UpdatedAt >= now-ActiveWindowSeconds {
			activeCount++
		}
	}

	return map[string]interface{}{
		"total_slots":       len(m.slots),
		"active_slots":      activeCount,
		"max_global_slots":  MaxGlobalSlots,
		"max_user_slots":    MaxUserSlots,
		"active_users":      len(m.userSlotIdx),
		"window_seconds":    ActiveWindowSeconds,
	}
}

// 高活跃任务告警相关常量
const (
	// HighActiveTaskScanInterval 扫描间隔（秒）
	HighActiveTaskScanInterval = 600 // 10分钟
	// HighActiveTaskThreshold 告警阈值
	HighActiveTaskThreshold = 5
	// HighActiveTaskWindowSeconds 统计窗口（秒）
	HighActiveTaskWindowSeconds = 600 // 10分钟
)

// HighActiveTaskRecord 高活跃任务历史记录
type HighActiveTaskRecord struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int    `json:"user_id" gorm:"index"`
	Username    string `json:"username" gorm:"type:varchar(64)"`
	ActiveSlots int    `json:"active_slots"`
	WindowSecs  int    `json:"window_secs"`
	CreatedAt   int64  `json:"created_at" gorm:"index"`
}

func (HighActiveTaskRecord) TableName() string {
	return "high_active_task_records"
}

// GetHighActiveUsers 获取指定时间窗口内活跃任务数超过阈值的用户
func (m *ActiveTaskSlotManager) GetHighActiveUsers(windowSeconds int64, threshold int) []UserActiveTaskCount {
	rank := m.GetActiveTaskRank(windowSeconds)
	var result []UserActiveTaskCount
	for _, u := range rank {
		if u.ActiveSlots >= threshold {
			result = append(result, u)
		}
	}
	return result
}

// StartHighActiveTaskScanner 启动高活跃任务扫描器
func StartHighActiveTaskScanner() {
	go func() {
		ticker := time.NewTicker(HighActiveTaskScanInterval * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			scanAndSaveHighActiveUsers()
		}
	}()
}

// scanAndSaveHighActiveUsers 扫描并保存高活跃用户到数据库
func scanAndSaveHighActiveUsers() {
	manager := GetActiveTaskSlotManager()
	highActiveUsers := manager.GetHighActiveUsers(HighActiveTaskWindowSeconds, HighActiveTaskThreshold)
	
	if len(highActiveUsers) == 0 {
		return
	}
	
	now := time.Now().Unix()
	for _, u := range highActiveUsers {
		// 排除管理员
		if IsAdmin(u.UserID) {
			continue
		}
		record := HighActiveTaskRecord{
			UserId:      u.UserID,
			Username:    u.Username,
			ActiveSlots: u.ActiveSlots,
			WindowSecs:  HighActiveTaskWindowSeconds,
			CreatedAt:   now,
		}
		DB.Create(&record)
	}
}

// GetHighActiveTaskHistory 获取高活跃任务历史记录
func GetHighActiveTaskHistory(startTime, endTime int64, userId int, limit int) ([]HighActiveTaskRecord, error) {
	var records []HighActiveTaskRecord
	query := DB.Model(&HighActiveTaskRecord{})
	
	if startTime > 0 {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("created_at <= ?", endTime)
	}
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	
	if limit <= 0 {
		limit = 100
	}
	
	err := query.Order("created_at desc").Limit(limit).Find(&records).Error
	return records, err
}

// RecordActiveTaskSlot 记录活跃任务槽（从请求上下文中提取数据）
// 使用请求体内容计算多级哈希，识别同一对话的连续请求
// 只对 chat 类请求统计，embedding 等不统计
func RecordActiveTaskSlot(c interface{}, userID int, username string, modelName string) {
	if userID <= 0 {
		return
	}

	type ginContext interface {
		Get(string) (interface{}, bool)
		Request() interface{}
	}

	gc, ok := c.(*gin.Context)
	if !ok {
		return
	}

	// 通过请求路径判断是否为 chat 类请求
	requestPath := gc.Request.URL.Path

	// 只对 chat 类请求统计活跃任务
	isChatRequest := strings.Contains(requestPath, "/chat/completions") ||
		strings.Contains(requestPath, "/v1/completions") ||
		strings.Contains(requestPath, "/v1/responses") ||
		strings.Contains(requestPath, "/v1/messages") ||
		(strings.Contains(requestPath, "/v1beta/models/") && strings.Contains(requestPath, "generateContent"))

	if !isChatRequest {
		return
	}

	var data string
	if body, exists := gc.Get("key_request_body"); exists {
		if bodyBytes, ok := body.([]byte); ok && len(bodyBytes) > 0 {
			data = string(bodyBytes)
		}
	}

	if data == "" {
		data = modelName
	}

	manager := GetActiveTaskSlotManager()
	manager.RecordTask(userID, username, data)
}
