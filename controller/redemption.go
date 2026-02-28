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
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

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
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.SearchRedemptions(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
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

const redemptionKeyMaxLength = 32

func AddRedemption(c *gin.Context) {
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
	if count > 100 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountMax)
		return
	}

	// 随机额度模式校验
	if req.RandomQuotaMode() {
		if req.QuotaMin == nil || req.QuotaMax == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "启用随机额度需同时提供 quota_min 与 quota_max",
			})
			return
		}
		if *req.QuotaMin <= 0 || *req.QuotaMax <= 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "quota_min 和 quota_max 必须大于 0",
			})
			return
		}
		if *req.QuotaMin > *req.QuotaMax {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "quota_min 不能大于 quota_max",
			})
			return
		}
	} else {
		if req.Quota <= 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "额度必须大于 0",
			})
			return
		}
	}

	if valid, msg := validateExpiredTime(c, req.ExpiredTime); !valid {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}

	keys := make([]string, 0, count)
	keyPrefix := strings.TrimSpace(req.KeyPrefix)

	// 校验前缀长度，确保至少有8个字符用于随机部分
	const minRandomLength = 8
	prefixLen := len(keyPrefix)
	if prefixLen > redemptionKeyMaxLength-minRandomLength {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "兑换码前缀过长，最多允许 " + strconv.Itoa(redemptionKeyMaxLength-minRandomLength) + " 个字符",
		})
		return
	}

	// 计算随机部分的长度
	randomLength := redemptionKeyMaxLength - prefixLen

	for i := 0; i < count; i++ {
		// 生成 Key：前缀 + 随机字符串（长度根据前缀动态调整）
		randomPart, err := common.GenerateRandomCharsKey(randomLength)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "生成兑换码失败: " + err.Error(),
				"data":    keys,
				"keys":    keys,
			})
			return
		}
		key := keyPrefix + randomPart

		// 确定额度：随机模式或固定模式
		quota := req.Quota
		if req.RandomQuotaMode() {
			randomQuota, err := cryptoRandIntInclusive(*req.QuotaMin, *req.QuotaMax)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "生成随机额度失败: " + err.Error(),
					"data":    keys,
					"keys":    keys,
				})
				return
			}
			quota = randomQuota
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
			common.SysError("failed to insert redemption: " + err.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgRedemptionCreateFailed),
				"data":    keys,
				"keys":    keys,
			})
			return
		}
		keys = append(keys, key)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    keys,
		"keys":    keys,
	})
	return
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

func cryptoRandIntInclusive(min int, max int) (int, error) {
	if min > max {
		return 0, errors.New("invalid range: min > max")
	}
	rangeSize := new(big.Int).SetInt64(int64(max - min + 1))
	n, err := crand.Int(crand.Reader, rangeSize)
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + min, nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// MySQL: Error 1062 / Duplicate entry
	if strings.Contains(msg, "error 1062") || strings.Contains(msg, "duplicate entry") {
		return true
	}
	// PostgreSQL: SQLSTATE 23505 / duplicate key value violates unique constraint
	if strings.Contains(msg, "sqlstate 23505") || strings.Contains(msg, "duplicate key value violates unique constraint") {
		return true
	}
	// SQLite: UNIQUE constraint failed
	if strings.Contains(msg, "unique constraint failed") {
		return true
	}
	return false
}
