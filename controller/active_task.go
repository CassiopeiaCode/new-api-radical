/*
Copyright (C) 2025 QuantumNous

活跃任务 API 控制器
*/

package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetActiveTaskRankAPI 获取活跃任务排名
// GET /api/active_task/rank
// 参数：
// - window: 时间窗口（秒），默认30
// - limit: 返回数量限制，默认50，最大200
func GetActiveTaskRankAPI(c *gin.Context) {
	windowSeconds, _ := strconv.ParseInt(c.Query("window"), 10, 64)
	if windowSeconds <= 0 {
		windowSeconds = model.ActiveWindowSeconds
	}
	if windowSeconds > 3600 {
		windowSeconds = 3600 // 最大1小时
	}

	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	manager := model.GetActiveTaskSlotManager()
	rank := manager.GetActiveTaskRank(windowSeconds)

	// 限制返回数量
	if len(rank) > limit {
		rank = rank[:limit]
	}

	common.ApiSuccess(c, gin.H{
		"rank":           rank,
		"window_seconds": windowSeconds,
	})
}

// GetActiveTaskStatsAPI 获取活跃任务统计信息
// GET /api/active_task/stats
func GetActiveTaskStatsAPI(c *gin.Context) {
	manager := model.GetActiveTaskSlotManager()
	stats := manager.GetStats()
	common.ApiSuccess(c, stats)
}
