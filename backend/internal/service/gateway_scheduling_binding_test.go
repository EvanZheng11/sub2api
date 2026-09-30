//go:build unit

package service

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// bindingSchedulingAccountRepo 是绑定维度调度的最小测试桩。
// ListSchedulableByGroupID 返回分组绑定的全部账号且不做平台过滤，
// 与 repository 的绑定维度查询语义一致（设计 §3.3）。
type bindingSchedulingAccountRepo struct {
	AccountRepository
	accounts        []Account
	groupQueryCount int
}

func (r *bindingSchedulingAccountRepo) ListSchedulableByGroupID(_ context.Context, _ int64) ([]Account, error) {
	r.groupQueryCount++
	return r.accounts, nil
}

func (r *bindingSchedulingAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	filtered := make([]Account, 0, len(r.accounts))
	for _, acc := range r.accounts {
		if acc.Platform == platform && acc.IsSchedulable() {
			filtered = append(filtered, acc)
		}
	}
	return filtered, nil
}

func (r *bindingSchedulingAccountRepo) ListSchedulableByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	platformSet := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		platformSet[platform] = struct{}{}
	}
	filtered := make([]Account, 0, len(r.accounts))
	for _, acc := range r.accounts {
		if _, ok := platformSet[acc.Platform]; ok && acc.IsSchedulable() {
			filtered = append(filtered, acc)
		}
	}
	return filtered, nil
}

func (r *bindingSchedulingAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	// 测试桩里所有账号都视为未分组账号：无分组路径保持既有平台过滤行为。
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *bindingSchedulingAccountRepo) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	return r.ListSchedulableByPlatforms(ctx, platforms)
}

func (r *bindingSchedulingAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			acc := r.accounts[i]
			return &acc, nil
		}
	}
	return nil, ErrNoAvailableAccounts
}

func newBindingSchedulingTestService(accounts []Account) (*GatewayService, *bindingSchedulingAccountRepo) {
	repo := &bindingSchedulingAccountRepo{accounts: accounts}
	svc := &GatewayService{
		accountRepo: repo,
		cache:       &mockGatewayCacheForPlatform{},
		cfg:         testConfig(),
	}
	return svc, repo
}

// bindingGroupContext 注入分组上下文，选号前的模型路由查询会读取它，
// 避免测试为了一个分组对象再引入 groupRepo 桩。
func bindingGroupContext(svc *GatewayService, groupID int64, platform string) context.Context {
	return svc.withGroupContext(context.Background(), &Group{
		ID:       groupID,
		Hydrated: true,
		Platform: platform,
		Status:   StatusActive,
	})
}

// captureServiceLogs 把默认 slog 输出重定向到内存缓冲区，用于断言结构化日志。
func captureServiceLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// TestSelectAccountForModelWithPlatform_CrossPlatformBindingSkipped 分组平台 anthropic、
// 绑定候选含 opencode_go 账号：能力矩阵判定无适配器，即使优先级更高也必须跳过（设计 §3.2/§3.3）。
func TestSelectAccountForModelWithPlatform_CrossPlatformBindingSkipped(t *testing.T) {
	svc, repo := newBindingSchedulingTestService([]Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 5, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true},
	})

	ctx := bindingGroupContext(svc, 11, PlatformAnthropic)
	acc, err := svc.selectAccountForModelWithPlatform(ctx, ptr(int64(11)), "", "", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(1), acc.ID, "opencode_go 账号不能承接 anthropic 分组请求")
	require.Equal(t, 1, repo.groupQueryCount, "分组候选必须走绑定维度查询")
}

// TestSelectAccountForModelWithPlatform_AntigravityMixedCandidateAllowed
// anthropic 分组绑定启用 mixed_scheduling 的 antigravity 账号：矩阵显式支持，可以入选。
func TestSelectAccountForModelWithPlatform_AntigravityMixedCandidateAllowed(t *testing.T) {
	svc, _ := newBindingSchedulingTestService([]Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 5, Status: StatusActive, Schedulable: true},
		{ID: 3, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Priority: 2, Status: StatusActive, Schedulable: true,
			Extra: map[string]any{"mixed_scheduling": true}},
	})

	ctx := bindingGroupContext(svc, 12, PlatformAnthropic)
	acc, err := svc.selectAccountForModelWithPlatform(ctx, ptr(int64(12)), "", "", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(3), acc.ID, "mixed_scheduling 开启的 antigravity 账号应可承接 anthropic 分组")
}

// TestSelectAccountForModelWithPlatform_OpenAIxOpenCode openai 分组绑定 opencode_go 账号：
// 能力矩阵已注册 openai -> opencode_go，必须被选中（设计 §3.2）。
func TestSelectAccountForModelWithPlatform_OpenAIxOpenCode(t *testing.T) {
	svc, repo := newBindingSchedulingTestService([]Account{
		{ID: 9, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true},
	})

	ctx := bindingGroupContext(svc, 13, PlatformOpenAI)
	acc, err := svc.selectAccountForModelWithPlatform(ctx, ptr(int64(13)), "", "", nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, int64(9), acc.ID)
	require.Equal(t, 1, repo.groupQueryCount)
}

// TestSelectAccountForModelWithPlatform_NoAdapterError 分组内只有无适配器账号时，
// 选号失败错误必须指出"分组平台 × 账号平台无适配器"，并保持 ErrNoAvailableAccounts 分类，
// 同时记录 binding_capability_unsupported Warn 且不得泄漏账号凭据（设计 §3.2/§6）。
func TestSelectAccountForModelWithPlatform_NoAdapterError(t *testing.T) {
	logs := captureServiceLogs(t)
	svc, _ := newBindingSchedulingTestService([]Account{
		{ID: 5, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"api_key": "sk-binding-secret-must-not-be-logged"}},
	})

	ctx := bindingGroupContext(svc, 14, PlatformAnthropic)
	_, err := svc.selectAccountForModelWithPlatform(ctx, ptr(int64(14)), "", "glm-5.3", nil, PlatformAnthropic)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts, "错误必须保持 503 分类")
	require.Contains(t, err.Error(), `group platform "anthropic"`, "错误文案需含分组平台: %v", err)
	require.Contains(t, err.Error(), `has no adapter for account platform "opencode_go"`, "错误文案需含账号平台: %v", err)
	require.Contains(t, err.Error(), "glm-5.3", "错误文案需含模型: %v", err)

	logText := logs.String()
	require.Contains(t, logText, "binding_capability_unsupported", "必须记录结构化 Warn")
	require.Contains(t, logText, "group_platform=anthropic")
	require.Contains(t, logText, "account_platform=opencode_go")
	require.Contains(t, logText, "model=glm-5.3")
	require.NotContains(t, logText, "sk-binding-secret-must-not-be-logged", "日志不得输出账号凭据")
}

// TestSelectAccountForModelWithPlatform_MixedSchedulingUnchanged anthropic 分组 ×
// antigravity 账号：mixed_scheduling 开关语义不变（开=可入选，关=不入选，设计 §3.4）。
func TestSelectAccountForModelWithPlatform_MixedSchedulingUnchanged(t *testing.T) {
	svcOn, _ := newBindingSchedulingTestService([]Account{
		{ID: 21, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true,
			Extra: map[string]any{"mixed_scheduling": true}},
	})
	ctxOn := bindingGroupContext(svcOn, 21, PlatformAnthropic)
	acc, err := svcOn.selectAccountForModelWithPlatform(ctxOn, ptr(int64(21)), "", "", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(21), acc.ID)

	svcOff, _ := newBindingSchedulingTestService([]Account{
		{ID: 22, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true,
			Extra: map[string]any{"mixed_scheduling": false}},
	})
	ctxOff := bindingGroupContext(svcOff, 22, PlatformAnthropic)
	_, err = svcOff.selectAccountForModelWithPlatform(ctxOff, ptr(int64(22)), "", "", nil, PlatformAnthropic)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
}

// TestListSchedulableAccounts_UngroupedPathKeepsPlatformFilter 无分组 API key 路径
// 没有绑定关系，必须继续按账号平台过滤（opencode_go 账号不得进入 anthropic 无分组候选）。
func TestListSchedulableAccounts_UngroupedPathKeepsPlatformFilter(t *testing.T) {
	svc, repo := newBindingSchedulingTestService([]Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 2, Status: StatusActive, Schedulable: true},
	})

	accounts, useMixed, err := svc.listSchedulableAccounts(context.Background(), nil, PlatformAnthropic, false)
	require.NoError(t, err)
	require.True(t, useMixed)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(1), accounts[0].ID)
	require.Zero(t, repo.groupQueryCount, "无分组路径不得走绑定维度查询")
}

// TestSchedulerSnapshotLoadAccountsFromDB_BindingDimension 快照加载与直查一致：
// 分组路径按绑定关系取候选，再在内存按能力矩阵过滤（设计 §3.3）。
func TestSchedulerSnapshotLoadAccountsFromDB_BindingDimension(t *testing.T) {
	repo := &bindingSchedulingAccountRepo{accounts: []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 2, Status: StatusActive, Schedulable: true},
		{ID: 3, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Priority: 3, Status: StatusActive, Schedulable: true,
			Extra: map[string]any{"mixed_scheduling": true}},
	}}
	svc := &SchedulerSnapshotService{accountRepo: repo, cfg: testConfig()}

	accounts, err := svc.loadAccountsFromDB(context.Background(), SchedulerBucket{
		GroupID:  7,
		Platform: PlatformAnthropic,
		Mode:     SchedulerModeMixed,
	}, false)
	require.NoError(t, err)
	require.Equal(t, 1, repo.groupQueryCount, "分组快照必须走绑定维度查询")

	gotIDs := make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		gotIDs = append(gotIDs, acc.ID)
	}
	require.Equal(t, []int64{1, 3}, gotIDs, "只保留能力矩阵允许的同平台与 antigravity mixed 账号")
}

// TestBindingUnsupportedSelectionError_SkippedPlatformsSorted 多个无适配器平台时，
// 错误文案与 Warn 字段稳定排序，便于排查（设计 §3.2）。
func TestBindingUnsupportedSelectionError_SkippedPlatformsSorted(t *testing.T) {
	logs := captureServiceLogs(t)
	svc, _ := newBindingSchedulingTestService([]Account{
		{ID: 1, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformGrok, Type: AccountTypeAPIKey, Priority: 2, Status: StatusActive, Schedulable: true},
	})

	ctx := bindingGroupContext(svc, 15, PlatformAnthropic)
	_, err := svc.selectAccountForModelWithPlatform(ctx, ptr(int64(15)), "", "glm-5.3", nil, PlatformAnthropic)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Contains(t, err.Error(), `has no adapter for account platform "grok", "opencode_go"`, "平台列表需稳定排序: %v", err)
	require.Contains(t, logs.String(), "account_platform=grok,opencode_go")
}

// TestBindingUnsupportedSelectionError_NoGroupKeepsLegacyError 无分组失败不进入
// 绑定能力错误分支（无绑定关系可言），沿用既有错误文案。
func TestBindingUnsupportedSelectionError_NoGroupKeepsLegacyError(t *testing.T) {
	svc, _ := newBindingSchedulingTestService(nil)

	err := svc.bindingUnsupportedSelectionError(context.Background(), nil, PlatformAnthropic, "glm-5.3")
	require.NoError(t, err, "无分组时不应产生绑定能力错误")

	err = svc.bindingUnsupportedSelectionError(context.Background(), ptr(int64(31)), PlatformOpenAI, "glm-5.3")
	require.NoError(t, err, "无候选账号时不应产生绑定能力错误")
}
