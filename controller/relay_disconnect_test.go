package controller

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestShouldStopRetryForClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)

	if shouldStopRetryForClientDisconnect(c) {
		t.Fatal("active request must not stop retry")
	}
	cancel()
	if !shouldStopRetryForClientDisconnect(c) {
		t.Fatal("cancelled request must stop retry")
	}
}
