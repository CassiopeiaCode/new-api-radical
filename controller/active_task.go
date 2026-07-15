package controller

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetActiveTaskStats exposes only operational aggregate state to admins. The
// slot manager never includes request bodies, credentials, or prompt content.
func GetActiveTaskStats(c *gin.Context) {
	common.ApiSuccess(c, model.GetActiveTaskSlotManager().Stats())
}

func GetHighActiveTaskHistory(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userID, _ := strconv.Atoi(c.Query("user_id"))
	records, total, err := model.ListHighActiveTaskRecords(pageInfo, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

// GetMyRecentTokenUsage is intentionally authenticated as the current user;
// there is no user-id selector on this route, preventing cross-account usage
// disclosure. quota_data is hourly, so the time range begins at 24 hours ago.
func GetMyRecentTokenUsage(c *gin.Context) {
	usage, err := model.GetUserRecentTokenUsage(c.GetInt("id"), time.Now().Add(-24*time.Hour).Unix(), 100)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"window_hours": 24, "items": usage})
}
