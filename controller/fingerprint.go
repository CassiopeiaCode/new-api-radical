package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type RecordFingerprintRequest struct {
	VisitorId string `json:"visitor_id" binding:"required"`
}

// RecordFingerprint 记录用户指纹
func RecordFingerprint(c *gin.Context) {
	var req RecordFingerprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	userId := c.GetInt("id")
	if userId == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户未登录",
		})
		return
	}

	userAgent := c.GetHeader("User-Agent")
	ip := c.ClientIP()

	err := model.RecordFingerprint(userId, req.VisitorId, userAgent, ip)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "记录失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// GetUserFingerprints 获取用户的指纹历史
func GetUserFingerprints(c *gin.Context) {
	userId := c.GetInt("id")
	fingerprints, err := model.GetUserFingerprints(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    fingerprints,
	})
}

// GetAllFingerprints 获取所有指纹记录（管理员）
func GetAllFingerprints(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	fingerprints, total, err := model.GetAllFingerprints(pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(fingerprints)
	common.ApiSuccess(c, pageInfo)
}

// SearchFingerprints 搜索指纹记录（管理员）
func SearchFingerprints(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)

	fingerprints, total, err := model.SearchFingerprints(keyword, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(fingerprints)
	common.ApiSuccess(c, pageInfo)
}

// FindUsersByVisitorId 查找相同visitor id的用户（管理员）
func FindUsersByVisitorId(c *gin.Context) {
	visitorId := c.Query("visitor_id")
	if visitorId == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "visitor_id 不能为空",
		})
		return
	}

	pageInfo := common.GetPageQuery(c)
	users, total, err := model.FindUsersByVisitorId(visitorId, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}

// GetDuplicateVisitorIds 获取有多个用户使用的visitor id列表（管理员）
func GetDuplicateVisitorIds(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	duplicates, total, err := model.GetDuplicateVisitorIds(pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(duplicates)
	common.ApiSuccess(c, pageInfo)
}
