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
	"bytes"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

const (
	// 全局槽上限
	MaxGlobalSlots = 1000
	// 单用户槽上限
	MaxUserSlots = 50
	// 活跃时间窗口（秒）
	ActiveWindowSeconds = 30
	// 哈希前缀长度（字节）
	HashPrefixLen = 16
)

// 哈希层级长度: 8, 64, 512, 4096, 32768, 131072 (128k)
var hashLevels = []int{8, 64, 512, 4096, 32768, 131072}

// HashLevelCount 哈希层级数量
const HashLevelCount = 6

// MatchLevelCount 匹配时使用的层级数量（从最高级往下）
const MatchLevelCount = 2

// TaskSlot 单个任务槽
type TaskSlot struct {
	UserID       int
	Username     string
	UpdatedAt    int64                                // Unix 秒
	HashPrefix   [HashLevelCount][HashPrefixLen]byte  // 6个层级的哈希前缀
	MaxLevelIdx  int                                  // 数据长度对应的最高层级索引
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

// computeHashPrefixes 计算多级哈希前缀（增量计算，避免重复处理）
// 返回哈希数组和最高层级索引
func computeHashPrefixes(data string) ([HashLevelCount][HashPrefixLen]byte, int) {
	var result [HashLevelCount][HashPrefixLen]byte
	dataLen := len(data)
	maxLevelIdx := 0
	
	prevEnd := 0
	h := sha256.New()
	
	for i, level := range hashLevels {
		end := level
		if end > dataLen {
			end = dataLen
		}
		
		// 增量写入：只写入上次结束位置到当前位置的新数据
		if end > prevEnd {
			h.Write([]byte(data[prevEnd:end]))
			prevEnd = end
		}
		
		// 获取当前状态的哈希（不影响后续计算）
		sum := h.Sum(nil)
		copy(result[i][:], sum[:HashPrefixLen])
		
		// 记录数据长度能覆盖到的最高层级
		if dataLen >= level {
			maxLevelIdx = i
		}
	}
	
	return result, maxLevelIdx
}

// matchHashPrefix 检查是否有哈希匹配
// 基于当前请求的最高级往下 MatchLevelCount 个层级进行匹配
func matchHashPrefix(slotHash, newHash [HashLevelCount][HashPrefixLen]byte, slotMaxLevel, newMaxLevel int) bool {
	// 基于当前请求文本的最高级往下匹配
	startLevel := newMaxLevel - MatchLevelCount + 1
	if startLevel < 0 {
		startLevel = 0
	}
	
	// 只在当前请求的匹配范围内比较
	for i := startLevel; i <= newMaxLevel; i++ {
		// 槽的该层级必须有效（槽的数据长度也要覆盖到这一级）
		if i <= slotMaxLevel && bytes.Equal(slotHash[i][:], newHash[i][:]) {
			return true
		}
	}
	return false
}

// RecordTask 记录一次任务请求
// data: 用于计算哈希的原始数据（如请求内容的哈希）
func (m *ActiveTaskSlotManager) RecordTask(userID int, username string, data string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().Unix()
	hashPrefixes, maxLevelIdx := computeHashPrefixes(data)

	// 1. 在该用户的槽中查找可继承的槽
	userSlots := m.userSlotIdx[userID]
	for _, idx := range userSlots {
		slot := m.slots[idx]
		if matchHashPrefix(slot.HashPrefix, hashPrefixes, slot.MaxLevelIdx, maxLevelIdx) {
			// 找到匹配，更新时间和哈希
			slot.UpdatedAt = now
			slot.HashPrefix = hashPrefixes
			slot.MaxLevelIdx = maxLevelIdx
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
			m.reuseSlot(oldestIdx, userID, username, now, hashPrefixes, maxLevelIdx)
			return
		}
	}

	// 检查全局槽数是否已满
	if len(m.slots) >= MaxGlobalSlots {
		// LRU 淘汰全局最旧的槽
		if len(m.lruOrder) > 0 {
			oldestIdx := m.lruOrder[0]
			m.reuseSlot(oldestIdx, userID, username, now, hashPrefixes, maxLevelIdx)
			return
		}
	}

	// 3. 分配新槽
	newSlot := &TaskSlot{
		UserID:      userID,
		Username:    username,
		UpdatedAt:   now,
		HashPrefix:  hashPrefixes,
		MaxLevelIdx: maxLevelIdx,
	}
	newIdx := len(m.slots)
	m.slots = append(m.slots, newSlot)
	m.userSlotIdx[userID] = append(m.userSlotIdx[userID], newIdx)
	m.lruOrder = append(m.lruOrder, newIdx)
}

// reuseSlot 复用一个槽
func (m *ActiveTaskSlotManager) reuseSlot(idx int, newUserID int, username string, now int64, hashPrefixes [HashLevelCount][HashPrefixLen]byte, maxLevelIdx int) {
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
	oldSlot.HashPrefix = hashPrefixes
	oldSlot.MaxLevelIdx = maxLevelIdx

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
	HighActiveTaskScanInterval = 60
	// HighActiveTaskThreshold 告警阈值
	HighActiveTaskThreshold = 10
	// HighActiveTaskWindowSeconds 统计窗口（秒）
	HighActiveTaskWindowSeconds = 300
)

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
			scanAndNotifyHighActiveUsers()
		}
	}()
}

// scanAndNotifyHighActiveUsers 扫描并通知管理员高活跃用户
func scanAndNotifyHighActiveUsers() {
	manager := GetActiveTaskSlotManager()
	highActiveUsers := manager.GetHighActiveUsers(HighActiveTaskWindowSeconds, HighActiveTaskThreshold)
	
	if len(highActiveUsers) == 0 {
		return
	}
	
	// 构建通知内容
	content := fmt.Sprintf("检测到 %d 个用户在 %d 秒内活跃任务数超过 %d：\n", 
		len(highActiveUsers), HighActiveTaskWindowSeconds, HighActiveTaskThreshold)
	for _, u := range highActiveUsers {
		content += fmt.Sprintf("- 用户 %s (ID:%d): %d 个活跃任务\n", u.Username, u.UserID, u.ActiveSlots)
	}
	
	// 获取所有管理员并发送站内信
	notifyAllAdmins(content)
}

// notifyAllAdmins 给所有管理员发送站内信
func notifyAllAdmins(content string) {
	var admins []User
	err := DB.Where("role >= ?", 10).Select("id").Find(&admins).Error
	if err != nil {
		return
	}
	
	for _, admin := range admins {
		RecordLog(admin.Id, LogTypeSystem, content)
	}
}

// RecordActiveTaskSlot 记录活跃任务槽（从请求上下文中提取数据）
// 使用 用户ID + 模型名 + 请求体哈希 作为任务标识
func RecordActiveTaskSlot(c interface{}, userID int, username string, modelName string) {
	if userID <= 0 {
		return
	}

	// 构建用于哈希的数据
	// 使用模型名 + 请求ID（如果有）作为任务标识
	var data string
	if gc, ok := c.(interface{ GetString(string) string }); ok {
		requestID := gc.GetString("X-Request-Id")
		if requestID != "" {
			data = modelName + ":" + requestID
		} else {
			data = modelName + ":" + fmt.Sprintf("%d:%d", userID, time.Now().UnixNano())
		}
	} else {
		data = modelName + ":" + fmt.Sprintf("%d:%d", userID, time.Now().UnixNano())
	}

	manager := GetActiveTaskSlotManager()
	manager.RecordTask(userID, username, data)
}
