//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAntigravityConnectionTestModel(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-sonnet-4-6", antigravityConnectionTestModel(""))
	require.Equal(t, "gemini-3.1-pro-preview", antigravityConnectionTestModel("gemini-3.1-pro-preview"))
}

func antigravityPromptTestAccount() *Account {
	return &Account{
		ID:          9301,
		Name:        "antigravity-prompt",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "ag-access-token",
			"expires_at":   time.Now().Add(time.Hour).Format(time.RFC3339),
			"project_id":   "ag-project",
		},
	}
}

func antigravityPromptTestService(account *Account, upstream *httpUpstreamRecorder) *AccountTestService {
	return &AccountTestService{
		accountRepo:  &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
		httpUpstream: upstream,
		cfg:          reasoningTestConfig(),
		antigravityGatewayService: &AntigravityGatewayService{
			tokenProvider: NewAntigravityTokenProvider(nil, nil, nil),
			httpUpstream:  upstream,
		},
	}
}

// Antigravity OAuth 测试必须把真实题目带到上游请求里，不能继续用 "." + maxOutputTokens=1 的固定探测。
func TestAccountTestConnectionAntigravityOAuthUsesRealPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := antigravityPromptTestAccount()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"<svg></svg>\"}]}}]}}\n\n")),
	}}
	svc := antigravityPromptTestService(account, upstream)
	c, rec := newTestContext()

	const prompt = "请生成一个红色圆形 SVG 的完整代码。"
	require.NoError(t, svc.TestAccountConnection(c, account.ID, "claude-sonnet-4-6", prompt, ""))
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, prompt, gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.0.text").String(),
		"自定义题目必须原样进入 Antigravity 上游请求")
	require.Greater(t, gjson.GetBytes(upstream.lastBody, "request.generationConfig.maxOutputTokens").Int(), int64(1),
		"自定义题目不能沿用最小探测的 1 token 输出预算")
	require.Contains(t, rec.Body.String(), "test_complete")
	require.Contains(t, rec.Body.String(), "\"success\":true")
}

// 空题目仍走网关原有的最小探测，保持公共行为不变。
func TestAccountTestConnectionAntigravityEmptyPromptKeepsFixedProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := antigravityPromptTestAccount()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}}\n\n")),
	}}
	svc := antigravityPromptTestService(account, upstream)
	c, _ := newTestContext()

	require.NoError(t, svc.TestAccountConnection(c, account.ID, "claude-sonnet-4-6", "", ""))
	require.Equal(t, ".", gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.0.text").String())
	require.Equal(t, int64(1), gjson.GetBytes(upstream.lastBody, "request.generationConfig.maxOutputTokens").Int())
}
