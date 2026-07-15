package controller

import (
	"strconv"

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
