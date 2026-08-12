package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeakProtectionScansFromSecondMostRecentUserMessage(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "old-user"},
			{Role: "assistant", Content: "old-assistant"},
			{Role: "user", Content: "second-latest-user"},
			{Role: "assistant", Content: "assistant-after-second"},
			{Role: "tool", Content: "tool-after-second"},
			{Role: "user", Content: "latest-user"},
			{Role: "assistant", Content: "latest-assistant"},
		},
	}

	meta := leakProtectionTokenCountMeta(request)
	require.NotNil(t, meta)
	assert.NotContains(t, meta.CombineText, "old-user")
	assert.NotContains(t, meta.CombineText, "old-assistant")
	assert.Contains(t, meta.CombineText, "second-latest-user")
	assert.Contains(t, meta.CombineText, "assistant-after-second")
	assert.Contains(t, meta.CombineText, "tool-after-second")
	assert.Contains(t, meta.CombineText, "latest-user")
	assert.Contains(t, meta.CombineText, "latest-assistant")
}

func TestLeakProtectionKeepsNonChatPrompt(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Prompt: "standalone prompt"}

	meta := leakProtectionTokenCountMeta(request)
	require.NotNil(t, meta)
	assert.Contains(t, meta.CombineText, "standalone prompt")
}
