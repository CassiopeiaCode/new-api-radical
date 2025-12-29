package controller

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// Checkin 用户签到
func Checkin(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()

	if !setting.CheckinEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "签到功能未启用",
		})
		return
	}

	userId := c.GetInt("id")

	// 检查今天是否已签到
	hasChecked, err := model.HasCheckedInToday(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "检查签到状态失败",
		})
		return
	}

	if hasChecked {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "今天已经签到过了，明天再来吧",
		})
		return
	}

	// 计算奖励额度
	var quota int
	if setting.CheckinRandomMode {
		rand.Seed(time.Now().UnixNano())
		minQ := setting.CheckinMinQuota
		maxQ := setting.CheckinMaxQuota
		if maxQ <= minQ {
			quota = minQ
		} else {
			quota = minQ + rand.Intn(maxQ-minQ+1)
		}
	} else {
		quota = setting.CheckinQuota
	}

	// 创建签到记录（唯一索引防止并发重复）
	err = model.CreateCheckinRecord(userId, quota)
	if err != nil {
		// 唯一索引冲突说明已签到
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "今天已经签到过了",
		})
		return
	}

	// 增加用户额度
	err = model.IncreaseUserQuota(userId, quota, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "增加额度失败",
		})
		return
	}

	// 记录日志
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("签到奖励 %s", logger.LogQuota(quota)))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "签到成功",
		"data": gin.H{
			"quota": quota,
		},
	})
}

// GetCheckinStatus 获取签到状态
func GetCheckinStatus(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	userId := c.GetInt("id")

	hasChecked, _ := model.HasCheckedInToday(userId)
	checkinCount, _ := model.GetUserCheckinCount(userId)
	totalQuota, _ := model.GetUserTotalCheckinQuota(userId)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":       setting.CheckinEnabled,
			"has_checked":   hasChecked,
			"today_date":    model.GetTodayDateUTC8(),
			"checkin_count": checkinCount,
			"total_quota":   totalQuota,
		},
	})
}

// GetCheckinHistory 获取签到历史
func GetCheckinHistory(c *gin.Context) {
	userId := c.GetInt("id")

	records, err := model.GetUserCheckinHistory(userId, 30)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取签到历史失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    records,
	})
}
