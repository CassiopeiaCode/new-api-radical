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

// 哈希层级长度
var hashLevels = []int{8, 64, 512, 4096}

// TaskSlot 单个任务槽
type TaskSlot struct {
	UserID     int
	Username   string
	UpdatedAt  int64    // Unix 秒
	HashPrefix [4][HashPrefixLen]byte // 4个层级的哈希前缀
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

// computeHashPrefixes 计算多级哈希前缀
func computeHashPrefixes(data string) [4][HashPrefixLen]byte {
	var result [4][HashPrefixLen]byte
	for i, level := range hashLevels {
		// 取 data 的前 level 个字符（或全部）
		end := level
		if end > len(data) {
			end = len(data)
		}
		hash := sha256.Sum256([]byte(data[:end]))
		copy(result[i][:], hash[:HashPrefixLen])
	}
	return result
}

// matchHashPrefix 检查是否有任意一级哈希匹配
func matchHashPrefix(a, b [4][HashPrefixLen]byte) bool {
	for i := 0; i < 4; i++ {
		if bytes.Equal(a[i][:], b[i][:]) {
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
	hashPrefixes := computeHashPrefixes(data)

	// 1. 在该用户的槽中查找可继承的槽
	userSlots := m.userSlotIdx[userID]
	for _, idx := range userSlots {
		slot := m.slots[idx]
		if matchHashPrefix(slot.HashPrefix, hashPrefixes) {
			// 找到匹配，更新时间和哈希
			slot.UpdatedAt = now
			slot.HashPrefix = hashPrefixes
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
			m.reuseSlot(oldestIdx, userID, username, now, hashPrefixes)
			return
		}
	}

	// 检查全局槽数是否已满
	if len(m.slots) >= MaxGlobalSlots {
		// LRU 淘汰全局最旧的槽
		if len(m.lruOrder) > 0 {
			oldestIdx := m.lruOrder[0]
			m.reuseSlot(oldestIdx, userID, username, now, hashPrefixes)
			return
		}
	}

	// 3. 分配新槽
	newSlot := &TaskSlot{
		UserID:     userID,
		Username:   username,
		UpdatedAt:  now,
		HashPrefix: hashPrefixes,
	}
	newIdx := len(m.slots)
	m.slots = append(m.slots, newSlot)
	m.userSlotIdx[userID] = append(m.userSlotIdx[userID], newIdx)
	m.lruOrder = append(m.lruOrder, newIdx)
}

// reuseSlot 复用一个槽
func (m *ActiveTaskSlotManager) reuseSlot(idx int, newUserID int, username string, now int64, hashPrefixes [4][HashPrefixLen]byte) {
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
