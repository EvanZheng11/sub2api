package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestShouldRecordChatUsage(t *testing.T) {
	require.False(t, shouldRecordChatUsage(nil, false))
	require.False(t, shouldRecordChatUsage(&service.OpenAIForwardResult{}, true))
	require.True(t, shouldRecordChatUsage(&service.OpenAIForwardResult{}, false))
	require.False(t, shouldRecordChatUsage(&service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{}}, true))
	for _, usage := range []service.OpenAIUsage{
		{InputTokens: 1}, {OutputTokens: 1}, {CacheReadInputTokens: 1},
		{CacheCreationInputTokens: 1}, {ImageInputTokens: 1},
		{ImageOutputTokens: 1}, {ImageCacheReadTokens: 1},
	} {
		require.True(t, shouldRecordChatUsage(&service.OpenAIForwardResult{Usage: usage}, true))
	}
	for _, result := range []*service.OpenAIForwardResult{
		{ImageCount: 1}, {VideoCount: 1}, {SearchCount: 1}, {WebSearchCalls: 1},
		{AudioUsage: &service.AudioUsage{DurationOrUnits: 0.1}},
	} {
		require.True(t, shouldRecordChatUsage(result, true))
	}
}
