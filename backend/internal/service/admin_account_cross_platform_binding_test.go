//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureSlogForBindingTest 用内存 handler 替换默认 slog logger，返回 buffer 供断言。
func captureSlogForBindingTest(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	return &buf
}

func mixedChannelMembersByGroup() map[int64][]Account {
	// 分组 10 中已有 Anthropic 账号：antigravity 账号绑定该分组即构成混合渠道风险。
	return map[int64][]Account{
		10: {{ID: 99, Platform: PlatformAnthropic}},
	}
}

func bindingGroupRepo() *groupRepoStubForAdmin {
	return &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Name: "openai-group", Platform: PlatformOpenAI},
		},
	}
}

// TestAdminService_CreateAccount_OpenCodeBindsOpenAIGroupSucceeds 证明 opencode_go 账号
// 绑定 openai 分组可以走完保存路径（跨平台绑定必须成功）。
func TestAdminService_CreateAccount_OpenCodeBindsOpenAIGroupSucceeds(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{createID: 7}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: bindingGroupRepo()}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "opencode account",
		Platform:             PlatformOpenCodeGo,
		Type:                 AccountTypeAPIKey,
		Concurrency:          1,
		GroupIDs:             []int64{10},
		SkipDefaultGroupBind: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.ElementsMatch(t, []int64{10}, accountRepo.bindGroupsByAccount[7])
}

// TestAdminService_CreateAccount_AntigravityBindsAnthropicGroupLogsWarnAndSucceeds 证明
// 原先被混合渠道风险阻断的创建绑定现在放行，并记录中文结构化 slog.Warn 审计日志。
func TestAdminService_CreateAccount_AntigravityBindsAnthropicGroupLogsWarnAndSucceeds(t *testing.T) {
	buf := captureSlogForBindingTest(t)
	accountRepo := &accountRepoStubForBulkUpdate{
		createID:        7,
		listByGroupData: mixedChannelMembersByGroup(),
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: bindingGroupRepo()}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "antigravity account",
		Platform:             PlatformAntigravity,
		Type:                 AccountTypeAPIKey,
		Concurrency:          1,
		GroupIDs:             []int64{10},
		SkipDefaultGroupBind: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.ElementsMatch(t, []int64{10}, accountRepo.bindGroupsByAccount[7])

	logs := buf.String()
	require.Contains(t, logs, "level=WARN")
	require.Contains(t, logs, "混合渠道")
	require.Contains(t, logs, "account_id=0")
	require.Contains(t, logs, "group_id=10")
	require.Contains(t, logs, "account_platform=Antigravity")
	require.Contains(t, logs, "group_platform=Anthropic")
}

// TestAdminService_UpdateAccount_AntigravityBindsAnthropicGroupSucceedsWithAudit 证明
// 更新路径同样放行并记录审计日志。
func TestAdminService_UpdateAccount_AntigravityBindsAnthropicGroupSucceedsWithAudit(t *testing.T) {
	buf := captureSlogForBindingTest(t)
	accountRepo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			7: {ID: 7, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Status: StatusActive, Extra: map[string]any{}},
		},
		listByGroupData: mixedChannelMembersByGroup(),
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: bindingGroupRepo()}
	groupIDs := []int64{10}

	account, err := svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{GroupIDs: &groupIDs})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.Len(t, accountRepo.updatedAccounts, 1)
	require.ElementsMatch(t, []int64{10}, accountRepo.bindGroupsByAccount[7])

	logs := buf.String()
	require.Contains(t, logs, "level=WARN")
	require.Contains(t, logs, "混合渠道")
	require.Contains(t, logs, "account_id=7")
	require.Contains(t, logs, "group_id=10")
	require.Contains(t, logs, "account_platform=Antigravity")
	require.Contains(t, logs, "group_platform=Anthropic")
	// 审计日志不得记录凭据。
	require.NotContains(t, logs, "api_key")
	require.NotContains(t, logs, "credentials")
}

// TestAdminService_BulkUpdateAccounts_MixedChannelConflictWarnsButSaves 取代旧的阻断断言：
// 批量更新遇到混合渠道冲突时放行保存并记录审计日志。
func TestAdminService_BulkUpdateAccounts_MixedChannelConflictWarnsButSaves(t *testing.T) {
	buf := captureSlogForBindingTest(t)
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{ID: 1, Platform: PlatformAntigravity},
		},
		listByGroupData: mixedChannelMembersByGroup(),
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo:   &groupRepoStubForAdmin{getByID: &Group{ID: 10, Name: "target-group"}},
	}

	groupIDs := []int64{10}
	input := &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		GroupIDs:   &groupIDs,
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.ElementsMatch(t, []int64{1}, result.SuccessIDs)
	// 检查仍发生在写操作之前，风险放行后正常绑定。
	require.ElementsMatch(t, []int64{1}, repo.bindGroupsCalls)

	logs := buf.String()
	require.Contains(t, logs, "level=WARN")
	require.Contains(t, logs, "混合渠道")
	require.Contains(t, logs, "account_id=1")
	require.Contains(t, logs, "group_id=10")
	require.Contains(t, logs, "account_platform=Antigravity")
	require.Contains(t, logs, "group_platform=Anthropic")
}

// TestAdminService_UpdateAccount_InvalidGroupStillFails 保留真正的失败校验：分组不存在仍报错。
func TestAdminService_UpdateAccount_InvalidGroupStillFails(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			7: {ID: 7, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Status: StatusActive, Extra: map[string]any{}},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{},
	}}
	groupIDs := []int64{2000}

	account, err := svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{GroupIDs: &groupIDs})

	require.Nil(t, account)
	require.ErrorIs(t, err, ErrGroupNotFound)
	require.Empty(t, accountRepo.bindGroupsByAccount)
}

// TestAdminService_UpdateAccount_MissingAccountStillFails 保留真正的失败校验：账号不存在仍报错。
func TestAdminService_UpdateAccount_MissingAccountStillFails(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: bindingGroupRepo()}
	groupIDs := []int64{10}

	account, err := svc.UpdateAccount(context.Background(), 404, &UpdateAccountInput{GroupIDs: &groupIDs})

	require.Nil(t, account)
	require.Error(t, err)
	require.Empty(t, accountRepo.bindGroupsByAccount)
}

// TestAdminService_CheckMixedChannelRiskEndpointStillReturnsConflict 证明旧只读检查端点
// 背后的 CheckMixedChannelRisk 仍然注册且行为不变（返回混合渠道风险错误）。
func TestAdminService_CheckMixedChannelRiskEndpointStillReturnsConflict(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{listByGroupData: mixedChannelMembersByGroup()}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: bindingGroupRepo()}

	err := svc.CheckMixedChannelRisk(context.Background(), 7, PlatformAntigravity, []int64{10})

	require.Error(t, err)
	var mixedErr *MixedChannelError
	require.True(t, errors.As(err, &mixedErr))
	require.Equal(t, int64(10), mixedErr.GroupID)
	require.Equal(t, "Antigravity", mixedErr.CurrentPlatform)
	require.Equal(t, "Anthropic", mixedErr.OtherPlatform)
	require.True(t, strings.Contains(err.Error(), "mixed_channel_warning"))
}
