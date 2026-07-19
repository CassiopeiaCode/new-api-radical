package controller

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const iframeJWTLifetime = 60 * time.Second

type iframeJWTClaims struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	jwt.RegisteredClaims
}

func signIframeJWT(user *model.User, secret string, now time.Time) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", errors.New("iframe JWT is not configured")
	}

	claims := iframeJWTClaims{
		Id:          user.Id,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(iframeJWTLifetime)),
		},
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func GenerateIframeJWT(c *gin.Context) {
	if c.GetBool("use_access_token") {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "iframe JWT requires dashboard session authentication",
		})
		return
	}

	secret := common.GetEnvOrDefaultString("IFRAME_JWT_SECRET", "")
	if strings.TrimSpace(secret) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "iframe JWT is not configured",
		})
		return
	}

	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	token, err := signIframeJWT(user, secret, time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"token":      token,
			"expires_in": int(iframeJWTLifetime.Seconds()),
		},
	})
}
