//go:build unit

package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func reasoningTestConfig() *config.Config {
	return rawChatCompletionsTestConfig()
}

func reasoningTestService(account *Account, upstream *httpUpstreamRecorder) *AccountTestService {
	return &AccountTestService{
		accountRepo:  &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
		httpUpstream: upstream,
		cfg:          reasoningTestConfig(),
	}
}

func reasoningTestClaudeAccount() *Account {
	return &Account{
		ID:          9101,
		Name:        "reasoning-claude",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-ant-test",
			"base_url": "http://anthropic.example",
		},
	}
}

func reasoningTestOpenAIAccount() *Account {
	return &Account{
		ID:          9102,
		Name:        "reasoning-openai",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-openai-test",
			"base_url": "http://openai.example",
		},
	}
}

func reasoningTestGrokAccount() *Account {
	return &Account{
		ID:          9103,
		Name:        "reasoning-grok",
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "xai-test",
			"base_url": "http://grok.example",
		},
	}
}

// 真实题目应从 Claude 测试 payload 原样发往上游，effort 走 output_config.effort。
func TestCreateTestPayloadUsesPromptAndReasoningEffort(t *testing.T) {
	payload, err := createTestPayload("claude-opus-5", "请用三段文字说明什么是幂等性。", "high")
	require.NoError(t, err)

	require.Equal(t, "请用三段文字说明什么是幂等性。", gjson.GetBytes(reasoningTestJSON(t, payload), "messages.0.content.0.text").String())
	require.Equal(t, "high", gjson.GetBytes(reasoningTestJSON(t, payload), "output_config.effort").String())

	legacy, err := createTestPayload("claude-opus-5", "", "")
	require.NoError(t, err)
	require.Equal(t, "hi", gjson.GetBytes(reasoningTestJSON(t, legacy), "messages.0.content.0.text").String())
	require.False(t, gjson.GetBytes(reasoningTestJSON(t, legacy), "output_config").Exists())
}

func TestCreateOpenAIResponsesTestPayloadUsesPromptAndReasoningEffort(t *testing.T) {
	payload := createOpenAIResponsesTestPayload("gpt-5.4", false, "用 JSON 返回三天天气。", "medium")
	body := reasoningTestJSON(t, payload)

	require.Equal(t, "用 JSON 返回三天天气。", gjson.GetBytes(body, "input.0.content.0.text").String())
	require.Equal(t, "medium", gjson.GetBytes(body, "reasoning.effort").String())

	legacy := reasoningTestJSON(t, createOpenAITestPayload("gpt-5.4", false))
	require.Equal(t, "hi", gjson.GetBytes(legacy, "input.0.content.0.text").String())
	require.False(t, gjson.GetBytes(legacy, "reasoning").Exists())
}

func TestCreateOpenAIChatCompletionsTestPayloadUsesPromptAndReasoningEffort(t *testing.T) {
	payload := createOpenAIChatCompletionsTestPayload("gpt-5.4", "写一份 CSV 示例。", "low")
	require.Equal(t, "写一份 CSV 示例。", gjson.GetBytes(reasoningTestJSON(t, payload), "messages.0.content").String())
	require.Equal(t, "low", gjson.GetBytes(reasoningTestJSON(t, payload), "reasoning_effort").String())
}

func TestCreateGeminiTestPayloadUsesPromptAndThinkingLevel(t *testing.T) {
	payload := createGeminiTestPayload("gemini-3.1-pro-preview", "解释一下 SQL 注入。", "high")
	require.Equal(t, "解释一下 SQL 注入。", gjson.GetBytes(payload, "contents.0.parts.0.text").String())
	require.Equal(t, "high", gjson.GetBytes(payload, "generationConfig.thinkingConfig.thinkingLevel").String())
}

func TestCreateGrokResponsesTestPayloadUsesPromptAndReasoningEffort(t *testing.T) {
	payload, err := createGrokResponsesTestPayload("grok-4.5", "解析这段 Markdown 表格。", "medium")
	require.NoError(t, err)
	require.Equal(t, "解析这段 Markdown 表格。", gjson.GetBytes(payload, "input").String())
	require.Equal(t, "medium", gjson.GetBytes(payload, "reasoning.effort").String())

	legacy, err := createGrokResponsesTestPayload("grok-4.5", "", "")
	require.NoError(t, err)
	require.Equal(t, grokQuotaProbeInput, gjson.GetBytes(legacy, "input").String())
	require.False(t, gjson.GetBytes(legacy, "reasoning").Exists())
}

// 自定义题目需要足够输出预算（SVG 等长文本极易被 1024 截断）；空题目保留原最小探测预算。
func TestCreateTestPayloadCustomPromptGetsLargerOutputBudget(t *testing.T) {
	custom, err := createTestPayload("claude-opus-5", "请生成一个 SVG 时钟的完整代码。", "")
	require.NoError(t, err)
	require.Equal(t, int64(8192), gjson.GetBytes(reasoningTestJSON(t, custom), "max_tokens").Int())

	probe, err := createTestPayload("claude-opus-5", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1024), gjson.GetBytes(reasoningTestJSON(t, probe), "max_tokens").Int())

	// Bedrock 空探测保留 256，自定义题目同样放大。
	require.Equal(t, 256, accountTestOutputBudget("", 256))
	require.Equal(t, 8192, accountTestOutputBudget("写一个 SVG", 256))
}

// compact 探测不执行自定义题目：必须在任何 SSE 写入前返回 400 级错误。
func TestTestAccountConnectionRejectsCompactWithCustomPromptBeforeSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := reasoningTestOpenAIAccount()
	upstream := &httpUpstreamRecorder{}
	svc := reasoningTestService(account, upstream)
	c, recorder := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "请生成一个 SVG 圆环。", AccountTestModeCompact)
	require.Error(t, err)
	var reqErr *AccountTestRequestError
	require.True(t, errors.As(err, &reqErr))
	require.Contains(t, reqErr.Error(), "compact")
	require.Nil(t, upstream.lastReq)
	require.Empty(t, recorder.Body.String())
}

// 图片模型没有 compact 语义：必须在任何 SSE 前 400，而不是误报成功。
func TestTestAccountConnectionRejectsCompactImageModelBeforeSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := reasoningTestOpenAIAccount()
	upstream := &httpUpstreamRecorder{}
	svc := reasoningTestService(account, upstream)
	c, recorder := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "gpt-image-1", "", AccountTestModeCompact)
	require.Error(t, err)
	var reqErr *AccountTestRequestError
	require.True(t, errors.As(err, &reqErr))
	require.Contains(t, reqErr.Error(), "图片模型")
	require.Nil(t, upstream.lastReq)
	require.Empty(t, recorder.Body.String())
}

func TestResolveAccountTestReasoningEffort(t *testing.T) {
	claudeAccount := reasoningTestClaudeAccount()
	openAIAccount := reasoningTestOpenAIAccount()
	grokAccount := reasoningTestGrokAccount()
	geminiAccount := &Account{ID: 9104, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "gemini-test"}}
	cnAccount := adaptiveCNAccountTestAccount(9105, PlatformZhipu)

	cases := []struct {
		name    string
		account *Account
		model   string
		mode    string
		effort  string
		want    string
		wantErr string
	}{
		{name: "claude catalog accepts high", account: claudeAccount, model: "claude-opus-5", effort: "high", want: "high"},
		{name: "claude rejects unknown level", account: claudeAccount, model: "claude-opus-5", effort: "banana", wantErr: "reasoning_effort"},
		{name: "claude model without catalog rejects", account: claudeAccount, model: "claude-3-5-sonnet", effort: "high", wantErr: "不支持"},
		{name: "openai gpt-5 accepts high", account: openAIAccount, model: "gpt-5.4", effort: "high", want: "high"},
		{name: "openai model without max rejects max", account: openAIAccount, model: "gpt-5.4", effort: "max", wantErr: "reasoning_effort"},
		{name: "openai max-capable model accepts max", account: openAIAccount, model: "gpt-5.6", effort: "max", want: "max"},
		{name: "openai non reasoning model rejects", account: openAIAccount, model: "gpt-4o", effort: "high", wantErr: "不支持"},
		{name: "openai compact mode rejects", account: openAIAccount, model: "gpt-5.4", mode: AccountTestModeCompact, effort: "high", wantErr: "不支持"},
		{name: "gemini 3 accepts high", account: geminiAccount, model: "gemini-3.1-pro-preview", effort: "high", want: "high"},
		{name: "gemini 2.5 rejects", account: geminiAccount, model: "gemini-2.5-flash", effort: "high", wantErr: "不支持"},
		{name: "grok 4.5 accepts high", account: grokAccount, model: "grok-4.5", effort: "high", want: "high"},
		{name: "grok 4.6 accepts xhigh", account: grokAccount, model: "grok-4.6", effort: "xhigh", want: "xhigh"},
		{name: "grok 4.5 rejects xhigh", account: grokAccount, model: "grok-4.5", effort: "xhigh", wantErr: "reasoning_effort"},
		{name: "grok image mode rejects", account: grokAccount, model: "grok-4.5", mode: AccountTestModeGrokImage, effort: "high", wantErr: "不支持"},
		{name: "cn provider rejects", account: cnAccount, model: "glm-4.7", effort: "high", wantErr: "不支持"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveAccountTestReasoningEffort(tc.account, tc.model, tc.mode, tc.effort)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				var reqErr *AccountTestRequestError
				require.True(t, errors.As(err, &reqErr))
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestTestAccountConnectionClaudeUsesRealPromptAndEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := reasoningTestClaudeAccount()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"message":"probe only"}}`)),
	}}
	svc := reasoningTestService(account, upstream)
	c, _ := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "claude-opus-5", "请用中文写出三条测试建议。", "", AccountTestOptions{ReasoningEffort: "high"})
	require.Error(t, err)
	require.Equal(t, "请用中文写出三条测试建议。", gjson.GetBytes(upstream.lastBody, "messages.0.content.0.text").String())
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
	require.Contains(t, upstream.lastReq.Header.Get("anthropic-beta"), claude.BetaEffort)
}

func TestTestAccountConnectionRejectsUnknownEffortBeforeSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := reasoningTestClaudeAccount()
	upstream := &httpUpstreamRecorder{}
	svc := reasoningTestService(account, upstream)
	c, recorder := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "claude-opus-5", "hello", "", AccountTestOptions{ReasoningEffort: "banana"})
	require.Error(t, err)
	var reqErr *AccountTestRequestError
	require.True(t, errors.As(err, &reqErr))
	require.Contains(t, reqErr.Error(), "reasoning_effort")
	require.Nil(t, upstream.lastReq)
	require.Empty(t, recorder.Body.String())
}

// gpt-6.1-sol 复用网关既有校验：minimal/none 被明确禁用，测试矩阵不得给出该档位，
// 直传非法档位必须在写入 SSE 之前按 400 级错误拒绝，不能静默降级成其它档位。
func TestTestAccountConnectionRejectsGPT61SolMinimalBeforeSSE(t *testing.T) {
	account := reasoningTestOpenAIAccount()
	for _, model := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol_minimal", "gpt-6.1-sol-low"} {
		support, ok := accountTestReasoningSupportFor(account, model, AccountTestModeDefault)
		require.True(t, ok, model)
		require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, support.Options, model)
	}

	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{}
	svc := reasoningTestService(account, upstream)
	c, recorder := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "gpt-6.1-sol", "hello", "", AccountTestOptions{ReasoningEffort: "minimal"})
	require.Error(t, err)
	var reqErr *AccountTestRequestError
	require.True(t, errors.As(err, &reqErr))
	require.Contains(t, reqErr.Error(), "reasoning_effort")
	require.Nil(t, upstream.lastReq)
	require.Empty(t, recorder.Body.String())
}

func TestTestAccountConnectionOpenAIResponsesUsesRealPromptAndEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := reasoningTestOpenAIAccount()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"probe only"}}`)),
	}}
	svc := reasoningTestService(account, upstream)
	c, _ := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "用 Markdown 写一份部署检查清单。", "", AccountTestOptions{ReasoningEffort: "medium"})
	require.Error(t, err)
	require.Equal(t, "用 Markdown 写一份部署检查清单。", gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())
	require.Equal(t, "medium", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
}

func TestTestAccountConnectionGrokResponsesUsesRealPromptAndEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := reasoningTestGrokAccount()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\"}\n\n",
		)),
	}}
	svc := reasoningTestService(account, upstream)
	c, _ := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "grok-4.5", "请输出一个 JSON 对象。", "", AccountTestOptions{ReasoningEffort: "high"})
	require.NoError(t, err)
	require.Equal(t, "请输出一个 JSON 对象。", gjson.GetBytes(upstream.lastBody, "input").String())
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
}

func reasoningTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body
}
