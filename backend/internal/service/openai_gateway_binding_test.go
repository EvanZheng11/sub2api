//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIBindingAccountRepo struct {
	schedulerTestOpenAIAccountRepo
}

func (r openAIBindingAccountRepo) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]Account, error) {
	var accounts []Account
	for _, account := range r.accounts {
		if openAIStickyAccountMatchesGroup(&account, &groupID) && account.IsSchedulable() {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func TestOpenAIGatewayBinding_SelectOpenCodeDeepseek(t *testing.T) {
	for _, mode := range []string{"旧调度", "旧负载均衡", "新负载均衡", "新粘性会话"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			groupID := int64(2)
			account := Account{
				ID: 53, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true, Concurrency: 1,
				AccountGroups: []AccountGroup{{GroupID: groupID}},
				Credentials: map[string]any{
					"base_url":      "https://opencode.ai/zen/go/v1",
					"model_mapping": map[string]any{"deepseek-v4.1-flash": "deepseek-v4.1-flash"},
				},
			}
			repo := openAIBindingAccountRepo{schedulerTestOpenAIAccountRepo{accounts: []Account{account}}}
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"会话": 53}}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			svc := &OpenAIGatewayService{
				accountRepo: repo, cfg: cfg, cache: cache,
				concurrencyService: NewConcurrencyService(&schedulerTestConcurrencyCache{}),
				schedulerSnapshot: &SchedulerSnapshotService{
					accountRepo: repo, cfg: cfg,
					cache: &openAISnapshotCacheStub{
						snapshotAccounts: []*Account{&account},
						accountsByID:     map[int64]*Account{53: &account},
					},
				},
			}
			scheduler := &defaultOpenAIAccountScheduler{service: svc}
			req := OpenAIAccountScheduleRequest{
				GroupID: &groupID, Platform: PlatformOpenAI,
				RequestedModel: "deepseek-v4.1-flash", RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
			}
			var selection *AccountSelectionResult
			var err error
			switch mode {
			case "旧调度":
				selected, selectErr := svc.SelectAccountForModel(ctx, &groupID, "", req.RequestedModel)
				require.NoError(t, selectErr)
				require.Equal(t, int64(53), selected.ID)
				return
			case "旧负载均衡":
				selection, err = svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", req.RequestedModel, nil)
			case "新负载均衡":
				selection, _, _, _, err = scheduler.selectByLoadBalance(ctx, req)
			case "新粘性会话":
				req.SessionHash = "会话"
				req.StickyAccountID = 53
				selection, _, err = scheduler.selectBySessionHash(ctx, req)
			}
			require.NoError(t, err)
			require.NotNil(t, selection, "真实绑定的 OpenCode 账号不得被平台相等检查排除")
			require.Equal(t, int64(53), selection.Account.ID)
			require.True(t, selection.Acquired)
			selection.ReleaseFunc()
		})
	}
}

func TestOpenAIGatewayBinding_DiagnoseOpenCodeDeepseek(t *testing.T) {
	groupID := int64(2)
	cooldown := time.Now().Add(time.Hour)
	account := Account{
		ID: 53, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
		AccountGroups: []AccountGroup{{GroupID: groupID}},
		Credentials:   map[string]any{"model_mapping": map[string]any{"deepseek-v4.1-flash": "deepseek-v4.1-flash"}},
	}
	for _, transientlyBlocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "正常", true: "临时限流不应误报404"}[transientlyBlocked], func(t *testing.T) {
			candidate := account
			if transientlyBlocked {
				candidate.RateLimitResetAt = &cooldown
				candidate.TempUnschedulableUntil = &cooldown
			}
			svc := &OpenAIGatewayService{
				accountRepo: &mockAccountRepoForPlatform{accounts: []Account{
					candidate,
					{ID: 52, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false,
						AccountGroups: []AccountGroup{{GroupID: groupID}}, Credentials: account.Credentials},
				}},
			}
			diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "deepseek-v4.1-flash", PlatformOpenAI)
			require.Equal(t, ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}, diag)
			for _, platform := range []string{PlatformGrok, PlatformDeepseek} {
				diag = svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "deepseek-v4.1-flash", platform)
				require.Equal(t, ModelAvailabilityDiagnosis{}, diag, "未注册的跨平台组合不能污染诊断")
			}
			diag = svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "未配置模型", PlatformOpenAI)
			require.Equal(t, ModelAvailabilityDiagnosis{HasAccountsInPool: true}, diag)
			diag = svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "deepseek-v4.1-flash", PlatformOpenAI)
			require.Equal(t, ModelAvailabilityDiagnosis{}, diag, "无分组请求不可见已绑定账号")
		})
	}
}
