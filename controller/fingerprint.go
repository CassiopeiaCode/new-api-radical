package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type RecordFingerprintRequest struct {
	VisitorID string `json:"visitor_id" binding:"required"`
}

func RecordFingerprint(c *gin.Context) {
	var request RecordFingerprintRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "invalid fingerprint payload")
		return
	}
	if err := model.RecordFingerprint(c.GetInt("id"), request.VisitorID, c.GetHeader("User-Agent"), c.ClientIP()); err != nil {
		common.ApiErrorMsg(c, "fingerprint was not recorded: "+err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

func GetUserFingerprints(c *gin.Context) {
	items, err := model.GetUserFingerprints(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func GetAllFingerprints(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.GetAllFingerprints(pageInfo, c.Query("keyword"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func FindUsersByFingerprint(c *gin.Context) {
	visitorID, ip := strings.TrimSpace(c.Query("visitor_id")), strings.TrimSpace(c.Query("ip"))
	if visitorID == "" && ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "visitor_id or ip is required"})
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.FindUsersByFingerprint(visitorID, ip, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetDuplicateFingerprints(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.GetDuplicateFingerprints(pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
