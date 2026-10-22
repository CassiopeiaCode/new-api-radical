package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHistoricalRankingVendor(t *testing.T) {
	for _, tc := range []struct{ name, vendor string }{
		{"deepseek-v4.1-flash(fingerprint≈k82)", "DeepSeek"},
		{"minimax/minimax-m3.1-flash(fingerprint≈k181)", "MiniMax"},
		{"earth/vip-gpt-5.6-sol(≈claude-opus-5)", "OpenAI"},
		{"unidentified(≈gpt-5)", rankingUnknownVendor},
	} {
		assert.Equal(t, tc.vendor, modelMeta(tc.name, map[string]rankingModelMeta{}).vendor)
	}
	assert.Equal(t, "Custom", modelMeta("gpt-custom", map[string]rankingModelMeta{"gpt-custom": {vendor: "Custom"}}).vendor)
	assert.Equal(t, "DeepSeek", modelMeta("deepseek-v4", map[string]rankingModelMeta{"deepseek-v4": {vendor: rankingUnknownVendor}}).vendor)
}
