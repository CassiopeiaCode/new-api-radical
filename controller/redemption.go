package controller

import (
	crand "crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const redemptionBulkCreateMaxCount = 100000

func GetAllRedemptions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.GetAllRedemptions(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func SearchRedemptions(c *gin.Context) {
	keyword := c.Query("keyword")
	status := c.Query("status")
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.SearchRedemptions(keyword, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetRedemption(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption, err := model.GetRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    redemption,
	})
	return
}

func AddRedemption(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	req := dto.CreateRedemptionRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if utf8.RuneCountInString(req.Name) == 0 || utf8.RuneCountInString(req.Name) > 20 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionNameLength)
		return
	}
	count := req.EffectiveCount()
	if count <= 0 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountPositive)
		return
	}
	if count > redemptionBulkCreateMaxCount {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountMax)
		return
	}
	if valid, msg := validateExpiredTime(c, req.ExpiredTime); !valid {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	if req.RandomEnabled() {
		if req.RandomMin == nil || req.RandomMax == nil {
			common.ApiError(c, errors.New("random_min and random_max are required for random redemption generation"))
			return
		}
		if *req.RandomMin > *req.RandomMax {
			common.ApiError(c, errors.New("random_min cannot exceed random_max"))
			return
		}
		if !randomRangeHasCapacity(*req.RandomMin, *req.RandomMax, count) {
			common.ApiError(c, errors.New("random quota range is smaller than requested count"))
			return
		}
		// The persisted key column is char(32). Keep room for a signed int64
		// suffix so prefix-based random codes remain portable across databases.
		if utf8.RuneCountInString(req.RandomPrefix) > 12 {
			common.ApiError(c, errors.New("random_prefix is too long"))
			return
		}
	}

	keys := make([]string, 0, count)
	seen := make(map[int64]struct{}, count)
	maxAttempts := count * 10
	for len(keys) < count && maxAttempts > 0 {
		maxAttempts--
		quota := req.Quota
		key := common.GetUUID()
		if req.RandomEnabled() {
			value, randomErr := cryptoRandInt64Inclusive(*req.RandomMin, *req.RandomMax)
			if randomErr != nil {
				common.ApiError(c, randomErr)
				return
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			quota = int(value)
			key = req.RandomPrefix + strconv.FormatInt(value, 10)
		}
		cleanRedemption := model.Redemption{
			UserId:      c.GetInt("id"),
			Name:        req.Name,
			Key:         key,
			CreatedTime: common.GetTimestamp(),
			Quota:       quota,
			ExpiredTime: req.ExpiredTime,
		}
		err = cleanRedemption.Insert()
		if err != nil {
			if isUniqueConstraintError(err) {
				continue
			}
			common.SysError("failed to insert redemption: " + err.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgRedemptionCreateFailed),
				"data":    keys,
			})
			return
		}
		keys = append(keys, key)
	}
	if len(keys) != count {
		common.ApiError(c, errors.New("failed to generate enough unique redemption codes"))
		return
	}
	recordManageAudit(c, "redemption.create", map[string]interface{}{
		"name":  req.Name,
		"count": count,
		"quota": logger.LogQuota(req.Quota),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    keys,
	})
	return
}

func cryptoRandInt64Inclusive(min, max int64) (int64, error) {
	if min > max {
		return 0, errors.New("invalid random redemption range")
	}
	rangeSize := new(big.Int).Sub(big.NewInt(max), big.NewInt(min))
	rangeSize.Add(rangeSize, big.NewInt(1))
	n, err := crand.Int(crand.Reader, rangeSize)
	if err != nil {
		return 0, err
	}
	return new(big.Int).Add(n, big.NewInt(min)).Int64(), nil
}

func randomRangeHasCapacity(min, max int64, count int) bool {
	if count <= 0 || min > max {
		return false
	}
	capacity := new(big.Int).Sub(big.NewInt(max), big.NewInt(min))
	capacity.Add(capacity, big.NewInt(1))
	return capacity.Cmp(big.NewInt(int64(count))) >= 0
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "sqlstate 23505") ||
		strings.Contains(message, "unique constraint failed")
}

func DeleteRedemption(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateRedemption(c *gin.Context) {
	statusOnly := c.Query("status_only")
	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cleanRedemption, err := model.GetRedemptionById(redemption.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if statusOnly == "" {
		if valid, msg := validateExpiredTime(c, redemption.ExpiredTime); !valid {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
			return
		}
		// If you add more fields, please also update redemption.Update()
		cleanRedemption.Name = redemption.Name
		cleanRedemption.Quota = redemption.Quota
		cleanRedemption.ExpiredTime = redemption.ExpiredTime
	}
	if statusOnly != "" {
		cleanRedemption.Status = redemption.Status
	}
	err = cleanRedemption.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanRedemption,
	})
	return
}

func DeleteInvalidRedemption(c *gin.Context) {
	rows, err := model.DeleteInvalidRedemptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

func validateExpiredTime(c *gin.Context, expired int64) (bool, string) {
	if expired != 0 && expired < common.GetTimestamp() {
		return false, i18n.T(c, i18n.MsgRedemptionExpireTimeInvalid)
	}
	return true, ""
}
