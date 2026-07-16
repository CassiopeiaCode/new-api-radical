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
	windowSeconds, _ := strconv.ParseInt(c.Query("window"), 10, 64)
	if windowSeconds <= 0 {
		windowSeconds = 30
	}
	if windowSeconds > 3600 {
		windowSeconds = 3600
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	stats := model.GetActiveTaskSlotManager().ActivityStats(time.Duration(windowSeconds) * time.Second)
	if len(stats.Rank) > limit {
		stats.Rank = stats.Rank[:limit]
	}
	common.ApiSuccess(c, stats)
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
