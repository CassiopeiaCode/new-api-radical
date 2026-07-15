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
	currentOrigin := requestOrigin(c)
	normalized, ok := normalizedHTTPSOrigin(origin)
	if !ok || !linuxDOStateOriginMatchesRequest(normalized, currentOrigin) {
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

// relayLinuxDOCallback makes one bounded relay hop when LinuxDO calls the
// operator-selected callback host but the signed state was created on another
// site. Once the request reaches the source host, this returns false so that
// the normal session-bound validation consumes the state locally.
func relayLinuxDOCallback(c *gin.Context, state string) bool {
	claims, err := parseLinuxDOState(state)
	if err != nil || linuxDOStateOriginMatchesRequest(claims.Origin, requestOrigin(c)) {
		return false
	}
	query := url.Values{}
	for _, key := range []string{"code", "state", "error", "error_description"} {
		if value := c.Query(key); value != "" {
			query.Set(key, value)
		}
	}
	c.Redirect(http.StatusFound, claims.Origin+"/api/oauth/linuxdo?"+query.Encode())
	return true
}

// linuxDOStateOriginMatchesRequest accepts an HTTPS origin that arrived as
// HTTP through a TLS-terminating proxy only when its host is unchanged. The
// signed, expiring state and source-host session nonce remain mandatory.
func linuxDOStateOriginMatchesRequest(origin string, currentOrigin string) bool {
	if origin == currentOrigin {
		return true
	}

	normalizedOrigin, ok := normalizedHTTPSOrigin(origin)
	if !ok {
		return false
	}
	current, err := url.Parse(currentOrigin)
	if err != nil || current.Scheme != "http" || current.Host == "" || current.User != nil || current.RawQuery != "" || current.Fragment != "" || (current.Path != "" && current.Path != "/") {
		return false
	}
	return normalizedOrigin == "https://"+strings.ToLower(current.Host)
}

func validateAndConsumeLinuxDOState(c *gin.Context, session sessions.Session, state string) (*linuxDOOAuthState, error) {
	claims, err := parseLinuxDOState(state)
	if err != nil {
		return nil, err
	}
	if !linuxDOStateOriginMatchesRequest(claims.Origin, requestOrigin(c)) || session.Get("oauth_state") != claims.Nonce || session.Get("linuxdo_oauth_origin") != claims.Origin {
		return nil, errors.New("LinuxDO OAuth state does not match this session")
	}
	session.Delete("oauth_state")
	session.Delete("linuxdo_oauth_origin")
	if err := session.Save(); err != nil {
		return nil, err
	}
	return claims, nil
}
