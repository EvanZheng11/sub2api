package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// accountTestModelMaxOutputTokens 只读账号缓存的上游模型元数据快照，不发外部请求。
func TestAccountTestModelMaxOutputTokensFromSnapshot(t *testing.T) {
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{
			"claude-sonnet-5": {ID: "claude-sonnet-5", MaxOutputTokens: 64000},
			"claude-empty":    {ID: "claude-empty"},
		},
	})

	require.Equal(t, int64(64000), accountTestModelMaxOutputTokens(account, "claude-sonnet-5"))
	// 快照里未声明输出上限 → 0，表示上限未知。
	require.Zero(t, accountTestModelMaxOutputTokens(account, "claude-empty"))
	// 快照里没有的模型 → 0。
	require.Zero(t, accountTestModelMaxOutputTokens(account, "claude-unknown"))
	// 空账号/空模型 ID 不 panic。
	require.Zero(t, accountTestModelMaxOutputTokens(nil, "claude-sonnet-5"))
	require.Zero(t, accountTestModelMaxOutputTokens(account, "  "))
}
