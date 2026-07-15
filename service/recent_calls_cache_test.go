package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendStreamChunkBufferedUntilFinalize(t *testing.T) {
	t.Parallel()

	cch := newRecentCallsCache(RecentCallsCacheConfig{Capacity: 8})
	t.Cleanup(func() {
		if cch.tempSessionDir != "" {
			_ = os.RemoveAll(cch.tempSessionDir)
		}
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	id := cch.BeginFromContext(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, []byte(`{"a":1}`))
	require.NotZero(t, id)

	cch.EnsureStreamByContext(c, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	})
	cch.AppendStreamChunkByContext(c, `{"delta":"hello"}`)

	cch.mu.RLock()
	entry := cch.getLocked(id)
	cch.mu.RUnlock()
	require.NotNil(t, entry)

	entry.mu.Lock()
	require.Greater(t, entry.streamChunkBuf.Len(), 0)
	streamPath := entry.streamPath
	entry.mu.Unlock()

	onDiskBeforeFinalize, err := os.ReadFile(streamPath)
	require.NoError(t, err)
	assert.Empty(t, onDiskBeforeFinalize)

	cch.FinalizeStreamAggregatedTextByContext(c, "hello")

	entry.mu.Lock()
	assert.Equal(t, 0, entry.streamChunkBuf.Len())
	entry.mu.Unlock()

	onDiskAfterFinalize, err := os.ReadFile(streamPath)
	require.NoError(t, err)
	assert.Contains(t, string(onDiskAfterFinalize), `"{\"delta\":\"hello\"}"`)

	record, ok := cch.Get(id)
	require.True(t, ok)
	require.NotNil(t, record.Stream)
	assert.Equal(t, []string{`{"delta":"hello"}`}, record.Stream.Chunks)
	assert.Equal(t, "hello", record.Stream.AggregatedText)
}
