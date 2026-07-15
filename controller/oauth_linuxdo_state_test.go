package controller

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestLinuxDOStateOriginMatchesRequest(t *testing.T) {
	t.Setenv("LINUXDO_OAUTH_ALLOWED_ORIGINS", "https://elysiver.h-e.top,https://elysia.h-e.top")

	tests := []struct {
		name          string
		origin        string
		currentOrigin string
		want          bool
	}{
		{
			name:          "exact HTTPS origin",
			origin:        "https://elysiver.h-e.top",
			currentOrigin: "https://elysiver.h-e.top",
			want:          true,
		},
		{
			name:          "allowlisted HTTPS origin terminated to HTTP",
			origin:        "https://elysiver.h-e.top",
			currentOrigin: "http://elysiver.h-e.top",
			want:          true,
		},
		{
			name:          "different host is rejected",
			origin:        "https://elysiver.h-e.top",
			currentOrigin: "http://attacker.example",
			want:          false,
		},
		{
			name:          "non allowlisted host is rejected",
			origin:        "https://unlisted.example",
			currentOrigin: "http://unlisted.example",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linuxDOStateOriginMatchesRequest(tt.origin, tt.currentOrigin); got != tt.want {
				t.Fatalf("linuxDOStateOriginMatchesRequest(%q, %q) = %t, want %t", tt.origin, tt.currentOrigin, got, tt.want)
			}
		})
	}
}

func signedLinuxDOStateForTest(t *testing.T, origin string) string {
	t.Helper()
	originalSecret := common.SessionSecret
	common.SessionSecret = "linuxdo-oauth-test-secret"
	t.Cleanup(func() {
		common.SessionSecret = originalSecret
	})
	data, err := json.Marshal(linuxDOOAuthState{
		Nonce:  "test-nonce",
		Origin: origin,
		Expiry: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	return payload + "." + signLinuxDOState(payload)
}

func TestRelayLinuxDOCallbackForwardsOnlyCrossSiteState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LINUXDO_OAUTH_ALLOWED_ORIGINS", "https://elysiver.h-e.top,https://elysia.h-e.top")
	t.Setenv("LINUXDO_OAUTH_CALLBACK_URL", "https://elysiver.h-e.top/api/oauth/linuxdo")

	t.Run("forwards a signed elysia state from the fixed callback", func(t *testing.T) {
		state := signedLinuxDOStateForTest(t, "https://elysia.h-e.top")
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "http://elysiver.h-e.top/api/oauth/linuxdo?code=authorization-code&state="+url.QueryEscape(state), nil)

		if !relayLinuxDOCallback(context, state) {
			t.Fatal("expected the fixed callback to relay a cross-site state")
		}
		if recorder.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
		}
		location := recorder.Header().Get("Location")
		if !strings.HasPrefix(location, "https://elysia.h-e.top/api/oauth/linuxdo?") {
			t.Fatalf("unexpected relay location: %q", location)
		}
	})

	t.Run("keeps a same-site state on the callback host", func(t *testing.T) {
		state := signedLinuxDOStateForTest(t, "https://elysiver.h-e.top")
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "http://elysiver.h-e.top/api/oauth/linuxdo?state="+url.QueryEscape(state), nil)

		if relayLinuxDOCallback(context, state) {
			t.Fatal("same-site state must be validated locally instead of relayed")
		}
	})
}
