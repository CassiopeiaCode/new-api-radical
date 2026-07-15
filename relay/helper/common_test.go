package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type flushCountingWriter struct {
	gin.ResponseWriter
	flushes int
}

func (w *flushCountingWriter) Flush() {
	w.flushes++
	w.ResponseWriter.Flush()
}

func newFlushTestContext(t *testing.T) (*gin.Context, *flushCountingWriter) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	writer := &flushCountingWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	return c, writer
}

func TestStreamFlushPolicy_InitialEventsAndCoalescing(t *testing.T) {
	c, writer := newFlushTestContext(t)

	for i := 0; i < streamImmediateEventCount; i++ {
		require.NoError(t, StringData(c, "x"))
		require.Equal(t, i+1, writer.flushes, "each initial SSE event must flush immediately")
	}

	require.NoError(t, StringData(c, "x"))
	require.Equal(t, streamImmediateEventCount, writer.flushes, "fourth small event should be coalesced")

	time.Sleep(streamFlushTimeThreshold + 5*time.Millisecond)
	require.NoError(t, StringData(c, "x"))
	require.Equal(t, streamImmediateEventCount+1, writer.flushes, "elapsed flush interval must release coalesced bytes")
}

func TestStreamFlushPolicy_FinalFlushReleasesPendingData(t *testing.T) {
	c, writer := newFlushTestContext(t)
	for i := 0; i < streamImmediateEventCount+1; i++ {
		require.NoError(t, StringData(c, "x"))
	}
	require.Equal(t, streamImmediateEventCount, writer.flushes)

	require.NoError(t, FlushPendingWriter(c))
	require.Equal(t, streamImmediateEventCount+1, writer.flushes)
}
