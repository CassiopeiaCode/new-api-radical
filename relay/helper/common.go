package helper

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	streamFlushStateContextKey = "stream_flush_state"
	downstreamStreamStateKey   = "downstream_stream_state"
	streamFlushByteThreshold   = 8 << 10
	streamFlushTimeThreshold   = 25 * time.Millisecond
	streamImmediateEventCount  = 3
)

type downstreamStreamState struct {
	mu               sync.RWMutex
	startedAt        time.Time
	keepaliveWritten bool
	semanticWritten  bool
	terminalWritten  bool
	pingTriggered    bool
	suppressSemantic bool
}

func getDownstreamStreamState(c *gin.Context) *downstreamStreamState {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(downstreamStreamStateKey); ok {
		if state, ok := value.(*downstreamStreamState); ok {
			return state
		}
	}
	state := &downstreamStreamState{startedAt: time.Now()}
	c.Set(downstreamStreamStateKey, state)
	return state
}

func InitializeDownstreamStreamState(c *gin.Context) {
	_ = getDownstreamStreamState(c)
}

func BeginStreamAttempt(c *gin.Context) {
	if state := getDownstreamStreamState(c); state != nil {
		state.mu.Lock()
		state.suppressSemantic = false
		state.mu.Unlock()
	}
}

func SuppressAttemptSemanticOutput(c *gin.Context) {
	if state := getDownstreamStreamState(c); state != nil {
		state.mu.Lock()
		state.suppressSemantic = true
		state.mu.Unlock()
	}
}

func shouldSuppressSemanticOutput(c *gin.Context) bool {
	state := getDownstreamStreamState(c)
	if state == nil {
		return false
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.suppressSemantic
}

func NextPingDelay(c *gin.Context, triggerDelay time.Duration, pingInterval time.Duration) time.Duration {
	state := getDownstreamStreamState(c)
	if state == nil {
		return triggerDelay
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.pingTriggered {
		return pingInterval
	}
	remaining := triggerDelay - time.Since(state.startedAt)
	if remaining <= 0 {
		return time.Nanosecond
	}
	return remaining
}

func markKeepaliveWritten(c *gin.Context) {
	if state := getDownstreamStreamState(c); state != nil {
		state.mu.Lock()
		state.keepaliveWritten = true
		state.pingTriggered = true
		state.mu.Unlock()
	}
}

func markSemanticWritten(c *gin.Context) {
	if state := getDownstreamStreamState(c); state != nil {
		state.mu.Lock()
		state.semanticWritten = true
		state.mu.Unlock()
	}
}

func markTerminalWritten(c *gin.Context) {
	if state := getDownstreamStreamState(c); state != nil {
		state.mu.Lock()
		state.semanticWritten = true
		state.terminalWritten = true
		state.mu.Unlock()
	}
}

func HasKeepaliveOnly(c *gin.Context) bool {
	state := getDownstreamStreamState(c)
	if state == nil {
		return false
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.keepaliveWritten && !state.semanticWritten
}

func HasSemanticOutput(c *gin.Context) bool {
	state := getDownstreamStreamState(c)
	if state == nil {
		return false
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.semanticWritten
}

func HasTerminalOutput(c *gin.Context) bool {
	state := getDownstreamStreamState(c)
	if state == nil {
		return false
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.terminalWritten
}

type streamFlushState struct {
	pendingBytes, events int
	lastFlushTime        time.Time
}

func getStreamFlushState(c *gin.Context) *streamFlushState {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(streamFlushStateContextKey); ok {
		if s, ok := v.(*streamFlushState); ok {
			return s
		}
	}
	s := &streamFlushState{lastFlushTime: time.Now()}
	c.Set(streamFlushStateContextKey, s)
	return s
}
func maybeFlushWriter(c *gin.Context, force bool, wrote int) error {
	s := getStreamFlushState(c)
	if s != nil && wrote > 0 {
		s.pendingBytes += wrote
		s.events++
	}
	if !force && s != nil && s.events > streamImmediateEventCount && s.pendingBytes < streamFlushByteThreshold && time.Since(s.lastFlushTime) < streamFlushTimeThreshold {
		return nil
	}
	if err := FlushWriter(c); err != nil {
		return err
	}
	if s != nil {
		s.pendingBytes = 0
		s.lastFlushTime = time.Now()
	}
	return nil
}
func FlushPendingWriter(c *gin.Context) error { return maybeFlushWriter(c, true, 0) }

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	flusher.Flush()
	return nil
}

func requestContextDone(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	if shouldSuppressSemanticOutput(c) {
		return nil
	}
	if requestContextDone(c) {
		return nil
	}

	jsonData, err := common.Marshal(resp)
	if err != nil {
		common.SysError("error marshalling stream response: " + err.Error())
	} else {
		if resp.Type == "error" || resp.Type == "message_stop" {
			markTerminalWritten(c)
		} else {
			markSemanticWritten(c)
		}
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonData)})
	}
	_ = maybeFlushWriter(c, false, len(resp.Type)+len(jsonData)+16)
	return nil
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	if shouldSuppressSemanticOutput(c) {
		return
	}
	if requestContextDone(c) {
		return
	}

	if resp.Type == "error" || resp.Type == "message_stop" {
		markTerminalWritten(c)
	} else {
		markSemanticWritten(c)
	}
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s\n", data)})
	_ = maybeFlushWriter(c, false, len(resp.Type)+len(data)+16)
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	if shouldSuppressSemanticOutput(c) {
		return nil
	}
	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if resp.Type == "response.completed" || resp.Type == "response.done" || resp.Type == "response.incomplete" ||
		resp.Type == "response.failed" || resp.Type == "response.error" || resp.Type == "error" {
		markTerminalWritten(c)
	} else {
		markSemanticWritten(c)
	}
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s", data)})
	return maybeFlushWriter(c, false, len(resp.Type)+len(data)+16)
}

func StringData(c *gin.Context, str string) error {
	if shouldSuppressSemanticOutput(c) {
		return nil
	}
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	markSemanticWritten(c)
	c.Render(-1, common.CustomEvent{Data: "data: " + str})
	return maybeFlushWriter(c, false, len(str)+8)
}

func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	markKeepaliveWritten(c)
	return maybeFlushWriter(c, true, len(": PING\n\n"))
}

func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	if shouldSuppressSemanticOutput(c) {
		return
	}
	if StringData(c, "[DONE]") == nil {
		markTerminalWritten(c)
		_ = FlushPendingWriter(c)
	}
}

func StreamError(c *gin.Context, relayFormat types.RelayFormat, err *types.NewAPIError) error {
	if err == nil {
		return nil
	}
	BeginStreamAttempt(c)
	SetEventStreamHeaders(c)
	switch relayFormat {
	case types.RelayFormatClaude:
		claudeError := err.ToClaudeError()
		return ClaudeData(c, dto.ClaudeResponse{Type: "error", Error: claudeError})
	case types.RelayFormatOpenAIResponses:
		openAIError := err.ToOpenAIError()
		failedStatus := common.StringToByteSlice(`"failed"`)
		payload := dto.ResponsesStreamResponse{
			Type: "response.failed",
			Response: &dto.OpenAIResponsesResponse{
				Status: failedStatus,
				Error:  openAIError,
			},
		}
		data, marshalErr := common.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		markTerminalWritten(c)
		c.Render(-1, common.CustomEvent{Data: "event: response.failed\n"})
		c.Render(-1, common.CustomEvent{Data: "data: " + string(data)})
		return maybeFlushWriter(c, true, len(data)+32)
	default:
		payload := struct {
			Error types.OpenAIError `json:"error"`
		}{Error: err.ToOpenAIError()}
		if writeErr := ObjectData(c, payload); writeErr != nil {
			return writeErr
		}
		Done(c)
		return nil
	}
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
