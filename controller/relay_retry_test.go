package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestShouldStopRetryForClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	if shouldStopRetryForClientDisconnect(c) {
		t.Fatal("unexpected retry stop before request context cancellation")
	}

	cancel()

	if !shouldStopRetryForClientDisconnect(c) {
		t.Fatal("expected retry stop after request context cancellation")
	}
}
