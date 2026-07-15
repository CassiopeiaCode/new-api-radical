package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const linuxDOOAuthStateLifetime = 5 * time.Minute

type linuxDOOAuthState struct {
	Nonce  string `json:"n"`
	Origin string `json:"o"`
	Expiry int64  `json:"e"`
}

func requestOrigin(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	// Deployments commonly terminate TLS before Gin. Only accept the standard
	// proxy header when it contains one of the two valid schemes.
	if forwarded := strings.ToLower(strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + c.Request.Host
}

func normalizedHTTPSOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", false
	}
	return "https://" + strings.ToLower(u.Host), true
}

func signLinuxDOState(payload string) string {
	h := hmac.New(sha256.New, []byte(common.SessionSecret))
	_, _ = h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func createLinuxDOState(c *gin.Context, session sessions.Session, origin string) (string, error) {
	normalized, ok := normalizedHTTPSOrigin(origin)
	if !ok {
		return "", errors.New("LinuxDO OAuth origin is not allowed")
	}
	nonce := common.GetRandomString(32)
	claims := linuxDOOAuthState{Nonce: nonce, Origin: normalized, Expiry: time.Now().Add(linuxDOOAuthStateLifetime).Unix()}
	data, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	session.Set("oauth_state", nonce)
	session.Set("linuxdo_oauth_origin", normalized)
	return payload + "." + signLinuxDOState(payload), nil
}

func parseLinuxDOState(state string) (*linuxDOOAuthState, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !hmac.Equal([]byte(signLinuxDOState(parts[0])), []byte(parts[1])) {
		return nil, errors.New("invalid LinuxDO OAuth state signature")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid LinuxDO OAuth state payload")
	}
	claims := &linuxDOOAuthState{}
	if err := json.Unmarshal(data, claims); err != nil || claims.Nonce == "" || claims.Expiry < time.Now().Unix() {
		return nil, errors.New("expired or invalid LinuxDO OAuth state")
	}
	if normalized, ok := normalizedHTTPSOrigin(claims.Origin); !ok || normalized != claims.Origin {
		return nil, errors.New("invalid LinuxDO OAuth state origin")
	}
	return claims, nil
}

// relayLinuxDOCallback uses the site-local session instead of proxy-provided
// Host headers to decide whether this callback reached its source site. When
// it did not, it performs one bounded redirect to the signed source origin.
func relayLinuxDOCallback(c *gin.Context, session sessions.Session, state string) bool {
	claims, err := parseLinuxDOState(state)
	if err != nil {
		return false
	}
	if session.Get("oauth_state") == claims.Nonce && session.Get("linuxdo_oauth_origin") == claims.Origin {
		return false
	}
	if c.Query("linuxdo_relay") == "1" {
		return false
	}
	query := url.Values{}
	for _, key := range []string{"code", "state", "error", "error_description"} {
		if value := c.Query(key); value != "" {
			query.Set(key, value)
		}
	}
	query.Set("linuxdo_relay", "1")
	c.Redirect(http.StatusFound, claims.Origin+"/api/oauth/linuxdo?"+query.Encode())
	return true
}

func validateAndConsumeLinuxDOState(c *gin.Context, session sessions.Session, state string) (*linuxDOOAuthState, error) {
	claims, err := parseLinuxDOState(state)
	if err != nil {
		return nil, err
	}
	if session.Get("oauth_state") != claims.Nonce || session.Get("linuxdo_oauth_origin") != claims.Origin {
		return nil, errors.New("LinuxDO OAuth state does not match this session")
	}
	session.Delete("oauth_state")
	session.Delete("linuxdo_oauth_origin")
	if err := session.Save(); err != nil {
		return nil, err
	}
	return claims, nil
}
