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

// openAIBindingGrokFixture 构造 openai 分组绑定 grok（supergrok OAuth）账号的调度夹具：
// 分组 ID 7、账号 ID 61，账号仅映射 grok-4.7（与线上 model_mapping 一致）。
func openAIBindingGrokFixture() (*OpenAIGatewayService, *defaultOpenAIAccountScheduler, int64) {
	groupID := int64(7)
	account := Account{
		ID: 61, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		AccountGroups: []AccountGroup{{GroupID: groupID}},
		Credentials: map[string]any{
			"model_mapping": map[string]any{"grok-4.7": "grok-4.7"},
		},
	}
	repo := openAIBindingAccountRepo{schedulerTestOpenAIAccountRepo{accounts: []Account{account}}}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"会话": 61}}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := &OpenAIGatewayService{
		accountRepo: repo, cfg: cfg, cache: cache,
		concurrencyService: NewConcurrencyService(&schedulerTestConcurrencyCache{}),
		schedulerSnapshot: &SchedulerSnapshotService{
			accountRepo: repo, cfg: cfg,
			cache: &openAISnapshotCacheStub{
				snapshotAccounts: []*Account{&account},
				accountsByID:     map[int64]*Account{61: &account},
			},
		},
	}
	return svc, &defaultOpenAIAccountScheduler{service: svc}, groupID
}

// TestOpenAIGatewayBinding_SelectGrokResponsesModel 锁定 openai 分组绑定 grok 账号
// 时 grok-4.7 的选号契约（线上 404 model_not_found 回归）：绑定能力矩阵必须放行
// openai 入站 → grok 账号，四个调度入口都不得因平台相等检查排除该账号。
func TestOpenAIGatewayBinding_SelectGrokResponsesModel(t *testing.T) {
	for _, mode := range []string{"旧调度", "旧负载均衡", "新负载均衡", "新粘性会话"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, scheduler, groupID := openAIBindingGrokFixture()
			req := OpenAIAccountScheduleRequest{
				GroupID: &groupID, Platform: PlatformOpenAI,
				RequestedModel: "grok-4.7", RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
			}
			var selection *AccountSelectionResult
			var err error
			switch mode {
			case "旧调度":
				selected, selectErr := svc.SelectAccountForModel(ctx, &groupID, "", req.RequestedModel)
				require.NoError(t, selectErr)
				require.Equal(t, int64(61), selected.ID)
				return
			case "旧负载均衡":
				selection, err = svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", req.RequestedModel, nil)
			case "新负载均衡":
				selection, _, _, _, err = scheduler.selectByLoadBalance(ctx, req)
			case "新粘性会话":
				req.SessionHash = "会话"
				req.StickyAccountID = 61
				selection, _, err = scheduler.selectBySessionHash(ctx, req)
			}
			require.NoError(t, err)
			require.NotNil(t, selection, "openai 分组绑定的 grok 账号不得被平台相等检查排除")
			require.Equal(t, int64(61), selection.Account.ID)
			require.True(t, selection.Acquired)
			selection.ReleaseFunc()
		})
	}
}

// TestOpenAIGatewayBinding_SelectGrokResponsesCapability 锁定 grok 原生 /v1/responses
// 的能力契约：生图意图与 compact 的 /v1/responses 要求 Responses 能力，
// grok 账号必须通过该能力过滤进入候选池，而不是被 capability_mismatch 拦截。
func TestOpenAIGatewayBinding_SelectGrokResponsesCapability(t *testing.T) {
	for _, mode := range []string{"新负载均衡", "新粘性会话"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			_, scheduler, groupID := openAIBindingGrokFixture()
			req := OpenAIAccountScheduleRequest{
				GroupID: &groupID, Platform: PlatformOpenAI,
				RequestedModel:     "grok-4.7",
				RequiredTransport:  OpenAIUpstreamTransportHTTPSSE,
				RequiredCapability: OpenAIEndpointCapabilityResponses,
			}
			var selection *AccountSelectionResult
			var err error
			if mode == "新粘性会话" {
				req.SessionHash = "会话"
				req.StickyAccountID = 61
				selection, _, err = scheduler.selectBySessionHash(ctx, req)
			} else {
				selection, _, _, _, err = scheduler.selectByLoadBalance(ctx, req)
			}
			require.NoError(t, err)
			require.NotNil(t, selection, "grok 原生 /v1/responses 必须声明 Responses 能力")
			require.Equal(t, int64(61), selection.Account.ID)
			selection.ReleaseFunc()
		})
	}
}

// TestOpenAIGatewayBinding_DiagnoseGrokResponsesModel 锁定 openai 分组模型可用性诊断：
// 候选池必须覆盖可承接 openai 入站的 grok 平台，否则绑定 grok 账号的模型会被误判为
// 404 model_not_found（filtered: model_not_supported 的误导来源）。
func TestOpenAIGatewayBinding_DiagnoseGrokResponsesModel(t *testing.T) {
	groupID := int64(7)
	account := Account{
		ID: 61, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		AccountGroups: []AccountGroup{{GroupID: groupID}},
		Credentials:   map[string]any{"model_mapping": map[string]any{"grok-4.7": "grok-4.7"}},
	}
	svc := &OpenAIGatewayService{
		accountRepo: &mockAccountRepoForPlatform{accounts: []Account{account}},
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "grok-4.7", PlatformOpenAI)
	require.Equal(t, ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}, diag)

	// grok 分组自身诊断结果不受影响。
	diag = svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "grok-4.7", PlatformGrok)
	require.Equal(t, ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}, diag)

	// 账号未映射的模型仍应报"无模型支持"，避免新增平台掩盖真实缺失。
	diag = svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "grok-4.6", PlatformOpenAI)
	require.Equal(t, ModelAvailabilityDiagnosis{HasAccountsInPool: true}, diag)
}
